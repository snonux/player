import 'dart:async';
import 'dart:math';

import 'package:flutter/foundation.dart';

import '../../api/player_api_client.dart';
import '../playback_request.dart';
import '../playback_session_coordinator.dart';
import 'cast_device.dart';
import '../sleep_timer.dart';
import 'cast_session.dart';

/// Builds what the receiver plays for a request. Separate from
/// [CastController] so the server lookup can be faked in tests.
typedef CastMediaResolver = Future<CastMedia> Function(
  PlaybackRequest request, {
  required bool isVideo,
});

typedef CastSessionStarter = Future<RemotePlaybackSession> Function(
    CastDevice device);

/// Why casting could not start or stopped early; each has its own message.
enum CastFailure {
  /// Local files and offline copies live on the phone, which the cast
  /// device cannot reach.
  notCastable,

  /// No connection to the device, or it did not start the receiver.
  deviceUnreachable,

  /// The server refused the cast ticket or could not be reached.
  serverRefused,

  /// The receiver could not load the media (format, or it cannot reach the
  /// server address).
  loadFailed,
}

class CastStartException implements Exception {
  const CastStartException(this.failure);

  final CastFailure failure;

  String get message => castFailureMessage(failure);

  @override
  String toString() => 'CastStartException(${failure.name})';
}

String castFailureMessage(CastFailure failure) => switch (failure) {
      CastFailure.notCastable =>
        'Files stored on this phone cannot be cast. Play them here instead.',
      CastFailure.deviceUnreachable =>
        'Could not connect to the playback device. DLNA speakers must be on '
            'the Player server’s network; Cast devices use this phone’s Wi-Fi.',
      CastFailure.serverRefused =>
        'The server did not allow casting this item. Try again.',
      CastFailure.loadFailed =>
        'The cast device could not play this item. It must be able to reach '
            'the server at the address set in Settings.',
    };

/// Whether [request] can be cast: the receiver fetches the media itself, so
/// only server items and public shares qualify.
bool isCastable(PlaybackRequest request) =>
    request is ServerPlaybackRequest || request is PublicSharePlaybackRequest;

/// What the controller is casting right now.
@immutable
class ActiveCast {
  const ActiveCast({
    required this.device,
    required this.request,
    required this.status,
    required this.position,
  });

  final CastDevice device;
  final PlaybackRequest request;
  final CastMediaStatus status;

  /// Estimated position at the time this snapshot was taken.
  final Duration position;
}

/// Where casting ended, for the phone to continue from. A null [position]
/// means the item finished on the receiver.
@immutable
class CastEnded {
  const CastEnded({required this.identity, this.position, this.failure});

  final String identity;
  final Duration? position;
  final CastFailure? failure;
}

/// Owns at most one cast at a time, independent of any screen, so playback
/// on the TV and its progress saving continue when the player screen is
/// closed.
///
/// A cast holds the shared [PlaybackSessionCoordinator] lease like a local
/// player: starting a cast stops local playback, and local playback of
/// another item (or signing out) stops the cast.
class CastController extends ChangeNotifier {
  CastController({
    this.sleepTimer,
    required PlaybackSessionCoordinator coordinator,
    required CastMediaResolver resolveMedia,
    CastSessionStarter? startSession,
    this.progressInterval = const Duration(seconds: 10),
  })  : _coordinator = coordinator,
        _resolveMedia = resolveMedia,
        _startSession = startSession ?? ((device) => CastSession.start(device));

  final SleepTimer? sleepTimer;
  final PlaybackSessionCoordinator _coordinator;
  final CastMediaResolver _resolveMedia;
  final CastSessionStarter _startSession;
  final Duration progressInterval;

