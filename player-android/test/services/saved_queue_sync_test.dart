import 'dart:async';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:player_android/services/music_queue_snapshot.dart';
import 'package:player_android/services/saved_queue_sync.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  const snapshot = MusicQueueSnapshot(
      entries: [(id: 1, mediaId: 7), (id: 2, mediaId: 7)],
      index: 1,
      position: 7.25,
      label: 'Repeated',
      upNext: {},
      canonical: [1, 2],
      shuffle: true,
      shuffleMode: 'albums');
  setUp(() => SharedPreferences.setMockInitialValues({}));
  test(
      'ordered repeated occurrences, restart pending, conflict, explicit choices',
      () async {
    var server = {...portableQueue(snapshot), 'revision': 0};
    var online = false;
    var writes = 0;
    final account = SavedQueueAccount(
        scope: 'a',
        current: () => true,
        read: () async {
          if (!online) throw StateError('Offline');
          return Map.of(server);
        },
        write: (body, revision) async {
          writes++;
          if (revision != server['revision']) {
            return (conflict: true, queue: Map.of(server));
          }
          server = {...body, 'revision': revision + 1};
          return (conflict: false, queue: Map.of(server));
        });
    var sync = SavedQueueSync();
    await sync.capture(account, snapshot);
    await expectLater(sync.sync(account), throwsStateError);
    sync = SavedQueueSync();
    expect(await sync.pending(account), 1);
    online = true;
    await sync.sync(account);
    expect(server['current_id'], 2);
    expect(server['position_seconds'], 7.25);
    expect((server['entries'] as List).length, 2);
    expect(await sync.pending(account), 0);
    await sync.capture(
        account,
        const MusicQueueSnapshot(
            entries: [(id: 2, mediaId: 7)],
            index: 0,
            position: 8,
            label: 'New',
            upNext: {},
            canonical: [2]));
    server = {...server, 'revision': 2, 'position_seconds': 20.0};
    await expectLater(sync.sync(account), throwsStateError);
    expect(sync.conflict('a'), true);
    expect(server['position_seconds'], 20);
    await sync.keepOther(account);
    expect(await sync.pending(account), 0);
    await sync.replace(account, snapshot);
    await sync.sync(account);
    expect(server['position_seconds'], 7.25);
    expect(server['revision'], 3);
    expect(writes, 3);
    final decoded = portableQueueSnapshot(server);
    expect(decoded.entries.map((e) => e.id), [1, 2]);
    expect(decoded.index, 1);
    expect(decoded.position, 7.25);
    expect(decoded.shuffleMode, 'albums');
    expect(
        portableQueueSnapshot({...server}..remove('shuffle_mode')).shuffleMode,
        'tracks');
  });
  test('baseline recovery does not publish; new device keeps other session',
      () async {
    var writes = 0;
    final account = SavedQueueAccount(
        scope: 'b',
        current: () => true,
        read: () async => {...portableQueue(snapshot), 'revision': 4},
        write: (body, revision) async {
          writes++;
          return (conflict: false, queue: {...body, 'revision': revision + 1});
        });
    final sync = SavedQueueSync();
    await sync.capture(account, snapshot, baseline: true);
    await sync.sync(account);
    expect(writes, 0);
    await sync.capture(
        account,
        const MusicQueueSnapshot(
            entries: [], index: -1, position: 0, label: '', upNext: {}));
    await expectLater(sync.sync(account), throwsStateError);
    expect(writes, 0);
    expect(sync.conflict('b'), true);
  });
  test('late credential lease response never acknowledges pending edits',
      () async {
    var current = true;
    final read = Completer<Map<String, dynamic>>();
    final account = SavedQueueAccount(
        scope: 'old',
        current: () => current,
        read: () => read.future,
        write: (body, revision) async => throw StateError('Should not send'));
    final sync = SavedQueueSync();
    await sync.capture(account, snapshot);
    final running = sync.sync(account);
    await Future<void>.delayed(Duration.zero);
    current = false;
    read.complete({...portableQueue(snapshot), 'revision': 5});
    await running;
    expect(await sync.pending(account), 1);
    expect(sync.scope, 'old');
  });
  test(
      'adoption async load cannot erase a capture started after its edit stamp',
      () async {
    var server = {
      ...portableQueue(snapshot),
      'revision': 5,
      'mutation_id': 'remote'
    };
    final account = SavedQueueAccount(
        scope: 'adopt-race',
        current: () => true,
        read: () async => Map.of(server),
        write: (body, revision) async {
          expect(revision, 5);
          server = {...body, 'revision': 6};
          return (conflict: false, queue: Map.of(server));
        });
    final sync = SavedQueueSync();
    await sync.capture(account, snapshot, baseline: true);
    final stamp = sync.editStamp(account.scope);
    final adopting = sync.adopt(account, server, snapshot,
        expectedEditStamp: stamp, unchanged: true);
    const changed = MusicQueueSnapshot(
        entries: [(id: 2, mediaId: 7), (id: 1, mediaId: 7)],
        index: 0,
        position: 7.25,
        label: 'Repeated',
        upNext: {},
        canonical: [2, 1]);
    final editing = sync.capture(account, changed);
    await Future.wait([adopting, editing]);
    final restarted = SavedQueueSync();
    expect(await restarted.pending(account), 1);
    await restarted.sync(account);
    expect((server['entries'] as List).first['id'], 2);
  });
}
