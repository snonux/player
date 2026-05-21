import 'package:audio_service/audio_service.dart';
import 'package:just_audio/just_audio.dart';

// ---------------------------------------------------------------------------
// PlayerAudioHandler
// ---------------------------------------------------------------------------

/// [BaseAudioHandler] subclass that wraps a [just_audio] [AudioPlayer] and
/// bridges it to the Android media-session / iOS AVAudioSession so that:
///   - Playback continues in the background as a foreground service.
///   - Lock-screen and notification controls (play/pause/seek/skip) work.
///   - Audio focus is honoured: playback pauses on phone calls or other
///     audio interruptions and resumes when focus is regained.
///   - Bluetooth headset media buttons (play, pause, next, previous) are
///     forwarded by [AudioService] and handled here.
///
/// Single Responsibility: this class only translates between the
/// [BaseAudioHandler] protocol and [AudioPlayer]'s API.  All progress
/// reporting and navigation logic lives in [AudioPlayerScreen].
///
/// The handler is registered once via [AudioService.init] in [main].
/// Consumers retrieve the singleton through [audioHandlerProvider].
class PlayerAudioHandler extends BaseAudioHandler with SeekHandler {
  /// Creates the handler with an already-configured [AudioPlayer].
  ///
  /// The player is injected so that tests can supply a mock without any
  /// platform channels (Dependency Inversion Principle).
  PlayerAudioHandler(this._player) {
    // Propagate just_audio's playback state into the audio_service stream so
    // the notification, lock screen, and Wear OS clients see live updates.
    _player.playbackEventStream.listen(_onPlaybackEvent);

    // Propagate playing/paused transitions, which are not always carried in
    // playback events (just_audio emits them separately).
    _player.playingStream.listen((_) => _broadcastState());

    // Propagate processing-state changes (e.g. loading → ready → completed).
    _player.processingStateStream.listen((_) => _broadcastState());
  }

  final AudioPlayer _player;

  // ---------------------------------------------------------------------------
  // Public accessors (used by AudioPlayerScreen to avoid a second Player)
  // ---------------------------------------------------------------------------

  /// The underlying [AudioPlayer] so the screen can subscribe to position /
  /// duration streams and still use bearer-token authenticated sources.
  ///
  /// Exposing the player directly is intentional: [AudioPlayerScreen] owns the
  /// progress-sync timer and needs raw position/duration access.  No extra
  /// abstraction layer is needed here (YAGNI).
  AudioPlayer get player => _player;

  // ---------------------------------------------------------------------------
  // BaseAudioHandler — playback controls
  // ---------------------------------------------------------------------------

  @override
  Future<void> play() => _player.play();

  @override
  Future<void> pause() => _player.pause();

  @override
  Future<void> stop() async {
    await _player.stop();
    await super.stop();
  }

  /// Seeks to [position] within the current item.
  ///
  /// [SeekHandler] mixin provides the default [fastForward] / [rewind]
  /// implementations in terms of this method.
  @override
  Future<void> seek(Duration position) => _player.seek(position);

  /// Skip forward 15 seconds (media-button "next" maps to a short skip for
  /// podcast and audiobook use-cases rather than a full track change).
  @override
  Future<void> skipToNext() async {
    final current = _player.position;
    final total = _player.duration ?? Duration.zero;
    final next = _clamp(current + const Duration(seconds: 15), Duration.zero, total);
    await _player.seek(next);
  }

  /// Skip back 15 seconds.
  @override
  Future<void> skipToPrevious() async {
    final current = _player.position;
    final total = _player.duration ?? Duration.zero;
    final prev = _clamp(current - const Duration(seconds: 15), Duration.zero, total);
    await _player.seek(prev);
  }

  /// Changes playback speed and refreshes the notification state.
  @override
  Future<void> setSpeed(double speed) async {
    await _player.setSpeed(speed);
    _broadcastState();
  }

  // ---------------------------------------------------------------------------
  // Media item helpers (called by AudioPlayerScreen after player is ready)
  // ---------------------------------------------------------------------------

  /// Pushes a new [MediaItem] onto the [mediaItem] stream so the Android media
  /// notification and lock-screen controls show the correct title and duration.
  ///
  /// Named [setMediaItem] rather than [updateMediaItem] to avoid clashing with
  /// [BaseAudioHandler.updateMediaItem] (which takes a full [MediaItem] and
  /// notifies children in a queue scenario — not applicable here).
  ///
  /// Called once per session after the player finishes loading the source.
  void setMediaItem({required String id, required String title}) {
    mediaItem.add(
      MediaItem(
        id: id,
        title: title,
        duration: _player.duration,
      ),
    );
  }

  // ---------------------------------------------------------------------------
  // Internal — state broadcast
  // ---------------------------------------------------------------------------

  /// Converts [just_audio]'s [PlaybackEvent] into [audio_service]'s
  /// [PlaybackState] and pushes it onto the [playbackState] stream.
  ///
  /// Called on every playback event so the notification and lock-screen
  /// controls always reflect the true player state.
  void _onPlaybackEvent(PlaybackEvent event) => _broadcastState();

  /// Emits the current [PlaybackState] derived from the underlying player.
  ///
  /// Maps [just_audio]'s processing state to [audio_service]'s equivalents and
  /// advertises which controls are enabled so Android can render the correct
  /// notification buttons.
  void _broadcastState() {
    final processingState = _mapProcessingState(_player.processingState);

    playbackState.add(
      PlaybackState(
        // Advertise all supported actions so the notification, lock screen,
        // and Bluetooth headset buttons all get the correct controls.
        controls: [
          MediaControl.skipToPrevious, // maps to skipToPrevious (–15 s)
          if (_player.playing) MediaControl.pause else MediaControl.play,
          MediaControl.stop,
          MediaControl.skipToNext, // maps to skipToNext (+15 s)
        ],
        systemActions: const {
          MediaAction.seek,
          MediaAction.seekForward,
          MediaAction.seekBackward,
        },
        androidCompactActionIndices: const [0, 1, 3], // prev, play/pause, next
        processingState: processingState,
        playing: _player.playing,
        updatePosition: _player.position,
        bufferedPosition: _player.bufferedPosition,
        speed: _player.speed,
      ),
    );
  }

  /// Translates [just_audio]'s [ProcessingState] to [audio_service]'s
  /// [AudioProcessingState] so the notification can show loading spinners.
  AudioProcessingState _mapProcessingState(ProcessingState state) {
    switch (state) {
      case ProcessingState.idle:
        return AudioProcessingState.idle;
      case ProcessingState.loading:
        return AudioProcessingState.loading;
      case ProcessingState.buffering:
        return AudioProcessingState.buffering;
      case ProcessingState.ready:
        return AudioProcessingState.ready;
      case ProcessingState.completed:
        return AudioProcessingState.completed;
    }
  }

  // ---------------------------------------------------------------------------
  // Helpers
  // ---------------------------------------------------------------------------

  /// Clamps [value] to [[min], [max]]; [Duration] doesn't implement [Comparable]
  /// so we do the comparison manually (mirrors VideoPlayerScreen pattern).
  Duration _clamp(Duration value, Duration min, Duration max) {
    if (value < min) return min;
    if (max > Duration.zero && value > max) return max;
    return value;
  }
}
