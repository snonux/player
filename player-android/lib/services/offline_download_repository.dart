import 'dart:async';
import 'dart:io';

import 'package:sqflite/sqflite.dart';

import '../models/offline_media.dart';
import 'playback_request.dart';
import 'personal_media_store.dart';

/// App-private downloads, independent of document permissions and server state.
/// Only generated hashes are used as filenames; remote paths never reach disk.
class OfflineDownloadRepository {
  OfflineDownloadRepository({Directory? directory}) : _directory = directory;

  Directory? _directory;
  Directory get directory => _directory!;
  Future<Database>? _database;
  Future<void> _tail = Future.value();

  Future<Database> get database =>
      _database ??= _open().catchError((Object error, StackTrace stack) {
        _database = null;
        Error.throwWithStackTrace(error, stack);
      });

  Future<Database> _open() async {
    _directory ??= Directory('${await getDatabasesPath()}/offline-downloads');
    await directory.create(recursive: true);
    final db = await openDatabase('${directory.path}/index.db',
        version: 2,
        onConfigure: (db) => db.execute('PRAGMA foreign_keys = ON'),
        onUpgrade: (db, oldVersion, _) async {
          if (oldVersion < 2) {
            await db.execute(
                "ALTER TABLE downloads ADD COLUMN instance_id TEXT NOT NULL DEFAULT ''");
            await db.execute(
                'UPDATE downloads SET instance_id = lower(hex(randomblob(16)))');
            await _createPersonalSchema(db);
          }
        },
        onCreate: (db, _) async {
          await db.execute('''CREATE TABLE downloads (
        key TEXT PRIMARY KEY, instance_id TEXT NOT NULL, origin TEXT NOT NULL, user_id INTEGER NOT NULL,
        media_id INTEGER NOT NULL, title TEXT NOT NULL, type TEXT NOT NULL,
        size_bytes INTEGER NOT NULL, added_at INTEGER NOT NULL,
        position_seconds REAL NOT NULL DEFAULT 0,
        finished INTEGER NOT NULL DEFAULT 0
      )''');
          await _createPersonalSchema(db);
        });
    try {
      // A killed process may leave a partial download or a completed file whose
      // metadata was never committed. Neither is presented as ready.
      final keys = (await db.query('downloads', columns: ['key']))
          .map((row) => row['key'])
          .toSet();
      await for (final entry in directory.list(followLinks: false)) {
        final name = entry.uri.pathSegments.last;
        if (entry is File &&
            (RegExp(r'^[a-f0-9]{64}\.part$').hasMatch(name) ||
                (RegExp(r'^[a-f0-9]{64}\.media$').hasMatch(name) &&
                    !keys.contains(name.substring(0, 64))))) {
          await entry.delete();
        }
      }
      return db;
    } catch (_) {
      await db.close();
      rethrow;
    }
  }

  static Future<void> _createPersonalSchema(DatabaseExecutor db) =>
      createPersonalMediaSchema(db,
          ownerTable: 'downloads', ownerColumn: 'key', keyType: 'TEXT');

  PersonalMediaStore personalStore(String key) => SqlitePersonalMediaStore(
      database: () => database,
      write: <T>(action) => _write((db) => db.transaction(action)),
      ownerTable: 'downloads',
      ownerColumn: 'key',
      ownerId: key,
      identity: 'offline:$key',
      ownerRevision: _write((db) async {
        final rows = await db.query('downloads',
            columns: ['instance_id'], where: 'key = ?', whereArgs: [key]);
        return rows.isEmpty ? null : rows.single['instance_id'];
      }));

  File file(String key, {bool partial = false}) {
    if (!RegExp(r'^[a-f0-9]{64}$').hasMatch(key)) {
      throw ArgumentError('Invalid download identity');
    }
    return File('${directory.path}/$key.${partial ? 'part' : 'media'}');
  }

  Future<T> _write<T>(Future<T> Function(Database db) action) async {
    final previous = _tail;
    final release = Completer<void>();
    _tail = release.future;
    await previous;
    try {
      return await action(await database);
    } finally {
      release.complete();
    }
  }

  Future<List<OfflineMedia>> list() async =>
      (await (await database).query('downloads', orderBy: 'added_at DESC'))
          .map(_fromRow)
          .toList();

  Future<OfflineMedia?> get(String key) async {
    final rows = await (await database)
        .query('downloads', where: 'key = ?', whereArgs: [key]);
    return rows.isEmpty ? null : _fromRow(rows.single);
  }

  Future<void> add(OfflineMedia item) => _write((db) async {
        if (item.key !=
                offlineMediaKey(item.origin, item.userId, item.mediaId) ||
            !{'audio', 'video', 'image'}.contains(item.type) ||
            item.sizeBytes <= 0) {
          throw ArgumentError('Invalid download metadata');
        }
        await db.insert('downloads', {
          'key': item.key,
          'instance_id':
              (await db.rawQuery('SELECT lower(hex(randomblob(16))) AS id'))
                  .single['id'],
          'origin': item.origin,
          'user_id': item.userId,
          'media_id': item.mediaId,
          'title': item.title,
          'type': item.type,
          'size_bytes': item.sizeBytes,
          'added_at': item.addedAt.toUtc().millisecondsSinceEpoch,
        });
      });

  Future<void> remove(String key) => _write((db) async {
        final source = file(key);
        if (await source.exists()) await source.delete();
        await db.delete('downloads', where: 'key = ?', whereArgs: [key]);
      });

  Future<void> saveProgress(String key, double seconds,
          {bool finished = false}) =>
      _write((db) async {
        if (!seconds.isFinite || seconds < 0) {
          throw ArgumentError('Invalid position');
        }
        // UPDATE deliberately cannot recreate a deleted download.
        await db.update(
            'downloads',
            {
              'position_seconds': seconds,
              'finished': finished ? 1 : 0,
            },
            where: 'key = ?',
            whereArgs: [key]);
      });

  Future<OfflinePlaybackRequest> playbackRequest(String key) async {
    final item = await get(key);
    if (item == null ||
        !await file(key).exists() ||
        await file(key).length() != item.sizeBytes) {
      throw StateError(
          'Offline copy is missing or incomplete. Delete it and download again.');
    }
    return OfflinePlaybackRequest(
      downloadKey: key,
      title: item.title,
      sourceUri: file(key).uri,
      readPosition: () async {
        final saved = await get(key);
        return saved?.finished == true ? 0 : saved?.positionSeconds;
      },
      savePosition: (seconds) => saveProgress(key, seconds),
      markFinished: () async {
        final saved = await get(key);
        if (saved != null) {
          await saveProgress(key, saved.positionSeconds, finished: true);
        }
      },
    );
  }

  Future<void> close() async {
    await _tail;
    if (_database != null) await (await _database!).close();
    _database = null;
  }

  static OfflineMedia _fromRow(Map<String, Object?> row) => OfflineMedia(
        key: row['key'] as String,
        origin: row['origin'] as String,
        userId: row['user_id'] as int,
        mediaId: row['media_id'] as int,
        title: row['title'] as String,
        type: row['type'] as String,
        sizeBytes: row['size_bytes'] as int,
        addedAt: DateTime.fromMillisecondsSinceEpoch(row['added_at'] as int,
            isUtc: true),
        positionSeconds: (row['position_seconds'] as num).toDouble(),
        finished: row['finished'] == 1,
      );
}
