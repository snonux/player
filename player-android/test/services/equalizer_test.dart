import 'dart:async';
import 'dart:convert';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:just_audio/just_audio.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:player_android/services/equalizer.dart';

class _Player extends AudioPlayer {
  int? session = 11;
  final sessions = StreamController<int?>.broadcast(sync: true);
  @override
  int? get androidAudioSessionId => session;
  @override
  Stream<int?> get androidAudioSessionIdStream => sessions.stream;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test('safe defaults, validation and subtractive headroom', () {
    expect(EqualizerSettings.fromJson({}).enabled, false);
    expect(
        EqualizerSettings.fromJson({
          'bands': [-99, 99, 'bad']
        }).bands,
        [-12, 12, 0, 0, 0]);
    expect(
        EqualizerSettings(enabled: true, bands: equalizerPresets['Bass boost']!)
            .effectiveBands,
        [0, -3, -6, -8, -10]);
    expect(EqualizerSettings(bands: equalizerPresets['Voice']!).effectiveBands,
        [0, 0, 0, 0, 0]);
  });
  test(
      'session replacement, bypass, reset, ordered persistence and unavailable retry',
      () async {
    SharedPreferences.setMockInitialValues({});
    const channel = MethodChannel('test/equalizer');
    final calls = <Map<dynamic, dynamic>>[];
    var fail = false;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
      if (fail) throw PlatformException(code: 'equalizer_unavailable');
      calls.add(call.arguments as Map);
      return null;
    });
    addTearDown(() => TestDefaultBinaryMessengerBinding
        .instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null));
    final player = _Player();
    addTearDown(player.sessions.close);
    final controller = EqualizerController(player, channel: channel);
    await controller.restore();
    expect(calls.last['enabled'], false);
    await controller.update(EqualizerSettings(
        enabled: true, bands: equalizerPresets['Bass boost']!));
    expect(calls.last['bands'], [0, -3, -6, -8, -10]);
    player.session = 22;
    player.sessions.add(22);
    await controller.apply();
    expect(calls.last['sessionId'], 22);
    final first = controller.update(
        EqualizerSettings(enabled: true, bands: equalizerPresets['Voice']!));
    final last = controller.update(const EqualizerSettings());
    await Future.wait([first, last]);
    final saved = jsonDecode((await SharedPreferences.getInstance())
        .getString(EqualizerController.preferenceKey)!);
    expect(saved['enabled'], false);
    expect(calls.last['enabled'], false);
    fail = true;
    await controller.update(const EqualizerSettings(enabled: true));
    expect(controller.status, contains('unavailable'));
    fail = false;
    await controller.apply();
    expect(controller.status, contains('active'));
    player.session = null;
    player.sessions.add(null);
    await controller.apply();
    expect(controller.status, contains('Start local playback'));
  });
}
