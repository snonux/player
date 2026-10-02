import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:just_audio/just_audio.dart';
import 'package:player_android/services/audio_handler.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/services/playback_session_coordinator.dart';

class _Player extends AudioPlayer {
  final changes = StreamController<bool>.broadcast();
  final processing = StreamController<ProcessingState>.broadcast();
  Duration? total = const Duration(seconds: 100);
  Duration elapsed = const Duration(seconds: 8);
  bool isPlaying = true;
  bool isStopped = false;
  Completer<Duration?>? pendingSource;
  AudioSource? loadedSource;
  final seeks = <Duration>[];
  int sourceRequests = 0;
  int playCalls = 0;
  double selectedSpeed = 1;

  @override
  Future<void> play() async {
    playCalls++;
    isStopped = false;
    isPlaying = true;
  }

  @override
  Future<void> setSpeed(double speed) async {
    selectedSpeed = speed;
  }

  @override
  Duration get position => elapsed;
  @override
  Duration? get duration => total;
  @override
  bool get playing => isPlaying;
  @override
  Stream<bool> get playingStream => changes.stream;
  @override
  Stream<PlaybackEvent> get playbackEventStream => const Stream.empty();
  @override
  Stream<ProcessingState> get processingStateStream => processing.stream;
  @override
  ProcessingState get processingState =>
      isStopped ? ProcessingState.idle : ProcessingState.ready;

  @override
  Future<void> pause() async {
    isPlaying = false;
    changes.add(false);
  }

  @override
  Future<void> stop() async {
    isStopped = true;
    isPlaying = false;
    changes.add(false);
  }

  @override
  Future<Duration?> setAudioSource(
    AudioSource source, {
    bool preload = true,
    int? initialIndex,
    Duration? initialPosition,
  }) {
    isStopped = false;
    sourceRequests++;
    loadedSource = source;
    return pendingSource?.future ?? Future.value(const Duration(seconds: 100));
  }

  @override
  Future<void> seek(Duration? position, {int? index}) async {
    seeks.add(position ?? Duration.zero);
  }
}

void main() {
  test('media controls cannot restart audio after ownership transfer',
      () async {
    final player = _Player();
    final handler = PlayerAudioHandler(player);
    final coordinator = PlaybackSessionCoordinator();
    final lease = await coordinator.claim(
        kind: PlaybackSourceKind.local,
        identity: 'local:7',
        stop: handler.stop);
    final generation =
        handler.beginSourceSession(ownsSource: () => lease!.isCurrent);
    expect(handler.activateSourceSession(generation), isTrue);
    await handler.play();
    expect(player.playCalls, 1);
    await coordinator.claim(
        kind: PlaybackSourceKind.server,
        identity: 'server:video:42',
        stop: () async {});
    await handler.play();
    await handler.seek(const Duration(seconds: 19));
    await handler.skipToNext();
    await handler.skipToPrevious();
    await handler.setSpeed(1.5);
    expect(player.playCalls, 1);
    expect(player.isPlaying, isFalse);
    expect(player.seeks, isEmpty);
    expect(player.selectedSpeed, 1);
    expect(handler.isSourceSessionCurrent(generation), isFalse);
    await player.changes.close();
    await player.processing.close();
  });

  testWidgets('native completion finishes audio with unknown duration once',
      (tester) async {
    final player = _Player()..total = null;
    final handler = PlayerAudioHandler(player);
    var finished = 0;
    final saved = <double>[];
    handler.startProgress(
      savePosition: (seconds) async => saved.add(seconds),
      markFinished: () async => finished++,
    );
    await tester.pump(const Duration(seconds: 5));
    expect(finished, 0);
    player.processing.add(ProcessingState.completed);
    await tester.pump();
    expect(finished, 1);
    player.processing.add(ProcessingState.completed);
    await tester.pump();
    await tester.runAsync(handler.stop);
    expect(finished, 1);
    expect(saved, [8]);
    await player.changes.close();
    await player.processing.close();
  });

  test('stop invalidates a pending source load and stale seeks', () async {
    final player = _Player()..pendingSource = Completer<Duration?>();
    final handler = PlayerAudioHandler(player);
    final oldGeneration = handler.beginSourceSession();
    final loading = handler.loadSourceForSession(
      AudioSource.uri(Uri.parse('content://provider/old')),
      oldGeneration,
    );
    await Future<void>.delayed(Duration.zero);

    final stopping = handler.stop();
    player.pendingSource!.complete(const Duration(seconds: 100));
    await Future.wait([loading, stopping]);

    expect(player.sourceRequests, 1);
    expect(player.isPlaying, isFalse);
    expect(
        await handler.seekForSession(const Duration(seconds: 9), oldGeneration),
        isFalse);
    expect(player.seeks, isEmpty);

    final newGeneration = handler.beginSourceSession();
    expect(
      await handler.seekForSession(const Duration(seconds: 5), newGeneration),
      isTrue,
    );
    expect(player.seeks, [const Duration(seconds: 5)]);
    await player.changes.close();
  });

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
    // Completion must remain the final command: a later position would
    // unset the finished state on the server.
    expect(saved.last, 96);
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
