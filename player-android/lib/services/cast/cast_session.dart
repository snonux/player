import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/foundation.dart';

import 'cast_device.dart';
import 'cast_protocol.dart';

const _nsConnection = 'urn:x-cast:com.google.cast.tp.connection';
const _nsHeartbeat = 'urn:x-cast:com.google.cast.tp.heartbeat';
const _nsReceiver = 'urn:x-cast:com.google.cast.receiver';
const _nsMedia = 'urn:x-cast:com.google.cast.media';

/// Google's Default Media Receiver: plays a URL with standard controls and
/// needs no receiver app of our own.
const kDefaultMediaReceiverAppId = 'CC1AD845';

const _senderId = 'sender-0';
const _receiverId = 'receiver-0';

/// A byte connection to a cast device; [SecureSocket] in the app, a fake in
/// tests.
abstract interface class CastTransport {
  Stream<List<int>> get input;
  void add(List<int> bytes);
  Future<void> close();
}

typedef CastTransportFactory = Future<CastTransport> Function(
  CastDevice device,
);

/// Connects with TLS. Cast devices present a self-signed certificate that
/// no CA vouches for, so it is accepted for this socket only; the protocol
/// carries no secrets (media URLs hold short-lived tickets).
Future<CastTransport> connectCastSocket(CastDevice device) async {
  final socket = await SecureSocket.connect(
    device.host,
    device.port,
    onBadCertificate: (_) => true,
    timeout: const Duration(seconds: 8),
  );
  return _SocketTransport(socket);
}

class _SocketTransport implements CastTransport {
  _SocketTransport(this._socket);

  final SecureSocket _socket;

  @override
  Stream<List<int>> get input => _socket;

  @override
  void add(List<int> bytes) => _socket.add(bytes);

  @override
  Future<void> close() async {
    try {
      await _socket.close();
    } catch (_) {}
    _socket.destroy();
  }
}

/// What the cast device is doing, from its latest MEDIA_STATUS.
enum CastPlayerState { idle, buffering, playing, paused }

@immutable
class CastMediaStatus {
  const CastMediaStatus({
    required this.playerState,
    required this.position,
    this.duration,
    this.idleReason,
    this.volume,
  });

  static const initial = CastMediaStatus(
    playerState: CastPlayerState.buffering,
    position: Duration.zero,
  );

  final CastPlayerState playerState;
  final Duration position;
  final Duration? duration;

  /// Why the player is idle: FINISHED, CANCELLED, INTERRUPTED or ERROR.
  final String? idleReason;

  /// Receiver volume 0..1, when reported.
  final double? volume;

  bool get finished =>
      playerState == CastPlayerState.idle && idleReason == 'FINISHED';
  bool get failed =>
      playerState == CastPlayerState.idle && idleReason == 'ERROR';

  CastMediaStatus copyWith({double? volume}) => CastMediaStatus(
        playerState: playerState,
        position: position,
        duration: duration,
        idleReason: idleReason,
        volume: volume ?? this.volume,
      );
}

/// What to play on the receiver.
@immutable
class CastMedia {
  const CastMedia({
    required this.url,
    required this.contentType,
    required this.title,
    this.imageUrl,
    this.artist = '',
    this.album = '',
    this.isVideo = false,
  });

  final Uri url;
  final String contentType;
  final String title;
  final Uri? imageUrl;
  final String artist;
  final String album;
  final bool isVideo;
}

class CastException implements Exception {
  const CastException(this.message);

  final String message;

  @override
  String toString() => 'CastException($message)';
}

/// One connection to a cast device running the Default Media Receiver.
///
/// [start] connects, launches the receiver app, and joins it; [load] plays a
/// URL; the transport methods control it. [status] follows the receiver.
/// [disconnect] leaves the receiver playing; [stop] ends it.
class CastSession {
  CastSession._(this.device, this._transport, this._now) {
    _subscription = _transport.input.listen(
      _onData,
      onError: (Object _) => _close(),
      onDone: _close,
      cancelOnError: true,
    );
  }

