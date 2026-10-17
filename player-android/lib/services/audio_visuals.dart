import 'dart:math' as math;

import 'package:flutter/services.dart';

enum AudioVisualMode {
  bars('Spectrum'),
  wave('Waveform'),
  radial('Radial'),
  mirror('Mirror'),
  orbit('Orbit'),
  off('Off');

  const AudioVisualMode(this.label);
  final String label;
}

bool isAudiobook(String name) =>
    RegExp(r'\.m4b($|[?#\s])', caseSensitive: false).hasMatch(name) ||
    RegExp(r'(^|[^a-z])audio[\s_-]*books?([^a-z]|$)', caseSensitive: false)
        .hasMatch(name);

class AudioVisualFrame {
  const AudioVisualFrame(this.waveform, this.spectrum);
  final List<double> waveform;
  final List<double> spectrum;

  factory AudioVisualFrame.fromEvent(dynamic event) {
    final data = event as Map;
    final wave = data['waveform'] as Uint8List;
    final fft = data['fft'] as Uint8List;
    double signed(int index) =>
        fft[index] < 128 ? fft[index].toDouble() : fft[index] - 256.0;
    final bins = List<double>.filled(fft.length ~/ 2, 0);
    // Android packs signed real/imaginary pairs, with DC and Nyquist in
    // the first pair. Skip those for the music spectrum.
    for (var i = 1; i < bins.length; i++) {
      final re = signed(i * 2);
      final im = signed(i * 2 + 1);
      bins[i] = math.log(1 + math.sqrt(re * re + im * im)) / math.log(182);
    }
    final spectrum = List<double>.generate(48, (band) {
      final start = band * bins.length ~/ 48;
      final end = math.max(start + 1, (band + 1) * bins.length ~/ 48);
      var peak = 0.0;
      for (var i = start; i < math.min(end, bins.length); i++) {
        peak = math.max(peak, bins[i]);
      }
      return peak.clamp(0.0, 1.0);
    });
    return AudioVisualFrame(
        wave.map((sample) => (sample - 128) / 128.0).toList(), spectrum);
  }
}

class AudioVisualsService {
  const AudioVisualsService();
  static const _methods =
      MethodChannel('zone.foo.player_android/audio_visuals');
  static const _events =
      EventChannel('zone.foo.player_android/audio_visual_frames');

  Future<bool> permission({bool request = false}) async =>
      await _methods.invokeMethod<bool>('permission', {'request': request}) ??
      false;

  Stream<AudioVisualFrame> frames(int sessionId) =>
      _events.receiveBroadcastStream({'sessionId': sessionId}).map(
          AudioVisualFrame.fromEvent);
}
