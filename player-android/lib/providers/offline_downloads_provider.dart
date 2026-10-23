import 'dart:io';
import 'server_endpoint_provider.dart';
import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/dio_client.dart';
import '../models/media.dart';
import '../models/offline_media.dart';
import '../services/offline_download_repository.dart';
import '../services/offline_downloads.dart';
import 'api_client_provider.dart';
import 'auth_state_provider.dart';
import 'playback_preparer_provider.dart';
import 'playback_session_provider.dart';

final offlineDownloadRepositoryProvider =
    Provider<OfflineDownloadRepository>((ref) {
  final repository = OfflineDownloadRepository();
  ref.onDispose(() => unawaited(repository.close()));
  return repository;
});

final offlineDownloadsProvider =
    ChangeNotifierProvider<OfflineDownloads>((ref) {
  final downloads = OfflineDownloads(
      ref.watch(offlineDownloadRepositoryProvider),
      ref.watch(playbackSessionCoordinatorProvider));
  ref.listen(playerBaseUrlProvider, (_, __) => downloads.cancel());
  ref.listen(authStateProvider, (_, __) => downloads.cancel());
  return downloads;
});

final offlineMediaListProvider = FutureProvider<List<OfflineMedia>>((ref) {
  ref.watch(offlineDownloadsProvider
      .select((downloads) => downloads.libraryRevision));
  return ref.watch(offlineDownloadRepositoryProvider).list();
});

final offlineMediaProvider =
    FutureProvider.autoDispose.family<OfflineMedia?, String>((ref, key) {
  ref.watch(offlineDownloadsProvider
      .select((downloads) => downloads.libraryRevision));
  return ref.watch(offlineDownloadRepositoryProvider).get(key);
});

/// Captures credentials only for the selected account and never persists them.
Future<void> keepMediaOffline(WidgetRef ref, Media media) =>
    mediaOfflineSaver(ref)(media);

/// Capture dependencies once so a batch can outlive its initiating screen.
Future<void> Function(Media) mediaOfflineSaver(WidgetRef ref,
    {bool batchMember = false}) {
  final user = ref.read(authStateProvider).valueOrNull?.user;
  if (user == null) throw StateError('Sign in before downloading.');
  final origin = ref.read(playerBaseUrlProvider).origin;
  final mutations = ref.read(credentialMutationQueueProvider);
  final generation = mutations.generation;
  final client = ref.read(apiClientProvider);
  final downloads = ref.read(offlineDownloadsProvider);
  final preparer = ref.read(playbackPreparerProvider);
  final storage = ref.read(tokenStorageProvider);
  final cookieJar = ref.read(cookieJarProvider);
  final endpointRouter = ref.read(serverEndpointRouterProvider);
  return (media) async {
    final uri = Uri.parse(client.offlineDownloadUrl(media));
    final headers = await accountRequestHeaders(
      uri: uri,
      baseUrl: Uri.parse(origin),
      storage: storage,
      cookieJar: cookieJar,
      mutations: mutations,
    );
    final transportUri = await endpointRouter.resolve(uri);
    try {
      await downloads.download(
        media: media,
        batchMember: batchMember,
        origin: origin,
        userId: user.id,
        uri: transportUri,
        trustedTransportOrigin: transportUri.origin,
        headers: headers,
        authorized: () =>
            (!batchMember || downloads.batch?.cancelled != true) &&
            mutations.credentialsEnabled &&
            mutations.generation == generation &&
            mutations.credentialOrigin == origin,
        prepare: !media.transcoded
            ? null
            : (job) async {
                final preparation = preparer.prepare(uri,
                    probe: client.probePlayback,
                    stillWanted: () =>
                        !job.cancelled && mutations.generation == generation);
                job.abort = preparation.cancel;
                if (!await preparation.ready) throw OfflineDownloadCancelled();
              },
      );
    } on SocketException {
      endpointRouter.failed(transportUri);
      rethrow;
    } on TimeoutException {
      endpointRouter.failed(transportUri);
      rethrow;
    }
  };
}
