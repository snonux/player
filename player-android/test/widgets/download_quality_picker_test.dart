import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/widgets/download_quality_picker.dart';

class _Client extends Fake implements PlayerApiClient {
  @override
  final Dio rawDio = Dio(BaseOptions(baseUrl: 'https://player.example'));
  @override
  Future<List<Map<String, dynamic>>> streamProfiles() async => [
        {'id': 'opus96', 'label': 'Opus 96 kbps'}
      ];
}

// A consumer can only start when the picker has published a durable selection.
class _StartDownload extends StatefulWidget {
  const _StartDownload(this.load, this.save, this.started);
  final Future<String> Function(String) load;
  final Future<void> Function(String, String) save;
  final List<String> started;
  @override
  State<_StartDownload> createState() => _StartDownloadState();
}

class _StartDownloadState extends State<_StartDownload> {
  String? ready;
  @override
  Widget build(BuildContext context) => MaterialApp(
          home: Scaffold(
              body: SingleChildScrollView(
                  child: Column(children: [
        DownloadQualityPicker(
            client: _Client(),
            loadPreference: widget.load,
            savePreference: widget.save,
            onSelectionReady: (value) => setState(() => ready = value)),
        FilledButton(
            onPressed: ready == null ? null : () => widget.started.add(ready!),
            child: const Text('Keep offline')),
      ]))));
}

void main() {
  testWidgets('delayed initial preference cannot start the displayed Original',
      (tester) async {
    final load = Completer<String>();
    final started = <String>[];
    await tester.pumpWidget(
        _StartDownload((_) => load.future, (_, __) async {}, started));
    expect(tester.widget<FilledButton>(find.byType(FilledButton)).onPressed,
        isNull);
    expect(
        tester
            .widget<DropdownButtonFormField<String>>(
                find.byType(DropdownButtonFormField<String>))
            .onChanged,
        isNull);
    await tester.tap(find.text('Keep offline'));
    expect(started, isEmpty);
    load.complete('opus96');
    await tester.pumpAndSettle();
    await tester.tap(find.text('Keep offline'));
    expect(started, ['opus96']);
  });

  for (final fail in [false, true]) {
    testWidgets('delayed preference save blocks starting; failure=$fail',
        (tester) async {
      final save = Completer<void>();
      final started = <String>[];
      await tester.pumpWidget(_StartDownload(
          (_) async => 'original', (_, __) => save.future, started));
      await tester.pumpAndSettle();
      await tester.tap(find.byType(DropdownButtonFormField<String>));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Opus 96 kbps').last);
      await tester.pumpAndSettle();
      expect(tester.widget<FilledButton>(find.byType(FilledButton)).onPressed,
          isNull);
      await tester.tap(find.text('Keep offline'));
      expect(started, isEmpty);
      if (fail) {
        save.completeError(StateError('storage refused save'));
      } else {
        save.complete();
      }
      await tester.pumpAndSettle();
      await tester.tap(find.text('Keep offline'));
      expect(started, [fail ? 'original' : 'opus96']);
      if (fail) {
        expect(
            find.textContaining('Previous selection retained'), findsOneWidget);
      }
    });
  }
}
