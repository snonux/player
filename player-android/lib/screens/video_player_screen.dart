import 'dart:async';

import 'package:chewie/chewie.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:video_player/video_player.dart';

import '../api/player_api_client.dart';
import '../providers/api_client_provider.dart';
import '../providers/progress_queue_provider.dart';
import '../services/progress_queue.dart';

// How often progress updates are emitted to the server while playing.
const _kProgressInterval = Duration(seconds: 5);

// Playback fraction at which the item is considered finished (95 %).
const _kFinishedThreshold = 0.95;

// ---------------------------------------------------------------------------
// VideoPlayerScreen
// ---------------------------------------------------------------------------

/// Full-screen video player that streams from `/api/v1/media/{id}/stream`.
///
/// Design decisions:
///   - [ConsumerStatefulWidget] gives access to Riverpod providers while
///     holding the mutable controller state in [State].
///   - Bearer token is attached via `httpHeaders` on [VideoPlayerController]
///     so the native platform layer (ExoPlayer / AVPlayer) can authenticate
///     directly without routing bytes through Dart.
///   - Progress updates (every [_kProgressInterval]) and the finished mark
///     are fire-and-forget: errors are swallowed silently so a transient
///     network blip never interrupts playback.
///   - Both controllers are disposed in [dispose] to prevent resource leaks.
///   - All async continuations guard on [mounted] before calling [setState].
class VideoPlayerScreen extends ConsumerStatefulWidget {
  const VideoPlayerScreen({
    super.key,
    required this.mediaId,
    this.mediaUrl,
    this.startPosition,
  });

  /// The media item identifier extracted from the '/video/:mediaId' route path.
  final String mediaId;

  /// The resolved HLS/direct stream URL, optionally provided as route extra.
  /// When null, [PlayerApiClient.streamUrl] is called to derive the URL so the
  /// base URL stays in a single place (Dependency Inversion Principle).
  final String? mediaUrl;

  /// Optional start position in seconds, forwarded from the continue-watching
  /// screen to resume at the saved position without an extra API round-trip.
  /// When null, [PlayerApiClient.getMediaProgress] is called instead.
  final double? startPosition;

  @override
  ConsumerState<VideoPlayerScreen> createState() => _VideoPlayerScreenState();
}

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

class _VideoPlayerScreenState extends ConsumerState<VideoPlayerScreen> {
  // Nullable until initialisation completes (or fails).
  VideoPlayerController? _videoController;
  ChewieController? _chewieController;

  // Non-null when initialisation failed; shown in the error view.
  String? _error;

  // True while the controllers are being set up; shows a full-screen spinner.
  bool _isLoading = true;

  // Prevents emitting a "finished" update more than once per playback session.
  bool _finishedEmitted = false;

