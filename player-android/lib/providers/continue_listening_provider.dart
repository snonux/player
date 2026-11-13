import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../app_routes.dart';
import 'api_client_provider.dart';
import 'auth_state_provider.dart';
import 'local_library_provider.dart';
import 'offline_downloads_provider.dart';
import 'settings_provider.dart';

class ContinueListeningItem {
  const ContinueListeningItem(
      {required this.title,
      required this.source,
      required this.position,
      required this.path});
  final String title, source;
  final double position;
  final String path;
}

/// Reads durable device progress without contacting servers. The server
/// files a locked account kept offline stay hidden.
final continueListeningProvider = FutureProvider.autoDispose
    .family<List<ContinueListeningItem>, bool>((ref, deviceOnly) async {
  // Public device browsing must not initialize the server's auth/network graph.
  final auth = !deviceOnly && ref.exists(authStateProvider)
      ? ref.watch(authStateProvider).valueOrNull
      : null;
  final settings = ref.watch(settingsProvider).valueOrNull;
  final account = !deviceOnly &&
      settings?.destination == LibraryDestination.server &&
      auth?.isAuthenticated == true &&
      auth?.user != null;
  final items = <ContinueListeningItem>[];
  if (auth?.isOffline != true) {
    final media = await ref.watch(localMediaRepositoryProvider).list();
    final progress = ref.watch(localProgressRepositoryProvider);
    for (final item in media) {
      final saved = await progress.get(item.id);
      if (saved == null || saved.finished || saved.positionSeconds <= 0) {
        continue;
      }
      items.add(ContinueListeningItem(
          title: item.title,
          source: 'On this device',
          position: saved.positionSeconds,
          path: AppRoutes.localPlayerPath(item.id)));
      if (items.length == 5) break;
    }
  }
  if (account) {
    ref.watch(offlineDownloadsProvider
        .select((downloads) => downloads.libraryRevision));
    final origin = ref.watch(playerBaseUrlProvider).origin;
    final repository = ref.watch(offlineDownloadRepositoryProvider);
    var count = 0;
    for (final item in await repository.list()) {
      if (item.origin != origin ||
          item.userId != auth!.user!.id ||
          item.finished ||
          item.positionSeconds <= 0 ||
          !{'audio', 'video'}.contains(item.type)) {
        continue;
      }
      try {
        await repository.validatePlaybackCopy(item.key);
        items.add(ContinueListeningItem(
            title: item.title,
            source: 'Downloaded file',
            position: item.positionSeconds,
            path: AppRoutes.offlinePlayerPath(item.key)));
        if (++count == 5) break;
      } catch (_) {/* Missing copies do not offer a broken resume action. */}
    }
  }
  return items;
});
