import 'dart:async';
import 'package:cookie_jar/cookie_jar.dart';
import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:just_audio/just_audio.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/models/user.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/audio_handler_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/screens/music_artists_screen.dart';
import 'package:player_android/widgets/music_collection_actions.dart';
import 'package:player_android/widgets/overflow_menu.dart';
import 'package:player_android/services/music_collections.dart';
import 'package:player_android/screens/music_playlists_screen.dart';
import 'package:player_android/services/audio_handler.dart';
import 'package:player_android/services/music_queue.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/services/playback_session_coordinator.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../support/idle_playback.dart';
import '../support/server_endpoint.dart';

class _Api extends PlayerApiClient {
  _Api() : super(dio: Dio());
  final album = Completer<Map<String, dynamic>>();
  @override
  Future<Map<String, dynamic>> playlist(String key) async => {
        'playlist_id': key,
        'name': 'Playlist',
        'comment': 'Comment',
        'song_count': 1,
        'duration': 12,
        'songs': [
          {
            'id': 1,
            'set_id': 1,
            'file_name': 'song.mp3',
            'type': 'audio',
            'duration': 12
          }
        ],
        'entries': [
          {'entry_id': 'entry', 'media_id': 1}
        ],
      };
  @override
  Future<Map<String, dynamic>> musicArtist(String key) async => {
        'artist_key': key,
        'name': 'Artist',
        'roles': ['track_artist'],
        'album_count': 1,
        'song_count': 1,
        'albums': [
          {
            'album_key': 'album',
            'title': 'Album',
            'artist': 'Artist',
            'roles': ['track_artist']
          }
        ],
      };
  @override
  Future<Map<String, dynamic>> musicAlbum(String key) => album.future;
}

class _Auth extends AuthStateNotifier {
  @override
  Future<AuthState> build() async => const AuthState.authenticated(
      user: User(id: 7, username: 'viewer', isAdmin: false));
}

class _Player extends AudioPlayer {
  int loads = 0;
  AudioSource? source;
  @override
  bool get playing => false;
  @override
  Duration get position => Duration.zero;
  @override
  Duration get duration => const Duration(seconds: 12);
  @override
  ProcessingState get processingState => ProcessingState.ready;
  @override
  Future<void> pause() async {}
  @override
  Future<void> play() async {}
  @override
  Future<void> seek(Duration? position, {int? index}) async {}

  @override
  Stream<PlaybackEvent> get playbackEventStream => const Stream.empty();
  @override
  Stream<ProcessingState> get processingStateStream => const Stream.empty();
  @override
  Stream<bool> get playingStream => const Stream.empty();
  @override
  Future<Duration?> setAudioSource(AudioSource source,
      {bool preload = true,
      int? initialIndex,
      Duration? initialPosition}) async {
    loads++;
    this.source = source;
    return const Duration(seconds: 12);
  }

  @override
  Future<void> stop() async {}
}

