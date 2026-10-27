// AudioHandlerRadioEngine plays through the shared audio handler and the
// playback coordinator: it stops library playback, authenticates the relayed
// stream, and marks the notification item as live.

import 'dart:async';

import 'package:audio_service/audio_service.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:just_audio/just_audio.dart';
import 'package:player_android/services/audio_handler.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/services/playback_session_coordinator.dart';
import 'package:player_android/services/radio_engine.dart';

import '../support/radio_fakes.dart';

class _Player extends AudioPlayer {
  final sources = <AudioSource>[];
  final states = StreamController<PlayerState>.broadcast();
  Object? loadError;
  int plays = 0;
  int stops = 0;

  @override
  Stream<PlaybackEvent> get playbackEventStream => const Stream.empty();
  @override
  Stream<bool> get playingStream => const Stream.empty();
  @override
  Stream<ProcessingState> get processingStateStream => const Stream.empty();
  @override
  Stream<PlayerState> get playerStateStream => states.stream;
  @override
  ProcessingState get processingState => ProcessingState.ready;
  @override
  bool get playing => false;
  @override
  Duration get position => Duration.zero;
  @override
  Duration? get duration => null;

  @override
  Future<Duration?> setAudioSource(AudioSource source,
      {bool preload = true,
      int? initialIndex,
      Duration? initialPosition}) async {
    if (loadError != null) throw loadError!;
    sources.add(source);
    return null;
  }

  @override
  Future<void> play() async => plays++;

  @override
  Future<void> stop() async => stops++;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late _Player player;
  late PlayerAudioHandler handler;
  late PlaybackSessionCoordinator coordinator;
  late AudioHandlerRadioEngine engine;

  setUp(() {
    player = _Player();
    handler = PlayerAudioHandler(player);
    coordinator = PlaybackSessionCoordinator();
    engine = AudioHandlerRadioEngine(
      handler: handler,
      coordinator: coordinator,
      urlFor: (path) => 'https://player.test$path',
      headersFor: (_) async => {'Authorization': 'Bearer t'},
    );
  });

  test('starting a station stops library playback and plays the relay',
      () async {
    var libraryStopped = false;
    await coordinator.claim(
      kind: PlaybackSourceKind.server,
      identity: 'server:x:1:5',
      stop: () async => libraryStopped = true,
    );
    var stoppedElsewhere = false;
    await engine.start(radioStation('abc', name: 'Jazz'),
        onStoppedElsewhere: () => stoppedElsewhere = true);
    await Future<void>.delayed(Duration.zero);

    expect(libraryStopped, isTrue);
    final source = player.sources.single as UriAudioSource;
    expect(source.uri.toString(),
        'https://player.test/api/radio/stations/abc/stream');
    expect(source.headers, {'Authorization': 'Bearer t'});
    expect(player.plays, 1);
    final item = handler.mediaItem.value!;
    expect(item.title, 'Jazz');
    expect(item.isLive, isTrue);
    expect(handler.playbackState.value.controls,
        isNot(contains(MediaControl.skipToNext)));
    expect(stoppedElsewhere, isFalse);

    // Library playback taking the player back reports it.
    await coordinator.claim(
      kind: PlaybackSourceKind.server,
      identity: 'server:x:1:6',
      stop: () async {},
    );
    expect(stoppedElsewhere, isTrue);
  });

  test('a stream that fails to load releases the player and throws', () async {
    player.loadError = StateError('404');
    await expectLater(
        engine.start(radioStation('dead'), onStoppedElsewhere: () {}),
        throwsStateError);
    expect(coordinator.activeKind, isNull);
    expect(player.plays, 0);
  });

  test('stop ends the session without reporting a takeover', () async {
    var stoppedElsewhere = false;
    await engine.start(radioStation('abc'),
        onStoppedElsewhere: () => stoppedElsewhere = true);
    await engine.stop();
    expect(coordinator.activeKind, isNull);
    expect(player.stops, greaterThan(0));
    expect(stoppedElsewhere, isFalse);
  });

  test('playing is reported only when loaded and audible', () async {
    await engine.start(radioStation('abc'), onStoppedElsewhere: () {});
    final seen = <bool>[];
    final sub = engine.playing.listen(seen.add);
    addTearDown(sub.cancel);
    player.states.add(PlayerState(true, ProcessingState.buffering));
    player.states.add(PlayerState(true, ProcessingState.ready));
    player.states.add(PlayerState(false, ProcessingState.ready));
    await Future<void>.delayed(Duration.zero);
    expect(seen, [false, true, false]);
  });
}
