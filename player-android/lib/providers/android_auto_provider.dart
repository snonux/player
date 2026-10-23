import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:audio_service/audio_service.dart';

import '../library_navigation.dart';
import '../services/android_auto_catalog.dart';
import '../services/music_queue.dart';
import '../services/playback_request.dart';
import 'api_client_provider.dart';
import 'audio_handler_provider.dart';
import 'auth_state_provider.dart';
import 'playback_cache_provider.dart';
import 'playback_session_provider.dart';
import 'progress_queue_provider.dart';
import 'settings_provider.dart';

/// MediaBrowser commands work without opening a Flutter playback route. They
/// enter the very same queue, ownership coordinator, loader and progress actor.
final androidAutoCatalogProvider = Provider<AndroidAutoCatalog>((ref) {
  final handler = ref.read(audioHandlerProvider);
  AutoAccount? account() {
    final auth = ref.read(authStateProvider).valueOrNull;
    final mutations = ref.read(credentialMutationQueueProvider);
    final origin = ref.read(playerBaseUrlProvider).origin;
    final user = auth?.user;
    if (auth?.isAuthenticated != true ||
        user == null ||
        !mutations.credentialsEnabled ||
        mutations.credentialOrigin != origin ||
        ref.read(settingsProvider).valueOrNull?.destination !=
            LibraryDestination.server) {
      return null;
    }
    final epoch = mutations.generation;
    final coordinator = ref.read(playbackSessionCoordinatorProvider);
    final source = coordinator.generation;
    final transport = handler.transportRevision;
    bool current() =>
        epoch == mutations.generation &&
        mutations.credentialsEnabled &&
        origin == ref.read(playerBaseUrlProvider).origin &&
        ref.read(authStateProvider).valueOrNull?.user?.id == user.id &&
        ref.read(authStateProvider).valueOrNull?.isAuthenticated == true &&
        ref.read(settingsProvider).valueOrNull?.destination ==
            LibraryDestination.server;
    return AutoAccount(
        ref.read(apiClientProvider), origin, user.id, epoch, current,
        playbackCurrent: () =>
            current() &&
            source == coordinator.generation &&
            transport == handler.transportRevision);
  }

  MusicQueueState visibleQueue() {
    final state = handler.musicQueue;
    final a = account();
    final origin = ref.read(playerBaseUrlProvider).origin;
    final user = ref.read(authStateProvider).valueOrNull?.user?.id;
    if (a != null &&
        state.entries.every((e) =>
            e.request is ServerPlaybackRequest &&
            (e.request as ServerPlaybackRequest).serverOrigin == origin &&
            (e.request as ServerPlaybackRequest).userId == user)) {
      return state;
    }
    return const MusicQueueState(
        entries: [],
        index: -1,
        label: '',
        active: false,
        busy: false,
        canUndo: false,
        status: '');
  }

  final catalog = AndroidAutoCatalog(
      account: account,
      queue: visibleQueue,
      playOccurrence: (occurrence) async {
        final state = visibleQueue();
        final index =
            state.entries.indexWhere((entry) => entry.id == occurrence);
        if (index < 0) throw StateError('Queue changed. Browse again.');
        await handler.jumpQueue(index);
      },
      playSongs: (a, songs, index, label) async {
        if (!a.current()) return;
        final origin = ref.read(playerBaseUrlProvider).origin;
        final user = ref.read(authStateProvider).valueOrNull!.user!;
        final progress = ref.read(progressQueueProvider);
        final load = ref.read(cachedAudioLoaderProvider);
        final coordinator = ref.read(playbackSessionCoordinatorProvider);
        final source = coordinator.generation;
        bool current() => a.current();
        final entries = songs
            .map((song) => ServerPlaybackRequest(
                mediaId: song.id,
                serverOrigin: origin,
                userId: user.id,
                sourceUri: Uri.parse(a.api.playbackUrl(song)),
                title: song.displayTitle,
                artist: song.music?.artist ?? '',
                album: song.music?.album ?? '',
                music: song.music,
                sourceName: song.relPath,
                artworkUri: song.thumbnailPath.isNotEmpty
                    ? Uri.parse(a.api.thumbnailUrl(song.id))
                    : null,
                startPosition: song.duration <= 1200 ? 0 : null,
                readPosition: () => a.api.getMediaProgress(song.id),
                savePosition: (seconds) => progress.enqueue(song.id, seconds),
                markFinished: () => progress.enqueueFinished(song.id)))
            .toList();
        if (!current() ||
            source != coordinator.generation ||
            !a.playbackCurrent()) {
          return;
        }
        final added = await handler.insertQueue(
            entries: entries,
            action: QueueInsertion.append,
            replace: true,
            sourceLabel: label,
            claim: (request) async {
              if (!current()) return null;
              return coordinator.claim(
                  kind: request.kind,
                  identity: request.identity,
                  sourceUri: request.sourceUri.toString(),
                  stop: handler.stop);
            },
            load: (request, wanted) =>
                load(request, () => current() && wanted()));
        if (!current() ||
            source != coordinator.generation ||
            !a.playbackCurrent()) {
          return;
        }
        if (added) await handler.jumpQueue(index);
        if (current() && (!added || handler.musicState?.error != null)) {
          throw StateError('Could not play music. Check your phone and retry.');
        }
      });
  // The handler gets these hooks at the composition root after startup. No
  // credentials or account snapshots survive a callback invocation here.
  void refresh({bool queueOnly = false}) {
    final a = account();
    final state = visibleQueue();
    final rows = a == null ? <MediaItem>[] : catalog.queueItems(a);
    final start = (state.index - 20).clamp(0, rows.length);
    handler.publishAutoQueue(rows.skip(start).take(100).toList(), state.label);
    handler.refreshAutoChildren(queueOnly: queueOnly);
  }

  Future<void> initializeAccount() async {
    final a = account();
    final user = ref.read(authStateProvider).valueOrNull?.user;
    if (a == null || user == null) return;
    await initializeServerProgressQueueWith(ref.read, user);
    if (a.current()) refresh();
  }

  ref.listen(authStateProvider, (_, __) {
    refresh();
    unawaited(initializeAccount());
  });
  ref.listen(playerBaseUrlProvider, (_, __) => refresh());
  ref.listen(settingsProvider, (_, __) => refresh());
  final changes = handler.queueChanges.listen((_) => refresh(queueOnly: true));
  ref.onDispose(() => unawaited(changes.cancel()));
  refresh();
  unawaited(initializeAccount());
  return catalog;
});
