import '../l10n/music_localizations.dart';
import '../services/progress_queue.dart';
import '../providers/playback_cache_provider.dart';
import '../widgets/stream_quality_sheet.dart';
import '../services/stream_quality.dart';
import '../widgets/equalizer_sheet.dart';
import '../widgets/loudness_sheet.dart';
import '../widgets/music_queue_sheet.dart';
import '../widgets/music_info_dialog.dart';
import '../api/dio_player_api_client.dart';
import '../providers/auth_state_provider.dart';
import 'dart:async';

import 'package:cookie_jar/cookie_jar.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:just_audio/just_audio.dart';

import '../api/dio_client.dart';
import '../models/models.dart';
import '../services/music_sequence.dart';
import '../services/music_queue.dart';
import '../api/player_api_client.dart';
import '../providers/api_client_provider.dart';
import '../providers/audio_handler_provider.dart';
import '../providers/cast_provider.dart';
import '../providers/progress_queue_provider.dart';
import '../providers/public_api_client_provider.dart';
import '../providers/playback_session_provider.dart';
import '../services/playback_session_coordinator.dart';
import '../services/audio_handler.dart';
import '../services/playback_request.dart';
import '../services/playback_source_resolver.dart';
import '../utils/playback_errors.dart';
import '../widgets/lyrics_panel.dart';
import '../widgets/audio_cover_art.dart';
import '../widgets/audio_visuals.dart';
import '../widgets/cast_controls.dart';
import '../widgets/sleep_timer_control.dart';
import '../widgets/playback_loading_view.dart';
import '../widgets/personal_media_actions.dart';
import 'cast_screen_mixin.dart';
import 'playback_preparation_mixin.dart';

// Available playback speed options for the speed selector.
const _kSpeedOptions = [0.5, 1.0, 1.25, 1.5, 2.0];

// Skip-forward / skip-back amount.
const _kSkipDuration = Duration(seconds: 15);

// ---------------------------------------------------------------------------
// AudioPlayerScreen
// ---------------------------------------------------------------------------

/// Full-screen audio player that streams from `/api/v1/media/{id}/stream`,
/// or from `/compat` when the server marks the item as transcoded.
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
///   - A compatibility stream is probed first ([PlaybackPreparationMixin]):
///     while the server is still transcoding it answers 503, which the native
///     player could only report as a broken source.
///   - Failures are shown as a readable sentence ([playbackErrorMessage]),
///     never as the raw exception text.
///   - All async continuations guard on [mounted] before calling [setState].
class AudioPlayerScreen extends ConsumerStatefulWidget {
  const AudioPlayerScreen({
    super.key,
    required this.mediaId,
    this.mediaUrl,
    this.mediaTitle,
    this.sourceName = '',
    this.startPosition,
    this.isPublicShare = false,
    this.request,
    this.serverUserId = 0,
    this.sequence = const [],
    this.sequenceIndex,
    this.albumCollection = false,
    this.traverseFolders = false,
    this.attachCurrent = false,
    this.attachmentLease,
    this.attachmentGeneration,
  });

  /// The media item identifier extracted from the '/audio/:mediaId' route path.
  final String mediaId;

  /// The resolved stream URL, optionally provided as route extra.
  /// When null, the screen starts from [PlayerApiClient.streamUrl] and looks
  /// the item up to learn whether the server wants its compatibility stream
  /// played instead, so the URL rules stay in the API client.
  final String? mediaUrl;

  /// Readable file name or episode title for the app bar and the system
  /// media notification. Falls back to a generic label when the
  /// caller has no loaded metadata.
  final String? mediaTitle;
  final String sourceName;

  /// Optional start position in seconds, forwarded from the continue-watching
  /// screen to resume at the saved position without an extra API round-trip.
  /// When null, [PlayerApiClient.getMediaProgress] is called instead.
  final double? startPosition;
  final bool isPublicShare;
  final PlaybackRequest? request;
  final int serverUserId;
  final List<Media> sequence;

  /// Selected occurrence in sequence; media IDs alone cannot identify repeats.
  final int? sequenceIndex;
  final bool albumCollection;
  final bool traverseFolders;
  final bool attachCurrent;
  final PlaybackSessionLease? attachmentLease;
  final int? attachmentGeneration;

  @override
  ConsumerState<AudioPlayerScreen> createState() => _AudioPlayerScreenState();
}

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

