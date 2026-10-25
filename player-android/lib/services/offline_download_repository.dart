import 'dart:async';
import 'dart:convert';
import '../models/music_metadata.dart';
import '../models/lyrics.dart';
import 'dart:io';

import 'package:sqflite/sqflite.dart';

import '../models/offline_media.dart';
import '../models/media_bookmark.dart';
import '../models/note.dart';
import 'playback_request.dart';
import 'personal_media_store.dart';

/// App-private downloads, independent of document permissions and server state.
/// Only generated hashes are used as filenames; remote paths never reach disk.
class OfflineDownloadRepository {
  static const _metadataColumns = [
    'key',
    'origin',
    'user_id',
    'media_id',
    'title',
    'source_name',
    'automation_music',
    'download_profile',
    'content_type',
    'music',
    'type',
    'size_bytes',
    'added_at',
    'position_seconds',
    'finished',
    'instance_id'
  ];
  OfflineDownloadRepository({Directory? directory, DateTime Function()? now})
      : _directory = directory,
        _now = now ?? DateTime.now;
  final DateTime Function() _now;

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
        version: 9,
        onConfigure: (db) => db.execute('PRAGMA foreign_keys = ON'),
        onUpgrade: (db, oldVersion, _) async {
          if (oldVersion < 9) {
            await db.execute('ALTER TABLE downloads ADD COLUMN lyrics TEXT');
          }
          if (oldVersion < 8) {
            await db.execute(
                'ALTER TABLE downloads ADD COLUMN automation_music INTEGER NOT NULL DEFAULT 0');
          }
          if (oldVersion < 7) {
            await db.execute(
                "ALTER TABLE downloads ADD COLUMN download_profile TEXT NOT NULL DEFAULT ''");
            await db.execute(
                "ALTER TABLE downloads ADD COLUMN content_type TEXT NOT NULL DEFAULT ''");
          }
          if (oldVersion < 6) await _createMusicListeningSchema(db);
          if (oldVersion >= 3 && oldVersion < 5) {
            await db
                .execute('ALTER TABLE sync_items ADD COLUMN gone_ms INTEGER');
          }
          if (oldVersion < 4) {
            await db.execute('ALTER TABLE downloads ADD COLUMN music TEXT');
            await db.execute(
                "ALTER TABLE downloads ADD COLUMN source_name TEXT NOT NULL DEFAULT ''");
          }
          if (oldVersion < 2) {
            await db.execute(
                "ALTER TABLE downloads ADD COLUMN instance_id TEXT NOT NULL DEFAULT ''");
            await db.execute(
                'UPDATE downloads SET instance_id = lower(hex(randomblob(16)))');
            await _createPersonalSchema(db);
          }
          if (oldVersion < 3) {
            await _createSyncSchema(db);
            await _migrateSync(db);
          }
        },
        onCreate: (db, _) async {
          await db.execute('''CREATE TABLE downloads (
        key TEXT PRIMARY KEY, instance_id TEXT NOT NULL, origin TEXT NOT NULL, user_id INTEGER NOT NULL,
        media_id INTEGER NOT NULL, title TEXT NOT NULL, type TEXT NOT NULL,
        size_bytes INTEGER NOT NULL, added_at INTEGER NOT NULL,
        position_seconds REAL NOT NULL DEFAULT 0,
        finished INTEGER NOT NULL DEFAULT 0, music TEXT, source_name TEXT NOT NULL DEFAULT '',
        download_profile TEXT NOT NULL DEFAULT '', content_type TEXT NOT NULL DEFAULT '',
        automation_music INTEGER NOT NULL DEFAULT 0, lyrics TEXT
      )''');
          await _createPersonalSchema(db);
          await _createSyncSchema(db);
          await _createMusicListeningSchema(db);
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

  PersonalMediaStore personalStore(String key) {
    final revision = _write((db) async {
      final rows = await db.query('downloads',
          columns: ['instance_id'], where: 'key=?', whereArgs: [key]);
      return rows.isEmpty ? null : rows.single['instance_id'];
    });
    return DownloadPersonalMediaStore(
        this,
        key,
        revision,
        SqlitePersonalMediaStore(
            database: () => database,
            write: <T>(action) => _write((db) => db.transaction(action)),
            ownerTable: 'downloads',
            ownerColumn: 'key',
            ownerId: key,
            identity: 'offline:$key',
            ownerRevision: revision));
  }

  Future<Map<String, Object?>> _syncOwner(
      DatabaseExecutor db, String key, Future<Object?> revision) async {
    final rows = await db.query('downloads',
        columns: _metadataColumns, where: 'key=?', whereArgs: [key]);
    if (rows.isEmpty || rows.single['instance_id'] != await revision) {
      throw StateError('This offline copy has been removed or replaced.');
    }
    return rows.single;
  }

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
      (await (await database).query('downloads',
              columns: _metadataColumns, orderBy: 'added_at DESC'))
          .map(_fromRow)
          .toList();

  Future<OfflineMedia?> get(String key) async {
    final rows = await (await database).query('downloads',
        columns: _metadataColumns, where: 'key = ?', whereArgs: [key]);
    return rows.isEmpty ? null : _fromRow(rows.single);
  }

  Future<void> add(OfflineMedia item,
          {Map<String, dynamic>? lyrics, bool automaticMusic = false}) =>
      _write((db) => db.transaction((db) async {
            if (item.key !=
                    offlineMediaKey(item.origin, item.userId, item.mediaId) ||
                !{'audio', 'video', 'image'}.contains(item.type) ||
                item.sizeBytes <= 0) {
              throw ArgumentError('Invalid download metadata');
            }
            await db.insert('downloads', {
              'key': item.key,
              'lyrics': lyrics == null ? null : jsonEncode(lyrics),
              'instance_id':
                  (await db.rawQuery('SELECT lower(hex(randomblob(16))) AS id'))
                      .single['id'],
              'origin': item.origin,
              'user_id': item.userId,
              'media_id': item.mediaId,
              'title': item.title,
              'source_name': item.sourceName,
              'automation_music': item.automationMusicEligible ? 1 : 0,
              'download_profile': item.downloadProfile,
              'content_type': item.contentType,
              'music':
                  item.music == null ? null : jsonEncode(item.music!.toJson()),
              'type': item.type,
              'size_bytes': item.sizeBytes,
              'added_at': item.addedAt.toUtc().millisecondsSinceEpoch,
            });
            if (automaticMusic) {
              await db.execute(
                  'CREATE TABLE IF NOT EXISTS automatic_music_copies (download_key TEXT PRIMARY KEY REFERENCES downloads(key) ON DELETE CASCADE)');
              await db
                  .insert('automatic_music_copies', {'download_key': item.key});
            }
            await db.insert(
                'sync_items',
                {
                  'key': item.key,
                  'origin': item.origin,
                  'user_id': item.userId,
                  'media_id': item.mediaId
                },
                conflictAlgorithm: ConflictAlgorithm.ignore);
            final state = (await db
                    .query('sync_items', where: 'key=?', whereArgs: [item.key]))
                .single;
            await db.update(
                'downloads',
                {
                  'position_seconds': state['position_seconds'],
                  'finished': state['finished']
                },
                where: 'key=?',
                whereArgs: [item.key]);
            onSyncNeeded?.call();
          }));

  Future<void> remove(String key) => _write((db) async {
        final source = file(key);
        if (await source.exists()) await source.delete();
        await db.delete('downloads', where: 'key = ?', whereArgs: [key]);
        await _pruneSynced(db, key);
      });

  /// Saved lyrics have exactly the same account and lifetime as their audio.
  Future<Lyrics> lyrics(String key, String origin, int user) async {
    final rows = await (await database).query('downloads',
        columns: ['lyrics'],
        where: 'key=? AND origin=? AND user_id=?',
        whereArgs: [key, origin, user]);
    if (rows.isEmpty || rows.single['lyrics'] is! String) return const Lyrics();
    try {
      final data = jsonDecode(rows.single['lyrics'] as String);
      return Lyrics.fromJson(data is Map
          ? {...data, 'provider_enabled': false, 'saved': true}
          : null);
    } catch (_) {
      return const Lyrics();
    }
  }

  static Future<void> _pruneSynced(DatabaseExecutor db, String key) async {
    await db.rawDelete('''DELETE FROM sync_items WHERE key=? AND pending=0
      AND NOT EXISTS(SELECT 1 FROM downloads WHERE downloads.key=sync_items.key)
      AND NOT EXISTS(SELECT 1 FROM sync_bookmarks WHERE owner_id=sync_items.key AND pending=1)
      AND NOT EXISTS(SELECT 1 FROM sync_music_plays WHERE owner_id=sync_items.key AND accumulated_seconds>=60)''',
        [key]);
  }

  Future<void> saveProgress(String key, double seconds,
      {bool finished = false, String? listeningSession}) {
    if (!validBookmarkPosition(seconds)) {
      return Future.error(ArgumentError('Invalid position'));
    }
    final observed = _now().toUtc().millisecondsSinceEpoch;
    return _write((db) => db.transaction((tx) async {
          final changed = await tx.update('downloads',
              {'position_seconds': seconds, 'finished': finished ? 1 : 0},
              where: 'key=?', whereArgs: [key]);
          if (changed == 0) return;
          if (listeningSession != null && !finished) {
            await tx.rawUpdate('''UPDATE sync_music_plays SET
              accumulated_seconds=accumulated_seconds+MIN(MAX(?-last_position,0),12),
              last_position=?,observed_ms=CASE WHEN accumulated_seconds<60 THEN ? ELSE observed_ms END
              WHERE id=? AND owner_id=? AND accumulated_seconds<60''',
                [seconds, seconds, observed, listeningSession, key]);
          }
          await tx.rawUpdate(
              'UPDATE sync_items SET position_seconds=?,finished=?,observed_ms=MAX(observed_ms+1,?),pending=1 WHERE key=?',
              [seconds, finished ? 1 : 0, observed, key]);
          onSyncNeeded?.call();
        }));
  }

  /// Disk/metadata validation only: no resume/listening state is changed.
  Future<OfflineMedia> validatePlaybackCopy(String key) async {
    final item = await get(key);
    if (item == null ||
        !await file(key).exists() ||
        await file(key).length() != item.sizeBytes) {
      throw StateError(
          'Offline copy is missing or incomplete. Delete it and download again.');
    }
    return item;
  }

  Future<void> setAutomationMusicEligibility(
          String key, String origin, int userId, bool eligible) =>
      _write((db) async {
        await db.update('downloads', {'automation_music': eligible ? 1 : 0},
            where: 'key=? AND origin=? AND user_id=?',
            whereArgs: [key, origin, userId]);
      });

  Future<OfflinePlaybackRequest> playbackRequest(String key,
      {bool startListeningSession = true}) async {
    final item = await validatePlaybackCopy(key);
    String? session;
    Future<void> beginListening() async {
      await beforePlayback?.call(key);
      final latest = await validatePlaybackCopy(key);
      final db = await database;
      session = (await db.rawQuery('SELECT lower(hex(randomblob(16))) AS id'))
          .single['id'] as String;
      await _write((db) => db.transaction((tx) async {
            await tx.delete('sync_music_plays',
                where: 'owner_id=? AND accumulated_seconds<60',
                whereArgs: [key]);
            await tx.insert('sync_music_plays', {
              'id': session,
              'owner_id': key,
              'last_position': latest.finished ? 0 : latest.positionSeconds,
              'accumulated_seconds': 0,
              'observed_ms': 0
            });
          }));
    }

    if (startListeningSession) await beginListening();
    return OfflinePlaybackRequest(
      downloadKey: key,
      automationMusicEligible: item.automationMusicEligible,
      beginListening: beginListening,
      serverOrigin: item.origin,
      userId: item.userId,
      title: item.title,
      artist: item.music?.artist ?? '',
      album: item.music?.album ?? '',
      music: item.music,
      sourceName: item.sourceName,
      sourceUri: file(key).uri,
      readPosition: () async {
        final saved = await get(key);
        return saved?.finished == true ? 0 : saved?.positionSeconds;
      },
      savePosition: (seconds) => saveProgress(key, seconds,
          listeningSession: item.type == 'audio' ? session : null),
      markFinished: () async {
        final saved = await get(key);
        if (saved != null) {
          await saveProgress(key, saved.positionSeconds, finished: true);
        }
      },
    );
  }

  void Function()? onSyncNeeded;
  Future<void> Function(String key)? beforePlayback;

  static Future<void> _createMusicListeningSchema(DatabaseExecutor db) async {
    await db.execute('''CREATE TABLE IF NOT EXISTS sync_music_plays (
      id TEXT PRIMARY KEY,owner_id TEXT NOT NULL REFERENCES sync_items(key) ON DELETE CASCADE,
      last_position REAL NOT NULL,accumulated_seconds REAL NOT NULL,observed_ms INTEGER NOT NULL)''');
  }

  static Future<void> _createSyncSchema(DatabaseExecutor db) async {
    // Sync state deliberately outlives removal of downloaded bytes. Removing a
    // copy must neither erase queued edits nor delete server bookmarks.
    await db.execute('''CREATE TABLE sync_items (
      key TEXT PRIMARY KEY, origin TEXT NOT NULL, user_id INTEGER NOT NULL,
      media_id INTEGER NOT NULL, position_seconds REAL NOT NULL DEFAULT 0,
      finished INTEGER NOT NULL DEFAULT 0, observed_ms INTEGER NOT NULL DEFAULT 0,
      pending INTEGER NOT NULL DEFAULT 0, gone_ms INTEGER)''');
    await db.execute('''CREATE TABLE sync_bookmarks (
      id INTEGER PRIMARY KEY AUTOINCREMENT, owner_id TEXT NOT NULL,
      sync_key TEXT NOT NULL, position_seconds REAL NOT NULL, label TEXT NOT NULL,
      observed_ms INTEGER NOT NULL, deleted INTEGER NOT NULL DEFAULT 0,
      pending INTEGER NOT NULL DEFAULT 0, UNIQUE(owner_id,sync_key),
      FOREIGN KEY(owner_id) REFERENCES sync_items(key) ON DELETE CASCADE)''');
  }

  static Future<void> _migrateSync(DatabaseExecutor db) async {
    // Historical positions have no edit time; keep them locally until a server
    // state is available, without presenting migration time as a playback edit.
    await db.execute(
        '''INSERT INTO sync_items(key,origin,user_id,media_id,position_seconds,finished)
      SELECT key,origin,user_id,media_id,position_seconds,finished FROM downloads''');
    await db.execute(
        '''INSERT INTO sync_bookmarks(owner_id,sync_key,position_seconds,label,observed_ms,pending)
      SELECT owner_id,lower(hex(randomblob(16))),position_seconds,label,created_at,1 FROM personal_bookmarks''');
    await db.delete('personal_bookmarks');
  }

  Future<List<Map<String, Object?>>> syncItems(
          String origin, int userId) async =>
      (await database).query('sync_items',
          where: 'origin=? AND user_id=?', whereArgs: [origin, userId]);

  Future<void> markSyncGone(String key, bool Function() current) =>
      _write((db) async {
        if (current()) {
          await db.update(
              'sync_items', {'gone_ms': DateTime.now().millisecondsSinceEpoch},
              where: 'key=?', whereArgs: [key]);
        }
      });
  Future<void> clearSyncGone(String key, bool Function() current) =>
      _write((db) async {
        if (current()) {
          await db.update('sync_items', {'gone_ms': null},
              where: 'key=?', whereArgs: [key]);
        }
      });

  Future<Map<String, dynamic>> syncPayload(String key) async {
    final db = await database;
    final rows = await db.query('sync_items', where: 'key=?', whereArgs: [key]);
    if (rows.isEmpty) {
      return {'bookmarks': <Object>[]};
    }
    final item = rows.single;
    final musicPlays = await db.query('sync_music_plays',
        where: 'owner_id=? AND accumulated_seconds>=60',
        whereArgs: [key],
        limit: 100);
    return {
      if (musicPlays.isNotEmpty)
        'music_plays': musicPlays
            .map((play) => {
                  'id': play['id'],
                  'observed_ms': play['observed_ms'],
                  'accumulated_seconds': 60
                })
            .toList(),
      if (item['pending'] == 1)
        'progress': {
          'position_seconds': item['position_seconds'],
          'finished': item['finished'] == 1,
          'observed_ms': item['observed_ms']
        },
      'bookmarks': (await db.query('sync_bookmarks',
              where: 'owner_id=? AND pending=1', whereArgs: [key], limit: 1000))
          .map((b) => {
                'sync_key': b['sync_key'],
                'position_seconds': b['position_seconds'],
                'label': b['label'],
                'observed_ms': b['observed_ms'],
                'deleted': b['deleted'] == 1
              })
          .toList(),
    };
  }

  Future<void> mergeSync(
          String key, Map<String, dynamic> result, bool Function() current) =>
      _write((db) => db.transaction((tx) async {
            if (!current()) return;
            final rows =
                await tx.query('sync_items', where: 'key=?', whereArgs: [key]);
            if (rows.isEmpty) return;
            for (final id
                in (result['accepted_music_plays'] as List? ?? const [])) {
              if (id is String) {
                await tx.delete('sync_music_plays',
                    where: 'owner_id=? AND id=? AND accumulated_seconds>=60',
                    whereArgs: [key, id]);
              }
            }
            final p = result['progress'];
            if (p is Map &&
                (p['observed_ms'] as int) >=
                    (rows.single['observed_ms'] as int)) {
              await tx.update(
                  'sync_items',
                  {
                    'position_seconds': p['position_seconds'],
                    'finished': p['finished'] == true ? 1 : 0,
                    'observed_ms': p['observed_ms'],
                    'pending': 0
                  },
                  where: 'key=?',
                  whereArgs: [key]);
              await tx.update(
                  'downloads',
                  {
                    'position_seconds': p['position_seconds'],
                    'finished': p['finished'] == true ? 1 : 0
                  },
                  where: 'key=?',
                  whereArgs: [key]);
            }
            for (final value in (result['bookmarks'] as List? ?? [])) {
              final b = value as Map;
              final old = await tx.query('sync_bookmarks',
                  where: 'owner_id=? AND sync_key=?',
                  whereArgs: [key, b['sync_key']]);
              if (old.isNotEmpty &&
                  (old.single['observed_ms'] as int) >
                      (b['observed_ms'] as int)) {
                continue;
              }
              final data = <String, Object?>{
                'owner_id': key,
                'sync_key': b['sync_key'],
                'position_seconds': b['position_seconds'],
                'label': b['label'],
                'observed_ms': b['observed_ms'],
                'deleted': b['deleted'] == true ? 1 : 0,
                'pending': 0
              };
              if (old.isEmpty) {
                await tx.insert('sync_bookmarks', data);
              } else {
                await tx.update('sync_bookmarks', data,
                    where: 'id=?', whereArgs: [old.single['id']]);
              }
            }
            if (!current()) {
              throw StateError('Account changed during sync.');
            }
            await _pruneSynced(tx, key);
          }));

  Future<List<MediaBookmark>> syncedBookmarks(
          String key, Future<Object?> revision) =>
      _write((db) => db.transaction((tx) async {
            await _syncOwner(tx, key, revision);
            return (await tx.query('sync_bookmarks',
                    where: 'owner_id=? AND deleted=0',
                    whereArgs: [key],
                    orderBy: 'position_seconds,id'))
                .map((b) => MediaBookmark(
                    id: b['id'] as int,
                    positionSeconds: (b['position_seconds'] as num).toDouble(),
                    label: b['label'] as String,
                    createdAt: DateTime.fromMillisecondsSinceEpoch(
                        b['observed_ms'] as int,
                        isUtc: true)))
                .toList();
          }));

  Future<MediaBookmark> addSyncedBookmark(
      String key, double seconds, String label, Future<Object?> revision) {
    final observed = _now().toUtc().millisecondsSinceEpoch;
    return _write((db) => db.transaction((tx) async {
          final item = await _syncOwner(tx, key, revision);
          if (item['type'] == 'image') {
            throw StateError(
                'Images use favorites instead of position bookmarks.');
          }
          if (!validBookmarkPosition(seconds) ||
              label.trim().runes.length > 200) {
            throw ArgumentError('Invalid bookmark.');
          }
          final ms = observed;
          final syncKey =
              (await tx.rawQuery('SELECT lower(hex(randomblob(16))) AS id'))
                  .single['id'];
          final id = await tx.insert('sync_bookmarks', {
            'owner_id': key,
            'sync_key': syncKey,
            'position_seconds': seconds,
            'label': label.trim(),
            'observed_ms': ms,
            'pending': 1
          });
          onSyncNeeded?.call();
          return MediaBookmark(
              id: id,
              positionSeconds: seconds,
              label: label.trim(),
              createdAt: DateTime.fromMillisecondsSinceEpoch(ms, isUtc: true));
        }));
  }

  Future<void> deleteSyncedBookmark(
      String key, int id, Future<Object?> revision) {
    final observed = _now().toUtc().millisecondsSinceEpoch;
    return _write((db) => db.transaction((db) async {
          await _syncOwner(db, key, revision);
          await db.rawUpdate(
              'UPDATE sync_bookmarks SET deleted=1,observed_ms=MAX(observed_ms+1,?),pending=1 WHERE owner_id=? AND id=? AND deleted=0',
              [observed, key, id]);
          onSyncNeeded?.call();
        }));
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
        sourceName: row['source_name'] as String? ?? '',
        automationMusicEligible: row['automation_music'] == 1,
        downloadProfile: row['download_profile'] as String? ?? '',
        contentType: row['content_type'] as String? ?? '',
        music: row['music'] is String
            ? MusicMetadata.fromJson(
                jsonDecode(row['music'] as String) as Map<String, dynamic>)
            : null,
        type: row['type'] as String,
        sizeBytes: row['size_bytes'] as int,
        addedAt: DateTime.fromMillisecondsSinceEpoch(row['added_at'] as int,
            isUtc: true),
        positionSeconds: (row['position_seconds'] as num).toDouble(),
        finished: row['finished'] == 1,
      );
}

/// Notes and favorites remain device-local; bookmarks use durable server IDs.
class DownloadPersonalMediaStore implements PersonalMediaStore {
  DownloadPersonalMediaStore(
      this.repository, this.key, this.revision, this.local);
  final Future<Object?> revision;
  final OfflineDownloadRepository repository;
  final String key;
  final PersonalMediaStore local;
  @override
  String get identity => local.identity;
  @override
  Future<Note?> getNote() => local.getNote();
  @override
  Future<Note> saveNote(String content) => local.saveNote(content);
  @override
  Future<void> deleteNote() => local.deleteNote();
  @override
  Future<bool> getFavorite() => local.getFavorite();
  @override
  Future<bool> toggleFavorite() => local.toggleFavorite();
  @override
  Future<List<MediaBookmark>> listBookmarks() async {
    return repository.syncedBookmarks(key, revision);
  }

  @override
  Future<MediaBookmark> addBookmark(double seconds, String label) async {
    return repository.addSyncedBookmark(key, seconds, label, revision);
  }

  @override
  Future<void> deleteBookmark(int id) async {
    await repository.deleteSyncedBookmark(key, id, revision);
  }
}
