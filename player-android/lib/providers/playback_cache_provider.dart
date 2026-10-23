import '../services/playback_cache_access.dart';
import '../api/player_api_client.dart';
import 'dart:async';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:just_audio/just_audio.dart';
import '../api/dio_client.dart';
import '../services/playback_cache.dart';
import '../services/playback_request.dart';
import '../services/playback_source_resolver.dart';
import '../services/stream_quality.dart';
import 'api_client_provider.dart';
import 'audio_handler_provider.dart';
import 'auth_state_provider.dart';
import 'playback_preparer_provider.dart';
import 'server_endpoint_provider.dart';

final playbackCacheProvider = ChangeNotifierProvider<PlaybackCache>((ref) {
  final cache = PlaybackCache();
  ref.listen(authStateProvider, (_, __) => cache.release());
  ref.listen(playerBaseUrlProvider, (_, __) => cache.release());
  return cache;
});

typedef CachedAudioLoader = Future<AudioSource?> Function(
    PlaybackRequest, bool Function());

/// Captures account dependencies for queue callbacks that outlive their screen.
final cachedAudioLoaderProvider = Provider<CachedAudioLoader>((ref) {
  final cache = ref.read(playbackCacheProvider);
  final client = ref.watch(apiClientProvider);
  final router = ref.watch(serverEndpointRouterProvider);
  final storage = ref.read(tokenStorageProvider),
      jar = ref.read(cookieJarProvider);
  final mutations = ref.read(credentialMutationQueueProvider);
  final generation = mutations.generation;
  final preparer = ref.read(playbackPreparerProvider);
  final handler = ref.read(audioHandlerProvider);
  final subscription = handler.player.sequenceStateStream.listen((state) {
    final source = state?.currentSource;
    cache.playingUri(source is UriAudioSource ? source.uri : null);
  });
  ref.onDispose(() => unawaited(subscription.cancel()));
  Future<AudioSource?> load(PlaybackRequest request, bool Function() current,
      {bool prefetch = false}) async {
    if (request is! ServerPlaybackRequest) {
      return AudioSource.uri(request.sourceUri);
    }
    bool wanted() =>
        current() &&
        generation == mutations.generation &&
        mutations.credentialsEnabled &&
        mutations.credentialOrigin == request.serverOrigin;
    if (!wanted()) return null;
    var cacheReady = request.userId > 0;
    try {
      if (cacheReady) await cache.initialize();
    } catch (_) {
      cacheReady = false;
    }
    final key = cache.key(request.serverOrigin, request.userId, request.mediaId,
        StreamQuality.selected(request.serverOrigin));
    if (!prefetch) cache.cancel();
    Uri? saved;
    try {
      if (cacheReady) saved = await cache.lookup(key, protect: !prefetch);
    } catch (_) {
      cacheReady = false;
    }
    Future<void> prefetchNext() async {
      if (!wanted() || prefetch || !cacheReady || !cache.prefetch) return;
      final queue = handler.musicQueue;
      if (queue.index + 1 < queue.entries.length) {
        await load(queue.entries[queue.index + 1].request, wanted,
            prefetch: true);
      }
    }

    Uri canonical, transport;
    Map<String, String> headers;
    try {
      canonical = await currentServerAudioUri(request, client);
      if (!wanted()) return null;
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
        final ready = await preparer
            .prepare(canonical,
                probe: client.probePlayback, stillWanted: wanted)
            .ready;
        if (!ready) throw StateError('Playback preparation stopped.');
      }
    } catch (error) {
      if (cacheAccessDenied(error)) await cache.invalidate(key);
      final network = cacheTransportFailure(error);
      if (network && saved != null && wanted()) {
        unawaited(prefetchNext().catchError((Object _) {}));
        return AudioSource.uri(saved);
      }
      rethrow;
    }
    if (!wanted()) return null;
    Future<void> fill() => cache.download(
        key: key,
        uri: transport,
        headers: headers,
        title: request.title,
        origin: request.serverOrigin,
        userId: request.userId,
        authorized: wanted);
    if (cacheReady && saved == null && cache.completeBuffer && !prefetch) {
      try {
        await fill();
        saved = await cache.lookup(key);
      } catch (_) {
        cacheReady = false;
      }
    }
    if (cacheReady && saved == null) {
      unawaited(fill().then((_) => prefetchNext()).catchError((Object _) {}));
    } else {
      unawaited(prefetchNext().catchError((Object _) {}));
    }
    return AudioSource.uri(saved ?? transport,
        headers: saved != null || headers.isEmpty ? null : headers);
  }

  return (request, current) => load(request, current);
});