  RemotePlaybackSession? _session;
  RemotePlaybackSession? _pendingRenderer;
  int _startRevision = 0;
  PlaybackRequest? _request;
  PlaybackSessionLease? _lease;
  StreamSubscription<CastMediaStatus>? _statusSubscription;
  Timer? _progressTimer;
  bool _finished = false;
  bool _starting = false;
  bool _startingRenderer = false;
  bool get isStartingRenderer => _startingRenderer;
  Duration? _lastSaved;
  final _ended = StreamController<CastEnded>.broadcast();
  List<PlaybackRequest> _rendererQueue = const [];
  int _rendererIndex = 0;
  bool _advancing = false;
  bool _isVideo = false;
  bool _rendererRepeatAll = false, _rendererRepeatOne = false;
  String _occurrence = '';
  int get rendererIndex => _rendererIndex;
  bool get hasPrevious =>
      _rendererQueue.isNotEmpty && (_rendererIndex > 0 || _rendererRepeatAll);
  bool get hasNext =>
      _rendererQueue.isNotEmpty &&
      (_rendererIndex + 1 < _rendererQueue.length || _rendererRepeatAll);
  List<PlaybackRequest> get rendererQueue => List.unmodifiable(_rendererQueue);
  PlaybackRequest? get currentRequest => _request;
  String get currentOccurrenceID => _occurrence;

  /// The cast in progress, or null.
  ActiveCast? get active {
    final session = _session;
    final request = _request;
    if (session == null || request == null) return null;
    return ActiveCast(
      device: session.device,
      request: request,
      status: session.lastStatus,
      position: session.position,
    );
  }

  bool get isStarting => _starting;

  bool isCasting(String identity) => _request?.identity == identity;

  /// Fires once per cast when it ends, for whatever reason.
  Stream<CastEnded> get ended => _ended.stream;

  /// Casts [request] to [device] from [position]. Local playback of any item
  /// is stopped first. Throws [CastStartException] when it cannot start.
  Future<void> start({
    required CastDevice device,
    required PlaybackRequest request,
    required Duration position,
    required bool isVideo,
    List<PlaybackRequest> rendererQueue = const [],
    int rendererIndex = 0,
    String? existingOccurrence,
    bool rendererRepeatAll = false,
    bool rendererRepeatOne = false,
  }) async {
    if (!isCastable(request)) {
      throw const CastStartException(CastFailure.notCastable);
    }
    if (_starting) return;
    final startRevision = ++_startRevision;
    var localGeneration = _coordinator.generation;
    final sleepRevision = sleepTimer?.beginPlaybackIntent();
    _starting = true;
    _startingRenderer = device.isRenderer;
    notifyListeners();
    try {
      await _end(saveProgress: true);
      if (_coordinator.generation != localGeneration &&
          _coordinator.activeIdentity != null) {
        return;
      }
      localGeneration = _coordinator.generation;
      final CastMedia media;
      try {
        media = await _resolveMedia(request, isVideo: isVideo);
      } catch (_) {
        throw const CastStartException(CastFailure.serverRefused);
      }
      if (startRevision != _startRevision ||
          localGeneration != _coordinator.generation) {
        return;
      }
      final RemotePlaybackSession session;
      try {
        session = await _startSession(device);
      } catch (_) {
        throw const CastStartException(CastFailure.deviceUnreachable);
      }
      if (startRevision != _startRevision ||
          localGeneration != _coordinator.generation) {
        try {
          await session.stop();
        } catch (_) {}
        return;
      }
      if (device.isRenderer) _pendingRenderer = session;
      try {
        // Resolve/connect can outlive expiry. Never issue an autoplay LOAD
        // once that deadline has already expired.
        sleepTimer?.check();
        if (sleepRevision != null &&
            !sleepTimer!.allowsPlayback(sleepRevision)) {
          try {
            await session.stop();
          } catch (_) {}
          return;
        }
        await session.load(media, position: position);
        sleepTimer?.check();
        if (sleepTimer?.expired == true &&
            sleepTimer?.revision != sleepRevision) {
          await session.pause();
        }
      } catch (_) {
        try {
          await session.stop();
        } catch (_) {}
        throw const CastStartException(CastFailure.loadFailed);
      }
      if (startRevision != _startRevision ||
          localGeneration != _coordinator.generation) {
        try {
          await session.stop();
        } catch (_) {}
        return;
      }
      // Claimed only now: until the TV plays, the phone keeps playing.
      final lease = await _coordinator.claim(
        kind: request.kind,
        identity: request.identity,
        stop: () => _end(saveProgress: true),
      );
      if (lease == null || startRevision != _startRevision) {
        try {
          await session.stop();
        } catch (_) {}
        await lease?.release();
        return;
      }
      _pendingRenderer = null;
      _rendererQueue =
          device.isRenderer ? List.unmodifiable(rendererQueue) : const [];
      _rendererIndex = rendererIndex.clamp(
          0, _rendererQueue.isEmpty ? 0 : _rendererQueue.length - 1);
      _isVideo = isVideo;
      _rendererRepeatAll = rendererRepeatAll;
      _rendererRepeatOne = rendererRepeatOne;
      _occurrence = existingOccurrence?.isNotEmpty == true
          ? existingOccurrence!
          : _newOccurrence();
      _attach(session, request, lease);
    } finally {
      _pendingRenderer = null;
      _starting = false;
      _startingRenderer = false;
      notifyListeners();
    }
  }

