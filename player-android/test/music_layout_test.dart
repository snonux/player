import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/widgets/music_layout_panel.dart';

void main() {
  test('year direction keeps unknown metadata last and preserves album order',
      () {
    final albums = <Map<String, dynamic>>[
      {'album_key': 'old', 'year': 1990},
      {'album_key': 'unknown'},
      {'album_key': 'new', 'year': 2020},
    ];
    final layout = MusicLayout('albums', {'sort': 'year', 'descending': true});
    expect(layout.sorted(albums).map((a) => a['album_key']),
        ['new', 'old', 'unknown']);
    expect(albums.map((a) => a['album_key']), ['old', 'unknown', 'new']);
  });
  test('song artist sorting uses metadata and serializes column choices', () {
    final layout = MusicLayout('songs', {
      'sort': 'artist',
      'columns': ['year', 'invalid']
    });
    final songs = <Map<String, dynamic>>[
      {
        'id': 2,
        'music': {'artist': 'Z'}
      },
      {
        'id': 1,
        'music': {'artist': 'A'}
      },
    ];
    expect(layout.sorted(songs).map((s) => s['id']), [1, 2]);
    expect(MusicLayout('songs', layout.toJson()).columns, ['year']);
    expect(MusicLayout('songs', {'sort': 'rating'}).sort, 'rating');
    expect(MusicLayout('songs', {'columns': []}).columns, isEmpty);
  });
}
