import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:just_audio/just_audio.dart';

import '../api/player_api_client.dart';
import '../providers/api_client_provider.dart';

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
///   - Progress updates (every [_kProgressInterval]) and the finished mark are
///     fire-and-forget: errors are swallowed so a transient network blip never
///     interrupts playback.
///   - [AudioPlayer] is disposed in [dispose] to prevent resource leaks.
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
  // Nullable until initialisation completes (or fails).
  AudioPlayer? _audioPlayer;

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
    // Cancel the timer before disposing the player so the callback cannot fire
    // against a disposed player (mirrors VideoPlayerScreen dispose order).
    _progressTimer?.cancel();
    _audioPlayer?.dispose();
    super.dispose();
  }

  // ---------------------------------------------------------------------------
  // Player initialisation
  // ---------------------------------------------------------------------------

  /// Initialises [AudioPlayer] with bearer-token auth and resumes position.
  ///
  /// Steps:
  ///   1. Resolve the stream URL (from route extra or [PlayerApiClient]).
  ///   2. Read the bearer token for the `Authorization` header.
  ///   3. Create [AudioPlayer] and set the authenticated [AudioSource.uri].
  ///   4. Fetch the saved position via [getMediaProgress] and seek to it.
  ///   5. Start playback and the progress ticker.
  Future<void> _initPlayer() async {
    if (!mounted) return;

    final client = ref.read(apiClientProvider);
    final storage = ref.read(tokenStorageProvider);
    final mediaIdInt = int.tryParse(widget.mediaId) ?? 0;

    // Step 1: resolve the stream URL — prefer the route-extra URL so the
    // calling screen can forward a pre-computed URL; fall back to streamUrl.
    final url = widget.mediaUrl ?? client.streamUrl(mediaIdInt);

    // Step 2: read the bearer token so the native player can authenticate
    // without routing bytes through Dart (performance and correctness).
    final token = await storage.readToken();
    if (!mounted) return;

    final headers = <String, String>{
      if (token != null && token.isNotEmpty) 'Authorization': 'Bearer $token',
    };

    // Step 3: create the AudioPlayer and load the authenticated source.
    final audioPlayer = AudioPlayer();
    try {
      await audioPlayer.setAudioSource(
        AudioSource.uri(Uri.parse(url), headers: headers),
      );
    } catch (e) {
      audioPlayer.dispose();
      if (!mounted) return;
      setState(() {
        _error = _initErrorMessage(e);
        _isLoading = false;
      });
      return;
    }

    if (!mounted) {
      audioPlayer.dispose();
      return;
    }

    // Step 4: resume from the saved position.
    // Prefer [widget.startPosition] (forwarded by the continue-watching screen)
    // to avoid a redundant API round-trip.  Fall back to [getMediaProgress] so
    // audio items opened from other screens still resume correctly.
    try {
      final savedSeconds =
          widget.startPosition ?? await client.getMediaProgress(mediaIdInt);
      if (savedSeconds != null && savedSeconds > 0) {
        await audioPlayer.seek(
          Duration(milliseconds: (savedSeconds * 1000).round()),
        );
      }
    } catch (_) {
      // Progress fetch failure is non-fatal; start from the beginning.
    }

    if (!mounted) {
      audioPlayer.dispose();
      return;
    }

    setState(() {
      _audioPlayer = audioPlayer;
      _isLoading = false;
    });

    // Step 5: begin playback and start the periodic progress ticker.
    unawaited(audioPlayer.play());
    _startProgressTicker(mediaIdInt, client, audioPlayer);
  }

  // ---------------------------------------------------------------------------
  // Progress reporting
  // ---------------------------------------------------------------------------

  /// Starts a periodic timer that emits progress updates every
  /// [_kProgressInterval] and marks the item finished at [_kFinishedThreshold].
  ///
  /// The [client] and [player] references are captured once here so we avoid
  /// accessing [ref] or [_audioPlayer] inside the timer callback after the
  /// widget may have been disposed.
  void _startProgressTicker(
    int mediaId,
    PlayerApiClient client,
    AudioPlayer player,
  ) {
    _progressTimer = Timer.periodic(_kProgressInterval, (_) async {
      // Skip network calls while paused — no progress to record and avoids
      // unnecessary server traffic when the user has paused playback.
      if (player.playing == false) return;

      final position = player.position;
      final duration = player.duration;

      // Emit raw position update — fire-and-forget so a transient network
      // error never interrupts playback.
      try {
        await client.updateProgress(
          mediaId: mediaId,
          positionSeconds: position.inMilliseconds / 1000.0,
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

  /// Tears down the current player and re-runs [_initPlayer].
  ///
  /// Extracted to keep [_buildErrorView] below 30 lines (style guideline).
  void _onRetry() {
    _progressTimer?.cancel();
    _audioPlayer?.dispose();
    setState(() {
      _audioPlayer = null;
      _error = null;
      _isLoading = true;
      _finishedEmitted = false;
      _playbackSpeed = 1.0;
    });
    _initPlayer();
  }

  /// Skips playback by [delta]; clamps to [Duration.zero] and total duration.
  Future<void> _skip(Duration delta) async {
    final player = _audioPlayer;
    if (player == null) return;
    final current = player.position;
    final total = player.duration ?? Duration.zero;
    // Duration does not implement Comparable, so clamp manually.
    final raw = current + delta;
    final next = raw < Duration.zero
        ? Duration.zero
        : (total > Duration.zero && raw > total ? total : raw);
    await player.seek(next);
  }

  /// Applies [speed] to the player and updates the UI state.
  Future<void> _setSpeed(double speed) async {
    final player = _audioPlayer;
    if (player == null) return;
    await player.setSpeed(speed);
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
    final player = _audioPlayer!;
    return Padding(
      key: const Key('audio_player_view'),
      padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 16),
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          _buildCoverArt(),
          const SizedBox(height: 32),
          _buildSeekBar(player),
          const SizedBox(height: 16),
          _buildControls(player),
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
  Widget _buildSeekBar(AudioPlayer player) {
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
              onChanged: total > 0
                  ? (v) => player.seek(Duration(milliseconds: v.round()))
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
  Widget _buildControls(AudioPlayer player) {
    return StreamBuilder<bool>(
      stream: player.playingStream,
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
            // Play / Pause
            IconButton(
              key: const Key('audio_player_play_pause'),
              icon: Icon(
                isPlaying ? Icons.pause_circle_filled : Icons.play_circle_filled,
                color: Colors.white,
                size: 64,
              ),
              onPressed: isPlaying ? player.pause : player.play,
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

