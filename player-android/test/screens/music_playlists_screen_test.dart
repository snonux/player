import 'package:player_android/providers/current_user_provider.dart';
import 'package:player_android/models/user.dart';
import 'dart:async';
import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/screens/music_playlists_screen.dart';

class _Client extends PlayerApiClient {
  _Client() : super(dio: Dio()) {
    rawDio.interceptors.add(InterceptorsWrapper(onRequest: (options, handler) {
      if (options.path.contains('/music/artwork/')) {
        handler.resolve(Response(
            requestOptions: options,
            statusCode: 200,
            data: {'has_artwork': false, 'can_edit': true, 'revision': ''}));
      } else {
        handler.next(options);
      }
    }));
  }
  bool fail = false;
  int saves = 0, deletes = 0, adds = 0, exports = 0;
  Completer<String>? pendingExport;
  @override
  Future<String> exportPlaylist(String id) async {
    exports++;
    if (fail) throw StateError('offline');
    if (pendingExport != null) return pendingExport!.future;
    return '#EXTM3U\n#PLAYLIST:Road Trip\n';
  }

  Map<String, dynamic>? sourceBinding;
  int sourceSyncs = 0;
  @override
  Future<Map<String, dynamic>> syncPlaylistSource(String id,
      {bool detach = false}) async {
    sourceSyncs++;
    if (detach) sourceBinding = null;
    return data;
  }

