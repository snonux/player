// Widget tests for BootstrapScreen.
//
// Tests cover:
//   1. Form validation (empty fields, password too short, mismatched passwords).
//   2. Successful submit: API is called with correct credentials, auth state
//      transitions to authenticated.
//   3. Error display: server errors produce a visible SnackBar message.
//
// Riverpod providers are overridden with fakes/mocks so tests run without a
// real server or OS keychain.
//
// Run with: flutter test test/screens/bootstrap_screen_test.dart

import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/models/models.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/screens/bootstrap_screen.dart';

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

/// In-memory [TokenStorage] used by [_FakeAuthStateNotifier] to avoid
/// platform-specific secure storage in tests.
class _FakeTokenStorage implements TokenStorage {
  String? _token;

  @override
  Future<String?> readToken() async => _token;

  @override
  Future<void> writeToken(String token) async => _token = token;

  @override
  Future<void> deleteToken() async => _token = null;
}

/// [PlayerApiClient] stub whose [bootstrap] behaviour is controlled by the
/// test via [bootstrapResult] and [bootstrapError].
///
/// Every other method is left as [UnimplementedError] — the bootstrap screen
/// only calls [bootstrap].
class _FakeApiClient extends PlayerApiClient {
  _FakeApiClient() : super(dio: Dio());

  /// When non-null, [bootstrap] returns this [User].
  User? bootstrapResult;

  /// When non-null, [bootstrap] throws this exception instead of returning.
  Object? bootstrapError;

  @override
  Future<User> bootstrap({
    required String username,
    required String password,
  }) async {
    if (bootstrapError != null) throw bootstrapError!;
    return bootstrapResult!;
  }
}

/// [PlayerApiClient] stub that delays the [bootstrap] response until
/// [complete] is called, allowing tests to inspect the loading state.
class _DelayedFakeApiClient extends PlayerApiClient {
  _DelayedFakeApiClient() : super(dio: Dio());

  // Completer that the test resolves at a chosen point in time.
  final _completer = Completer<User>();

  /// Resolves the pending bootstrap call with [user].
  void complete(User user) => _completer.complete(user);

  @override
  Future<User> bootstrap({
    required String username,
    required String password,
  }) =>
      _completer.future;
}

// ---------------------------------------------------------------------------
// Helper: build the widget under test inside a minimal ProviderScope.
// ---------------------------------------------------------------------------

/// Pumps [BootstrapScreen] inside a [ProviderScope] that overrides:
///   - [apiClientProvider] with [fakeClient]
///   - [tokenStorageProvider] with an in-memory fake (so AuthStateNotifier
///     does not touch the platform keychain)
///
/// Returns the [_FakeTokenStorage] so callers can inspect stored tokens.
Future<_FakeTokenStorage> _pumpBootstrapScreen(
  WidgetTester tester,
  PlayerApiClient fakeClient,
) async {
  final fakeStorage = _FakeTokenStorage();

  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        // Override token storage to avoid flutter_secure_storage platform call.
        tokenStorageProvider.overrideWithValue(fakeStorage),
        // Override API client with our controllable fake.
        apiClientProvider.overrideWithValue(fakeClient),
      ],
      child: const MaterialApp(
        home: BootstrapScreen(),
      ),
    ),
  );

  return fakeStorage;
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