  /// Seconds between PINGs. The receiver drops a sender that is silent for
  /// about 10 s, and this side gives up after [_heartbeatTimeout].
  static const _heartbeatInterval = Duration(seconds: 5);
  static const _heartbeatTimeout = Duration(seconds: 15);
  static const _requestTimeout = Duration(seconds: 15);

  /// Positions are reported only on changes; while playing this side asks
  /// for a fresh status at this pace so progress saving stays accurate.
  static const _statusPollInterval = Duration(seconds: 5);

  final CastDevice device;
  final CastTransport _transport;
  final DateTime Function() _now;
  late final StreamSubscription<List<int>> _subscription;
  final _decoder = CastFrameDecoder();
  final _pending = <int, _Pending>{};
  final _statusController = StreamController<CastMediaStatus>.broadcast();
  final _closed = Completer<void>();
  bool _closing = false;
  Timer? _heartbeat;
  Timer? _statusPoll;
  DateTime _lastHeard = DateTime.now();
  int _nextRequestId = 1;

  String? _transportId;
  String? _appSessionId;
  int? _mediaSessionId;
  CastMediaStatus _status = CastMediaStatus.initial;
  DateTime _statusAt = DateTime.now();

  /// Connects to [device] and launches the Default Media Receiver on it.
  static Future<CastSession> start(
    CastDevice device, {
    CastTransportFactory connect = connectCastSocket,
    DateTime Function() now = DateTime.now,
  }) async {
    final transport = await connect(device);
    final session = CastSession._(device, transport, now);
    try {
      await session._launch();
    } catch (_) {
      await session._close();
      rethrow;
    }
    return session;
  }

  /// Receiver updates. Completes when the connection ends.
  Stream<CastMediaStatus> get status => _statusController.stream;

  CastMediaStatus get lastStatus => _status;

  /// Completes once the connection to the device is gone.
  Future<void> get closed => _closed.future;

  bool get isClosed => _closing;

  /// The playback position now: the last reported one plus the time since,
  /// while playing.
  Duration get position {
    if (_status.playerState != CastPlayerState.playing) return _status.position;
    final estimate = _status.position + _now().difference(_statusAt);
    final duration = _status.duration;
    return duration != null && estimate > duration ? duration : estimate;
  }

  Future<void> _launch() async {
    _send(_receiverId, _nsConnection, {'type': 'CONNECT'});
    _heartbeat = Timer.periodic(_heartbeatInterval, (_) => _beat());
    final reply = await _request(
        _receiverId,
        _nsReceiver,
        {
          'type': 'LAUNCH',
          'appId': kDefaultMediaReceiverAppId,
        },
        accept: (json) => _launchedApp(json) != null);
    final app = _launchedApp(reply);
    if (app == null) throw const CastException('Receiver did not start');
    _transportId = app['transportId'] as String;
    _appSessionId = app['sessionId'] as String?;
    _send(_transportId!, _nsConnection, {'type': 'CONNECT'});
  }

  /// The Default Media Receiver entry of a RECEIVER_STATUS, if it runs.
  static Map<String, dynamic>? _launchedApp(Map<String, dynamic> json) {
    if (json['type'] == 'LAUNCH_ERROR') {
      throw CastException('Launch failed: ${json['reason'] ?? 'unknown'}');
    }
    if (json['type'] != 'RECEIVER_STATUS') return null;
    final apps = (json['status'] as Map?)?['applications'];
    if (apps is! List) return null;
    for (final app in apps.whereType<Map<String, dynamic>>()) {
      if (app['appId'] == kDefaultMediaReceiverAppId &&
          app['transportId'] is String) {
        return app;
      }
    }
    return null;
  }

