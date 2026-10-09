import 'dart:async';

import 'package:chewie/chewie.dart';
import 'package:flutter/foundation.dart';
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
import '../services/playback_source_resolver.dart';
import '../services/shared_video_events.dart';
import '../services/playback_session_coordinator.dart';
import '../utils/playback_errors.dart';
import '../widgets/playback_loading_view.dart';
import 'playback_preparation_mixin.dart';

// How often progress updates are emitted to the server while playing.
const _kProgressInterval = Duration(seconds: 5);

// Playback fraction at which the item is considered finished (95 %).
const _kFinishedThreshold = 0.95;

// ---------------------------------------------------------------------------
// VideoPlayerScreen
// ---------------------------------------------------------------------------

/// Full-screen video player that streams from `/api/v1/media/{id}/stream`,
/// or from `/compat` when the server marks the item as transcoded.
///
/// Design decisions:
///   - A compatibility stream is probed first ([PlaybackPreparationMixin]):
///     while the server is still transcoding it answers 503, which the native
///     player could only report as a broken source.
///   - Failures are shown as a readable sentence ([playbackErrorMessage]),
///     never as the raw exception text.
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
  /// When null, the screen starts from [PlayerApiClient.streamUrl] and looks
  /// the item up to learn whether the server wants its compatibility stream
  /// played instead, so the URL rules stay in the API client.
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

