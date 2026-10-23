import 'dart:async';
import 'dart:convert';
import 'dart:math' as math;
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:just_audio/just_audio.dart';
import 'package:shared_preferences/shared_preferences.dart';

const equalizerFrequencies = [60, 230, 910, 3600, 14000];
const equalizerPresets = <String, List<double>>{
  'Flat': [0, 0, 0, 0, 0],
  'Bass boost': [6, 3, 0, -2, -4],
  'Voice': [-4, -2, 4, 3, -2],
};

class EqualizerSettings {
  final bool enabled;
  final List<double> bands;
  const EqualizerSettings(
      {this.enabled = false, this.bands = const [0, 0, 0, 0, 0]});
  factory EqualizerSettings.fromJson(Map<String, dynamic> json) {
    final values = json['bands'];
    return EqualizerSettings(
        enabled: json['enabled'] == true,
        bands: List.generate(5, (i) {
          final n = values is List && i < values.length ? values[i] : null;
          return n is num && n.isFinite ? n.toDouble().clamp(-12, 12) : 0;
        }));
  }
  Map<String, dynamic> toJson() => {'enabled': enabled, 'bands': bands};
  List<double> get effectiveBands {
    final headroom = bands.fold<double>(0, math.max);
    return bands.map((n) => enabled ? n - headroom : 0.0).toList();
  }
}

/// Owns the device preference and the effect for this player's audio session.
/// Engine-owned native effects remain attached while Now Playing is closed.
class EqualizerController extends ChangeNotifier {
  EqualizerController(this.player,
      {MethodChannel channel =
          const MethodChannel('zone.foo.player_android/equalizer')})
      : _channel = channel {
    player.androidAudioSessionIdStream.distinct().listen((_) => apply());
  }
  final AudioPlayer player;
  final MethodChannel _channel;
  EqualizerSettings settings = const EqualizerSettings();
  String status = 'Equalizer bypassed.';
  Future<void> _tail = Future.value();
  static const preferenceKey = 'player-equalizer-v1';
  Future<void> restore() async {
    try {
      final raw =
          (await SharedPreferences.getInstance()).getString(preferenceKey);
      if (raw != null) {
        settings =
            EqualizerSettings.fromJson(jsonDecode(raw) as Map<String, dynamic>);
      }
    } catch (_) {/* Invalid preferences keep bypass active. */}
    await apply();
  }

  Future<void> update(EqualizerSettings value) {
    settings = EqualizerSettings.fromJson(value.toJson());
    // Serialize effect and persistence together, so rapid slider events cannot
    // persist an older curve after the final one.
    final snapshot = settings;
    return _enqueue(() async {
      await _apply();
      try {
        if (!await (await SharedPreferences.getInstance())
            .setString(preferenceKey, jsonEncode(snapshot.toJson()))) {
          throw StateError('save failed');
        }
      } catch (_) {
        status += ' Could not save equalizer on this device.';
        notifyListeners();
      }
    });
  }

  Future<void> _enqueue(Future<void> Function() action) {
    _tail = _tail.then((_) => action());
    return _tail;
  }

  Future<void> apply() => _enqueue(_apply);
  Future<void> _apply() async {
    final session = player.androidAudioSessionId ?? 0;
    try {
      await _channel.invokeMethod<void>('apply', {
        'sessionId': session,
        'enabled': settings.enabled,
        'bands': settings.effectiveBands,
      });
      status = !settings.enabled
          ? 'Equalizer bypassed.'
          : session <= 0
              ? 'Start local playback to use the equalizer.'
              : 'Equalizer active. Boost headroom reserved.';
    } catch (_) {
      status =
          'Equalizer unavailable on this device. Playback continues. Try again after starting local audio.';
    }
    notifyListeners();
  }
}
