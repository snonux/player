import 'dart:io';
import 'package:flutter_test/flutter_test.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';
import 'package:player_android/services/podcast_repository.dart';
import 'package:player_android/services/podcast_sync_store.dart';
import 'package:player_android/services/podcast_server_metadata.dart';
import 'podcast_repository_test.dart' show FixtureFetcher;

void main() {
  sqfliteFfiInit();
  databaseFactory = databaseFactoryFfi;
  late Directory directory;
  late PodcastRepository repository;
  late String feed;
  setUp(() async {
    directory = await Directory.systemTemp.createTemp('podcast-catalogue-');
    repository = PodcastRepository(
        databasePath: '${directory.path}/test.db', fetcher: FixtureFetcher());
    feed = await repository.subscribe('A', 'https://example.com/feed');
    await (await repository.database).update('podcast_feeds',
        {'last_error': 'Publisher unavailable', 'etag': 'keep-etag'});
  });
  tearDown(() async {
    await repository.close();
    await directory.delete(recursive: true);
  });
  Map<String, dynamic> row(String key, {String? enclosure}) => {
        'feed_key': feed,
        'episode_key': key,
        'feed_title': 'Fixture',
        'title': 'Server $key',
        'description': 'Server notes',
        'image_url': 'https://example.com/episode.jpg',
        'feed_image_url': 'https://example.com/feed.jpg',
        'published_at': '2026-10-28T12:00:00Z',
        'duration_seconds': 51.5,
        'episode_url':
            enclosure ?? 'https://example.com/${Uri.encodeComponent(key)}.mp3',
        'completed': false,
        'position_seconds': 999,
      };

  test('server enrichment repairs known metadata missing a publication date',
      () async {
    final db = await repository.database;
    await db.update('podcast_feeds', {'last_error': null});
    expect(await repository.uncachedSubscriptions('A'), [feed]);
    final old = (await repository.episodes(feed)).single;
    await repository.importServerEpisodes(
        'A', feed, [row('guid:a', enclosure: old['enclosure_url'] as String)],
        current: () => true);
    expect(await repository.uncachedSubscriptions('A'), isEmpty);
    expect((await repository.episodes(feed)).single['published_at'],
        DateTime.utc(2026, 10, 28, 12).millisecondsSinceEpoch);
  });

  test(
      'metadata import preserves GUID, downloaded copy, tombstone and all personal clocks',
      () async {
    final db = await repository.database;
    final old = (await repository.episodes(feed)).single;
    await PodcastSyncStore(repository).saveEpisode('A', feed, 'guid:a',
        seconds: 42, completed: true, deleted: true);
    await db.insert('podcast_downloads', {
      'id': 'copy',
      'feed_key': feed,
      'episode_key': 'guid:a',
      'state': 'available'
    });
    final states = await db.query('podcast_episode_state');
    final copies = await db.query('podcast_downloads');
    await repository.importServerEpisodes(
        'A', feed, [row('guid:a', enclosure: old['enclosure_url'] as String)],
        current: () => true);
    expect(await db.query('podcast_episode_state'), states);
    expect(await db.query('podcast_downloads'), copies);
    final episode = (await repository.episodes(feed)).single;
    expect(episode['episode_key'], 'guid:a');
    expect(episode['title'], 'Server guid:a');
    expect(episode['duration_seconds'], 52);
    expect(episode['published_at'],
        DateTime.utc(2026, 10, 28, 12).millisecondsSinceEpoch);
    expect(await repository.browseEpisodes('A', filter: 'all'), isEmpty);
    expect((await db.query('podcast_feeds')).single['etag'], 'keep-etag');
    expect(await repository.uncachedSubscriptions('A'), [feed]);
  });

  test(
      'enclosure match preserves existing device GUID and records server alias',
      () async {
    final old = (await repository.episodes(feed)).single;
    await repository.importServerEpisodes('A', feed,
        [row('guid:changed', enclosure: old['enclosure_url'] as String)],
        current: () => true);
    final episode = (await repository.episodes(feed)).single;
    expect(episode['episode_key'], 'guid:a');
    expect(episode['aliases'], contains('guid:changed'));
  });

  test(
      'pages all server metadata including listened; does not read response personal state',
      () async {
    final calls = <int>[];
    final metadata = PodcastServerMetadata(
        repository: repository,
        scope: 'A',
        current: () => true,
        fetchPage: (feedKey, offset, limit) async {
          expect(feedKey, feed);
          expect(limit, 200);
          calls.add(offset);
          return [
            for (var i = offset; i < 205 && i < offset + limit; i++)
              row('guid:$i')
          ];
        });
    await metadata.refresh(feed);
    expect(calls, [0, 200]);
    expect(await repository.episodes(feed), hasLength(206));
    expect(await (await repository.database).query('podcast_episode_state'),
        isEmpty);
  });

  test(
      'account change after network reply refuses import and transaction guard rolls back',
      () async {
    var current = true;
    final before = await repository.episodes(feed);
    final metadata = PodcastServerMetadata(
        repository: repository,
        scope: 'A',
        current: () => current,
        fetchPage: (_, __, ___) async {
          current = false;
          return [row('guid:new')];
        });
    await expectLater(metadata.refresh(feed), throwsStateError);
    expect(await repository.episodes(feed), before);
    var checks = 0;
    await expectLater(
        repository.importServerEpisodes('A', feed, [row('guid:new')],
            current: () => ++checks < 3),
        throwsStateError);
    expect(await repository.episodes(feed), before);
  });

  test(
      'account-filtered fallback remains retryable for another subscriber with omitted deleted episodes',
      () async {
    final db = await repository.database;
    final sub = (await db.query('podcast_subscriptions')).single;
    await db.insert('podcast_subscriptions', {...sub, 'scope': 'B'});
    await db.delete('podcast_episodes');
    await PodcastSyncStore(repository)
        .saveEpisode('A', feed, 'guid:a', deleted: true);
    await repository.importServerEpisodes('A', feed, [row('guid:b')],
        current: () => true);
    expect(await repository.uncachedSubscriptions('B'), [feed]);
    await repository.importServerEpisodes(
        'B', feed, [row('guid:a'), row('guid:b')],
        current: () => true);
    expect(await repository.browseEpisodes('B', filter: 'all'), hasLength(2));
    expect(await repository.browseEpisodes('A', filter: 'all'), hasLength(1));
    expect(await repository.uncachedSubscriptions('A'), [feed]);
  });
}
