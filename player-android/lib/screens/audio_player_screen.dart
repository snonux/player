import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:just_audio/just_audio.dart';

import '../api/dio_client.dart';
import '../api/player_api_client.dart';
import '../providers/api_client_provider.dart';
import '../providers/audio_handler_provider.dart';
import '../providers/progress_queue_provider.dart';
import '../services/audio_handler.dart';
import '../services/progress_queue.dart';

// How often progress updates are emitted to the server while playing.
// Mirrors VideoPlayerScreen._kProgressInterval exactly.
const _kProgressInterval = Duration(seconds: 5);

// Playback fraction at which the item is considered finished (95 %).
// Mirrors VideoPlayerScreen._kFinishedThreshold exactly.
const _kFinishedThreshold = 0.95;

// Available playback speed options for the speed selector.
const _kSpeedOptions = [0.5, 1.0, 1.25, 1.5, 2.0];

// Skip-forward / skip-back amount.
const _kSkipDuration = Duration(seconds: 15);

// ---------------------------------------------------------------------------
// AudioPlayerScreen
// ---------------------------------------------------------------------------

/// Full-screen audio player that streams from `/api/v1/media/{id}/stream`.
///
/// Design decisions mirror VideoPlayerScreen exactly so both player types
/// share the same progress-sync contract:
///   - [ConsumerStatefulWidget] gives access to Riverpod providers while
///     holding the mutable controller state in [State].
///   - Bearer token is attached via `headers` on [AudioSource.uri] so the
///     just_audio native layer can authenticate without routing bytes through
///     Dart.
///   - The [PlayerAudioHandler] (obtained via [audioHandlerProvider]) wraps
///     the underlying [AudioPlayer] and bridges it to the Android media
///     session, enabling lock-screen controls and background playback.
///   - Progress updates (every [_kProgressInterval]) and the finished mark are
///     fire-and-forget: errors are swallowed so a transient network blip never
///     interrupts playback.
///   - The progress-sync timer intentionally stays in the screen (not in the
///     handler) so it can call [updateProgress] via [apiClientProvider] without
///     the handler needing a reference to the API layer — preserving the
///     Single Responsibility of each class.
///   - All async continuations guard on [mounted] before calling [setState].
class AudioPlayerScreen extends ConsumerStatefulWidget {
  const AudioPlayerScreen({
    super.key,
    required this.mediaId,
    this.mediaUrl,
    this.startPosition,
  });

  /// The media item identifier extracted from the '/audio/:mediaId' route path.
  final String mediaId;

  /// The resolved stream URL, optionally provided as route extra.
  /// When null, [PlayerApiClient.streamUrl] is called to derive the URL so the
  /// base URL stays in a single place (Dependency Inversion Principle).
  final String? mediaUrl;

  /// Optional start position in seconds, forwarded from the continue-watching
  /// screen to resume at the saved position without an extra API round-trip.
  /// When null, [PlayerApiClient.getMediaProgress] is called instead.
  final double? startPosition;

  @override
  ConsumerState<AudioPlayerScreen> createState() => _AudioPlayerScreenState();
}

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

class _AudioPlayerScreenState extends ConsumerState<AudioPlayerScreen> {
  // Non-null when initialisation failed; shown in the error view.
  String? _error;

  // True while the player is being set up; shows a full-screen spinner.
  bool _isLoading = true;

  // Prevents emitting a "finished" update more than once per playback session.
  bool _finishedEmitted = false;

  // Periodic timer that fires every [_kProgressInterval] while playing.
  Timer? _progressTimer;

  // Current playback speed; updated by the speed selector.
  double _playbackSpeed = 1.0;

  // ---------------------------------------------------------------------------
  // Lifecycle
  // ---------------------------------------------------------------------------

  @override
  void initState() {
    super.initState();
    // Defer initialisation so all Riverpod provider overrides are applied
    // before we read from [ref] (important for widget tests).
    WidgetsBinding.instance.addPostFrameCallback((_) => _initPlayer());
  }