  void _attach(
    RemotePlaybackSession session,
    PlaybackRequest request,
    PlaybackSessionLease lease,
  ) {
    _session = session;
    _request = request;
    _lease = lease;
    _finished = false;
    _lastSaved = null;
    _statusSubscription = session.status.listen((status) {
      if (_advancing) return;
      if (status.finished && !_finished) {
        _finished = true;
        unawaited(
            _finish(_request ?? request, session, _occurrence, _startRevision));
        // Completion persistence can wait for network sync. Observers must
        // see the receiver's idle state immediately (including screen awake).
        notifyListeners();
        return;
      }
      if (status.failed) {
        unawaited(_end(saveProgress: true, failure: CastFailure.loadFailed));
        return;
      }
      notifyListeners();
    });
    _progressTimer = Timer.periodic(progressInterval, (_) => _saveProgress());
    unawaited(session.closed.then((_) {
      if (identical(_session, session)) unawaited(_end(saveProgress: true));
    }));
    notifyListeners();
  }

  Future<void> play() async {
    sleepTimer?.resume();
    await _session?.play();
  }

  Future<void> pause() async => _session?.pause();

  Future<void> seek(Duration position) async => _session?.seek(position);

  Future<void> setVolume(double level) async => _session?.setVolume(level);

  /// Ends the cast and the receiver's playback. Returns where it was, for
  /// the phone to continue from.
  Future<Duration?> stop() async {
    ++_startRevision;
    final pending = _pendingRenderer;
    _pendingRenderer = null;
    if (pending != null) {
      try {
        await pending.stop();
      } catch (_) {}
    }
    final position = _finished ? null : _session?.position;
    await _end(saveProgress: true);
    return position;
  }

  Future<void> _finish(PlaybackRequest request, RemotePlaybackSession session,
      String occurrence, int revision) async {
    try {
      await request.markFinished?.call();
    } catch (_) {}
    // Saving completion may outlive a manual queue move or a replacement
    // session, including another occurrence of the same media identity.
    if (!identical(_session, session) ||
        !identical(_request, request) ||
        _occurrence != occurrence ||
        _startRevision != revision ||
        _lease?.isCurrent != true) {
      return;
    }
    if (_session?.device.isRenderer == true &&
        (_rendererRepeatOne || hasNext)) {
      await moveRendererQueue(_rendererRepeatOne ? 0 : 1, explicit: false);
    } else {
      await _end(saveProgress: false);
    }
  }

  String _newOccurrence() {
    final random = Random.secure();
    return List.generate(
            16, (_) => random.nextInt(256).toRadixString(16).padLeft(2, '0'))
        .join();
  }

