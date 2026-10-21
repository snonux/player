import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/screens/music_albums_screen.dart';

class _Client extends PlayerApiClient {
  _Client() : super(dio: Dio());
  bool fail = false;
  @override
  Future<Map<String, dynamic>> musicAlbum(String key) async {
    if (fail) {
      throw StateError('offline');
    }
    return {
      'album_key': key,
      'title': 'Night Transit',
      'artist': 'Aurora Lane',
      'artist_key': 'ar_a',
      'song_count': 2,
      'duration': 24,
      'songs': [
        {
          'id': 2,
          'set_id': 1,
          'file_name': 'second.mp3',
          'type': 'audio',
          'duration': 12,
          'music': {
            'title': 'First by track',
            'disc': 1,
            'track': 1,
            'artist': 'A'
          }
        },
        {
          'id': 1,
          'set_id': 2,
          'file_name': 'first.mp3',
          'type': 'audio',
          'duration': 12,
          'music': {
            'title': 'Second by track',
            'disc': 2,
            'track': 1,
            'artist': 'B'
          }
        },
      ]
    };
  }

  @override
  String playbackUrl(media) => 'http://local/api/v1/media/${media.id}/stream';
}

void main() {
  testWidgets(
      'album detail exposes totals and sends ordered cross-set snapshot/start0 to player',
      (tester) async {
    final client = _Client();
    Object? extra;
    final router = GoRouter(routes: [
      GoRoute(
          path: '/',
          builder: (_, __) => const MusicAlbumsScreen(albumKey: 'al_a')),
      GoRoute(
          path: '/audio/:id',
          builder: (_, state) {
            extra = state.extra;
            return const Scaffold(body: Text('Player opened'));
          })
    ]);
    await tester.pumpWidget(ProviderScope(
        overrides: [apiClientProvider.overrideWithValue(client)],
        child: MaterialApp.router(routerConfig: router)));
    await tester.pumpAndSettle();
    expect(find.text('2 songs · 0:24'), findsOneWidget);
    expect(find.text('First by track'), findsOneWidget);
    expect(find.text('1 · 1'), findsOneWidget);
    expect(find.text('2 · 1'), findsOneWidget);
    await tester.tap(find.text('Play album'));
    await tester.pumpAndSettle();
    final request = extra! as Map<String, dynamic>;
    expect(request['albumCollection'], true);
    expect(request['position'], 0.0);
    expect(request['mediaUrl'], 'http://local/api/v1/media/2/stream');
    expect((request['sequence'] as List).map((m) => m.id), [2, 1]);
    router.dispose();
  });
  testWidgets('request failure has visible Retry that recovers',
      (tester) async {
    final client = _Client()..fail = true;
    await tester.pumpWidget(ProviderScope(
        overrides: [apiClientProvider.overrideWithValue(client)],
        child: const MaterialApp(home: MusicAlbumsScreen(albumKey: 'al_a'))));
    await tester.pumpAndSettle();
    expect(find.text('Retry'), findsOneWidget);
    client.fail = false;
    await tester.tap(find.text('Retry'));
    await tester.pumpAndSettle();
    expect(find.text('Play album'), findsOneWidget);
  });
}
