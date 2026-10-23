import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/models/media.dart';
import 'package:player_android/models/music_metadata.dart';
import 'package:player_android/widgets/music_info_dialog.dart';
import 'package:player_android/widgets/music_metadata_details.dart';

void main() {
  test('audio numbers keep zero gains and snapshots and omit invalid values',
      () {
    final music = MusicMetadata.fromJson({
      'bpm': 123.5,
      'sample_rate': 48000,
      'bit_depth': 24,
      'channels': 2,
      'replaygain_track_gain_db': 0,
      'replaygain_album_gain_db': -6.25,
      'replaygain_track_peak': 0.89
    });
    expect(music.details['Tempo'], '123.5 BPM (beats/min)');
    final rows = Map.fromEntries(music.audioDetails);
    expect(rows['Sample rate'], '48 kHz (48000 Hz)');
    expect(rows['Bit depth'], '24 bits');
    expect(rows['Channels'], '2 (stereo)');
    expect(rows['Track ReplayGain'], '0 dB');
    expect(rows['Album ReplayGain'], '-6.25 dB');
    expect(rows['Track peak'], '0.89 (1 = full scale)');
    expect(Map.fromEntries(MusicMetadata.fromJson(music.toJson()).audioDetails),
        Map.fromEntries(music.audioDetails));
    final invalid = MusicMetadata.fromJson({
      'bpm': 0,
      'sample_rate': -1,
      'bit_depth': 0,
      'channels': 1.5,
      'replaygain_track_gain_db': double.infinity,
      'replaygain_track_peak': -1
    });
    expect(invalid.audioDetails, isEmpty);
    expect(invalid.details.containsKey('Tempo'), false);
  });

  testWidgets('advanced audio starts collapsed and stays selectable',
      (tester) async {
    final music = MusicMetadata.fromJson(
        {'sample_rate': 44100, 'channels': 1, 'replaygain_track_gain_db': 0});
    await tester.pumpWidget(MaterialApp(
        home: Scaffold(
            body: SingleChildScrollView(
                child: MusicMetadataDetails(music: music)))));
    expect(find.text('Advanced audio'), findsOneWidget);
    expect(find.text('Track ReplayGain: 0 dB'), findsNothing);
    await tester.tap(find.text('Advanced audio'));
    await tester.pumpAndSettle();
    expect(find.text('Sample rate: 44.1 kHz (44100 Hz)'), findsOneWidget);
    expect(find.text('Channels: 1 (mono)'), findsOneWidget);
    expect(find.textContaining('Bit depth:'), findsNothing);
    expect(find.byType(SelectableText), findsWidgets);
  });

  testWidgets('loading failure Retry success Close leave opener available',
      (tester) async {
    var calls = 0;
    final pending = Completer<Media>();
    await tester.pumpWidget(MaterialApp(
        home: Builder(
            builder: (context) => Scaffold(
                body: TextButton(
                    onPressed: () => showDialog<void>(
                        context: context,
                        builder: (_) => MusicInfoDialog(
                            load: () {
                              calls++;
                              if (calls == 1) {
                                return Future.error(StateError('offline'));
                              }
                              return pending.future;
                            },
                            isCurrent: () => true)),
                    child: const Text('Info'))))));
    await tester.tap(find.text('Info'));
    await tester.pumpAndSettle();
    expect(find.textContaining('Could not load information'), findsOneWidget);
    await tester.tap(find.text('Retry'));
    await tester.pump();
    expect(find.text('Loading information…'), findsOneWidget);
    pending.complete(Media.fromJson({
      'id': 7,
      'type': 'audio',
      'file_name': 'source.flac',
      'rel_path': 'source.flac',
      'codec': 'flac',
      'bitrate': 1000000,
      'duration': 120,
      'file_size_bytes': 1234,
      'music': {'bpm': 123.5, 'sample_rate': 48000}
    }));
    await tester.pumpAndSettle();
    expect(find.text('Tempo: 123.5 BPM (beats/min)'), findsOneWidget);
    await tester.tap(find.text('Close'));
    await tester.pumpAndSettle();
    expect(find.text('Music and file information'), findsNothing);
    expect(find.text('Info'), findsOneWidget);
    expect(calls, 2);
  });

  testWidgets('late response from changed source cannot disclose metadata',
      (tester) async {
    final pending = Completer<Media>();
    var current = true;
    await tester.pumpWidget(MaterialApp(
        home: MusicInfoDialog(
            load: () => pending.future, isCurrent: () => current)));
    current = false;
    pending.complete(Media.fromJson({
      'file_name': 'Secret name',
      'music': {'title': 'Secret tag'}
    }));
    await tester.pumpAndSettle();
    expect(find.textContaining('Secret'), findsNothing);
    expect(find.textContaining('Could not load information'), findsOneWidget);
  });
}