  /// Advances a renderer's snapshot without releasing remote playback ownership.
  Future<void> moveRendererQueue(int delta, {bool explicit = true}) async {
    if (_advancing || _starting) return;
    var index = _rendererIndex + delta;
    if (_rendererRepeatAll && _rendererQueue.isNotEmpty) {
      index %= _rendererQueue.length;
    }
    final session = _session;
    if (session == null ||
        !session.device.isRenderer ||
        index < 0 ||
        index >= _rendererQueue.length) {
      return;
    }
    ++_startRevision;
    _saveProgress();
    _advancing = true;
    final request = _rendererQueue[index];
    try {
      sleepTimer?.check();
      if (!explicit && sleepTimer?.expired == true) return;
      final revision =
          explicit ? sleepTimer?.beginPlaybackIntent() : sleepTimer?.revision;
      final media = await _resolveMedia(request, isVideo: _isVideo);
      if (!identical(_session, session) || _lease?.isCurrent != true) return;
      sleepTimer?.check();
      if (revision != null && !sleepTimer!.allowsPlayback(revision)) return;
      await session.load(media);
      sleepTimer?.check();
      if (revision != null && !sleepTimer!.allowsPlayback(revision)) {
        await session.pause();
      }
      if (!identical(_session, session)) return;
      _request = request;
      _rendererIndex = index;
      _finished = false;
      _lastSaved = null;
      _occurrence = _newOccurrence();
      _lease?.updateSource(request);
      notifyListeners();
    } catch (_) {
      await _end(saveProgress: true, failure: CastFailure.loadFailed);
    } finally {
      _advancing = false;
    }
  }

  void _saveProgress() {
    final session = _session;
    final save = _request?.savePosition;
    if (session == null || save == null || _finished || _advancing) return;
    if (session.lastStatus.playerState == CastPlayerState.idle) return;
    final position = session.position;
    if (position <= Duration.zero || position == _lastSaved) return;
    _lastSaved = position;
    final listening = _request?.saveListeningPosition;
    final seconds = position.inMilliseconds / 1000.0;
    unawaited(
        (listening != null ? listening(seconds, _occurrence) : save(seconds))
            .catchError((_) {}));
  }

  Future<void> _end({required bool saveProgress, CastFailure? failure}) async {
    final session = _session;
    final request = _request;
    final lease = _lease;
    if (session == null || request == null) return;
    if (saveProgress) _saveProgress();
    final position = _finished ? null : session.position;
    _session = null;
    _request = null;
    _lease = null;
    _progressTimer?.cancel();
    _progressTimer = null;
    final statusSubscription = _statusSubscription;
    _statusSubscription = null;
    // Release presentation state before asynchronous transport cleanup.
    notifyListeners();
    await statusSubscription?.cancel();
    try {
      await session.stop();
    } catch (_) {
      failure ??= CastFailure.deviceUnreachable;
    }
    if (lease != null) await lease.release();
    _ended.add(CastEnded(
      identity: request.identity,
      position: position,
      failure: failure,
    ));
    notifyListeners();
  }

  @override
  void dispose() {
    unawaited(_end(saveProgress: true));
    unawaited(_ended.close());
    super.dispose();
  }
}

/// Resolves server items to ticket URLs and public shares to their own URL.
CastMediaResolver serverCastMediaResolver(PlayerApiClient Function() client) =>
    (request, {required isVideo}) async {
      if (request is ServerPlaybackRequest) {
        final api = client();
        final ticket = await api.createCastTicket(request.mediaId);
        final compat = (await api.getMedia(request.mediaId)).transcoded;
        return CastMedia(
          mediaId: request.mediaId,
          url: Uri.parse(api.castStreamUrl(ticket, compat: compat)),
          contentType:
              compat ? ticket.compatContentType : ticket.streamContentType,
          title: request.title,
          artist: request.artist,
          album: request.album,
          imageUrl: Uri.parse(api.castThumbnailUrl(ticket)),
          isVideo: isVideo,
        );
      }
      if (request is PublicSharePlaybackRequest) {
        // A share URL needs no credentials; its "view" parameter keeps the
        // receiver's many range requests to one share use.
        return CastMedia(
          url: request.sourceUri,
          contentType: isCompatStreamUrl(request.sourceUri)
              ? (isVideo ? 'video/mp4' : 'audio/mp4')
              : (isVideo ? 'video/mp4' : 'audio/mpeg'),
          title: request.title,
          artist: request.artist,
          album: request.album,
          isVideo: isVideo,
        );
      }
      throw const CastStartException(CastFailure.notCastable);
    };
