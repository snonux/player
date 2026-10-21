import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/services/music_collections.dart';

class _Api extends PlayerApiClient {
  _Api() : super(dio: Dio());
  bool fail = false;
  void Function()? completed;
  Map<String, dynamic> song(int id) =>
      {'id': id, 'set_id': 1, 'type': 'audio', 'file_name': '$id.mp3'};
  @override
  Future<Map<String, dynamic>> musicAlbum(String key) async {
    if (fail && key == 'b') throw StateError('offline');
    completed?.call();
    return {
      'songs': (key == 'a' ? [2, 1] : [3, 2]).map(song).toList(),
      'song_credits': {
        '1': [
          {'artist_key': 'artist'}
        ],
        '2': [
          {'artist_key': 'other'}
        ],
        '3': [
          {'artist_key': 'artist'}
        ]
      }
    };
  }

  @override
  Future<Map<String, dynamic>> playlist(String key) async => {
        'songs': [1, 2, 1].map(song).toList()
      };
  @override
  Future<Map<String, dynamic>> musicArtist(String key) async => {
        'albums': [
          {'album_key': 'a'},
          {'album_key': 'b'},
          {'album_key': 'a'}
        ]
      };
}

void main() {
  test('catalog order retains overlap and repeated playlist occurrences',
      () async {
    final api = _Api();
    final albums = await resolveMusicCollections(
        api,
        [
          const MusicCollection('album', 'a'),
          const MusicCollection('album', 'b')
        ],
        () => true);
    expect(albums.map((m) => m.id), [2, 1, 3, 2]);
    final lists = await resolveMusicCollections(
        api,
        [
          const MusicCollection('playlist', 'p'),
          const MusicCollection('playlist', 'q')
        ],
        () => true);
    expect(lists.map((m) => m.id), [1, 2, 1, 1, 2, 1]);
  });
  test(
      'artist credits omit unrelated compilation songs and dedupe only that artist',
      () async {
    final songs = await resolveMusicCollections(
        _Api(),
        [
          const MusicCollection('artist', 'artist'),
          const MusicCollection('album', 'a')
        ],
        () => true);
    expect(songs.map((m) => m.id), [1, 3, 2, 1]);
  });
  test(
      'failed second collection and superseded scope never return partial batch',
      () async {
    await expectLater(
        resolveMusicCollections(
            _Api()..fail = true,
            [
              const MusicCollection('album', 'a'),
              const MusicCollection('album', 'b')
            ],
            () => true),
        throwsStateError);
    bool current = true;
    final api = _Api()..completed = () => current = false;
    await expectLater(
        resolveMusicCollections(
            api, [const MusicCollection('album', 'a')], () => current),
        throwsStateError);
  });
}
