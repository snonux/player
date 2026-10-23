import 'dart:async';
import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/radio_api.dart';
import 'package:player_android/screens/radio_screen.dart';

void main() {
  test(
      'radio requests carry captured epoch and reject stale responses/mutations',
      () async {
    var current = true, calls = 0;
    final pending = Completer<void>();
    final dio = Dio()
      ..interceptors
          .add(InterceptorsWrapper(onRequest: (request, handler) async {
        calls++;
        expect(request.extra['credentialEpoch'], 42);
        await pending.future;
        handler.resolve(Response(requestOptions: request, data: <dynamic>[]));
      }));
    final api = RadioApi(dio, credentialEpoch: 42, isCurrent: () => current);
    final listing = api.list();
    await Future<void>.delayed(Duration.zero);
    current = false;
    pending.complete();
    await expectLater(listing, throwsStateError);
    await expectLater(api.remove(1), throwsStateError);
    await expectLater(api.save(1, {'name': 'Old station'}), throwsStateError);
    expect(calls, 1,
        reason: 'stale dialogs must not send to another account/server');
  });
  testWidgets('station editor refuses Save after its account changes',
      (tester) async {
    var current = true, calls = 0;
    final dio = Dio()
      ..interceptors.add(InterceptorsWrapper(onRequest: (request, handler) {
        calls++;
        handler.resolve(Response(requestOptions: request, data: {}));
      }));
    final api = RadioApi(dio, credentialEpoch: 7, isCurrent: () => current);
    await tester.pumpWidget(MaterialApp(
        home: Scaffold(
            body: RadioStationEditor(api: api, station: const {
      'id': 1,
      'name': 'Old station',
      'stream_url': 'https://example.com/live',
      'homepage_url': '',
      'artwork_url': '',
      'local_network': false
    }))));
    current = false;
    await tester.ensureVisible(find.text('Save'));
    await tester.tap(find.text('Save'));
    await tester.pump();
    expect(calls, 0);
    expect(
        find.text('Account changed. Close this dialog and refresh stations.'),
        findsOneWidget);
    await tester.pumpWidget(const SizedBox.shrink());
  });
}