  /// Loads [media] and starts it at [position].
  Future<void> load(CastMedia media,
      {Duration position = Duration.zero}) async {
    final metadata = <String, dynamic>{
      // 0 = generic, 3 = music track; the receiver shows title and image.
      'metadataType': media.isVideo ? 0 : 3,
      if (!media.isVideo && media.artist.isNotEmpty) 'artist': media.artist,
      if (!media.isVideo && media.album.isNotEmpty) 'albumName': media.album,
      'title': media.title,
      if (media.imageUrl != null)
        'images': [
          {'url': media.imageUrl.toString()}
        ],
    };
    final reply = await _request(_requireTransport(), _nsMedia, {
      'type': 'LOAD',
      'autoplay': true,
      'currentTime': position.inMilliseconds / 1000.0,
      'media': {
        'contentId': media.url.toString(),
        'contentType': media.contentType,
        'streamType': 'BUFFERED',
        'metadata': metadata,
      },
    });
    if (reply['type'] != 'MEDIA_STATUS') {
      throw CastException('${reply['type'] ?? 'Load failed'}');
    }
    _statusPoll ??= Timer.periodic(_statusPollInterval, (_) {
      if (_status.playerState == CastPlayerState.playing) {
        _send(_requireTransport(), _nsMedia, {
          'type': 'GET_STATUS',
          'requestId': _nextRequestId++,
        });
      }
    });
  }

  Future<void> play() => _mediaCommand({'type': 'PLAY'});

  Future<void> pause() => _mediaCommand({'type': 'PAUSE'});

  Future<void> seek(Duration position) => _mediaCommand({
        'type': 'SEEK',
        'currentTime': position.inMilliseconds / 1000.0,
      });

  /// Sets the receiver's volume, 0..1.
  Future<void> setVolume(double level) async {
    final clamped = level.clamp(0.0, 1.0);
    _publish(_status.copyWith(volume: clamped), keepTime: true);
    _send(_receiverId, _nsReceiver, {
      'type': 'SET_VOLUME',
      'volume': {'level': clamped},
      'requestId': _nextRequestId++,
    });
  }

  /// Ends playback on the device and closes the connection.
  Future<void> stop() async {
    if (isClosed) return;
    final appSession = _appSessionId;
    if (appSession != null) {
      _send(_receiverId, _nsReceiver, {
        'type': 'STOP',
        'sessionId': appSession,
        'requestId': _nextRequestId++,
      });
    }
    await _close();
  }

  /// Closes the connection; the device keeps playing.
  Future<void> disconnect() async {
    if (isClosed) return;
    final transportId = _transportId;
    if (transportId != null) {
      _send(transportId, _nsConnection, {'type': 'CLOSE'});
    }
    await _close();
  }

  Future<void> _mediaCommand(Map<String, dynamic> command) async {
    final mediaSession = _mediaSessionId;
    if (mediaSession == null) throw const CastException('Nothing is loaded');
    await _request(_requireTransport(), _nsMedia,
        {...command, 'mediaSessionId': mediaSession});
  }

  String _requireTransport() {
    final id = _transportId;
    if (id == null || isClosed) throw const CastException('Not connected');
    return id;
  }

  Future<Map<String, dynamic>> _request(
    String destination,
    String namespace,
    Map<String, dynamic> payload, {
    bool Function(Map<String, dynamic>)? accept,
  }) {
    if (isClosed) return Future.error(const CastException('Not connected'));
    final id = _nextRequestId++;
    final pending = _Pending(accept);
    _pending[id] = pending;
    _send(destination, namespace, {...payload, 'requestId': id});
    return pending.completer.future.timeout(_requestTimeout, onTimeout: () {
      throw const CastException('The cast device did not answer');
    }).whenComplete(() => _pending.remove(id));
  }

  void _send(
      String destination, String namespace, Map<String, dynamic> payload) {
    if (isClosed) return;
    _transport.add(encodeCastFrame(CastMessage(
      sourceId: _senderId,
      destinationId: destination,
      namespace: namespace,
      payload: jsonEncode(payload),
    )));
  }

  void _beat() {
    if (DateTime.now().difference(_lastHeard) > _heartbeatTimeout) {
      unawaited(_close());
      return;
    }
    _send(_receiverId, _nsHeartbeat, {'type': 'PING'});
  }

  void _onData(List<int> chunk) {
    _lastHeard = DateTime.now();
    final List<CastMessage> messages;
    try {
      messages = _decoder.add(chunk);
    } on FormatException {
      unawaited(_close());
      return;
    }
    for (final message in messages) {
      _handle(message);
    }
  }