class _VideoPlayerScreenState extends ConsumerState<VideoPlayerScreen>
    with WidgetsBindingObserver, PlaybackPreparationMixin {
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
    WidgetsBinding.instance.addObserver(this);
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
        cancelPlaybackPreparation();
        _isLoading = true;
        _error = null;
      });
      WidgetsBinding.instance.addPostFrameCallback((_) => _initPlayer());
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    // Do not replace durable resume with the initial position during setup.
    if (_isLoading || state == AppLifecycleState.resumed) return;
    final request = _activeRequest;
    final lease = _sessionLease;
    if (request != null && lease?.isCurrent == true) {
      unawaited(_recordProgress(request, lease: lease, force: true));
    }
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
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
  /// Ownership is checked after each async step before the controller can
  /// start playback. A server compatibility stream is awaited before the
  /// native player is created.
  Future<void> _initPlayer() async {
    if (!mounted) return;
    final generation = _initGeneration;
    final request = await _resolveRequest();
    if (!mounted || generation != _initGeneration) return;
    final lease = await _claimSession(request, generation);
    if (lease == null) return;
    final attempt = _PlaybackAttempt(request, lease, generation);
    _sessionLease = lease;
    _activeRequest = request;
    setState(() {
      _isLoading = true;
      _error = null;
    });
    final headers = await _readHeaders(attempt);
    if (headers == null || !_isCurrent(attempt)) return;
    if (!await _streamIsPlayable(attempt)) return;
    await _startPlayback(attempt, headers);
  }

  /// True while [attempt] is the one this screen is showing and still owns
  /// the shared player.
  bool _isCurrent(_PlaybackAttempt attempt) =>
      mounted &&
      attempt.generation == _initGeneration &&
      attempt.lease.isCurrent;

  /// Takes over the player from whatever was playing. Returns null when the
  /// claim failed (error shown) or this attempt was superseded meanwhile.
  Future<PlaybackSessionLease?> _claimSession(
    PlaybackRequest request,
    int generation,
  ) async {
    final PlaybackSessionLease? lease;
    try {
      lease = await ref.read(playbackSessionCoordinatorProvider).claim(
            kind: request.kind,
            identity: request.identity,
            stop: _stopOwnedSession,
            sourceUri: request.sourceUri.toString(),
          );
    } catch (error) {
      _showError(error, request, generation);
      return null;
    }
    if (lease == null) return null;
    if (!mounted || generation != _initGeneration) {
      await lease.release();
      return null;
    }
    return lease;
  }

  /// Reads the request headers for the native player. Only server requests
  /// read account credentials. Returns null when that failed (error shown).
  Future<Map<String, String>?> _readHeaders(_PlaybackAttempt attempt) async {
    final request = attempt.request;
    if (request is! ServerPlaybackRequest) return <String, String>{};
    try {
      return await accountRequestHeaders(
        uri: request.sourceUri,
        baseUrl: Uri.parse(request.serverOrigin),
        storage: ref.read(tokenStorageProvider),
        cookieJar: ref.read(cookieJarProvider),
        mutations: ref.read(credentialMutationQueueProvider),
      );
    } catch (error) {
      if (_isCurrent(attempt)) {
        await attempt.lease.release();
        _showError(error, request, attempt.generation);
      }
      return null;
    }
  }

  /// Waits for a server compatibility stream to be ready. Returns false when
  /// playback must not start: this attempt was superseded, preparing failed
  /// (error shown), or another item took the player meanwhile (the "stopped"
  /// state with Retry is shown instead of a spinner that would never end).
  Future<bool> _streamIsPlayable(_PlaybackAttempt attempt) async {
    try {
      final ready = await waitUntilPlayable(
        attempt.request,
        current: () => _isCurrent(attempt),
      );
      if (ready && _isCurrent(attempt)) return true;
      _showMessage(kPlaybackStoppedMessage, attempt.generation);
      return false;
    } catch (error) {
      if (!_isCurrent(attempt)) return false;
      await attempt.lease.release();
      _showError(error, attempt.request, attempt.generation);
      return false;
    }
  }

  /// Creates and initializes the native player, restores the saved position,
  /// shows the controls and starts playing. Any failure tears the player
  /// down again and ends in the error view.
  Future<void> _startPlayback(
    _PlaybackAttempt attempt,
    Map<String, String> headers,
  ) async {
    final request = attempt.request;
    VideoPlayerController? controller;
    try {
      controller = request is LocalPlaybackRequest
          ? VideoPlayerController.contentUri(request.sourceUri)
          : VideoPlayerController.networkUrl(
              request.sourceUri,
              httpHeaders: headers,
            );
      // Track the controller while initialization is pending so a library
      // mutation or ownership transfer can dispose its URI before releasing it.
      _videoController = controller;
      SharedVideoEvents.install();
      await controller.initialize();
      if (!await _stillOwns(attempt, controller)) return;

      // ignore: invalid_use_of_visible_for_testing_member
      final nativePlayerId = controller.playerId;
      _nativePlayerId = nativePlayerId;
      final resumePosition = await _readResumePosition(request);
      if (!await _stillOwns(attempt, controller)) return;
      await _applyResumePosition(controller, nativePlayerId, resumePosition);
      if (!await _stillOwns(attempt, controller)) return;

      _watchCompletion(attempt, nativePlayerId);
      _showPlayer(attempt, controller, nativePlayerId);
      await _playController(controller, attempt.lease, nativePlayerId);
    } catch (error) {
      await _cleanUpFailedStart(attempt, controller);
      _showError(error, request, attempt.generation);
    }
  }

  /// Disposes [controller] and returns false when [attempt] was superseded
  /// while an async setup step was pending.
  Future<bool> _stillOwns(
    _PlaybackAttempt attempt,
    VideoPlayerController controller,
  ) async {
    if (_isCurrent(attempt)) return true;
    await _disposeController(controller);
    return false;
  }

  /// The position to resume at: the one the caller passed, else the saved
  /// one. The lookup is optional; a provider error starts at zero.
  Future<double?> _readResumePosition(PlaybackRequest request) async {
    final readPosition = request.readPosition;
    if (request.startPosition != null || readPosition == null) {
      return request.startPosition;
    }
    try {
      return await readPosition();
    } catch (_) {
      return null;
    }
  }

  Future<void> _applyResumePosition(
    VideoPlayerController controller,
    int nativePlayerId,
    double? resumePosition,
  ) async {
    if (resumePosition == null || resumePosition <= 0) return;
    try {
      final position = Duration(milliseconds: (resumePosition * 1000).round());
      if (controller.value.duration <= Duration.zero) {
        // The plugin clamps seeks to its (unknown, zero) duration, so seek
        // the native player directly and mirror the position.
        await platform.VideoPlayerPlatform.instance
            .seekTo(nativePlayerId, position);
        controller.value =
            controller.value.copyWith(position: position, isCompleted: false);
      } else {
        await controller.seekTo(position);
      }
    } catch (_) {
      // A stale saved position must not prevent playback.
    }
  }

  /// Observes the shared native event stream for completion rather than
  /// isCompleted, which also becomes true on zero-duration polls.
  void _watchCompletion(_PlaybackAttempt attempt, int nativePlayerId) {
    final events =
        platform.VideoPlayerPlatform.instance.videoEventsFor(nativePlayerId);
    _nativeCompleted = false;
    if (!events.isBroadcast) return;
    _completionSubscription = events.listen((event) {
      if (event.eventType == platform.VideoEventType.completed &&
          _isCurrent(attempt)) {
        _nativeCompleted = true;
        unawaited(_recordProgress(
          attempt.request,
          lease: attempt.lease,
          force: true,
        ));
      }
    }, onError: (Object _) {
      // The controller's existing subscription handles native errors.
    });
  }

  /// Swaps the spinner for the Chewie player and starts progress reporting
  /// and the watch for failures after this point.
  void _showPlayer(
    _PlaybackAttempt attempt,
    VideoPlayerController controller,
    int nativePlayerId,
  ) {
    final chewieController = ChewieController(
      videoPlayerController: controller,
      autoPlay: false,
      looping: false,
      allowFullScreen: true,
      allowMuting: true,
      showOptions: false,
      customControls: controller.value.duration <= Duration.zero
          ? _UnknownDurationControls(
              controller: controller,
              onPlay: () =>
                  _playController(controller, attempt.lease, nativePlayerId),
            )
          : null,
    );
    setState(() {
      _videoController = controller;
      _chewieController = chewieController;
      _isLoading = false;
      _error = null;
    });
    final request = attempt.request;
    if (request.savePosition != null && request.markFinished != null) {
      _startProgressTicker(request, attempt.lease);
    }
    _watchPlaybackErrors(attempt, controller);
  }

  /// Replaces the player with the error view (and its Retry button) when the
  /// native player fails after it started: a connection lost for good, a
  /// rendition that vanished, or a codec it cannot decode further in.
  void _watchPlaybackErrors(
    _PlaybackAttempt attempt,
    VideoPlayerController controller,
  ) {
    void listener() {
      if (!controller.value.hasError) return;
      controller.removeListener(listener);
      if (_isCurrent(attempt)) unawaited(_failPlayback(attempt));
    }

    controller.addListener(listener);
  }

  /// Saves progress and releases the failed player, then shows why.
  Future<void> _failPlayback(_PlaybackAttempt attempt) async {
    final request = attempt.request;
    try {
      await attempt.lease.stop();
    } catch (_) {
      // The failure message matters more than a failed cleanup.
    }
    _showMessage(
      sourceErrorMessage(request.title, request.kind),
      attempt.generation,
    );
  }

  /// Releases whatever a failed [_startPlayback] left behind, so the error
  /// can be shown afterwards.
  Future<void> _cleanUpFailedStart(
    _PlaybackAttempt attempt,
    VideoPlayerController? controller,
  ) async {
    // The plugin exposes no production creation-status API. Its player ID
    // distinguishes failed creation from errors after a native player exists.
    // ignore: invalid_use_of_visible_for_testing_member
    final creationFailed = controller?.playerId == -1;
    if (creationFailed && controller != null) {
      // Failed creation leaves the plugin's creation completer unresolved.
      // No native resource exists, so dispose must not block source cleanup.
      if (identical(_videoController, controller)) _videoController = null;
      unawaited(_disposeController(controller).catchError((_) {}));
    }
    try {
      if (attempt.lease.isCurrent) {
        await attempt.lease.stop();
      } else if (controller != null && !creationFailed) {
        await _disposeController(controller);
      }
    } catch (_) {
      // Surface the original initialization failure after cleanup is attempted.
    }
  }

  /// Builds the request, looking up the playback URL when the route supplied
  /// only a media ID (no typed request and no URL in the route extra).
  Future<PlaybackRequest> _resolveRequest() {
    final request = _effectiveRequest();
    if (widget.request != null || widget.mediaUrl != null) {
      return Future.value(request);
    }
    return resolveServerPlaybackSource(request, ref.read(apiClientProvider));
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
      return PublicSharePlaybackRequest(
        shareToken: shareTokenFromUrl(Uri.parse(url)),
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
        _error = kPlaybackStoppedMessage;
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

  /// Shows the error view for [error], unless the attempt of [generation]
  /// was superseded or the screen is gone.
  ///
  /// The user gets the item's title and a plain reason from
  /// [playbackErrorMessage]. The debug log gets only the source kind and the
  /// error's type: the request identity carries share tokens and account
  /// IDs, and exception texts can carry URLs, none of which belong in logcat.
  void _showError(Object error, PlaybackRequest request, int generation) {
    if (kDebugMode) {
      debugPrint(
        'Video playback (${request.kind.name}) failed: ${error.runtimeType}',
      );
    }
    _showMessage(
      playbackErrorMessage(error, title: request.title, source: request.kind),
      generation,
    );
  }

  /// Ends the loading state with [message] and the Retry button.
  void _showMessage(String message, int generation) {
    if (!mounted || _initGeneration != generation) return;
    setState(() {
      _error = message;
      _isLoading = false;
    });
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

  /// Full-screen loading spinner shown while the player initialises, with a
  /// "preparing" label while the server is still producing the stream.
  Widget _buildLoadingView() {
    return PlaybackLoadingView(
      key: const Key('video_player_loading'),
      preparing: isPreparingPlayback,
    );
  }

  /// Error view shown when initialisation or playback fails, and when
  /// another item took over the player.
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
      cancelPlaybackPreparation();
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

/// One run of [_VideoPlayerScreenState._initPlayer]: what is being played,
/// the ownership of the shared player it claimed, and the screen generation
/// that started it. A newer generation (Retry, another item) supersedes it.
class _PlaybackAttempt {
  const _PlaybackAttempt(this.request, this.lease, this.generation);

  final PlaybackRequest request;
  final PlaybackSessionLease lease;
  final int generation;
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
