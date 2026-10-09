import 'package:flutter/services.dart' show PlatformException;
import 'package:just_audio/just_audio.dart' show PlayerException;

import '../services/playback_preparer.dart';
import '../services/playback_request.dart';

/// Shown next to the spinner while a compatibility stream is checked and,
/// if needed, produced by the server. The live end-to-end script waits for
/// this exact text to go away, so it must not contain any word that script
/// treats as a failure.
const kPreparingPlaybackLabel = 'Preparing playback…';

/// Shown when another item took over the player, including while this one
/// was still being prepared. Not a failure: Retry plays the item again.
const kPlaybackStoppedMessage =
    'Playback stopped. Tap Retry to play this item again.';

/// Turns a playback failure into a sentence for the player's error view.
///
/// Native players report failures as opaque exception texts such as
/// `PlatformException(VideoError, Video player had error V.l: Source error,
/// null, null)` or `(0) Source error`. Those never reach the user: the
/// message names the item ([title], normally the file name) and says what
/// went wrong in plain words. [source] decides how definite the cause can be.
///
/// This maps failures of *starting* playback. A failure after playback began
/// is always a source failure, whatever its type: use [sourceErrorMessage].
String playbackErrorMessage(
  Object error, {
  required String title,
  required PlaybackSourceKind source,
}) {
  if (error is PlaybackPreparationException) {
    return _preparationErrorMessage(error, title, source);
  }
  if (error is PlatformException || error is PlayerException) {
    return sourceErrorMessage(title, source);
  }
  return 'Could not start playback of “$title”. Please try again.';
}

/// The sentence for a source the native player gave up on.
///
/// ExoPlayer reports an undecodable file and a failed download alike as a
/// generic "Source error". For a file on the device only the format can be
/// at fault. For a stream it may as well be the network, the server, or an
/// expired session, so that message names both causes instead of blaming
/// the format.
String sourceErrorMessage(String title, PlaybackSourceKind source) =>
    source == PlaybackSourceKind.local
        ? 'Cannot play “$title”. This format cannot be played on this device.'
        : 'Cannot play “$title”. The connection may have been interrupted, '
            'or this format is not supported.';

String _preparationErrorMessage(
  PlaybackPreparationException error,
  String title,
  PlaybackSourceKind source,
) =>
    switch (error.failure) {
      PlaybackPreparationFailure.conversionFailed =>
        'Cannot play “$title”. The server could not convert this file.',
      PlaybackPreparationFailure.outOfStorage =>
        'Cannot play “$title”. The server is out of storage space '
            'for converted files.',
      PlaybackPreparationFailure.timedOut =>
        'Could not prepare “$title” in time. Please try again later.',
      PlaybackPreparationFailure.unreachable =>
        'Could not reach the server to play “$title”. '
            'Check your connection and try again.',
      PlaybackPreparationFailure.rejected =>
        _rejectedMessage(error.statusCode, title, source),
    };

// A share link answers these once it expired, was revoked, or is refused.
const _kDeadShareStatuses = {401, 403, 404, 410};

String _rejectedMessage(
  int? statusCode,
  String title,
  PlaybackSourceKind source,
) {
  if (source == PlaybackSourceKind.publicShare &&
      _kDeadShareStatuses.contains(statusCode)) {
    return 'Cannot play “$title”. This share link is no longer valid '
        'or access was refused.';
  }
  return 'Cannot play “$title”. The server refused to prepare this file'
      '${statusCode == null ? '' : ' (HTTP $statusCode)'}.';
}
