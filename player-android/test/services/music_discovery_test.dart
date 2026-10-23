import 'dart:math';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/music_discovery.dart';

class _ZeroRandom implements Random {
  @override
  int nextInt(int max) => 0;
  @override
  double nextDouble() => 0;
  @override
  bool nextBool() => false;
}

void main() {
  test(
      'album collections use personal activity and preserve catalog identities',
      () {
    final albums = [
      {
        'album_key': 'a',
        'title': 'A',
        'added_at': '2020-01-01',
        'last_played_at': '2020-01-01',
        'personal_play_count': 9,
        'rating': 2
      },
      {
        'album_key': 'b',
        'title': 'B',
        'added_at': '2025-01-01',
        'last_played_at': '2025-01-01',
        'personal_play_count': 2,
        'rating': 5,
        'is_favorite': true
      },
      {
        'album_key': 'c',
        'title': 'C',
        'added_at': '2020-01-01',
        'play_count': 1000
      },
    ];
    final expected = {
      'recently_added': ['b', 'a', 'c'],
      'recently_played': ['b', 'a'],
      'most_played': ['a', 'b'],
      'favorites': ['b'],
      'liked_mix': ['b'],
      'rediscover': ['a', 'b'],
      'top_rated': ['b', 'a']
    };
    for (final entry in expected.entries) {
      expect(
          discoverAlbums(albums, entry.key).map((a) => a['album_key']).toList(),
          entry.value,
          reason: entry.key);
    }
    final random = discoverAlbums(albums, 'random', random: _ZeroRandom());
    expect(random.map((a) => a['album_key']).toList(), ['b', 'c', 'a'],
        reason: 'zero RNG must perform the known Fisher–Yates permutation');
    expect(albums.map((a) => a['album_key']).toList(), ['a', 'b', 'c']);
    expect(discoverAlbums([], 'top_rated'), isEmpty);
  });
}