class _AudioPlayerScreenState extends ConsumerState<AudioPlayerScreen>
    with PlaybackPreparationMixin, CastScreenMixin {
  // Invalidates older async setup attempts when Retry starts a new one.
  int _initGeneration = 0;
  // Shown in the error view (with Retry) when non-null: why starting or
  // continuing playback failed, or that another item took over the player.
  String? _error;
  PlaybackRequest? _activeRequest;
  PlayerAudioHandler? _handler;
  int? _activeSourceGeneration;

  // True while the player is being set up; shows a full-screen spinner.
  bool _isLoading = true;
  bool _refreshQualityOnRetry = false;
  ServerPlaybackRequest? _qualityRetryRequest;

  // Current playback speed; updated by the speed selector.
  double _playbackSpeed = 1.0;
  PlaybackSessionLease? _sessionLease;

  // Native failures after the source was loaded (see [_watchPlayback]).
  StreamSubscription<Object>? _playbackErrorSubscription;
  StreamSubscription<Duration>? _positionSubscription;
  StreamSubscription<MusicSequenceState>? _musicSubscription;
  List<PlaybackRequest> _musicEntries = const [];
  int _musicStartIndex = 0;
  Future<List<PlaybackRequest>> Function()? _expandMusic;

  // Where this item last was while it played on this screen; Retry resumes
  // here. Null before playback progressed and once the item completed.
  Duration? _lastKnownPosition;

  // Set when casting ended: load the item where the TV left it, but do not
  // start playing on the phone by surprise.
  bool _startPaused = false;
  PlaybackRequest? _remoteReturnRequest;
  List<PlaybackRequest> _remoteReturnQueue = const [];
  int _remoteReturnIndex = 0;
  String? _remoteReturnOccurrence;

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
  void didUpdateWidget(covariant AudioPlayerScreen oldWidget) {
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
      // A changed widget may be a different item: its position is unknown.
      _lastKnownPosition = null;
      _qualityRetryRequest = null;
      _refreshQualityOnRetry = false;
      setState(() {
        cancelPlaybackPreparation();
        _error = null;
        _isLoading = true;
      });
      WidgetsBinding.instance.addPostFrameCallback((_) => _initPlayer());
    }
  }

  @override
  void dispose() {
    _initGeneration++;
    _stopWatchingPlayback();
    unawaited(_musicSubscription?.cancel());
    // Completed setup belongs to the background handler. An abandoned setup
    // must release its source, even when credentials or resume reads are pending.
    final lease = _sessionLease;
    if (_isLoading &&
        lease?.isCurrent == true &&
        _handler?.musicState == null) {
      unawaited(lease!.stop());
    }
    super.dispose();
  }

  // ---------------------------------------------------------------------------
  // Player initialisation
  // ---------------------------------------------------------------------------

  /// Loads the selected source, applies its resume position, and transfers
  /// progress ownership to the background handler. Ownership is checked after
  /// async setup so an older screen cannot restart a replacement source. A
  /// server compatibility stream is awaited before the source is loaded.
  Future<void> _initPlayer() async {
    if (!mounted) return;

    final remoteController = ref.read(castControllerProvider);
    final remote = remoteController.active;
    if (remote?.device.isRenderer == true &&
        remote!.request.identity == _effectiveRequest().identity) {
      _handler = ref.read(audioHandlerProvider);
      _activeRequest = remote.request;
      _remoteReturnRequest = remote.request;
      _remoteReturnQueue = remoteController.rendererQueue;
      _remoteReturnIndex = remoteController.rendererIndex;
      _remoteReturnOccurrence = remoteController.currentOccurrenceID;
      setState(() {
        _isLoading = false;
        _error = null;
      });
      return;
    }
    if (widget.attachCurrent && _remoteReturnRequest == null) {
      final handler = ref.read(audioHandlerProvider);
      final state = handler.musicState;
      final lease = handler.sequenceLease;
      if (state == null ||
          lease?.isCurrent != true ||
          state.request.identity != widget.request?.identity ||
          !identical(lease, widget.attachmentLease) ||
          state.sourceGeneration != widget.attachmentGeneration) {
        setState(() {
          _isLoading = false;
          _error = 'This track is no longer playing. Return to the queue.';
        });
        return;
      }
      _handler = handler;
      _sessionLease = lease;
      _activeRequest = state.request;
      _activeSourceGeneration = state.sourceGeneration;
      _watchMusicState(handler, lease!);
      if (!state.loading && state.error == null) {
        _watchPlayback(_PlaybackAttempt(
            request: state.request,
            lease: lease,
            generation: _initGeneration,
            handler: handler,
            sourceGeneration: state.sourceGeneration,
            stopGeneration: handler.stopGeneration));
      }
      setState(() {
        _isLoading = state.loading;
        _error = state.error == null
            ? null
            : playbackErrorMessage(state.error!,
                title: state.request.title, source: state.request.kind);
      });
      return;
    }
    final sleepIntent =
        ref.read(audioHandlerProvider).sleepTimer.beginPlaybackIntent();
    ref.read(audioHandlerProvider).clearMediaItem();
    final generation = _initGeneration;
    unawaited(_musicSubscription?.cancel());
    _musicSubscription = null;
    _stopWatchingPlayback();
    final PlaybackRequest request;
    try {
      request = await _resolveRequest(generation);
    } catch (error) {
      if (mounted && generation == _initGeneration) {
        _showError(error, _effectiveRequest(), generation);
      }
      return;
    }
    if (!mounted || generation != _initGeneration) return;
    final handler = ref.read(audioHandlerProvider);
    _handler = handler;
    _activeRequest = request;
    if (ref.read(castControllerProvider).isCasting(request.identity)) {
      // Reopened while it plays on a cast device: show the remote controls.
      setState(() => _isLoading = false);
      return;
    }
    final lease = await _claimSession(request, handler, generation);
    if (lease == null) return;
    _sessionLease = lease;
    final attempt = _PlaybackAttempt(
      request: request,
      lease: lease,
      generation: generation,
      handler: handler,
      sourceGeneration: handler.beginSourceSession(
          ownsSource: () => lease.isCurrent, sleepIntent: sleepIntent),
      stopGeneration: handler.stopGeneration,
    );
    _activeSourceGeneration = attempt.sourceGeneration;
    handler.setMediaRequest(request);

    final headers = await _readHeaders(attempt);
    if (headers == null || !_isCurrent(attempt)) return;
    if (attempt.request is! ServerPlaybackRequest &&
        !await _streamIsPlayable(attempt)) {
      return;
    }
    if (!await _loadAndResume(attempt, headers)) return;
    _startLoadedSource(attempt);
  }

  /// True while [attempt] is the one this screen is showing, still owns the
  /// shared player, and playback was not stopped (notification, logout).
  bool _isCurrent(_PlaybackAttempt attempt) =>
      mounted &&
      _initGeneration == attempt.generation &&
      attempt.handler.stopGeneration == attempt.stopGeneration &&
      attempt.lease.isCurrent;

  /// Takes over the player from whatever was playing. Returns null when the
  /// claim failed (error shown) or this attempt was superseded meanwhile.
  Future<PlaybackSessionLease?> _claimSession(
    PlaybackRequest request,
    PlayerAudioHandler handler,
    int generation,
  ) async {
    // The stop callback outlives this screen during background playback, so
    // it holds the wait handle, not the state. Ending the wait there lets
    // the screen leave "preparing" at once when another item takes over.
    final wait = playbackWait;
    final PlaybackSessionLease? lease;
    try {
      lease = await ref.read(playbackSessionCoordinatorProvider).claim(
            kind: request.kind,
            identity: request.identity,
            stop: () {
              wait.cancel();
              return handler.stop();
            },
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

  /// Reads the request headers for the native player. Local and public-share
  /// requests carry no account credentials. Returns null when reading them
  /// failed (error shown).
  Future<Map<String, String>?> _readHeaders(_PlaybackAttempt attempt) async {
    final request = attempt.request;
    if (request is! ServerPlaybackRequest) return <String, String>{};
    try {
      return await _buildAuthHeaders(
        ref.read(tokenStorageProvider),
        ref.read(cookieJarProvider),
        ref.read(credentialMutationQueueProvider),
        Uri.parse(request.serverOrigin),
        request.sourceUri,
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

  /// Replaces the handler's source with this item and seeks to its resume
  /// point. Returns false when loading failed (error shown) or the attempt
  /// was superseded.
  Future<bool> _loadAndResume(
    _PlaybackAttempt attempt,
    Map<String, String> headers,
  ) async {
    // Flush the previous item before replacing its source, then load.
    await attempt.handler.endProgress();
    if (!_isCurrent(attempt)) return false;
    final loaded = await _loadSource(attempt, headers);
    if (!loaded || !_isCurrent(attempt)) {
      if (attempt.lease.isCurrent) await attempt.lease.release();
      return false;
    }
    _watchPlayback(attempt);

    final resumePosition = await _readResumePosition(attempt.request);
    if (!_isCurrent(attempt)) return false;
    if (resumePosition != null && resumePosition > 0) {
      try {
        await attempt.handler.seekForSession(
          Duration(milliseconds: (resumePosition * 1000).round()),
          attempt.sourceGeneration,
        );
      } catch (_) {
        // A bad or stale resume position must not prevent playback.
      }
    }
    return _isCurrent(attempt);
  }

  /// The position to resume at. After a Retry that is where this screen
  /// last saw the item play: the caller's start position and the saved one
  /// are both older by then, and starting from them would also write that
  /// older position over the newer progress. Otherwise it is the position
  /// the caller passed, else the saved one; that lookup is optional and a
  /// provider error starts at zero.
  Future<double?> _readResumePosition(PlaybackRequest request) async {
    final lastKnown = _lastKnownPosition;
    if (lastKnown != null) return lastKnown.inMilliseconds / 1000.0;
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

  /// Shows the controls, hands progress reporting to the handler and plays.
  void _startLoadedSource(_PlaybackAttempt attempt) {
    final request = attempt.request;
    final handler = attempt.handler;
    handler.setMediaRequest(request);

    setState(() => _isLoading = false);

    // The callbacks capture dependencies, never the screen/ref. The handler's
    // session outlives this route during background playback.
    if (request.savePosition != null && request.markFinished != null) {
      handler.startProgress(
        savePosition: request.savePosition!,
        saveListeningPosition: request.saveListeningPosition,
        listeningOccurrenceID: _remoteReturnOccurrence,
        markFinished: request.markFinished!,
      );
    }
    if (_musicEntries.isNotEmpty) _attachMusicSequence(attempt);
    final startPaused = _startPaused;
    _startPaused = false;
    if (_isCurrent(attempt) &&
        handler.activateSourceSession(attempt.sourceGeneration) &&
        !startPaused) {
      unawaited(handler.play(automatic: true));
    }
  }

  /// Follows the loaded source while this route shows it: remembers where
  /// playback is, for Retry, and shows the error view when the native player
  /// fails, e.g. a codec it cannot decode or a connection lost for good.
  /// Whatever the type of that failure, it is a source failure by then.
  /// While this route is gone (background playback) nothing listens; the
  /// handler still marks the media session as failed.
  void _watchPlayback(_PlaybackAttempt attempt) {
    final request = attempt.request;
    final player = attempt.handler.player;
    _playbackErrorSubscription = attempt.handler.playbackErrors.listen((error) {
      if (!_isCurrent(attempt)) return;
      _logFailure(error, request);
      _showMessage(
        sourceErrorMessage(request.title, request.kind),
        attempt.generation,
      );
    });
    _positionSubscription = player.positionStream.listen((position) {
      if (_isCurrent(attempt)) _rememberPosition(position, attempt);
    });
  }

  void _stopWatchingPlayback() {
    unawaited(_playbackErrorSubscription?.cancel());
    unawaited(_positionSubscription?.cancel());
    _playbackErrorSubscription = null;
    _positionSubscription = null;
  }

  /// Keeps [_lastKnownPosition] current. A failed player reports no useful
  /// position, so it has to be noted while playback still works: positions
  /// reported while the player is idle or loading are not playback.
  ///
  /// Zero counts once playback has progressed (the user sought back to the
  /// start); before that it only means "not started". Near the end the
  /// position is kept, because the handler records "finished" only on its
  /// next tick and a failure in between must not restart from the stale
  /// route position. It is forgotten when the player reports completion —
  /// the handler records "finished" then — or, for sources without progress
  /// (public shares), past the finished threshold; replaying then starts by
  /// the usual rules rather than at the end.
  void _rememberPosition(Duration position, _PlaybackAttempt attempt) {
    final player = attempt.handler.player;
    final state = player.processingState;
    if (state == ProcessingState.idle || state == ProcessingState.loading) {
      return;
    }
    final duration = player.duration;
    final pastEnd = duration != null &&
        duration > Duration.zero &&
        position.inMilliseconds / duration.inMilliseconds >= 0.95;
    final finished = state == ProcessingState.completed ||
        (pastEnd && attempt.request.markFinished == null);
    if (finished) {
      _lastKnownPosition = null;
    } else if (position > Duration.zero || _lastKnownPosition != null) {
      _lastKnownPosition = position;
    }
  }

  /// Builds the request, looking up the playback URL when the route supplied
  /// only a media ID (no typed request and no URL in the route extra), as the
  /// podcast episode list does.
  Future<PlaybackRequest> _resolveRequest(int generation) async {
    var request = _effectiveRequest();
    if (_remoteReturnRequest != null) {
      _musicEntries = _remoteReturnQueue;
      _musicStartIndex = _remoteReturnIndex;
      final entries = _musicEntries;
      _expandMusic = () async => entries;
      return _refreshRetryQuality(_remoteReturnRequest!, generation,
          remoteReturn: true);
    }
    _musicEntries = const [];
    _musicStartIndex = 0;
    _expandMusic = null;
    if (widget.request == null && widget.mediaUrl == null) {
      request = await resolveServerPlaybackSource(
          request, ref.read(apiClientProvider));
    }
    request = await _refreshRetryQuality(request, generation);
    if (request is! ServerPlaybackRequest || widget.sequence.isEmpty) {
      return request;
    }
    final mediaId = request.mediaId;
    final client = ref.read(apiClientProvider);
    final progress = ref.read(progressQueueProvider);
    final sets = await client.listSets();
    final selected = widget.sequence.where((m) => m.id == mediaId);
    if (selected.isEmpty || !isMusicTrack(selected.first, sets)) return request;
    final origin = request.serverOrigin;
    final user = request.userId;
    final setId = selected.first.setId;
    PlaybackRequest entry(Media media) => ServerPlaybackRequest(
        mediaId: media.id,
        serverOrigin: origin,
        userId: user,
        sourceUri: Uri.parse(client.playbackUrl(media)),
        title: media.displayTitle,
        artist: media.music?.artist ?? '',
        album: media.music?.album ?? '',
        music: media.music,
        artworkUri: media.thumbnailPath.isNotEmpty
            ? Uri.parse(client.thumbnailUrl(media.id))
            : null,
        sourceName: media.relPath,
        startPosition:
            widget.albumCollection && media.duration <= 1200 ? 0 : null,
        readPosition: () => client.getMediaProgress(media.id),
        savePosition: (seconds) => progress.enqueue(media.id, seconds),
        saveListeningPosition: (seconds, occurrence) =>
            progress.enqueueListeningPosition(media.id, seconds, occurrence),
        markFinished: () => progress.enqueueFinished(media.id));
    bool included(Media media) =>
        (widget.albumCollection || media.setId == setId) &&
        isMusicTrack(media, sets);
    final requestedIndex = widget.sequenceIndex;
    final selectedIndex = requestedIndex != null &&
            requestedIndex >= 0 &&
            requestedIndex < widget.sequence.length &&
            widget.sequence[requestedIndex].id == mediaId
        ? requestedIndex
        : widget.sequence.indexWhere((media) => media.id == mediaId);
    // Preserve the selected occurrence while filtering generic sequences.
    // Repeated media IDs refer to distinct queue entries, not one song slot.
    _musicStartIndex =
        widget.sequence.take(selectedIndex).where(included).length;
    _musicEntries = widget.sequence.where(included).map(entry).toList();
    final displayed = List<PlaybackRequest>.of(_musicEntries);
    final traverse = widget.traverseFolders;
    _expandMusic = () async {
      if (!traverse) return displayed;
      final media = await browseMusicSet(client, setId);
      return media.where((m) => isMusicTrack(m, sets)).map(entry).toList();
    };
    if (widget.albumCollection) {
      return _musicEntries[_musicStartIndex];
    }
    final display = _musicEntries[_musicStartIndex];
    return ServerPlaybackRequest(
        mediaId: request.mediaId,
        serverOrigin: request.serverOrigin,
        userId: request.userId,
        sourceUri: request.sourceUri,
        title: display.title,
        artist: display.artist,
        album: display.album,
        music: display.music,
        artworkUri: display.artworkUri,
        sourceName: request.sourceName,
        startPosition: request.startPosition,
        readPosition: request.readPosition,
        savePosition: request.savePosition,
        saveListeningPosition: request.saveListeningPosition,
        markFinished: request.markFinished);
  }

  Future<PlaybackRequest> _refreshRetryQuality(
      PlaybackRequest request, int generation,
      {bool remoteReturn = false}) async {
    if (!_refreshQualityOnRetry || request is! ServerPlaybackRequest) {
      return request;
    }
    final uri =
        await currentServerAudioUri(request, ref.read(apiClientProvider));
    if (!mounted || generation != _initGeneration) return request;
    final refreshed = request.withSourceUri(uri);
    if (remoteReturn) {
      _remoteReturnRequest = refreshed;
    } else {
      _qualityRetryRequest = refreshed;
    }
    _refreshQualityOnRetry = false;
    return refreshed;
  }

  void _attachMusicSequence(_PlaybackAttempt attempt) {
    final handler = attempt.handler;
    final lease = attempt.lease;
    final coordinator = ref.read(playbackSessionCoordinatorProvider);
    handler.startMusicSequence(
        queueLabel:
            widget.sequence.firstOrNull?.music?.album ?? context.tr('Album'),
        sequenceLease: lease,
        claimQueue: (request) => coordinator.claim(
            kind: request.kind,
            identity: request.identity,
            sourceUri: request.sourceUri.toString(),
            stop: handler.stop),
        wrap: _remoteReturnRequest == null && !widget.albumCollection,
        entries: _musicEntries,
        index: _musicStartIndex,
        expand: _expandMusic!,
        changed: lease.updateSource,
        load: ref.read(cachedAudioLoaderProvider));
    _watchMusicState(handler, lease);
  }

  void _watchMusicState(
      PlayerAudioHandler handler, PlaybackSessionLease lease) {
    unawaited(_musicSubscription?.cancel());
    _musicSubscription = handler.musicChanges.listen((state) {
      final activeLease = handler.sequenceLease ?? lease;
      if (!mounted || !activeLease.isCurrent) return;
      _sessionLease = activeLease;
      _stopWatchingPlayback();
      if (_activeRequest?.identity != state.request.identity) {
        _lastKnownPosition = null;
      }
      _activeRequest = state.request;
      if (_remoteReturnRequest != null) {
        _remoteReturnRequest = state.request;
        _remoteReturnIndex = state.index;
        final queue = handler.musicQueue;
        _remoteReturnQueue = queue.active && queue.entries.isNotEmpty
            ? queue.entries.map((entry) => entry.request).toList()
            : _musicEntries;
        _remoteReturnOccurrence = handler.listeningOccurrenceID;
      }

      _activeSourceGeneration = state.sourceGeneration;
      setState(() {
        _isLoading = state.loading;
        _error = state.error == null
            ? null
            : playbackErrorMessage(state.error!,
                title: state.request.title, source: state.request.kind);
      });
      if (!state.loading && state.error == null) {
        _watchPlayback(_PlaybackAttempt(
            request: state.request,
            lease: activeLease,
            generation: _initGeneration,
            handler: handler,
            sourceGeneration: state.sourceGeneration,
            stopGeneration: handler.stopGeneration));
      }
    });
  }

  PlaybackRequest _effectiveRequest() {
    final request =
        _remoteReturnRequest ?? _qualityRetryRequest ?? widget.request;
    if (request != null) return request;
    final title = widget.mediaTitle ??
        (widget.isPublicShare ? 'Shared Audio' : 'Audio – ${widget.mediaId}');
    if (widget.isPublicShare) {
      final url = widget.mediaUrl;
      if (url == null) throw StateError('Public audio share has no stream URL');
      return PublicSharePlaybackRequest(
        shareToken: shareTokenFromUrl(Uri.parse(url)),
        sourceUri: Uri.parse(url),
        title: title,
        sourceName: widget.sourceName,
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
      sourceName: widget.sourceName,
      startPosition: widget.startPosition,
      readPosition: widget.startPosition == null
          ? () => client.getMediaProgress(mediaId)
          : null,
      savePosition: (seconds) => queue.enqueue(mediaId, seconds),
      saveListeningPosition: (seconds, occurrence) =>
          queue.enqueueListeningPosition(mediaId, seconds, occurrence),
      markFinished: () => queue.enqueueFinished(mediaId),
    );
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

  /// Loads the attempt's source into the player with [headers]; returns
  /// `true` on success.
  ///
  /// On failure, sets the error UI state and returns `false` so the caller
  /// can short-circuit without nesting the remaining steps inside a try/catch.
  Future<bool> _loadSource(
    _PlaybackAttempt attempt,
    Map<String, String> headers,
  ) async {
    try {
      final source = attempt.request is ServerPlaybackRequest
          ? await ref.read(cachedAudioLoaderProvider)(
              attempt.request, () => _isCurrent(attempt),
              onPreparing: (preparation) {
              if (!_isCurrent(attempt)) {
                preparation.cancel();
                return;
              }
              trackPlaybackPreparation(preparation);
            })
          : AudioSource.uri(attempt.request.sourceUri,
              headers: headers.isEmpty ? null : headers);
      if (source == null || !_isCurrent(attempt)) {
        _showMessage(kPlaybackStoppedMessage, attempt.generation);
        return false;
      }
      await attempt.handler
          .loadSourceForSession(source, attempt.sourceGeneration);
      return true;
    } catch (error) {
      if (_isCurrent(attempt)) {
        _showError(error, attempt.request, attempt.generation);
      }
      return false;
    }
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
    _logFailure(error, request);
    _showMessage(
      playbackErrorMessage(error, title: request.title, source: request.kind),
      generation,
    );
  }

  /// Debug-only, kind and type only: see [_showError].
  void _logFailure(Object error, PlaybackRequest request) {
    if (!kDebugMode) return;
    debugPrint(
      'Audio playback (${request.kind.name}) failed: ${error.runtimeType}',
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
  // Casting
  // ---------------------------------------------------------------------------

  @override
  PlaybackRequest? get castableRequest => _activeRequest;

  @override
  List<PlaybackRequest> get castQueue {
    final handler = ref.read(audioHandlerProvider);
    final queue = handler.musicQueue;
    return queue.active && queue.entries.isNotEmpty
        ? queue.entries.map((entry) => entry.request).toList()
        : _musicEntries;
  }

  @override
  int get castQueueIndex {
    final handler = ref.read(audioHandlerProvider);
    return handler.musicQueue.active
        ? handler.musicQueue.index
        : (handler.musicState?.index ?? _musicStartIndex);
  }

  @override
  String? get castOccurrenceID =>
      ref.read(audioHandlerProvider).listeningOccurrenceID;
  @override
  bool get castRepeatAll =>
      ref.read(audioHandlerProvider).musicQueue.repeat == MusicRepeat.all;
  @override
  bool get castRepeatOne =>
      ref.read(audioHandlerProvider).musicQueue.repeat == MusicRepeat.one;

  @override
  void onRemoteRequestChanged(PlaybackRequest request) {
    final controller = ref.read(castControllerProvider);
    unawaited(_musicSubscription?.cancel());
    _musicSubscription = null;
    _stopWatchingPlayback();
    _activeRequest = request;
    _remoteReturnRequest = request;
    _remoteReturnQueue = controller.rendererQueue;
    _remoteReturnIndex = controller.rendererIndex;
    _remoteReturnOccurrence = controller.currentOccurrenceID;
    setState(() {
      _isLoading = false;
      _error = null;
    });
  }

  @override
  bool get castIsVideo => false;

  @override
  Duration currentLocalPosition() =>
      ref.read(audioHandlerProvider).player.position;

  @override
  Future<void> pauseLocalPlayback() async {
    final generation = _activeSourceGeneration;
    final handler = ref.read(audioHandlerProvider);
    if (generation != null && handler.isSourceSessionCurrent(generation)) {
      await handler.pause();
    }
  }

  @override
  void onCastEnded(Duration? position) {
    // Finished on the TV: start over here rather than at the very end.
    final controller = ref.read(castControllerProvider);
    _remoteReturnOccurrence = controller.currentOccurrenceID;
    _lastKnownPosition = position ?? Duration.zero;
    _startPaused = true;
    _onRetry();
  }

  // ---------------------------------------------------------------------------
  // Actions
  // ---------------------------------------------------------------------------

  /// Tears down the current player source and re-runs [_initPlayer].
  ///
  /// Shared by error recovery and returning from remote playback.
  void _onRetry() {
    final handler = ref.read(audioHandlerProvider);
    if (handler.musicState?.error != null && _sessionLease?.isCurrent == true) {
      _refreshQualityOnRetry = false;
      unawaited(handler.retryMusicTrack());
      return;
    }
    _initGeneration++;
    setState(() {
      cancelPlaybackPreparation();
      _error = null;
      _isLoading = true;
      _playbackSpeed = 1.0;
    });
    _initPlayer();
  }

  Future<void> _showErrorQuality() async {
    final request = _activeRequest;
    if (request is! ServerPlaybackRequest) return;
    final generation = _initGeneration;
    final previous = StreamQuality.selected(request.serverOrigin);
    await showStreamQualitySheet(context, ref.read(apiClientProvider));
    if (!mounted ||
        generation != _initGeneration ||
        _activeRequest?.identity != request.identity) {
      return;
    }
    _refreshQualityOnRetry = _refreshQualityOnRetry ||
        StreamQuality.selected(request.serverOrigin) != previous;
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
    final sourceGeneration = _activeSourceGeneration;
    if (sourceGeneration != null) {
      await handler.seekForSession(next, sourceGeneration);
    }
  }

  Future<void> _seek(Duration position) async {
    final generation = _activeSourceGeneration;
    if (generation == null) return;
    await ref.read(audioHandlerProvider).seekForSession(position, generation);
  }

  /// Applies [speed] to the handler and updates the UI state.
  Future<void> _setSpeed(double speed) async {
    final handler = ref.read(audioHandlerProvider);
    final generation = _activeSourceGeneration;
    if (generation == null || !handler.isSourceSessionCurrent(generation)) {
      return;
    }
    await handler.setSpeed(speed);
    if (!mounted) return;
    setState(() => _playbackSpeed = speed);
  }

  // ---------------------------------------------------------------------------
  // Build
  // ---------------------------------------------------------------------------

  @override
  Widget build(BuildContext context) {
    final casting = isCastingRequest;
    return Scaffold(
      backgroundColor: Colors.black,
      appBar: AppBar(
        actions: [
          if (!widget.isPublicShare && !casting) const MusicQueueButton(),
          if (_activeRequest is ServerPlaybackRequest &&
              !_isLoading &&
              _error == null)
            IconButton(
                tooltip: context.tr('Info'),
                icon: const Icon(Icons.info_outline),
                onPressed: _showMusicInfo),
          buildCastButton(ready: !_isLoading && _error == null),
          if (_activeRequest != null &&
              !_isLoading &&
              _error == null &&
              !casting &&
              !widget.isPublicShare)
            _buildPersonalActions(),
        ],
        backgroundColor: Colors.black,
        foregroundColor: Colors.white,
        title: Text(_activeRequest?.title ??
            widget.mediaTitle ??
            (widget.isPublicShare
                ? 'Shared Audio'
                : 'Audio – ${widget.mediaId}')),
      ),
      body: Column(children: [
        SleepTimerControl(timer: ref.read(audioHandlerProvider).sleepTimer),
        Expanded(
            child:
                casting ? CastPlaybackView(onStop: stopCasting) : _buildBody()),
      ]),
    );
  }

  void _showMusicInfo() {
    final request = _activeRequest;
    if (request is! ServerPlaybackRequest) return;
    final lease = _sessionLease;
    final generation = _activeSourceGeneration;
    final mutations = ref.read(credentialMutationQueueProvider);
    final epoch = mutations.generation;
    final currentClient = ref.read(apiClientProvider);
    final client = currentClient is DioPlayerApiClient
        ? DioPlayerApiClient(
            dio: currentClient.rawDio, credentialEpoch: () => epoch)
        : currentClient;
    bool current() =>
        mounted &&
        lease != null &&
        lease.isCurrent &&
        _sessionLease == lease &&
        _activeRequest?.identity == request.identity &&
        generation != null &&
        ref.read(audioHandlerProvider).isSourceSessionCurrent(generation) &&
        mutations.generation == epoch &&
        request.serverOrigin == ref.read(playerBaseUrlProvider).origin &&
        !ref.read(authStateProvider).isLoading &&
        ref.read(authStateProvider).valueOrNull?.isAuthenticated == true &&
        ref.read(authStateProvider).valueOrNull?.user?.id == request.userId;
    showDialog<void>(
        context: context,
        builder: (_) => MusicInfoDialog(
            load: () => client.getMedia(request.mediaId), isCurrent: current));
  }

  Widget _buildPersonalActions() {
    final request = _activeRequest!;
    final lease = _sessionLease;
    final generation = _activeSourceGeneration;
    final handler = ref.read(audioHandlerProvider);
    void check() {
      if (!mounted ||
          lease == null ||
          !lease.isCurrent ||
          _sessionLease != lease ||
          _activeRequest?.identity != request.identity ||
          generation == null ||
          !handler.isSourceSessionCurrent(generation) ||
          _error != null) {
        throw StateError('Playback is no longer active.');
      }
    }

    return PlaybackPersonalActions(
        request: request,
        position: () {
          check();
          return handler.player.position;
        },
        duration: () {
          check();
          return handler.player.duration;
        },
        onSeek: (position) async {
          check();
          await handler.seekForSession(position, generation!);
        });
  }

  /// Selects the appropriate body widget based on current state.
  Widget _buildBody() {
    if (_isLoading) return _buildLoadingView();
    if (_error != null) return _buildErrorView(_error!);
    return _buildPlayerView();
  }

  /// Full-screen loading spinner shown while the player initialises, with a
  /// "preparing" label while the server is still producing the stream.
  Widget _buildLoadingView() {
    return PlaybackLoadingView(
      key: const Key('audio_player_loading'),
      preparing: isPreparingPlayback,
    );
  }

  /// Error view shown when initialisation or playback fails, and when
  /// another item took over the player.
  ///
  /// Provides a human-readable message and a retry button so the user can
  /// play the item again without navigating away.
  Widget _buildErrorView(String message) {
    return Center(
      key: const Key('audio_player_error'),
      child: SingleChildScrollView(
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
              child: Text(context.tr('Retry')),
            ),
            if (_activeRequest?.kind == PlaybackSourceKind.server) ...[
              const SizedBox(height: 16),
              TextButton.icon(
                onPressed: _showErrorQuality,
                icon: const Icon(Icons.tune),
                label: const Text('Quality'),
              ),
              const Text(
                'Choose another quality, then Retry.',
                style: TextStyle(color: Colors.white70),
                textAlign: TextAlign.center,
              ),
            ],
          ],
        ),
      ),
    );
  }

  /// The main playback UI: cover art, seek bar, and controls.
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
                AudioVisuals(
                  key: ValueKey('visuals:${_activeRequest!.identity}'),
                  player: handler.player,
                  title: _activeRequest!.visualContext,
                  artwork: AudioCoverArt(request: _activeRequest!),
                ),
                const SizedBox(height: 16),
                Semantics(
                    label:
                        '${_activeRequest!.title}, ${_activeRequest!.subtitle}',
                    child: Column(children: [
                      Text(_activeRequest!.title,
                          key: const Key('audio_track_title'),
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                          textAlign: TextAlign.center,
                          style: const TextStyle(
                              color: Colors.white,
                              fontSize: 20,
                              fontWeight: FontWeight.w600)),
                      const SizedBox(height: 4),
                      Text(_activeRequest!.subtitle,
                          key: const Key('audio_track_subtitle'),
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                          textAlign: TextAlign.center,
                          style: const TextStyle(color: Colors.white70)),
                    ])),
                const SizedBox(height: 16),
                _buildSeekBar(handler),
                const SizedBox(height: 16),
                _buildControls(handler),
                const SizedBox(height: 16),
                if (handler.musicState != null)
                  Row(mainAxisAlignment: MainAxisAlignment.center, children: [
                    IconButton(
                        onPressed: () => _skip(-_kSkipDuration),
                        tooltip: 'Skip back 15 seconds',
                        icon:
                            const Icon(Icons.fast_rewind, color: Colors.white)),
                    IconButton(
                        onPressed: () => _skip(_kSkipDuration),
                        tooltip: 'Skip forward 15 seconds',
                        icon: const Icon(Icons.fast_forward,
                            color: Colors.white)),
                  ]),
                _buildSpeedSelector(),
                StreamBuilder<void>(
                    stream: handler.queueChanges,
                    builder: (context, _) => Column(children: [
                          SwitchListTile.adaptive(
                              title: const Text('Gapless music queue',
                                  style: TextStyle(color: Colors.white)),
                              subtitle: Text(handler.gaplessStatus,
                                  style:
                                      const TextStyle(color: Colors.white70)),
                              value: handler.gaplessEnabled,
                              onChanged: handler.setGaplessEnabled),
                          const Padding(
                              padding: EdgeInsets.symmetric(horizontal: 16),
                              child: Text(
                                  'Prepares the next song in the native decoder. '
                                  'Applies when you next open a Music queue song. '
                                  'Spoken audio, resume items and remote playback '
                                  'use normal advance.',
                                  style: TextStyle(color: Colors.white70))),
                        ])),
                TextButton.icon(
                  onPressed: () => showLoudnessSheet(context, handler.loudness),
                  icon: const Icon(Icons.volume_up),
                  label: const Text('Loudness'),
                ),
                TextButton.icon(
                  onPressed: () =>
                      showEqualizerSheet(context, handler.equalizer),
                  icon: const Icon(Icons.equalizer),
                  label: const Text('Equalizer'),
                ),
                if (_activeRequest?.kind == PlaybackSourceKind.server)
                  TextButton.icon(
                    onPressed: () => showStreamQualitySheet(
                        context, ref.read(apiClientProvider)),
                    icon: const Icon(Icons.tune),
                    label: const Text('Quality'),
                  ),
                if (_activeRequest?.kind != PlaybackSourceKind.local)
                  _buildLyrics(handler),
              ],
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildLyrics(PlayerAudioHandler handler) {
    final request = _activeRequest!;
    return LyricsPanel(
      key: ValueKey('lyrics:$_initGeneration:${request.identity}'),
      positionStream: handler.player.positionStream,
      position: () => handler.player.position,
      onSeek: _seek,
      load: request is ServerPlaybackRequest
          ? () => ref.read(apiClientProvider).getMediaLyrics(request.mediaId)
          : () {
              final share = request as PublicSharePlaybackRequest;
              return ref.read(publicApiClientProvider).getSharedLyrics(
                    share.shareToken,
                    view: share.sourceUri.queryParameters['view'],
                  );
            },
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
                  ? (v) => _seek(Duration(milliseconds: v.round()))
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
    final music = handler.musicState != null;
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
              icon: Icon(music ? Icons.skip_previous : Icons.fast_rewind,
                  color: Colors.white, size: 36),
              onPressed: () =>
                  music ? handler.skipToPrevious() : _skip(-_kSkipDuration),
              constraints: const BoxConstraints(minWidth: 48, minHeight: 48),
              tooltip:
                  music ? context.tr('Previous track') : 'Skip back 15 seconds',
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
              onPressed: () {
                if (_sessionLease?.isCurrent != true) return;
                final generation = _activeSourceGeneration;
                if (generation == null ||
                    !handler.isSourceSessionCurrent(generation)) {
                  _onRetry();
                  return;
                }
                if (isPlaying) {
                  handler.pause();
                } else {
                  handler.play();
                }
              },
              tooltip: isPlaying ? context.tr('Pause') : context.tr('Play'),
            ),
            const SizedBox(width: 16),
            // Skip forward 15 s
            IconButton(
              key: const Key('audio_player_skip_forward'),
              icon: Icon(
                music ? Icons.skip_next : Icons.fast_forward,
                color: Colors.white,
                size: 36,
              ),
              onPressed: () =>
                  music ? handler.skipToNext() : _skip(_kSkipDuration),
              constraints: const BoxConstraints(minWidth: 48, minHeight: 48),
              tooltip:
                  music ? context.tr('Next track') : 'Skip forward 15 seconds',
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

/// One run of [_AudioPlayerScreenState._initPlayer]: what is being played,
/// the ownership of the shared player it claimed, and the generations that
/// tell whether it was superseded (Retry or another item on this screen, a
/// newer source or a stop on the handler).
class _PlaybackAttempt {
  const _PlaybackAttempt({
    required this.request,
    required this.lease,
    required this.generation,
    required this.handler,
    required this.sourceGeneration,
    required this.stopGeneration,
  });

  final PlaybackRequest request;
  final PlaybackSessionLease lease;
  final int generation;
  final PlayerAudioHandler handler;
  final int sourceGeneration;
  final int stopGeneration;
}
