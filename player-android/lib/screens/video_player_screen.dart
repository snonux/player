import 'dart:async';

import 'package:chewie/chewie.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:video_player/video_player.dart';
import 'package:video_player_platform_interface/video_player_platform_interface.dart'
    as platform;

import '../api/player_api_client.dart';
import '../api/dio_client.dart';
import '../providers/api_client_provider.dart';
import '../providers/playback_session_provider.dart';
import '../providers/progress_queue_provider.dart';
import '../services/playback_request.dart';
import '../services/shared_video_events.dart';
import '../services/playback_session_coordinator.dart';

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
///   - Progress updates and the finished command are stored by the queue,
///     which retries network failures without interrupting playback.
///   - Both controllers are disposed in [dispose] to prevent resource leaks.
///   - All async continuations guard on [mounted] before calling [setState].
class VideoPlayerScreen extends ConsumerStatefulWidget {
  const VideoPlayerScreen({
    super.key,
    required this.mediaId,
    this.mediaUrl,
    this.mediaTitle,
    this.startPosition,
    this.isPublicShare = false,
    this.request,
    this.serverUserId = 0,
  });

  /// The media item identifier extracted from the '/video/:mediaId' route path.
  final String mediaId;

  /// The resolved HLS/direct stream URL, optionally provided as route extra.
  /// When null, [PlayerApiClient.streamUrl] is called to derive the URL so the
  /// base URL stays in a single place (Dependency Inversion Principle).
  final String? mediaUrl;

  /// Readable file name or episode title for the app bar. Falls back to a generic label when the
  /// caller has no loaded metadata.
  final String? mediaTitle;

