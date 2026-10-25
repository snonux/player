import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/global_sync_provider.dart';
import 'package:player_android/providers/podcast_library_provider.dart';
import 'package:player_android/services/podcast_repository.dart';
import 'package:player_android/services/podcast_sync_store.dart';
import 'package:player_android/services/podcast_downloads.dart';
import 'package:player_android/services/playback_session_coordinator.dart';

class BrowsingRepository extends PodcastRepository {
  bool completed = false, failSave = false;
  final queries = <(String?, String, int)>[];
  int count = 1;
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
              'enclosure_url': 'https://example/audio.mp3'
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
    fake.completed = completed!;
  }
}

class BrowsingDownloads extends PodcastDownloads {
  BrowsingDownloads(PodcastRepository repository)
      : super(repository, PlaybackSessionCoordinator());
  @override
  Future<Map<String, Object?>?> copy(String feed, String episode) async => null;
}

Widget browsingApp(Widget screen, BrowsingRepository repository,
        {BrowsingStore? store, Future<String> Function(Ref)? scope}) =>
    ProviderScope(overrides: [
      podcastRepositoryProvider.overrideWithValue(repository),
      podcastScopeProvider
          .overrideWith((ref) => scope?.call(ref) ?? Future.value('local')),
      podcastSyncStoreProvider
          .overrideWithValue(store ?? BrowsingStore(repository)),
      podcastDownloadsProvider
          .overrideWith((ref) => BrowsingDownloads(repository)),
      // Any legacy server browse/download/login dependency fails this test.
      apiClientProvider.overrideWith(
          (ref) => throw StateError('Server API must not be used')),
    ], child: MaterialApp(home: screen));
