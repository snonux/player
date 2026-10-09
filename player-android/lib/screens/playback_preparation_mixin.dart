import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/player_api_client.dart';
import '../providers/api_client_provider.dart';
import '../providers/playback_preparer_provider.dart';
import '../providers/public_api_client_provider.dart';
import '../services/playback_preparer.dart';
import '../services/playback_request.dart';

/// Shared by the audio and video player screens: waits for a server
/// compatibility stream to be ready before a native player opens it, and
/// stops waiting when the screen goes away or starts another item.
///
/// Behaviour worth knowing:
///   - The "preparing" label is shown for every compatibility stream, also
///     when the first probe already answers "ready": until that answer
///     arrives the app cannot tell a finished rendition from a transcode
///     that just started, and the server may hold the request for a while.
///   - Preparation starts after the screen claimed the shared player, so
///     whatever was playing before (for example the previous audio item)
///     stops when preparation starts, not when the new item is ready.
///   - Leaving the screen cancels the wait, for audio too: background audio
///     only begins once a source is loaded, so there is nothing to carry on
///     while the server is still transcoding. The transcode itself continues
///     on the server; opening the item again picks it up.
mixin PlaybackPreparationMixin<T extends ConsumerStatefulWidget>
    on ConsumerState<T> {
  PlaybackPreparation? _preparation;
  bool _preparing = false;

  /// True from the first probe until [cancelPlaybackPreparation]; the screen
  /// shows the "preparing" label next to its loading spinner meanwhile. It
  /// deliberately stays set after the stream is ready, so the label is
  /// replaced by the player itself and not by a bare spinner in between.
  bool get isPreparingPlayback => _preparing;

  /// Completes with true when [request] can be handed to the player and with
  /// false when the wait was cancelled or [current] turned false (another
  /// item took over the player). Fails with a
  /// [PlaybackPreparationException] when the server will not serve it.
  ///
  /// Only compatibility streams are probed: originals are served directly,
  /// and local files never involve a server.
  Future<bool> waitUntilPlayable(
    PlaybackRequest request, {
    required bool Function() current,
  }) {
    if (request is LocalPlaybackRequest ||
        !isCompatStreamUrl(request.sourceUri)) {
      return Future.value(true);
    }
    // The probe must use the client that owns the URL's credentials: the
    // account client for library media, the anonymous one for share links.
    final client = request is PublicSharePlaybackRequest
        ? ref.read(publicApiClientProvider)
        : ref.read(apiClientProvider);
    _preparation?.cancel();
    final preparation = ref.read(playbackPreparerProvider).prepare(
          request.sourceUri,
          probe: client.probePlayback,
          stillWanted: current,
        );
    _preparation = preparation;
    setState(() => _preparing = true);
    return preparation.ready;
  }

  /// Ends a pending wait and clears the label. Call inside the `setState`
  /// that starts a new attempt (retry or a different item).
  void cancelPlaybackPreparation() {
    _preparation?.cancel();
    _preparation = null;
    _preparing = false;
  }

  @override
  void dispose() {
    _preparation?.cancel();
    super.dispose();
  }
}
