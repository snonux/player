import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/models/music_metadata.dart';
import 'package:player_android/models/offline_media.dart';
import 'package:player_android/services/offline_download_repository.dart';
import 'package:player_android/services/offline_music_library.dart';

void main() {
  test('local groups retain artist identity, track order and account isolation',
      () async {
    OfflineMedia song(int id, String artist, int track, {int user = 1}) =>
        OfflineMedia(
            key: '$id',
            origin: 'http://server.test',
            userId: user,
            mediaId: id,
            title: 'Song $id',
            type: 'audio',
            sizeBytes: 3,
            addedAt: DateTime(2026),
            music: MusicMetadata.fromJson(
                {'album': 'Same title', 'artist': artist, 'track': track}));
    final items = [
      song(1, 'A', 2),
      song(2, 'A', 1),
      song(3, 'B', 1),
      song(4, 'Private', 1, user: 2)
    ];
    final repository = OfflineDownloadRepository();
    final albums = await offlineMusicGroups(
        repository, items, offlineMusicAccount(items.first), 'Albums');
    expect(albums.length, 2);
    expect(albums.first.songs.map((s) => s.mediaId), [2, 1]);
    expect(albums.expand((a) => a.songs).any((s) => s.userId == 2), false);
    expect(offlineMusicMatches(albums.first, 'A Song 2'), true);
    expect(offlineMusicMatches(albums.first, 'Private'), false);
  });
}
