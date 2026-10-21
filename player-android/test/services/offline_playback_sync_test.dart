import 'dart:async';
import 'dart:io';

import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';
import 'package:player_android/models/offline_media.dart';
import 'package:player_android/services/offline_download_repository.dart';
import 'package:player_android/services/offline_playback_sync.dart';

class _Connectivity implements Connectivity {
  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
  @override
  Stream<List<ConnectivityResult>> get onConnectivityChanged =>
      const Stream.empty();
}

OfflineMedia item(
        {String origin = 'https://one.test', int user = 1, int media = 5}) =>
    OfflineMedia(
        key: offlineMediaKey(origin, user, media),
        origin: origin,
        userId: user,
        mediaId: media,
        title: 'Audio',
        type: 'audio',
        sizeBytes: 3,
        addedAt: DateTime.utc(2026));

void main() {
  setUpAll(() {
    sqfliteFfiInit();
    databaseFactory = databaseFactoryFfi;
  });
  late Directory dir;
  late OfflineDownloadRepository repo;
  DateTime now = DateTime.utc(2026, 10, 15, 12);
  setUp(() async {
    dir = await Directory.systemTemp.createTemp('playback-sync-');
    repo = OfflineDownloadRepository(directory: dir, now: () => now);
  });
  tearDown(() async {
    await repo.close();
    await dir.delete(recursive: true);
  });

  test(
      'offline edits and tombstones survive reopen and copy removal, and keep edit timestamps',
      () async {
    final media = item();
    await repo.add(media);
    await repo.saveProgress(media.key, 37, finished: true);
    final store = repo.personalStore(media.key);
    final bookmark = await store.addBookmark(12, 'chapter');
    now = now.add(const Duration(seconds: 1));
    await store.deleteBookmark(bookmark.id);
    final payload = await repo.syncPayload(media.key);
    expect((payload['progress'] as Map)['observed_ms'],
        now.subtract(const Duration(seconds: 1)).millisecondsSinceEpoch);
    final edits = payload['bookmarks'] as List;
    expect(edits.single['deleted'], isTrue);
    expect(edits.single['observed_ms'], now.millisecondsSinceEpoch);
    await repo.remove(media.key);
    await repo.close();
    repo = OfflineDownloadRepository(directory: dir, now: () => now);
    expect(await repo.syncPayload(media.key), payload);
    await repo.add(media);
    expect((await repo.get(media.key))?.finished, isTrue);
    expect(await repo.personalStore(media.key).listBookmarks(), isEmpty);
  });

  test(
      'response cannot acknowledge or overwrite a newer edit made while request is in flight',
      () async {
    final media = item();
    await repo.add(media);
    await repo.saveProgress(media.key, 10);
    final old = await repo.syncPayload(media.key);
    final bookmark =
        await repo.personalStore(media.key).addBookmark(2, 'first');
    final oldBookmarks = (await repo.syncPayload(media.key))['bookmarks'];
    now = now.add(const Duration(seconds: 1));
    await repo.saveProgress(media.key, 20);
    await repo.personalStore(media.key).deleteBookmark(bookmark.id);
    await repo.mergeSync(media.key,
        {'progress': old['progress'], 'bookmarks': oldBookmarks}, () => true);
    expect((await repo.get(media.key))?.positionSeconds, 20);
    final pending = await repo.syncPayload(media.key);
    expect((pending['progress'] as Map)['position_seconds'], 20);
    expect((pending['bookmarks'] as List).single['deleted'], isTrue);
    await repo.mergeSync(media.key, pending, () => true);
    expect((await repo.syncPayload(media.key))['progress'], isNull);
    expect((await repo.syncPayload(media.key))['bookmarks'], isEmpty);
  });

  test(
      'retry preserves payload, isolates origin/account, and ignores a response after locking',
      () async {
    final one = item(),
        otherUser = item(user: 2),
        otherOrigin = item(origin: 'https://two.test');
    for (final media in [one, otherUser, otherOrigin]) {
      await repo.add(media);
      await repo.saveProgress(media.key, 10);
    }
    var enabled = false;
    final calls = <Map<String, dynamic>>[];
    var failing = true;
    Completer<Map<String, dynamic>>? blocked;
    final sync = OfflinePlaybackSync(
        repo,
        () => enabled
            ? OfflineSyncAccount(
                origin: one.origin,
                userId: one.userId,
                current: () => enabled,
                send: (id, payload) async {
                  expect(id, one.mediaId);
                  calls.add(payload);
                  if (failing) throw const SocketException('offline');
                  if (blocked != null) return blocked.future;
                  return payload;
                })
            : null,
        interval: const Duration(days: 1),
        connectivity: _Connectivity());
    try {
      await sync.sync();
      expect(calls, isEmpty);
      enabled = true;
      await sync.sync();
      expect(calls.length, 1);
      final original = calls.single;
      now = now.add(const Duration(days: 1));
      failing = false;
      blocked = Completer<Map<String, dynamic>>();
      final running = sync.sync();
      while (calls.length < 2) {
        await Future<void>.delayed(Duration.zero);
      }
      enabled = false;
      blocked.complete(original);
      await running;
      expect(await repo.syncPayload(one.key), original);
      expect(calls.last, original);
      enabled = true;
      blocked = null;
      await sync.sync();
      expect((await repo.syncPayload(one.key))['progress'], isNull);
      expect((await repo.syncPayload(otherUser.key))['progress'], isNotNull);
      expect((await repo.syncPayload(otherOrigin.key))['progress'], isNotNull);
    } finally {
      await sync.dispose();
    }
  });
  test(
      'large outboxes drain in bounded batches and removed acknowledged state is pruned',
      () async {
    final media = item();
    await repo.add(media);
    final db = await repo.database;
    await db.transaction((tx) async {
      final batch = tx.batch();
      for (var i = 0; i < 1001; i++) {
        batch.insert('sync_bookmarks', {
          'owner_id': media.key,
          'sync_key': 'key-$i',
          'position_seconds': i.toDouble(),
          'label': 'bookmark',
          'observed_ms': now.millisecondsSinceEpoch,
          'pending': 1
        });
      }
      await batch.commit(noResult: true);
    });
    await repo.remove(media.key);
    expect(await repo.syncItems(media.origin, media.userId), hasLength(1));
    final first = await repo.syncPayload(media.key);
    expect(first['bookmarks'], hasLength(1000));
    await repo.mergeSync(media.key, first, () => true);
    final last = await repo.syncPayload(media.key);
    expect(last['bookmarks'], hasLength(1));
    await repo.mergeSync(media.key, last, () => true);
    expect(await repo.syncItems(media.origin, media.userId), isEmpty);
    expect(await db.query('sync_bookmarks'), isEmpty);
  });

  test(
      'foreground selected item refresh bypasses unrelated blocked background request',
      () async {
    final slow = item(), selected = item(media: 6);
    await repo.add(slow);
    await repo.add(selected);
    final started = Completer<void>(), release = Completer<void>();
    final sync = OfflinePlaybackSync(
        repo,
        () => OfflineSyncAccount(
            origin: slow.origin,
            userId: slow.userId,
            current: () => true,
            send: (id, payload) async {
              if (id == slow.mediaId) {
                if (!started.isCompleted) started.complete();
                await release.future;
              }
              return {
                'progress': {
                  'position_seconds': id == selected.mediaId ? 88.0 : 12.0,
                  'finished': false,
                  'observed_ms': now.millisecondsSinceEpoch
                },
                'bookmarks': <Object>[]
              };
            }),
        interval: const Duration(days: 1),
        connectivity: _Connectivity());
    try {
      await started.future;
      await sync.syncKey(selected.key);
      expect((await repo.get(selected.key))?.positionSeconds, 88);
    } finally {
      release.complete();
      await sync.dispose();
    }
  });

  test('stale bookmark editor cannot mutate a replaced download instance',
      () async {
    final media = item();
    await repo.add(media);
    final old = repo.personalStore(media.key);
    final bookmark = await old.addBookmark(3, 'original');
    final removal = repo.remove(media.key);
    final replacement = repo.add(media);
    final lateAdd = old.addBookmark(4, 'stale');
    await removal;
    await replacement;
    await expectLater(lateAdd, throwsStateError);
    await expectLater(old.deleteBookmark(bookmark.id), throwsStateError);
    expect((await repo.personalStore(media.key).listBookmarks()).single.label,
        'original');
  });
  test(
      'v2 migration preserves legacy progress and queues bookmarks with their original creation time',
      () async {
    final media = item();
    await repo.add(media);
    final db = await repo.database;
    await db.update('downloads', {'position_seconds': 19.5},
        where: 'key=?', whereArgs: [media.key]);
    await db.insert('personal_bookmarks', {
      'owner_id': media.key,
      'position_seconds': 7.5,
      'label': 'legacy',
      'created_at': now.millisecondsSinceEpoch
    });
    await db.execute('DROP TABLE sync_bookmarks');
    await db.execute('DROP TABLE sync_items');
    // Model the actual v2 columns, before embedded music/source snapshots.
    await db.execute('ALTER TABLE downloads DROP COLUMN music');
    await db.execute('ALTER TABLE downloads DROP COLUMN source_name');
    await db.setVersion(2);
    await repo.close();
    repo = OfflineDownloadRepository(
        directory: dir, now: () => now.add(const Duration(days: 1)));
    final migrated = await repo.get(media.key);
    expect(migrated?.positionSeconds, 19.5);
    expect(migrated?.music, isNull);
    expect(migrated?.sourceName, isEmpty);
    final payload = await repo.syncPayload(media.key);
    expect(payload['progress'], isNull);
    final bookmark = (payload['bookmarks'] as List).single as Map;
    expect(bookmark['label'], 'legacy');
    expect(bookmark['position_seconds'], 7.5);
    expect(bookmark['observed_ms'], now.millisecondsSinceEpoch);
    expect((await repo.personalStore(media.key).listBookmarks()).single.label,
        'legacy');
    expect(await (await repo.database).query('personal_bookmarks'), isEmpty);
  });
}