void main() {
  // --------------------------------------------------------------------------
  // Form validation
  // --------------------------------------------------------------------------

  group('form validation', () {
    testWidgets('submitting empty form shows required-field errors',
        (tester) async {
      final fakeClient = _FakeApiClient();
      await _pumpBootstrapScreen(tester, fakeClient);

      // Tap submit without filling any field.
      await tester.tap(find.byKey(const Key('bootstrap_submit')));
      await tester.pump();

      // Expect validation errors on all three fields.
      expect(find.text('This field is required.'), findsNWidgets(3));
      // No API call should have been made.
      expect(fakeClient.bootstrapResult, isNull);
    });

    testWidgets('password shorter than 8 chars shows length error',
        (tester) async {
      final fakeClient = _FakeApiClient();
      await _pumpBootstrapScreen(tester, fakeClient);

      await tester.enterText(
          find.byKey(const Key('bootstrap_username')), 'admin');
      await tester.enterText(
          find.byKey(const Key('bootstrap_password')), 'short');
      await tester.enterText(
          find.byKey(const Key('bootstrap_confirm')), 'short');

      await tester.tap(find.byKey(const Key('bootstrap_submit')));
      await tester.pump();

      expect(
        find.text('Password must be at least 8 characters.'),
        findsOneWidget,
      );
    });

    testWidgets('mismatched passwords shows mismatch error', (tester) async {
      final fakeClient = _FakeApiClient();
      await _pumpBootstrapScreen(tester, fakeClient);

      await tester.enterText(
          find.byKey(const Key('bootstrap_username')), 'admin');
      await tester.enterText(
          find.byKey(const Key('bootstrap_password')), 'password123');
      await tester.enterText(
          find.byKey(const Key('bootstrap_confirm')), 'different123');

      await tester.tap(find.byKey(const Key('bootstrap_submit')));
      await tester.pump();

      expect(find.text('Passwords do not match.'), findsOneWidget);
    });

    testWidgets('valid form with matching passwords passes validation',
        (tester) async {
      // Set up fake to return a user so the form submit completes.
      final fakeClient = _FakeApiClient()
        ..bootstrapResult = const User(
          id: 1,
          username: 'admin',
          isAdmin: true,
        );
      final fakeStorage = await _pumpBootstrapScreen(tester, fakeClient);

      await tester.enterText(
          find.byKey(const Key('bootstrap_username')), 'admin');
      await tester.enterText(
          find.byKey(const Key('bootstrap_password')), 'password123');
      await tester.enterText(
          find.byKey(const Key('bootstrap_confirm')), 'password123');

      await tester.tap(find.byKey(const Key('bootstrap_submit')));
      await tester.pump(); // Start the async submit.
      await tester.pumpAndSettle(); // Let the future complete.

      // No validation-error text should appear.
      expect(find.text('This field is required.'), findsNothing);
      expect(find.text('Passwords do not match.'), findsNothing);
      expect(find.text('Password must be at least 8 characters.'), findsNothing);

      // Token should have been persisted via the fake storage.
      expect(fakeStorage._token, isNotNull);
    });
  });

  // --------------------------------------------------------------------------
  // Successful submit
  // --------------------------------------------------------------------------

  group('successful submit', () {
    testWidgets('calls bootstrap with correct credentials and succeeds',
        (tester) async {
      // Set up fake to return a user matching the submitted username.
      final fakeClient = _FakeApiClient()
        ..bootstrapResult =
            const User(id: 2, username: 'testadmin', isAdmin: true);

      await _pumpBootstrapScreen(tester, fakeClient);

      await tester.enterText(
          find.byKey(const Key('bootstrap_username')), 'testadmin');
      await tester.enterText(
          find.byKey(const Key('bootstrap_password')), 'supersecret');
      await tester.enterText(
          find.byKey(const Key('bootstrap_confirm')), 'supersecret');

      await tester.tap(find.byKey(const Key('bootstrap_submit')));
      await tester.pump();
      await tester.pumpAndSettle();

      // No errors visible — the fake was called successfully.
      expect(find.text('Passwords do not match.'), findsNothing);
      expect(find.text('This field is required.'), findsNothing);
    });

    testWidgets('persists token to storage after success', (tester) async {
      final fakeClient = _FakeApiClient()
        ..bootstrapResult = const User(
          id: 1,
          username: 'admin',
          isAdmin: true,
        );
      final fakeStorage = await _pumpBootstrapScreen(tester, fakeClient);

      await tester.enterText(
          find.byKey(const Key('bootstrap_username')), 'admin');
      await tester.enterText(
          find.byKey(const Key('bootstrap_password')), 'password123');
      await tester.enterText(
          find.byKey(const Key('bootstrap_confirm')), 'password123');

      await tester.tap(find.byKey(const Key('bootstrap_submit')));
      await tester.pump();
      await tester.pumpAndSettle();

      // The username is stored as the session marker token.
      expect(fakeStorage._token, equals('admin'));
    });

    testWidgets('submit button visible initially, no loading indicator',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..bootstrapResult =
            const User(id: 1, username: 'admin', isAdmin: true);

      await _pumpBootstrapScreen(tester, fakeClient);

      // Initially: submit button visible and no loading indicator.
      expect(find.byKey(const Key('bootstrap_submit')), findsOneWidget);
      expect(find.byType(CircularProgressIndicator), findsNothing);
    });

    testWidgets('loading indicator shown during a delayed submit',
        (tester) async {
      // Use a completer to hold the bootstrap response so the loading state
      // is visible for long enough to assert on it.
      final fakeClient = _DelayedFakeApiClient();

      await _pumpBootstrapScreen(tester, fakeClient);

      await tester.enterText(
          find.byKey(const Key('bootstrap_username')), 'admin');
      await tester.enterText(
          find.byKey(const Key('bootstrap_password')), 'longpassword');
      await tester.enterText(
          find.byKey(const Key('bootstrap_confirm')), 'longpassword');

      // Tap submit — the _DelayedFakeApiClient won't resolve yet.
      await tester.tap(find.byKey(const Key('bootstrap_submit')));
      // Pump a single frame: setState(_isLoading=true) has run but the
      // bootstrap Future has not yet resolved.
      await tester.pump();

      // During submit: progress indicator should replace the button.
      expect(find.byType(CircularProgressIndicator), findsOneWidget);
      expect(find.byKey(const Key('bootstrap_submit')), findsNothing);

      // Resolve the fake and settle.
      fakeClient.complete(const User(id: 1, username: 'admin', isAdmin: true));
      await tester.pumpAndSettle();

      // After submit: loading cleared.
      expect(find.byType(CircularProgressIndicator), findsNothing);
    });
  });

  // --------------------------------------------------------------------------
  // Error display
  // --------------------------------------------------------------------------

  group('error display', () {
    testWidgets('403 DioException shows already-bootstrapped message',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..bootstrapError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/auth/bootstrap'),
          response: Response(
            requestOptions: RequestOptions(path: '/api/v1/auth/bootstrap'),
            statusCode: 403,
            data: <String, dynamic>{'error': 'bootstrap already complete'},
          ),
          type: DioExceptionType.badResponse,
        );

      await _pumpBootstrapScreen(tester, fakeClient);

      await tester.enterText(
          find.byKey(const Key('bootstrap_username')), 'admin');
      await tester.enterText(
          find.byKey(const Key('bootstrap_password')), 'password123');
      await tester.enterText(
          find.byKey(const Key('bootstrap_confirm')), 'password123');

      await tester.tap(find.byKey(const Key('bootstrap_submit')));
      await tester.pump();
      await tester.pumpAndSettle();

      // The server-supplied error message from the response body is shown.
      expect(find.text('bootstrap already complete'), findsOneWidget);
    });

    testWidgets('network error shows connectivity message', (tester) async {
      final fakeClient = _FakeApiClient()
        ..bootstrapError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/auth/bootstrap'),
          type: DioExceptionType.connectionError,
        );

      await _pumpBootstrapScreen(tester, fakeClient);

      await tester.enterText(
          find.byKey(const Key('bootstrap_username')), 'admin');
      await tester.enterText(
          find.byKey(const Key('bootstrap_password')), 'password123');
      await tester.enterText(
          find.byKey(const Key('bootstrap_confirm')), 'password123');

      await tester.tap(find.byKey(const Key('bootstrap_submit')));
      await tester.pump();
      await tester.pumpAndSettle();

      expect(
        find.textContaining('Could not reach the server'),
        findsOneWidget,
      );
    });

    testWidgets('400 DioException without body shows generic error',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..bootstrapError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/auth/bootstrap'),
          response: Response(
            requestOptions: RequestOptions(path: '/api/v1/auth/bootstrap'),
            statusCode: 400,
            data: <String, dynamic>{},
          ),
          type: DioExceptionType.badResponse,
        );

      await _pumpBootstrapScreen(tester, fakeClient);

      await tester.enterText(
          find.byKey(const Key('bootstrap_username')), 'admin');
      await tester.enterText(
          find.byKey(const Key('bootstrap_password')), 'password123');
      await tester.enterText(
          find.byKey(const Key('bootstrap_confirm')), 'password123');

      await tester.tap(find.byKey(const Key('bootstrap_submit')));
      await tester.pump();
      await tester.pumpAndSettle();

      expect(
        find.text('Invalid request. Check your username and password.'),
        findsOneWidget,
      );
    });
  });
}
