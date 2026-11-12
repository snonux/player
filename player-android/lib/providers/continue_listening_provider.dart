import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../app_routes.dart';
import '../services/podcast_repository.dart';
import 'api_client_provider.dart';
import 'auth_state_provider.dart';
import 'local_library_provider.dart';
import 'offline_downloads_provider.dart';
import 'podcast_library_provider.dart';
import 'settings_provider.dart';

class ContinueListeningItem {
  const ContinueListeningItem(
      {required this.title,
      required this.source,
      required this.position,
      this.path,
      this.episode,
      this.scope,
      this.streams = false});
  final String title, source;
  final double position;
  final String? path, scope;
  final Map<String, Object?>? episode;

  /// A podcast episode without a downloaded copy streams from its publisher.
  final bool streams;
}

/// Most podcast episodes the shortcut lists; three show before "Show more".
const continueListeningPodcastLimit = 10;

/// Reads durable device progress without contacting servers or publishers.
/// Signed out, started podcasts stay reachable from the sign-in screen; the
/// server files a locked account kept offline stay hidden.
final continueListeningProvider = FutureProvider.autoDispose
    .family<List<ContinueListeningItem>, bool>((ref, deviceOnly) async {
  // Public device browsing must not initialize the server's auth/network graph.
  final auth = !deviceOnly && ref.exists(authStateProvider)
      ? ref.watch(authStateProvider).valueOrNull
      : null;
  final settings = ref.watch(settingsProvider).valueOrNull;
  final repository = ref.watch(podcastRepositoryProvider);
  final librarySelection = ref.watch(podcastDeviceScopeSelectionProvider);
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
  // Podcast rows resume the library the Podcasts screen shows. Using files
  // on this device keeps the account library last used online, so offline
  // and online playback continue from the same saved position. Signed out
  // (sign-in and server screens) the shortcut offers that same library next
  // to device-only podcasts, so started episodes stay one tap away without
  // signing in. Podcasts are device subscriptions: they download or stream
  // from the publisher, never from the server.
  final scopes = account
      ? [
          podcastAccountScope(
              ref.watch(playerBaseUrlProvider).origin, auth!.user!.id)
        ]
      : {
          if (settings != null)
            await devicePodcastScope(repository, settings,
                selection: librarySelection),
          if (settings?.destination != LibraryDestination.local) 'local',
        }.toList();
  final downloads = ref.watch(podcastDownloadsProvider);
  final started = <(String, Map<String, Object?>)>[];
  for (final scope in scopes) {
    for (final episode in await repository.browseEpisodes(scope,
        filter: 'started', limit: 200)) {
      started.add((scope, episode));
    }
  }
  // Most recently played first across libraries; one row per episode.
  started.sort((a, b) => (b.$2['position_ms'] as int? ?? 0)
      .compareTo(a.$2['position_ms'] as int? ?? 0));
  final seen = <(Object?, Object?)>{};
  var count = 0;
  for (final (scope, episode) in started) {
    if (!seen.add((episode['feed_key'], episode['episode_key']))) continue;
    final copy = await downloads.copy(
        episode['feed_key'] as String, episode['episode_key'] as String);
    final downloaded = copy?['state'] == 'available';
    // Offline, only a downloaded copy can play.
    if (!downloaded && auth?.isOffline == true) continue;
    items.add(ContinueListeningItem(
        title: episode['title'] as String,
        source: episode['podcast_title'] as String? ?? 'Podcast',
        position: (episode['position_seconds'] as num).toDouble(),
        episode: episode,
        scope: scope,
        streams: !downloaded));
    if (++count == continueListeningPodcastLimit) break;
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
