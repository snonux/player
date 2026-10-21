import '../api/player_api_client.dart';
import '../models/media.dart';

/// A catalog identity, resolved using the currently authenticated API client.
class MusicCollection {
  const MusicCollection(this.kind, this.key);
  final String kind, key;
}

/// Expands a batch in supplied catalog order, retaining playlist occurrences.
Future<List<Media>> resolveMusicCollections(PlayerApiClient api,
    List<MusicCollection> collections, bool Function() current) async {
  final songs = <Media>[];
  void check() {
    if (!current()) throw StateError('Collection action cancelled');
  }

  for (final collection in collections) {
    check();
    if (collection.kind == 'album' || collection.kind == 'playlist') {
      final detail = collection.kind == 'album'
          ? await api.musicAlbum(collection.key)
          : await api.playlist(collection.key);
      check();
      songs.addAll((detail['songs'] as List)
          .cast<Map<String, dynamic>>()
          .map(Media.fromJson));
    } else if (collection.kind == 'artist') {
      final artist = await api.musicArtist(collection.key);
      check();
      final seen = <int>{};
      for (final album
          in (artist['albums'] as List).cast<Map<String, dynamic>>()) {
        final detail = await api.musicAlbum(album['album_key'] as String);
        check();
        final credits = detail['song_credits'] as Map? ?? {};
        for (final json
            in (detail['songs'] as List).cast<Map<String, dynamic>>()) {
          final song = Media.fromJson(json);
          final people = (credits['${song.id}'] as List? ?? [])
              .cast<Map<String, dynamic>>();
          if (!seen.contains(song.id) &&
              people.any((p) => p['artist_key'] == collection.key)) {
            seen.add(song.id);
            songs.add(song);
          }
        }
      }
    } else {
      throw ArgumentError('Unknown collection');
    }
  }
  return songs;
}
