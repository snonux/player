// Unit tests for PlayerAudioHandler's failure handling.
//
// just_audio reports an undecodable source twice: thrown from setAudioSource
// and added as an error to playbackEventStream. The second report used to be
// an unhandled `PlatformException(0, Source error, {index: 0})`. A test fails
// on any unhandled asynchronous error, so reaching the expectations already
// proves the handler caught it.

import 'dart:async';

import 'package:audio_service/audio_service.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:just_audio/just_audio.dart';
import 'package:player_android/services/audio_handler.dart';

class _Player extends AudioPlayer {
  final events = StreamController<PlaybackEvent>.broadcast();
  Object? playError;
  Object? loadError;

  @override
  Stream<PlaybackEvent> get playbackEventStream => events.stream;
  @override
  Stream<bool> get playingStream => const Stream.empty();
  @override
  Stream<ProcessingState> get processingStateStream => const Stream.empty();
  @override
  ProcessingState get processingState => ProcessingState.ready;
  @override
  bool get playing => false;
  @override
  Duration get position => Duration.zero;
  @override
  Duration? get duration => const Duration(seconds: 100);

  @override
  Future<Duration?> setAudioSource(
    AudioSource source, {
    bool preload = true,
    int? initialIndex,
    Duration? initialPosition,
  }) async {
    if (loadError != null) throw loadError!;
    return duration;
  }

  @override
  Future<void> play() async {
    if (playError != null) throw playError!;
  }

  @override
  Future<void> stop() async {}
}

PlatformException _sourceError() => PlatformException(
      code: '0',
      message: 'Source error',
      details: {'index': 0},
    );

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('a source error on the event stream is republished, not unhandled',
      () async {
    final player = _Player();
    final handler = PlayerAudioHandler(player);
    final reported = <Object>[];
    final subscription = handler.playbackErrors.listen(reported.add);
    addTearDown(subscription.cancel);
    addTearDown(player.events.close);

    final error = _sourceError();
    player.events.addError(error);
    await Future<void>.delayed(Duration.zero);

    expect(reported, [error]);
    expect(
      handler.playbackState.value.processingState,
      AudioProcessingState.error,
    );
    expect(handler.playbackState.value.playing, isFalse);
  });

  test('a source error with no screen listening is still swallowed', () async {
    final player = _Player();
    PlayerAudioHandler(player);
    addTearDown(player.events.close);

    player.events.addError(_sourceError());
    await Future<void>.delayed(Duration.zero);
  });

  test('a failing play() is reported instead of thrown', () async {
    final player = _Player()..playError = _sourceError();
    final handler = PlayerAudioHandler(player);
    final reported = <Object>[];
    final subscription = handler.playbackErrors.listen(reported.add);
    addTearDown(subscription.cancel);
    addTearDown(player.events.close);
    final generation = handler.beginSourceSession();
    expect(handler.activateSourceSession(generation), isTrue);

    await handler.play();
    await Future<void>.delayed(Duration.zero);

    expect(reported, [player.playError]);
  });

  test('a load failure is still thrown to the caller that loads', () async {
    final player = _Player()..loadError = PlayerException(0, 'Source error');
    final handler = PlayerAudioHandler(player);
    addTearDown(player.events.close);
    final generation = handler.beginSourceSession();

    await expectLater(
      handler.loadSourceForSession(
        AudioSource.uri(Uri.parse('content://provider/song.wma')),
        generation,
      ),
      throwsA(isA<PlayerException>()),
    );
  });
}
