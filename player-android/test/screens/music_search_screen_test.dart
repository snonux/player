import 'dart:async';
import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:player_android/models/media.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/screens/music_search_screen.dart';

class _Client extends PlayerApiClient {
  _Client() : super(dio: Dio());
  final queries = <Map<String, String>>[];
  bool fail = false;
  bool withSongs = false;
  final pending = <String, Completer<Map<String, dynamic>>>{};
  @override
  Future<Map<String, dynamic>> musicSearch(
      {String query = '', String artist = '', String album = ''}) async {
    queries.add({'q': query, 'artist': artist, 'album': album});
    if (fail) throw StateError('offline');
    if (pending.containsKey(query)) return pending[query]!.future;
    return {
      'songs': withSongs
          ? [
              for (final id in [11, 12])
                {
                  'id': id,
                  'type': 'audio',
                  'file_name': 'file_$id.flac',
                  'duration': 12,
                  'music': {
                    'title': 'Song $id',
                    'artist': 'A & B; C/D',
                    'album': 'Café 東京'
                  }
                }
            ]
          : [],
      'albums': query == 'none'
          ? []
          : [
              {'title': 'Café 東京', 'artist': 'A & B; C/D', 'album_key': 'al_1'}
            ],
      'artists': query == 'none'
          ? []
          : [
              {
                'name': 'A & B; C/D',
                'artist_key': 'ar_1',
                'album_count': 1,
                'song_count': 2
              }
            ],
    };
  }
}

void main() {
  Future<void> mount(WidgetTester tester, _Client client) async {
    await tester.pumpWidget(ProviderScope(
        overrides: [apiClientProvider.overrideWithValue(client)],
        child: const MaterialApp(home: MusicSearchScreen())));
    await tester.pumpAndSettle();
  }

  Future<void> search(WidgetTester tester, String text) async {
    await tester.enterText(find.byType(TextField), text);
    await tester.pump(const Duration(milliseconds: 350));
    await tester.pump();
  }

  test('metadata scopes preserve real names and Unicode', () {
    expect(musicSearchQuery('Партия artist:"A & B; C/D" album:"Café 夜"'),
        {'q': 'Партия', 'artist': 'A & B; C/D', 'album': 'Café 夜'});
    expect(musicSearchQuery('ALBUM:Café'),
        {'q': '', 'artist': '', 'album': 'Café'});
  });
  testWidgets(
      'empty input, grouped results, group chips and explicit no results',
      (tester) async {
    final client = _Client();
    await mount(tester, client);
    expect(find.text('Search songs, albums and artists.'), findsOneWidget);
    expect(client.queries, isEmpty);
    await search(tester, 'Café');
    expect(find.text('Albums (1)'), findsOneWidget);
    expect(find.text('Artists (1)'), findsOneWidget);
    await tester.tap(find.widgetWithText(FilterChip, 'Songs'));
    await tester.pump();
    expect(find.text('Nothing matches these filters'), findsOneWidget);
    expect(find.text('Café 東京'), findsNothing);
    await tester.tap(find.widgetWithText(FilterChip, 'All'));
    await tester.pump();
    expect(find.text('Café 東京'), findsOneWidget);
    await search(tester, 'none');
    expect(find.text('Nothing matches these filters'), findsOneWidget);
    await tester.tap(find.text('Clear filters'));
    await tester.pump();
    expect(find.text('Search songs, albums and artists.'), findsOneWidget);
    expect(client.queries.length, 2);
  });
  testWidgets('failed query Retry retains scopes and recovers', (tester) async {
    final client = _Client()..fail = true;
    await mount(tester, client);
    await search(tester, 'artist:"A & B; C/D"');
    expect(find.textContaining('Could not search music.'), findsOneWidget);
    client.fail = false;
    await tester.tap(find.text('Retry'));
    await tester.pumpAndSettle();
    expect(client.queries.last, {'q': '', 'artist': 'A & B; C/D', 'album': ''});
    expect(find.text('Café 東京'), findsOneWidget);
  });
  testWidgets(
      'typing invalidates an older request before debounce and disposal',
      (tester) async {
    final old = Completer<Map<String, dynamic>>(), client = _Client();
    client.pending['old'] = old;
    await mount(tester, client);
    await search(tester, 'old');
    expect(find.byType(CircularProgressIndicator), findsOneWidget);
    await tester.enterText(find.byType(TextField), 'none');
    old.complete({
      'songs': [],
      'albums': [
        {'title': 'Stale', 'artist': 'Old', 'album_key': 'al_old'}
      ],
      'artists': []
    });
    await tester.pump();
    expect(find.text('Stale'), findsNothing);
    await tester.pump(const Duration(milliseconds: 350));
    await tester.pump();
    expect(find.text('Nothing matches these filters'), findsOneWidget);
    final late = Completer<Map<String, dynamic>>();
    client.pending['late'] = late;
    await search(tester, 'late');
    await tester.pumpWidget(const SizedBox());
    late.complete({'songs': [], 'albums': [], 'artists': []});
    await tester.pump();
    expect(tester.takeException(), isNull);
  });
  testWidgets(
      'song tap snapshots Songs only and entity Back preserves query/chip',
      (tester) async {
    final client = _Client()..withSongs = true;
    Map<String, dynamic>? playback;
    final router = GoRouter(initialLocation: '/search', routes: [
      GoRoute(path: '/search', builder: (_, __) => const MusicSearchScreen()),
      GoRoute(
          path: '/audio/:id',
          builder: (context, state) {
            playback = state.extra as Map<String, dynamic>;
            return Scaffold(
                body: TextButton(
                    onPressed: () => context.pop(),
                    child: const Text('Return')));
          }),
      GoRoute(
          path: '/music/albums',
          builder: (context, state) => Scaffold(
              body: TextButton(
                  onPressed: () => context.pop(),
                  child: Text('Album ${state.uri.queryParameters['album']}')))),
    ]);
    addTearDown(router.dispose);
    await tester.pumpWidget(ProviderScope(
        overrides: [apiClientProvider.overrideWithValue(client)],
        child: MaterialApp.router(routerConfig: router)));
    await tester.pumpAndSettle();
    await search(tester, 'Café');
    await tester.tap(find.text('Song 12'));
    await tester.pumpAndSettle();
    expect((playback!['sequence'] as List<Media>).map((song) => song.id),
        [11, 12]);
    expect(playback!['albumCollection'], true);
    expect(playback!['title'], 'Song 12');
    expect(playback!['position'], 0.0);
    await tester.tap(find.text('Return'));
    await tester.pumpAndSettle();
    expect(find.widgetWithText(TextField, 'Café'), findsOneWidget);
    await tester.tap(find.widgetWithText(FilterChip, 'Albums'));
    await tester.pump();
    await tester.tap(find.text('Café 東京'));
    await tester.pumpAndSettle();
    expect(find.text('Album al_1'), findsOneWidget);
    await tester.tap(find.text('Album al_1'));
    await tester.pumpAndSettle();
    expect(find.widgetWithText(TextField, 'Café'), findsOneWidget);
    expect(
        tester
            .widget<FilterChip>(find.widgetWithText(FilterChip, 'Albums'))
            .selected,
        true);
    expect(find.text('Song 11'), findsNothing);
  });
}
