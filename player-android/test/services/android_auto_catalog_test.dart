import 'dart:async';
import 'dart:convert';

import 'package:audio_service/audio_service.dart';
import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/models/media.dart';
import 'package:player_android/services/android_auto_catalog.dart';
import 'package:player_android/services/music_queue.dart';

Map<String, dynamic> song(int id) => {
      'id': id,
      'type': 'audio',
      'file_name': '$id.mp3',
      'music': {'title': 'Track $id', 'artist': 'Artist'}
    };
Map<String, dynamic> albumRow(int id) =>
    {'album_key': 'al_$id', 'title': 'Album $id', 'artist': 'Artist'};

class Api extends PlayerApiClient {
  Api() : super(dio: Dio());
  int calls = 0;
  bool fail = false;
  Completer<void>? wait;
  Completer<void>? detailWait;
  List<Map<String, dynamic>> songs = [song(7), song(3), song(7)];
  @override
  Future<List<Map<String, dynamic>>> musicAlbums(
      {String? artist, int? mediaId}) async {
    calls++;
    if (fail) throw StateError('private detail');
    if (wait != null) await wait!.future;
    return List.generate(43, albumRow);
  }

  @override
  Future<Map<String, dynamic>> musicAlbum(String key) async {
    if (detailWait != null) await detailWait!.future;
    return {'title': 'Album', 'songs': songs};
  }

  @override
  Future<Map<String, dynamic>> playlist(String id) async =>
      {'name': 'Playlist', 'songs': songs};
  @override
  Future<List<Map<String, dynamic>>> playlists() async => [
        {'playlist_id': 'pl_1', 'name': 'Playlist'}
      ];
  @override
  Future<Map<String, dynamic>> musicArtist(String key) async => {
        'albums': [albumRow(1)]
      };
  @override
  Future<List<Map<String, dynamic>>> musicArtists(
          {String role = 'artists'}) async =>
      [
        {'artist_key': 'ar_1', 'name': 'Artist'}
      ];
  @override
  Future<Map<String, dynamic>> musicSearch(
      {String query = '', String artist = '', String album = ''}) async {
    calls++;
    return {
      'songs': List.generate(30, (i) => song(i + 1)),
      'albums': List.generate(30, albumRow),
      'artists':
          List.generate(30, (i) => {'artist_key': 'ar_$i', 'name': 'Artist $i'})
    };
  }

  @override
  Future<Media> getMedia(int id, {CancelToken? cancelToken}) async =>
      Media.fromJson(song(id));
}

