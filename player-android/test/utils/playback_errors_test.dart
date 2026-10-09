// Unit tests for playbackErrorMessage: raw player exceptions must never
// reach the user, every message must name the item, and a cause is only
// stated as definite where it is.
//
// The live end-to-end script (test/e2e-live) recognises a failed playback by
// UI text matching _kFailurePattern, and waits for the preparing label to
// disappear, so both properties are pinned here.

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:just_audio/just_audio.dart';
import 'package:player_android/services/playback_preparer.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/utils/playback_errors.dart';

final _kFailurePattern = RegExp(
  'error|failed|unsupported|cannot|could not|unable',
  caseSensitive: false,
);

const _streamMessage = 'Cannot play “clip.wmv”. The connection may have been '
    'interrupted, or this format is not supported.';
const _localMessage =
    'Cannot play “clip.wmv”. This format cannot be played on this device.';

/// The three shapes in which the native players report a broken source.
List<Object> _sourceErrors() => [
      // video_player: initialize() or a failure during playback.
      PlatformException(
        code: 'VideoError',
        message: 'Video player had error V.l: Source error',
      ),
      // just_audio: thrown from setAudioSource; toString is "(0) Source error".
      PlayerException(0, 'Source error'),
      // just_audio: added to the playback event stream.
      PlatformException(
          code: '0', message: 'Source error', details: {'index': 0}),
    ];

void main() {
  test('a failing stream names both possible causes, not the format alone', () {
    // Offline, server down, 401/404 on /stream, a dropped connection and an
    // evicted rendition all look like this to the app.
    for (final source in [
      PlaybackSourceKind.server,
      PlaybackSourceKind.publicShare,
    ]) {
      for (final error in _sourceErrors()) {
        expect(
          playbackErrorMessage(error, title: 'clip.wmv', source: source),
          _streamMessage,
        );
      }
    }
  });

  test('a failing local file can only be an unsupported format', () {
    for (final error in _sourceErrors()) {
      expect(
        playbackErrorMessage(
          error,
          title: 'clip.wmv',
          source: PlaybackSourceKind.local,
        ),
        _localMessage,
      );
    }
    expect(sourceErrorMessage('clip.wmv', PlaybackSourceKind.local),
        _localMessage);
    expect(sourceErrorMessage('clip.wmv', PlaybackSourceKind.server),
        _streamMessage);
  });

  test('other failures get a generic message without the exception text', () {
    final message = playbackErrorMessage(
      StateError('secret detail'),
      title: 'song.mp3',
      source: PlaybackSourceKind.server,
    );
    expect(
        message, 'Could not start playback of “song.mp3”. Please try again.');
  });

  String preparationMessage(
    PlaybackPreparationFailure failure, {
    int? status,
    PlaybackSourceKind source = PlaybackSourceKind.server,
  }) =>
      playbackErrorMessage(
        PlaybackPreparationException(failure, statusCode: status),
        title: 'clip.wmv',
        source: source,
      );

  test('each preparation failure has its own message', () {
    expect(
      preparationMessage(PlaybackPreparationFailure.conversionFailed,
          status: 500),
      'Cannot play “clip.wmv”. The server could not convert this file.',
    );
    expect(
      preparationMessage(PlaybackPreparationFailure.outOfStorage, status: 507),
      'Cannot play “clip.wmv”. The server is out of storage space '
      'for converted files.',
    );
    expect(
      preparationMessage(PlaybackPreparationFailure.timedOut),
      'Could not prepare “clip.wmv” in time. Please try again later.',
    );
    expect(
      preparationMessage(PlaybackPreparationFailure.unreachable),
      'Could not reach the server to play “clip.wmv”. '
      'Check your connection and try again.',
    );
    expect(
      preparationMessage(PlaybackPreparationFailure.rejected, status: 400),
      'Cannot play “clip.wmv”. The server refused to prepare this file '
      '(HTTP 400).',
    );
  });

  test('a share link that expired or is refused says so', () {
    for (final status in [401, 403, 404, 410]) {
      expect(
        preparationMessage(
          PlaybackPreparationFailure.rejected,
          status: status,
          source: PlaybackSourceKind.publicShare,
        ),
        'Cannot play “clip.wmv”. This share link is no longer valid '
        'or access was refused.',
      );
    }
    // Not a dead link: a share whose file needs no conversion.
    expect(
      preparationMessage(
        PlaybackPreparationFailure.rejected,
        status: 400,
        source: PlaybackSourceKind.publicShare,
      ),
      contains('(HTTP 400)'),
    );
    // Library media has no share link to blame.
    expect(
      preparationMessage(PlaybackPreparationFailure.rejected, status: 403),
      contains('(HTTP 403)'),
    );
  });

  test('no message leaks exception class names or native error text', () {
    final errors = <Object>[
      ..._sourceErrors(),
      StateError('boom'),
      for (final failure in PlaybackPreparationFailure.values)
        PlaybackPreparationException(failure, statusCode: 500),
    ];
    for (final source in PlaybackSourceKind.values) {
      for (final error in errors) {
        final message =
            playbackErrorMessage(error, title: 'clip.wmv', source: source);
        expect(message, contains('clip.wmv'));
        expect(message, isNot(contains('Exception')));
        expect(message, isNot(contains('Source error')));
        expect(message, isNot(contains('Playback failed:')));
        expect(_kFailurePattern.hasMatch(message), isTrue, reason: message);
      }
    }
  });

  test('the preparing and stopped texts are not mistaken for a failure', () {
    expect(kPreparingPlaybackLabel, 'Preparing playback…');
    expect(_kFailurePattern.hasMatch(kPreparingPlaybackLabel), isFalse);
    expect(
      kPlaybackStoppedMessage,
      'Playback stopped. Tap Retry to play this item again.',
    );
    expect(_kFailurePattern.hasMatch(kPlaybackStoppedMessage), isFalse);
  });
}
