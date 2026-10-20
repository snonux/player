import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/models/audio_format.dart';
import 'package:player_android/widgets/audio_format_line.dart';

void main() {
  test('summary matches the web player wording', () {
    expect(
        AudioFormat.fromJson({
          'codec_label': 'FLAC',
          'bit_depth': 24,
          'sample_rate': 96000,
          'bitrate': 2304000,
          'channels': 2,
          'lossless': true,
        }).summary,
        'FLAC · 24-bit · 96 kHz · 2304 kbps · Stereo');
    expect(
        AudioFormat.fromJson({
          'codec_label': 'MP3',
          'sample_rate': 44100,
          'bitrate': 320000,
          'channels': 1,
        }).summary,
        'MP3 · 44.1 kHz · 320 kbps · Mono');
    expect(
        AudioFormat.fromJson({
          'codec_label': 'AC-3',
          'sample_rate': 48000,
          'channels': 6,
          'channel_layout': '5.1(side)',
        }).summary,
        'AC-3 · 48 kHz · 5.1');
    expect(
        AudioFormat.fromJson(
            {'codec_label': 'WMA', 'channels': 4, 'transcoded': true}).summary,
        'WMA · 4 ch (plays as AAC)');
  });

  test('invalid and empty payloads give no summary', () {
    expect(AudioFormat.fromJson(null).summary, isEmpty);
    expect(AudioFormat.fromJson({'lossless': false}).summary, isEmpty);
    expect(
        AudioFormat.fromJson({
          'codec_label': 'X',
          'bitrate': -1,
          'sample_rate': '44100',
          'bit_depth': 1.5,
        }).summary,
        'X');
  });

  testWidgets('line shows the summary and hides failures', (tester) async {
    await tester.pumpWidget(MaterialApp(
        home: Column(children: [
      AudioFormatLine(
          load: () async => AudioFormat.fromJson(
              {'codec_label': 'Opus', 'sample_rate': 48000, 'channels': 2})),
      AudioFormatLine(load: () async => throw Exception('offline')),
    ])));
    await tester.pump();
    expect(find.text('Opus · 48 kHz · Stereo'), findsOneWidget);
    expect(find.byKey(const Key('audio_player_format')), findsOneWidget);
  });
}
