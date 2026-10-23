import 'dart:convert';

import 'package:audio_service/audio_service.dart';
import 'package:crypto/crypto.dart';

import '../api/player_api_client.dart';
import '../models/media.dart';
import 'music_queue.dart';

/// An unlocked account captured for one browser operation. IDs contain no URLs,
/// tokens or paths, and cannot be replayed against another account/credential.
class AutoAccount {
  AutoAccount(this.api, String origin, int userId, int epoch, this.current,
      {bool Function()? playbackCurrent})
      : playbackCurrent = playbackCurrent ?? current,
        scope =
            sha256.convert(utf8.encode('$origin|$userId|$epoch')).toString();
  final PlayerApiClient api;
  final String scope;
  final bool Function() current;
  final bool Function() playbackCurrent;
}

typedef AutoPlaySongs = Future<void> Function(
    AutoAccount account, List<Media> songs, int index, String label);

/// Android Auto's host owns layout, focus, voice input and transport chrome.
/// This adapter supplies only safe, paged catalog rows and explicit play intent.
class AndroidAutoCatalog {
  AndroidAutoCatalog(
      {required this.account,
      required this.queue,
      required this.playSongs,
      required this.playOccurrence});

  final AutoAccount? Function() account;
  final MusicQueueState Function() queue;
  final AutoPlaySongs playSongs;
  final Future<void> Function(int occurrence) playOccurrence;
  int _playIntent = 0;
  static const requestTimeout = Duration(seconds: 15);
  static const pageSize = 40;
  static const maxQueueSongs = 1000;

  bool isQueueParent(String parent) {
    final a = account();
    return a != null && _decode(a, parent)?[1] == 'queue';
  }

