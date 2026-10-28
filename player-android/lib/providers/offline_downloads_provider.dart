import 'dart:io';
import 'server_endpoint_provider.dart';
import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/dio_client.dart';
import '../models/media.dart';
import '../models/offline_media.dart';
import '../services/offline_download_repository.dart';
import '../services/offline_downloads.dart';
import '../services/download_quality.dart';
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

final offlineMediaListProvider =
    FutureProvider<List<OfflineMedia>>((ref) async {
  ref.watch(offlineDownloadsProvider
      .select((downloads) => downloads.libraryRevision));
  final offlineUserId = ref.watch(authStateProvider.select((state) =>
      state.valueOrNull?.isOffline == true
          ? state.valueOrNull?.user?.id
          : null));
  final origin = ref.watch(playerBaseUrlProvider).origin;
  final repository = ref.watch(offlineDownloadRepositoryProvider);
  final items = await repository.list();
  if (offlineUserId == null) return items;
  final matching = <OfflineMedia>[];
  for (final item in items) {
    if (item.origin != origin || item.userId != offlineUserId) continue;
    try {
      matching.add(await repository.validatePlaybackCopy(item.key));
    } catch (_) {/* Missing and incomplete copies cannot be played. */}
  }
  return matching;
});

final offlineMediaProvider =
    FutureProvider.autoDispose.family<OfflineMedia?, String>((ref, key) {
  ref.watch(offlineDownloadsProvider
      .select((downloads) => downloads.libraryRevision));
  final offlineUserId = ref.watch(authStateProvider.select((state) =>
      state.valueOrNull?.isOffline == true
          ? state.valueOrNull?.user?.id
          : null));
  final origin = ref.watch(playerBaseUrlProvider).origin;
  return ref.watch(offlineDownloadRepositoryProvider).get(key).then((item) =>
      offlineUserId != null &&
              (item?.origin != origin || item?.userId != offlineUserId)
          ? null
          : item);
});

/// Captures credentials only for the selected account and never persists them.
Future<void> keepMediaOffline(WidgetRef ref, Media media) =>
    mediaOfflineSaver(ref)(media);

/// Capture dependencies once so a batch can outlive its initiating screen.
Future<void> Function(Media) mediaOfflineSaver(WidgetRef ref,
    {bool batchMember = false, String? downloadProfile}) {
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
  final quality = downloadProfile == null
      ? DownloadQuality.selected(client.rawDio.options.baseUrl)
      : Future.value(downloadProfile);
  return (media) async {
    final selected = media.type == 'audio' ? await quality : '';
    final explicit = selected.isNotEmpty && selected != 'original';
    final sourceName = media.type == 'audio'
        ? DownloadQuality.sourceName(media.relPath, selected)
        : media.transcoded
            ? '${media.relPath}.mp4'
            : media.relPath;
    final uri = Uri.parse(media.type == 'audio'
        ? explicit
            ? '${client.compatUrl(media.id)}?profile=${Uri.encodeQueryComponent(selected)}'
            : client.downloadUrl(media.id)
        : client.offlineDownloadUrl(media));
    final headers = await accountRequestHeaders(
      uri: uri,
      baseUrl: Uri.parse(origin),
      storage: storage,
      cookieJar: cookieJar,
      mutations: mutations,
    );
    final transportUri = await endpointRouter.resolve(uri);
    bool? automationMusicEligible;
    if (media.type == 'audio') {
      try {
        automationMusicEligible =
            (await client.musicAlbums(mediaId: media.id)).isNotEmpty;
      } catch (_) {
        /* Manual downloads remain available without catalog support. */
      }
    }
    try {
      await downloads.download(
        media: media,
        automationMusicEligible: automationMusicEligible,
        downloadProfile: selected,
        expectedContentType: DownloadQuality.contentType(selected),
        sourceName: sourceName,
        originalBytes: media.type == 'audio' ? !explicit : !media.transcoded,
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
        prepare: !(media.type == 'audio' ? explicit : media.transcoded)
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
