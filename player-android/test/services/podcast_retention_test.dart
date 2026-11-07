import 'dart:io';
import 'package:flutter_test/flutter_test.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';
import 'package:player_android/services/podcast_repository.dart';
import 'package:player_android/services/podcast_sync_store.dart';
import 'package:player_android/services/podcast_downloads.dart';
import 'package:player_android/services/podcast_retention.dart';
import 'package:player_android/services/playback_session_coordinator.dart';
import 'package:player_android/services/playback_request.dart';

void main() {
  sqfliteFfiInit();
  databaseFactory = databaseFactoryFfi;
  const feed = 'feed:example.com/rss', episode = 'guid:one';
  const id = '0123456789abcdef0123456789abcdef';
  const start = 1800000000000;
  const week = 7 * 86400000;
  late Directory directory;
  late PodcastRepository repository;
  late PodcastSyncStore store;
  late PodcastDownloads downloads;
  late PlaybackSessionCoordinator playback;
  late PodcastRetention retention;
  late File file;
  setUp(() async {
    directory = await Directory.systemTemp.createTemp('podcast-retention-');
    repository = PodcastRepository(databasePath: '${directory.path}/db');
    store = PodcastSyncStore(repository);
    playback = PlaybackSessionCoordinator();
    downloads = PodcastDownloads(repository, playback, directory: directory);
    retention = PodcastRetention(store, downloads);
    await downloads.ready;
    file = File('${directory.path}/$id.audio');
    await file.writeAsBytes([1, 2, 3]);
    await (await repository.database).insert('podcast_downloads', {
      'id': id,
      'feed_key': feed,
      'episode_key': episode,
      'state': 'available',
      'size_bytes': 3,
    });
    await store.saveEpisode('local', feed, episode,
        seconds: 23, completed: true, observedMs: 20);
  });
  tearDown(() async {
    downloads.dispose();
    await repository.close();
    await directory.delete(recursive: true);
  });
  Future<List<Map<String, Object?>>> state() async =>
      (await repository.database).query('podcast_episode_state',
          where: 'scope=?', whereArgs: ['local']);

  test(
      'exact seven days hides and removes copy, preserves history without queuing a synchronized deletion',
      () async {
    await retention.sweep('local', current: () => true, nowMs: start);
    await retention.sweep('local',
        current: () => true, nowMs: start + week - 1);
    expect(await file.exists(), isTrue);
    await retention.sweep('local', current: () => true, nowMs: start + week);
    expect(await file.exists(), isFalse);
    final row = (await state()).single;
    expect(row['deleted'], 0);
    expect(row['completed'], 1);
    expect(row['position_seconds'], 23);
    expect(row['deletion_pending_edit_id'], isNull);
    final edit = row['deletion_pending_edit_id'];
    await retention.sweep('local',
        current: () => true, nowMs: start + week + 1);
    expect((await state()).single['deletion_pending_edit_id'], edit);
  });

  test(
      'another subscribed library protects global device copy until its deletion',
      () async {
    final db = await repository.database;
    await db.insert('podcast_subscriptions', {
      'scope': 'other',
      'feed_key': feed,
      'subscribed': 1,
      'subscription_ms': 1
    });
    await retention.sweep('local', current: () => true, nowMs: start);
    await retention.sweep('local', current: () => true, nowMs: start + week);
    expect((await state()).single['deleted'], 0);
    expect(await file.exists(), isTrue);
    await store.saveEpisode('other', feed, episode,
        deleted: true, observedMs: 30);
    await retention.sweep('local',
        current: () => true, nowMs: start + week + 1);
    expect(await file.exists(), isFalse);
  });

  test('active playback is skipped and retried after stopping', () async {
    await playback.claim(
        kind: PlaybackSourceKind.local,
        identity: 'podcast',
        sourceUri: file.uri.toString(),
        stop: () async {});
    await retention.sweep('local', current: () => true, nowMs: start);
    await retention.sweep('local', current: () => true, nowMs: start + week);
    expect(await file.exists(), isTrue);
    await playback.stopCurrent();
    await retention.sweep('local',
        current: () => true, nowMs: start + week + 1);
    expect(await file.exists(), isFalse);
  });

  test('uncompleted, newer playback, restore and changed account retain copies',
      () async {
    await retention.sweep('local', current: () => true, nowMs: start);
    await store.saveEpisode('local', feed, episode,
        seconds: 24, observedMs: 30);
    await retention.sweep('local', current: () => true, nowMs: start + week);
    expect((await state()).single['deleted'], 0);
    expect(await file.exists(), isTrue);
    await store.saveEpisode('local', feed, episode,
        completed: true, observedMs: 40);
    await retention.sweep('local', current: () => true, nowMs: start + week);
    await retention.sweep('local',
        current: () => false, nowMs: start + 2 * week);
    expect((await state()).single['deleted'], 0);
    await store.saveEpisode('local', feed, episode,
        deleted: false, observedMs: 50);
    await retention.sweep('local',
        current: () => true, nowMs: start + 2 * week);
    expect((await state()).single['deleted'], 0);
    await store.saveEpisode('local', feed, episode,
        completed: false, observedMs: 60);
    await retention.sweep('local',
        current: () => true, nowMs: start + 3 * week);
    expect(await file.exists(), isTrue);
  });
  for (final guard in ['replay', 'restore', 'recompletion']) {
    test('offline expiry never sends deletion over unknown remote $guard',
        () async {
      final db = await repository.database;
      await db.insert('podcast_subscriptions', {
        'scope': 'local',
        'feed_key': feed,
        'subscribed': 1,
        'subscription_ms': 1
      });
      await db.insert('podcast_feeds',
          {'feed_key': feed, 'fetch_url': 'https://example.com/rss'});
      await db.insert('podcast_episodes', {
        'feed_key': feed,
        'episode_key': episode,
        'title': 'Episode',
        'description': '',
        'enclosure_url': 'https://example.com/one.mp3',
        'enclosure_key': 'enc:example.com/one.mp3',
        'published_at': 1,
        'duration_seconds': 120
      });
      final now = DateTime.now().millisecondsSinceEpoch;
      await retention.sweep('local', current: () => true, nowMs: now - week);
      await retention.sweep('local', current: () => true, nowMs: now);
      expect(await repository.browseEpisodes('local', filter: 'all'), isEmpty);
      final pending = await store.payload('local');
      for (final row in pending['episodes'] as List) {
        expect(row['deletion'], isNull,
            reason: 'A local timer must never send a deletion');
      }
      await store.apply(
          'local',
          {
            'acked': PodcastSyncStore.editIds(pending).toList(),
            'episodes': [
              {
                'feed_key': feed,
                'episode_key': episode,
                'position': {
                  'seconds': guard == 'replay' ? 42.0 : 23.0,
                  'observed_ms': guard == 'replay' ? 30 : 20
                },
                'completion': {
                  'completed': true,
                  'source': 'manual',
                  'observed_ms': guard == 'recompletion' ? 40 : 20
                },
                if (guard == 'restore')
                  'deletion': {'deleted': false, 'observed_ms': 40},
              }
            ],
          },
          () => true,
          scoped: true);
      expect(await repository.browseEpisodes('local', filter: 'all'),
          hasLength(1));
      await retention.sweep('local', current: () => true, nowMs: now + 1);
      final after = await store.payload('local');
      expect(after['episodes'], isEmpty);
      expect((await state()).single['deleted'], 0);
    });
  }
  for (final remote in [false, true]) {
    for (final guard in ['replay', 'restore', 'recompletion', 'uncompleted']) {
      test(
          'unlink revalidates ${remote ? "remote" : "local"} $guard after due snapshot',
          () async {
        await store.retainCompleted('local', nowMs: start);
        final due = await store.retainCompleted('local', nowMs: start + week);
        expect(due, hasLength(1));
        if (remote) {
          final pending = await store.payload('local');
          await store.apply(
              'local',
              {
                'acked': PodcastSyncStore.editIds(pending).toList(),
                'episodes': [
                  {
                    'feed_key': feed,
                    'episode_key': episode,
                    'position': {
                      'seconds': guard == 'replay' ? 42.0 : 23.0,
                      'observed_ms': guard == 'replay' ? 30 : 20
                    },
                    'completion': {
                      'completed': guard != 'uncompleted',
                      'source': 'manual',
                      'observed_ms':
                          guard == 'recompletion' || guard == 'uncompleted'
                              ? 30
                              : 20
                    },
                    if (guard == 'restore')
                      'deletion': {'deleted': false, 'observed_ms': 30},
                  }
                ]
              },
              () => true,
              scoped: true);
        } else {
          await store.saveEpisode('local', feed, episode,
              seconds: guard == 'replay' ? 42 : null,
              completed: guard == 'recompletion'
                  ? true
                  : guard == 'uncompleted'
                      ? false
                      : null,
              deleted: guard == 'restore' ? false : null,
              observedMs: 30);
        }
        await downloads.removeExpiredCopy('local', feed, episode,
            current: () => true, nowMs: start + week);
        expect(await file.exists(), isTrue);
        expect(
            (await (await repository.database).query('podcast_downloads'))
                .single['state'],
            'available');
      });
    }
  }

  test(
      'account feed and episode aliases find physical bytes and protect other account',
      () async {
    final db = await repository.database;
    const canonicalFeed = 'feed:canonical.example.com/rss',
        canonicalEpisode = 'guid:canonical';
    await store.retainCompleted('local', nowMs: start);
    await db.update('podcast_episode_state',
        {'feed_key': canonicalFeed, 'episode_key': canonicalEpisode},
        where: 'scope=?', whereArgs: ['local']);
    Future<void> alias(String scope, String kind, String feedKey, String source,
        String target) async {
      await db.insert('podcast_account_aliases', {
        'scope': scope,
        'kind': kind,
        'feed_key': feedKey,
        'source': source,
        'target': target
      });
    }

    await alias('local', 'feed', '', feed, canonicalFeed);
    await alias('local', 'episode', canonicalFeed, episode, canonicalEpisode);
    const otherFeed = 'feed:other.example.com/rss', otherEpisode = 'guid:other';
    await alias('other', 'feed', '', feed, otherFeed);
    await alias('other', 'episode', otherFeed, episode, otherEpisode);
    await db.insert('podcast_subscriptions', {
      'scope': 'other',
      'feed_key': otherFeed,
      'subscribed': 1,
      'subscription_ms': 1
    });
    final due = await store.retainCompleted('local', nowMs: start + week);
    expect(due.single['feed_key'], canonicalFeed);
    await downloads.removeExpiredCopy('local', canonicalFeed, canonicalEpisode,
        current: () => true, nowMs: start + week);
    expect(await file.exists(), isTrue,
        reason: 'Other account needs its aliased copy');
    await store.saveEpisode('other', otherFeed, otherEpisode,
        deleted: true, observedMs: 30);
    await downloads.removeExpiredCopy('local', canonicalFeed, canonicalEpisode,
        current: () => true, nowMs: start + week);
    expect(await file.exists(), isFalse,
        reason: 'Physical keys must map forward into selected canonical keys');
    final copy = (await db.query('podcast_downloads')).single;
    expect(copy['feed_key'], feed);
    expect(copy['episode_key'], episode);
    expect(copy['state'], 'missing');
  });
}
