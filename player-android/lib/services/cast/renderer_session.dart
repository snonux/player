import 'dart:async';
import 'dart:math';

import 'package:dio/dio.dart';

import '../../api/player_api_client.dart';
import 'cast_device.dart';
import 'cast_session.dart';

/// DLNA uses the authenticated Player server as its LAN control point.
class RendererDiscovery implements CastDiscovery {
  RendererDiscovery(this.client, this.cast);
  final PlayerApiClient Function() client;
  final CastDiscovery cast;

  @override
  Stream<List<CastDevice>> devices() {
    late StreamController<List<CastDevice>> output;
    StreamSubscription<List<CastDevice>>? subscription;
    var google = <CastDevice>[];
    var renderers = <CastDevice>[];
    var cancelled = false;
    void publish() {
      if (!cancelled) output.add([...google, ...renderers]);
    }

    Future<void> search() async {
      try {
        final response =
            await client().rawDio.get<List<dynamic>>('/api/v1/renderers');
        renderers = (response.data ?? []).map((item) {
          final data = item as Map<String, dynamic>;
          return CastDevice(
              id: data['id'] as String,
              name: data['name'] as String,
              host: '',
              port: 0,
              model: 'DLNA / UPnP · server network',
              isRenderer: true);
        }).toList();
        publish();
      } catch (error, stack) {
        if (!cancelled && google.isEmpty) output.addError(error, stack);
      }
    }

    output = StreamController<List<CastDevice>>(
      onListen: () {
        subscription = cast.devices().listen((devices) {
          google = devices;
          publish();
        }, onError: (Object _) {});
        unawaited(search());
      },
      onCancel: () async {
        cancelled = true;
        await subscription?.cancel();
      },
    );
    return output.stream;
  }
}

/// One server-owned renderer; only media IDs and commands cross this API.
class RendererSession implements RemotePlaybackSession {
  RendererSession(this.device, this.api) : _sessionID = _newSessionID();
  static String _newSessionID() {
    final random = Random.secure();
    return List.generate(
            16, (_) => random.nextInt(256).toRadixString(16).padLeft(2, '0'))
        .join();
  }

  final String _sessionID;
  int _revision = 0;
  @override
  final CastDevice device;
  final PlayerApiClient api;
  final _updates = StreamController<CastMediaStatus>.broadcast();
  final _closed = Completer<void>();
  CastMediaStatus _status = CastMediaStatus.initial;
  Timer? _timer;
  bool _polling = false;
  bool _stopped = false;
  bool _hasPlayed = false;
  int _failures = 0;
  DateTime _loadedAt = DateTime.now();

  @override
  Stream<CastMediaStatus> get status => _updates.stream;
  @override
  CastMediaStatus get lastStatus => _status;
  @override
  Future<void> get closed => _closed.future;
  @override
  Duration get position => _status.position;

  Future<Map<String, dynamic>> _command(String action,
      {int? mediaId, Duration? at}) async {
    final response = await api.rawDio.post<dynamic>('/api/v1/renderers/control',
        options: action == 'stop'
            ? Options(
                validateStatus: (status) => status != null && status < 600,
                receiveTimeout: const Duration(seconds: 6),
                sendTimeout: const Duration(seconds: 6))
            : null,
        data: {
          'device_id': device.id,
          'session_id': _sessionID,
          'revision': _revision,
          'action': action,
          if (mediaId != null) 'media_id': mediaId,
          if (at != null) 'position': at.inMilliseconds / 1000,
        });
    if ((response.statusCode ?? 500) >= 400) {
      throw const CastException(
          'Speaker could not be stopped. It may still be playing.');
    }
    final data = response.data;
    if (data is! Map<String, dynamic>) {
      throw const CastException('Invalid speaker response.');
    }
    return data;
  }

  @override
  Future<void> load(CastMedia media,
      {Duration position = Duration.zero}) async {
    final id = media.mediaId;
    if (id == null) {
      throw const CastException(
          'Choose an authenticated server track for DLNA.');
    }
    if (_stopped) throw const CastException('Renderer session ended.');
    final generation = ++_revision;
    final loaded = await _command('load', mediaId: id, at: position);
    if (_stopped || generation != _revision) return;
    _hasPlayed = false;
    _loadedAt = DateTime.now();
    _status = CastMediaStatus(
        playerState: CastPlayerState.buffering,
        position: position,
        duration: Duration(
            milliseconds:
                (((loaded['duration'] as num?) ?? 0) * 1000).round()));
    _timer?.cancel();
    _timer = Timer.periodic(
        const Duration(milliseconds: 1500), (_) => unawaited(_poll()));
    await _poll();
  }

  Future<void> _poll() async {
    if (_polling || _stopped) return;
    _polling = true;
    final generation = _revision;
    try {
      final data = await _command('status');
      if (_stopped || generation != _revision) return;
      _failures = 0;
      final at =
          Duration(milliseconds: ((data['position'] as num) * 1000).round());
      final reported =
          Duration(milliseconds: ((data['duration'] as num) * 1000).round());
      final length = reported > Duration.zero
          ? reported
          : (_status.duration ?? Duration.zero);
      final state = data['state'];
      if (state == 'PLAYING') _hasPlayed = true;
      final finished = state == 'STOPPED' &&
          _hasPlayed &&
          length > Duration.zero &&
          (at > position ? at : position) >=
              length - const Duration(seconds: 3);
      _status = CastMediaStatus(
        playerState: switch (state) {
          'PLAYING' => CastPlayerState.playing,
          'PAUSED_PLAYBACK' => CastPlayerState.paused,
          'TRANSITIONING' => CastPlayerState.buffering,
          _ => CastPlayerState.idle,
        },
        position: at,
        duration: length,
        idleReason: finished
            ? 'FINISHED'
            : (state == 'STOPPED' &&
                    (_hasPlayed ||
                        DateTime.now().difference(_loadedAt).inSeconds > 15)
                ? 'ERROR'
                : null),
      );
      _updates.add(_status);
    } catch (_) {
      if (!_stopped && generation == _revision && ++_failures >= 3) {
        _status = CastMediaStatus(
            playerState: CastPlayerState.idle,
            position: position,
            duration: _status.duration,
            idleReason: 'ERROR');
        _updates.add(_status);
        _timer?.cancel();
      }
    } finally {
      _polling = false;
    }
  }

  @override
  Future<void> play() async {
    await _command('play');
    await _poll();
  }

  @override
  Future<void> pause() async {
    await _command('pause');
    await _poll();
  }

  @override
  Future<void> seek(Duration position) async {
    await _command('seek', at: position);
    await _poll();
  }

  @override
  Future<void> setVolume(double level) async {}
  @override
  Future<void> stop() async {
    if (_stopped) return;
    _stopped = true;
    ++_revision;
    _timer?.cancel();
    try {
      await _command('stop');
    } finally {
      if (!_closed.isCompleted) _closed.complete();
      await _updates.close();
    }
  }
}
