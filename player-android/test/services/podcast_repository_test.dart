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
  String enclosure = 'https://cdn.example/e.mp3';
  @override
  Future<PublisherFeed> fetch(String url,
      {String etag = '', String lastModified = ''}) async {
    if (fail) throw const SocketException('offline');
    return parsePodcastFeed(
        '<rss><channel><title>Fixture</title><item><title>Episode</title><guid>$guid</guid><enclosure url="$enclosure"/></item></channel></rss>',
        fetchUrl: moved ?? url,
        aliasUrls: moved == null ? [] : [moved!]);
  }
}

void main() {
  sqfliteFfiInit();
  databaseFactory = databaseFactoryFfi;
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
