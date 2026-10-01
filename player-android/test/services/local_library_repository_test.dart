import 'dart:async';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';

import 'package:player_android/api/dio_client.dart';
import 'package:player_android/models/local_media.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/local_library_provider.dart';
import 'package:player_android/services/local_library_repository.dart';

class _NoTokenStorage implements TokenStorage {
  @override
  Future<String?> readToken() async => null;

  @override
  Future<void> writeToken(String value) async {}

  @override
  Future<void> deleteToken() async {}
}

void main() {
  setUpAll(() {
    sqfliteFfiInit();
    databaseFactory = databaseFactoryFfi;
  });

  test('local records and playback state persist after reopening SQLite',
      () async {
    final directory = await Directory.systemTemp.createTemp('local-library-');
    final path = '${directory.path}/library.db';
    try {
      final first = LocalLibraryDatabase(databasePath: path);
      final media = SqliteLocalMediaRepository(first);
      final progress = SqliteLocalProgressRepository(first);
      final added = await media.importDocument(
        uri: 'content://documents/audio/one',
        title: 'Track one',
        mimeType: 'audio/mpeg',
        sizeBytes: 1234,
      );
      await media.markOpened(added.id, at: DateTime.utc(2026, 10, 1, 12));
      await progress.savePosition(added.id, 32.5);
      await first.close();

      final reopened = LocalLibraryDatabase(databasePath: path);
      final loadedMedia =
          await SqliteLocalMediaRepository(reopened).getById(added.id);
      final loadedProgress =
          await SqliteLocalProgressRepository(reopened).get(added.id);
      expect(loadedMedia, isA<LocalMedia>());
      expect(loadedMedia!.uri, 'content://documents/audio/one');
      expect(loadedMedia.lastOpenedAt, DateTime.utc(2026, 10, 1, 12));
      expect(loadedMedia.durationMs, isNull);
      expect(loadedProgress!.positionSeconds, 32.5);
      expect(loadedProgress.finished, isFalse);
      await reopened.close();
    } finally {
      await directory.delete(recursive: true);
    }
  });

  test('imports deduplicate exact URIs but preserve distinct opaque URIs',
      () async {
    final library = LocalLibraryDatabase(databasePath: inMemoryDatabasePath);
    final media = SqliteLocalMediaRepository(library);
    final first = await media.importDocument(
      uri: 'content://provider/document/abc',
      title: 'Original title',
      mimeType: 'audio/aac',
    );
    final duplicate = await media.importDocument(
      uri: 'content://provider/document/abc',
      title: 'Changed metadata',
      mimeType: 'audio/mpeg',
    );
    final different = await media.importDocument(
      uri: 'content://provider/document/ABC',
      title: 'Different URI',
      mimeType: 'audio/mpeg',
    );

    expect(duplicate.id, first.id);
    expect(duplicate.title, 'Original title');
    expect(different.id, isNot(first.id));
    expect(await media.list(), hasLength(2));
    await library.close();
  });

  test('relink retains the local ID and clamps progress when duration appears',
      () async {
    final library = LocalLibraryDatabase(databasePath: inMemoryDatabasePath);
    final media = SqliteLocalMediaRepository(library);
    final progress = SqliteLocalProgressRepository(library);
    final added = await media.importDocument(
      uri: 'content://provider/old',
      title: 'Original',
      mimeType: 'video/mp4',
    );
    await progress.savePosition(added.id, 125);
    final relinked = await media.relink(
      id: added.id,
      uri: 'content://provider/new',
      title: 'Replacement',
      mimeType: 'video/mp4',
      durationMs: 60000,
    );

    expect(relinked.id, added.id);
    expect(relinked.uri, 'content://provider/new');
    expect(relinked.title, 'Replacement');
    expect((await progress.get(added.id))!.positionSeconds, 60);
    await library.close();
  });

  test('a relink collision fails without changing either record', () async {
    final library = LocalLibraryDatabase(databasePath: inMemoryDatabasePath);
    final media = SqliteLocalMediaRepository(library);
    final first = await media.importDocument(
      uri: 'content://provider/first',
      title: 'First',
      mimeType: 'audio/ogg',
    );
    final second = await media.importDocument(
      uri: 'content://provider/second',
      title: 'Second',
      mimeType: 'audio/ogg',
    );

    await expectLater(
      media.relink(
        id: first.id,
        uri: 'content://provider/second',
        title: 'Changed',
        mimeType: 'audio/ogg',
      ),
      throwsStateError,
    );
    expect((await media.getById(first.id))!.uri, 'content://provider/first');
    expect((await media.getById(second.id))!.title, 'Second');
    await library.close();
  });

  test('failed SQLite import and removal roll back their transactions',
      () async {
    final library = LocalLibraryDatabase(databasePath: inMemoryDatabasePath);
    final media = SqliteLocalMediaRepository(library);
    final progress = SqliteLocalProgressRepository(library);
    final db = await library.database;
    await db.execute('''
      CREATE TRIGGER reject_local_media BEFORE INSERT ON local_media
      BEGIN SELECT RAISE(ABORT, 'injected import failure'); END
    ''');
    await expectLater(
      media.importDocument(
        uri: 'content://provider/rejected',
        title: 'Rejected',
        mimeType: 'audio/mpeg',
      ),
      throwsA(isA<DatabaseException>()),
    );
    expect(await media.list(), isEmpty);
    await db.execute('DROP TRIGGER reject_local_media');

    final added = await media.importDocument(
      uri: 'content://provider/kept',
      title: 'Kept',
      mimeType: 'audio/mpeg',
    );
    await progress.savePosition(added.id, 9);
    await db.execute('''
      CREATE TRIGGER reject_local_media_delete BEFORE DELETE ON local_media
      BEGIN SELECT RAISE(ABORT, 'injected removal failure'); END
    ''');
    await expectLater(
        media.remove(added.id), throwsA(isA<DatabaseException>()));
    expect(await media.getById(added.id), isNotNull);
    expect((await progress.get(added.id))!.positionSeconds, 9);
    await library.close();
  });

  test('removal serializes ahead of a late progress write without resurrection',
      () async {
    final library = LocalLibraryDatabase(databasePath: inMemoryDatabasePath);
    final media = SqliteLocalMediaRepository(library);
    final progress = SqliteLocalProgressRepository(library);
    final added = await media.importDocument(
      uri: 'content://provider/remove-race',
      title: 'Race',
      mimeType: 'audio/mpeg',
    );
    final transactionStarted = Completer<void>();
    final releaseTransaction = Completer<void>();
    final blocker = library.write((transaction) async {
      transactionStarted.complete();
      await releaseTransaction.future;
    });
    await transactionStarted.future;

    final removal = media.remove(added.id);
    final lateWrite = progress.savePosition(added.id, 17);
    releaseTransaction.complete();
    await blocker;

    expect(await removal, isTrue);
    expect(await lateWrite, isFalse);
    expect(await media.getById(added.id), isNull);
    expect(await progress.get(added.id), isNull);
    await library.close();
  });

  test(
      'positions reject invalid values, clamp to duration, and replay clears completion',
      () async {
    final library = LocalLibraryDatabase(databasePath: inMemoryDatabasePath);
    final media = SqliteLocalMediaRepository(library);
    final progress = SqliteLocalProgressRepository(library);
    final added = await media.importDocument(
      uri: 'content://provider/progress',
      title: 'Progress',
      mimeType: 'audio/mpeg',
      durationMs: 10000,
    );

    for (final invalid in [-1.0, double.nan, double.infinity]) {
      expect(
          () => progress.savePosition(added.id, invalid), throwsArgumentError);
    }
    expect(await progress.get(added.id), isNull);
    await progress.savePosition(added.id, 15);
    expect((await progress.get(added.id))!.positionSeconds, 10);
    expect(await progress.markFinished(added.id), isTrue);
    expect((await progress.get(added.id))!.finished, isTrue);

    await progress.savePosition(added.id, 2.5);
    final replay = (await progress.get(added.id))!;
    expect(replay.positionSeconds, 2.5);
    expect(replay.finished, isFalse);
    await library.close();
  });

  test('unknown duration allows progress and explicit completion', () async {
    final library = LocalLibraryDatabase(databasePath: inMemoryDatabasePath);
    final media = SqliteLocalMediaRepository(library);
    final progress = SqliteLocalProgressRepository(library);
    final added = await media.importDocument(
      uri: 'content://provider/unknown-duration',
      title: 'Unknown duration',
      mimeType: 'audio/aac',
    );
    expect(await progress.savePosition(added.id, 900), isTrue);
    expect((await progress.get(added.id))!.finished, isFalse);
    expect(await progress.markFinished(added.id), isTrue);
    expect((await progress.get(added.id))!.finished, isTrue);
    await library.close();
  });

  test('v1 library schema migrates to add last-opened timestamps', () async {
    final directory = await Directory.systemTemp.createTemp('local-v1-');
    final path = '${directory.path}/library.db';
    try {
      final old = await databaseFactoryFfi.openDatabase(
        path,
        options: OpenDatabaseOptions(
          version: 1,
          onCreate: (db, _) async {
            await db.execute('''
              CREATE TABLE local_media (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                uri TEXT NOT NULL UNIQUE,
                title TEXT NOT NULL,
                mime_type TEXT NOT NULL,
                size_bytes INTEGER,
                duration_ms INTEGER,
                added_at INTEGER NOT NULL
              )
            ''');
            await db.execute('''
              CREATE TABLE local_progress (
                local_media_id INTEGER PRIMARY KEY,
                position_seconds REAL NOT NULL,
                finished INTEGER NOT NULL DEFAULT 0,
                updated_at INTEGER NOT NULL,
                FOREIGN KEY (local_media_id) REFERENCES local_media(id)
                  ON DELETE CASCADE
              )
            ''');
          },
        ),
      );
      final id = await old.insert('local_media', {
        'uri': 'content://provider/migrated',
        'title': 'Migrated',
        'mime_type': 'audio/mpeg',
        'added_at': DateTime.utc(2026).millisecondsSinceEpoch,
      });
      await old.insert('local_progress', {
        'local_media_id': id,
        'position_seconds': 4.5,
        'updated_at': DateTime.utc(2026).millisecondsSinceEpoch,
      });
      await old.close();

      final current = LocalLibraryDatabase(databasePath: path);
      final migratedMedia =
          await SqliteLocalMediaRepository(current).getById(id);
      final migratedProgress =
          await SqliteLocalProgressRepository(current).get(id);
      expect(migratedMedia!.lastOpenedAt, isNull);
      expect(migratedProgress!.positionSeconds, 4.5);
      final version =
          await (await current.database).rawQuery('PRAGMA user_version');
      expect(version.single['user_version'], 2);
      await current.close();
    } finally {
      await directory.delete(recursive: true);
    }
  });

  test('logout and changing server settings leave local media untouched',
      () async {
    SharedPreferences.setMockInitialValues({
      'library_destination': 'local',
    });
    final library = LocalLibraryDatabase(databasePath: inMemoryDatabasePath);
    final container = ProviderContainer(overrides: [
      localLibraryDatabaseProvider.overrideWithValue(library),
      tokenStorageProvider.overrideWithValue(_NoTokenStorage()),
    ]);
    addTearDown(() async {
      container.dispose();
      await library.close();
    });
    final media = container.read(localMediaRepositoryProvider);
    final added = await media.importDocument(
      uri: 'content://provider/independent',
      title: 'Local file',
      mimeType: 'audio/mpeg',
    );
    await container.read(authStateProvider.future);

    expect(await container.read(authStateProvider.notifier).logout(), isTrue);
    await container
        .read(authStateProvider.notifier)
        .switchServer('https://player.example');

    expect(
        (await media.getById(added.id))!.uri, 'content://provider/independent');
  });
}
