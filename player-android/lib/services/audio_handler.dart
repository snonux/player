import 'dart:async';

import 'package:audio_service/audio_service.dart';
import 'package:just_audio/just_audio.dart';

import 'audio_progress_session.dart';
import 'music_sequence.dart';
import 'playback_request.dart';

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
/// The active progress session belongs to this handler so reporting continues
/// when the player screen is disposed during background playback.
///
/// Native playback failures are caught here and republished on
/// [playbackErrors]; nothing the player reports may escape as an unhandled
/// asynchronous error, because the handler outlives every screen.
///
/// The handler is registered once via [AudioService.init] in [main].
/// Consumers retrieve the singleton through [audioHandlerProvider].
class PlayerAudioHandler extends BaseAudioHandler with SeekHandler {
  /// Creates the handler with an already-configured [AudioPlayer].
  ///
  /// The player is injected so that tests can supply a mock without any
  /// platform channels (Dependency Inversion Principle).
  PlayerAudioHandler(AudioPlayer player) : _player = player {
    // Propagate just_audio's playback state into the audio_service stream so
    // the notification, lock screen, and Wear OS clients see live updates.
    // just_audio adds a failure (for example `PlatformException(0, Source
    // error, {index: 0})` for a file ExoPlayer cannot decode) to this stream
    // as an error, in addition to throwing it from setAudioSource. Without
    // an error handler that second report is an unhandled exception.
    _player.playbackEventStream.listen(
      _onPlaybackEvent,
      onError: (Object error, StackTrace _) => _onPlaybackError(error),
    );

    // Propagate playing/paused transitions, which are not always carried in
    // playback events (just_audio emits them separately).
    _player.playingStream.listen((playing) {
      _broadcastState();
      final progress = _progress;
      if (!playing && progress != null) {
        unawaited(progress.record(force: true));
      }
    });

    // Propagate processing-state changes (e.g. loading → ready → completed).
    _player.processingStateStream.listen((state) {
      _broadcastState();
      final progress = _progress;
      if (state == ProcessingState.completed && progress != null) {
        unawaited(_completeTrack(progress));
      }
    });
  }

  final AudioPlayer _player;
  List<PlaybackRequest> _musicEntries = const [];
  int _musicIndex = -1;
  bool _movingTrack = false;
  bool _musicWrap = true;
  bool _collectionEnded = false;
  int? _failedMusicDelta;
  Duration? _retryMusicPosition;
  Duration _musicPosition = Duration.zero;
  StreamSubscription<Duration>? _musicPositions;
  Future<List<PlaybackRequest>> Function()? _expandMusic;
  Future<AudioSource?> Function(PlaybackRequest, bool Function())? _musicLoader;
  void Function(PlaybackRequest)? _musicChanged;
  final _musicChanges = StreamController<MusicSequenceState>.broadcast();
  MusicSequenceState? _musicState;
  MusicSequenceState? get musicState => _musicState;
  Stream<MusicSequenceState> get musicChanges => _musicChanges.stream;

  /// Transfers the displayed source list and account-scoped loader to the background
  /// handler. Neither callback may capture a screen or a WidgetRef.
  void startMusicSequence(
      {bool wrap = true,
      required List<PlaybackRequest> entries,
      required int index,
      required Future<AudioSource?> Function(PlaybackRequest, bool Function())
          load,
      required void Function(PlaybackRequest) changed,
      required Future<List<PlaybackRequest>> Function() expand}) {
    unawaited(_musicPositions?.cancel());
    _musicPosition = _player.position;
    _failedMusicDelta = null;
    _retryMusicPosition = null;
    _musicPositions = _player.positionStream.listen((position) {
      if (_musicIndex >= 0 &&
          _canControlSource &&
          _player.processingState == ProcessingState.ready) {
        _musicPosition = position;
      }
    });
    _collectionEnded = false;
    _musicWrap = wrap;
    _musicEntries = List.unmodifiable(entries);
    _musicIndex = index;
    _musicLoader = load;
    _musicChanged = changed;
    _expandMusic = expand;
    _publishMusic();
    _broadcastState();
  }

  Future<void> retryMusicTrack() {
    final delta = _failedMusicDelta ?? 0;
    return _moveTrack(delta,
        resumePosition: delta == 0 ? _retryMusicPosition : null);
  }

