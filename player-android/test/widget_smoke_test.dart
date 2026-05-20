// Widget smoke tests for PlayerAndroidApp.
//
// These tests verify that the app boots without crashing and that the root
// widget tree renders correctly.  Navigation is handled by go_router and
// Riverpod; deeper screen tests live in test/screens/*.dart.
//
// The app requires a [ProviderScope] ancestor at the root — [PlayerAndroidApp]
// is a [ConsumerWidget] that reads [routerProvider] from Riverpod.  Wrapping
// it in a [ProviderScope] and overriding [tokenStorageProvider] and
// [apiClientProvider] avoids any platform-specific code (OS keychain, network)
// during tests.
//
// Run with: flutter test test/widget_smoke_test.dart

import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/main.dart';
import 'package:player_android/providers/api_client_provider.dart';

// ---------------------------------------------------------------------------
// Minimal fakes to avoid platform-specific code in smoke tests.
// ---------------------------------------------------------------------------

/// In-memory token storage that simulates a logged-in session so the router
/// does not attempt a redirect to /login (which would trigger firstRunProvider
/// and make a real network call).
class _LoggedInTokenStorage implements TokenStorage {
  @override
  Future<String?> readToken() async => 'fake-session-token';

  @override
  Future<void> writeToken(String token) async {}

  @override
  Future<void> deleteToken() async {}
}

/// Minimal [PlayerApiClient] stub — only [countUsers] is exercised by the
/// router redirect when auth state is unauthenticated.
class _FakeApiClient extends PlayerApiClient {
  _FakeApiClient() : super(dio: Dio());

  @override
  Future<int> countUsers() async => 1; // Non-zero: normal login flow.
}

void main() {
  group('PlayerAndroidApp', () {
    testWidgets('boots without crashing inside a ProviderScope', (tester) async {
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            // Simulate a logged-in session so the router does not try to
            // hit the network for the firstRunProvider check.
            tokenStorageProvider.overrideWithValue(_LoggedInTokenStorage()),
            apiClientProvider.overrideWithValue(_FakeApiClient()),
          ],
          child: const PlayerAndroidApp(),
        ),
      );

      // Let all async providers (authStateProvider, router initialisation)
      // settle before asserting.
      await tester.pumpAndSettle();

      // The app renders — no unhandled exceptions.
      expect(tester.takeException(), isNull);
    });
  });
}
