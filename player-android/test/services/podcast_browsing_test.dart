import 'dart:io';
import 'package:flutter_test/flutter_test.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';
import 'package:player_android/services/podcast_repository.dart';
import 'package:player_android/services/podcast_sync_store.dart';
import 'podcast_repository_test.dart' show FixtureFetcher;

void main() {
  sqfliteFfiInit();
  databaseFactory = databaseFactoryFfi;

  test(
      'deleted episodes stay absent after metadata refresh and remain visible to another account',
      () async {
    final repository = PodcastRepository(
        databasePath: inMemoryDatabasePath, fetcher: FixtureFetcher());
    try {
      final feed = await repository.subscribe('A', 'https://example.com/feed');
      await repository.subscribe('B', 'https://example.com/feed');
      final episode =
          (await repository.episodes(feed)).single['episode_key'] as String;
      final store = PodcastSyncStore(repository);
      await store.saveEpisode('A', feed, episode, seconds: 39, completed: true);
      await store.saveEpisode('A', feed, episode, deleted: true);
      await repository.refresh(feed);
      expect(await repository.browseEpisodes('A', filter: 'all'), isEmpty);
      expect(
          await repository.browseEpisodes('A', feed: feed, filter: 'completed'),
          isEmpty);
      expect(await repository.browseEpisodes('B', filter: 'all'), hasLength(1));
      final row =
          (await (await repository.database).query('podcast_episode_state'))
              .single;
      expect(row['position_seconds'], 39);
      expect(row['completed'], 1);
      expect(await repository.episodes(feed), hasLength(1));
    } finally {
      await repository.close();
    }
  });

  test('missing subscription metadata is detected across the full library',
      () async {
    final repository = PodcastRepository(
        databasePath: inMemoryDatabasePath,
        fetcher: FixtureFetcher()
          ..publication = 'Mon, 26 Oct 2026 14:00:00 GMT');
    try {
      await repository.subscribe('A', 'https://example.com/feed');
      final db = await repository.database;
      for (var i = 0; i < 22; i++) {
        await db.insert('podcast_subscriptions', {
          'scope': 'A',
          'feed_key': 'feed:example.com/$i',
          'subscribed': 1,
          'subscription_ms': 1,
          'fetch_url': 'https://example.com/$i'
        });
      }
      expect(await repository.uncachedSubscriptions('A'), hasLength(22));
      for (final key in await repository.uncachedSubscriptions('A')) {
        await repository.refreshSubscription('A', key);
      }
      expect(await repository.uncachedSubscriptions('A'), isEmpty);
      expect(
          await repository.browseEpisodes('A', filter: 'all'), hasLength(23));
    } finally {
      await repository.close();
    }
  });

  test(
      'newest first pages include started; manual completion survives restart without changing position',
      () async {
    final directory = await Directory.systemTemp.createTemp('podcast-browse-');
    var repository = PodcastRepository(
        databasePath: '${directory.path}/test.db', fetcher: FixtureFetcher());
    try {
      final feed =
          await repository.subscribe('local', 'https://example.com/feed');
      final db = await repository.database;
      final template = (await repository.episodes(feed)).single;
      for (var i = 0; i < 82; i++) {
        await db.insert('podcast_episodes', {
          ...template,
          'episode_key': 'guid:$i',
          'title': 'Episode $i',
          'published_at': i + 1
        });
      }
      final store = PodcastSyncStore(repository);
      await store.saveEpisode('local', feed, 'guid:81', seconds: 42);
      final first = await repository.browseEpisodes('local');
      final second = await repository.browseEpisodes('local', offset: 40);
      expect(first.length, 40);
      expect(first.first['episode_key'], 'guid:81');
      expect(first.first['position_seconds'], 42);
      expect(
          first
              .map((e) => e['episode_key'])
              .toSet()
              .intersection(second.map((e) => e['episode_key']).toSet()),
          isEmpty);
      await store.saveEpisode('local', feed, 'guid:81', completed: true);
      expect((await repository.browseEpisodes('local')).first['episode_key'],
          'guid:80');
      expect(
          (await repository.browseEpisodes('local',
                  feed: feed, filter: 'completed'))
              .single['position_seconds'],
          42);
      await repository.close();
      repository = PodcastRepository(databasePath: '${directory.path}/test.db');
      final completed =
          (await repository.browseEpisodes('local', filter: 'completed'))
              .single;
      expect(completed['completion_pending_edit_id'], isNotEmpty);
      expect(completed['position_pending_edit_id'], isNotEmpty);
      await PodcastSyncStore(repository)
          .saveEpisode('local', feed, 'guid:81', completed: false);
      expect(
          (await repository.browseEpisodes('local')).first['position_seconds'],
          42);
      expect(await repository.browseEpisodes('other'), isEmpty);
      await expectLater(
          repository.browseEpisodes('local', offset: -1), throwsArgumentError);
      await expectLater(repository.browseEpisodes('local', filter: 'bogus'),
          throwsArgumentError);
    } finally {
      await repository.close();
      await directory.delete(recursive: true);
    }
  });

  test(
      'account aliases collapse catalog duplicates and retain device copy keys without leaking to another account',
      () async {
    final repository = PodcastRepository(
        databasePath: inMemoryDatabasePath, fetcher: FixtureFetcher());
    try {
      final source =
          await repository.subscribe('A', 'https://example.com/source');
      final target =
          await repository.subscribe('B', 'https://example.com/target');
      final db = await repository.database;
      await db.update('podcast_subscriptions', {'feed_key': target},
          where: 'scope=?', whereArgs: ['A']);
      await db.insert('podcast_account_aliases',
          {'scope': 'A', 'kind': 'feed', 'source': source, 'target': target});
      await db.update('podcast_episodes', {'episode_key': 'guid:device'},
          where: 'feed_key=?', whereArgs: [source]);
      await db.insert('podcast_account_aliases', {
        'scope': 'A',
        'kind': 'episode',
        'feed_key': target,
        'source': 'guid:device',
        'target': 'guid:a'
      });
      await db.insert('podcast_downloads', {
        'id': 'copy',
        'feed_key': source,
        'episode_key': 'guid:device',
        'state': 'available'
      });
      await PodcastSyncStore(repository)
          .saveEpisode('A', target, 'guid:a', seconds: 19, completed: true);
      final rows = await repository.browseEpisodes('A', filter: 'all');
      expect(rows, hasLength(1));
      expect(rows.single['feed_key'], source);
      expect(rows.single['episode_key'], 'guid:device');
      expect(rows.single['scope_feed_key'], target);
      expect(rows.single['scope_episode_key'], 'guid:a');
      expect(rows.single['position_seconds'], 19);
      final other = (await repository.browseEpisodes('B')).single;
      expect(other['feed_key'], target);
      expect(other['completed'], 0);
      expect(other['position_seconds'], 0);
      await repository.unsubscribe('A', target);
      expect(await repository.browseEpisodes('A', filter: 'all'), isEmpty);
      expect(await db.query('podcast_downloads'), hasLength(1));
      expect(await db.query('podcast_episode_state'), hasLength(1));
      expect(await db.query('podcast_feeds'), hasLength(2));
    } finally {
      await repository.close();
    }
  });

  for (final transferState in [
    'pending',
    'downloading',
    'failed',
    'missing',
    null
  ]) {
    for (final availableFirst in [false, true]) {
      test(
          'aliased available copy beats $transferState when availableFirst=$availableFirst',
          () async {
        final repository = PodcastRepository(
            databasePath: inMemoryDatabasePath, fetcher: FixtureFetcher());
        try {
          final source =
              await repository.subscribe('A', 'https://example.com/source');
          final target =
              await repository.subscribe('B', 'https://example.com/target');
          final db = await repository.database;
          await db.update('podcast_subscriptions', {'feed_key': target},
              where: 'scope=?', whereArgs: ['A']);
          await db.insert('podcast_account_aliases', {
            'scope': 'A',
            'kind': 'feed',
            'source': source,
            'target': target
          });
          // Flip query order independently of the device keys.
          await db.update(
              'podcast_episodes', {'published_at': availableFirst ? 1 : 2},
              where: 'feed_key=?', whereArgs: [source]);
          await db.update(
              'podcast_episodes', {'published_at': availableFirst ? 2 : 1},
              where: 'feed_key=?', whereArgs: [target]);
          if (transferState != null) {
            await db.insert('podcast_downloads', {
              'id': 'transfer',
              'feed_key': source,
              'episode_key': 'guid:a',
              'state': transferState
            });
          }
          await db.insert('podcast_downloads', {
            'id': 'ready',
            'feed_key': target,
            'episode_key': 'guid:a',
            'state': 'available'
          });
          final row = (await repository.browseEpisodes('A')).single;
          expect(row['feed_key'], target);
          expect(row['episode_key'], 'guid:a');
          final selectedCopy = (await db.query('podcast_downloads',
                  where: 'feed_key=? AND episode_key=?',
                  whereArgs: [row['feed_key'], row['episode_key']]))
              .single;
          expect(selectedCopy['id'], 'ready');
          expect(selectedCopy['state'], 'available');
          expect(await db.query('podcast_downloads'),
              hasLength(transferState == null ? 1 : 2));
        } finally {
          await repository.close();
        }
      });
    }
  }

  test(
      'refresh hydrates a sync-only subscription without editing its register clock',
      () async {
    final repository = PodcastRepository(
        databasePath: inMemoryDatabasePath, fetcher: FixtureFetcher());
    try {
      final db = await repository.database;
      await db.insert('podcast_subscriptions', {
        'scope': 'A',
        'feed_key': 'feed:example.com/feed',
        'subscribed': 1,
        'subscription_ms': 123,
        'fetch_url': 'https://example.com/feed'
      });
      await repository.refreshSubscription('A', 'feed:example.com/feed');
      expect(await repository.browseEpisodes('A'), hasLength(1));
      final row = (await repository.subscriptions('A')).single;
      expect(row['subscription_ms'], 123);
      expect(row['pending_edit_id'], isNull);
    } finally {
      await repository.close();
    }
  });
}