  /// Optional start position in seconds, forwarded from the continue-watching
  /// screen to resume at the saved position without an extra API round-trip.
  /// When null, [PlayerApiClient.getMediaProgress] is called instead.
  final double? startPosition;
  final bool isPublicShare;
  final PlaybackRequest? request;
  final int serverUserId;

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
  bool _finishedPending = false;
  int _initGeneration = 0;
  PlaybackRequest? _activeRequest;
  PlaybackSessionLease? _sessionLease;
  StreamSubscription<platform.VideoEvent>? _completionSubscription;
  bool _nativeCompleted = false;
  int? _nativePlayerId;
  Future<void> _progressWrite = Future<void>.value();

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
  void didUpdateWidget(covariant VideoPlayerScreen oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.request?.identity != widget.request?.identity ||
        oldWidget.request?.sourceUri != widget.request?.sourceUri ||
        oldWidget.request?.title != widget.request?.title ||
        oldWidget.request?.startPosition != widget.request?.startPosition ||
        oldWidget.mediaId != widget.mediaId ||
        oldWidget.serverUserId != widget.serverUserId ||
        oldWidget.startPosition != widget.startPosition ||
        oldWidget.mediaUrl != widget.mediaUrl ||
        oldWidget.isPublicShare != widget.isPublicShare) {
      _initGeneration++;
      setState(() {
        _isLoading = true;
        _error = null;
      });
      WidgetsBinding.instance.addPostFrameCallback((_) => _initPlayer());
    }
  }

  @override
  void dispose() {
    _disposing = true;
    _progressTimer?.cancel();
    final lease = _sessionLease;
    if (lease != null && lease.isCurrent) {
      unawaited(lease.stop());
    } else {
      unawaited(_stopOwnedSession());
    }
    super.dispose();
  }

  // ---------------------------------------------------------------------------
  // Player initialisation
  // ---------------------------------------------------------------------------

  /// Initializes the typed source with a content-URI or network controller,
  /// applies its resume position, and starts controls and progress reporting.
  /// Only server requests read account credentials. Ownership is checked after
  /// each async step before the controller can start playback.
  Future<void> _initPlayer() async {
    if (!mounted) return;
    final initGeneration = _initGeneration;
    final request = _effectiveRequest();
    final coordinator = ref.read(playbackSessionCoordinatorProvider);
    final PlaybackSessionLease? lease;
    try {
      lease = await coordinator.claim(
        kind: request.kind,
        identity: request.identity,
        stop: _stopOwnedSession,
        sourceUri: request.sourceUri.toString(),
      );
    } catch (error) {
      if (mounted && _initGeneration == initGeneration) {
        setState(() {
          _error = _initErrorMessage(error);
          _isLoading = false;
        });
      }
      return;
    }
    if (lease == null) return;
    if (!mounted || initGeneration != _initGeneration) {
      await lease.release();
      return;
    }
    _sessionLease = lease;
    _activeRequest = request;
    setState(() {
      _isLoading = true;
      _error = null;
    });
    bool current() =>
        mounted &&
        initGeneration == _initGeneration &&
        lease?.isCurrent == true;

    Map<String, String> headers;
    try {
      headers = request is ServerPlaybackRequest
          ? await accountRequestHeaders(
              uri: request.sourceUri,
              baseUrl: Uri.parse(request.serverOrigin),
              storage: ref.read(tokenStorageProvider),
              cookieJar: ref.read(cookieJarProvider),
              mutations: ref.read(credentialMutationQueueProvider),
            )
          : <String, String>{};
    } catch (error) {
      if (current()) {
        await lease.release();
        if (!mounted || _initGeneration != initGeneration) return;
        setState(() {
          _error = _initErrorMessage(error);
          _isLoading = false;
        });
      }
      return;
    }
    if (!current()) return;

    VideoPlayerController? videoController;
    try {
      videoController = request is LocalPlaybackRequest
          ? VideoPlayerController.contentUri(request.sourceUri)
          : VideoPlayerController.networkUrl(
              request.sourceUri,
              httpHeaders: headers,
            );
      // Track the controller while initialization is pending so a library
      // mutation or ownership transfer can dispose its URI before releasing it.
      _videoController = videoController;
      SharedVideoEvents.install();
      await videoController.initialize();
      if (!current()) {
        await _disposeController(videoController);
        return;
      }

      // ignore: invalid_use_of_visible_for_testing_member
      final nativePlayerId = videoController.playerId;
      _nativePlayerId = nativePlayerId;

      double? resumePosition = request.startPosition;
      if (resumePosition == null && request.readPosition != null) {
        try {
          resumePosition = await request.readPosition!();
        } catch (_) {
          // Resume lookup is optional; start at zero on provider errors.
        }
      }
      if (!current()) {
        await _disposeController(videoController);
        return;
      }
      if (resumePosition != null && resumePosition > 0) {
        try {
          final position =
              Duration(milliseconds: (resumePosition * 1000).round());
          if (videoController.value.duration <= Duration.zero) {
            await platform.VideoPlayerPlatform.instance
                .seekTo(nativePlayerId, position);
            videoController.value = videoController.value
                .copyWith(position: position, isCompleted: false);
          } else {
            await videoController.seekTo(position);
          }
        } catch (_) {
          // A stale saved position must not prevent playback.
        }
      }
      if (!current()) {
        await _disposeController(videoController);
        return;
      }

      // Observe the shared native event stream
      // rather than isCompleted, which also becomes true on zero-duration polls.
      final events =
          platform.VideoPlayerPlatform.instance.videoEventsFor(nativePlayerId);
      _nativeCompleted = false;
      if (events.isBroadcast) {
        _completionSubscription = events.listen((event) {
          if (event.eventType == platform.VideoEventType.completed &&
              current()) {
            _nativeCompleted = true;
            unawaited(_recordProgress(request, lease: lease, force: true));
          }
        }, onError: (Object _) {
          // The controller's existing subscription handles native errors.
        });
      }

      final chewieController = ChewieController(
        videoPlayerController: videoController,
        autoPlay: false,
        looping: false,
        allowFullScreen: true,
        allowMuting: true,
        showOptions: false,
        customControls: videoController.value.duration <= Duration.zero
            ? _UnknownDurationControls(
                controller: videoController,
                onPlay: () =>
                    _playController(videoController!, lease!, nativePlayerId),
              )
            : null,
      );
      setState(() {
        _videoController = videoController;
        _chewieController = chewieController;
        _isLoading = false;
        _error = null;
      });
      if (request.savePosition != null && request.markFinished != null) {
        _startProgressTicker(request, lease);
      }
      await _playController(videoController, lease, nativePlayerId);
    } catch (error) {
      // The plugin exposes no production creation-status API. Its player ID
      // distinguishes failed creation from errors after a native player exists.
      // ignore: invalid_use_of_visible_for_testing_member
      final creationFailed = videoController?.playerId == -1;
      if (creationFailed && videoController != null) {
        // Failed creation leaves the plugin's creation completer unresolved.
        // No native resource exists, so dispose must not block source cleanup.
        if (identical(_videoController, videoController)) {
          _videoController = null;
        }
        unawaited(_disposeController(videoController).catchError((_) {}));
      }
      try {
        if (lease.isCurrent) {
          await lease.stop();
        } else if (videoController != null && !creationFailed) {
          await _disposeController(videoController);
        }
      } catch (_) {
        // Surface the original initialization failure after cleanup is attempted.
      }
      if (mounted && _initGeneration == initGeneration) {
        setState(() {
          _error = _initErrorMessage(error);
          _isLoading = false;
        });
      }
    }
  }

  Future<void> _playController(
    VideoPlayerController controller,
    PlaybackSessionLease lease,
    int nativePlayerId,
  ) async {
    if (!lease.isCurrent) return;
    if (controller.value.duration <= Duration.zero) {
      final backend = platform.VideoPlayerPlatform.instance;
      final position = backend is SharedVideoEvents
          ? backend.latestPosition(nativePlayerId) ?? controller.value.position
          : controller.value.position;
      // play() treats position == duration as end-of-file. Zero duration is
      // unknown, so preserve the native elapsed position before resuming.
      controller.value =
          controller.value.copyWith(position: position, isCompleted: false);
    }
    await controller.play();
  }

  PlaybackRequest _effectiveRequest() {
    final request = widget.request;
    if (request != null) return request;
    final title = widget.mediaTitle ??
        (widget.isPublicShare ? 'Shared Video' : 'Video – ${widget.mediaId}');
    if (widget.isPublicShare) {
      final url = widget.mediaUrl;
      if (url == null) throw StateError('Public video share has no stream URL');
      final segments = Uri.parse(url).pathSegments;
      return PublicSharePlaybackRequest(
        shareToken: segments.length > 1 && segments.first == 's'
            ? segments[1]
            : 'unknown',
        sourceUri: Uri.parse(url),
        title: title,
      );
    }

    final client = ref.read(apiClientProvider);
    final mediaId = int.tryParse(widget.mediaId) ?? 0;
    final url = widget.mediaUrl ?? client.streamUrl(mediaId);
    final queue = ref.read(progressQueueProvider);
    return ServerPlaybackRequest(
      mediaId: mediaId,
      serverOrigin: ref.read(playerBaseUrlProvider).origin,
      userId: widget.serverUserId,
      sourceUri: Uri.parse(url),
      title: title,
      startPosition: widget.startPosition,
      readPosition: widget.startPosition == null
          ? () => client.getMediaProgress(mediaId)
          : null,
      savePosition: (seconds) => queue.enqueue(mediaId, seconds),
      markFinished: () => queue.enqueueFinished(mediaId),
    );
  }

  // ---------------------------------------------------------------------------
  // Progress reporting
  // ---------------------------------------------------------------------------

  /// Starts a periodic timer that emits progress updates every
  /// [_kProgressInterval] and marks the item finished at [_kFinishedThreshold].
  ///
  /// The request captures server queue or local repository callbacks, keeping
  /// progress bound to the source without reading providers after disposal.
  void _startProgressTicker(
    PlaybackRequest request,
    PlaybackSessionLease lease,
  ) {
    _finishedEmitted = false;
    _finishedPending = false;
    _progressTimer?.cancel();
    _videoController?.addListener(_onVideoValueChanged);
    _progressTimer = Timer.periodic(_kProgressInterval, (_) {
      unawaited(_recordProgress(request, lease: lease));
    });
  }

  void _onVideoValueChanged() {
    final controller = _videoController;
    final request = _activeRequest;
    final lease = _sessionLease;
    if (controller == null || request == null || lease == null) return;
    if (!controller.value.isPlaying && lease.isCurrent) {
      unawaited(_recordProgress(request, lease: lease, force: true));
    }
  }

  Future<void> _recordProgress(
    PlaybackRequest request, {
    PlaybackSessionLease? lease,
    bool force = false,
    bool allowStaleLease = false,
  }) {
    final controller = _videoController;
    final savePosition = request.savePosition;
    final markFinished = request.markFinished;
    if (controller == null || savePosition == null || markFinished == null) {
      return _progressWrite;
    }
    if (!controller.value.isInitialized) return _progressWrite;
    if (_finishedEmitted || (_finishedPending && !force)) return _progressWrite;
    if (!force && !controller.value.isPlaying) return _progressWrite;
    if (!allowStaleLease && (lease == null || !lease.isCurrent)) {
      return _progressWrite;
    }

    final duration = controller.value.duration;
    final backend = platform.VideoPlayerPlatform.instance;
    // The video plugin clamps positions to its duration, including zero.
    // Preserve the real native sample for unknown-duration progress.
    final position = duration <= Duration.zero && backend is SharedVideoEvents
        ? backend.latestPosition(_nativePlayerId) ?? controller.value.position
        : controller.value.position;
    final reachedFinish = _nativeCompleted ||
        (duration.inMilliseconds > 0 &&
            position.inMilliseconds / duration.inMilliseconds >=
                _kFinishedThreshold);
    if (reachedFinish) _finishedPending = true;
    _progressWrite = _progressWrite.then((_) async {
      if (_finishedEmitted) return;
      try {
        await savePosition(position.inMilliseconds / 1000.0);
      } catch (_) {}
      if (!reachedFinish) return;
      try {
        await markFinished();
        if (lease?.isCurrent == true || allowStaleLease) {
          _finishedEmitted = true;
        }
      } catch (_) {
        // The next tick or final save retries failed local/database writes.
      } finally {
        _finishedPending = false;
      }
    });
    return _progressWrite;
  }

  Future<void>? _ownedStop;
  bool _disposing = false;
  final _controllerDisposals =
      Expando<Future<void>>('video controller disposal');

  Future<void> _disposeController(VideoPlayerController controller) =>
      _controllerDisposals[controller] ??= controller.dispose();

  Future<void> _stopOwnedSession() {
    final pending = _ownedStop;
    if (pending != null) return pending;
    final stopping = _performOwnedStop();
    _ownedStop = stopping;
    return stopping.whenComplete(() {
      if (identical(_ownedStop, stopping)) _ownedStop = null;
    });
  }

  Future<void> _performOwnedStop() async {
    if (mounted && !_disposing) {
      setState(() {
        _isLoading = false;
        _error = 'Playback stopped. Tap Retry to play this item again.';
      });
    }
    await _completionSubscription?.cancel();
    _completionSubscription = null;
    final hadProgress = _progressTimer != null;
    _progressTimer?.cancel();
    _progressTimer = null;
    final request = _activeRequest;
    final controller = _videoController;
    final lease = _sessionLease;
    if (request != null && controller != null) {
      try {
        await controller.pause();
      } catch (_) {}
      if (hadProgress) {
        await _recordProgress(
          request,
          lease: lease,
          force: true,
          allowStaleLease: true,
        );
      }
    }
    controller?.removeListener(_onVideoValueChanged);
    _chewieController?.dispose();
    _chewieController = null;
    _videoController = null;
    _sessionLease = null;
    if (controller != null) await _disposeController(controller);
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
        title: Text(_activeRequest?.title ??
            widget.mediaTitle ??
            (widget.isPublicShare
                ? 'Shared Video'
                : 'Video – ${widget.mediaId}')),
      ),
      body: _buildBody(),
    );
  }

  /// Selects the appropriate body widget based on current state.
  Widget _buildBody() {
    if (_isLoading) return _buildLoadingView();
    if (_error != null) return _buildErrorView(_error!);
    if (_videoController == null || _chewieController == null) {
      return _buildLoadingView();
    }
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
    unawaited(_retry());
  }

  Future<void> _retry() async {
    _initGeneration++;
    setState(() {
      _error = null;
      _isLoading = true;
      _finishedEmitted = false;
      _finishedPending = false;
    });
    await _stopOwnedSession();
    await _sessionLease?.release();
    _sessionLease = null;
    if (!mounted) return;
    _initPlayer();
  }
}