  // Periodic timer that fires every [_kProgressInterval] while playing.
  Timer? _progressTimer;

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
    _progressTimer?.cancel();
    _chewieController?.dispose();
    _videoController?.dispose();
    super.dispose();
  }

  // ---------------------------------------------------------------------------
  // Player initialisation
  // ---------------------------------------------------------------------------

  /// Initialises [VideoPlayerController] and [ChewieController].
  ///
  /// Steps:
  ///   1. Resolve the stream URL (from route extra or [PlayerApiClient]).
  ///   2. Read the bearer token for the `Authorization` header.
  ///   3. Create and initialise [VideoPlayerController.networkUrl].
  ///   4. Fetch the saved position via [getMediaProgress] and seek to it.
  ///   5. Wrap in [ChewieController] and start the progress ticker.
  Future<void> _initPlayer() async {
    if (!mounted) return;

    final client = ref.read(apiClientProvider);
    final storage = ref.read(tokenStorageProvider);
    final cookieJar = ref.read(cookieJarProvider);
    final mediaIdInt = int.tryParse(widget.mediaId) ?? 0;

    // Step 1: resolve the stream URL — prefer the route-extra URL so the
    // calling screen can forward a pre-computed URL; fall back to streamUrl.
    final url = widget.mediaUrl ?? client.streamUrl(mediaIdInt);

    // Step 2: read the auth artefacts so the native player can authenticate
    // without routing bytes through Dart.  Both Bearer (API-token auth) and
    // Cookie (session auth) headers are attached because ExoPlayer has its
    // own HTTP stack and does not share Dio's cookie jar.
    final token = await storage.readToken();
    final cookies = await cookieJar.loadForRequest(Uri.parse(url));
    if (!mounted) return;

    final cookieHeader =
        cookies.map((c) => '${c.name}=${c.value}').join('; ');
    final headers = <String, String>{
      if (token != null && token.isNotEmpty) 'Authorization': 'Bearer $token',
      if (cookieHeader.isNotEmpty) 'Cookie': cookieHeader,
    };

    // Step 3: create and initialise the VideoPlayerController.
    VideoPlayerController videoController;
    try {
      videoController = VideoPlayerController.networkUrl(
        Uri.parse(url),
        httpHeaders: headers,
      );
      await videoController.initialize();
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = _initErrorMessage(e);
        _isLoading = false;
      });
      return;
    }

    if (!mounted) {
      videoController.dispose();
      return;
    }

    // Step 4: resume from the saved position.
    // Prefer [widget.startPosition] (forwarded by the continue-watching screen)
    // to avoid a redundant API round-trip.  Fall back to [getMediaProgress] so
    // videos opened from other screens still resume correctly.
    try {
      final savedSeconds =
          widget.startPosition ?? await client.getMediaProgress(mediaIdInt);
      if (savedSeconds != null && savedSeconds > 0) {
        await videoController.seekTo(
          Duration(milliseconds: (savedSeconds * 1000).round()),
        );
      }
    } catch (_) {
      // Progress fetch failure is non-fatal; start from the beginning.
    }

    if (!mounted) {
      videoController.dispose();
      return;
    }

    // Step 5: wrap in ChewieController with sensible defaults for a
    // distraction-free full-screen experience.
    final chewieController = ChewieController(
      videoPlayerController: videoController,
      autoPlay: true,
      looping: false,
      allowFullScreen: true,
      allowMuting: true,
      showOptions: false,
    );

    setState(() {
      _videoController = videoController;
      _chewieController = chewieController;
      _isLoading = false;
    });

    // Start the periodic progress ticker now that playback is ready.
    // The queue is read once here so the timer callback does not access [ref]
    // after the widget may have been disposed (mirrors the client capture).
    final queue = ref.read(progressQueueProvider);
    _startProgressTicker(mediaIdInt, client, queue);
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
  /// The [client] and [queue] references are captured once here so we avoid
  /// accessing [ref] inside the timer callback after the widget may have been
  /// disposed.
  void _startProgressTicker(
    int mediaId,
    PlayerApiClient client,
    ProgressQueueBase queue,
  ) {
    _progressTimer = Timer.periodic(_kProgressInterval, (_) async {
      final vc = _videoController;
      if (vc == null) return;

      // Skip updates while paused — no progress to record and avoids
      // unnecessary DB writes when the user has paused playback.
      if (!vc.value.isPlaying) return;

      final position = vc.value.position;
      final duration = vc.value.duration;

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

  /// Converts a controller initialisation exception to a readable UI string.
  ///
  /// Kept in the state class because it is tightly coupled to this screen's
  /// error UI — no general-purpose helper needed (YAGNI).
  String _initErrorMessage(Object e) {
    final detail = e.toString();
    if (detail.isNotEmpty && detail != 'null') {
      return 'Playback failed: $detail';
    }
    return 'Could not start video playback. Please try again.';
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
        title: Text('Video – ${widget.mediaId}'),
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
      key: Key('video_player_loading'),
      child: CircularProgressIndicator(),
    );
  }

  /// Error view shown when initialisation fails.
  ///
  /// Provides a human-readable message and a retry button so the user can
  /// attempt re-initialisation without navigating away.
  Widget _buildErrorView(String message) {
    return Center(
      key: const Key('video_player_error'),
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
              key: const Key('video_player_error_message'),
            ),
            const SizedBox(height: 24),
            ElevatedButton(
              key: const Key('video_player_retry'),
              onPressed: _onRetry,
              child: const Text('Retry'),
            ),
          ],
        ),
      ),
    );
  }

  /// The Chewie player widget that fills the available space.
  Widget _buildPlayerView() {
    return Center(
      key: const Key('video_player_chewie'),
      child: AspectRatio(
        aspectRatio: _videoController!.value.aspectRatio,
        child: Chewie(controller: _chewieController!),
      ),
    );
  }

  // ---------------------------------------------------------------------------
  // Actions
  // ---------------------------------------------------------------------------

  /// Tears down current controllers and re-runs [_initPlayer].
  ///
  /// Extracted to keep [_buildErrorView] below 30 lines (style guideline).
  void _onRetry() {
    _progressTimer?.cancel();
    _chewieController?.dispose();
    _videoController?.dispose();
    setState(() {
      _chewieController = null;
      _videoController = null;
      _error = null;
      _isLoading = true;
      _finishedEmitted = false;
    });
    _initPlayer();
  }
}
