import 'dart:async';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';
import '../services/podcast_repository.dart';
import '../services/podcast_retention.dart';
import '../services/podcast_sync_store.dart';
import '../services/podcast_sync.dart';
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

final podcastRetentionProvider = Provider<PodcastRetention>((ref) =>
    PodcastRetention(PodcastSyncStore(ref.watch(podcastRepositoryProvider)),
        ref.watch(podcastDownloadsProvider)));

Future<void> retainSelectedPodcasts(Ref ref, String scope) async {
  var alive = true;
  ref.onDispose(() => alive = false);
  final credentials = ref.read(credentialMutationQueueProvider);
  final generation = credentials.generation;
  final retention = ref.read(podcastRetentionProvider);
  bool current() =>
      alive &&
      credentials.generation == generation &&
      ref.read(podcastScopeProvider).valueOrNull == scope;
  await retention.sweep(scope, current: current);
}

/// Device browsing chooses a cached library, never a server credential.
final podcastDeviceScopeSelectionProvider =
    StateProvider<String?>((ref) => null);

final podcastScopeProvider = FutureProvider<String>((ref) async {
  final selection = ref.watch(podcastDeviceScopeSelectionProvider);
  final settings = ref.watch(settingsProvider).valueOrNull;
  final auth = ref.watch(authStateProvider).valueOrNull;
  if (settings == null || auth == null) return 'local';
  final prefs = await SharedPreferences.getInstance();
  final origin = settings.serverBaseUrl == null
      ? null
      : Uri.parse(settings.serverBaseUrl!).origin;
  if (settings.destination == LibraryDestination.server && origin != null) {
    final userId = auth.user?.id;
    if (auth.isAuthenticated && userId != null) {
      final scope = podcastAccountScope(origin, userId);
      await prefs.setInt('podcast_account:$origin', userId);
      await prefs.setString('podcast_device_scope', scope);
      await prefs.setString('podcast_library_label:$scope',
          '${auth.user!.username} · ${Uri.parse(origin).host}');
      return scope;
    }
    final remembered = prefs.getInt('podcast_account:$origin');
    return remembered == null
        ? 'local'
        : podcastAccountScope(origin, remembered);
  }
  final cached =
      await ref.watch(podcastRepositoryProvider).cachedAccountScopes();
  final chosen = prefs.getString('podcast_device_scope') ?? selection;
  if (chosen == 'local' || cached.contains(chosen)) return chosen!;
  // Migrate existing installs which only remembered an identity per server.
  final remembered =
      origin == null ? null : prefs.getInt('podcast_account:$origin');
  final previous =
      remembered == null ? null : podcastAccountScope(origin!, remembered);
  if (cached.contains(previous)) return previous!;
  return cached.length == 1 ? cached.single : 'local';
});

final podcastDeviceLibrariesProvider =
    FutureProvider<Map<String, String>>((ref) async {
  await ref.watch(podcastSubscriptionsProvider.future);
  final cached =
      await ref.watch(podcastRepositoryProvider).cachedAccountScopes();
  final prefs = await SharedPreferences.getInstance();
  return {
    'local': 'Device-only podcasts',
    for (final scope in cached)
      scope: prefs.getString('podcast_library_label:$scope') ??
          scope.replaceFirst('account:', 'Saved podcasts · '),
  };
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
  final rows = (await ref.watch(podcastRepositoryProvider).browseEpisodes(scope,
          feed: query.$1, filter: query.$2, offset: query.$3, limit: 41))
      .map((row) => {...row, 'browse_scope': scope})
      .toList();
  // Cached rows already derive expiry from durable observation metadata.
  // Observing new completions and unlinking files must never delay browsing.
  // Maintenance does not invalidate this page or restart an observed clock.
  unawaited(retainSelectedPodcasts(ref, scope).catchError((Object _) {}));
  return rows;
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

/// Opening either podcast view refreshes account membership independently of
/// the durable incremental cursor. Cached browsing remains available meanwhile.
final podcastServerSubscriptionsProvider =
    FutureProvider.autoDispose<PodcastSyncResult?>((ref) async {
  var active = true;
  ref.onDispose(() => active = false);
  final scope = await ref.watch(podcastScopeProvider.future);
  if (!active || !scope.startsWith('account:')) return null;
  final account = captureSyncAccount(ref);
  if (account == null || account.podcast.scope != scope) return null;
  final lease = PodcastSyncAccount(
    scope: account.podcast.scope,
    current: () => active && account.podcast.current(),
    supported: account.podcast.supported,
    send: account.podcast.send,
  );
  final result =
      await ref.read(podcastSyncProvider).refreshSubscriptions(lease);
  if (active && lease.current()) ref.invalidate(podcastSubscriptionsProvider);
  return result;
});

/// Populate sync-only subscriptions while the overview keeps showing its cache.
/// Publisher artwork is public; authenticated fallback stays inside the API client.
final podcastOverviewMetadataProvider =
    FutureProvider.autoDispose<void>((ref) async {
  var active = true;
  ref.onDispose(() => active = false);
  final scopeFuture = ref.watch(podcastScopeProvider.future);
  final repository = ref.watch(podcastRepositoryProvider);
  final metadata = ref.watch(podcastServerMetadataProvider);
  final membership = ref.watch(podcastServerSubscriptionsProvider.future);
  final scope = await scopeFuture;
  if (!active) return;
  // Membership reconciliation may add feeds after the cached first frame.
  try {
    await membership;
  } catch (_) {
    // Membership errors do not prevent hydrating existing cached subscriptions.
  }
  if (!active) return;
  for (final key in await repository.missingSubscriptions(scope)) {
    if (!active) return;
    try {
      await repository.refreshSubscription(scope, key);
    } catch (_) {
      if (active && metadata != null && metadata.scope == scope) {
        try {
          await metadata.refresh(key);
        } catch (_) {
          // Optional metadata must not hide cached subscriptions or block use.
        }
      }
    }
    if (active) ref.invalidate(podcastSubscriptionsProvider);
  }
});
