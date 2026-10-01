import 'dart:async';

import 'package:sqflite/sqflite.dart';

import '../models/local_media.dart';

const _databaseName = 'local_library.db';
const _databaseVersion = 2;
const _mediaTable = 'local_media';
const _progressTable = 'local_progress';

/// Owns the separate SQLite database used only by the on-device library.
/// Writes are serialized across repositories so a delayed progress callback
/// cannot recreate progress after a removal has completed.
class LocalLibraryDatabase {
  LocalLibraryDatabase({String? databasePath}) : _databasePath = databasePath;

  final String? _databasePath;
  Future<Database>? _databaseFuture;
  Future<void> _writeTail = Future<void>.value();

  Future<Database> get database => _databaseFuture ??= _openDatabase();

  Future<T> write<T>(Future<T> Function(Transaction transaction) action) {
    final previous = _writeTail;
    final release = Completer<void>();
    _writeTail = release.future;
    return _runWrite(previous, release, action);
  }

  Future<T> _runWrite<T>(
    Future<void> previous,
    Completer<void> release,
    Future<T> Function(Transaction transaction) action,
  ) async {
    await previous;
    try {
      final db = await database;
      return await db.transaction(action);
    } finally {
      release.complete();
    }
  }

  Future<void> close() async {
    await _writeTail;
    final opened = _databaseFuture;
    if (opened == null) return;
    await (await opened).close();
    _databaseFuture = null;
  }

  Future<Database> _openDatabase() => openDatabase(
        _databasePath ?? _databaseName,
        version: _databaseVersion,
        onConfigure: (db) => db.execute('PRAGMA foreign_keys = ON'),
        onCreate: (db, _) => _createSchema(db),
        onUpgrade: _upgradeSchema,
      );

  static Future<void> _createSchema(DatabaseExecutor db) async {
    await db.execute('''
      CREATE TABLE $_mediaTable (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        uri TEXT NOT NULL UNIQUE,
        title TEXT NOT NULL,
        mime_type TEXT NOT NULL,
        size_bytes INTEGER,
        duration_ms INTEGER,
        added_at INTEGER NOT NULL,
        last_opened_at INTEGER
      )
    ''');
    await db.execute('''
      CREATE TABLE $_progressTable (
        local_media_id INTEGER PRIMARY KEY,
        position_seconds REAL NOT NULL,
        finished INTEGER NOT NULL DEFAULT 0,
        updated_at INTEGER NOT NULL,
        FOREIGN KEY (local_media_id) REFERENCES $_mediaTable(id)
          ON DELETE CASCADE
      )
    ''');
  }

  static Future<void> _createVersionOneSchema(DatabaseExecutor db) async {
    await db.execute('''
      CREATE TABLE $_mediaTable (
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
      CREATE TABLE $_progressTable (
        local_media_id INTEGER PRIMARY KEY,
        position_seconds REAL NOT NULL,
        finished INTEGER NOT NULL DEFAULT 0,
        updated_at INTEGER NOT NULL,
        FOREIGN KEY (local_media_id) REFERENCES $_mediaTable(id)
          ON DELETE CASCADE
      )
    ''');
  }

  static Future<void> _upgradeSchema(
    Database db,
    int oldVersion,
    int newVersion,
  ) async {
    if (oldVersion < 1) await _createVersionOneSchema(db);
    if (oldVersion < 2 && newVersion >= 2) {
      await db.execute(
        'ALTER TABLE $_mediaTable ADD COLUMN last_opened_at INTEGER',
      );
    }
  }
}

/// Metadata operations for device documents. Implementations must keep imports
/// and removals atomic with their local progress rows.
abstract interface class LocalMediaRepository {
  Future<List<LocalMedia>> list();
  Future<LocalMedia?> getById(int id);
  Future<LocalMedia?> getByUri(String uri);

  /// Adds a URI once. An exact URI duplicate returns its existing record.
  Future<LocalMedia> importDocument({
    required String uri,
    required String title,
    required String mimeType,
    int? sizeBytes,
    int? durationMs,
  });

  /// Replaces metadata and URI while retaining the local record ID.
  Future<LocalMedia> relink({
    required int id,
    required String uri,
    required String title,
    required String mimeType,
    int? sizeBytes,
    int? durationMs,
  });

  Future<bool> markOpened(int id, {DateTime? at});
  Future<bool> updateDuration(int id, int? durationMs);
  Future<bool> remove(int id);
}

/// Local playback progress operations. This repository never uses or calls the
/// server progress queue or a Player API client.
abstract interface class LocalProgressRepository {
  Future<LocalProgress?> get(int localMediaId);

  /// Stores a position and clears completion, as a replay starts a new session.
  /// Returns false if the media record was removed before the write ran.
  Future<bool> savePosition(int localMediaId, double positionSeconds);

  /// Marks completion even when the provider did not report a duration.
  Future<bool> markFinished(int localMediaId);

  Future<bool> clear(int localMediaId);
}