  String _id(AutoAccount a, String kind, String key,
          [int index = 0, int media = 0]) =>
      'auto:${base64Url.encode(utf8.encode(jsonEncode([
            a.scope,
            kind,
            key,
            index,
            media
          ])))}';

  List<dynamic>? _decode(AutoAccount a, String id) {
    if (!id.startsWith('auto:') || id.length > 4096) return null;
    try {
      final value = jsonDecode(utf8.decode(base64Url.decode(id.substring(5))));
      if (value is List &&
          value.length == 5 &&
          value[0] == a.scope &&
          value[1] is String &&
          value[2] is String &&
          value[3] is int &&
          value[3] >= 0 &&
          value[3] <= 1000000 &&
          value[4] is int) {
        return value;
      }
    } catch (_) {/* Foreign/stale host IDs are never URLs to resolve. */}
    return null;
  }

  MediaItem _folder(AutoAccount a, String kind, String key, String title,
          {String? subtitle, int page = 0}) =>
      MediaItem(
          id: _id(a, kind, key, page),
          title: title,
          artist: subtitle,
          playable: false);

  MediaItem _message(String text) =>
      MediaItem(id: 'auto:status', title: text, playable: false);

  MediaItem _song(AutoAccount a, Media song, String kind, String key, int at) =>
      MediaItem(
          id: _id(a, kind, key, at, song.id),
          title: song.displayTitle,
          artist: song.music?.artist,
          album: song.music?.album,
          duration: Duration(milliseconds: (song.duration * 1000).round()),
          playable: true);

  List<MediaItem> _page(
      AutoAccount a, List<MediaItem> rows, String kind, String key, int page) {
    final start = page * pageSize;
    final result = rows.skip(start).take(pageSize).toList();
    if (rows.length > start + pageSize) {
      result.add(_folder(a, kind, key, 'More…', page: page + 1));
    }
    return result.isEmpty ? [_message('No music available')] : result;
  }

  List<Media> _songs(Map<String, dynamic> detail) =>
      (detail['songs'] as List? ?? [])
          .cast<Map<String, dynamic>>()
          .map(Media.fromJson)
          .where((s) => s.id > 0 && s.type == 'audio' && s.deletedAt == null)
          .toList();

  Future<List<MediaItem>> children(String parent) async {
    final a = account();
    if (a == null) {
      return [_message('Open Player on your phone to sign in or unlock')];
    }
    try {
      if (parent == AudioService.browsableRootId) {
        return [
          _folder(a, 'albums', '', 'Albums'),
          _folder(a, 'artists', '', 'Artists'),
          _folder(a, 'playlists', '', 'Playlists'),
          _folder(a, 'queue', '', 'Queue'),
        ];
      }
      if (parent == AudioService.recentRootId) return [];
      final id = _decode(a, parent);
      if (id == null) return [];
      final kind = id[1] as String, key = id[2] as String, page = id[3] as int;
      List<MediaItem> rows;
      switch (kind) {
        case 'albums':
          rows = _albums(a, await a.api.musicAlbums().timeout(requestTimeout));
        case 'artists':
          rows = (await a.api.musicArtists().timeout(requestTimeout))
              .map((v) => _folder(
                  a, 'artist', v['artist_key'] as String, v['name'] as String))
              .toList();
        case 'playlists':
          rows = (await a.api.playlists().timeout(requestTimeout))
              .map((v) => _folder(a, 'playlist', v['playlist_id'] as String,
                  v['name'] as String))
              .toList();
        case 'artist':
          rows = _albums(a, await _artistAlbums(a, key));
        case 'album':
        case 'playlist':
          final detail = kind == 'album'
              ? await a.api.musicAlbum(key).timeout(requestTimeout)
              : await a.api.playlist(key).timeout(requestTimeout);
          final songs = _songs(detail);
          rows = [
            if (songs.isNotEmpty)
              MediaItem(
                  id: _id(a, '$kind-all', key),
                  title: songs.length > maxQueueSongs
                      ? 'Play first $maxQueueSongs songs'
                      : 'Play all',
                  playable: true),
            for (var i = 0; i < songs.length; i++)
              _song(a, songs[i], '$kind-song', key, i),
          ];
        case 'queue':
          rows = queueItems(a);
        default:
          return [];
      }
      if (!a.current()) return [];
      return _page(a, rows, kind, key, page);
    } catch (_) {
      return a.current()
          ? [_message('Could not load music. Check your phone and retry.')]
          : [];
    }
  }

  Future<List<Map<String, dynamic>>> _artistAlbums(
          AutoAccount a, String key) async =>
      ((await a.api.musicArtist(key).timeout(requestTimeout))['albums']
                  as List? ??
              [])
          .cast<Map<String, dynamic>>();

  List<MediaItem> _albums(AutoAccount a, List<Map<String, dynamic>> albums) =>
      albums
          .map((v) => _folder(
              a, 'album', v['album_key'] as String, v['title'] as String,
              subtitle: v['artist'] as String?))
          .toList();

  List<MediaItem> queueItems(AutoAccount a) => queue()
      .entries
      .map((entry) => MediaItem(
          id: _id(a, 'occurrence', '${entry.id}'),
          title: entry.request.title,
          artist: entry.request.artist,
          album: entry.request.album,
          extras: {'queueOccurrence': entry.id},
          playable: true))
      .toList();

  Future<List<MediaItem>> search(String query) async {
    final a = account();
    final text = query.trim();
    if (a == null || text.isEmpty || text.length > 200) return [];
    try {
      final found =
          await a.api.musicSearch(query: text).timeout(requestTimeout);
      if (!a.current()) return [];
      final songs = (found['songs'] as List? ?? [])
          .cast<Map<String, dynamic>>()
          .map(Media.fromJson)
          .where((s) => s.type == 'audio' && s.deletedAt == null);
      return [
        ...songs.take(20).map((s) => _song(a, s, 'song', '${s.id}', 0)),
        ..._albums(
            a,
            (found['albums'] as List? ?? [])
                .cast<Map<String, dynamic>>()
                .take(10)
                .toList()),
        ...(found['artists'] as List? ?? [])
            .cast<Map<String, dynamic>>()
            .take(10)
            .map((v) => _folder(
                a, 'artist', v['artist_key'] as String, v['name'] as String)),
      ];
    } catch (_) {
      return a.current()
          ? [_message('Search unavailable. Check your phone and retry.')]
          : [];
    }
  }

  Future<void> play(String mediaId) async {
    final intent = ++_playIntent;
    final a = account();
    if (a == null) {
      throw StateError('Open Player on your phone to sign in or unlock');
    }
    final id = _decode(a, mediaId);
    if (id == null) throw StateError('Music selection expired. Browse again.');
    final kind = id[1] as String, key = id[2] as String;
    if (kind == 'occurrence') {
      final occurrence = int.tryParse(key);
      if (occurrence != null && a.current()) await playOccurrence(occurrence);
      return;
    }
    List<Media> songs;
    var at = 0;
    var label = 'Search';
    if (kind == 'song') {
      songs = [await a.api.getMedia(int.parse(key)).timeout(requestTimeout)];
    } else if (['album-all', 'album-song', 'playlist-all', 'playlist-song']
        .contains(kind)) {
      final album = kind.startsWith('album');
      final detail = album
          ? await a.api.musicAlbum(key).timeout(requestTimeout)
          : await a.api.playlist(key).timeout(requestTimeout);
      songs = _songs(detail);
      label = detail[album ? 'title' : 'name'] as String? ?? 'Music';
      at = kind.endsWith('-song') ? id[3] as int : 0;
    } else {
      throw StateError('Choose a song or Play all.');
    }
    if (intent != _playIntent || !a.current() || !a.playbackCurrent()) return;
    if (songs.isEmpty ||
        at >= songs.length ||
        songs.any((s) => s.type != 'audio' || s.deletedAt != null)) {
      throw StateError('Music is no longer available. Browse again.');
    }
    if ((id[4] as int) > 0 && songs[at].id != id[4]) {
      throw StateError('Music collection changed. Browse again.');
    }
    // Keep the selected song's ordered block, including preceding songs. The
    // queue label states the range when a collection exceeds the native cap.
    final start = (at ~/ maxQueueSongs) * maxQueueSongs;
    final selected = songs.skip(start).take(maxQueueSongs).toList();
    if (songs.length > maxQueueSongs) {
      label = '$label · songs ${start + 1}–${start + selected.length}';
    }
    await playSongs(a, selected, at - start, label);
  }

  Future<void> playSearch(String query) async {
    final intent = ++_playIntent;
    final a = account();
    final results = await search(query);
    if (intent != _playIntent || a == null || !a.playbackCurrent()) return;
    final songs = results.where((item) => item.playable == true);
    if (songs.isEmpty) {
      throw StateError('No matching songs. Search or browse again.');
    }
    await play(songs.first.id);
  }
}
