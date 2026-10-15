import 'package:sqflite/sqflite.dart';

import '../api/player_api_client.dart';
import '../models/media_bookmark.dart';
import '../models/note.dart';

/// Bound to one durable item. Device stores never hold account credentials.
abstract interface class PersonalMediaStore {
  String get identity;
  Future<Note?> getNote();
  Future<Note> saveNote(String content);
  Future<void> deleteNote();
  Future<List<MediaBookmark>> listBookmarks();
  Future<MediaBookmark> addBookmark(double seconds, String label);
  Future<void> deleteBookmark(int id);
  Future<bool> getFavorite();
  Future<bool> toggleFavorite();
}

class ServerPersonalMediaStore implements PersonalMediaStore {
  ServerPersonalMediaStore(this.client, this.mediaId, this.identity,
      {this.isCurrent});
  final PlayerApiClient client;
  final int mediaId;
  @override
  final String identity;
  final bool Function()? isCurrent;

  Future<T> _run<T>(Future<T> Function() action) async {
    if (isCurrent?.call() == false) {
      throw StateError('This account has changed.');
    }
    final result = await action();
    if (isCurrent?.call() == false) {
      throw StateError('This account has changed.');
    }
    return result;
  }

  @override
  Future<Note?> getNote() => _run(() => client.getNote(mediaId));
  @override
  Future<Note> saveNote(String content) =>
      _run(() => client.upsertNote(mediaId, content));
  @override
  Future<void> deleteNote() => _run(() => client.deleteNote(mediaId));
  @override
  Future<List<MediaBookmark>> listBookmarks() =>
      _run(() => client.listBookmarks(mediaId));
  @override
  Future<MediaBookmark> addBookmark(double seconds, String label) =>
      _run(() => client.addBookmark(mediaId, seconds, label));
  @override
  Future<void> deleteBookmark(int id) =>
      _run(() => client.deleteBookmark(mediaId, id));
  @override
  Future<bool> getFavorite() =>
      _run(() async => (await client.getMedia(mediaId)).favorite);
  @override
  Future<bool> toggleFavorite() => _run(() => client.toggleFavorite(mediaId));
}

/// Installed in each owning database: the foreign key gives removal the same
/// transaction boundary as the media/progress row. A relink keeps the owner ID.
Future<void> createPersonalMediaSchema(
  DatabaseExecutor db, {
  required String ownerTable,
  required String ownerColumn,
  required String keyType,
}) async {
  await db.execute('''CREATE TABLE personal_media (
    owner_id $keyType PRIMARY KEY, content TEXT, favorite INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (owner_id) REFERENCES $ownerTable($ownerColumn) ON DELETE CASCADE
  )''');
  await db.execute('''CREATE TABLE personal_bookmarks (
    id INTEGER PRIMARY KEY AUTOINCREMENT, owner_id $keyType NOT NULL,
    position_seconds REAL NOT NULL, label TEXT NOT NULL, created_at INTEGER NOT NULL,
    FOREIGN KEY (owner_id) REFERENCES $ownerTable($ownerColumn) ON DELETE CASCADE
  )''');
  await db.execute(
      'CREATE INDEX personal_bookmark_owner ON personal_bookmarks(owner_id, position_seconds, id)');
}

typedef PersonalWrite = Future<T> Function<T>(
    Future<T> Function(Transaction) action);

class SqlitePersonalMediaStore implements PersonalMediaStore {
  SqlitePersonalMediaStore(
      {required this.database,
      required this.write,
      required this.ownerTable,
      required this.ownerColumn,
      required this.ownerId,
      required this.identity,
      this.bookmarksAllowed = true,
      this.durationColumn,
      this.ownerRevision});
  final Future<Database> Function() database;
  final PersonalWrite write;
  final String ownerTable;
  final String ownerColumn;
  final Object ownerId;
  final bool bookmarksAllowed;
  final String? durationColumn;
  final Future<Object?>? ownerRevision;
  @override
  final String identity;

