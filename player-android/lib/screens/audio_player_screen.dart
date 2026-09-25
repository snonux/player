import 'dart:async';

import 'package:cookie_jar/cookie_jar.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:just_audio/just_audio.dart';

import '../api/dio_client.dart';
import '../api/player_api_client.dart';
import '../providers/api_client_provider.dart';
import '../providers/audio_handler_provider.dart';
import '../providers/progress_queue_provider.dart';
import '../services/audio_handler.dart';

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
///   - The handler owns progress reporting so route disposal does not stop
///     updates while audio continues in the background.
///   - All async continuations guard on [mounted] before calling [setState].
class AudioPlayerScreen extends ConsumerStatefulWidget {
  const AudioPlayerScreen({
    super.key,
    required this.mediaId,
    this.mediaUrl,
    this.startPosition,
    this.isPublicShare = false,
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
  final bool isPublicShare;

  @override
  ConsumerState<AudioPlayerScreen> createState() => _AudioPlayerScreenState();
}

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

class _AudioPlayerScreenState extends ConsumerState<AudioPlayerScreen> {
  // Invalidates older async setup attempts when Retry starts a new one.
  int _initGeneration = 0;
  // Non-null when initialisation failed; shown in the error view.
  String? _error;

  // True while the player is being set up; shows a full-screen spinner.
  bool _isLoading = true;

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

    final initGeneration = _initGeneration;
    final handler = ref.read(audioHandlerProvider);
    final stopGeneration = handler.stopGeneration;
    bool current() =>
        mounted &&
        _initGeneration == initGeneration &&
        handler.stopGeneration == stopGeneration;
    final player = handler.player;
    final client = ref.read(apiClientProvider);
    final storage = ref.read(tokenStorageProvider);
    final cookieJar = ref.read(cookieJarProvider);
    final mediaIdInt = int.tryParse(widget.mediaId) ?? 0;
    final url = widget.mediaUrl ?? client.streamUrl(mediaIdInt);

    // Step 1–2: build auth headers (Bearer + session cookie).
    final headers = widget.isPublicShare
        ? <String, String>{}
        : await _buildAuthHeaders(
            storage,
            cookieJar,
            ref.read(credentialMutationQueueProvider),
            ref.read(playerBaseUrlProvider),
            Uri.parse(url));
    if (!current()) return;

    // Step 3: flush the previous item before replacing its source, then load.
    await handler.endProgress();
    if (!current()) return;
    final loaded = await _loadSource(player, url, headers, current);
    if (!loaded || !current()) return;

    // Step 4: seek to the saved position (non-fatal if unavailable).
    if (!widget.isPublicShare) {
      await _resumeFromSavedPosition(player, client, mediaIdInt);
    }
    if (!current()) return;

    // Step 5: publish media-session metadata to notification/lock-screen.
    handler.setMediaItem(
      id: widget.mediaId,
      title:
          widget.isPublicShare ? 'Shared Audio' : 'Audio – ${widget.mediaId}',
    );

    setState(() => _isLoading = false);

    // The callbacks capture dependencies, never the screen/ref. The handler's
    // session outlives this route during background playback.
    if (!widget.isPublicShare) {
      final queue = ref.read(progressQueueProvider);
      handler.startProgress(
        savePosition: (seconds) => queue.enqueue(mediaIdInt, seconds),
        markFinished: () => client.updateProgressStatus(
          mediaId: mediaIdInt,
          status: 'finished',
        ),
      );
    }
    unawaited(handler.play());
  }

  /// Builds the headers map for an authenticated stream request.
  ///
  /// just_audio runs a localhost proxy that forwards these headers to
  /// ExoPlayer's underlying HTTP request, which is how we authenticate against
  /// the session-cookie-protected `/api/v1/media/{id}/stream` endpoint without
  /// sharing Dio's HTTP stack.  Both Bearer (for API-token auth) and Cookie
  /// (for session auth) are attached so either auth scheme works.
  Future<Map<String, String>> _buildAuthHeaders(
    TokenStorage storage,
    CookieJar jar,
    CredentialMutationQueue mutations,
    Uri baseUrl,
    Uri url,
  ) async {
    return accountRequestHeaders(
      uri: url,
      baseUrl: baseUrl,
      storage: storage,
      cookieJar: jar,
      mutations: mutations,
    );
  }

  /// Loads [url] into [player] with [headers]; returns `true` on success.
  ///
  /// On failure, sets the error UI state and returns `false` so [_initPlayer]
  /// can short-circuit without nesting the remaining steps inside a try/catch.
  Future<bool> _loadSource(
    AudioPlayer player,
    String url,
    Map<String, String> headers,
    bool Function() current,
  ) async {
    try {
      await player.setAudioSource(
        AudioSource.uri(Uri.parse(url), headers: headers),
      );
      return true;
    } catch (e) {
      if (!current()) return false;
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
    _initGeneration++;
    setState(() {
      _error = null;
      _isLoading = true;
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
        title: Text(widget.isPublicShare
            ? 'Shared Audio'
            : 'Audio – ${widget.mediaId}'),
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
    return LayoutBuilder(
      builder: (context, constraints) => SingleChildScrollView(
        key: const Key('audio_player_view'),
        child: ConstrainedBox(
          constraints: BoxConstraints(minHeight: constraints.maxHeight),
          child: Padding(
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
          ),
        ),
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
              padding: const EdgeInsets.symmetric(horizontal: 8),
              child: Row(
                mainAxisAlignment: MainAxisAlignment.spaceBetween,
                children: [
                  Flexible(
                    child: Text(
                      _formatDuration(position),
                      key: const Key('audio_player_position'),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style:
                          const TextStyle(color: Colors.white70, fontSize: 12),
                    ),
                  ),
                  Flexible(
                    child: Text(
                      _formatDuration(duration),
                      key: const Key('audio_player_duration'),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      textAlign: TextAlign.end,
                      style:
                          const TextStyle(color: Colors.white70, fontSize: 12),
                    ),
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
              icon:
                  const Icon(Icons.fast_rewind, color: Colors.white, size: 36),
              onPressed: () => _skip(-_kSkipDuration),
              tooltip: 'Skip back 15 seconds',
            ),
            const SizedBox(width: 16),
            // Play / Pause — delegate to handler so the notification updates.
            IconButton(
              key: const Key('audio_player_play_pause'),
              icon: Icon(
                isPlaying
                    ? Icons.pause_circle_filled
                    : Icons.play_circle_filled,
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

  /// Speed selector wraps into more rows at narrow widths or larger text.
  ///
  /// The active speed is highlighted; inactive speeds are white70 so the
  /// selection is clear at a glance.
  Widget _buildSpeedSelector() {
    return Wrap(
      key: const Key('audio_player_speed_selector'),
      alignment: WrapAlignment.center,
      spacing: 4,
      runSpacing: 4,
      children: _kSpeedOptions.map((speed) {
        final isSelected = _playbackSpeed == speed;
        return TextButton(
          key: Key(
              'audio_player_speed_${speed.toString().replaceAll('.', '_')}'),
          onPressed: () => _setSpeed(speed),
          style: TextButton.styleFrom(
            minimumSize: const Size(56, 48),
            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
          ),
          child: Text(
            '${speed}x',
            style: TextStyle(
              color: isSelected ? Colors.white : Colors.white54,
              fontWeight: isSelected ? FontWeight.bold : FontWeight.normal,
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
