import 'dart:typed_data';
import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/widgets/listening_history_settings.dart';

class _Auth extends AuthStateNotifier {
  @override
  Future<AuthState> build() async => const AuthState.authenticated();
  void changed() => state = const AsyncData(AuthState.unauthenticated());
}

class _Adapter implements HttpClientAdapter {
  int deletes = 0;
  @override
  Future<ResponseBody> fetch(RequestOptions options, Stream<Uint8List>? stream,
      Future<void>? cancel) async {
    if (options.method == 'DELETE') deletes++;
    return ResponseBody.fromString(
        '{"settings":{"recording":true,"retention_days":365},"events":[{"title":"Private account song","played_at":"2026-10-23T12:00:00Z"}]}',
        200,
        headers: {
          Headers.contentTypeHeader: ['application/json']
        });
  }

  @override
  void close({bool force = false}) {}
}

void main() {
  testWidgets(
      'confirmation cannot clear the next account and old rows disappear',
      (tester) async {
    final adapter = _Adapter();
    final dio = Dio()..httpClientAdapter = adapter;
    final auth = _Auth();
    final container = ProviderContainer(overrides: [
      apiClientProvider.overrideWithValue(PlayerApiClient(dio: dio)),
      authStateProvider.overrideWith(() => auth),
    ]);
    addTearDown(container.dispose);
    await tester.pumpWidget(UncontrolledProviderScope(
        container: container,
        child: const MaterialApp(
            home: Scaffold(
                body: SingleChildScrollView(
                    child: ListeningHistorySettings())))));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Listening history'));
    await tester.pumpAndSettle();
    expect(find.text('Private account song'), findsOneWidget);
    await tester.ensureVisible(find.text('Clear history'));
    await tester.tap(find.text('Clear history'));
    await tester.pumpAndSettle();
    container.read(credentialMutationQueueProvider).beginAuthChange();
    auth.changed();
    await tester.pump();
    await tester.tap(find.widgetWithText(TextButton, 'Clear history').last);
    await tester.pumpAndSettle();
    expect(adapter.deletes, 0);
    expect(find.text('Private account song'), findsNothing);
  });
}