/// Unknown-duration media cannot provide a seek fraction. Keep playback,
/// muting and fullscreen available without dividing by a zero duration.
class _UnknownDurationControls extends StatelessWidget {
  const _UnknownDurationControls(
      {required this.controller, required this.onPlay});

  final VideoPlayerController controller;
  final Future<void> Function() onPlay;

  @override
  Widget build(BuildContext context) => AnimatedBuilder(
        animation: controller,
        builder: (context, _) => Align(
          alignment: Alignment.bottomCenter,
          child: ColoredBox(
            color: Colors.black54,
            child: Row(
              key: const Key('video_unknown_duration_controls'),
              mainAxisAlignment: MainAxisAlignment.spaceEvenly,
              children: [
                IconButton(
                  tooltip: controller.value.isPlaying ? 'Pause' : 'Play',
                  color: Colors.white,
                  icon: Icon(controller.value.isPlaying
                      ? Icons.pause
                      : Icons.play_arrow),
                  onPressed: () => controller.value.isPlaying
                      ? controller.pause()
                      : onPlay(),
                ),
                IconButton(
                  tooltip: controller.value.volume == 0 ? 'Unmute' : 'Mute',
                  color: Colors.white,
                  icon: Icon(controller.value.volume == 0
                      ? Icons.volume_off
                      : Icons.volume_up),
                  onPressed: () => controller
                      .setVolume(controller.value.volume == 0 ? 1 : 0),
                ),
                IconButton(
                  tooltip: 'Toggle fullscreen',
                  color: Colors.white,
                  icon: const Icon(Icons.fullscreen),
                  onPressed: () =>
                      ChewieController.of(context).toggleFullScreen(),
                ),
              ],
            ),
          ),
        ),
      );
}