  @override
  void dispose() {
    // Cancel the timer before the player is detached so the callback cannot
    // fire with a stale player reference (mirrors VideoPlayerScreen order).
    _progressTimer?.cancel();
    super.dispose();
  }

  // ---------------------------------------------------------------------------
  // Player initialisation
  // ---------------------------------------------------------------------------

  /// Top-level orchestrator for player setup.
  ///
  /// Delegates each step to a focused helper so this method stays under 30
  /// lines and each concern (auth, source loading, seek) is independently
  /// testable and readable (Separation of Concerns).
  Future<void> _initPlayer() async {
    if (!mounted) return;

    final handler = ref.read(audioHandlerProvider);
    final player = handler.player;
    final client = ref.read(apiClientProvider);
    final storage = ref.read(tokenStorageProvider);
    final mediaIdInt = int.tryParse(widget.mediaId) ?? 0;
    final url = widget.mediaUrl ?? client.streamUrl(mediaIdInt);

    // Step 1–2: build auth headers.
    final headers = await _buildAuthHeaders(storage);
    if (!mounted) return;

    // Step 3: load the authenticated source; show error UI on failure.
    final loaded = await _loadSource(player, url, headers);
    if (!loaded || !mounted) return;

    // Step 4: seek to the saved position (non-fatal if unavailable).
    await _resumeFromSavedPosition(player, client, mediaIdInt);
    if (!mounted) return;

    // Step 5: publish media-session metadata to notification/lock-screen.
    handler.setMediaItem(
      id: widget.mediaId,
      title: 'Audio – ${widget.mediaId}',
    );

    setState(() => _isLoading = false);

    // Step 6: begin playback and start the periodic progress ticker.
    // The queue is read once here so the timer callback does not access [ref]
    // after the widget may have been disposed (mirrors the client capture).
    unawaited(handler.play());
    final queue = ref.read(progressQueueProvider);
    _startProgressTicker(mediaIdInt, client, player, queue);
  }

  /// Reads the bearer token and returns the `Authorization` header map.
  ///
  /// Returns an empty map when no token is stored so the source can still be
  /// loaded (e.g., public streams or during tests).
  Future<Map<String, String>> _buildAuthHeaders(TokenStorage storage) async {
    final token = await storage.readToken();
    return <String, String>{
      if (token != null && token.isNotEmpty) 'Authorization': 'Bearer $token',
    };
  }

  /// Loads [url] into [player] with [headers]; returns `true` on success.
  ///
  /// On failure, sets the error UI state and returns `false` so [_initPlayer]
  /// can short-circuit without nesting the remaining steps inside a try/catch.
  Future<bool> _loadSource(
    AudioPlayer player,
    String url,
    Map<String, String> headers,
  ) async {
    try {
      await player.setAudioSource(
        AudioSource.uri(Uri.parse(url), headers: headers),
      );
      return true;
    } catch (e) {
      if (!mounted) return false;
      setState(() {
        _error = _initErrorMessage(e);
        _isLoading = false;
      });
      return false;
    }
  }

  /// Seeks [player] to the saved position for this media item.
  ///
  /// Prefers [widget.startPosition] to avoid a redundant API round-trip; falls
  /// back to [client.getMediaProgress].  Failure is non-fatal — the player
  /// simply starts from the beginning.
  Future<void> _resumeFromSavedPosition(
    AudioPlayer player,
    PlayerApiClient client,
    int mediaId,
  ) async {
    try {
      final savedSeconds =
          widget.startPosition ?? await client.getMediaProgress(mediaId);
      if (savedSeconds != null && savedSeconds > 0) {
        await player.seek(
          Duration(milliseconds: (savedSeconds * 1000).round()),
        );
      }
    } catch (_) {
      // Progress fetch failure is non-fatal; start from the beginning.
    }
  }

