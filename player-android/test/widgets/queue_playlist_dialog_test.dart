import 'dart:async';
import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/widgets/queue_playlist_dialog.dart';

class _Client extends PlayerApiClient {
  _Client() : super(dio: Dio());
  final calls = <List<int>>[];
  bool fail = false;
  Completer<Map<String, dynamic>>? pending;
  @override
  Future<Map<String, dynamic>> createPlaylistFromQueue(
      String name, List<int> ids, int epoch) async {
    calls.add(List.of(ids));
    if (fail) throw StateError('offline');
    return pending?.future ?? Future.value({'playlist_id': 'saved'});
  }
}

void main() {
  Future<void> mount(WidgetTester t, _Client api,
      {bool Function()? current}) async {
    await t.pumpWidget(MaterialApp(
        home: Builder(
            builder: (context) => Scaffold(
                body: TextButton(
                    onPressed: () => showDialog<String>(
                        context: context,
                        builder: (_) => QueuePlaylistDialog(
                            api: api,
                            mediaIds: const [2, 1, 2],
                            epoch: 7,
                            current: current ?? () => true)),
                    child: const Text('Save queue'))))));
    await t.tap(find.text('Save queue'));
    await t.pumpAndSettle();
  }

  testWidgets(
      'failure retains name; save preserves repeated order and offers Open playlist',
      (t) async {
    final api = _Client()..fail = true;
    await mount(t, api);
    await t.enterText(find.byType(TextField), '旅 🌙');
    await t.tap(find.text('Save playlist'));
    await t.pumpAndSettle();
    expect(find.text('旅 🌙'), findsOneWidget);
    expect(find.textContaining('Could not save playlist.'), findsOneWidget);
    api.fail = false;
    await t.tap(find.text('Save playlist'));
    await t.pumpAndSettle();
    expect(api.calls.last, [2, 1, 2]);
    expect(find.text('Open playlist'), findsOneWidget);
  });
  testWidgets('dismissed pending save does not affect a later dialog',
      (t) async {
    final api = _Client()..pending = Completer();
    await mount(t, api);
    await t.enterText(find.byType(TextField), 'copy');
    await t.tap(find.text('Save playlist'));
    await t.pump();
    await t.tap(find.text('Save playlist'));
    expect(api.calls.length, 1);
    await t.tap(find.text('Cancel'));
    await t.pumpAndSettle();
    api.pending!.complete({'playlist_id': 'late'});
    await t.pumpAndSettle();
    expect(find.text('Open playlist'), findsNothing);
    expect(t.takeException(), isNull);
  });
  testWidgets('account switch refuses stale snapshot submission', (t) async {
    final api = _Client();
    var current = true;
    await mount(t, api, current: () => current);
    await t.enterText(find.byType(TextField), 'old account');
    current = false;
    await t.tap(find.text('Save playlist'));
    await t.pumpAndSettle();
    expect(api.calls, isEmpty);
    expect(find.text('Save playlist'), findsNothing);
  });
}
