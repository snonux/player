import 'dart:async';

import 'package:flutter/foundation.dart';

import '../../api/player_api_client.dart';
import '../playback_request.dart';
import '../playback_session_coordinator.dart';
import 'cast_device.dart';
import 'cast_session.dart';

/// Builds what the receiver plays for a request. Separate from
/// [CastController] so the server lookup can be faked in tests.
typedef CastMediaResolver = Future<CastMedia> Function(
  PlaybackRequest request, {
  required bool isVideo,
});

typedef CastSessionStarter = Future<CastSession> Function(CastDevice device);

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
        'Could not connect to the cast device. Check that it is on the same '
            'network as this phone.',
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
    required PlaybackSessionCoordinator coordinator,
    required CastMediaResolver resolveMedia,
    CastSessionStarter? startSession,
    this.progressInterval = const Duration(seconds: 10),
  })  : _coordinator = coordinator,
        _resolveMedia = resolveMedia,
        _startSession = startSession ?? ((device) => CastSession.start(device));

  final PlaybackSessionCoordinator _coordinator;
  final CastMediaResolver _resolveMedia;
  final CastSessionStarter _startSession;
  final Duration progressInterval;

  CastSession? _session;
  PlaybackRequest? _request;
  PlaybackSessionLease? _lease;
  StreamSubscription<CastMediaStatus>? _statusSubscription;
  Timer? _progressTimer;
  bool _finished = false;
  bool _starting = false;
  Duration? _lastSaved;
  final _ended = StreamController<CastEnded>.broadcast();

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
  }) async {
    if (!isCastable(request)) {
      throw const CastStartException(CastFailure.notCastable);
    }
    if (_starting) return;
    _starting = true;
    notifyListeners();
    try {
      await _end(saveProgress: true);
      final CastMedia media;
      try {
        media = await _resolveMedia(request, isVideo: isVideo);
      } catch (_) {
        throw const CastStartException(CastFailure.serverRefused);
      }
      final CastSession session;
      try {
        session = await _startSession(device);
      } catch (_) {
        throw const CastStartException(CastFailure.deviceUnreachable);
      }
      try {
        await session.load(media, position: position);
      } catch (_) {
        await session.stop();
        throw const CastStartException(CastFailure.loadFailed);
      }
      // Claimed only now: until the TV plays, the phone keeps playing.
      final lease = await _coordinator.claim(
        kind: request.kind,
        identity: request.identity,
        stop: () => _end(saveProgress: true),
      );
      if (lease == null) {
        await session.stop();
        return;
      }
      _attach(session, request, lease);
    } finally {
      _starting = false;
      notifyListeners();
    }
  }

  void _attach(
    CastSession session,
    PlaybackRequest request,
    PlaybackSessionLease lease,
  ) {
    _session = session;
    _request = request;
    _lease = lease;
    _finished = false;
    _lastSaved = null;
    _statusSubscription = session.status.listen((status) {
      if (status.finished) {
        _finished = true;
        unawaited(_finish(request));
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

  Future<void> play() async => _session?.play();

  Future<void> pause() async => _session?.pause();

  Future<void> seek(Duration position) async => _session?.seek(position);

  Future<void> setVolume(double level) async => _session?.setVolume(level);

  /// Ends the cast and the receiver's playback. Returns where it was, for
  /// the phone to continue from.
  Future<Duration?> stop() async {
    final position = _finished ? null : _session?.position;
    await _end(saveProgress: true);
    return position;
  }

  Future<void> _finish(PlaybackRequest request) async {
    try {
      await request.markFinished?.call();
    } catch (_) {}
    await _end(saveProgress: false);
  }

  void _saveProgress() {
    final session = _session;
    final save = _request?.savePosition;
    if (session == null || save == null || _finished) return;
    if (session.lastStatus.playerState == CastPlayerState.idle) return;
    final position = session.position;
    if (position <= Duration.zero || position == _lastSaved) return;
    _lastSaved = position;
    unawaited(save(position.inMilliseconds / 1000.0).catchError((_) {}));
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
    await _statusSubscription?.cancel();
    _statusSubscription = null;
    await session.stop();
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
        final compat = isCompatStreamUrl(request.sourceUri);
        return CastMedia(
          url: Uri.parse(api.castStreamUrl(ticket, compat: compat)),
          contentType:
              compat ? ticket.compatContentType : ticket.streamContentType,
          title: request.title,
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
          isVideo: isVideo,
        );
      }
      throw const CastStartException(CastFailure.notCastable);
    };