class SqliteLocalMediaRepository implements LocalMediaRepository {
  SqliteLocalMediaRepository(this._database);

  final LocalLibraryDatabase _database;

  @override
  Future<List<LocalMedia>> list() async {
    final rows = await (await _database.database).query(
      _mediaTable,
      orderBy: 'COALESCE(last_opened_at, added_at) DESC, id DESC',
    );
    return rows.map(_mediaFromRow).toList(growable: false);
  }

  @override
  Future<LocalMedia?> getById(int id) async {
    final rows = await (await _database.database).query(
      _mediaTable,
      where: 'id = ?',
      whereArgs: [id],
      limit: 1,
    );
    return rows.isEmpty ? null : _mediaFromRow(rows.single);
  }

  @override
  Future<LocalMedia?> getByUri(String uri) async {
    final rows = await (await _database.database).query(
      _mediaTable,
      where: 'uri = ?',
      whereArgs: [uri],
      limit: 1,
    );
    return rows.isEmpty ? null : _mediaFromRow(rows.single);
  }

  @override
  Future<LocalMedia> importDocument({
    required String uri,
    required String title,
    required String mimeType,
    int? sizeBytes,
    int? durationMs,
  }) async {
    _validateDocument(uri, title, mimeType, sizeBytes, durationMs);
    return _database.write((transaction) async {
      final existing = await transaction.query(
        _mediaTable,
        where: 'uri = ?',
        whereArgs: [uri],
        limit: 1,
      );
      if (existing.isNotEmpty) return _mediaFromRow(existing.single);

      final now = DateTime.now().toUtc();
      final id = await transaction.insert(_mediaTable, {
        'uri': uri,
        'title': title,
        'mime_type': mimeType,
        'size_bytes': sizeBytes,
        'duration_ms': durationMs,
        'added_at': now.millisecondsSinceEpoch,
      });
      return _mediaFromRow((await transaction.query(
        _mediaTable,
        where: 'id = ?',
        whereArgs: [id],
        limit: 1,
      ))
          .single);
    });
  }

  @override
  Future<LocalMedia> relink({
    required int id,
    required String uri,
    required String title,
    required String mimeType,
    int? sizeBytes,
    int? durationMs,
  }) async {
    _validateDocument(uri, title, mimeType, sizeBytes, durationMs);
    return _database.write((transaction) async {
      final record = await _mediaRow(transaction, id);
      if (record == null) throw StateError('Local media $id no longer exists');
      final collision = await transaction.query(
        _mediaTable,
        columns: ['id'],
        where: 'uri = ? AND id != ?',
        whereArgs: [uri, id],
        limit: 1,
      );
      if (collision.isNotEmpty) {
        throw StateError('That document is already in the local library');
      }

      await transaction.update(
        _mediaTable,
        {
          'uri': uri,
          'title': title,
          'mime_type': mimeType,
          'size_bytes': sizeBytes,
          'duration_ms': durationMs,
        },
        where: 'id = ?',
        whereArgs: [id],
      );
      if (durationMs != null) await _clampProgress(transaction, id, durationMs);
      return _mediaFromRow((await _mediaRow(transaction, id))!);
    });
  }

  @override
  Future<bool> markOpened(int id, {DateTime? at}) => _database.write(
        (transaction) async =>
            await transaction.update(
              _mediaTable,
              {
                'last_opened_at':
                    (at ?? DateTime.now()).toUtc().millisecondsSinceEpoch,
              },
              where: 'id = ?',
              whereArgs: [id],
            ) >
            0,
      );

  @override
  Future<bool> updateDuration(int id, int? durationMs) {
    if (durationMs != null && durationMs < 0) {
      throw ArgumentError.value(
          durationMs, 'durationMs', 'must be nonnegative');
    }
    return _database.write((transaction) async {
      final updated = await transaction.update(
        _mediaTable,
        {'duration_ms': durationMs},
        where: 'id = ?',
        whereArgs: [id],
      );
      if (updated == 0) return false;
      if (durationMs != null) await _clampProgress(transaction, id, durationMs);
      return true;
    });
  }

  @override
  Future<bool> remove(int id) => _database.write((transaction) async {
        final removed = await transaction.delete(
          _mediaTable,
          where: 'id = ?',
          whereArgs: [id],
        );
        return removed > 0;
      });

  Future<void> _clampProgress(
    Transaction transaction,
    int id,
    int durationMs,
  ) async {
    final progress = await transaction.query(
      _progressTable,
      columns: ['position_seconds'],
      where: 'local_media_id = ?',
      whereArgs: [id],
      limit: 1,
    );
    if (progress.isEmpty) return;
    final limitSeconds = durationMs / 1000.0;
    final position = (progress.single['position_seconds'] as num).toDouble();
    if (position <= limitSeconds) return;
    await transaction.update(
      _progressTable,
      {
        'position_seconds': limitSeconds,
        'updated_at': DateTime.now().toUtc().millisecondsSinceEpoch,
      },
      where: 'local_media_id = ?',
      whereArgs: [id],
    );
  }
}