  void _handle(CastMessage message) {
    final json = message.json;
    if (json == null) return;
    final type = json['type'];
    if (message.namespace == _nsHeartbeat) {
      if (type == 'PING') {
        _send(message.sourceId, _nsHeartbeat, {'type': 'PONG'});
      }
      return;
    }
    if (message.namespace == _nsConnection && type == 'CLOSE') {
      unawaited(_close());
      return;
    }
    if (message.namespace == _nsReceiver && type == 'RECEIVER_STATUS') {
      _onReceiverStatus(json);
    }
    if (message.namespace == _nsMedia && type == 'MEDIA_STATUS') {
      _onMediaStatus(json);
    }
    final requestId = json['requestId'];
    final answered = requestId is int ? _pending[requestId] : null;
    if (answered != null && answered.accept == null) {
      answered.offer(json);
      return;
    }
    // A request with [accept] (LAUNCH) completes on the first status that
    // passes it, which may come as a broadcast without its requestId.
    for (final pending in List.of(_pending.values)) {
      if (pending.accept != null) pending.offer(json);
    }
  }

  void _onReceiverStatus(Map<String, dynamic> json) {
    final status = json['status'];
    if (status is! Map) return;
    final level = (status['volume'] as Map?)?['level'];
    if (level is num) {
      _publish(_status.copyWith(volume: level.toDouble()), keepTime: true);
    }
    // Another sender or the remote ended our receiver app.
    final apps = status['applications'];
    final ours = _appSessionId;
    if (ours != null &&
        (apps is! List ||
            !apps.any((app) => app is Map && app['sessionId'] == ours))) {
      unawaited(_close());
    }
  }

  void _onMediaStatus(Map<String, dynamic> json) {
    final entries = json['status'];
    if (entries is! List || entries.isEmpty) return;
    final entry = entries.first;
    if (entry is! Map) return;
    final mediaSession = entry['mediaSessionId'];
    if (mediaSession is int) _mediaSessionId = mediaSession;
    final duration = (entry['media'] as Map?)?['duration'];
    final volume = (entry['volume'] as Map?)?['level'];
    _publish(CastMediaStatus(
      playerState: switch (entry['playerState']) {
        'PLAYING' => CastPlayerState.playing,
        'PAUSED' => CastPlayerState.paused,
        'BUFFERING' || 'LOADING' => CastPlayerState.buffering,
        _ => CastPlayerState.idle,
      },
      position: _seconds(entry['currentTime']) ?? _status.position,
      duration: duration is num ? _seconds(duration) : _status.duration,
      idleReason: entry['idleReason'] as String?,
      volume: volume is num ? volume.toDouble() : _status.volume,
    ));
  }

  static Duration? _seconds(Object? value) => value is num && value.isFinite
      ? Duration(milliseconds: (value * 1000).round())
      : null;

  void _publish(CastMediaStatus status, {bool keepTime = false}) {
    if (!keepTime) _statusAt = _now();
    _status = status;
    if (!_statusController.isClosed) _statusController.add(status);
  }

  Future<void> _close() async {
    if (_closing) return;
    _closing = true;
    _heartbeat?.cancel();
    _statusPoll?.cancel();
    for (final pending in _pending.values) {
      pending.fail(const CastException('Connection closed'));
    }
    _pending.clear();
    await _subscription.cancel();
    await _transport.close();
    await _statusController.close();
    _closed.complete();
  }
}

/// A request waiting for its answer. With [accept], only a message it
/// accepts answers it; [accept] may throw to fail the request.
class _Pending {
  _Pending(this.accept);

  final bool Function(Map<String, dynamic>)? accept;
  final completer = Completer<Map<String, dynamic>>();

  void offer(Map<String, dynamic> json) {
    if (completer.isCompleted) return;
    try {
      if (accept?.call(json) ?? true) completer.complete(json);
    } catch (error) {
      completer.completeError(error);
    }
  }

  void fail(Object error) {
    if (!completer.isCompleted) completer.completeError(error);
  }
}
