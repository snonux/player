import 'package:flutter_secure_storage/flutter_secure_storage.dart';
// Widget tests for LoginScreen.
//
// Tests cover:
//   1. Successful login: API called with correct credentials, token persisted,
//      auth state transitions to authenticated.
//   2. 401 error display: invalid-credentials response shows the error message.
//   3. Network error display: connection failure shows connectivity message.
//   4. Loading state: CircularProgressIndicator replaces the submit button
//      while the HTTP request is in flight.
//   5. Form validation: empty fields prevent submission.
//
// Riverpod providers are overridden with fakes so tests run without a real
// server or OS keychain.
//
// Run with: flutter test test/screens/login_screen_test.dart

import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/models/models.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/screens/login_screen.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/settings_provider.dart';
import 'package:player_android/services/saved_server_credentials.dart';
import 'package:shared_preferences/shared_preferences.dart';

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

/// In-memory [TokenStorage] used to avoid the platform-specific OS keychain
/// in tests.  Stores the token in a plain Dart field.
class _FakeTokenStorage implements TokenStorage {
  String? _token;

  @override
  Future<String?> readToken() async => _token;

  @override
  Future<void> writeToken(String token) async => _token = token;

  @override
  Future<void> deleteToken() async => _token = null;
}

class _BiometricChoice implements ServerBiometricUnlock {
  _BiometricChoice(this.accepted);
  final bool accepted;
  int calls = 0;
  @override
  Future<bool> authenticate(String origin) async {
    calls++;
    return accepted;
  }
}

/// [PlayerApiClient] stub whose [login] behaviour is controlled by the test
/// via [loginResult] and [loginError].
///
/// Every other method is left as [UnimplementedError] — [LoginScreen] only
/// calls [login].
class _FakeApiClient extends PlayerApiClient {
  _FakeApiClient() : super(dio: Dio());

  /// When non-null, [login] returns this [User].
  User? loginResult;

  /// When non-null, [login] throws this exception instead of returning.
  Object? loginError;

  Object? tokenError;

  @override
  Future<Map<String, dynamic>> createAPIToken(
      {required String name, int? expiresInDays}) async {
    if (tokenError != null) throw tokenError!;
    return {'id': 7, 'token': 'pt-test-token'};
  }

  // Captures the last credentials passed to login for assertion in tests.
  String? capturedUsername;
  String? capturedPassword;

  @override
  Future<User> login({
    required String username,
    required String password,
  }) async {
    capturedUsername = username;
    capturedPassword = password;
    if (loginError != null) throw loginError!;
    return loginResult!;
  }
}

/// [PlayerApiClient] stub that delays the [login] response until [complete]
/// is called, allowing tests to inspect the loading state mid-flight.
class _DelayedFakeApiClient extends PlayerApiClient {
  _DelayedFakeApiClient() : super(dio: Dio());

  // Completer that the test resolves at a chosen point in time.
  final _completer = Completer<User>();

  @override
  Future<Map<String, dynamic>> createAPIToken(
          {required String name, int? expiresInDays}) async =>
      {'id': 7, 'token': 'pt-test-token'};

  /// Resolves the pending login call with [user].
  void complete(User user) => _completer.complete(user);

  @override
  Future<User> login({
    required String username,
    required String password,
  }) =>
      _completer.future;
}

// ---------------------------------------------------------------------------
// Helper: build the widget under test inside a minimal ProviderScope.
// ---------------------------------------------------------------------------

/// Pumps [LoginScreen] inside a [ProviderScope] that overrides:
///   - [apiClientProvider] with [fakeClient]
///   - [tokenStorageProvider] with an in-memory fake (avoids platform keychain)
///
/// Returns the [_FakeTokenStorage] so callers can inspect what was persisted.
Future<_FakeTokenStorage> _pumpLoginScreen(
  WidgetTester tester,
  PlayerApiClient fakeClient,
) async {
  final fakeStorage = _FakeTokenStorage();

  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        // Avoid OS keychain calls during tests.
        tokenStorageProvider.overrideWithValue(fakeStorage),
        // Use the controllable fake instead of a real HTTP client.
        apiClientProvider.overrideWithValue(fakeClient),
      ],
      child: const MaterialApp(
        home: LoginScreen(),
      ),
    ),
  );

  return fakeStorage;
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

