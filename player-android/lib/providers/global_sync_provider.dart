import 'dart:async';
import 'automatic_music_provider.dart';
import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../api/dio_player_api_client.dart';
import '../services/global_sync_coordinator.dart';
import '../services/offline_playback_sync.dart';
import '../services/progress_queue.dart';
import '../services/sync_account.dart';
import 'api_client_provider.dart';
import 'saved_queue_provider.dart';
import 'auth_state_provider.dart';
import 'offline_downloads_provider.dart';
import 'offline_playback_sync_provider.dart';
import 'progress_queue_provider.dart';

final globalSyncStatusStoreProvider =
    Provider<GlobalSyncStatusStore>((ref) => GlobalSyncStatusStore());

/// Capture the credential generation before any async operation; all
/// domains use that same lease and refuse responses after switching accounts.
GlobalSyncAccount? captureSyncAccount(Ref ref) {
  final auth = ref.read(authStateProvider).valueOrNull;
  final mutations = ref.read(credentialMutationQueueProvider);
  if (auth?.isAuthenticated != true || !mutations.credentialsEnabled) {
    return null;
  }
  final origin = ref.read(playerBaseUrlProvider).origin;
  final userId = auth!.user!.id;
  final generation = mutations.generation;
  final api = ref.read(apiClientProvider);
  if (api is! DioPlayerApiClient) return null;
  final client = DioPlayerApiClient(
      dio: api.rawDio,
      credentialEpoch: () => generation,
      quietUnauthorized: true);
  bool current() =>
      mutations.credentialsEnabled &&
      mutations.generation == generation &&
      mutations.credentialOrigin == origin &&
      ref.read(authStateProvider).valueOrNull?.user?.id == userId;
  final scope = ProgressScope(origin: origin, userId: userId);
  final downloads = ref.read(offlineDownloadRepositoryProvider);
  final queue = ref.read(progressQueueProvider);
  final offline = ref.read(offlinePlaybackSyncProvider);
  final savedQueueAccount = captureSavedQueueAccount(ref);
  final savedQueues = ref.read(savedQueueSyncProvider);
  final musicAccount = captureAutomaticMusicAccount(ref);
  final scheduler = ref.read(automaticMusicSchedulerProvider);
  return GlobalSyncAccount(
    syncMusic: musicAccount == null
        ? null
        : (explicit) => scheduler.sync(musicAccount, explicit: explicit),
    syncQueue: savedQueueAccount == null
        ? null
        : () => savedQueues.syncForGlobal(savedQueueAccount),
    scope: accountSyncScope(origin, userId),
    current: current,
    syncMedia: (explicit) => offline.syncAll(
        OfflineSyncAccount(
            origin: origin,
            userId: userId,
            current: current,
            send: client.syncPlayback),
        explicit: explicit),
    flushProgress: () async {
      if (!current()) throw StateError('Account changed');
      if (queue is ProgressQueue) {
        await queue.resume(scope);
        if (!current()) throw StateError('Account changed');
        await queue.flushForSync(scope);
      } else {
        throw StateError('Progress sync unavailable');
      }
    },
    pendingOther: () async {
      final items = await downloads.syncItems(origin, userId);
      var count = savedQueueAccount == null
          ? 0
          : await savedQueues.pending(savedQueueAccount);
      for (final item in items) {
        if (item['gone_ms'] != null) continue;
        final payload = await downloads.syncPayload(item['key'] as String);
        count += (payload['progress'] == null ? 0 : 1) +
            (payload['bookmarks'] as List).length +
            (payload['music_plays'] as List? ?? const []).length;
      }
      if (queue is ProgressQueue) {
        count += (await queue.pendingIds(scope)).length;
      }
      return count;
    },
  );
}

class _ForegroundSync extends WidgetsBindingObserver {
  _ForegroundSync(this.wake);
  final void Function() wake;
  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) wake();
  }
}

final globalSyncCoordinatorProvider =
    ChangeNotifierProvider<GlobalSyncCoordinator>((ref) {
  final sync = GlobalSyncCoordinator(
      ref.watch(globalSyncStatusStoreProvider), () => captureSyncAccount(ref));
  final musicScheduler = ref.watch(automaticMusicSchedulerProvider);
  musicScheduler.onWake = () => sync.syncNow(explicit: false);
  final downloads = ref.watch(offlineDownloadRepositoryProvider);
  final queue = ref.watch(progressQueueProvider);
  downloads.onSyncNeeded = sync.trigger;
  if (queue is ProgressQueue) queue.onSyncNeeded = sync.trigger;
  Future<void> wake() async {
    if (ref.read(authStateProvider).valueOrNull?.isOffline == true) {
      await ref.read(authStateProvider.notifier).reconnectSavedAccount();
    }
    sync.trigger();
  }

  final retry = Timer.periodic(const Duration(seconds: 15), (_) {
    if (ref.read(authStateProvider).valueOrNull?.isOffline == true) {
      unawaited(wake());
    }
  });
  final connectivity =
      Connectivity().onConnectivityChanged.listen((_) => unawaited(wake()));
  final lifecycle = _ForegroundSync(() => unawaited(wake()));
  WidgetsBinding.instance.addObserver(lifecycle);
  ref.listen(
      authStateProvider,
      (_, next) =>
          sync.trigger(signedIn: next.valueOrNull?.isAuthenticated == true));
  ref.onDispose(() {
    retry.cancel();
    musicScheduler.onWake = null;
    unawaited(connectivity.cancel());
    WidgetsBinding.instance.removeObserver(lifecycle);
    downloads.onSyncNeeded = null;
    if (queue is ProgressQueue) queue.onSyncNeeded = null;
  });
  sync.trigger();
  return sync;
});
