import '../services/audio_handler.dart';
import '../services/network_policy.dart';
import '../services/playback_cache_access.dart';
import '../api/player_api_client.dart';
import 'dart:async';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:just_audio/just_audio.dart';
import '../api/dio_client.dart';
import '../services/playback_cache.dart';
import '../services/playback_request.dart';
import '../services/playback_preparer.dart';
import '../services/playback_source_resolver.dart';
import '../services/stream_quality.dart';
import 'api_client_provider.dart';
import 'audio_handler_provider.dart';
import 'auth_state_provider.dart';
import 'playback_preparer_provider.dart';
import 'server_endpoint_provider.dart';

final playbackCacheProvider = ChangeNotifierProvider<PlaybackCache>((ref) {
  final cache = PlaybackCache();
  // Local cached playback can start without creating an online audio loader.
  // Track source release here so cold offline playback has the same eviction
  // protection lifecycle as authenticated cached streaming.
  final handler = ref.read(audioHandlerProvider);
  final subscription = handler.player.sequenceStateStream.listen((state) {
    final source = state?.currentSource;
    cache.playingUri(source is UriAudioSource ? source.uri : null);
  });
  ref.onDispose(() => unawaited(subscription.cancel()));
  MusicNetworkPolicy.changes.addListener(cache.cancel);
  ref.onDispose(() => MusicNetworkPolicy.changes.removeListener(cache.cancel));
  ref.listen(authStateProvider, (_, __) => cache.release());
  ref.listen(playerBaseUrlProvider, (_, __) => cache.release());
  return cache;
});

typedef CachedAudioLoader = Future<AudioSource?> Function(
    PlaybackRequest, bool Function(),
    {void Function(PlaybackPreparation)? onPreparing});