void main() {
  setUp(() {
    SharedPreferences.setMockInitialValues({});
    FlutterSecureStorage.setMockInitialValues({});
  });
  // --------------------------------------------------------------------------
  // Form validation
  // --------------------------------------------------------------------------

  group('form validation', () {
    testWidgets('submitting empty form shows required-field errors',
        (tester) async {
      final fakeClient = _FakeApiClient();
      await _pumpLoginScreen(tester, fakeClient);

      // Tap submit without filling any field.
      await tester.tap(find.byKey(const Key('login_submit')));
      await tester.pump();

      // Both fields should show a required-field error.
      expect(find.text('This field is required.'), findsNWidgets(2));
      // No API call should have been made.
      expect(fakeClient.capturedUsername, isNull);
    });

    testWidgets('empty username shows required error', (tester) async {
      final fakeClient = _FakeApiClient();
      await _pumpLoginScreen(tester, fakeClient);

      // Fill in password but leave username empty.
      await tester.enterText(
          find.byKey(const Key('login_password')), 'secret123');
      await tester.tap(find.byKey(const Key('login_submit')));
      await tester.pump();

      expect(find.text('This field is required.'), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Successful login
  // --------------------------------------------------------------------------

  for (final accepted in [false, true]) {
    testWidgets(
        'saved fingerprint button requires device approval and retains password fallback ($accepted)',
        (tester) async {
      final storage = _FakeTokenStorage();
      final biometric = _BiometricChoice(accepted);
      const user = User(id: 1, username: 'alice', isAdmin: false);
      final api = _FakeApiClient()..loginResult = user;
      late ProviderContainer container;
      await tester.runAsync(() async {
        SharedPreferences.setMockInitialValues(
            {'server_base_url': 'https://a.example'});
        container = ProviderContainer(overrides: [
          tokenStorageProvider.overrideWithValue(storage),
          apiClientProvider.overrideWithValue(api),
          serverBiometricUnlockProvider.overrideWithValue(biometric),
        ]);
        await container.read(settingsProvider.future);
        await container
            .read(settingsProvider.notifier)
            .selectDestination(LibraryDestination.server);
        await container.read(authStateProvider.future);
        await container.read(savedServerCredentialsProvider).write(
            SavedServerCredential(
                origin: 'https://a.example',
                token: 'saved-token',
                tokenId: 8,
                user: user,
                expiresAt: DateTime.now()
                    .add(const Duration(days: 1))
                    .millisecondsSinceEpoch));
      });
      addTearDown(container.dispose);
      await tester.pumpWidget(UncontrolledProviderScope(
          container: container, child: const MaterialApp(home: LoginScreen())));
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('login_fingerprint')), findsOneWidget);
      expect(storage._token, isNull);
      expect(api.capturedUsername, isNull);
      await tester.runAsync(() async {
        await tester.tap(find.byKey(const Key('login_fingerprint')));
        await Future<void>.delayed(Duration.zero);
      });
      await tester.pumpAndSettle();
      expect(biometric.calls, 1);
      if (accepted) {
        expect(storage._token, 'saved-token');
        expect(container.read(authStateProvider).requireValue.isAuthenticated,
            isTrue);
        expect(api.capturedUsername, isNull);
      } else {
        expect(storage._token, isNull);
        expect(container.read(authStateProvider).requireValue.isUnauthenticated,
            isTrue);
        expect(find.textContaining('Login remains locked'), findsOneWidget);
        await tester.enterText(
            find.byKey(const Key('login_password')), 'server-password');
        await tester.runAsync(() async {
          await tester.tap(find.byKey(const Key('login_submit')));
          await Future<void>.delayed(Duration.zero);
        });
        await tester.pumpAndSettle();
        expect(api.capturedUsername, 'alice');
        expect(api.capturedPassword, 'server-password');
        expect(storage._token, 'pt-test-token');
        expect(container.read(authStateProvider).requireValue.isAuthenticated,
            isTrue);
      }
    });
  }

  group('successful login', () {
    testWidgets('calls login with correct credentials', (tester) async {
      final fakeClient = _FakeApiClient()
        ..loginResult = const User(id: 1, username: 'alice', isAdmin: false);

      await _pumpLoginScreen(tester, fakeClient);

      await tester.enterText(find.byKey(const Key('login_username')), 'alice');
      await tester.enterText(
          find.byKey(const Key('login_password')), 'mysecret');

      await tester.tap(find.byKey(const Key('login_submit')));
      await tester.pump();
      await tester.pumpAndSettle();

      // The fake should have received the exact credentials.
      expect(fakeClient.capturedUsername, equals('alice'));
      expect(fakeClient.capturedPassword, equals('mysecret'));
    });

    testWidgets('stores a bearer token with the returned identity',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..loginResult = const User(id: 2, username: 'bob', isAdmin: false);

      final fakeStorage = await _pumpLoginScreen(tester, fakeClient);
      fakeStorage._token = 'stale-bearer';

      await tester.enterText(find.byKey(const Key('login_username')), 'bob');
      await tester.enterText(
          find.byKey(const Key('login_password')), 'supersecret');

      await tester.tap(find.byKey(const Key('login_submit')));
      await tester.pump();
      await tester.pumpAndSettle();

      expect(fakeStorage._token, 'pt-test-token');
      final prefs = await SharedPreferences.getInstance();
      expect(prefs.getBool('auth_session_present'), isTrue);
    });

    testWidgets('shows submit button initially and no loading indicator',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..loginResult = const User(id: 1, username: 'alice', isAdmin: false);

      await _pumpLoginScreen(tester, fakeClient);

      // Before any interaction: button visible, no spinner.
      expect(find.byKey(const Key('login_submit')), findsOneWidget);
      expect(find.byType(CircularProgressIndicator), findsNothing);
    });
  });

  testWidgets('token issuance failure leaves the user signed out',
      (tester) async {
    final fakeClient = _FakeApiClient()
      ..loginResult = const User(id: 1, username: 'alice', isAdmin: false)
      ..tokenError = StateError('token issuance failed');
    final storage = await _pumpLoginScreen(tester, fakeClient);
    await tester.enterText(find.byKey(const Key('login_username')), 'alice');
    await tester.enterText(find.byKey(const Key('login_password')), 'secret');
    await tester.tap(find.byKey(const Key('login_submit')));
    await tester.pumpAndSettle();

    final prefs = await SharedPreferences.getInstance();
    expect(prefs.getBool('auth_session_present'), isNull);
    expect(storage._token, isNull);
    expect(find.text('An unexpected error occurred. Please try again.'),
        findsOneWidget);
  });

  // --------------------------------------------------------------------------
  // Loading state
  // --------------------------------------------------------------------------

  group('loading state', () {
    testWidgets('loading indicator shown during a delayed login',
        (tester) async {
      // Use a completer so the login response is held until we choose to
      // resolve it, giving us a window to assert on the loading state.
      final fakeClient = _DelayedFakeApiClient();

      await _pumpLoginScreen(tester, fakeClient);

      await tester.enterText(find.byKey(const Key('login_username')), 'alice');
      await tester.enterText(
          find.byKey(const Key('login_password')), 'supersecret');

      // Tap submit — the _DelayedFakeApiClient won't resolve yet.
      await tester.tap(find.byKey(const Key('login_submit')));
      // Pump exactly one frame: setState(_isLoading=true) has run but the
      // Future has not yet resolved.
      await tester.pump();

      // Loading state: spinner replaces the submit button.
      expect(find.byType(CircularProgressIndicator), findsOneWidget);
      expect(find.byKey(const Key('login_submit')), findsNothing);

      // Resolve the fake and let the widget settle.
      fakeClient.complete(const User(id: 1, username: 'alice', isAdmin: false));
      await tester.pumpAndSettle();

      // After completion: spinner gone.
      expect(find.byType(CircularProgressIndicator), findsNothing);
    });
  });

  // --------------------------------------------------------------------------
  // Error display
  // --------------------------------------------------------------------------

  group('error display', () {
    testWidgets('401 DioException shows invalid-credentials message',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..loginError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/auth/login'),
          response: Response(
            requestOptions: RequestOptions(path: '/api/v1/auth/login'),
            statusCode: 401,
            // Server returns {"error": "invalid credentials"} for bad logins.
            data: <String, dynamic>{'error': 'invalid credentials'},
          ),
          type: DioExceptionType.badResponse,
        );

      await _pumpLoginScreen(tester, fakeClient);

      await tester.enterText(find.byKey(const Key('login_username')), 'alice');
      await tester.enterText(
          find.byKey(const Key('login_password')), 'wrongpassword');

      await tester.tap(find.byKey(const Key('login_submit')));
      await tester.pump();
      await tester.pumpAndSettle();

      // Server-supplied error message from the JSON body is shown in the
      // SnackBar — this is the "error" field from the response.
      expect(find.text('invalid credentials'), findsOneWidget);
    });

    testWidgets('401 without body shows fallback invalid-credentials message',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..loginError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/auth/login'),
          response: Response(
            requestOptions: RequestOptions(path: '/api/v1/auth/login'),
            statusCode: 401,
            // Empty body — no server-supplied message.
            data: <String, dynamic>{},
          ),
          type: DioExceptionType.badResponse,
        );

      await _pumpLoginScreen(tester, fakeClient);

      await tester.enterText(find.byKey(const Key('login_username')), 'alice');
      await tester.enterText(
          find.byKey(const Key('login_password')), 'wrongpassword');

      await tester.tap(find.byKey(const Key('login_submit')));
      await tester.pump();
      await tester.pumpAndSettle();

      // Without a body message, the 401 fallback text is shown.
      expect(find.text('Invalid username or password.'), findsOneWidget);
    });

    testWidgets('network error shows connectivity message', (tester) async {
      final fakeClient = _FakeApiClient()
        ..loginError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/auth/login'),
          type: DioExceptionType.connectionError,
        );

      await _pumpLoginScreen(tester, fakeClient);

      await tester.enterText(find.byKey(const Key('login_username')), 'alice');
      await tester.enterText(find.byKey(const Key('login_password')), 'secret');

      await tester.tap(find.byKey(const Key('login_submit')));
      await tester.pump();
      await tester.pumpAndSettle();

      expect(
        find.textContaining('Could not reach the server'),
        findsOneWidget,
      );
    });

    testWidgets('500 server error shows generic server-error message',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..loginError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/auth/login'),
          response: Response(
            requestOptions: RequestOptions(path: '/api/v1/auth/login'),
            statusCode: 500,
            data: <String, dynamic>{},
          ),
          type: DioExceptionType.badResponse,
        );

      await _pumpLoginScreen(tester, fakeClient);

      await tester.enterText(find.byKey(const Key('login_username')), 'alice');
      await tester.enterText(find.byKey(const Key('login_password')), 'secret');

      await tester.tap(find.byKey(const Key('login_submit')));
      await tester.pump();
      await tester.pumpAndSettle();

      expect(
        find.text('Server error (500). Please try again.'),
        findsOneWidget,
      );
    });
  });
}
