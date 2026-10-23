import 'dart:async';
import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/widgets/playlist_import_dialog.dart';

class _ImportClient extends PlayerApiClient {
  _ImportClient() : super(dio: Dio());
  final calls = <Map<String, dynamic>>[];
  bool fail = false;
  Completer<Map<String, dynamic>>? pending;
  @override
  Future<Map<String, dynamic>> importPlaylist(Map<String, dynamic> body) async {
    calls.add(body);
    if (fail) throw StateError('offline');
    if (pending != null) return pending!.future;
    return {
      'name': '旅',
      'available': 3,
      'unavailable': 2,
      'rejected': 1,
      'unavailable_lines': [5, 6],
      'fingerprint': 'reviewed'
    };
  }
}

void main() {
  Future<void> mount(WidgetTester t, _ImportClient api) async {
    await t.pumpWidget(ProviderScope(
        overrides: [apiClientProvider.overrideWithValue(api)],
        child: MaterialApp(
            home: Builder(
                builder: (context) => Scaffold(
                    body: TextButton(
                        onPressed: () => showDialog<String>(
                            context: context,
                            builder: (_) => const PlaylistImportDialog()),
                        child: const Text('Import')))))));
    await t.tap(find.text('Import'));
    await t.pumpAndSettle();
  }

  testWidgets(
      'source preview shows unavailable feedback and confirms exact base and follow mode',
      (t) async {
    final api = _ImportClient();
    await mount(t, api);
    await t.enterText(find.byType(TextField), 'Music/lists/旅.m3u8');
    await t.tap(find.byType(Switch));
    await t.pumpAndSettle();
    await t.tap(find.text('Preview import'));
    await t.pumpAndSettle();
    expect(find.text('3 songs · 2 unavailable · 1 unsafe entries skipped.'),
        findsOneWidget);
    expect(
        find.textContaining('Unavailable source lines: 5, 6'), findsOneWidget);
    await t.ensureVisible(find.text('Import and follow'));
    await t.tap(find.text('Import and follow'));
    await t.pumpAndSettle();
    expect(api.calls.last, {
      'path': 'Music/lists/旅.m3u8',
      'base': 'folder',
      'follow': true,
      'preview': false,
      'fingerprint': 'reviewed'
    });
    await t.enterText(find.byType(TextField), 'Music/new.m3u8');
    await t.pumpAndSettle();
    expect(find.text('Preview import'), findsOneWidget);
    expect(find.text('旅'), findsNothing);
  });
  testWidgets(
      'failed import retains source and dismissed preview cannot reopen dialog',
      (t) async {
    final api = _ImportClient()..fail = true;
    await mount(t, api);
    await t.enterText(find.byType(TextField), 'Music/retry.m3u');
    await t.tap(find.text('Preview import'));
    await t.pumpAndSettle();
    expect(find.textContaining('Could not import playlist.'), findsOneWidget);
    expect(find.text('Music/retry.m3u'), findsOneWidget);
    api.fail = false;
    api.pending = Completer();
    await t.tap(find.text('Preview import'));
    await t.pump();
    await t.tap(find.text('Cancel'));
    await t.pumpAndSettle();
    api.pending!.complete({
      'name': 'Late',
      'available': 0,
      'unavailable': 0,
      'rejected': 0,
      'unavailable_lines': [],
      'fingerprint': 'late'
    });
    await t.pumpAndSettle();
    expect(find.byType(AlertDialog), findsNothing);
    expect(t.takeException(), isNull);
  });
}