/// Captures account dependencies for queue callbacks that outlive their screen.
// A loader can outlive its screen in a music queue. Key it by the credential
// generation so an old queue keeps its revoked lease while a new sign-in gets
// a fresh loader, even when the API client and endpoint did not change.
final cachedAudioLoaderProvider =
    Provider.family<CachedAudioLoader, int>((ref, generation) {
  final cache = ref.read(playbackCacheProvider);
  final client = ref.watch(apiClientProvider);
  final router = ref.watch(serverEndpointRouterProvider);
  final storage = ref.read(tokenStorageProvider),
      jar = ref.read(cookieJarProvider);
  final mutations = ref.read(credentialMutationQueueProvider);
  final preparer = ref.read(playbackPreparerProvider);
  final handler = ref.read(audioHandlerProvider);
  MusicNetworkPolicy.changes.addListener(handler.refreshGaplessPolicy);
  ref.onDispose(() =>
      MusicNetworkPolicy.changes.removeListener(handler.refreshGaplessPolicy));
  ServerPlaybackRequest? currentRequest;
  bool Function()? currentLease;
  var currentCacheReady = false;
  String? prefetchingKey;
  var disposed = false;
  late Future<void> Function() prefetchNext;
  // Decoder preparation must not cancel current downloads, change cache
  // protection, or publish a different playing URI. ExoPlayer will buffer
  // this authenticated source itself as the second concatenating child.
  handler.prepareGaplessSource = (request, current) async {
    bool wanted() =>
        current() &&
        generation == mutations.generation &&
        mutations.credentialsEnabled &&
        request is ServerPlaybackRequest &&
        MusicNetworkPolicy.prefetchAllowed &&
        mutations.credentialOrigin == request.serverOrigin;
    if (!wanted() ||
        request is! ServerPlaybackRequest ||
        !MusicNetworkPolicy.prefetchAllowed) {
      return null;
    }
    final canonical = await currentServerAudioUri(request, client);
    if (!wanted()) return null;
    final headers = await accountRequestHeaders(
        uri: canonical,
        baseUrl: Uri.parse(request.serverOrigin),
        storage: storage,
        cookieJar: jar,
        mutations: mutations);
    if (!wanted()) return null;
    if (isCompatStreamUrl(canonical)) {
      final ready = await preparer
          .prepare(canonical, probe: client.probePlayback, stillWanted: wanted)
          .ready;
      if (!ready || !wanted()) return null;
    }
    final transport = await router.mediaUri(canonical);
    if (!wanted() || !MusicNetworkPolicy.prefetchAllowed) return null;
    return AudioSource.uri(transport,
        headers: headers.isEmpty ? null : headers);
  };
  Future<AudioSource?> load(PlaybackRequest request, bool Function() current,
      {bool prefetch = false,
      void Function(PlaybackPreparation)? onPreparing}) async {
    if (request is! ServerPlaybackRequest) {
      return AudioSource.uri(request.sourceUri);
    }
    bool wanted() =>
        current() &&
        generation == mutations.generation &&
        mutations.credentialsEnabled &&
        mutations.credentialOrigin == request.serverOrigin;
    if (!wanted()) return null;
    await MusicNetworkPolicy.initialize();
    if (prefetch && !MusicNetworkPolicy.prefetchAllowed) return null;
    MusicNetworkPolicy.assertStreamingAllowed();
    final quality = StreamQuality.selected(request.serverOrigin);
    var cacheReady = request.userId > 0;
    try {
      if (cacheReady) await cache.initialize();
    } catch (_) {
      cacheReady = false;
    }
    final key = cache.key(
        request.serverOrigin, request.userId, request.mediaId, quality);
    if (!prefetch) cache.cancel();
    Uri? saved;
    try {
      if (cacheReady) saved = await cache.lookup(key, protect: !prefetch);
    } catch (_) {
      cacheReady = false;
    }
    Uri canonical, transport;
    Map<String, String> headers;
    try {
      canonical =
          await currentServerAudioUri(request, client, quality: quality);
      if (!wanted()) return null;
      MusicNetworkPolicy.assertStreamingAllowed();
      headers = await accountRequestHeaders(
          uri: canonical,
          baseUrl: Uri.parse(request.serverOrigin),
          storage: storage,
          cookieJar: jar,
          mutations: mutations);
      transport = await router.mediaUri(canonical);
      if (saved != null) {
        if (!await validateCachedAudio(transport, headers, cache.etag(key))) {
          cache.playingUri(null);
          await cache.remove(key);
          saved = null;
        }
      }

      if (saved == null && isCompatStreamUrl(canonical)) {
        final preparation = preparer.prepare(canonical,
            probe: client.probePlayback, stillWanted: wanted);
        onPreparing?.call(preparation);
        final ready = await preparation.ready;
        if (!ready) return null;
      }
    } catch (error) {
      MusicNetworkPolicy.assertStreamingAllowed();
      if (cacheAccessDenied(error)) await cache.invalidate(key);
      final network = cacheTransportFailure(error);
      if (network && saved != null && wanted()) {
        MusicNetworkPolicy.assertStreamingAllowed();
        if (!prefetch) {
          currentRequest = request;
          currentLease = wanted;
          currentCacheReady = cacheReady;
        }
        unawaited(prefetchNext().catchError((Object _) {}));
        return AudioSource.uri(saved);
      }
      rethrow;
    }
    if (!wanted()) return null;
    Future<void> fill() async {
      var automationMusicEligible = false;
      try {
        automationMusicEligible =
            (await client.musicAlbums(mediaId: request.mediaId)).isNotEmpty;
      } catch (_) {/* Catalog failure cannot block ordinary playback/cache. */}
      return cache.download(
          key: key,
          uri: transport,
          headers: headers,
          title: request.title,
          mediaId: request.mediaId,
          music: request.music,
          automationMusicEligible: automationMusicEligible,
          sourceName: request.sourceName,
          origin: request.serverOrigin,
          userId: request.userId,
          authorized: () =>
              wanted() &&
              !MusicNetworkPolicy.streamBlocked &&
              (!prefetch || MusicNetworkPolicy.prefetchAllowed));
    }

    if (cacheReady && saved == null && cache.completeBuffer && !prefetch) {
      try {
        await fill();
        saved = await cache.lookup(key);
      } catch (_) {
        cacheReady = false;
      }
    }
    if (!prefetch) {
      currentRequest = request;
      currentLease = wanted;
      currentCacheReady = cacheReady;
    }
    if (cacheReady && saved == null) {
      final transfer = fill();
      if (prefetch) {
        await transfer;
      } else {
        unawaited(
            transfer.then((_) => prefetchNext()).catchError((Object _) {}));
      }
    } else if (!prefetch) {
      unawaited(prefetchNext().catchError((Object _) {}));
    }
    MusicNetworkPolicy.assertStreamingAllowed();
    return AudioSource.uri(saved ?? transport,
        headers: saved != null || headers.isEmpty ? null : headers);
  }

  ServerPlaybackRequest? queuedNext(ServerPlaybackRequest anchor) {
    final queue = handler.musicQueue;
    if (queue.index < 0 ||
        queue.index + 1 >= queue.entries.length ||
        queue.entries[queue.index].request.identity != anchor.identity) {
      return null;
    }
    final next = queue.entries[queue.index + 1].request;
    return next is ServerPlaybackRequest &&
            next.serverOrigin == anchor.serverOrigin &&
            next.userId == anchor.userId
        ? next
        : null;
  }

  prefetchNext = () async {
    final anchor = currentRequest, lease = currentLease;
    if (disposed ||
        anchor == null ||
        lease == null ||
        !lease() ||
        !currentCacheReady ||
        cache.budget == 0 ||
        !cache.prefetch ||
        !MusicNetworkPolicy.prefetchAllowed) {
      return;
    }
    final next = queuedNext(anchor);
    if (next == null) return;
    final quality = StreamQuality.selected(next.serverOrigin);
    final key =
        cache.key(next.serverOrigin, next.userId, next.mediaId, quality);
    if (prefetchingKey == key) return;
    prefetchingKey = key;
    try {
      await load(
          next,
          () =>
              !disposed &&
              identical(currentRequest, anchor) &&
              lease() &&
              queuedNext(anchor)?.identity == next.identity &&
              StreamQuality.selected(next.serverOrigin) == quality,
          prefetch: true);
    } finally {
      if (prefetchingKey == key) prefetchingKey = null;
      // If the queue changed during a transfer, a cancellation may have left
      // the new next song waiting behind the old one.
      final replacement = currentRequest == anchor ? queuedNext(anchor) : null;
      if (!disposed &&
          replacement != null &&
          cache.key(
                  replacement.serverOrigin,
                  replacement.userId,
                  replacement.mediaId,
                  StreamQuality.selected(replacement.serverOrigin)) !=
              key) {
        scheduleMicrotask(
            () => unawaited(prefetchNext().catchError((Object _) {})));
      }
    }
  };
  final queueChanges = handler.queueChanges.listen((_) {
    unawaited(prefetchNext().catchError((Object _) {}));
  });
  ref.onDispose(() {
    disposed = true;
    unawaited(queueChanges.cancel());
  });

  return (request, current, {onPreparing}) =>
      load(request, current, onPreparing: onPreparing);
});