  // ---------------------------------------------------------------------------
  // Progress reporting
  // ---------------------------------------------------------------------------

  /// Starts a periodic timer that emits progress updates every
  /// [_kProgressInterval] and marks the item finished at [_kFinishedThreshold].
  ///
  /// Progress updates are routed through [queue] rather than calling
  /// [client.updateProgress] directly so that offline buffering and
  /// online batch-flush are handled transparently (Open-Closed: screens
  /// need not change if the queue strategy changes).
  ///
  /// The [client], [player], and [queue] references are captured once here so
  /// we avoid accessing [ref] inside the timer callback after the widget may
  /// have been disposed.
  ///
  /// The timer intentionally lives in the screen — not in the handler — so
  /// that [PlayerApiClient] (an HTTP concern) is not imported into
  /// [PlayerAudioHandler] (an audio-session concern), preserving SRP.
  void _startProgressTicker(
    int mediaId,
    PlayerApiClient client,
    AudioPlayer player,
    ProgressQueueBase queue,
  ) {
    _progressTimer = Timer.periodic(_kProgressInterval, (_) async {
      // Skip updates while paused — no progress to record and avoids
      // unnecessary DB writes when the user has paused playback.
      if (player.playing == false) return;

      final position = player.position;
      final duration = player.duration;

      // Enqueue position update — fire-and-forget so a transient error
      // never interrupts playback.  The queue handles online/offline.
      try {
        await queue.enqueue(
          mediaId,
          position.inMilliseconds / 1000.0,
        );
      } catch (_) {}

      // Mark finished once when playback fraction reaches the threshold.
      // Guard with [_finishedEmitted] to avoid duplicate server calls.
      if (!_finishedEmitted &&
          duration != null &&
          duration.inMilliseconds > 0 &&
          position.inMilliseconds / duration.inMilliseconds >=
              _kFinishedThreshold) {
        _finishedEmitted = true;
        try {
          // The finished status update is still sent directly to the API
          // because it is a distinct endpoint and should not be queued with
          // position updates (different semantics: idempotent status vs.
          // position accumulation).
          await client.updateProgressStatus(
            mediaId: mediaId,
            status: 'finished',
          );
        } catch (_) {}
      }
    });
  }

  // ---------------------------------------------------------------------------
  // Error mapping
  // ---------------------------------------------------------------------------

  /// Converts a player initialisation exception to a readable UI string.
  ///
  /// Kept in the state class because it is tightly coupled to this screen's
  /// error UI — no general-purpose helper needed (YAGNI).
  String _initErrorMessage(Object e) {
    final detail = e.toString();
    if (detail.isNotEmpty && detail != 'null') {
      return 'Playback failed: $detail';
    }
    return 'Could not start audio playback. Please try again.';
  }

  // ---------------------------------------------------------------------------
  // Actions
  // ---------------------------------------------------------------------------

  /// Tears down the current player source and re-runs [_initPlayer].
  ///
  /// Extracted to keep [_buildErrorView] below 30 lines (style guideline).
  void _onRetry() {
    _progressTimer?.cancel();
    setState(() {
      _error = null;
      _isLoading = true;
      _finishedEmitted = false;
      _playbackSpeed = 1.0;
    });
    _initPlayer();
  }

  /// Skips playback by [delta]; clamps to [Duration.zero] and total duration.
  Future<void> _skip(Duration delta) async {
    final handler = ref.read(audioHandlerProvider);
    final player = handler.player;
    final current = player.position;
    final total = player.duration ?? Duration.zero;
    // Duration does not implement Comparable, so clamp manually.
    final raw = current + delta;
    final next = raw < Duration.zero
        ? Duration.zero
        : (total > Duration.zero && raw > total ? total : raw);
    await handler.seek(next);
  }

  /// Applies [speed] to the handler and updates the UI state.
  Future<void> _setSpeed(double speed) async {
    final handler = ref.read(audioHandlerProvider);
    await handler.setSpeed(speed);
    if (!mounted) return;
    setState(() => _playbackSpeed = speed);
  }

