import 'dart:async';
import 'package:flutter/services.dart';
import 'package:just_audio/just_audio.dart';

/// A gain effect attached to the existing player's session, independent of its
/// source loading. Unsupported devices must not fail ordinary playback.
abstract class LoudnessEffect {
  bool get enabled;
  void Function()? onSessionChanged;
  Future<void> setEnabled(bool enabled);
  Future<void> setTargetGain(double db);
}

class SessionLoudnessEffect extends LoudnessEffect {
  SessionLoudnessEffect(AudioPlayer player,
      {MethodChannel channel =
          const MethodChannel('zone.foo.player_android/loudness')})
      : _channel = channel {
    _session = player.androidAudioSessionId;
    _sessions = player.androidAudioSessionIdStream.distinct().listen((session) {
      _session = session;
      onSessionChanged?.call();
    });
  }
  final MethodChannel _channel;
  late final StreamSubscription<int?> _sessions;
  int? _session;
  double _gain = 0;
  bool _enabled = false, _attached = false;
  @override
  bool get enabled => _enabled;

  @override
  Future<void> setTargetGain(double db) async {
    _gain = db;
  }

  @override
  Future<void> setEnabled(bool enabled) async {
    _enabled = enabled;
    if (!enabled && !_attached) return;
    // A stopped player has no valid session. Releasing is always safe; the
    // controller reapplies the latest gain when the new session arrives.
    final gain = enabled && (_session ?? 0) > 0 ? _gain : 0.0;
    await _channel.invokeMethod<void>(
        'apply', {'sessionId': _session ?? 0, 'gainMb': (gain * 100).round()});
    _attached = gain > 0;
  }

  Future<void> close() async {
    await _sessions.cancel();
    await setEnabled(false);
  }
}