  void _publishMusic({bool loading = false, Object? error}) {
    if (_musicIndex < 0) return;
    final state = MusicSequenceState(
        request: _musicEntries[_musicIndex],
        index: _musicIndex,
        length: _musicEntries.length,
        sourceGeneration: _sourceGeneration,
        loading: loading,
        error: error);
    _musicState = state;
    _musicChanges.add(state);
  }

  Future<void> _completeTrack(AudioProgressSession progress) async {
    await progress.record(force: true, completed: true);
    if (!identical(progress, _progress) || !_canControlSource) return;
    if (_musicIndex >= 0) await _moveTrack(1, completed: true);
  }

  Future<void> _moveTrack(int delta,
      {Duration? resumePosition, bool completed = false}) async {
    if (_movingTrack || !_canControlSource || _musicIndex < 0) return;
    var target = _musicIndex + delta;
    // Manual boundary controls are no-ops: keep the source, playback and its
    // progress session. Natural completion still flushes and stops below.
    if (!_musicWrap &&
        !completed &&
        (target < 0 || target >= _musicEntries.length)) {
      return;
    }
    _collectionEnded = false;
    _failedMusicDelta = delta;
    if (delta != 0) _retryMusicPosition = null;
    _movingTrack = true;
    final generation = ++_sourceGeneration;
    final owns = _ownsSource;
    bool current() => generation == _sourceGeneration && owns?.call() != false;
    _controlsEnabled = false;
    try {
      await endProgress();
      if (!current()) return;
      await _player.pause();
      _publishMusic(loading: true);
      if (!_musicWrap && (target < 0 || target >= _musicEntries.length)) {
        _controlsEnabled = true;
        _collectionEnded = completed;
        _failedMusicDelta = null;
        _publishMusic();
        _broadcastState();
        return;
      }
      if (target < 0 || target >= _musicEntries.length) {
        final old = _musicEntries[_musicIndex].identity;
        final expanded = await _expandMusic!();
        if (!current()) return;
        final base = expanded.indexWhere((request) => request.identity == old);
        if (expanded.isNotEmpty && base >= 0) {
          _musicEntries = List.unmodifiable(expanded);
          target = (base + delta) % expanded.length;
        } else {
          target = (target % _musicEntries.length);
        }
      }
      _musicIndex = target;
      _failedMusicDelta = 0;
      _publishMusic(loading: true);
      final request = _musicEntries[target];
      final source = await _musicLoader!(request, current);
      if (!current()) return;
      if (source == null) throw StateError('Playback preparation stopped');
      await loadSourceForSession(source, generation);
      if (!current()) return;
      double position = resumePosition?.inMilliseconds.toDouble() ?? -1;
      position = position >= 0 ? position / 1000 : (request.startPosition ?? 0);
      if (resumePosition == null &&
          request.startPosition == null &&
          request.readPosition != null) {
        try {
          position = await request.readPosition!() ?? 0;
        } catch (_) {}
      }
      if (!current()) return;
      await seekForSession(
          Duration(milliseconds: (position * 1000).round()), generation);
      if (!current()) return;
      setMediaItem(id: request.identity, title: request.title);
      startProgress(
          savePosition: request.savePosition!,
          markFinished: request.markFinished!);
      _musicPosition = Duration(milliseconds: (position * 1000).round());
      _retryMusicPosition = null;
      _failedMusicDelta = null;
      _musicChanged?.call(request);
      activateSourceSession(generation);
      _publishMusic();
      unawaited(play());
    } catch (error) {
      if (current()) {
        _controlsEnabled = true;
        _onPlaybackError(error);
      }
    } finally {
      _movingTrack = false;
    }
  }

  AudioProgressSession? _progress;
  int _stopGeneration = 0;
  int _sourceGeneration = 0;
  bool _controlsEnabled = false;
  bool Function()? _ownsSource;
  Future<void>? _sourceOperationTail;
  final _playbackErrors = StreamController<Object>.broadcast();

  /// Failures reported by the native player: undecodable sources, lost
  /// connections, and errors from starting playback. The player screen
  /// listens once its source is loaded to replace the controls with a
  /// readable message; load failures are thrown to the loader as well.
  Stream<Object> get playbackErrors => _playbackErrors.stream;

