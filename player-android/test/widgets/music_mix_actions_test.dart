import 'package:cookie_jar/cookie_jar.dart';
import 'package:just_audio/just_audio.dart';
import 'package:player_android/models/user.dart';
import 'package:player_android/providers/audio_handler_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/playback_session_provider.dart';
import 'package:player_android/services/audio_handler.dart';
import 'package:player_android/services/music_queue.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/services/playback_session_coordinator.dart';
import 'dart:async';
import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/services/music_action_intent.dart';
import 'package:player_android/widgets/music_mix_actions.dart';

class _Api extends PlayerApiClient {
  _Api() : super(dio: Dio());
  final reads = <Completer<Map<String, dynamic>>>[];
  int? minimum;
  @override
  Future<Map<String, dynamic>> musicMix(String kind, String key,
      {int minRating = 0}) {
    minimum = minRating;
    final result = Completer<Map<String, dynamic>>();
    reads.add(result);
    return result.future;
  }
}

class _Auth extends AuthStateNotifier {
  @override
  Future<AuthState> build() async => const AuthState.authenticated(
      user: User(id: 7, username: 'viewer', isAdmin: false));
}

class _Player extends AudioPlayer {
  bool fail = false;
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
    if (fail) throw StateError('decoder failure');
    this.source = source;
    return const Duration(seconds: 12);
  }

  @override
  Future<void> stop() async {}
}

// Keep real source leases while avoiding network progress flush in source stop.
class _Coordinator extends PlaybackSessionCoordinator {
  @override
  Future<PlaybackSessionLease?> claim(
          {required PlaybackSourceKind kind,
          required String identity,
          required StopPlaybackSession stop,
          String? sourceUri}) =>
      super.claim(
          kind: kind,
          identity: identity,
          sourceUri: sourceUri,
          stop: () async {});
}

class _ObservedHandler extends PlayerAudioHandler {
  _ObservedHandler(super.player);
  int insertions = 0;
  String mode = 'accepted';
  @override
  MusicQueueState get musicQueue => mode == 'busy'
      ? const MusicQueueState(
          entries: [],
          index: -1,
          label: '',
          active: false,
          busy: true,
          canUndo: false,
          status: '')
      : super.musicQueue;
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
    if (mode == 'failed') return false;
    if (mode == 'error') throw StateError('transport failed');
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
  testWidgets('Mix uses rating, explains no-match, and retries a failure',
      (tester) async {
    final api = _Api();
    await tester.pumpWidget(ProviderScope(
        overrides: [apiClientProvider.overrideWithValue(api)],
        child: const MaterialApp(
            home: Scaffold(
                body: MusicMixActions(kind: 'song', entityKey: '1')))));
    await tester.tap(find.byType(DropdownButtonFormField<int>));
    await tester.pumpAndSettle();
    await tester.tap(find.text('4+ stars').last);
    await tester.pumpAndSettle();
    await tester.tap(find.text('Start mix'));
    await tester.pump();
    expect(api.minimum, 4);
    expect(find.text('Finding mix songs…'), findsOneWidget);
    api.reads[0].complete({'title': 'Source song', 'songs': []});
    await tester.pumpAndSettle();
    expect(find.textContaining('No matching songs for Source song'),
        findsOneWidget);
    await tester.tap(find.text('Start mix'));
    api.reads[1].completeError(Exception('offline'));
    await tester.pumpAndSettle();
    expect(find.textContaining('Could not start mix'), findsOneWidget);
  });

