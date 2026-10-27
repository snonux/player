import 'dart:async';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:just_audio/just_audio.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:player_android/services/audio_visuals.dart';
import 'package:player_android/widgets/audio_visuals.dart';
import 'package:player_android/widgets/overflow_menu.dart';

class _Player extends AudioPlayer {
  final sessions = StreamController<int?>.broadcast();
  final playingChanges = StreamController<bool>.broadcast();
  @override
  int? get androidAudioSessionId => 42;
  @override
  Stream<int?> get androidAudioSessionIdStream => sessions.stream;
  @override
  bool get playing => true;
  @override
  Stream<bool> get playingStream => playingChanges.stream;
  @override
  ProcessingState get processingState => ProcessingState.ready;
  @override
  Stream<ProcessingState> get processingStateStream => const Stream.empty();
}

class _Visuals extends AudioVisualsService {
  bool permitted = true;
  int requests = 0;
  int cancellations = 0;
  final sessions = <int>[];
  final framesController = StreamController<AudioVisualFrame>.broadcast();

  @override
  Future<bool> permission({bool request = false}) async {
    if (request) requests++;
    return permitted;
  }

  @override
  Stream<AudioVisualFrame> frames(int sessionId) {
    sessions.add(sessionId);
    framesController.onCancel = () {
      cancellations++;
    };
    return framesController.stream;
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('Android FFT signed pairs and unsigned waveform decode correctly', () {
    final fft = Uint8List(512);
    fft[2] = 128; // -128, not +128 or a wrapped positive byte.
    fft[3] = 127;
    final frame = AudioVisualFrame.fromEvent({
      'waveform': Uint8List.fromList([0, 128, 255]),
      'fft': fft,
    });
    expect(frame.waveform, [-1, 0, 127 / 128]);
    expect(frame.spectrum, hasLength(48));
    expect(frame.spectrum.first, greaterThan(.99));
    expect(frame.spectrum.skip(1), everyElement(0));
    expect(isAudiobook('Chapter.M4B /api/media/1/stream'), isTrue);
    expect(isAudiobook('Audio Books/chapter.mp3'), isTrue);
    expect(isAudiobook('notanaudiobook/song.mp3'), isFalse);
  });

  late _Player player;
  late _Visuals service;
  var created = false;
  setUp(() {
    SharedPreferences.setMockInitialValues({});
    created = false;
  });
  tearDown(() async {
    if (!created) return;
    await player.sessions.close();
    await player.playingChanges.close();
    await player.dispose();
    await service.framesController.close();
  });

  Future<void> flush(WidgetTester tester) async {
    await tester.pumpAndSettle();
    await tester.runAsync(() => Future<void>.delayed(Duration.zero));
    await tester.pumpAndSettle();
  }

  Future<void> mount(WidgetTester tester,
      {String title = 'Song.mp3', bool reducedMotion = false}) async {
    // Stream cancellation futures must belong to the widget test's clock.
    if (!created) {
      player = _Player();
      service = _Visuals();
      created = true;
    }
    await tester.pumpWidget(MaterialApp(
        home: MediaQuery(
      data: MediaQueryData(disableAnimations: reducedMotion),
      child: Scaffold(
          body: AudioVisuals(
              player: player,
              title: title,
              service: service,
              artwork: const Text('Cover artwork'))),
    )));
    await flush(tester);
  }

  Future<void> choose(WidgetTester tester, AudioVisualMode mode) async {
    final state = tester.state<AudioVisualsState>(find.byType(AudioVisuals));
    final choosing = state.choose(mode);
    await flush(tester);
    await choosing;
    await flush(tester);
  }

  const frame = AudioVisualFrame([-1, 1, -.5, .5], [.3, .8, .2, 1]);

  testWidgets('saved mode resumes with existing permission without prompting',
      (tester) async {
    SharedPreferences.setMockInitialValues({'player-audio-visual': 'orbit'});
    await mount(tester);
    expect(service.requests, 0);
    expect(service.sessions, [42]);
    service.framesController.add(frame);
    await flush(tester);
    expect(find.byKey(const Key('audio_visual_canvas')), findsOneWidget);
    expect(tester.state<AudioVisualsState>(find.byType(AudioVisuals)).mode,
        AudioVisualMode.orbit);
  });

  testWidgets(
      'Off needs no permission; denied access preserves artwork and selector',
      (tester) async {
    await mount(tester);
    expect(service.requests, 0);
    expect(service.sessions, isEmpty);
    service.permitted = false;
    await choose(tester, AudioVisualMode.bars);
    expect(service.requests, 1);
    expect(service.sessions, isEmpty);
    expect(find.text('Cover artwork'), findsOneWidget);
    expect(find.textContaining('Allow audio access'), findsOneWidget);
    service.permitted = true;
    await choose(tester, AudioVisualMode.wave);
    expect(service.sessions.last, 42);
    service.framesController.add(frame);
    await flush(tester);
    expect(find.byKey(const Key('audio_visual_canvas')), findsOneWidget);
    await choose(tester, AudioVisualMode.off);
    expect(find.text('Cover artwork'), findsOneWidget);
    expect(service.cancellations, greaterThan(0));
  });

  testWidgets('all modes use real frames and save the preference',
      (tester) async {
    await mount(tester);
    for (final mode in AudioVisualMode.values
        .where((value) => value != AudioVisualMode.off)) {
      await choose(tester, mode);
      service.framesController.add(frame);
      await flush(tester);
      final painter = tester
          .widgetList<CustomPaint>(find.byType(CustomPaint))
          .map((widget) => widget.painter)
          .whereType<AudioVisualPainter>()
          .single;
      expect(painter.mode, mode);
      expect(painter.frame, same(frame));
      expect(
          (await SharedPreferences.getInstance())
              .getString('player-audio-visual'),
          mode.name);
    }
    expect(tester.takeException(), isNull);
  });

  testWidgets(
      'pause, background and disposal release capture; resume binds the current session',
      (tester) async {
    await mount(tester);
    await choose(tester, AudioVisualMode.bars);
    final started = service.sessions.length;
    player.playingChanges.add(false);
    await flush(tester);
    expect(service.cancellations, greaterThan(0));
    player.sessions.add(99);
    await tester.pump();
    await flush(tester);
    expect(service.sessions.length, started);
    player.playingChanges.add(true);
    await tester.pump();
    await flush(tester);
    expect(service.sessions.last, 99);
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.paused);
    await flush(tester);
    final cancelled = service.cancellations;
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    await flush(tester);
    expect(service.sessions.last, 99);
    await tester.pumpWidget(const SizedBox());
    await flush(tester);
    expect(service.cancellations, greaterThan(cancelled));
  });

