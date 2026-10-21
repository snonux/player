import 'package:dio/dio.dart';
import 'package:just_audio/just_audio.dart';
import 'package:cookie_jar/cookie_jar.dart';
import 'package:player_android/providers/audio_handler_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/models/user.dart';
import 'package:player_android/services/audio_handler.dart';
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

class _Auth extends AuthStateNotifier {
  @override
  Future<AuthState> build() async => const AuthState.authenticated(
      user: User(id: 7, username: 'test', isAdmin: false));
}

class _IdlePlayer extends AudioPlayer {
  @override
  Stream<PlaybackEvent> get playbackEventStream => const Stream.empty();
  @override
  Stream<ProcessingState> get processingStateStream => const Stream.empty();
  @override
  Stream<bool> get playingStream => const Stream.empty();
  @override
  Future<void> stop() async {}
}

void main() {
  testWidgets(
      'song selection enqueues displayed order, preserves selection and Back exits it',
      (tester) async {
    final handler = PlayerAudioHandler(_IdlePlayer());
    final overrides = [
      apiClientProvider.overrideWithValue(_Client()),
      audioHandlerProvider.overrideWithValue(handler),
      authStateProvider.overrideWith(_Auth.new),
      playerBaseUrlProvider.overrideWithValue(Uri.parse('http://local')),
      cookieJarProvider.overrideWithValue(CookieJar()),
    ];
    final router = GoRouter(initialLocation: '/album', routes: [
      GoRoute(
          path: '/',
          builder: (_, __) => const Scaffold(body: Text('Home')),
          routes: [
            GoRoute(
                path: 'album',
                builder: (_, __) => const MusicAlbumsScreen(albumKey: 'al_a'))
          ])
    ]);
    await tester.pumpWidget(ProviderScope(
        overrides: overrides,
        child: Consumer(builder: (context, ref, _) {
          ref.watch(authStateProvider);
          return MaterialApp.router(routerConfig: router);
        })));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Select songs'));
    await tester.pumpAndSettle();
    await tester.tap(find.bySemanticsLabel('Select Second by track'));
    await tester.pump();
    await tester.tap(find.bySemanticsLabel('Select First by track'));
    await tester.pump();
    expect(find.text('2 selected'), findsOneWidget);
    await tester.tap(find.byTooltip('Selected song actions'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Play next'));
    await tester.pumpAndSettle();
    expect(handler.musicQueue.entries.map((e) => e.request.title),
        ['First by track', 'Second by track']);
    expect(handler.musicQueue.active, isFalse);
    expect(find.text('2 selected'), findsOneWidget);
    expect(find.text('Added 2 songs'), findsOneWidget);
    await tester.tap(find.text('Undo'));
    await tester.pumpAndSettle();
    expect(handler.musicQueue.entries, isEmpty);
    final pop = tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    await pop;
    await tester.pumpAndSettle();
    expect(find.text('Select songs'), findsOneWidget);
    expect(find.byType(Checkbox), findsNothing);
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.runAsync(handler.stop);
    router.dispose();
  });

  testWidgets(
      'latest single-song confirmation replaces persistent batch snackbar and Undo targets only latest addition',
      (tester) async {
    final handler = PlayerAudioHandler(_IdlePlayer());
    final router = GoRouter(routes: [
      GoRoute(
          path: '/',
          builder: (_, __) => const MusicAlbumsScreen(albumKey: 'al_a'))
    ]);
    await tester.pumpWidget(ProviderScope(
        overrides: [
          apiClientProvider.overrideWithValue(_Client()),
          audioHandlerProvider.overrideWithValue(handler),
          authStateProvider.overrideWith(_Auth.new),
          playerBaseUrlProvider.overrideWithValue(Uri.parse('http://local')),
          cookieJarProvider.overrideWithValue(CookieJar()),
        ],
        child: Consumer(builder: (context, ref, _) {
          ref.watch(authStateProvider);
          return MaterialApp.router(
              routerConfig: router,
              builder: (context, child) => MediaQuery(
                  data: MediaQuery.of(context)
                      .copyWith(accessibleNavigation: true),
                  child: child!));
        })));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Select songs'));
    await tester.pumpAndSettle();
    await tester.tap(find.bySemanticsLabel('Select First by track'));
    await tester.pump();
    await tester.tap(find.bySemanticsLabel('Select Second by track'));
    await tester.pump();
    await tester.tap(find.byTooltip('Selected song actions'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Play next'));
    await tester.pumpAndSettle();
    expect(find.text('Added 2 songs'), findsOneWidget);
    await tester.tap(find.text('Cancel selection'));
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('Actions for First by track'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Add to queue'));
    await tester.pumpAndSettle();
    expect(handler.musicQueue.entries.map((e) => e.request.title),
        ['First by track', 'Second by track', 'First by track']);
    expect(find.text('Added 1 song'), findsOneWidget);
    expect(find.text('Added 2 songs'), findsNothing);
    await tester.tap(find.text('Undo'));
    await tester.pumpAndSettle();
    expect(handler.musicQueue.entries.map((e) => e.request.title),
        ['First by track', 'Second by track']);
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.runAsync(handler.stop);
    router.dispose();
  });

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