  testWidgets(
      'A newer playback intent cancels a deferred mix before queue access',
      (tester) async {
    final api = _Api();
    await tester.pumpWidget(ProviderScope(
        overrides: [apiClientProvider.overrideWithValue(api)],
        child: const MaterialApp(
            home: Scaffold(
                body: MusicMixActions(kind: 'artist', entityKey: 'artist')))));
    await tester.tap(find.widgetWithText(FilledButton, 'Artist radio'));
    await tester.pump();
    beginMusicActionIntent();
    api.reads.single.complete({
      'title': 'Old artist',
      'songs': [
        {'id': 1, 'type': 'audio'}
      ]
    });
    await tester.pumpAndSettle();
    expect(find.textContaining('Old artist'), findsNothing);
    expect(tester.takeException(), isNull);
    await tester.tap(find.widgetWithText(FilledButton, 'Artist radio'));
    api.reads.last.complete({'title': 'Current artist', 'songs': []});
    await tester.pumpAndSettle();
    expect(find.textContaining('No matching songs for Current artist'),
        findsOneWidget);
  });
  testWidgets('Account client change releases controls and drops the old mix',
      (tester) async {
    final oldApi = _Api(), newApi = _Api();
    final selected = StateProvider<PlayerApiClient>((ref) => oldApi);
    await tester.pumpWidget(ProviderScope(
        overrides: [
          apiClientProvider.overrideWith((ref) => ref.watch(selected))
        ],
        child: const MaterialApp(
            home: Scaffold(
                body: MusicMixActions(kind: 'song', entityKey: '1')))));
    final container =
        ProviderScope.containerOf(tester.element(find.byType(MusicMixActions)));
    await tester.tap(find.text('Start mix'));
    await tester.pump();
    container.read(selected.notifier).state = newApi;
    await tester.pumpAndSettle();
    expect(find.text('Finding mix songs…'), findsNothing);
    await tester.tap(find.text('Start mix'));
    await tester.pump();
    expect(newApi.reads, hasLength(1));
    oldApi.reads.single.complete({
      'title': 'Previous account',
      'songs': [
        {'id': 1, 'type': 'audio'}
      ]
    });
    await tester.pumpAndSettle();
    expect(find.textContaining('Previous account'), findsNothing);
    newApi.reads.single.complete({'title': 'Current account', 'songs': []});
    await tester.pumpAndSettle();
    expect(find.textContaining('No matching songs for Current account'),
        findsOneWidget);
    expect(tester.takeException(), isNull);
  });
  for (final mode in ['accepted', 'failed', 'error', 'busy', 'load-error']) {
    testWidgets('Mix insertion reports $mode without a false success',
        (tester) async {
      final api = _Api(), player = _Player();
      player.fail = mode == 'load-error';
      final handler = _ObservedHandler(player)..mode = mode;
      await tester.pumpWidget(ProviderScope(
          overrides: [
            apiClientProvider.overrideWithValue(api),
            audioHandlerProvider.overrideWithValue(handler),
            playbackSessionCoordinatorProvider
                .overrideWithValue(_Coordinator()),
            authStateProvider.overrideWith(_Auth.new),
            cookieJarProvider.overrideWithValue(CookieJar()),
            playerBaseUrlProvider.overrideWithValue(Uri.parse('http://local')),
          ],
          child: Consumer(builder: (context, ref, _) {
            ref.watch(authStateProvider);
            return const MaterialApp(
                home: Scaffold(
                    body: MusicMixActions(kind: 'song', entityKey: '1')));
          })));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Start mix'));
      api.reads.single.complete({
        'title': 'Seed song',
        'songs': [
          {
            'id': 4,
            'set_id': 1,
            'file_name': 'first.mp3',
            'type': 'audio',
            'duration': 12
          },
          {
            'id': 2,
            'set_id': 1,
            'file_name': 'second.mp3',
            'type': 'audio',
            'duration': 12
          },
        ]
      });
      await tester.pumpAndSettle();
      expect(handler.insertions, mode == 'busy' ? 0 : 1);
      if (mode == 'accepted') {
        expect(
            handler.musicQueue.entries.map(
                (entry) => (entry.request as ServerPlaybackRequest).mediaId),
            [4, 2]);
        expect(handler.musicQueue.label, 'Mix · Seed song');
        expect(player.loads, 1);
        expect(find.text('Mix · Seed song · 2 songs'), findsOneWidget);
        final scope = ProviderScope.containerOf(
            tester.element(find.byType(MusicMixActions)));
        await scope.read(playbackSessionCoordinatorProvider).stopCurrent();
        beginMusicActionIntent();
        await handler.jumpQueue(1);
        await tester.pumpAndSettle();
        expect(handler.musicQueue.index, 1);
        expect(player.loads, 2);
        expect(handler.musicQueue.active, isTrue);
      } else {
        if (mode == 'load-error') {
          expect(handler.musicState?.error, isNotNull);
          expect(player.loads, 1);
          expect(handler.musicQueue.entries, hasLength(2));
        } else {
          expect(handler.musicQueue.entries, isEmpty);
        }
        expect(find.text('Mix · Seed song · 2 songs'), findsNothing);
        expect(
            find.textContaining(mode == 'busy'
                ? 'Loading track… Please wait, then try again.'
                : 'Could not start mix.'),
            findsOneWidget);
      }
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.runAsync(handler.stop);
    });
  }
}
