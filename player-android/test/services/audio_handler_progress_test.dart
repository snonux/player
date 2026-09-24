import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:just_audio/just_audio.dart';
import 'package:player_android/services/audio_handler.dart';

class _Player extends AudioPlayer {
  final changes = StreamController<bool>.broadcast();
  Duration elapsed = const Duration(seconds: 8);
  bool isPlaying = true;

  @override
  Duration get position => elapsed;
  @override
  Duration? get duration => const Duration(seconds: 100);
  @override
  bool get playing => isPlaying;
  @override
  Stream<bool> get playingStream => changes.stream;
  @override
  Stream<PlaybackEvent> get playbackEventStream => const Stream.empty();
  @override
  Stream<ProcessingState> get processingStateStream => const Stream.empty();
  @override
  ProcessingState get processingState => ProcessingState.ready;

  @override
  Future<void> pause() async {
    isPlaying = false;
    changes.add(false);
  }

  @override
  Future<void> stop() async {
    isPlaying = false;
    changes.add(false);
  }
}

void main() {
  testWidgets('handler reports after route disposal and saves pause position',
      (tester) async {
    final player = _Player();
    final handler = PlayerAudioHandler(player);
    final saved = <double>[];
    var finished = 0;
    handler.startProgress(
      savePosition: (seconds) async => saved.add(seconds),
      markFinished: () async => finished++,
    );
    addTearDown(() async {
      await handler.endProgress();
      await player.changes.close();
    });

    await tester.pumpWidget(const MaterialApp(home: Text('player route')));
    await tester.pumpWidget(const MaterialApp(home: Text('library route')));
    player.elapsed = const Duration(seconds: 19);
    await tester.pump(const Duration(seconds: 5));
    await tester.runAsync(() async => Future<void>.delayed(Duration.zero));
    expect(saved, contains(19));

    player.elapsed = const Duration(seconds: 96);
    await tester.pump(const Duration(seconds: 5));
    await tester.runAsync(() async => Future<void>.delayed(Duration.zero));
    expect(saved, contains(96));
    expect(finished, 1);

    player.elapsed = const Duration(seconds: 97);
    await handler.pause(); // media-session pause after route disposal
    expect(saved.last, 97);
    await tester
        .runAsync(handler.stop); // notification stop after route disposal
    expect(finished, 1);
  });

  testWidgets('handler stop captures final position before stopping the player',
      (tester) async {
    final player = _Player();
    final handler = PlayerAudioHandler(player);
    final saved = <double>[];
    handler.startProgress(
      savePosition: (seconds) async => saved.add(seconds),
      markFinished: () async {},
    );
    addTearDown(player.changes.close);

    player.elapsed = const Duration(milliseconds: 24700);
    await tester.runAsync(handler.stop);
    expect(saved, [24.7]);
    await tester.pump(const Duration(seconds: 10));
    expect(saved, [24.7]);
  });
}
