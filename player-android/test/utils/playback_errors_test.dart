// Unit tests for playbackErrorMessage: raw player exceptions must never
// reach the user, and every message must name the item.
//
// The live end-to-end script (test/e2e-live) recognises a failed playback by
// UI text matching _kFailurePattern, and waits for the preparing label to
// disappear, so both properties are pinned here.

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:just_audio/just_audio.dart';
import 'package:player_android/services/playback_preparer.dart';
import 'package:player_android/utils/playback_errors.dart';

final _kFailurePattern = RegExp(
  'error|failed|unsupported|cannot|could not|unable',
  caseSensitive: false,
);

void main() {
  const formatMessage =
      'Cannot play “clip.wmv”. This format cannot be played on this device.';

  test('video_player source error becomes a readable format message', () {
    final message = playbackErrorMessage(
      PlatformException(
        code: 'VideoError',
        message: 'Video player had error V.l: Source error',
      ),
      title: 'clip.wmv',
    );
    expect(message, formatMessage);
  });

  test('just_audio load failure becomes a readable format message', () {
    // toString() of this exception is the raw "(0) Source error" text.
    final error = PlayerException(0, 'Source error');
    expect(playbackErrorMessage(error, title: 'clip.wmv'), formatMessage);
  });

  test('just_audio stream error becomes a readable format message', () {
    final error = PlatformException(
      code: '0',
      message: 'Source error',
      details: {'index': 0},
    );
    expect(playbackErrorMessage(error, title: 'clip.wmv'), formatMessage);
  });

  test('other failures get a generic message without the exception text', () {
    final message =
        playbackErrorMessage(StateError('secret detail'), title: 'song.mp3');
    expect(
        message, 'Could not start playback of “song.mp3”. Please try again.');
  });

  test('each preparation failure has its own message', () {
    String messageFor(PlaybackPreparationFailure failure, {int? status}) =>
        playbackErrorMessage(
          PlaybackPreparationException(failure, statusCode: status),
          title: 'clip.wmv',
        );

    expect(
      messageFor(PlaybackPreparationFailure.conversionFailed, status: 500),
      'Cannot play “clip.wmv”. The server could not convert this file.',
    );
    expect(
      messageFor(PlaybackPreparationFailure.outOfStorage, status: 507),
      'Cannot play “clip.wmv”. The server is out of storage space '
      'for converted files.',
    );
    expect(
      messageFor(PlaybackPreparationFailure.timedOut),
      'Could not prepare “clip.wmv” in time. Please try again later.',
    );
    expect(
      messageFor(PlaybackPreparationFailure.unreachable),
      'Could not reach the server to play “clip.wmv”. '
      'Check your connection and try again.',
    );
    expect(
      messageFor(PlaybackPreparationFailure.rejected, status: 400),
      'Cannot play “clip.wmv”. The server refused to prepare this file '
      '(HTTP 400).',
    );
  });

  test('no message leaks exception class names or native error text', () {
    final errors = <Object>[
      PlatformException(code: 'VideoError', message: 'V.l: Source error'),
      PlayerException(0, 'Source error'),
      StateError('boom'),
      for (final failure in PlaybackPreparationFailure.values)
        PlaybackPreparationException(failure, statusCode: 500),
    ];
    for (final error in errors) {
      final message = playbackErrorMessage(error, title: 'clip.wmv');
      expect(message, contains('clip.wmv'));
      expect(message, isNot(contains('Exception')));
      expect(message, isNot(contains('Source error')));
      expect(message, isNot(contains('Playback failed:')));
      expect(_kFailurePattern.hasMatch(message), isTrue, reason: message);
    }
  });

  test('the preparing label is not mistaken for a failure', () {
    expect(kPreparingPlaybackLabel, 'Preparing playback…');
    expect(_kFailurePattern.hasMatch(kPreparingPlaybackLabel), isFalse);
  });
}
