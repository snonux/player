import 'package:flutter/services.dart' show PlatformException;
import 'package:just_audio/just_audio.dart' show PlayerException;

import '../services/playback_preparer.dart';

/// Shown next to the spinner while the server prepares a compatibility
/// stream. The live end-to-end script waits for this exact text to go away,
/// so it must not contain any word that script treats as a failure.
const kPreparingPlaybackLabel = 'Preparing playback…';

/// Turns a playback failure into a sentence for the player's error view.
///
/// Native players report failures as opaque exception texts such as
/// `PlatformException(VideoError, Video player had error V.l: Source error,
/// null, null)` or `(0) Source error`. Those never reach the user: the
/// message names the item ([title], normally the file name) and says what
/// went wrong in plain words.
String playbackErrorMessage(Object error, {required String title}) {
  if (error is PlaybackPreparationException) {
    return _preparationErrorMessage(error, title);
  }
  // ExoPlayer reports an undecodable container or codec as a generic source
  // error through these two types (video_player and just_audio). The server
  // already converts what it knows to be unplayable, so what is left here is
  // a format this particular device cannot decode — also the usual cause for
  // files in the local library, where no server can convert them.
  if (error is PlatformException || error is PlayerException) {
    return unsupportedFormatMessage(title);
  }
  return 'Could not start playback of “$title”. Please try again.';
}

/// The sentence for a source the device's decoders reject. Also used where a
/// player reports such a failure without an exception object.
String unsupportedFormatMessage(String title) =>
    'Cannot play “$title”. This format cannot be played on this device.';

String _preparationErrorMessage(
  PlaybackPreparationException error,
  String title,
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
        'Cannot play “$title”. The server refused to prepare this file'
            '${error.statusCode == null ? '' : ' (HTTP ${error.statusCode})'}.',
    };
