import 'dart:async';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';
import '../services/podcast_repository.dart';
import '../services/podcast_server_metadata.dart';
import '../api/dio_player_api_client.dart';
import '../services/podcast_downloads.dart';
import 'playback_session_provider.dart';
import 'auth_state_provider.dart';
import 'api_client_provider.dart';
import 'settings_provider.dart';
import 'global_sync_provider.dart';

final podcastRepositoryProvider = Provider<PodcastRepository>((ref) {
  final repository = PodcastRepository();
  ref.onDispose(() => unawaited(repository.close()));
  return repository;
});

final podcastDownloadsProvider =
    ChangeNotifierProvider<PodcastDownloads>((ref) {
  return PodcastDownloads(ref.watch(podcastRepositoryProvider),
      ref.watch(playbackSessionCoordinatorProvider),
      captureSyncAccount: () => captureSyncAccount(ref)?.podcast);
});

/// Remember only the last account identity, never credentials. Signed-out
/// edits remain in that account; browsing device files selects local scope.
final podcastScopeProvider = FutureProvider<String>((ref) async {
  final settings = ref.watch(settingsProvider).valueOrNull;
  final auth = ref.watch(authStateProvider).valueOrNull;
  if (settings?.destination != LibraryDestination.server) return 'local';
  final origin = ref.watch(playerBaseUrlProvider).origin;
  final prefs = await SharedPreferences.getInstance();
  final userId = auth?.user?.id;
  if (auth?.isAuthenticated == true && userId != null) {
    await prefs.setInt('podcast_account:$origin', userId);
    return podcastAccountScope(origin, userId);
  }
  final remembered = prefs.getInt('podcast_account:$origin');
  return remembered == null ? 'local' : podcastAccountScope(origin, remembered);
});
final podcastSubscriptionsProvider =
    FutureProvider<List<Map<String, Object?>>>((ref) async {
  final scope = await ref.watch(podcastScopeProvider.future);
  return (await ref.watch(podcastRepositoryProvider).subscriptions(scope))
      .map((row) => {...row, 'browse_scope': scope})
      .toList();
});

final podcastEpisodePageProvider =
    FutureProvider.family<List<Map<String, Object?>>, (String?, String, int)>(
        (ref, query) async {
  final scope = await ref.watch(podcastScopeProvider.future);
  // Sync and feed refresh invalidate subscriptions; refresh listening rows too.
  await ref.watch(podcastSubscriptionsProvider.future);
  return (await ref.watch(podcastRepositoryProvider).browseEpisodes(scope,
          feed: query.$1, filter: query.$2, offset: query.$3, limit: 41))
      .map((row) => {...row, 'browse_scope': scope})
      .toList();
});

/// A generation-bound client is used only after direct publisher refresh fails.
final podcastServerMetadataProvider = Provider<PodcastServerMetadata?>((ref) {
  final settings = ref.watch(settingsProvider).valueOrNull;
  final auth = ref.watch(authStateProvider).valueOrNull;
  final userId = auth?.user?.id;
  if (settings?.destination != LibraryDestination.server ||
      auth?.isAuthenticated != true ||
      userId == null) {
    return null;
  }
  final origin = ref.watch(playerBaseUrlProvider).origin;
  final mutations = ref.watch(credentialMutationQueueProvider);
  if (!mutations.credentialsEnabled || mutations.credentialOrigin != origin) {
    return null;
  }
  final generation = mutations.generation;
  final api = ref.watch(apiClientProvider);
  if (api is! DioPlayerApiClient) return null;
  final client = DioPlayerApiClient(
      dio: api.rawDio,
      credentialEpoch: () => generation,
      quietUnauthorized: true);
  var active = true;
  ref.onDispose(() => active = false);
  bool current() =>
      active &&
      mutations.credentialsEnabled &&
      mutations.generation == generation &&
      mutations.credentialOrigin == origin &&
      ref.read(authStateProvider).valueOrNull?.user?.id == userId;
  return PodcastServerMetadata(
      repository: ref.watch(podcastRepositoryProvider),
      scope: podcastAccountScope(origin, userId),
      current: current,
      fetchPage: (feed, offset, limit) => client
          .listSubscribedPodcastEpisodes(feed, offset: offset, limit: limit));
});
