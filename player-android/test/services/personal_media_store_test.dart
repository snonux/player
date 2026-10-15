import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';
import 'package:player_android/models/offline_media.dart';
import 'package:player_android/services/local_library_repository.dart';
import 'package:player_android/services/offline_download_repository.dart';

void main() {
  setUpAll(() {
    sqfliteFfiInit();
    databaseFactory = databaseFactoryFfi;
  });

  test(
      'device annotations persist, retain identity on relink and cascade on removal',
      () async {
    final dir = await Directory.systemTemp.createTemp('personal-local-');
    final path = '${dir.path}/local.db';
    var database = LocalLibraryDatabase(databasePath: path);
    try {
      final media = SqliteLocalMediaRepository(database);
      final item = await media.importDocument(
          uri: 'content://first',
          title: 'First',
          mimeType: 'audio/mpeg',
          durationMs: 120000);
      final store = database.personalStore(item.id);
      await store.saveNote('Remember this');
      final bookmark = await store.addBookmark(70.5, '  Chapter two  ');
      await store.addBookmark(10, 'Beginning');
      expect((await store.listBookmarks()).map((b) => b.positionSeconds),
          [10, 70.5]);
      expect(bookmark.label, 'Chapter two');
      await store.toggleFavorite();
      await media.relink(
          id: item.id,
          uri: 'content://replacement',
          title: 'Replacement',
          mimeType: 'audio/mpeg',
          durationMs: 120000,
          retainProgress: false);
      await database.close();
      database = LocalLibraryDatabase(databasePath: path);
      final reopened = database.personalStore(item.id);
      expect((await reopened.getNote())?.content, 'Remember this');
      expect(await reopened.getFavorite(), isTrue);
      expect((await reopened.listBookmarks()).length, 2);
      await reopened.deleteBookmark(bookmark.id);
      expect((await reopened.listBookmarks()).single.positionSeconds, 10);
      await reopened.deleteNote();
      expect(await reopened.getNote(), isNull);
      await SqliteLocalMediaRepository(database).remove(item.id);
      await expectLater(reopened.saveNote('stale'), throwsStateError);
      await expectLater(reopened.addBookmark(5, 'stale'), throwsStateError);
      await expectLater(reopened.toggleFavorite(), throwsStateError);
      final db = await database.database;
      expect(await db.query('personal_media'), isEmpty);
      expect(await db.query('personal_bookmarks'), isEmpty);
    } finally {
      await database.close();
      await dir.delete(recursive: true);
    }
  });

  test('device v4 migration retains media and resume progress', () async {
    final dir = await Directory.systemTemp.createTemp('personal-migration-');
    final path = '${dir.path}/local.db';
    final old = await openDatabase(path, version: 4, onCreate: (db, _) async {
      await db.execute(
          'CREATE TABLE local_media (id INTEGER PRIMARY KEY AUTOINCREMENT, uri TEXT NOT NULL UNIQUE, title TEXT NOT NULL, mime_type TEXT NOT NULL, size_bytes INTEGER, duration_ms INTEGER, added_at INTEGER NOT NULL, last_opened_at INTEGER, owns_read_grant INTEGER NOT NULL DEFAULT 0)');
      await db.execute(
          'CREATE TABLE local_progress (local_media_id INTEGER PRIMARY KEY, position_seconds REAL NOT NULL, finished INTEGER NOT NULL DEFAULT 0, updated_at INTEGER NOT NULL, FOREIGN KEY (local_media_id) REFERENCES local_media(id) ON DELETE CASCADE)');
      await db.execute(
          'CREATE TABLE local_grant_cleanup (uri TEXT PRIMARY KEY, scheduled_at INTEGER NOT NULL)');
      await db.insert('local_media', {
        'id': 7,
        'uri': 'content://original',
        'title': 'Original',
        'mime_type': 'audio/mpeg',
        'added_at': 1
      });
      await db.insert('local_progress', {
        'local_media_id': 7,
        'position_seconds': 23.5,
        'finished': 0,
        'updated_at': 1
      });
    });
    await old.close();
    final database = LocalLibraryDatabase(databasePath: path);
    try {
      expect(
          (await SqliteLocalProgressRepository(database).get(7))
              ?.positionSeconds,
          23.5);
      expect((await SqliteLocalMediaRepository(database).getById(7))?.uri,
          'content://original');
      await database.personalStore(7).saveNote('Migrated');
      expect((await database.personalStore(7).getNote())?.content, 'Migrated');
    } finally {
      await database.close();
      await dir.delete(recursive: true);
    }
  });

  test(
      'invalid positions and labels reject without affecting progress or another item',
      () async {
    final database = LocalLibraryDatabase(databasePath: inMemoryDatabasePath);
    try {
      final media = SqliteLocalMediaRepository(database);
      final one = await media.importDocument(
          uri: 'content://one',
          title: 'One',
          mimeType: 'audio/mpeg',
          durationMs: 60000);
      final two = await media.importDocument(
          uri: 'content://two', title: 'Two', mimeType: 'video/mp4');
      final store = database.personalStore(one.id);
      for (final seconds in [double.nan, double.infinity, -1.0]) {
        expect(() => store.addBookmark(seconds, ''), throwsArgumentError);
      }
      await expectLater(store.addBookmark(61, ''), throwsArgumentError);
      expect(() => store.addBookmark(5, '🎵' * 201), throwsArgumentError);
      await store.addBookmark(5, '🎵' * 200);
      final otherBookmark =
          await database.personalStore(two.id).addBookmark(4, 'Other');
      await store.deleteBookmark(otherBookmark.id);
      expect(
          (await database.personalStore(two.id).listBookmarks()).single.label,
          'Other');
      expect(await SqliteLocalProgressRepository(database).get(one.id), isNull);
      // Removal wins over a later queued editor callback.
      final removal = media.remove(one.id);
      final delayedSave = store.saveNote('Too late');
      await removal;
      await expectLater(delayedSave, throwsStateError);
      expect(await (await database.database).query('personal_media'), isEmpty);
    } finally {
      await database.close();
    }
  });

  test(
      'offline v1 migration preserves bytes, progress and origin/account isolation',
      () async {
    final dir = await Directory.systemTemp.createTemp('personal-offline-');
    final first = OfflineMedia(
        key: offlineMediaKey('https://one.test', 1, 5),
        origin: 'https://one.test',
        userId: 1,
        mediaId: 5,
        title: 'One',
        type: 'audio',
        sizeBytes: 3,
        addedAt: DateTime.utc(2026));
    final old = await openDatabase('${dir.path}/index.db', version: 1,
        onCreate: (db, _) async {
      await db.execute(
          'CREATE TABLE downloads (key TEXT PRIMARY KEY, origin TEXT NOT NULL, user_id INTEGER NOT NULL, media_id INTEGER NOT NULL, title TEXT NOT NULL, type TEXT NOT NULL, size_bytes INTEGER NOT NULL, added_at INTEGER NOT NULL, position_seconds REAL NOT NULL DEFAULT 0, finished INTEGER NOT NULL DEFAULT 0)');
      await db.insert('downloads', {
        'key': first.key,
        'origin': first.origin,
        'user_id': 1,
        'media_id': 5,
        'title': 'One',
        'type': 'audio',
        'size_bytes': 3,
        'added_at': first.addedAt.millisecondsSinceEpoch,
        'position_seconds': 19.5,
        'finished': 0
      });
    });
    await old.close();
    await File('${dir.path}/${first.key}.media').writeAsBytes([1, 2, 3]);
    var repo = OfflineDownloadRepository(directory: dir);
    try {
      expect((await repo.get(first.key))?.positionSeconds, 19.5);
      final second = OfflineMedia(
          key: offlineMediaKey('https://one.test', 2, 5),
          origin: 'https://one.test',
          userId: 2,
          mediaId: 5,
          title: 'Two',
          type: 'image',
          sizeBytes: 3,
          addedAt: DateTime.utc(2026));
      final third = OfflineMedia(
          key: offlineMediaKey('https://two.test', 1, 5),
          origin: 'https://two.test',
          userId: 1,
          mediaId: 5,
          title: 'Three',
          type: 'video',
          sizeBytes: 3,
          addedAt: DateTime.utc(2026));
      await repo.add(second);
      await repo.add(third);
      final store = repo.personalStore(first.key);
      await store.saveNote('Account one');
      await store.addBookmark(10.5, 'Offline');
      final image = repo.personalStore(second.key);
      await image.saveNote('Picture note');
      expect(await image.toggleFavorite(), isTrue);
      await expectLater(image.addBookmark(0, 'Wrong kind'), throwsStateError);
      expect(await repo.personalStore(third.key).getNote(), isNull);
      await repo.close();
      repo = OfflineDownloadRepository(directory: dir);
      expect((await repo.personalStore(first.key).getNote())?.content,
          'Account one');
      expect(
          (await repo.personalStore(first.key).listBookmarks())
              .single
              .positionSeconds,
          10.5);
      expect(await repo.personalStore(second.key).getFavorite(), isTrue);
      expect((await repo.personalStore(second.key).getNote())?.content,
          'Picture note');
      expect(await repo.file(first.key).readAsBytes(), [1, 2, 3]);
      final stale = repo.personalStore(first.key);
      await repo.remove(first.key);
      await expectLater(stale.saveNote('Gone'), throwsStateError);
      expect(await repo.file(first.key).exists(), isFalse);
      await repo.add(first);
      await expectLater(stale.saveNote('Must not recreate after redownload'),
          throwsStateError);
      expect(await repo.personalStore(first.key).getNote(), isNull);
      expect((await repo.personalStore(second.key).getNote())?.content,
          'Picture note');
      expect(await (await repo.database).query('personal_bookmarks'), isEmpty);
    } finally {
      await repo.close();
      await dir.delete(recursive: true);
    }
  });
}
