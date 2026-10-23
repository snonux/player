import 'dart:convert';

import '../models/offline_media.dart';
import 'music_collections.dart';
import 'offline_collection_pins.dart';
import 'offline_download_repository.dart';

class OfflineMusicGroup {
  const OfflineMusicGroup(this.title, this.songs, this.total);
  final String title;
  final List<OfflineMedia> songs;
  final int total;
}

String offlineMusicAccount(OfflineMedia item) =>
    jsonEncode([item.origin, item.userId]);

/// Local metadata only. Play revalidates every file through the download repo.
Future<List<OfflineMusicGroup>> offlineMusicGroups(
    OfflineDownloadRepository repository,
    List<OfflineMedia> items,
    String account,
    String kind) async {
  final songs = items
      .where((s) => s.type == 'audio' && offlineMusicAccount(s) == account)
      .toList();
  if (kind == 'Songs') {
    return songs.map((s) => OfflineMusicGroup(s.title, [s], 1)).toList();
  }
  if (kind == 'Playlists') {
    final identity = jsonDecode(account) as List;
    final pins = OfflineCollectionPins(repository);
    final result = <OfflineMusicGroup>[];
    for (final row in await pins.collections()) {
      if (row['kind'] != 'playlist' ||
          row['origin'] != identity[0] ||
          row['user_id'] != identity[1]) {
        continue;
      }
      final pin = await pins.status(identity[0] as String, identity[1] as int,
          MusicCollection('playlist', row['collection_key'] as String));
      if (pin != null) {
        result.add(OfflineMusicGroup(
            pin.title,
            pin.mediaIds
                .map((id) =>
                    songs.where((song) => song.mediaId == id).firstOrNull)
                .whereType<OfflineMedia>()
                .toList(),
            pin.mediaIds.length));
      }
    }
    return result;
  }
  final groups = <String, List<OfflineMedia>>{};
  for (final song in songs) {
    final music = song.music;
    final title = kind == 'Artists'
        ? (music?.artist.isNotEmpty == true ? music!.artist : 'Unknown artist')
        : (music?.album.isNotEmpty == true ? music!.album : 'Unknown album');
    final artist =
        music?.toJson()['album_artist'] as String? ?? music?.artist ?? '';
    final key = kind == 'Albums' ? jsonEncode([title, artist]) : title;
    groups.putIfAbsent(key, () => []).add(song);
  }
  return groups.values.map((members) {
    members.sort((a, b) {
      final x = a.music?.toJson() ?? {}, y = b.music?.toJson() ?? {};
      for (final key in ['disc', 'track']) {
        final order = ((x[key] as int?) ?? 0).compareTo((y[key] as int?) ?? 0);
        if (order != 0) return order;
      }
      return a.title.compareTo(b.title);
    });
    final first = members.first;
    final title = kind == 'Artists'
        ? (first.music?.artist.isNotEmpty == true
            ? first.music!.artist
            : 'Unknown artist')
        : (first.music?.album.isNotEmpty == true
            ? first.music!.album
            : 'Unknown album');
    return OfflineMusicGroup(title, members, members.length);
  }).toList()
    ..sort((a, b) => a.title.toLowerCase().compareTo(b.title.toLowerCase()));
}

bool offlineMusicMatches(OfflineMusicGroup group, String query) {
  final text = [
    group.title,
    ...group.songs
        .expand((s) => [s.title, s.music?.artist ?? '', s.music?.album ?? ''])
  ].join(' ').toLowerCase();
  return query.toLowerCase().split(RegExp(r'\s+')).every(text.contains);
}