class SqliteLocalProgressRepository implements LocalProgressRepository {
  SqliteLocalProgressRepository(this._database);

  final LocalLibraryDatabase _database;

  @override
  Future<LocalProgress?> get(int localMediaId) async {
    final rows = await (await _database.database).query(
      _progressTable,
      where: 'local_media_id = ?',
      whereArgs: [localMediaId],
      limit: 1,
    );
    return rows.isEmpty ? null : _progressFromRow(rows.single);
  }

  @override
  Future<bool> savePosition(int localMediaId, double positionSeconds) {
    _validatePosition(positionSeconds);
    return _database.write((transaction) async {
      final media = await _mediaRow(transaction, localMediaId);
      if (media == null) return false;
      final durationMs = media['duration_ms'] as int?;
      final position = durationMs == null
          ? positionSeconds
          : _clampPosition(positionSeconds, durationMs);
      await transaction.insert(
        _progressTable,
        {
          'local_media_id': localMediaId,
          'position_seconds': position,
          'finished': 0,
          'updated_at': DateTime.now().toUtc().millisecondsSinceEpoch,
        },
        conflictAlgorithm: ConflictAlgorithm.replace,
      );
      return true;
    });
  }

  @override
  Future<bool> markFinished(int localMediaId) =>
      _database.write((transaction) async {
        final media = await _mediaRow(transaction, localMediaId);
        if (media == null) return false;
        final prior = await transaction.query(
          _progressTable,
          columns: ['position_seconds'],
          where: 'local_media_id = ?',
          whereArgs: [localMediaId],
          limit: 1,
        );
        final durationMs = media['duration_ms'] as int?;
        final position = durationMs == null
            ? (prior.isEmpty
                ? 0.0
                : (prior.single['position_seconds'] as num).toDouble())
            : durationMs / 1000.0;
        await transaction.insert(
          _progressTable,
          {
            'local_media_id': localMediaId,
            'position_seconds': position,
            'finished': 1,
            'updated_at': DateTime.now().toUtc().millisecondsSinceEpoch,
          },
          conflictAlgorithm: ConflictAlgorithm.replace,
        );
        return true;
      });

  @override
  Future<bool> clear(int localMediaId) => _database.write(
        (transaction) async =>
            await transaction.delete(
              _progressTable,
              where: 'local_media_id = ?',
              whereArgs: [localMediaId],
            ) >
            0,
      );
}

Future<Map<String, Object?>?> _mediaRow(Transaction transaction, int id) async {
  final rows = await transaction.query(
    _mediaTable,
    where: 'id = ?',
    whereArgs: [id],
    limit: 1,
  );
  return rows.isEmpty ? null : rows.single;
}

LocalMedia _mediaFromRow(Map<String, Object?> row) => LocalMedia(
      id: row['id'] as int,
      uri: row['uri'] as String,
      title: row['title'] as String,
      mimeType: row['mime_type'] as String,
      sizeBytes: row['size_bytes'] as int?,
      durationMs: row['duration_ms'] as int?,
      addedAt: _dateFromEpoch(row['added_at'] as int),
      lastOpenedAt: row['last_opened_at'] == null
          ? null
          : _dateFromEpoch(row['last_opened_at'] as int),
    );

LocalProgress _progressFromRow(Map<String, Object?> row) => LocalProgress(
      localMediaId: row['local_media_id'] as int,
      positionSeconds: (row['position_seconds'] as num).toDouble(),
      finished: (row['finished'] as int) != 0,
      updatedAt: _dateFromEpoch(row['updated_at'] as int),
    );

DateTime _dateFromEpoch(int milliseconds) =>
    DateTime.fromMillisecondsSinceEpoch(milliseconds, isUtc: true);

void _validateDocument(
  String uri,
  String title,
  String mimeType,
  int? sizeBytes,
  int? durationMs,
) {
  if (uri.isEmpty) throw ArgumentError.value(uri, 'uri', 'must not be empty');
  if (title.isEmpty) {
    throw ArgumentError.value(title, 'title', 'must not be empty');
  }
  if (mimeType.isEmpty) {
    throw ArgumentError.value(mimeType, 'mimeType', 'must not be empty');
  }
  if (sizeBytes != null && sizeBytes < 0) {
    throw ArgumentError.value(sizeBytes, 'sizeBytes', 'must be nonnegative');
  }
  if (durationMs != null && durationMs < 0) {
    throw ArgumentError.value(durationMs, 'durationMs', 'must be nonnegative');
  }
}

void _validatePosition(double positionSeconds) {
  if (!positionSeconds.isFinite || positionSeconds < 0) {
    throw ArgumentError.value(
      positionSeconds,
      'positionSeconds',
      'must be finite and nonnegative',
    );
  }
}

double _clampPosition(double positionSeconds, int durationMs) =>
    positionSeconds.clamp(0, durationMs / 1000.0).toDouble();