  // ---------------------------------------------------------------------------
  // Build
  // ---------------------------------------------------------------------------

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: Colors.black,
      appBar: AppBar(
        backgroundColor: Colors.black,
        foregroundColor: Colors.white,
        title: Text('Audio – ${widget.mediaId}'),
      ),
      body: _buildBody(),
    );
  }

  /// Selects the appropriate body widget based on current state.
  Widget _buildBody() {
    if (_isLoading) return _buildLoadingView();
    if (_error != null) return _buildErrorView(_error!);
    return _buildPlayerView();
  }

  /// Full-screen loading spinner shown while the player initialises.
  Widget _buildLoadingView() {
    return const Center(
      key: Key('audio_player_loading'),
      child: CircularProgressIndicator(),
    );
  }

  /// Error view shown when initialisation fails.
  ///
  /// Provides a human-readable message and a retry button so the user can
  /// attempt re-initialisation without navigating away.
  Widget _buildErrorView(String message) {
    return Center(
      key: const Key('audio_player_error'),
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(Icons.error_outline, color: Colors.white70, size: 64),
            const SizedBox(height: 16),
            Text(
              message,
              style: const TextStyle(color: Colors.white70),
              textAlign: TextAlign.center,
              key: const Key('audio_player_error_message'),
            ),
            const SizedBox(height: 24),
            ElevatedButton(
              key: const Key('audio_player_retry'),
              onPressed: _onRetry,
              child: const Text('Retry'),
            ),
          ],
        ),
      ),
    );
  }

  /// The main playback UI: cover art placeholder, seek bar, and controls.
  Widget _buildPlayerView() {
    final handler = ref.read(audioHandlerProvider);
    return Padding(
      key: const Key('audio_player_view'),
      padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 16),
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          _buildCoverArt(),
          const SizedBox(height: 32),
          _buildSeekBar(handler),
          const SizedBox(height: 16),
          _buildControls(handler),
          const SizedBox(height: 16),
          _buildSpeedSelector(),
        ],
      ),
    );
  }

  /// Cover art thumbnail — falls back to a headphones icon when no artwork is
  /// available.  Real cover-art loading can be wired later via CachedNetworkImage
  /// pointing at [PlayerApiClient.thumbnailUrl] (Open-Closed: no change here).
  Widget _buildCoverArt() {
    return Container(
      key: const Key('audio_player_cover_art'),
      width: 200,
      height: 200,
      decoration: BoxDecoration(
        color: Colors.grey[850],
        borderRadius: BorderRadius.circular(12),
      ),
      child: const Icon(
        Icons.headphones,
        size: 80,
        color: Colors.white54,
      ),
    );
  }

  /// Seek bar backed by [AudioPlayer.positionStream].
  ///
  /// Uses [StreamBuilder] so the slider reflects real-time position without
  /// calling [setState] on every tick — preventing unnecessary full rebuilds.
  ///
  /// All seeks are routed through [handler.seek] (not directly through the
  /// underlying [AudioPlayer]) so that the Android media-session notification
  /// position is updated when the user drags the slider (Law of Demeter:
  /// the screen should not bypass the handler for mutations).
  Widget _buildSeekBar(PlayerAudioHandler handler) {
    final player = handler.player;
    return StreamBuilder<Duration>(
      stream: player.positionStream,
      builder: (context, snapshot) {
        final position = snapshot.data ?? Duration.zero;
        final duration = player.duration ?? Duration.zero;
        final total = duration.inMilliseconds.toDouble();
        final current = position.inMilliseconds
            .toDouble()
            .clamp(0.0, total > 0 ? total : 1.0);

        return Column(
          children: [
            Slider(
              key: const Key('audio_player_seek_bar'),
              value: current,
              min: 0,
              max: total > 0 ? total : 1.0,
              // Route through the handler so the media-session notification
              // stays in sync with the slider position during a drag.
              onChanged: total > 0
                  ? (v) => handler.seek(Duration(milliseconds: v.round()))
                  : null,
              activeColor: Colors.white,
              inactiveColor: Colors.white24,
            ),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16),
              child: Row(
                mainAxisAlignment: MainAxisAlignment.spaceBetween,
                children: [
                  Text(
                    _formatDuration(position),
                    key: const Key('audio_player_position'),
                    style: const TextStyle(color: Colors.white70, fontSize: 12),
                  ),
                  Text(
                    _formatDuration(duration),
                    key: const Key('audio_player_duration'),
                    style: const TextStyle(color: Colors.white70, fontSize: 12),
                  ),
                ],
              ),
            ),
          ],
        );
      },
    );
  }

  /// Playback controls: skip-back, play/pause, skip-forward.
  ///
  /// All tap handlers delegate to [handler] instead of calling the underlying
  /// [AudioPlayer] directly, so the media-session notification stays in sync
  /// with every button press (Law of Demeter: one collaborator for mutations).
  Widget _buildControls(PlayerAudioHandler handler) {
    return StreamBuilder<bool>(
      stream: handler.player.playingStream,
      builder: (context, snapshot) {
        final isPlaying = snapshot.data ?? false;
        return Row(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            // Skip back 15 s
            IconButton(
              key: const Key('audio_player_skip_back'),
              icon: const Icon(Icons.fast_rewind, color: Colors.white, size: 36),
              onPressed: () => _skip(-_kSkipDuration),
              tooltip: 'Skip back 15 seconds',
            ),
            const SizedBox(width: 16),
            // Play / Pause — delegate to handler so the notification updates.
            IconButton(
              key: const Key('audio_player_play_pause'),
              icon: Icon(
                isPlaying ? Icons.pause_circle_filled : Icons.play_circle_filled,
                color: Colors.white,
                size: 64,
              ),
              onPressed: isPlaying ? handler.pause : handler.play,
              tooltip: isPlaying ? 'Pause' : 'Play',
            ),
            const SizedBox(width: 16),
            // Skip forward 15 s
            IconButton(
              key: const Key('audio_player_skip_forward'),
              icon: const Icon(
                Icons.fast_forward,
                color: Colors.white,
                size: 36,
              ),
              onPressed: () => _skip(_kSkipDuration),
              tooltip: 'Skip forward 15 seconds',
            ),
          ],
        );
      },
    );
  }

  /// Speed selector rendered as a row of text buttons.
  ///
  /// The active speed is highlighted; inactive speeds are white70 so the
  /// selection is clear at a glance.
  Widget _buildSpeedSelector() {
    return Row(
      key: const Key('audio_player_speed_selector'),
      mainAxisAlignment: MainAxisAlignment.center,
      children: _kSpeedOptions.map((speed) {
        final isSelected = _playbackSpeed == speed;
        return TextButton(
          key: Key('audio_player_speed_${speed.toString().replaceAll('.', '_')}'),
          onPressed: () => _setSpeed(speed),
          child: Text(
            '${speed}x',
            style: TextStyle(
              color: isSelected ? Colors.white : Colors.white54,
              fontWeight:
                  isSelected ? FontWeight.bold : FontWeight.normal,
            ),
          ),
        );
      }).toList(),
    );
  }

  // ---------------------------------------------------------------------------
  // Formatting helpers
  // ---------------------------------------------------------------------------

  /// Formats a [Duration] as `mm:ss` or `h:mm:ss` for durations >= 1 hour.
  String _formatDuration(Duration d) {
    final h = d.inHours;
    final m = d.inMinutes.remainder(60).toString().padLeft(2, '0');
    final s = d.inSeconds.remainder(60).toString().padLeft(2, '0');
    return h > 0 ? '$h:$m:$s' : '$m:$s';
  }
}