void main() {
  late Api api;
  late AutoAccount? account;
  late AndroidAutoCatalog catalog;
  late bool current;
  late List<int> played;
  late int selectedIndex;
  setUp(() {
    api = Api();
    selectedIndex = -1;
    current = true;
    played = [];
    account = AutoAccount(api, 'http://server', 1, 2, () => current);
    catalog = AndroidAutoCatalog(
        account: () => account,
        queue: () => const MusicQueueState(
            entries: [],
            index: -1,
            label: '',
            active: false,
            busy: false,
            canUndo: false,
            status: ''),
        playOccurrence: (_) async {},
        playSongs: (_, songs, index, label) async {
          played = songs.map((s) => s.id).toList();
          selectedIndex = index;
        });
  });

  test('locked root reveals no library and performs no request', () async {
    account = null;
    final rows = await catalog.children(AudioService.browsableRootId);
    expect(rows.single.title, contains('sign in or unlock'));
    expect(api.calls, 0);
  });
  test('root has four browsable tabs and no credentials or URLs', () async {
    final rows = await catalog.children(AudioService.browsableRootId);
    expect(
        rows.map((e) => e.title), ['Albums', 'Artists', 'Playlists', 'Queue']);
    expect(rows.every((e) => e.playable == false && e.artUri == null), isTrue);
    final decoded = utf8.decode(base64Url.decode(rows.first.id.substring(5)));
    expect(decoded, isNot(contains('http')));
  });
  test('browse pages expose More and preserve catalog order', () async {
    final root = await catalog.children(AudioService.browsableRootId);
    final first = await catalog.children(root.first.id);
    expect(first, hasLength(41));
    expect(first.last.title, 'More…');
    final second = await catalog.children(first.last.id);
    expect(second.map((e) => e.title), ['Album 40', 'Album 41', 'Album 42']);
  });
  test('artist resolves album detail and playlist retains repeated occurrences',
      () async {
    final root = await catalog.children(AudioService.browsableRootId);
    final artists = await catalog.children(root[1].id);
    final albums = await catalog.children(artists.first.id);
    expect(albums.single.title, 'Album 1');
    final playlists = await catalog.children(root[2].id);
    final tracks = await catalog.children(playlists.first.id);
    await catalog.play(tracks.first.id);
    expect(played, [7, 3, 7]);
    expect(tracks[1].id, isNot(tracks[3].id));
    await catalog.play(tracks[2].id);
    expect(played, [7, 3, 7]);
    expect(selectedIndex, 1);
  });
  test('changed playlist ordinal cannot start a different canonical song',
      () async {
    final root = await catalog.children(AudioService.browsableRootId);
    final playlists = await catalog.children(root[2].id);
    final tracks = await catalog.children(playlists.first.id);
    api.songs = [song(3), song(7)];
    await expectLater(catalog.play(tracks[1].id), throwsStateError);
    expect(played, isEmpty);
  });
  test('host IDs expire across account and credential changes', () async {
    final old = (await catalog.children(AudioService.browsableRootId)).first.id;
    account = AutoAccount(api, 'http://server', 1, 3, () => true);
    expect(await catalog.children(old), isEmpty);
    await expectLater(catalog.play(old), throwsStateError);
    expect(api.calls, 0);
  });
  test('late browse results are discarded on lock', () async {
    final id = (await catalog.children(AudioService.browsableRootId)).first.id;
    api.wait = Completer<void>();
    final result = catalog.children(id);
    current = false;
    api.wait!.complete();
    expect(await result, isEmpty);
  });
  test('failure is concise and contains no server exception details', () async {
    final id = (await catalog.children(AudioService.browsableRootId)).first.id;
    api.fail = true;
    final rows = await catalog.children(id);
    expect(rows.single.title, contains('retry'));
    expect(rows.single.title, isNot(contains('private detail')));
  });
  test('search and voice play are bounded and resolve canonical media',
      () async {
    final rows = await catalog.search('Track');
    expect(rows, hasLength(40));
    await catalog.playSearch('Track');
    expect(played, [1]);
    final calls = api.calls;
    expect(await catalog.search(' '), isEmpty);
    expect(await catalog.search('x' * 201), isEmpty);
    expect(api.calls, calls);
  });
  test('large collection states play limit and bounds actual queue', () async {
    api.songs = List.generate(1100, (i) => song(i + 1));
    final albums =
        await catalog.children((await catalog.children('root')).first.id);
    final tracks = await catalog.children(albums.first.id);
    expect(tracks.first.title, 'Play first 1000 songs');
    await catalog.play(tracks.first.id);
    expect(played.length, 1000);
  });
  test('pending collection play cannot override a later source or pause',
      () async {
    var transportCurrent = true;
    account = AutoAccount(api, 'http://server', 1, 2, () => current,
        playbackCurrent: () => transportCurrent);
    final albums =
        await catalog.children((await catalog.children('root')).first.id);
    final tracks = await catalog.children(albums.first.id);
    api.detailWait = Completer<void>();
    final play = catalog.play(tracks.first.id);
    transportCurrent = false;
    api.detailWait!.complete();
    await play;
    expect(played, isEmpty);
  });
  test('later Auto play intent wins over a slow old collection selection',
      () async {
    final albums =
        await catalog.children((await catalog.children('root')).first.id);
    final tracks = await catalog.children(albums.first.id);
    final search = await catalog.search('Track');
    api.detailWait = Completer<void>();
    final old = catalog.play(tracks.first.id);
    await catalog.play(search.first.id);
    expect(played, [1]);
    api.detailWait!.complete();
    await old;
    expect(played, [1]);
  });
}