  /// Changes immediately when playback is stopped (including logout).
  /// Pending screen setup must not restart playback from an older generation.
  int get stopGeneration => _stopGeneration;

  // ---------------------------------------------------------------------------
  // Public accessors (used by AudioPlayerScreen to avoid a second Player)
  // ---------------------------------------------------------------------------

  /// The underlying [AudioPlayer] so the screen can subscribe to position /
  /// duration streams and still use bearer-token authenticated sources.
  ///
  AudioPlayer get player => _player;

  /// Invalidates pending source work from the previous audio session.
  int beginSourceSession({bool Function()? ownsSource}) {
    unawaited(_musicPositions?.cancel());
    _musicPositions = null;
    _failedMusicDelta = null;
    _retryMusicPosition = null;
    _musicEntries = const [];
    _musicIndex = -1;
    _collectionEnded = false;
    _musicState = null;
    _musicLoader = null;
    _expandMusic = null;
    _musicChanged = null;
    _controlsEnabled = false;
    _ownsSource = ownsSource;
    return ++_sourceGeneration;
  }

  /// Enables media controls only after loading and restoring this source.
  bool activateSourceSession(int generation) {
    if (generation != _sourceGeneration || _ownsSource?.call() == false) {
      return false;
    }
    _controlsEnabled = true;
    return true;
  }

  bool isSourceSessionCurrent(int generation) =>
      generation == _sourceGeneration && _canControlSource;

  bool get _canControlSource =>
      _controlsEnabled && _ownsSource?.call() != false;

  /// Loads only while [generation] still owns the shared player.
  Future<void> loadSourceForSession(
    AudioSource source,
    int generation,
  ) =>
      _serializeSourceOperation(() async {
        if (generation != _sourceGeneration) return;
        await _player.setAudioSource(source);
      });

  /// A delayed seek from an old screen cannot seek a replacement source.
  Future<bool> seekForSession(Duration position, int generation) =>
      _serializeSourceOperation(() async {
        if (generation != _sourceGeneration) return false;
        await _player.seek(position);
        return generation == _sourceGeneration;
      });

  /// Starts tracking the loaded item. Call after the source and resume seek
  /// complete. The callbacks capture account-scoped API/queue dependencies
  /// without making the media handler depend on HTTP or local persistence.
  void startProgress({
    required Future<void> Function(double) savePosition,
    required Future<void> Function() markFinished,
  }) {
    if (_progress != null) {
      throw StateError('End the current audio progress session first');
    }
    _progress = AudioProgressSession(
      position: () => _player.position,
      duration: () => _player.duration,
      playing: () => _player.playing,
      savePosition: savePosition,
      markFinished: markFinished,
    );
  }

  /// Saves the current session without pausing background playback.
  Future<void> saveProgress() async {
    await _progress?.record(force: true);
  }

  /// Flushes and detaches the current item before its source is replaced.
  Future<void> endProgress() async {
    final session = _progress;
    _progress = null;
    await session?.close();
  }

  // ---------------------------------------------------------------------------
  // BaseAudioHandler — playback controls
  // ---------------------------------------------------------------------------

  /// Starts playback. A failure is reported on [playbackErrors] instead of
  /// being thrown: callers (the screen, the notification, a headset button)
  /// do not await the outcome, so a thrown error would go unhandled.
  @override
  Future<void> play() async {
    if (!_canControlSource) return;
    // The visible Retry owns failed album transitions. Native Play must not
    // resume the previous source while the failed target is shown.
    if (!_musicWrap && _musicState?.error != null) return;
    try {
      // A completed album ends its old terminal progress session. Replaying
      // that source starts a fresh session; a deliberate seek back is kept.
      if (_collectionEnded && _musicIndex >= 0 && _progress == null) {
        _collectionEnded = false;
        final generation = _sourceGeneration;
        final total = _player.duration;
        if (total != null && _player.position >= total) {
          await seekForSession(Duration.zero, generation);
        }
        if (generation != _sourceGeneration || !_canControlSource) return;
        final request = _musicEntries[_musicIndex];
        startProgress(
            savePosition: request.savePosition!,
            markFinished: request.markFinished!);
      }
      await _player.play();
    } catch (error) {
      _onPlaybackError(error);
    }
  }