  List<Map<String, dynamic>> members = [];
  final edits = <Map<String, dynamic>>[];
  String name = 'Road Trip', comment = 'Saved comment';
  Completer<Map<String, dynamic>>? pending;
  Map<String, dynamic> get data => {
        'playlist_id': 'pl_a',
        'owner_id': 1,
        'can_edit': true,
        if (sourceBinding != null) 'source': sourceBinding,
        'name': name,
        'comment': comment,
        'song_count': members.length,
        'duration': 0,
        'songs':
            members.map((entry) => song(entry['media_id'] as int)).toList(),
        'entries': members
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

  static Map<String, dynamic> song(int id) => {
        'id': id,
        'type': 'audio',
        'file_name': '$id.mp3',
        'duration': 12,
        'music': {
          'title': id == 1 ? 'First Light' : 'Harbour',
          'artist': 'Aurora Lane',
          'album': 'Night Transit'
        }
      };
  @override
  Future<Map<String, dynamic>> musicAlbum(String key) async => {
        'songs': [song(1), song(2)]
      };
  @override
  Future<Map<String, dynamic>> editPlaylistSongs(
      String id, Map<String, dynamic> body) async {
    edits.add(body);
    if (fail) throw StateError('offline');
    if (body['action'] == 'add') {
      for (final id in body['media_ids'] as List) {
        members.add({'entry_id': 'new${members.length}', 'media_id': id});
      }
    }
    if (body['action'] == 'remove') {
      members.removeWhere((entry) => entry['entry_id'] == body['entry_id']);
    }
    if (body['action'] == 'up' || body['action'] == 'down') {
      final at =
          members.indexWhere((entry) => entry['entry_id'] == body['entry_id']);
      final to = at + (body['action'] == 'up' ? -1 : 1);
      final moved = members.removeAt(at);
      members.insert(to, moved);
    }
    return data;
  }

  @override
  Future<void> addPlaylistAlbum(String id, String key) async {
    adds++;
    if (fail) throw StateError('offline');
  }
}

void main() {
  Future<void> mount(WidgetTester t, _Client api,
      {String? id,
      User user = const User(id: 1, username: "owner", isAdmin: false)}) async {
    await t.pumpWidget(ProviderScope(overrides: [
      apiClientProvider.overrideWithValue(api),
      currentUserProvider.overrideWith((ref) async => user)
    ], child: MaterialApp(home: MusicPlaylistsScreen(playlistId: id))));
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
  testWidgets('follow status keeps membership read only until confirmed detach',
      (t) async {
    final api = _Client()
      ..sourceBinding = {
        'path': 'Music/trip.m3u8',
        'base': 'folder',
        'status': 'unavailable',
        'unavailable': 1,
        'rejected': 0
      }
      ..members = [
        {'entry_id': 'first', 'media_id': 1}
      ];
    await mount(t, api, id: 'pl_a');
    expect(find.text('Source unavailable. Last imported songs retained.'),
        findsOneWidget);
    expect(find.text('Sync now'), findsOneWidget);
    await t.ensureVisible(find.text('Sync now'));
    await t.tap(find.text('Sync now'));
    await t.pumpAndSettle();
    expect(api.sourceSyncs, 1);
    expect(find.text('Add music'), findsNothing);
    expect(find.byTooltip('Song actions for First Light'), findsNothing);
    await t.ensureVisible(find.text('Stop following…'));
    await t.tap(find.text('Stop following…'));
    await t.pumpAndSettle();
    await t.tap(find.text('Cancel'));
    await t.pumpAndSettle();
    expect(api.sourceBinding, isNotNull);
    await t.tap(find.text('Stop following…'));
    await t.pumpAndSettle();
    await t.tap(find.text('Stop following'));
    await t.pumpAndSettle();
    expect(api.sourceBinding, isNull);
    expect(api.members.length, 1);
    expect(find.text('Add music'), findsOneWidget);
    expect(find.byTooltip('Song actions for First Light'), findsOneWidget);
  });
  testWidgets('admin can detach another owner source but cannot sync it',
      (t) async {
    final api = _Client()
      ..sourceBinding = {
        'path': 'Music/trip.m3u8',
        'base': 'folder',
        'status': 'ready',
        'unavailable': 0,
        'rejected': 0
      };
    await mount(t, api,
        id: 'pl_a', user: const User(id: 2, username: 'admin', isAdmin: true));
    expect(find.text('Sync now'), findsNothing);
    expect(find.text('Stop following…'), findsOneWidget);
    expect(find.text('Edit playlist'), findsOneWidget);
    expect(api.sourceSyncs, 0);
  });
  testWidgets(
      'Find retains occurrence IDs for later duplicate move/remove and failed edit',
      (t) async {
    t.view.physicalSize = const Size(800, 1200);
    t.view.devicePixelRatio = 1;
    addTearDown(t.view.resetPhysicalSize);
    addTearDown(t.view.resetDevicePixelRatio);
    final api = _Client()
      ..members = [
        {'entry_id': 'first', 'media_id': 1},
        {'entry_id': 'middle', 'media_id': 2},
        {'entry_id': 'later', 'media_id': 1}
      ];
    await mount(t, api, id: 'pl_a');
    await t.enterText(find.byType(TextField), 'First');
    await t.pumpAndSettle();
    expect(find.text('Showing 2 of 3 songs'), findsOneWidget);
    await t.ensureVisible(find.byTooltip('Song actions for First Light').last);
    await t.tap(find.byTooltip('Song actions for First Light').last);
    await t.pumpAndSettle();
    await t.tap(find.text('Move up'));
    await t.pumpAndSettle();
    expect(api.edits.last, {'action': 'up', 'entry_id': 'later'});
    expect(api.members.map((e) => e['entry_id']), ['first', 'later', 'middle']);
    await t.tap(find.byTooltip('Song actions for First Light').last);
    await t.pumpAndSettle();
    api.fail = true;
    await t.tap(find.text('Remove from playlist'));
    await t.pumpAndSettle();
    expect(find.textContaining('Could not update playlist.'), findsOneWidget);
    expect(api.members.length, 3);
    api.fail = false;
    await t.tap(find.byTooltip('Song actions for First Light').last);
    await t.pumpAndSettle();
    await t.tap(find.text('Remove from playlist'));
    await t.pumpAndSettle();
    expect(api.edits.last, {'action': 'remove', 'entry_id': 'later'});
    expect(find.text('Song removed from playlist.'), findsOneWidget);
    await t.tap(find.text('Undo'));
    await t.pumpAndSettle();
    expect(api.edits.last, {
      'action': 'add',
      'media_ids': [1],
      'before_entry_id': 'middle'
    });
    await t.tap(find.byTooltip('Clear search'));
    await t.pumpAndSettle();
    expect(find.text('Showing 3 of 3 songs'), findsOneWidget);
  });
  testWidgets(
      'Add songs retains selection across search and offline error then saves displayed order',
      (t) async {
    final api = _Client();
    await mount(t, api, id: 'pl_a');
    await t.tap(find.text('Add songs'));
    await t.pumpAndSettle();
    final dialog = find.byType(AlertDialog);
    final search =
        find.descendant(of: dialog, matching: find.byType(TextField));
    await t.enterText(search, 'Harbour');
    await t.pumpAndSettle();
    await t.tap(find.widgetWithText(CheckboxListTile, 'Harbour'));
    await t.pumpAndSettle();
    await t.enterText(search, 'First');
    await t.pumpAndSettle();
    await t.tap(find.widgetWithText(CheckboxListTile, 'First Light'));
    await t.pumpAndSettle();
    expect(find.text('2 selected'), findsOneWidget);
    api.fail = true;
    await t.tap(find.text('Add selected songs'));
    await t.pumpAndSettle();
    expect(find.textContaining('Could not add songs.'), findsOneWidget);
    expect(find.text('2 selected'), findsOneWidget);
    expect(find.widgetWithText(TextField, 'First'), findsOneWidget);
    api.fail = false;
    await t.tap(find.text('Add selected songs'));
    await t.pumpAndSettle();
    expect(api.edits.last, {
      'action': 'add',
      'media_ids': [1, 2]
    });
    expect(find.byType(AlertDialog), findsNothing);
    expect(find.text('Showing 2 of 2 songs'), findsOneWidget);
  });
  testWidgets(
      'export offers M3U8 system save, keeps retry and cancellation usable',
      (t) async {
    final api = _Client();
    final calls = <MethodCall>[];
    const channel = MethodChannel('zone.foo.player_android/playlist_export');
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
      calls.add(call);
      return calls.length > 1;
    });
    addTearDown(() => TestDefaultBinaryMessengerBinding
        .instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null));
    await mount(t, api, id: 'pl_a');
    await t.tap(find.byTooltip('Playlist actions'));
    await t.pumpAndSettle();
    await t.tap(find.text('Export M3U8…'));
    await t.pumpAndSettle();
    expect(find.textContaining('playlist-pl_a.m3u8'), findsOneWidget);
    api.fail = true;
    await t.tap(find.text('Save M3U8'));
    await t.pumpAndSettle();
    expect(find.textContaining('Could not export playlist.'), findsOneWidget);
    expect(calls, isEmpty);
    api.fail = false;
    await t.tap(find.text('Save M3U8'));
    await t.pumpAndSettle();
    expect(find.byType(AlertDialog), findsOneWidget);
    expect(calls.single.arguments, {
      'filename': 'playlist-pl_a.m3u8',
      'content': '#EXTM3U\n#PLAYLIST:Road Trip\n'
    });
    await t.tap(find.text('Save M3U8'));
    await t.pumpAndSettle();
    expect(find.byType(AlertDialog), findsNothing);
    expect(find.text('Playlist saved.'), findsOneWidget);
  });

  testWidgets('closing export while fetching prevents a late native save',
      (t) async {
    final api = _Client()..pendingExport = Completer<String>();
    var saves = 0;
    const channel = MethodChannel('zone.foo.player_android/playlist_export');
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (_) async {
      saves++;
      return true;
    });
    addTearDown(() => TestDefaultBinaryMessengerBinding
        .instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null));
    await mount(t, api, id: 'pl_a');
    await t.tap(find.byTooltip('Playlist actions'));
    await t.pumpAndSettle();
    await t.tap(find.text('Export M3U8…'));
    await t.pumpAndSettle();
    await t.tap(find.text('Save M3U8'));
    await t.pump();
    await t.tap(find.text('Cancel'));
    await t.pumpAndSettle();
    api.pendingExport!.complete('#EXTM3U\n');
    await t.pumpAndSettle();
    expect(saves, 0);
    expect(find.byType(AlertDialog), findsNothing);
  });
}
