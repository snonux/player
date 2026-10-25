import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/dio_player_api_client.dart';
import '../services/offline_playback_sync.dart';
import 'api_client_provider.dart';
import 'auth_state_provider.dart';
import 'offline_downloads_provider.dart';

final offlinePlaybackSyncProvider = Provider<OfflinePlaybackSync>((ref) {
  final repository = ref.watch(offlineDownloadRepositoryProvider);
  final sync = OfflinePlaybackSync(repository, () {
    final auth = ref.read(authStateProvider).valueOrNull;
    final mutations = ref.read(credentialMutationQueueProvider);
    if (auth?.isAuthenticated != true || !mutations.credentialsEnabled) {
      return null;
    }
    final origin = ref.read(playerBaseUrlProvider).origin;
    final userId = auth!.user!.id;
    final epoch = mutations.generation;
    final currentClient = ref.read(apiClientProvider);
    if (currentClient is! DioPlayerApiClient) return null;
    final client = DioPlayerApiClient(
        dio: currentClient.rawDio,
        credentialEpoch: () => epoch,
        quietUnauthorized: true);
    return OfflineSyncAccount(
        origin: origin,
        userId: userId,
        current: () =>
            mutations.credentialsEnabled &&
            mutations.generation == epoch &&
            mutations.credentialOrigin == origin &&
            ref.read(authStateProvider).valueOrNull?.user?.id == userId,
        send: client.syncPlayback);
  }, automatic: false);
  ref.onDispose(() => unawaited(sync.dispose()));
  return sync;
});
