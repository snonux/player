import 'dart:io';
import 'dart:convert';
import 'package:flutter_test/flutter_test.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';
import 'package:player_android/services/podcast_repository.dart';
import 'package:player_android/services/podcast_feed_fetcher.dart';

class FixtureFetcher extends PodcastFeedFetcher {
  bool fail = false;
  String? moved;
  String guid = 'a';
  String publication = '';
  String enclosure = 'https://cdn.example/e.mp3';
  @override
  Future<PublisherFeed> fetch(String url,
      {String etag = '', String lastModified = ''}) async {
    if (fail) throw const SocketException('offline');
    return parsePodcastFeed(
        '<rss><channel><title>Fixture</title><item><title>Episode</title><guid>$guid</guid><pubDate>$publication</pubDate><enclosure url="$enclosure"/></item></channel></rss>',
        fetchUrl: moved ?? url,
        aliasUrls: moved == null ? [] : [moved!]);
  }
}

void main() {
  sqfliteFfiInit();
  databaseFactory = databaseFactoryFfi;
  test(
      'existing zero-date cache refreshes without validators and retains history and copies',
      () async {
    final directory =
        await Directory.systemTemp.createTemp('podcast-date-repair-');
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final validators = <List<String?>>[];
    final requests = server.listen((request) async {
      final conditional = [
        request.headers.value(HttpHeaders.ifNoneMatchHeader),
        request.headers.value(HttpHeaders.ifModifiedSinceHeader)
      ];
      validators.add(conditional);
      if (conditional.any((v) => v != null)) {
        request.response.statusCode = 304;
      } else {
        request.response.headers.set(HttpHeaders.etagHeader, 'fixture');
        request.response.headers.set(
            HttpHeaders.lastModifiedHeader, 'Wed, 28 Oct 2026 12:57:50 GMT');
        request.response.write(
            '<rss><channel><title>Planet fixture</title><item><guid>stable</guid><title>Episode</title><pubDate>Wed, 28 Oct 2026 12:57:50 +0000</pubDate><enclosure url="https://example.com/a.mp3"/></item></channel></rss>');
      }
      await request.response.close();
    });
    final repository =
        PodcastRepository(databasePath: '${directory.path}/test.db');
    try {
      final key = await repository.subscribe(
          'local', 'http://127.0.0.1:${server.port}/rss');
      final db = await repository.database;
      await db.update('podcast_episodes', {'published_at': 0});
      await db.insert('podcast_episode_state', {
        'scope': 'local',
        'feed_key': key,
        'episode_key': 'guid:stable',
        'position_seconds': 31,
        'position_ms': 100,
        'completed': 1,
        'completion_ms': 200,
        'deleted': 1,
        'deletion_ms': 300,
        'deletion_pending_edit_id': 'keep-edit'
      });
      await db.insert('podcast_downloads', {
        'id': 'copy',
        'feed_key': key,
        'episode_key': 'guid:stable',
        'state': 'available'
      });
      final states = await db.query('podcast_episode_state'),
          copies = await db.query('podcast_downloads');
      expect(await repository.uncachedSubscriptions('local'), [key]);
      await repository.refreshSubscription('local', key);
      expect(validators.last, [null, null]);
      expect((await repository.episodes(key)).single['published_at'],
          DateTime.utc(2026, 10, 28, 12, 57, 50).millisecondsSinceEpoch);
      expect(await repository.uncachedSubscriptions('local'), isEmpty);
      expect(await db.query('podcast_episode_state'), states);
      expect(await db.query('podcast_downloads'), copies);
      await repository.refreshSubscription('local', key);
      expect(validators.last, ['fixture', 'Wed, 28 Oct 2026 12:57:50 GMT']);
    } finally {
      await repository.close();
      await requests.cancel();
      await server.close(force: true);
      await directory.delete(recursive: true);
    }
  });

  test('v5 migration keeps listening state, subscriptions, GUIDs and downloads',
      () async {
    final directory = await Directory.systemTemp.createTemp('podcast-v5-');
    final path = '${directory.path}/podcasts.db';
    var repository =
        PodcastRepository(databasePath: path, fetcher: FixtureFetcher());
    try {
      final key =
          await repository.subscribe('local', 'https://example.com/feed');
      var db = await repository.database;
      await db.insert('podcast_episode_state', {
        'scope': 'local',
        'feed_key': key,
        'episode_key': 'guid:a',
        'position_seconds': 17,
        'position_ms': 90,
        'completed': 1,
        'completion_ms': 100,
        'position_pending_edit_id': 'position',
        'completion_pending_edit_id': 'completion'
      });
      await db.insert('podcast_downloads', {
        'id': 'copy',
        'feed_key': key,
        'episode_key': 'guid:a',
        'state': 'available'
      });
      final subscriptions = await db.query('podcast_subscriptions');
      final episodes = await db.query('podcast_episodes');
      final downloads = await db.query('podcast_downloads');
      for (final column in [
        'deleted',
        'deletion_ms',
        'deletion_pending_edit_id'
      ]) {
        await db
            .execute('ALTER TABLE podcast_episode_state DROP COLUMN $column');
      }
      await db.setVersion(5);
      await repository.close();
      repository = PodcastRepository(databasePath: path);
      db = await repository.database;
      expect(await db.getVersion(), 6);
      expect(await db.query('podcast_subscriptions'), subscriptions);
      expect(await db.query('podcast_episodes'), episodes);
      expect(await db.query('podcast_downloads'), downloads);
      final state = (await db.query('podcast_episode_state')).single;
      expect(state['position_seconds'], 17);
      expect(state['completed'], 1);
      expect(state['position_pending_edit_id'], 'position');
      expect(state['completion_pending_edit_id'], 'completion');
      expect(state['deleted'], 0);
      expect(state['deletion_ms'], 0);
      expect(state['deletion_pending_edit_id'], isNull);
    } finally {
      await repository.close();
      await directory.delete(recursive: true);
    }
  });

  for (final oldVersion in [3, 4]) {
    test('upgrading v$oldVersion reparses metadata and preserves episode state',
        () async {
      final directory = await Directory.systemTemp.createTemp('podcast-dates-');
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      final origin = 'http://127.0.0.1:${server.port}';
      final validators = <List<String?>>[];
      final subscription = server.listen((request) async {
        final conditional = [
          request.headers.value(HttpHeaders.ifNoneMatchHeader),
          request.headers.value(HttpHeaders.ifModifiedSinceHeader)
        ];
        validators.add(conditional);
        if (conditional.any((value) => value != null)) {
          request.response.statusCode = 304;
        } else {
          request.response.headers.set(HttpHeaders.etagHeader, 'fixture');
          request.response.headers.set(
              HttpHeaders.lastModifiedHeader, 'Mon, 26 Oct 2026 14:00:00 GMT');
          request.response.write(
              '<rss xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd" xmlns:content="http://purl.org/rss/1.0/modules/content/"><channel><title>Fixture</title><itunes:image href="https://cdn.example/feed.jpg"/>'
              '<item><guid>a-older</guid><title>Older</title>'
              '<pubDate>Mon, 26 Oct 2026 15:00:00 +0200</pubDate>'
              '<enclosure url="https://cdn.example/older.mp3"/></item>'
              '<item><guid>z-newer</guid><title>Newer</title><itunes:image href="https://cdn.example/episode.jpg"/><description>Short</description><content:encoded><![CDATA[<p>Full notes</p>]]></content:encoded>'
              '<pubDate>Mon, 26 Oct 2026 10:30:00 -0330</pubDate>'
              '<enclosure url="https://cdn.example/newer.mp3"/></item>'
              '</channel></rss>');
        }
        await request.response.close();
      });
      final path = '${directory.path}/podcasts.db';
      var repository = PodcastRepository(databasePath: path);
      try {
        final key = await repository.subscribe('local', '$origin/feed');
        final healthyKey =
            await repository.subscribe('local', '$origin/healthy');
        var db = await repository.database;
        if (oldVersion == 3) {
          await db.update('podcast_episodes', {'published_at': 0},
              where: 'feed_key=?', whereArgs: [key]);
        }
        await db.insert('podcast_episode_state', {
          'scope': 'local',
          'feed_key': key,
          'episode_key': 'guid:z-newer',
          'position_seconds': 42.0,
          'position_ms': 100,
          'completed': 1,
          'completion_ms': 200,
          'completion_source': 'manual'
        });
        await db.insert('podcast_downloads', {
          'id': 'download',
          'feed_key': key,
          'episode_key': 'guid:z-newer',
          'state': 'complete',
          'size_bytes': 12345
        });
        final state = await db.query('podcast_episode_state');
        final downloads = await db.query('podcast_downloads');
        final subscriptions = await db.query('podcast_subscriptions');
        final healthy = (await db.query('podcast_feeds',
                where: 'feed_key=?', whereArgs: [healthyKey]))
            .single;
        final affected = (await db
                .query('podcast_feeds', where: 'feed_key=?', whereArgs: [key]))
            .single;
        // Simulate a persisted pre-fix cache, including validators which would
        // otherwise hide the original numeric dates behind a 304 response.
        await db.execute('ALTER TABLE podcast_episodes DROP COLUMN image_url');
        await db.setVersion(oldVersion);
        await repository.close();
        repository = PodcastRepository(databasePath: path);
        db = await repository.database;
        final repaired = (await db
                .query('podcast_feeds', where: 'feed_key=?', whereArgs: [key]))
            .single;
        expect(repaired, {...affected, 'etag': '', 'last_modified': ''});
        expect(
            (await db.query('podcast_feeds',
                    where: 'feed_key=?', whereArgs: [healthyKey]))
                .single,
            {...healthy, 'etag': '', 'last_modified': ''});
        expect(await db.getVersion(), 6);
        await repository.refresh(key);
        expect(validators.last, [null, null]);
        final episodes = await repository.episodes(key);
        expect(episodes.map((episode) => episode['episode_key']),
            ['guid:z-newer', 'guid:a-older']);
        expect(episodes.map((episode) => episode['published_at']), [
          DateTime.utc(2026, 10, 26, 14).millisecondsSinceEpoch,
          DateTime.utc(2026, 10, 26, 13).millisecondsSinceEpoch
        ]);
        expect(episodes.first['image_url'], 'https://cdn.example/episode.jpg');
        expect(episodes.first['description'], '<p>Full notes</p>');
        final browse =
            await repository.browseEpisodes('local', feed: key, filter: 'all');
        expect(browse.first['feed_image_url'], 'https://cdn.example/feed.jpg');
        expect(await db.query('podcast_episode_state'), state);
        expect(await db.query('podcast_downloads'), downloads);
        expect(await db.query('podcast_subscriptions'), subscriptions);
        await repository.refresh(key);
        expect(validators.last, ['fixture', 'Mon, 26 Oct 2026 14:00:00 GMT']);
        expect(await repository.episodes(key), episodes);
      } finally {
        await repository.close();
        await subscription.cancel();
        await server.close(force: true);
        await directory.delete(recursive: true);
      }
    });
  }
  test('association readiness and account change roll back the entire copy',
      () async {
    final repository = PodcastRepository(
        databasePath: inMemoryDatabasePath, fetcher: FixtureFetcher());
    const account = 'account:https://server.example#7';
    try {
      expect(await repository.canAssociateLocal(account), isFalse);
      await repository.subscribe('local', 'https://example.com/feed');
      expect(await repository.canAssociateLocal(account), isTrue);
      final db = await repository.database;
      final local = await db.query('podcast_subscriptions');
      var checks = 0;
      await expectLater(
          repository.associateLocal(account, current: () => ++checks == 1),
          throwsStateError);
      expect(checks, 2);
      expect(await db.query('podcast_subscriptions'), local);
      expect(
          await db
              .query('podcast_scopes', where: 'scope=?', whereArgs: [account]),
          isEmpty);
      expect(await repository.canAssociateLocal(account), isTrue);
      await repository.associateLocal(account, current: () => true);
      expect(await repository.canAssociateLocal(account), isFalse);
      expect(
          await db.query('podcast_subscriptions',
              where: 'scope=?', whereArgs: ['local']),
          local);
    } finally {
      await repository.close();
    }
  });
  test('publisher redirect policy, validators and Atom', () async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final origin = 'http://127.0.0.1:${server.port}';
    final requests = <HttpRequest>[];
    final subscription = server.listen((request) async {
      requests.add(request);
      if (request.uri.path == '/permanent' ||
          request.uri.path == '/temporary') {
        request.response.statusCode =
            request.uri.path == '/permanent' ? 301 : 302;
        request.response.headers.set('location', '/feed');
      } else if (request.headers.value('if-none-match') == 'fixture') {
        request.response.statusCode = 304;
      } else {
        request.response.headers.set('etag', 'fixture');
        request.response.write(
            '<feed xmlns="http://www.w3.org/2005/Atom"><title>Atom</title><entry><id>one</id><title>One</title><link rel="enclosure" href="https://cdn.example/one.mp3"/></entry></feed>');
      }
      await request.response.close();
    });
    try {
      final fetcher = PodcastFeedFetcher();
      final permanent = await fetcher.fetch('$origin/permanent');
      expect(permanent.aliasUrls, ['$origin/feed']);
      expect(permanent.fetchUrl, '$origin/feed');
      expect(permanent.episodes.single.identity.guid, 'one');
      final temporary = await fetcher.fetch('$origin/temporary');
      expect(temporary.aliasUrls, isEmpty);
      expect(temporary.fetchUrl, '$origin/temporary');
      expect((await fetcher.fetch('$origin/feed', etag: 'fixture')).notModified,
          isTrue);
      expect(
          requests.every((r) =>
              r.headers.value('authorization') == null &&
              r.headers.value('cookie') == null),
          isTrue);
    } finally {
      await subscription.cancel();
      await server.close(force: true);
    }
  });
  test(
      'feed merges preserve established feed and episode aliases beyond acquisition cap',
      () async {
    final fetcher = FixtureFetcher();
    final repository =
        PodcastRepository(databasePath: inMemoryDatabasePath, fetcher: fetcher);
    try {
      final source =
          await repository.subscribe('local', 'https://example.com/old');
      final target =
          await repository.subscribe('local', 'https://example.com/new');
      final db = await repository.database;
      final targetAliases =
          List.generate(16, (i) => 'feed:example.com/target-$i');
      await db.update('podcast_feeds', {'aliases': jsonEncode(targetAliases)},
          where: 'feed_key=?', whereArgs: [target]);
      await db.update(
          'podcast_feeds',
          {
            'aliases': jsonEncode(['feed:example.com/source-alias'])
          },
          where: 'feed_key=?',
          whereArgs: [source]);
      await db.update(
          'podcast_episodes',
          {
            'episode_key': 'enc:cdn.example/e.mp3',
            'aliases': jsonEncode(['guid:b'])
          },
          where: 'feed_key=?',
          whereArgs: [source]);
      await db.update('podcast_episodes',
          {'aliases': jsonEncode(List.generate(16, (i) => 'guid:target-$i'))},
          where: 'feed_key=?', whereArgs: [target]);
      await db.insert('podcast_episode_state', {
        'scope': 'local',
        'feed_key': source,
        'episode_key': 'enc:cdn.example/e.mp3',
        'position_seconds': 42.0,
        'position_ms': 100
      });
      fetcher.moved = 'https://example.com/new';
      await repository.refresh(source);
      for (final alias in [
        ...targetAliases,
        source,
        'feed:example.com/source-alias'
      ]) {
        expect(await repository.resolveFeed(db, alias), target);
      }
      final aliases = jsonDecode(
              (await repository.episodes(target)).single['aliases'] as String)
          as List;
      expect(aliases, contains('guid:b'));
      expect(aliases, contains('enc:cdn.example/e.mp3'));
      expect(aliases, hasLength(18));
      fetcher.moved = null;
      fetcher.guid = 'b';
      fetcher.enclosure = 'https://cdn.example/rotated.mp3';
      await repository.refresh(target);
      expect(
          (await repository.episodes(target)).single['episode_key'], 'guid:a');
      expect(
          (await db.query('podcast_episode_state')).single['position_seconds'],
          42.0);
      await repository.subscribe('local', 'https://example.com/old');
      expect(await repository.subscriptions('local'), hasLength(1));
      expect(await db.query('podcast_feeds'), hasLength(1));
    } finally {
      await repository.close();
    }
  });
  test('feed merge and per-register association preserve newest clocks',
      () async {
    final fetcher = FixtureFetcher();
    final repository =
        PodcastRepository(databasePath: inMemoryDatabasePath, fetcher: fetcher);
    try {
      final source =
          await repository.subscribe('local', 'https://example.com/old');
      final target =
          await repository.subscribe('local', 'https://example.com/new');
      final db = await repository.database;
      await db.insert('podcast_episode_state', {
        'scope': 'local',
        'feed_key': source,
        'episode_key': 'guid:a',
        'position_seconds': 42.0,
        'position_ms': 100,
        'completed': 1,
        'completion_ms': 200,
        'completion_source': 'manual'
      });
      await db.insert('podcast_episode_state', {
        'scope': 'account:https://server.example#7',
        'feed_key': target,
        'episode_key': 'guid:a',
        'position_seconds': 81.0,
        'position_ms': 300,
        'position_pending_edit_id': 'newer',
        'completed': 0,
        'completion_ms': 50
      });
      fetcher.moved = 'https://example.com/new';
      await repository.refresh(source);
      expect(await repository.subscriptions('local'), hasLength(1));
      expect(await repository.resolveFeed(db, source), target);
      await repository.associateLocal('account:https://server.example#7');
      final account = (await db.query('podcast_episode_state',
              where: 'scope=?',
              whereArgs: ['account:https://server.example#7']))
          .single;
      expect(account['position_seconds'], 81.0);
      expect(account['position_pending_edit_id'], 'newer');
      expect(account['completed'], 1);
      expect(account['completion_ms'], 200);
      expect(account['completion_pending_edit_id'], isNotEmpty);
      expect(
          (await db.query('podcast_episode_state',
                  where: 'scope=?', whereArgs: ['local']))
              .single['position_seconds'],
          42.0);
    } finally {
      await repository.close();
    }
  });
  test('standalone cached refresh, durable tombstone and account isolation',
      () async {
    final directory = await Directory.systemTemp.createTemp('podcast-test-');
    final fetcher = FixtureFetcher();
    final path = '${directory.path}/podcasts.db';
    var repository = PodcastRepository(databasePath: path, fetcher: fetcher);
    try {
      final key =
          await repository.subscribe('local', 'https://example.com/feed');
      expect((await repository.episodes(key)).single['episode_key'], 'guid:a');
      fetcher.fail = true;
      await expectLater(
          repository.refresh(key), throwsA(isA<SocketException>()));
      expect(await repository.episodes(key), hasLength(1));
      final account = podcastAccountScope('https://server.example', 7);
      expect(await repository.subscriptions(account), isEmpty);
      await repository.associateLocal(account);
      expect(await repository.subscriptions(account), hasLength(1));
      await repository.unsubscribe(account, key);
      await repository.close();
      repository = PodcastRepository(databasePath: path, fetcher: fetcher);
      expect(await repository.subscriptions(account), isEmpty);
      expect(await repository.subscriptions('local'), hasLength(1));
      expect(await repository.episodes(key), hasLength(1));
      final tombstone = (await (await repository.database).query(
              'podcast_subscriptions',
              where: 'scope=?',
              whereArgs: [account]))
          .single;
      expect(tombstone['subscribed'], 0);
      expect(tombstone['pending_edit_id'], isNotEmpty);
    } finally {
      await repository.close();
      await directory.delete(recursive: true);
    }
  });
}
