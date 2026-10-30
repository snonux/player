import 'dart:io';
import 'dart:async';
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
      'opening podcasts reconciles all 23 subscriptions without changing cursor or pending edits',
      () async {
    final db = await repository.database;
    await db.update('podcast_scopes', {'epoch': 'saved', 'cursor': 777},
        where: 'scope=?', whereArgs: [scope]);
    final rows = List.generate(
        23,
        (i) => <String, dynamic>{
              'feed_key': 'feed:example.com/$i',
              'title': 'Podcast $i',
              'fetch_url': 'https://example.com/$i',
              'urls': <String>[],
              'subscribed': true,
              'subscription_ms': 10,
            });
    await store.apply(
        scope, reply(subscriptions: rows.take(2).toList()), () => true,
        scoped: true);
    var calls = 0;
    final account = PodcastSyncAccount(
        scope: scope,
        current: () => true,
        supported: () async => true,
        send: (request) async {
          expect(request['subscriptions'], isEmpty);
          expect(request['episodes'], isEmpty);
          calls++;
          return calls.isOdd
              ? reply(
                  subscriptions: rows.take(12).toList(), more: true, cursor: 12)
              : reply(subscriptions: rows.skip(12).toList(), cursor: 23);
        });
    expect(
        (await PodcastSync(store).refreshSubscriptions(account)).full, isTrue);
    expect((await repository.subscriptions(scope)).length, 23);
    expect((await store.status(scope))['cursor'], 777);
    expect((await store.status(scope))['epoch'], 'saved');
    await db.update(
        'podcast_subscriptions',
        {
          'subscribed': 0,
          'subscription_ms': 20,
          'pending_edit_id': 'keep-local'
        },
        where: 'scope=? AND feed_key=?',
        whereArgs: [scope, rows.first['feed_key']]);
    expect(
        (await PodcastSync(store).refreshSubscriptions(account)).full, isTrue);
    final pending = (await db.query('podcast_subscriptions',
            where: 'scope=? AND feed_key=?',
            whereArgs: [scope, rows.first['feed_key']]))
        .single;
    expect(pending['subscribed'], 0);
    expect(pending['pending_edit_id'], 'keep-local');
    expect((await repository.subscriptions(scope)).length, 22);
  });

  test(
      'subscription reconciliation rejects a late reply after leaving the account',
      () async {
    var current = true;
    final result =
        await PodcastSync(store).refreshSubscriptions(PodcastSyncAccount(
            scope: scope,
            current: () => current,
            supported: () async => true,
            send: (_) async {
              current = false;
              return reply(subscriptions: [
                {'feed_key': feed, 'subscribed': true, 'subscription_ms': 10}
              ]);
            }));
    expect(result.full, isFalse);
    expect(result.errorKind, 'auth');
    expect(await repository.subscriptions(scope), isEmpty);
  });

  test(
      'rejected deletion keeps its exact edit for a bounded later retry and acknowledgement',
      () async {
    await store.saveEpisode(scope, feed, episode,
        deleted: true, observedMs: 100);
    final id = PodcastSyncStore.editIds(await store.payload(scope)).single;
    var reject = true;
    var calls = 0;
    final account = PodcastSyncAccount(
        scope: scope,
        current: () => true,
        supported: () async => true,
        send: (request) async {
          calls++;
          expect(PodcastSyncStore.editIds(request), {id});
          return {
            ...reply(acked: reject ? [] : [id]),
            if (reject)
              'rejected': [
                {'edit_id': id, 'reason': 'quota'}
              ]
          };
        });
    final failed = await PodcastSync(store).sync(account);
    expect(failed.full, false);
    expect(failed.errorKind, 'rejected');
    expect(calls, 1);
    await repository.close();
    expect(await store.pending(scope), {id});
    expect(
        (await (await repository.database).query('podcast_episode_state'))
            .single['deleted'],
        1);
    expect(
        (await (await repository.database).query('podcast_sync_rejections'))
            .single['reason'],
        'quota');
    reject = false;
    expect((await PodcastSync(store).sync(account)).full, true);
    expect(calls, 2);
    expect(await store.pending(scope), isEmpty);
  });

  test(
      'deletion wins positive clock ties on pulls and alias collisions; newer false restores',
      () async {
    Future<bool> pull(bool deleted, int clock) => store.apply(
        scope,
        reply(episodes: [
          {
            'feed_key': feed,
            'episode_key': episode,
            'deletion': {'deleted': deleted, 'observed_ms': clock},
          }
        ]),
        () => true);
    Future<Object?> deleted() async =>
        (await (await repository.database).query('podcast_episode_state',
                where: 'episode_key=?', whereArgs: [episode]))
            .single['deleted'];
    await pull(false, 100);
    await pull(true, 100);
    expect(await deleted(), 1);
    await pull(false, 100);
    expect(await deleted(), 1);
    await pull(false, 101);
    expect(await deleted(), 0);
    await store.saveEpisode(scope, feed, 'guid:alias',
        deleted: true, observedMs: 101);
    await store.apply(
        scope,
        reply(aliases: [
          {
            'kind': 'episode',
            'feed_key': feed,
            'from': 'guid:alias',
            'to': episode,
          }
        ]),
        () => true);
    expect(await deleted(), 1);
  });

  test(
      'deletion persists independently, ignores stale pulls and recovers across epochs',
      () async {
    await store.saveEpisode(scope, feed, episode,
        seconds: 42, completed: true, observedMs: 10);
    await store.saveEpisode(scope, feed, episode,
        deleted: true, observedMs: 100);
    final payload = await store.payload(scope);
    final deletionId = ((payload['episodes'] as List).single['deletion']
        as Map)['edit_id'] as String;
    await repository.close();
    expect((await store.payload(scope))['episodes'], payload['episodes']);
    await store.apply(
        scope,
        reply(episodes: [
          {
            'feed_key': feed,
            'episode_key': episode,
            'deletion': {'deleted': false, 'observed_ms': 50},
            'position': {'seconds': 9, 'observed_ms': 200},
          }
        ]),
        () => true);
    var row = (await (await repository.database).query('podcast_episode_state'))
        .single;
    expect(row['deleted'], 1);
    expect(row['position_seconds'], 42);
    await store.apply(
        scope,
        reply(acked: PodcastSyncStore.editIds(payload).toList(), episodes: [
          {
            'feed_key': feed,
            'episode_key': episode,
            'deletion': {'deleted': true, 'observed_ms': 90},
          }
        ]),
        () => true);
    row = (await (await repository.database).query('podcast_episode_state'))
        .single;
    expect(row['deletion_ms'], 90);
    expect(row['deletion_pending_edit_id'], isNull);
    expect(await store.pending(scope), isNot(contains(deletionId)));
    await store.apply(scope, reply(epoch: 'restored', cursor: 0), () => true);
    final recovered = (await store.payload(scope))['episodes'] as List;
    expect(recovered.single['deletion']['deleted'], true);
    expect(recovered.single['deletion']['observed_ms'], 90);
  });

  test(
      'older servers hold deletion without a resend loop and later send it when supported',
      () async {
    await store.saveEpisode(scope, feed, episode,
        deleted: true, observedMs: 100);
    var calls = 0;
    var supported = false;
    final account = PodcastSyncAccount(
        scope: scope,
        current: () => true,
        supported: () async => true,
        supportsDeletion: () async => supported,
        send: (request) async {
          calls++;
          if (!supported) expect(request['episodes'], isEmpty);
          return reply(acked: PodcastSyncStore.editIds(request).toList());
        });
    expect((await PodcastSync(store).sync(account)).full, true);
    expect(calls, 1);
    expect(await store.pending(scope), hasLength(1));
    supported = true;
    expect((await PodcastSync(store).sync(account)).full, true);
    expect(await store.pending(scope), isEmpty);
  });

  test('deletion follows episode aliases without changing other registers',
      () async {
    await store.saveEpisode(scope, feed, episode,
        deleted: true, observedMs: 100);
    await store.saveEpisode(scope, feed, 'guid:canonical',
        seconds: 77, observedMs: 200);
    await store.apply(
        scope,
        reply(aliases: [
          {
            'kind': 'episode',
            'feed_key': feed,
            'from': episode,
            'to': 'guid:canonical',
          }
        ]),
        () => true);
    final row =
        (await (await repository.database).query('podcast_episode_state'))
            .single;
    expect(row['episode_key'], 'guid:canonical');
    expect(row['deleted'], 1);
    expect(row['position_seconds'], 77);
    expect((await store.payload(scope))['episodes'], hasLength(1));
  });

  test(
      'atomic end uses one clock newer than both independently edited registers',
      () async {
    await store.saveEpisode(scope, feed, episode,
        seconds: 99, observedMs: 9000);
    await store.saveEpisode(scope, feed, episode,
        completed: false, observedMs: 12000);
    await store.saveEpisode(scope, feed, episode,
        seconds: 100, completed: true, source: 'auto', observedMs: 1000);
    final row =
        (await (await repository.database).query('podcast_episode_state'))
            .single;
    expect(row['position_ms'], 12001);
    expect(row['completion_ms'], 12001);
    expect(row['position_seconds'], 100);
    expect(row['completed'], 1);
    expect(await store.pending(scope), hasLength(2));
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
  test('only manual sync requests activate transient footer feedback',
      () async {
    var entered = Completer<void>();
    var release = Completer<void>();
    var fail = false;
    final coordinator = GlobalSyncCoordinator(
        PodcastSync(store),
        () => GlobalSyncAccount(
            podcast: PodcastSyncAccount(
                scope: scope,
                current: () => true,
                supported: () async => true,
                send: (_) async => reply()),
            syncMedia: (_) async {
              entered.complete();
              await release.future;
              if (fail) throw const SocketException('offline');
            },
            flushProgress: () async {},
            pendingOther: () async => 0));
    addTearDown(coordinator.dispose);
    final automatic = coordinator.syncNow(explicit: false);
    await entered.future;
    expect(coordinator.state.active, isTrue);
    expect(coordinator.manualSyncActive, isFalse);
    expect(coordinator.manualSyncRevision, 0);
    release.complete();
    await automatic;
    expect(coordinator.state.lastFullSuccessMs, isNotNull);
    expect(coordinator.manualSyncActive, isFalse);
    for (final failure in [false, true]) {
      fail = failure;
      entered = Completer<void>();
      release = Completer<void>();
      final manual = coordinator.syncNow();
      await entered.future;
      expect(coordinator.state.active, isTrue);
      expect(coordinator.manualSyncActive, isTrue);
      expect(coordinator.manualSyncRevision, failure ? 2 : 1);
      release.complete();
      await manual;
      expect(coordinator.manualSyncActive, isFalse);
      expect(coordinator.state.errorKind, failure ? 'offline' : isNull);
    }
  });
  for (final queuedManual in [false, true]) {
    test(
        'manual outcome survives queued ${queuedManual ? 'manual' : 'automatic'} sync',
        () async {
      final entered = [Completer<void>(), Completer<void>()];
      final release = [Completer<void>(), Completer<void>()];
      var runs = 0;
      final explicitRuns = <bool>[];
      final coordinator = GlobalSyncCoordinator(
          PodcastSync(store),
          () => GlobalSyncAccount(
              podcast: PodcastSyncAccount(
                  scope: scope,
                  current: () => true,
                  supported: () async => true,
                  send: (_) async => reply()),
              syncMedia: (explicit) async {
                final run = runs++;
                explicitRuns.add(explicit);
                entered[run].complete();
                await release[run].future;
                if (explicit) throw const SocketException('offline');
              },
              flushProgress: () async {},
              pendingOther: () async => 0));
      addTearDown(coordinator.dispose);
      final first = coordinator.syncNow(explicit: !queuedManual);
      await entered[0].future;
      coordinator.trigger(explicit: queuedManual);
      release[0].complete();
      await first;
      await entered[1].future;
      expect(coordinator.state.active, isTrue);
      expect(coordinator.manualSyncActive, queuedManual);
      if (queuedManual) {
        expect(coordinator.manualSyncResult, isNull,
            reason: 'the queued explicit attempt has not completed');
      } else {
        expect(coordinator.manualSyncResult?.active, isFalse);
        expect(coordinator.manualSyncResult?.errorKind, 'offline');
      }
      final finished = Completer<void>();
      coordinator.addListener(() {
        if (!coordinator.state.active &&
            !coordinator.manualSyncActive &&
            !finished.isCompleted) {
          finished.complete();
        }
      });
      release[1].complete();
      await finished.future;
      expect(explicitRuns, queuedManual ? [false, true] : [true, false]);
      expect(coordinator.manualSyncResult?.active, isFalse);
      expect(coordinator.manualSyncResult?.errorKind, 'offline');
      if (!queuedManual) {
        expect(coordinator.state.errorKind, isNull,
            reason: 'automatic success must not replace the manual failure');
      }
    });
  }
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
  test(
      'global account refresh drops prior status before loading, late run cannot stamp it',
      () async {
    const other = 'account:https://other.example#2';
    await store.status(other);
    final db = await store.database;
    await db.update('podcast_scopes', {'last_full_success_ms': 123},
        where: 'scope=?', whereArgs: [other]);
    var selected = scope;
    final held = Completer<Map<String, dynamic>>();
    final entered = Completer<void>();
    GlobalSyncAccount capture() {
      final owner = selected;
      return GlobalSyncAccount(
          podcast: PodcastSyncAccount(
              scope: owner,
              current: () => owner == selected,
              supported: () async => true,
              send: (_) async {
                entered.complete();
                return held.future;
              }),
          syncMedia: (_) async {},
          flushProgress: () async {},
          pendingOther: () async => owner == other ? 4 : 0);
    }

    final coordinator = GlobalSyncCoordinator(PodcastSync(store), capture);
    final running = coordinator.syncNow();
    await entered.future;
    expect(coordinator.manualSyncActive, isTrue);
    selected = other;
    final refreshing = coordinator.refreshStatus();
    expect(coordinator.state.scope, other);
    expect(coordinator.manualSyncActive, isFalse);
    expect(coordinator.state.lastFullSuccessMs, isNull);
    await refreshing;
    expect(coordinator.state.lastFullSuccessMs, 123);
    expect(coordinator.state.pending, 4);
    held.complete(reply());
    await running;
    expect(coordinator.state.scope, other);
    expect(coordinator.state.lastFullSuccessMs, 123);
    expect((await store.status(scope))['last_full_success_ms'], isNull);
    coordinator.dispose();
  });
  test('late status read cannot erase a newly committed full success',
      () async {
    final mediaEntered = Completer<void>();
    final releaseMedia = Completer<void>();
    final refreshEntered = Completer<void>();
    final releaseRefresh = Completer<int>();
    var pendingCalls = 0;
    final coordinator = GlobalSyncCoordinator(
        PodcastSync(store),
        () => GlobalSyncAccount(
            podcast: PodcastSyncAccount(
                scope: scope,
                current: () => true,
                supported: () async => true,
                send: (_) async => reply()),
            syncMedia: (_) async {
              mediaEntered.complete();
              await releaseMedia.future;
            },
            flushProgress: () async {},
            pendingOther: () {
              pendingCalls++;
              if (pendingCalls == 2) {
                refreshEntered.complete();
                return releaseRefresh.future;
              }
              return Future.value(0);
            }),
        now: () => DateTime.fromMillisecondsSinceEpoch(9000));
    final running = coordinator.syncNow();
    await mediaEntered.future;
    final refreshing = coordinator.refreshStatus();
    await refreshEntered.future;
    releaseMedia.complete();
    await running;
    expect((await store.status(scope))['last_full_success_ms'], 9000);
    expect(coordinator.state.lastFullSuccessMs, 9000);
    releaseRefresh.complete(0);
    await refreshing;
    expect(coordinator.state.lastFullSuccessMs, 9000);
    coordinator.dispose();
  });
  test(
      'global queue failure and new edits during success keep truthful pending',
      () async {
    var pending = 1, fail = true;
    final coordinator = GlobalSyncCoordinator(
        PodcastSync(store),
        () => GlobalSyncAccount(
            podcast: PodcastSyncAccount(
                scope: scope,
                current: () => true,
                supported: () async => true,
                send: (_) async => reply()),
            syncMedia: (_) async {},
            flushProgress: () async {},
            pendingOther: () async => pending,
            syncQueue: () async {
              if (fail) throw const SyncDeferred('policy');
              pending = 2;
            }),
        now: () => DateTime.fromMillisecondsSinceEpoch(9000));
    await coordinator.syncNow();
    expect(coordinator.state.lastFullSuccessMs, isNull);
    expect(coordinator.state.errorKind, 'policy');
    expect(coordinator.state.pending, 1);
    fail = false;
    await coordinator.syncNow();
    expect(coordinator.state.lastFullSuccessMs, 9000);
    expect(coordinator.state.pending, 2);
    expect(coordinator.state.errorKind, isNull);
    coordinator.dispose();
  });
}
