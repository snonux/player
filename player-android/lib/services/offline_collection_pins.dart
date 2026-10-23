import 'dart:convert';

import '../models/media.dart';
import '../models/offline_media.dart';
import 'music_collections.dart';
import 'offline_download_repository.dart';
import 'playback_request.dart';

class OfflineCollectionPin {
  const OfflineCollectionPin(
      this.title, this.mediaIds, this.pinned, this.saved);
  final String title;
  final List<int> mediaIds;
  final bool pinned;
  final List<OfflineMedia> saved;
  bool get complete => mediaIds.isNotEmpty && saved.length == mediaIds.length;
}

/// Collection snapshots reference the existing durable download files; cache
/// budgets and cache location changes never touch this table or these bytes.
class OfflineCollectionPins {
  OfflineCollectionPins(this.repository);
  final OfflineDownloadRepository repository;

  Future<void> _schema() async => (await repository.database).execute('''
    CREATE TABLE IF NOT EXISTS music_offline_pins (
      origin TEXT NOT NULL,user_id INTEGER NOT NULL,kind TEXT NOT NULL,
      collection_key TEXT NOT NULL,title TEXT NOT NULL,media_ids TEXT NOT NULL,
      pinned INTEGER NOT NULL,PRIMARY KEY(origin,user_id,kind,collection_key))''');

  Future<List<Map<String, Object?>>> collections() async {
    await _schema();
    return (await repository.database)
        .query('music_offline_pins', orderBy: 'title COLLATE NOCASE');
  }

  Future<OfflineCollectionPin?> status(
      String origin, int userId, MusicCollection collection) async {
    await _schema();
    final rows = await (await repository.database).query('music_offline_pins',
        where: 'origin=? AND user_id=? AND kind=? AND collection_key=?',
        whereArgs: [origin, userId, collection.kind, collection.key]);
    if (rows.isEmpty) return null;
    final row = rows.single;
    final ids = (jsonDecode(row['media_ids'] as String) as List).cast<int>();
    final saved = <OfflineMedia>[];
    for (final id in ids) {
      final item = await repository.get(offlineMediaKey(origin, userId, id));
      if (item == null) continue;
      final file = repository.file(item.key);
      if (await file.exists() && await file.length() == item.sizeBytes) {
        saved.add(item);
      }
    }
    return OfflineCollectionPin(
        row['title'] as String, ids, row['pinned'] == 1, saved);
  }

  Future<void> remember(String origin, int userId, MusicCollection collection,
      String title, List<Media> songs, bool Function() authorized) async {
    if (!['album', 'playlist'].contains(collection.kind) || userId <= 0) {
      throw ArgumentError('Choose an album or playlist for this account.');
    }
    final ids = songs.where((s) => s.type == 'audio').map((s) => s.id).toSet();
    if (ids.isEmpty) throw StateError('No songs available.');
    await _schema();
    if (!authorized()) throw StateError('Account changed.');
    await (await repository.database).rawInsert(
        '''
      INSERT INTO music_offline_pins(origin,user_id,kind,collection_key,title,media_ids,pinned)
      VALUES(?,?,?,?,?,?,1) ON CONFLICT(origin,user_id,kind,collection_key)
      DO UPDATE SET title=excluded.title,media_ids=excluded.media_ids,pinned=1''',
        [
          origin,
          userId,
          collection.kind,
          collection.key,
          title,
          jsonEncode(ids.toList())
        ]);
  }

  /// Unpin leaves manual and overlapping downloads intact. Explicit individual
  /// Remove download in Downloads may remove a file and makes this pin partial.
  Future<void> unpin(
      String origin, int userId, MusicCollection collection) async {
    await _schema();
    await (await repository.database).update(
        'music_offline_pins', {'pinned': 0},
        where: 'origin=? AND user_id=? AND kind=? AND collection_key=?',
        whereArgs: [origin, userId, collection.kind, collection.key]);
  }

  Future<List<OfflinePlaybackRequest>> playback(
      String origin, int userId, MusicCollection collection) async {
    final pin = await status(origin, userId, collection);
    if (pin == null || !pin.complete) {
      throw StateError('Some downloads are missing. Retry pinning first.');
    }
    return Future.wait(
        pin.saved.map((item) => repository.playbackRequest(item.key)));
  }
}
