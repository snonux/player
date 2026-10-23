import 'dart:async';
import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/screens/music_artists_screen.dart';

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
  final roles = <String>[];
  bool fail = false;
  Completer<List<Map<String, dynamic>>>? pending;
  @override
  Future<List<Map<String, dynamic>>> musicArtists(
      {String role = 'artists'}) async {
    roles.add(role);
    if (fail) throw StateError('offline');
    if (pending != null) return pending!.future;
    return role == 'artists'
        ? [
            {
              'artist_key': 'ar_a',
              'name': 'A & B; C/D',
              'roles': ['track_artist'],
              'album_count': 2,
              'song_count': 3
            }
          ]
        : [];
  }

  @override
  Future<Map<String, dynamic>> musicArtist(String key) async => {
        'artist_key': key,
        'name': 'Composer; 東京 / A',
        'roles': ['composer'],
        'album_count': 1,
        'song_count': 2,
        'albums': [
          {
            'album_key': 'al_a',
            'title': 'Compilation',
            'artist': 'Various Artists',
            'roles': ['composer']
          }
        ]
      };
}

void main() {
  Future<void> mount(WidgetTester tester, _Client client, {String? key}) async {
    await tester.pumpWidget(ProviderScope(
        overrides: [apiClientProvider.overrideWithValue(client)],
        child: MaterialApp(home: MusicArtistsScreen(artistKey: key))));
    await tester.pumpAndSettle();
  }

  testWidgets(
      'common Artists default, secondary Role filter and explicit empty state',
      (tester) async {
    final client = _Client();
    await mount(tester, client);
    expect(find.text('A & B; C/D'), findsOneWidget);
    expect(find.text('Artists (track and album)'), findsOneWidget);
    await tester.tap(find.byType(DropdownButtonFormField<String>));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Composer').last);
    await tester.pumpAndSettle();
    expect(client.roles, ['artists', 'composer']);
    expect(find.textContaining('No artists available for this role.'),
        findsOneWidget);
    expect(find.text('A & B; C/D'), findsNothing);
  });
  testWidgets('offline Retry recovers and missing credits never become people',
      (tester) async {
    final client = _Client()..fail = true;
    await mount(tester, client);
    expect(find.textContaining('Could not load artists.'), findsOneWidget);
    client.fail = false;
    await tester.tap(find.text('Retry'));
    await tester.pumpAndSettle();
    expect(find.text('A & B; C/D'), findsOneWidget);
  });
  testWidgets('detail labels roles and discography without guessing names',
      (tester) async {
    await mount(tester, _Client(), key: 'ar_composer');
    expect(find.text('Composer; 東京 / A'), findsNWidgets(2));
    expect(find.text('Compilation'), findsOneWidget);
    expect(find.text('Various Artists\nComposer'), findsOneWidget);
  });
}
