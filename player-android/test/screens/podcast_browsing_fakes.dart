import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:player_android/podcast_routes.dart';
import 'package:player_android/services/podcast_sync.dart';
import 'package:player_android/services/podcast_server_metadata.dart';
import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/global_sync_provider.dart';
import 'package:player_android/providers/podcast_library_provider.dart';
import 'package:player_android/services/podcast_repository.dart';
import 'package:player_android/services/podcast_sync_store.dart';
import 'package:player_android/services/podcast_downloads.dart';
import 'package:player_android/services/podcast_retention.dart';
import 'package:player_android/services/playback_session_coordinator.dart';

class BrowsingRepository extends PodcastRepository {
  @override
  Future<List<String>> cachedAccountScopes() async => [];
  bool completed = false, failSave = false, deleted = false;
  List<String> missingFeeds = [];
  final refreshed = <String>[];
  final failedFeeds = <String>{};
  @override
  Future<List<String>> missingSubscriptions(String scope) async =>
      missingFeeds.where((key) => !refreshed.contains(key)).toList();
  @override
  Future<List<String>> uncachedSubscriptions(String scope) async =>
      missingFeeds;
  @override
  Future<void> refreshSubscription(String scope, String key) async {
    refreshed.add(key);
    if (failedFeeds.contains(key)) throw StateError('offline');
  }

  final queries = <(String?, String, int)>[];
  int count = 1;
  Map<String, Object?> metadata = {};
  Completer<void>? gate;
  @override
  Future<List<Map<String, Object?>>> subscriptions(String scope) async => [
        {
          'feed_key': 'feed:example/feed',
          'title': 'My podcast',
          'fetch_url': 'https://example/feed'
        }
      ];
  @override
  Future<List<Map<String, Object?>>> browseEpisodes(String scope,
      {String? feed,
      String filter = 'uncompleted',
      int offset = 0,
      int limit = 40}) async {
    if (scope != 'local' && gate != null) await gate!.future;
    queries.add((feed, filter, offset));
    if (deleted) return [];
    if (filter == 'completed' && !completed ||
        filter == 'uncompleted' && completed) {
      return [];
    }
    return List.generate(
        count,
        (i) => <String, Object?>{
              'feed_key': 'feed:example/feed',
              'episode_key': 'guid:$i',
              'scope_feed_key': 'feed:example/feed',
              'scope_episode_key': 'guid:$i',
              'title': 'Episode $i',
              'podcast_title': 'My podcast',
              'description': 'Publisher description',
              'duration_seconds': 100,
              'position_seconds': 30.0,
              'completed': completed ? 1 : 0,
              'enclosure_url': 'https://example/audio.mp3',
              ...metadata
            }).skip(offset).take(limit).toList();
  }
}

class BrowsingStore extends PodcastSyncStore {
  BrowsingStore(this.fake) : super(fake);
  final BrowsingRepository fake;
  final saves = <Map<String, Object?>>[];
  @override
  Future<void> saveEpisode(String scope, String feed, String episode,
      {double? seconds,
      bool? completed,
      bool? deleted,
      String source = 'manual',
      String enclosureUrl = '',
      int? observedMs}) async {
    if (fake.failSave) throw StateError('disk');
    saves.add({
      'scope': scope,
      'completed': completed,
      'seconds': seconds,
      'source': source
    });
    if (completed != null) fake.completed = completed;
    if (deleted != null) fake.deleted = deleted;
  }
}

class BrowsingDownloads extends PodcastDownloads {
  BrowsingDownloads(PodcastRepository repository)
      : super(repository, PlaybackSessionCoordinator());
  @override
  Future<void> removeEpisodeCopy(String feed, String episode,
      {bool Function()? current}) async {}
  @override
  Future<Map<String, Object?>?> copy(String feed, String episode) async => null;
}

/// Screen fixtures have no database/filesystem. Service and native retention
/// tests use the real policy; this fake can deliberately hold maintenance open.
class BrowsingRetention extends PodcastRetention {
  BrowsingRetention(BrowsingRepository repository)
      : super(BrowsingStore(repository), BrowsingDownloads(repository));
  Completer<void>? gate;
  final scopes = <String>[];
  final leases = <bool Function()>[];
  var finished = 0;
  @override
  Future<void> sweep(String scope,
      {required bool Function() current, int? nowMs}) async {
    scopes.add(scope);
    leases.add(current);
    await gate?.future;
    if (current()) finished++;
  }
}

Widget browsingApp(Widget screen, BrowsingRepository repository,
        {BrowsingStore? store,
        BrowsingRetention? retention,
        Future<String> Function(Ref)? scope,
        PodcastServerMetadata? serverMetadata,
        Future<PodcastSyncResult?>? serverSync}) =>
    ProviderScope(overrides: [
      if (serverSync != null)
        podcastServerSubscriptionsProvider.overrideWith((ref) => serverSync),
      podcastRepositoryProvider.overrideWithValue(repository),
      podcastRetentionProvider
          .overrideWithValue(retention ?? BrowsingRetention(repository)),
      podcastServerMetadataProvider.overrideWithValue(serverMetadata),
      podcastScopeProvider
          .overrideWith((ref) => scope?.call(ref) ?? Future.value('local')),
      podcastSyncStoreProvider
          .overrideWithValue(store ?? BrowsingStore(repository)),
      podcastDownloadsProvider
          .overrideWith((ref) => BrowsingDownloads(repository)),
      // Any legacy server browse/download/login dependency fails this test.
      apiClientProvider.overrideWith(
          (ref) => throw StateError('Server API must not be used')),
    ], child: podcastRoutingApp(screen));

Widget podcastRoutingApp(Widget screen) {
  final router = GoRouter(initialLocation: '/test', routes: [
    GoRoute(path: '/test', builder: (_, __) => screen),
    ...podcastRoutes(),
  ]);
  addTearDown(router.dispose);
  return MaterialApp.router(routerConfig: router);
}