  @override
  Future<void> pause() async {
    await _player.pause();
    await _progress?.record(force: true);
  }

  @override
  Future<void> stop() async {
    unawaited(_musicPositions?.cancel());
    _musicPositions = null;
    _failedMusicDelta = null;
    _retryMusicPosition = null;
    _musicEntries = const [];
    _musicIndex = -1;
    _collectionEnded = false;
    _musicState = null;
    _musicLoader = null;
    _expandMusic = null;
    _musicChanged = null;
    _controlsEnabled = false;
    _stopGeneration++;
    _sourceGeneration++;
    await endProgress();
    // Stop immediately even when setAudioSource is still pending. The bumped
    // generation prevents that stale screen from seeking or starting playback
    // after the source operation finishes.
    await _player.stop();
    await super.stop();
  }

  /// Seeks to [position] within the current item.
  ///
  /// [SeekHandler] mixin provides the default [fastForward] / [rewind]
  /// implementations in terms of this method.
  @override
  Future<void> seek(Duration position) async {
    if (_canControlSource) await seekForSession(position, _sourceGeneration);
  }

  Future<T> _serializeSourceOperation<T>(Future<T> Function() operation) {
    final previous = _sourceOperationTail;
    final release = Completer<void>();
    final tail = release.future;
    _sourceOperationTail = tail;
    return _runSourceOperation(previous, release, tail, operation);
  }

  Future<T> _runSourceOperation<T>(
    Future<void>? previous,
    Completer<void> release,
    Future<void> tail,
    Future<T> Function() operation,
  ) async {
    if (previous != null) await previous;
    try {
      return await operation();
    } finally {
      release.complete();
      if (identical(_sourceOperationTail, tail)) _sourceOperationTail = null;
    }
  }

  /// Skip forward 15 seconds (media-button "next" maps to a short skip for
  /// podcast and audiobook use-cases rather than a full track change).
  @override
  Future<void> skipToNext() async {
    if (_musicIndex >= 0) return _moveTrack(1);
    if (!_canControlSource) return;
    final current = _player.position;
    final total = _player.duration ?? Duration.zero;
    final next =
        _clamp(current + const Duration(seconds: 15), Duration.zero, total);
    await seek(next);
  }

  /// Skip back 15 seconds.
  @override
  Future<void> skipToPrevious() async {
    if (_musicIndex >= 0) return _moveTrack(-1);
    if (!_canControlSource) return;
    final current = _player.position;
    final total = _player.duration ?? Duration.zero;
    final prev =
        _clamp(current - const Duration(seconds: 15), Duration.zero, total);
    await seek(prev);
  }

  @override
  Future<void> fastForward() => _seekBy(const Duration(seconds: 15));

  @override
  Future<void> rewind() => _seekBy(const Duration(seconds: -15));

  Future<void> _seekBy(Duration delta) => seek(_clamp(_player.position + delta,
      Duration.zero, _player.duration ?? Duration.zero));

  /// Changes playback speed and refreshes the notification state.
  @override
  Future<void> setSpeed(double speed) async {
    if (!_canControlSource) return;
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

  /// Marks the media session as failed, so the notification stops showing a
  /// playing item, and tells the screen (if one is listening).
  void _onPlaybackError(Object error) {
    _collectionEnded = false;
    if (_musicIndex >= 0) {
      // Runtime failures belong to the handler's current track even after
      // Back. A later Retry must not rebuild the original route's source.
      if (!_movingTrack) {
        _retryMusicPosition = _player.processingState == ProcessingState.ready
            ? _player.position
            : _musicPosition;
      }
      _publishMusic(error: error);
    }
    playbackState.add(
      playbackState.value.copyWith(
        processingState: AudioProcessingState.error,
        playing: false,
      ),
    );
    _playbackErrors.add(error);
  }

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
          if (_musicIndex >= 0)
            MediaControl.skipToPrevious
          else
            MediaControl.rewind,
          if (_player.playing) MediaControl.pause else MediaControl.play,
          MediaControl.stop,
          if (_musicIndex < 0)
            MediaControl.fastForward
          else
            MediaControl.skipToNext,
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