class _ObservedHandler extends PlayerAudioHandler {
  _ObservedHandler(super.player);
  int insertions = 0;
  @override
  Future<bool> insertQueue(
      {required List<PlaybackRequest> entries,
      required QueueInsertion action,
      bool replace = false,
      bool shuffled = false,
      String sourceLabel = 'Collection',
      required Future<PlaybackSessionLease?> Function(PlaybackRequest) claim,
      required Future<AudioSource?> Function(PlaybackRequest, bool Function())
          load}) async {
    insertions++;
    return super.insertQueue(
        entries: entries,
        action: action,
        replace: replace,
        shuffled: shuffled,
        sourceLabel: sourceLabel,
        claim: claim,
        // Keep real handler ownership/queue/source behavior; skip transport auth.
        load: (request, current) async => AudioSource.uri(request.sourceUri));
  }
}

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));
  for (final cancel in ['none', 'Cancel', 'Back', 'no-match']) {
    testWidgets(
        'Shuffle options actual menu submits or cancels deferred resolver $cancel',
        (tester) async {
      final api = _Api(), player = _Player();
      final handler = _ObservedHandler(player);
      await tester.pumpWidget(ProviderScope(
          overrides: [
            unpairedServerEndpointOverride(),
            apiClientProvider.overrideWithValue(api),
            audioHandlerProvider.overrideWithValue(handler),
            authStateProvider.overrideWith(_Auth.new),
            cookieJarProvider.overrideWithValue(CookieJar()),
            playerBaseUrlProvider.overrideWithValue(Uri.parse('http://local')),
          ],
          child: Consumer(builder: (context, ref, _) {
            ref.watch(authStateProvider);
            return const MaterialApp(
                home: MusicArtistsScreen(artistKey: 'artist'));
          })));
      await tester.pumpAndSettle();
      await tester.tap(find.descendant(
          of: find.byType(MusicCollectionActions),
          matching: find.byTooltip('More options')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Shuffle options…'));
      await tester.pumpAndSettle();
      expect(find.text('Shuffle options'), findsOneWidget);
      await tester.enterText(
          find.widgetWithText(TextField, 'Maximum songs (optional)'), '0');
      await tester.tap(find.widgetWithText(FilledButton, 'Shuffle'));
      await tester.pumpAndSettle();
      expect(find.text('Maximum songs must be a whole number from 1 to 10000.'),
          findsOneWidget);
      expect(handler.insertions, 0);
      await tester.enterText(
          find.widgetWithText(TextField, 'Maximum songs (optional)'), '2');
      if (cancel == 'no-match') {
        await tester.enterText(
            find.widgetWithText(TextField, 'Genre (optional)'),
            'Unknown genre');
      }
      await tester.tap(find.widgetWithText(FilledButton, 'Shuffle'));
      await tester.pumpAndSettle();
      expect(find.text('Loading songs…'), findsWidgets);
      if (cancel == 'Cancel') {
        await tester.tap(find.text('Cancel'));
      }
      if (cancel == 'Back') {
        await tester.binding.handlePopRoute();
      }
      await tester.pumpAndSettle();
      api.album.complete({
        'songs': [
          for (var i = 1; i <= 3; i++)
            {
              'id': i,
              'set_id': 1,
              'file_name': '$i.mp3',
              'type': 'audio',
              'duration': 12
            }
        ],
        'song_credits': {
          for (var i = 1; i <= 3; i++)
            '$i': [
              {
                'artist_key': 'artist',
                'roles': ['track_artist']
              }
            ]
        }
      });
      await tester.pumpAndSettle();
      if (cancel == 'no-match') {
        expect(find.text('No songs match these options.'), findsWidgets);
        expect(handler.insertions, 0);
        expect(handler.musicQueue.entries, isEmpty);
        expect(find.widgetWithText(TextField, 'Maximum songs (optional)'),
            findsOneWidget);
        expect(
            tester
                .widget<TextField>(
                    find.widgetWithText(TextField, 'Maximum songs (optional)'))
                .controller!
                .text,
            '2');
        await tester.enterText(
            find.widgetWithText(TextField, 'Genre (optional)'), '');
        await tester.tap(find.widgetWithText(FilledButton, 'Shuffle'));
        await tester.pumpAndSettle();
      }
      expect(handler.insertions,
          (cancel == 'none' || cancel == 'no-match') ? 1 : 0);
      expect(handler.musicQueue.entries.length,
          (cancel == 'none' || cancel == 'no-match') ? 2 : 0);
      expect(handler.musicQueue.shuffle,
          (cancel == 'none' || cancel == 'no-match'));
      expect(player.loads, (cancel == 'none' || cancel == 'no-match') ? 1 : 0);
      expect(find.text('Shuffle options'), findsNothing);
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.runAsync(handler.stop);
    });
  }

  testWidgets(
      'playlist More options semantic rectangle is a compact target excluding adjacent Add songs and opens its menu',
      (tester) async {
    tester.view.physicalSize = const Size(393, 844);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final semantics = tester.ensureSemantics();
    try {
      await tester.pumpWidget(ProviderScope(
          overrides: [
            ...idlePlaybackOverrides(),
            unpairedServerEndpointOverride(),
            apiClientProvider.overrideWithValue(_Api()),
          ],
          child: const MaterialApp(
              home: MusicPlaylistsScreen(playlistId: 'playlist'))));
      await tester.pumpAndSettle();
      final menu = find.descendant(
          of: find.byType(MusicCollectionActions),
          matching: find.byTooltip('More options'));
      final node = tester.getSemantics(menu);
      var rect = node.rect;
      for (var ancestor = node;;) {
        if (ancestor.transform != null) {
          rect = MatrixUtils.transformRect(ancestor.transform!, rect);
        }
        final parent = ancestor.parent;
        if (parent == null) break;
        ancestor = parent;
      }
      final actual = tester.getRect(menu),
          adjacent =
              tester.getRect(find.widgetWithText(FilledButton, 'Add songs'));
      expect(actual.height, greaterThanOrEqualTo(48));
      expect(rect.overlaps(adjacent), isFalse,
          reason:
              'Semantic target must not include the adjacent Add songs action');
      expect(rect.size, actual.size);
      expect(rect.center.dx, closeTo(actual.center.dx, .01));
      expect(rect.center.dy, closeTo(actual.center.dy, .01));
      await tester.tapAt(rect.center);
      await tester.pumpAndSettle();
      expect(find.text('Play next'), findsOneWidget);
      expect(find.text('Add to queue'), findsOneWidget);
      expect(find.text('Skip repeated songs'), findsNothing);
      final pop = tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      await pop;
      await tester.tap(find.text('Add songs'));
      await tester.pumpAndSettle();
      final duplicates = find.ancestor(
          of: find.text('Skip repeated songs'),
          matching: find.byType(CheckboxListTile));
      expect(duplicates, findsOneWidget);
      expect(tester.widget<CheckboxListTile>(duplicates).value, isTrue);
    } finally {
      await tester.pumpWidget(const SizedBox.shrink());
      semantics.dispose();
    }
  });

  testWidgets(
      'artist Play leaving for album then Back permanently cancels deferred resolution',
      (tester) async {
    final api = _Api(), player = _Player();
    final handler = _ObservedHandler(player);
    final router = GoRouter(routes: [
      GoRoute(
          path: '/',
          builder: (_, __) => const MusicArtistsScreen(artistKey: 'artist')),
      GoRoute(
          path: '/music/albums',
          builder: (context, _) => Scaffold(
              appBar: AppBar(title: const Text('Album route')),
              body: const Text('Album details'))),
    ]);
    try {
      await tester.pumpWidget(ProviderScope(
          overrides: [
            unpairedServerEndpointOverride(),
            apiClientProvider.overrideWithValue(api),
            audioHandlerProvider.overrideWithValue(handler),
            authStateProvider.overrideWith(_Auth.new),
            cookieJarProvider.overrideWithValue(CookieJar()),
            playerBaseUrlProvider.overrideWithValue(Uri.parse('http://local')),
          ],
          child: Consumer(builder: (context, ref, _) {
            ref.watch(authStateProvider);
            return MaterialApp.router(routerConfig: router);
          })));
      await tester.pumpAndSettle();
      final before = handler.musicQueue.entries.toList();
      await tester.tap(find.text('Play artist'));
      await tester.pumpAndSettle();
      expect(find.text('Loading songs…'), findsOneWidget);
      await tester.scrollUntilVisible(find.text('Album'), 200,
          scrollable: find.byType(Scrollable).first);
      await tester.tap(find.text('Album'));
      await tester.pumpAndSettle();
      expect(find.text('Album route'), findsOneWidget);
      router.pop();
      await tester.pumpAndSettle();
      await tester.scrollUntilVisible(find.text('Play artist'), -200,
          scrollable: find.byType(Scrollable).first);
      expect(find.text('Play artist'), findsOneWidget);
      api.album.complete({
        'songs': [
          {
            'id': 1,
            'set_id': 1,
            'file_name': 'song.mp3',
            'type': 'audio',
            'duration': 12
          }
        ],
        'song_credits': {
          '1': [
            {
              'artist_key': 'artist',
              'roles': ['track_artist']
            }
          ]
        }
      });
      await tester.pumpAndSettle();
      expect(handler.insertions, 0,
          reason: 'Back must never revive a request that left its route');
      expect(handler.musicQueue.entries, before);
      expect(player.loads, 0);
      expect(player.source, isNull);
      expect(find.text('Loading songs…'), findsNothing);
    } finally {
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.runAsync(handler.stop);
      router.dispose();
    }
  });

  testWidgets(
      'compact card exposes only More with Play and Keep offline inside',
      (tester) async {
    await tester.pumpWidget(ProviderScope(
        overrides: [
          ...idlePlaybackOverrides(),
          unpairedServerEndpointOverride(),
          apiClientProvider.overrideWithValue(_Api()),
        ],
        child: MaterialApp(
            home: Scaffold(
                body: MusicCollectionActions(
                    compact: true,
                    collections: const [MusicCollection('album', 'album')],
                    playLabel: 'Play',
                    sourceLabel: 'Album')))));
    await tester.pumpAndSettle();
    expect(find.text('Play'), findsNothing);
    expect(find.text('Shuffle'), findsNothing);
    expect(find.text('Keep offline'), findsNothing);
    await tester.tap(find.byTooltip('More options'));
    await tester.pumpAndSettle();
    expect(find.text('Play'), findsOneWidget);
    expect(find.text('Shuffle'), findsOneWidget);
    expect(find.text('Keep offline'), findsOneWidget);
    expect(find.text('Share…'), findsOneWidget);
    expect(find.text('Pin offline…'), findsOneWidget);
    expect(find.text('Play next'), findsOneWidget);
    expect(find.text('Add to queue'), findsOneWidget);
    expect(find.text('Shuffle options…'), findsOneWidget);
  });
  testWidgets(
      'empty compact playlist retains management without enabling playback',
      (tester) async {
    var unpinned = false;
    await tester.pumpWidget(ProviderScope(
        child: MaterialApp(
            home: Scaffold(
                body: MusicCollectionActions(
                    compact: true,
                    enabled: false,
                    collections: const [MusicCollection('playlist', 'empty')],
                    playLabel: 'Play playlist',
                    moreExtras: (_) => [
                          OverflowMenuItem(
                              icon: Icons.push_pin_outlined,
                              label: 'Unpin playlist',
                              onSelected: () => unpinned = true),
                        ])))));
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('More options'));
    await tester.pumpAndSettle();
    final play = tester.widget<OverflowMenuItem>(find.ancestor(
        of: find.text('Play playlist'),
        matching: find.byType(OverflowMenuItem)));
    expect(play.onSelected, isNull);
    await tester.ensureVisible(find.text('Unpin playlist'));
    await tester.tap(find.text('Unpin playlist'));
    await tester.pumpAndSettle();
    expect(unpinned, isTrue);
  });
}
