import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/dio_player_api_client.dart';
import '../services/personal_media_store.dart';
import '../services/playback_request.dart';
import 'api_client_provider.dart';
import 'local_library_provider.dart';
import 'offline_downloads_provider.dart';

PersonalMediaStore localPersonalStore(WidgetRef ref, int id) =>
    ref.read(localLibraryDatabaseProvider).personalStore(id);

PersonalMediaStore offlinePersonalStore(WidgetRef ref, String key) =>
    ref.read(offlineDownloadRepositoryProvider).personalStore(key);

PersonalMediaStore serverPersonalStore(WidgetRef ref, int id) {
  final origin = ref.read(playerBaseUrlProvider).origin;
  final mutations = ref.read(credentialMutationQueueProvider);
  final epoch = mutations.generation;
  final currentClient = ref.read(apiClientProvider);
  // The interceptor checks this captured epoch even if credentials change
  // while it is asynchronously loading cookies or a bearer token.
  final client = currentClient is DioPlayerApiClient
      ? DioPlayerApiClient(
          dio: currentClient.rawDio, credentialEpoch: () => epoch)
      : currentClient;
  return ServerPersonalMediaStore(client, id, 'server:$origin:$epoch:$id',
      isCurrent: () => mutations.generation == epoch);
}

PersonalMediaStore? playbackPersonalStore(
        WidgetRef ref, PlaybackRequest request) =>
    switch (request) {
      LocalPlaybackRequest() => localPersonalStore(ref, request.localMediaId),
      OfflinePlaybackRequest() =>
        offlinePersonalStore(ref, request.downloadKey),
      ServerPlaybackRequest() =>
        request.serverOrigin == ref.read(playerBaseUrlProvider).origin
            ? serverPersonalStore(ref, request.mediaId)
            : null,
      PublicSharePlaybackRequest() => null,
    };
