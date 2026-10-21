import 'dart:async';
import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/screens/music_playlists_screen.dart';

class _Client extends PlayerApiClient {
  _Client() : super(dio: Dio());
  bool fail = false;
  int saves = 0, deletes = 0, adds = 0;
  String name = 'Road Trip', comment = 'Saved comment';
  Completer<Map<String, dynamic>>? pending;
  Map<String, dynamic> get data => {
        'playlist_id': 'pl_a',
        'name': name,
        'comment': comment,
        'song_count': 0,
        'duration': 0,
        'songs': <Map<String, dynamic>>[]
      };
  @override
  Future<List<Map<String, dynamic>>> playlists() async => [];
  @override
  Future<Map<String, dynamic>> playlist(String id) async => data;
  @override
  Future<Map<String, dynamic>> createPlaylist(String n, String c) async {
    saves++;
    if (fail) throw StateError('offline');
    if (pending != null) return await pending!.future;
    return data;
  }

  @override
  Future<Map<String, dynamic>> updatePlaylist(
      String id, String n, String c) async {
    saves++;
    if (fail) throw StateError('offline');
    name = n;
    comment = c;
    if (pending != null) return await pending!.future;
    return data;
  }

  @override
  Future<void> deletePlaylist(String id) async {
    deletes++;
    if (fail) throw StateError('offline');
  }

  @override
  Future<List<Map<String, dynamic>>> musicAlbums(
      {String? artist, int? mediaId}) async {
    if (fail) throw StateError('offline');
    return [
      {'album_key': 'al_a', 'title': 'Night Transit', 'artist': 'Aurora Lane'}
    ];
  }

  @override
  Future<void> addPlaylistAlbum(String id, String key) async {
    adds++;
    if (fail) throw StateError('offline');
  }
}

void main() {
  Future<void> mount(WidgetTester t, _Client api, {String? id}) async {
    await t.pumpWidget(ProviderScope(
        overrides: [apiClientProvider.overrideWithValue(api)],
        child: MaterialApp(home: MusicPlaylistsScreen(playlistId: id))));
    await t.pumpAndSettle();
  }

  testWidgets(
      'empty catalog has create; Unicode codepoint validation and offline form retain input',
      (t) async {
    final api = _Client()..fail = true;
    await mount(t, api);
    expect(find.textContaining('No playlists yet.'), findsOneWidget);
    await t.tap(find.text('Create playlist'));
    await t.pumpAndSettle();
    await t.enterText(find.byType(TextFormField).first, ' ');
    await t.tap(find.widgetWithText(FilledButton, 'Create playlist').last);
    await t.pumpAndSettle();
    expect(find.text('Enter 1–200 characters.'), findsOneWidget);
    expect(api.saves, 0);
    final name = List.filled(200, '🌙').join();
    await t.enterText(find.byType(TextFormField).first, name);
    await t.enterText(find.byType(TextFormField).last, 'r/h/l/? 東京');
    await t.tap(find.widgetWithText(FilledButton, 'Create playlist').last);
    await t.pumpAndSettle();
    expect(api.saves, 1);
    expect(find.textContaining('Could not save playlist.'), findsOneWidget);
    expect(find.widgetWithText(TextFormField, name), findsOneWidget);
    expect(find.widgetWithText(TextFormField, 'r/h/l/? 東京'), findsOneWidget);
    await t.tap(find.text('Cancel'));
    await t.pumpAndSettle();
    expect(find.byType(AlertDialog), findsNothing);
  });
  testWidgets(
      'detail empty add music Retry and delete Cancel preserve document',
      (t) async {
    final api = _Client();
    await mount(t, api, id: 'pl_a');
    expect(find.text('This playlist is empty.'), findsOneWidget);
    expect(
        t
            .widget<FilledButton>(
                find.widgetWithText(FilledButton, 'Play playlist'))
            .onPressed,
        isNull);
    api.fail = true;
    await t.tap(find.text('Add music'));
    await t.pumpAndSettle();
    expect(find.textContaining('Could not load albums.'), findsOneWidget);
    api.fail = false;
    await t.tap(find.text('Retry'));
    await t.pumpAndSettle();
    expect(find.text('Night Transit'), findsOneWidget);
    await t.tap(find.text('Night Transit'));
    await t.pumpAndSettle();
    expect(api.adds, 1);
    await t.tap(find.text('Delete playlist'));
    await t.pumpAndSettle();
    expect(find.textContaining('Music files stay available'), findsOneWidget);
    await t.tap(find.text('Cancel'));
    await t.pumpAndSettle();
    expect(api.deletes, 0);
    expect(find.text('Saved comment'), findsOneWidget);
  });
  testWidgets('edit preserves membership metadata and saves name/comment',
      (t) async {
    final api = _Client();
    await mount(t, api, id: 'pl_a');
    await t.tap(find.text('Edit playlist'));
    await t.pumpAndSettle();
    await t.enterText(find.byType(TextFormField).first, 'Renamed 東京');
    await t.enterText(find.byType(TextFormField).last, 'New comment');
    await t.tap(find.text('Save changes'));
    await t.pumpAndSettle();
    expect(api.name, 'Renamed 東京');
    expect(api.comment, 'New comment');
    expect(find.text('New comment'), findsOneWidget);
  });
}
