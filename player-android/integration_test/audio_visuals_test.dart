import 'dart:async';
import 'dart:io';
import 'dart:math' as math;
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:just_audio/just_audio.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:player_android/services/audio_visuals.dart';
import 'package:player_android/widgets/audio_visuals.dart';

// Grant RECORD_AUDIO to this disposable emulator app when the test reports
// it is waiting. This exercises Android Visualizer, ExoPlayer and Flutter
// together, with synthetic music and no server/account/microphone input.
Future<void> waitFor(WidgetTester tester, bool Function() ready) async {
  final deadline = DateTime.now().add(const Duration(seconds: 15));
  while (!ready()) {
    if (DateTime.now().isAfter(deadline)) {
      fail('Timed out waiting for native music visuals');
    }
    await tester.pump(const Duration(milliseconds: 100));
  }
}

AudioVisualPainter? painter(WidgetTester tester) => tester
    .widgetList<CustomPaint>(find.byType(CustomPaint))
    .map((widget) => widget.painter)
    .whereType<AudioVisualPainter>()
    .firstOrNull;

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();
  testWidgets(
      'native playback produces all five visuals, survives Off, pause and track replacement',
      (tester) async {
    const service = AudioVisualsService();
    // Flutter's integration runner may reinstall the app and clear grants.
    // Give the host time to grant permission after that installation.
    debugPrint('Waiting for emulator RECORD_AUDIO permission');
    final permissionDeadline = DateTime.now().add(const Duration(seconds: 45));
    while (!await service.permission() &&
        DateTime.now().isBefore(permissionDeadline)) {
      await Future<void>.delayed(const Duration(milliseconds: 200));
    }
    expect(await service.permission(), isTrue,
        reason: 'Grant RECORD_AUDIO to the emulator app before this test.');
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString('player-audio-visual', 'off');
    final folder = await Directory.systemTemp.createTemp('player-visuals-');
    final wav = ByteData(44 + 44100 * 60 * 2);
    void text(int offset, String value) {
      for (var i = 0; i < value.length; i++) {
        wav.setUint8(offset + i, value.codeUnitAt(i));
      }
    }

    text(0, 'RIFF');
    wav.setUint32(4, wav.lengthInBytes - 8, Endian.little);
    text(8, 'WAVEfmt ');
    wav.setUint32(16, 16, Endian.little);
    wav.setUint16(20, 1, Endian.little);
    wav.setUint16(22, 1, Endian.little);
    wav.setUint32(24, 44100, Endian.little);
    wav.setUint32(28, 88200, Endian.little);
    wav.setUint16(32, 2, Endian.little);
    wav.setUint16(34, 16, Endian.little);
    text(36, 'data');
    wav.setUint32(40, wav.lengthInBytes - 44, Endian.little);
    for (var i = 0; i < 44100 * 60; i++) {
      final signal = math.sin(i * 2 * math.pi * 440 / 44100) * .6 +
          math.sin(i * 2 * math.pi * 1400 / 44100) * .3;
      wav.setInt16(44 + i * 2, (signal * 30000).round(), Endian.little);
    }
    final file = await File('${folder.path}/song.wav')
        .writeAsBytes(wav.buffer.asUint8List());
    final player = AudioPlayer();
    try {
      await player.setFilePath(file.path);
      unawaited(player.play());
      await tester.pumpWidget(MaterialApp(
          home: Scaffold(
              body: Padding(
                  padding: const EdgeInsets.all(24),
                  child: AudioVisuals(
                      player: player,
                      title: 'song.wav',
                      artwork: const Text('Cover artwork'))))));
      await waitFor(tester, () => player.position > Duration.zero);
      expect(player.androidAudioSessionId, greaterThan(0));
      for (final mode in AudioVisualMode.values
          .where((value) => value != AudioVisualMode.off)) {
        await tester.tap(find.byKey(const Key('audio_visual_mode')));
        await tester.pump(const Duration(milliseconds: 400));
        await tester.tap(find.text(mode.label).last);
        await waitFor(tester, () {
          final visual = painter(tester);
          return visual?.mode == mode &&
              visual!.frame.waveform.any((sample) => sample.abs() > .01) &&
              visual.frame.spectrum.any((value) => value > .01);
        });
        final frame = painter(tester)!.frame;
        expect(frame.waveform.any((sample) => sample.abs() > .01), isTrue);
        expect(frame.spectrum.any((value) => value > .01), isTrue);
        debugPrint('Native visual verified: ${mode.label}');
      }
      await player.pause();
      await tester.pump(const Duration(milliseconds: 300));
      final frozen = painter(tester)!.frame;
      await tester.pump(const Duration(milliseconds: 300));
      expect(painter(tester)!.frame, same(frozen));
      unawaited(player.play());
      await waitFor(tester, () => painter(tester)?.frame != frozen);
      await tester.tap(find.byKey(const Key('audio_visual_mode')));
      await tester.pump(const Duration(milliseconds: 400));
      await tester.tap(find.text('Off').last);
      await waitFor(
          tester, () => find.text('Cover artwork').evaluate().isNotEmpty);
      final before = player.position;
      await tester.pump(const Duration(milliseconds: 500));
      expect(player.position, greaterThan(before));
      await player.setFilePath(file.path);
      unawaited(player.play());
      await tester.tap(find.byKey(const Key('audio_visual_mode')));
      await tester.pump(const Duration(milliseconds: 400));
      await tester.tap(find.text('Spectrum').last);
      await waitFor(
          tester, () => painter(tester)?.mode == AudioVisualMode.bars);
      expect(player.playing, isTrue);
      await tester.pumpWidget(const SizedBox());
    } finally {
      await player.dispose();
      await folder.delete(recursive: true);
    }
  });
}