  Future<Map<String, Object?>> _owner(DatabaseExecutor db) async {
    final rows = await db
        .query(ownerTable, where: '$ownerColumn = ?', whereArgs: [ownerId]);
    if (rows.isEmpty) throw StateError('This item has been removed.');
    if (ownerRevision != null &&
        rows.single['instance_id'] != await ownerRevision) {
      throw StateError('This offline copy has been replaced.');
    }
    return rows.single;
  }

  Future<List<Map<String, Object?>>> _personal(DatabaseExecutor db) =>
      db.query('personal_media', where: 'owner_id = ?', whereArgs: [ownerId]);
  @override
  Future<Note?> getNote() async {
    final db = await database();
    await _owner(db);
    final rows = await _personal(db);
    final content = rows.isEmpty ? null : rows.single['content'] as String?;
    return content == null ? null : _note(content);
  }

  Note _note(String content) => Note(
      id: 0,
      mediaId: ownerId is int ? ownerId as int : 0,
      userId: 0,
      content: content);
  Future<void> _ensurePersonal(Transaction db) async {
    await _owner(db);
    await db.insert('personal_media', {'owner_id': ownerId},
        conflictAlgorithm: ConflictAlgorithm.ignore);
  }

  @override
  Future<Note> saveNote(String content) => write((db) async {
        await _ensurePersonal(db);
        await db.update('personal_media', {'content': content},
            where: 'owner_id = ?', whereArgs: [ownerId]);
        return _note(content);
      });
  @override
  Future<void> deleteNote() => write((db) async {
        await _owner(db);
        await db.update('personal_media', {'content': null},
            where: 'owner_id = ?', whereArgs: [ownerId]);
      });
  @override
  Future<bool> getFavorite() async {
    final db = await database();
    await _owner(db);
    final rows = await _personal(db);
    return rows.isNotEmpty && rows.single['favorite'] == 1;
  }

  @override
  Future<bool> toggleFavorite() => write((db) async {
        await _ensurePersonal(db);
        final favorite = (await _personal(db)).single['favorite'] != 1;
        await db.update('personal_media', {'favorite': favorite ? 1 : 0},
            where: 'owner_id = ?', whereArgs: [ownerId]);
        return favorite;
      });
  @override
  Future<List<MediaBookmark>> listBookmarks() async {
    final db = await database();
    await _owner(db);
    return (await db.query('personal_bookmarks',
            where: 'owner_id = ?',
            whereArgs: [ownerId],
            orderBy: 'position_seconds, id'))
        .map(_bookmark)
        .toList();
  }

  @override
  Future<MediaBookmark> addBookmark(double seconds, String label) {
    final trimmed = label.trim();
    if (!validBookmarkPosition(seconds) || trimmed.runes.length > 200) {
      throw ArgumentError(
          'Use a valid position and a label of at most 200 characters.');
    }
    return write((db) async {
      final owner = await _owner(db);
      if (!bookmarksAllowed ||
          owner['type'] == 'image' ||
          (owner['mime_type'] as String? ?? '').startsWith('image/')) {
        throw StateError('Images use favorites instead of position bookmarks.');
      }
      final durationMs =
          durationColumn == null ? null : owner[durationColumn!] as int?;
      if (durationMs != null && durationMs > 0 && seconds > durationMs / 1000) {
        throw ArgumentError('The bookmark is past the end of this file.');
      }
      final created = DateTime.now().toUtc();
      final id = await db.insert('personal_bookmarks', {
        'owner_id': ownerId,
        'position_seconds': seconds,
        'label': trimmed,
        'created_at': created.millisecondsSinceEpoch
      });
      return MediaBookmark(
          id: id, positionSeconds: seconds, label: trimmed, createdAt: created);
    });
  }

  @override
  Future<void> deleteBookmark(int id) => write((db) async {
        await _owner(db);
        await db.delete('personal_bookmarks',
            where: 'owner_id = ? AND id = ?', whereArgs: [ownerId, id]);
      });
  MediaBookmark _bookmark(Map<String, Object?> row) => MediaBookmark(
      id: row['id'] as int,
      positionSeconds: (row['position_seconds'] as num).toDouble(),
      label: row['label'] as String,
      createdAt: DateTime.fromMillisecondsSinceEpoch(row['created_at'] as int,
          isUtc: true));
}