  testWidgets('audiobooks and reduced motion suppress saved visuals',
      (tester) async {
    SharedPreferences.setMockInitialValues({'player-audio-visual': 'bars'});
    await mount(tester, title: 'Audiobooks/chapter.mp3');
    expect(service.sessions, isEmpty);
    expect(
        tester.state<AudioVisualsState>(find.byType(AudioVisuals)).offersModes,
        isFalse);
    await tester.pumpWidget(const SizedBox());
    await mount(tester, reducedMotion: true);
    expect(service.sessions, isEmpty);
    expect(tester.state<AudioVisualsState>(find.byType(AudioVisuals)).mode,
        AudioVisualMode.off);
  });

  testWidgets('native failure returns to artwork without affecting playback',
      (tester) async {
    await mount(tester);
    await choose(tester, AudioVisualMode.bars);
    service.framesController.addError(StateError('Effect unavailable'));
    await flush(tester);
    expect(find.text('Cover artwork'), findsOneWidget);
    expect(find.textContaining('Playback continues'), findsOneWidget);
    expect(player.playing, isTrue);
    expect(tester.takeException(), isNull);
  });

  testWidgets('menu row picks the mode for music and is absent for audiobooks',
      (tester) async {
    for (final title in ['Song.mp3', 'Audiobooks/chapter.mp3']) {
      SharedPreferences.setMockInitialValues({});
      if (!created) {
        player = _Player();
        service = _Visuals();
        created = true;
      }
      final visuals = GlobalKey<AudioVisualsState>();
      await tester.pumpWidget(MaterialApp(
          home: Builder(
              builder: (context) => Scaffold(
                  appBar: AppBar(actions: [
                    OverflowMenuButton(
                        items: (_) => [
                              AudioVisualsMenuItem(
                                  visuals: visuals, playerContext: context)
                            ]),
                  ]),
                  body: AudioVisuals(
                      key: visuals,
                      player: player,
                      title: title,
                      service: service,
                      artwork: const Text('Cover artwork'))))));
      await flush(tester);
      expect(find.text('Music visuals'), findsNothing,
          reason: 'the mode picker is not on the player screen');
      await tester.tap(find.byTooltip('More options'));
      await flush(tester);
      if (title.startsWith('Audiobooks')) {
        expect(find.byKey(const Key('audio_visual_mode')), findsNothing);
        continue;
      }
      expect(find.text('Off'), findsOneWidget);
      await tester.tap(find.byKey(const Key('audio_visual_mode')));
      await flush(tester);
      await tester.tap(find.byKey(const Key('audio_visual_mode_bars')));
      await flush(tester);
      expect(visuals.currentState!.mode, AudioVisualMode.bars);
      expect(
          (await SharedPreferences.getInstance())
              .getString('player-audio-visual'),
          'bars');
      await tester.pumpWidget(const SizedBox());
      await flush(tester);
    }
  });
}
