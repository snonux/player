import 'dart:async';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:just_audio/just_audio.dart';
import 'package:shared_preferences/shared_preferences.dart';
import '../services/music_action_intent.dart';
import '../services/music_automation.dart';
import '../services/music_automation_queue.dart';
import '../models/offline_media.dart';
import '../services/playback_request.dart';
import 'api_client_provider.dart';
import 'audio_handler_provider.dart';
import 'auth_state_provider.dart';
import 'offline_downloads_provider.dart';
import 'playback_cache_provider.dart';
import 'playback_session_provider.dart';
import 'progress_queue_provider.dart';
import 'settings_provider.dart';

const musicAutomationChannel =
    MethodChannel('zone.foo.player_android/music_automation');

/// Composition adapter only: existing queue/coordinator/loaders retain playback.
final musicAutomationProvider = Provider<void>((ref) {
  final handler = ref.read(audioHandlerProvider);
  final coordinator = ref.read(playbackSessionCoordinatorProvider);
  final cancellations = <String, void Function()>{};
  bool busy = false;

  Future<Map<String, dynamic>> run(Map<dynamic, dynamic> args) async {
    final action = args['action'], scope = args['scope'];
    final invocation = args['invocation'];
    if (busy ||
        invocation is! String ||
        !{'start', 'shuffle', 'offline', 'online'}.contains(action)) {
      throw StateError('Music automation is busy or invalid. Retry.');
    }
    final auth = ref.read(authStateProvider).valueOrNull;
    final origin = ref.read(playerBaseUrlProvider).origin;
    final user = auth?.user;
    final mutations = ref.read(credentialMutationQueueProvider);
    final epoch = mutations.generation;
    bool accountCurrent() =>
        user != null &&
        auth?.isAuthenticated == true &&
        mutations.credentialsEnabled &&
        mutations.generation == epoch &&
        mutations.credentialOrigin == origin &&
        ref.read(playerBaseUrlProvider).origin == origin &&
        ref.read(authStateProvider).valueOrNull?.isAuthenticated == true &&
        ref.read(authStateProvider).valueOrNull?.user?.id == user.id &&
        ref.read(settingsProvider).valueOrNull?.destination ==
            LibraryDestination.server;
    if (!musicAutomationAccountAllowed(
            origin: origin,
            userId: user?.id,
            configuredScope: scope is String ? scope : null,
            authenticated: auth?.isAuthenticated == true,
            credentialsEnabled: mutations.credentialsEnabled,
            credentialOrigin: mutations.credentialOrigin,
            serverSelected:
                ref.read(settingsProvider).valueOrNull?.destination ==
                    LibraryDestination.server) ||
        user == null ||
        !accountCurrent() ||
        scope != musicAutomationScope(origin, user.id)) {
      throw StateError(
          'Open Player and unlock the account used to save this music action.');
    }
    busy = true;
    bool committed = false;
    final cancellation = MusicAutomationCancellation(coordinator);
    final generation = coordinator.generation;
    final transport = handler.transportRevision;
    final intent = beginMusicActionIntent();
    bool current() =>
        accountCurrent() &&
        !cancellation.cancelled &&
        (committed || isMusicActionIntent(intent));
    bool preparing() =>
        current() &&
        generation == coordinator.generation &&
        transport == handler.transportRevision;
    cancellations[invocation] = () {
      unawaited(cancellation.cancel());
    };
    try {
      final prefs = await SharedPreferences.getInstance();
      if (!preparing()) throw StateError('Music action superseded. Retry.');
      final key = 'music-automation-offline-$scope';
      if (action == 'offline' || action == 'online') {
        if (!await prefs.setBool(key, action == 'offline')) {
          throw StateError(
              'Could not save automation offline behavior. Retry.');
        }
        if (!current()) throw StateError('Music action superseded. Retry.');
        return {
          'ok': true,
          'message': action == 'offline'
              ? 'Music automation now uses offline copies only.'
              : 'Music automation now allows online music with your normal network settings.'
        };
      }
      final queue = handler.musicQueue;
      if (queue.busy ||
          queue.entries.isEmpty ||
          queue.entries.any((entry) =>
              !automationMusicOwned(entry.request, origin, user.id))) {
        throw StateError('Open Player and queue music for this account first.');
      }
      final active = coordinator.activeIdentity;
      if (active != null &&
          (!queue.active ||
              !queue.entries.any((e) => e.request.identity == active))) {
        throw StateError(
            'Another source is active. Open Player and choose the music queue first.');
      }
      final offline = prefs.getBool(key) ?? false;
      var requests = queue.entries.map((e) => e.request).toList();
      final occurrences = queue.entries.map((e) => e.id).toList();
      bool sameQueue() {
        final now = handler.musicQueue.entries;
        return now.length == occurrences.length &&
            List.generate(now.length, (i) => now[i].id == occurrences[i])
                .every((same) => same);
      }

      final cache = ref.read(playbackCacheProvider);
      final downloads = ref.read(offlineDownloadRepositoryProvider);
      final progress = ref.read(progressQueueProvider);
      Map<String, int>? cacheMediaIds;
      List<OfflineMedia>? savedCopies;
      final classifications = <int, bool>{};
      Future<bool> catalogMusic(int id) async {
        if (id <= 0) {
          throw StateError(
              'Saved music metadata is missing. Choose music in Player again.');
        }
        final previous = classifications[id];
        if (previous != null) return previous;
        final eligible =
            (await ref.read(apiClientProvider).musicAlbums(mediaId: id))
                .isNotEmpty;
        if (!preparing() || !sameQueue()) {
          throw StateError('Music action superseded. Retry.');
        }
        savedCopies ??= [
          ...await downloads.list(),
          ...await cache.offlineLibrary()
        ];
        for (final copy in savedCopies!.where((s) =>
            s.origin == origin && s.userId == user.id && s.mediaId == id)) {
          if (!preparing() || !sameQueue()) {
            throw StateError('Music action superseded. Retry.');
          }
          if (copy.key.startsWith('cache:')) {
            await cache.setAutomationMusicEligibility(
                copy.key.substring(6), origin, user.id, eligible);
          } else {
            await downloads.setAutomationMusicEligibility(
                copy.key, origin, user.id, eligible);
          }
        }
        classifications[id] = eligible;
        return eligible;
      }

      Future<OfflinePlaybackRequest> localCopy(PlaybackRequest request,
          {bool protect = false}) async {
        final download = request as OfflinePlaybackRequest;
        if (!download.downloadKey.startsWith('cache:')) {
          return downloads.playbackRequest(download.downloadKey,
              startListeningSession: false);
        }
        final local = await cache.offlineRequest(
            download.downloadKey.substring(6), origin, user.id,
            protect: protect);
        cacheMediaIds ??= {
          for (final row in await cache.offlineLibrary())
            if (row.origin == origin && row.userId == user.id)
              row.key: row.mediaId
        };
        final mediaId = cacheMediaIds![download.downloadKey];
        if (mediaId == null || mediaId <= 0) {
          throw StateError(
              'Cached song metadata is missing. Download it again.');
        }
        // Cache copies lack progress callbacks; use the same durable account
        // progress actor as streaming, without making an offline network read.
        return OfflinePlaybackRequest(
            downloadKey: local.downloadKey,
            automationMusicEligible: local.automationMusicEligible,
            serverOrigin: origin,
            userId: user.id,
            sourceUri: local.sourceUri,
            title: local.title,
            sourceName: local.sourceName,
            music: local.music,
            artist: local.artist,
            album: local.album,
            savePosition: (seconds) => progress.enqueue(mediaId, seconds),
            markFinished: () => progress.enqueueFinished(mediaId));
      }

      if (offline) {
        final saved = [
          ...await downloads.list(),
          ...await cache.offlineLibrary()
        ];
        requests = await resolveAutomationMusic(requests, (request) async {
          if (request is OfflinePlaybackRequest) {
            return localCopy(request);
          }
          final id = (request as ServerPlaybackRequest).mediaId;
          final matches = saved.where((s) =>
              s.origin == origin &&
              s.userId == user.id &&
              s.mediaId == id &&
              s.type == 'audio');
          if (matches.isEmpty) {
            throw StateError(
                'Download every queued song before using offline-only automation.');
          }
          final song = matches.first;
          if (!song.key.startsWith('cache:')) {
            return downloads.playbackRequest(song.key,
                startListeningSession: false);
          }
          return localCopy(await cache.offlineRequest(
              song.key.substring(6), origin, user.id));
        }, () => preparing() && sameQueue());
      } else {
        // Upgrade only account-scoped metadata for legacy saved copies. No
        // audio is downloaded, and no listening occurrence is created here.
        requests = await resolveAutomationMusic(requests, (request) async {
          if (request is! OfflinePlaybackRequest) return request;
          final cached = request.downloadKey.startsWith('cache:');
          final saved = cached
              ? (await cache.offlineLibrary())
                  .where((s) =>
                      s.key == request.downloadKey &&
                      s.origin == origin &&
                      s.userId == user.id)
                  .firstOrNull
              : await downloads.validatePlaybackCopy(request.downloadKey);
          if (saved == null ||
              saved.origin != origin ||
              saved.userId != user.id) {
            throw StateError('Offline copy account changed.');
          }
          await catalogMusic(saved.mediaId);
          return localCopy(request);
        }, () => preparing() && sameQueue());
      }
      await validateAutomationQueueMusic(requests, origin, user.id,
          catalogMusic, () => preparing() && sameQueue());
      if (!preparing() || !sameQueue()) {
        throw StateError('Music action superseded. Retry.');
      }
      final load = ref.read(cachedAudioLoaderProvider);
      int reboundTransport = transport;
      if (!queue.active ||
          (offline &&
              queue.entries.any((e) => e.request is ServerPlaybackRequest))) {
        reboundTransport = await restoreAutomationQueueSources(
            handler, requests,
            current: () =>
                current() &&
                coordinator.generation == generation &&
                sameQueue(),
            claim: (request) async {
              if (!current()) return null;
              if (!committed &&
                  (coordinator.generation != generation ||
                      handler.transportRevision != reboundTransport)) {
                return null;
              }
              final acquired = await coordinator.claim(
                  kind: request.kind,
                  identity: request.identity,
                  sourceUri: request.sourceUri.toString(),
                  stop: handler.stop);
              final accepted = await cancellation.acquired(acquired);
              if (!accepted || !current()) {
                if (acquired?.isCurrent == true) {
                  await coordinator.stopCurrent();
                }
                return null;
              }
              return acquired;
            },
            load: (request, wanted) async {
              bool valid() => current() && wanted();
              if (!valid()) return null;
              if (request is OfflinePlaybackRequest) {
                if (!request.downloadKey.startsWith('cache:')) {
                  await downloads.validatePlaybackCopy(request.downloadKey);
                  if (!valid()) return null;
                  await request.beginListening?.call();
                  return valid() ? AudioSource.uri(request.sourceUri) : null;
                }
                final local = await localCopy(request, protect: true);
                return valid() ? AudioSource.uri(local.sourceUri) : null;
              }
              return load(request, valid);
            });
      }
      if (!current() ||
          coordinator.generation != generation ||
          handler.transportRevision != reboundTransport) {
        throw StateError('Music action superseded. Retry.');
      }
      bool sourceCurrent() => cancellation.lease == null
          ? coordinator.generation == generation
          : cancellation.lease!.isCurrent;
      await startAutomationQueue(handler,
          shuffle: action == 'shuffle',
          current: () => current() && sourceCurrent(),
          onResume: (revision) {
            final resumeGeneration = coordinator.generation;
            cancellation.cancelResume = () async {
              if (coordinator.generation == resumeGeneration &&
                  handler.transportRevision == revision) {
                await handler.pause();
              }
            };
          });
      // The native play Future lasts until pause/end. Confirm decoder motion
      // rather than treating queue insertion as playback success.
      final startedAt = handler.player.position;
      final startedTransport = handler.transportRevision;
      if (current() && sourceCurrent() && handler.musicState?.error == null) {
        try {
          await handler.player.positionStream
              .firstWhere((position) =>
                  !current() ||
                  !sourceCurrent() ||
                  (handler.player.playing &&
                      handler.player.processingState == ProcessingState.ready &&
                      position > startedAt + const Duration(milliseconds: 10)))
              .timeout(const Duration(milliseconds: 1500));
        } catch (_) {
          if (current() && startedTransport == handler.transportRevision) {
            await cancellation.cancel();
          }
          rethrow;
        }
      }
      if (!current() ||
          handler.musicState?.error != null ||
          !handler.player.playing ||
          startedTransport != handler.transportRevision ||
          handler.player.processingState != ProcessingState.ready ||
          !sourceCurrent()) {
        throw StateError(
            'Music did not start. Open Player, check audio and retry.');
      }
      committed = true;
      return {
        'ok': true,
        'message': action == 'shuffle'
            ? 'Shuffled queued music is playing.'
            : 'Queued music is playing.'
      };
    } finally {
      cancellations.remove(invocation);
      busy = false;
    }
  }

  musicAutomationChannel.setMethodCallHandler((call) async {
    if (call.method == 'cancel') {
      cancellations[call.arguments]?.call();
      return null;
    }
    if (call.method == 'configuration') {
      final auth = ref.read(authStateProvider).valueOrNull;
      final origin = ref.read(playerBaseUrlProvider).origin;
      final mutations = ref.read(credentialMutationQueueProvider);
      if (auth?.isAuthenticated != true ||
          auth?.user == null ||
          !mutations.credentialsEnabled ||
          mutations.credentialOrigin != origin ||
          ref.read(settingsProvider).valueOrNull?.destination !=
              LibraryDestination.server) {
        return {
          'message':
              'Open Player and unlock the intended server account, then try again.'
        };
      }
      return {
        'scope': musicAutomationScope(origin, auth!.user!.id),
        'message': '${auth.user!.username} · $origin'
      };
    }
    if (call.method != 'run') throw MissingPluginException();
    try {
      if (call.arguments is! Map) {
        throw StateError('Invalid music action. Save it again.');
      }
      return await run(call.arguments as Map);
    } catch (error) {
      return {
        'ok': false,
        'message': error is StateError
            ? error.message.toString()
            : 'Music automation failed. Check downloads, connection and audio in Player.'
      };
    }
  });
  ref.onDispose(() {
    for (final cancel in cancellations.values.toList()) {
      cancel();
    }
    musicAutomationChannel.setMethodCallHandler(null);
  });
});
