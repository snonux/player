import 'dart:io';
import 'package:flutter_test/flutter_test.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';
import 'package:player_android/services/podcast_repository.dart';
import 'package:player_android/services/podcast_sync_store.dart';
import 'package:player_android/services/podcast_sync.dart';
import 'package:player_android/services/global_sync_coordinator.dart';

const scope = 'account:https://example.com#1';
const feed = 'feed:example.com/rss';
const episode = 'guid:one';
Map<String, dynamic> reply(
        {String epoch = 'one',
        List<String> acked = const [],
        List<Map<String, dynamic>> episodes = const [],
        List<Map<String, dynamic>> subscriptions = const [],
        List<Map<String, dynamic>> aliases = const [],
        bool more = false,
        int cursor = 1}) =>
    {
      'epoch': epoch,
      'acked': acked,
      'rejected': [],
      'aliases': aliases,
      'episodes': episodes,
      'subscriptions': subscriptions,
      'next_since': cursor,
      'has_more': more,
    };
Map<String, dynamic> position(double seconds, int ms, {String key = episode}) =>
    {
      'feed_key': feed,
      'episode_key': key,
      'position': {'seconds': seconds, 'observed_ms': ms},
    };
void main() {
  sqfliteFfiInit();
  databaseFactory = databaseFactoryFfi;
  late Directory temp;
  late PodcastRepository repository;
  late PodcastSyncStore store;
  setUp(() async {
    temp = await Directory.systemTemp.createTemp('podcast-sync-');
    repository = PodcastRepository(databasePath: '${temp.path}/podcasts.db');
    store = PodcastSyncStore(repository);
    await store.status(scope);
  });
  tearDown(() async {
    await repository.close();
    await temp.delete(recursive: true);
  });
  test(
      'restart retains edit IDs; clamped ack adopts server, newer edit survives late ack',
      () async {
    await store.saveEpisode(scope, feed, episode,
        seconds: 12, observedMs: 99999);
    final first = PodcastSyncStore.editIds(await store.payload(scope)).single;
    await repository.close();
    expect(PodcastSyncStore.editIds(await store.payload(scope)), {first});
    await store.apply(scope,
        reply(acked: [first], episodes: [position(12, 123)]), () => true);
    var row = (await repository.database).query('podcast_episode_state');
    expect((await row).single['position_ms'], 123);
    await store.saveEpisode(scope, feed, episode, seconds: 20, observedMs: 200);
    final second = PodcastSyncStore.editIds(await store.payload(scope)).single;
    await store.saveEpisode(scope, feed, episode, seconds: 30, observedMs: 300);
    await store.apply(scope,
        reply(acked: [second], episodes: [position(20, 200)]), () => true);
    expect(
        (await (await repository.database).query('podcast_episode_state'))
            .single['position_seconds'],
        30);
    expect(await store.pending(scope), hasLength(1));
  });
  test(
      'completion and position merge independently, tombstone resists older pull',
      () async {
    final db = await repository.database;
    await db.insert('podcast_subscriptions', {
      'scope': scope,
      'feed_key': feed,
      'subscribed': 0,
      'subscription_ms': 500
    });
    await store.saveEpisode(scope, feed, episode,
        completed: true, observedMs: 200);
    await store.apply(
        scope,
        reply(episodes: [
          position(23, 300)
        ], subscriptions: [
          {'feed_key': feed, 'subscribed': true, 'subscription_ms': 400}
        ]),
        () => true);
    final row = (await db.query('podcast_episode_state')).single;
    expect(row['completed'], 1);
    expect(row['position_seconds'], 23);
    expect((await db.query('podcast_subscriptions')).single['subscribed'], 0);
  });
  test(
      'epoch recovery survives interrupted pull and repushes missing/newer registers',
      () async {
    await store.saveEpisode(scope, feed, episode, seconds: 50, observedMs: 500);
    final id = (await store.pending(scope)).single;
    await store.apply(
        scope, reply(acked: [id], episodes: [position(50, 500)]), () => true);
    await store.apply(
        scope, reply(epoch: 'restored', more: true, cursor: 2), () => true);
    await repository.close();
    expect((await store.payload(scope))['epoch'], 'restored');
    expect((await store.payload(scope))['since'], 2);
    await store.apply(
        scope,
        reply(epoch: 'restored', episodes: [position(10, 100)], cursor: 3),
        () => true);
    expect(await store.pending(scope), hasLength(1));
    expect(
        (await store.payload(scope))['episodes'][0]['position']['observed_ms'],
        500);
    expect((await store.status(scope))['epoch'], 'restored');
  });
  test(
      'scope alias collision preserves newer pending target and other account/catalog',
      () async {
    await store.saveEpisode(scope, feed, 'enc:cdn.example/old',
        seconds: 10, observedMs: 100);
    await store.saveEpisode(scope, feed, episode, seconds: 20, observedMs: 200);
    await store.saveEpisode('local', feed, 'enc:cdn.example/old',
        seconds: 30, observedMs: 300);
    final targetId = (await store.payload(scope))['episodes']
        .firstWhere((e) => e['episode_key'] == episode)['position']['edit_id'];
    await store.apply(
        scope,
        reply(aliases: [
          {
            'kind': 'episode',
            'feed_key': feed,
            'from': 'enc:cdn.example/old',
            'to': episode
          }
        ]),
        () => true);
    final rows = await (await repository.database)
        .query('podcast_episode_state', where: 'scope=?', whereArgs: [scope]);
    expect(rows, hasLength(1));
    expect(rows.single['position_seconds'], 20);
    expect(rows.single['position_pending_edit_id'], targetId);
    expect(
        (await (await repository.database).query('podcast_episode_state',
                where: 'scope=?', whereArgs: ['local']))
            .single['episode_key'],
        'enc:cdn.example/old');
  });
  test('clean alias winners replace pending target edits with fresh IDs',
      () async {
    final db = await repository.database;
    await db.update('podcast_scopes', {'epoch': 'one'},
        where: 'scope=?', whereArgs: [scope]);
    const sourceFeed = 'feed:old.example/rss';
    const sourceEpisode = 'enc:cdn.example/old';
    await db.insert('podcast_subscriptions', {
      'scope': scope,
      'feed_key': sourceFeed,
      'subscribed': 0,
      'subscription_ms': 200,
    });
    const oldSubscriptionId = 'pending-subscription-target';
    await db.insert('podcast_subscriptions', {
      'scope': scope,
      'feed_key': feed,
      'subscribed': 1,
      'subscription_ms': 100,
      'pending_edit_id': oldSubscriptionId,
    });
    await store.saveEpisode(scope, feed, episode,
        seconds: 10, completed: false, observedMs: 100);
    final oldEpisodeIds =
        (await store.pending(scope)).difference({oldSubscriptionId});
    await db.insert('podcast_episode_state', {
      'scope': scope,
      'feed_key': sourceFeed,
      'episode_key': sourceEpisode,
      'position_seconds': 20,
      'position_ms': 200,
      'completed': 1,
      'completion_source': 'auto',
      'completion_ms': 200,
    });
    await store.apply(
        scope,
        reply(aliases: [
          {'kind': 'feed', 'from': sourceFeed, 'to': feed},
          {
            'kind': 'episode',
            'feed_key': feed,
            'from': sourceEpisode,
            'to': episode,
          },
        ]),
        () => true);
    final subscription = (await db.query('podcast_subscriptions')).single;
    expect(subscription['subscribed'], 0);
    expect(subscription['subscription_ms'], 200);
    final state = (await db.query('podcast_episode_state')).single;
    expect(state['position_seconds'], 20);
    expect(state['position_ms'], 200);
    expect(state['completed'], 1);
    expect(state['completion_source'], 'auto');
    expect(state['completion_ms'], 200);
    final pending = await store.pending(scope);
    expect(pending, hasLength(3));
    expect(
        pending.intersection({...oldEpisodeIds, oldSubscriptionId}), isEmpty);
    final payload = await store.payload(scope);
    expect(PodcastSyncStore.editIds(payload), pending);
    expect(payload['subscriptions'].single['observed_ms'], 200);
    expect(payload['episodes'].single['position']['observed_ms'], 200);
    expect(payload['episodes'].single['completion']['observed_ms'], 200);
  });
  test('account switch during transaction rolls back response and cursor',
      () async {
    await store.saveEpisode(scope, feed, episode, seconds: 4, observedMs: 100);
    final old = await store.pending(scope);
    var currentChecks = 0;
    await expectLater(
        store.apply(
            scope,
            reply(acked: old.toList(), episodes: [position(4, 100)]),
            () => ++currentChecks == 1),
        throwsStateError);
    expect(currentChecks, 2);
    expect(await store.pending(scope), old);
    expect((await store.status(scope))['cursor'], 0);
  });
  test('register edit page limits keep retry IDs stable', () async {
    // Paging is exercised separately from transport-specific HTTP failures.
    for (var i = 0; i < 3; i++) {
      await store.saveEpisode(scope, feed, 'guid:$i',
          seconds: i.toDouble(), completed: false, observedMs: 100);
    }
    expect(PodcastSyncStore.editIds(await store.payload(scope, limit: 3)),
        hasLength(3));
    expect(PodcastSyncStore.editIds(await store.payload(scope, limit: 1)),
        hasLength(1));
    expect(PodcastSyncStore.editIds(await store.payload(scope, limit: 1)),
        PodcastSyncStore.editIds(await store.payload(scope, limit: 1)));
  });
  test(
      'global partial failure preserves full success timestamp and stale clock',
      () async {
    var now = 1000000;
    var fail = false;
    final service = PodcastSync(store);
    final account = PodcastSyncAccount(
        scope: scope,
        current: () => true,
        supported: () async => true,
        send: (_) async => reply());
    final coordinator = GlobalSyncCoordinator(
        service,
        () => GlobalSyncAccount(
            podcast: account,
            syncMedia: (_) async {
              if (fail) throw const SocketException('offline');
            },
            flushProgress: () async {},
            pendingOther: () async => 0),
        now: () => DateTime.fromMillisecondsSinceEpoch(now));
    expect(coordinator.state.configured, false);
    await coordinator.syncNow();
    expect(coordinator.state.lastFullSuccessMs, now);
    fail = true;
    now += const Duration(hours: 49).inMilliseconds;
    await coordinator.syncNow();
    expect(coordinator.state.lastFullSuccessMs, 1000000);
    expect(coordinator.state.staleAt(now), true);
    fail = false;
    await coordinator.syncNow();
    expect(coordinator.state.staleAt(now), false);
    coordinator.dispose();
  });
}
