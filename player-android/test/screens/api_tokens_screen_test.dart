// Widget tests for ApiTokensScreen (api_tokens_screen.dart).
//
// Tests cover:
//   1. Loading state: spinner shown while listAPITokens is in flight.
//   2. Renders token list after a successful load (name, dates, expiry).
//   3. "No expiry" shown when expires_at is absent.
//   4. Revoke: confirmation cancel leaves the row intact.
//   5. Revoke: confirmation confirm removes the row (optimistic).
//   6. Revoke optimistic UI: row re-appended and error SnackBar shown on failure.
//   7. Create dialog: opens on FAB tap.
//   8. Create dialog: cancel closes without calling createAPIToken.
//   9. Create dialog: validation — empty name is rejected.
//  10. Create dialog: submits and shows the plaintext token dialog.
//  11. Plaintext dialog: copy button writes token to clipboard.
//  12. Plaintext dialog: Done dismisses the dialog.
//  13. Create optimistic UI: placeholder visible while in flight, replaced on success.
//  14. Create optimistic UI: placeholder reverted and error SnackBar shown on failure.
//  15. Empty state: shown when listAPITokens returns [].
//  16. Error state: shown when listAPITokens throws.
//  17. Retry button re-calls listAPITokens after an error.
//  18. apiTokenErrorMessage unit tests (400, 404, connection, generic).
//
// Riverpod providers are overridden with fakes so tests run without a real
// server or OS keychain.
//
// Run with: flutter test test/screens/api_tokens_screen_test.dart

import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/screens/api_tokens_screen.dart';
import 'package:player_android/utils/error_mappers.dart';

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

/// In-memory [TokenStorage] that returns a fixed username without hitting
/// the OS keychain.
class _FakeTokenStorage implements TokenStorage {
  const _FakeTokenStorage();

  @override
  Future<String?> readToken() async => 'test-token';

  @override
  Future<void> writeToken(String token) async {}

  @override
  Future<void> deleteToken() async {}
}

/// Controllable [PlayerApiClient] stub for [ApiTokensScreen] tests.
///
/// [listAPITokens], [createAPIToken], and [revokeAPIToken] are the primary
/// subjects.  All other methods remain [UnimplementedError].
class _FakeApiClient extends PlayerApiClient {
  _FakeApiClient()
      : super(dio: Dio(BaseOptions(baseUrl: 'http://test.local')));

  // ---- listAPITokens ----

  /// When non-null, [listAPITokens] returns this list.
  List<Map<String, dynamic>>? tokensResult;

  /// When non-null, [listAPITokens] throws this instead of returning.
  Object? tokensError;

  /// Number of times [listAPITokens] has been called.
  int listTokensCallCount = 0;

  @override
  Future<List<Map<String, dynamic>>> listAPITokens() async {
    listTokensCallCount++;
    if (tokensError != null) throw tokensError!;
    return tokensResult!;
  }

  // ---- createAPIToken ----

  /// When non-null, [createAPIToken] returns this map.
  Map<String, dynamic>? createResult;

  /// When non-null, [createAPIToken] throws this instead of returning.
  Object? createError;

  /// Captures the last name passed to [createAPIToken].
  String? createdName;

  /// Captures the last expiresInDays passed to [createAPIToken].
  int? createdExpiresInDays;

  @override
  Future<Map<String, dynamic>> createAPIToken({
    required String name,
    int? expiresInDays,
  }) async {
    createdName = name;
    createdExpiresInDays = expiresInDays;
    if (createError != null) throw createError!;
    return createResult!;
  }

  // ---- revokeAPIToken ----

  /// When non-null, [revokeAPIToken] throws this instead of returning.
  Object? revokeError;

  /// The ID passed to the last [revokeAPIToken] call.
  int? revokedId;

  @override
  Future<void> revokeAPIToken(int tokenId) async {
    revokedId = tokenId;
    if (revokeError != null) throw revokeError!;
  }
}

/// Controllable stub that delays [listAPITokens] until [complete] is called.
class _DelayedListApiClient extends PlayerApiClient {
  _DelayedListApiClient() : super(dio: Dio());

  final _completer = Completer<List<Map<String, dynamic>>>();

  void complete(List<Map<String, dynamic>> tokens) =>
      _completer.complete(tokens);

  @override
  Future<List<Map<String, dynamic>>> listAPITokens() => _completer.future;
}

/// Stub whose [createAPIToken] call is controlled by an external [Completer].
///
/// Allows tests to inspect the optimistic placeholder while the network
/// request is still in flight.
class _DelayedCreateApiClient extends PlayerApiClient {
  _DelayedCreateApiClient({required List<Map<String, dynamic>> initialTokens})
      : _initialTokens = initialTokens,
        super(dio: Dio());

  final List<Map<String, dynamic>> _initialTokens;
  final _createCompleter = Completer<Map<String, dynamic>>();

  void completeCreate(Map<String, dynamic> result) =>
      _createCompleter.complete(result);

  @override
  Future<List<Map<String, dynamic>>> listAPITokens() async => _initialTokens;

  @override
  Future<Map<String, dynamic>> createAPIToken({
    required String name,
    int? expiresInDays,
  }) =>
      _createCompleter.future;
}

// ---------------------------------------------------------------------------
// Sample data
// ---------------------------------------------------------------------------

/// A token with an expiry date.
final _kTokenA = <String, dynamic>{
  'id': 1,
  'name': 'android-client',
  'created_at': '2026-01-15T10:00:00Z',
  'expires_at': '2027-01-15T10:00:00Z',
};

/// A token without an expiry date.
final _kTokenB = <String, dynamic>{
  'id': 2,
  'name': 'home-server',
  'created_at': '2026-03-01T08:00:00Z',
  'expires_at': null,
};

// ---------------------------------------------------------------------------
// Helper: pump ApiTokensScreen inside a minimal ProviderScope.
// ---------------------------------------------------------------------------

/// Pumps [ApiTokensScreen] with a [ProviderScope] that overrides
/// [apiClientProvider] and [tokenStorageProvider] with fakes.
///
/// Using [MaterialApp] (not [MaterialApp.router]) is sufficient because
/// [ApiTokensScreen] does not call [context.go]; it only shows dialogs/SnackBars.
Future<void> _pumpApiTokensScreen(
  WidgetTester tester,
  PlayerApiClient fakeClient,
) async {
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        tokenStorageProvider.overrideWithValue(const _FakeTokenStorage()),
        apiClientProvider.overrideWithValue(fakeClient),
      ],
      child: const MaterialApp(
        home: ApiTokensScreen(),
      ),
    ),
  );
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

void main() {
  // --------------------------------------------------------------------------
  // Loading state
  // --------------------------------------------------------------------------

  group('loading state', () {
    testWidgets('shows loading indicator while listAPITokens is in flight',
        (tester) async {
      final fakeClient = _DelayedListApiClient();

      await _pumpApiTokensScreen(tester, fakeClient);

      // Pump once so initState → addPostFrameCallback fires, but the Future
      // has not resolved yet.
      await tester.pump();

      expect(find.byKey(const Key('api_tokens_loading')), findsOneWidget);
      expect(find.byType(CircularProgressIndicator), findsOneWidget);

      // Resolve to avoid dangling-async warnings.
      fakeClient.complete([_kTokenA]);
      await tester.pumpAndSettle();
    });
  });

  // --------------------------------------------------------------------------
  // Renders token list
  // --------------------------------------------------------------------------

  group('renders token list', () {
    testWidgets('shows a tile for each token returned by listAPITokens',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..tokensResult = [_kTokenA, _kTokenB];

      await _pumpApiTokensScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('api_tokens_list')), findsOneWidget);
      expect(find.text('android-client'), findsOneWidget);
      expect(find.text('home-server'), findsOneWidget);
    });

    testWidgets('shows created-at date in each tile', (tester) async {
      final fakeClient = _FakeApiClient()..tokensResult = [_kTokenA];

      await _pumpApiTokensScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Created date is the first 10 characters of the ISO-8601 timestamp.
      expect(find.textContaining('2026-01-15'), findsOneWidget);
    });

    testWidgets('shows expiry date when expires_at is set', (tester) async {
      final fakeClient = _FakeApiClient()..tokensResult = [_kTokenA];

      await _pumpApiTokensScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.textContaining('2027-01-15'), findsOneWidget);
    });

    testWidgets('shows "No expiry" when expires_at is null', (tester) async {
      final fakeClient = _FakeApiClient()..tokensResult = [_kTokenB];

      await _pumpApiTokensScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.textContaining('No expiry'), findsOneWidget);
    });

    testWidgets('shows a revoke button for each token', (tester) async {
      final fakeClient = _FakeApiClient()
        ..tokensResult = [_kTokenA, _kTokenB];

      await _pumpApiTokensScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('api_token_revoke_1')), findsOneWidget);
      expect(find.byKey(const Key('api_token_revoke_2')), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Revoke action
  // --------------------------------------------------------------------------

  group('revoke action', () {
    testWidgets('confirmation cancel leaves the row intact', (tester) async {
      final fakeClient = _FakeApiClient()
        ..tokensResult = [_kTokenA, _kTokenB];

      await _pumpApiTokensScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('api_token_revoke_1')));
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('api_tokens_confirm_cancel')));
      await tester.pumpAndSettle();

      // Both tiles still present.
      expect(find.byKey(const Key('api_token_tile_1')), findsOneWidget);
      expect(fakeClient.revokedId, isNull);
    });

    testWidgets('confirmation confirm removes the row optimistically',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..tokensResult = [_kTokenA, _kTokenB];

      await _pumpApiTokensScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('api_token_revoke_1')));
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('api_tokens_confirm_revoke')));
      await tester.pumpAndSettle();

      // Token A removed; Token B still present.
      expect(find.byKey(const Key('api_token_tile_1')), findsNothing);
      expect(find.byKey(const Key('api_token_tile_2')), findsOneWidget);

      expect(fakeClient.revokedId, equals(1));
      expect(find.byKey(const Key('api_tokens_revoke_snackbar')), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Revoke optimistic UI
  // --------------------------------------------------------------------------

  group('revoke optimistic UI', () {
    testWidgets('reverts row and shows error SnackBar on revokeAPIToken failure',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..tokensResult = [_kTokenA, _kTokenB]
        ..revokeError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/auth/tokens/1'),
          type: DioExceptionType.connectionError,
        );

      await _pumpApiTokensScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('api_token_revoke_1')));
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('api_tokens_confirm_revoke')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 300));

      // Token A re-appears after the error.
      expect(find.byKey(const Key('api_token_tile_1')), findsOneWidget);

      // Error SnackBar shown.
      expect(find.byKey(const Key('api_tokens_error_snackbar')), findsOneWidget);
      expect(find.textContaining('Could not reach the server'), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Create dialog
  // --------------------------------------------------------------------------

  group('create dialog', () {
    testWidgets('opens on FAB tap', (tester) async {
      final fakeClient = _FakeApiClient()..tokensResult = [_kTokenA];

      await _pumpApiTokensScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('api_tokens_fab')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('api_tokens_create_dialog')), findsOneWidget);
    });

    testWidgets('cancel closes the dialog without calling createAPIToken',
        (tester) async {
      final fakeClient = _FakeApiClient()..tokensResult = [_kTokenA];

      await _pumpApiTokensScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('api_tokens_fab')));
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('api_tokens_create_cancel')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('api_tokens_create_dialog')), findsNothing);
      expect(fakeClient.createdName, isNull);
    });

    testWidgets('shows validation error for empty name', (tester) async {
      final fakeClient = _FakeApiClient()..tokensResult = [_kTokenA];

      await _pumpApiTokensScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('api_tokens_fab')));
      await tester.pumpAndSettle();

      // Submit without entering a name.
      await tester.tap(find.byKey(const Key('api_tokens_create_submit')));
      await tester.pumpAndSettle();

      expect(find.text('Token name is required.'), findsOneWidget);
      // Dialog still open.
      expect(find.byKey(const Key('api_tokens_create_dialog')), findsOneWidget);
    });

    testWidgets('submits and shows plaintext token dialog on success',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..tokensResult = [_kTokenA]
        ..createResult = {
          'id': 99,
          'name': 'my-token',
          'token': 'secret-plaintext-token',
          'created_at': '2026-05-22T12:00:00Z',
        };

      await _pumpApiTokensScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('api_tokens_fab')));
      await tester.pumpAndSettle();

      await tester.enterText(
        find.byKey(const Key('api_tokens_create_name')),
        'my-token',
      );
      await tester.tap(find.byKey(const Key('api_tokens_create_submit')));
      await tester.pumpAndSettle();

      // Create dialog dismissed.
      expect(find.byKey(const Key('api_tokens_create_dialog')), findsNothing);

      // Plaintext dialog shown with the one-time token.
      expect(find.byKey(const Key('api_tokens_plaintext_dialog')), findsOneWidget);
      expect(find.byKey(const Key('api_tokens_plaintext_value')), findsOneWidget);
      expect(find.text('secret-plaintext-token'), findsOneWidget);

      expect(fakeClient.createdName, equals('my-token'));
    });
  });

  // --------------------------------------------------------------------------
  // Plaintext token dialog
  // --------------------------------------------------------------------------

  group('plaintext token dialog', () {
    testWidgets('copy button writes token to clipboard and shows SnackBar',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..tokensResult = []
        ..createResult = {
          'id': 10,
          'name': 'clip-test',
          'token': 'clipboard-token-value',
          'created_at': '2026-05-22T12:00:00Z',
        };

      final List<MethodCall> clipboardCalls = [];
      tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        SystemChannels.platform,
        (call) async {
          clipboardCalls.add(call);
          return null;
        },
      );

      await _pumpApiTokensScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('api_tokens_fab')));
      await tester.pumpAndSettle();

      await tester.enterText(
        find.byKey(const Key('api_tokens_create_name')),
        'clip-test',
      );
      await tester.tap(find.byKey(const Key('api_tokens_create_submit')));
      await tester.pumpAndSettle();

      // Plaintext dialog is open.
      expect(find.byKey(const Key('api_tokens_plaintext_dialog')), findsOneWidget);

      // Tap the copy button.
      await tester.tap(find.byKey(const Key('api_tokens_plaintext_copy')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 300));

      // Clipboard.setData was called with the plaintext value.
      final setDataCall = clipboardCalls.firstWhere(
        (c) => c.method == 'Clipboard.setData',
        orElse: () => throw TestFailure('Clipboard.setData was not called'),
      );
      final text = (setDataCall.arguments as Map)['text'] as String?;
      expect(text, equals('clipboard-token-value'));

      // Copy SnackBar shown.
      expect(find.byKey(const Key('api_tokens_copy_snackbar')), findsOneWidget);

      // Restore the default handler.
      tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        SystemChannels.platform,
        null,
      );
    });

    testWidgets('Done button dismisses the plaintext dialog', (tester) async {
      final fakeClient = _FakeApiClient()
        ..tokensResult = []
        ..createResult = {
          'id': 11,
          'name': 'done-test',
          'token': 'done-token-value',
          'created_at': '2026-05-22T12:00:00Z',
        };

      await _pumpApiTokensScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('api_tokens_fab')));
      await tester.pumpAndSettle();

      await tester.enterText(
        find.byKey(const Key('api_tokens_create_name')),
        'done-test',
      );
      await tester.tap(find.byKey(const Key('api_tokens_create_submit')));
      await tester.pumpAndSettle();

      // Tap Done.
      await tester.tap(find.byKey(const Key('api_tokens_plaintext_done')));
      await tester.pumpAndSettle();

      // Plaintext dialog dismissed.
      expect(find.byKey(const Key('api_tokens_plaintext_dialog')), findsNothing);
    });
  });

  // --------------------------------------------------------------------------
  // Create optimistic UI
  // --------------------------------------------------------------------------

  group('create optimistic UI', () {
    testWidgets(
        'placeholder visible while createAPIToken is in flight, replaced on success',
        (tester) async {
      final serverToken = <String, dynamic>{
        'id': 42,
        'name': 'new-token',
        'token': 'plaintext42',
        'created_at': '2026-05-22T12:00:00Z',
      };
      final fakeClient = _DelayedCreateApiClient(initialTokens: [_kTokenA]);

      await _pumpApiTokensScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Open the create dialog, fill name, submit.
      await tester.tap(find.byKey(const Key('api_tokens_fab')));
      await tester.pumpAndSettle();

      await tester.enterText(
        find.byKey(const Key('api_tokens_create_name')),
        'new-token',
      );
      await tester.tap(find.byKey(const Key('api_tokens_create_submit')));

      // Allow the dialog pop animation to complete but not the pending Future.
      await tester.pump(const Duration(milliseconds: 300));
      await tester.pump(const Duration(milliseconds: 300));

      // Dialog dismissed.
      expect(find.byKey(const Key('api_tokens_create_dialog')), findsNothing);

      // Placeholder row (id=0) visible.
      expect(find.byKey(const Key('api_token_tile_0')), findsOneWidget);
      expect(find.text('new-token'), findsOneWidget);

      // Resolve the pending createAPIToken call.
      fakeClient.completeCreate(serverToken);
      await tester.pumpAndSettle();

      // Placeholder replaced by real row; also the plaintext dialog appears.
      expect(find.byKey(const Key('api_token_tile_0')), findsNothing);
      expect(find.byKey(const Key('api_token_tile_42')), findsOneWidget);
    });

    testWidgets(
        'placeholder reverted and error SnackBar shown on createAPIToken failure',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..tokensResult = [_kTokenA]
        ..createError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/auth/tokens'),
          response: Response(
            requestOptions: RequestOptions(path: '/api/v1/auth/tokens'),
            statusCode: 400,
            data: <String, dynamic>{'message': 'name required'},
          ),
          type: DioExceptionType.badResponse,
        );

      await _pumpApiTokensScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('api_tokens_fab')));
      await tester.pumpAndSettle();

      await tester.enterText(
        find.byKey(const Key('api_tokens_create_name')),
        'fail-token',
      );
      await tester.tap(find.byKey(const Key('api_tokens_create_submit')));
      await tester.pumpAndSettle();

      // Placeholder removed; only token A (id=1) still in the list.
      expect(find.byKey(const Key('api_token_tile_1')), findsOneWidget);
      expect(find.byKey(const Key('api_token_tile_0')), findsNothing);

      // Error SnackBar shown.
      expect(find.byKey(const Key('api_tokens_error_snackbar')), findsOneWidget);
      expect(find.textContaining('name required'), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Empty state
  // --------------------------------------------------------------------------

  group('empty state', () {
    testWidgets('shows empty-state widget when listAPITokens returns []',
        (tester) async {
      final fakeClient = _FakeApiClient()..tokensResult = [];

      await _pumpApiTokensScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('api_tokens_empty')), findsOneWidget);
      expect(find.byKey(const Key('api_tokens_list')), findsNothing);
      expect(find.byKey(const Key('api_tokens_loading')), findsNothing);
    });
  });

  // --------------------------------------------------------------------------
  // Error state
  // --------------------------------------------------------------------------

  group('error state', () {
    testWidgets('shows error message when listAPITokens throws a network error',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..tokensError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/auth/tokens'),
          type: DioExceptionType.connectionError,
        );

      await _pumpApiTokensScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('api_tokens_error')), findsOneWidget);
      expect(find.byKey(const Key('api_tokens_list')), findsNothing);
      expect(find.textContaining('Could not reach the server'), findsOneWidget);
    });

    testWidgets('retry button re-calls listAPITokens after an error',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..tokensError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/auth/tokens'),
          type: DioExceptionType.connectionError,
        );

      await _pumpApiTokensScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('api_tokens_retry')), findsOneWidget);

      // Fix the error before retry.
      fakeClient
        ..tokensError = null
        ..tokensResult = [_kTokenA];

      await tester.tap(find.byKey(const Key('api_tokens_retry')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('api_tokens_list')), findsOneWidget);
      // Called twice: once on init, once on retry.
      expect(fakeClient.listTokensCallCount, equals(2));
    });
  });

  // --------------------------------------------------------------------------
  // apiTokenErrorMessage unit tests
  // --------------------------------------------------------------------------

  group('apiTokenErrorMessage', () {
    test('returns connectivity message for connectionError', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/api/v1/auth/tokens'),
        type: DioExceptionType.connectionError,
      );
      expect(apiTokenErrorMessage(err), contains('Could not reach the server'));
    });

    test('returns server body message for 400 when body has message field', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/api/v1/auth/tokens'),
        response: Response(
          requestOptions: RequestOptions(path: '/api/v1/auth/tokens'),
          statusCode: 400,
          data: <String, dynamic>{'message': 'name required'},
        ),
        type: DioExceptionType.badResponse,
      );
      expect(apiTokenErrorMessage(err), equals('name required'));
    });

    test('returns generic invalid-request message for 400 without body', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/api/v1/auth/tokens'),
        response: Response(
          requestOptions: RequestOptions(path: '/api/v1/auth/tokens'),
          statusCode: 400,
        ),
        type: DioExceptionType.badResponse,
      );
      expect(apiTokenErrorMessage(err), contains('Invalid request'));
    });

    test('returns not-found message for 404', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/api/v1/auth/tokens/99'),
        response: Response(
          requestOptions: RequestOptions(path: '/api/v1/auth/tokens/99'),
          statusCode: 404,
        ),
        type: DioExceptionType.badResponse,
      );
      expect(apiTokenErrorMessage(err), contains('not found'));
    });

    test('returns server-error message for 500', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/api/v1/auth/tokens'),
        response: Response(
          requestOptions: RequestOptions(path: '/api/v1/auth/tokens'),
          statusCode: 500,
        ),
        type: DioExceptionType.badResponse,
      );
      expect(apiTokenErrorMessage(err), contains('500'));
    });

    test('returns generic message for non-Dio error', () {
      expect(
        apiTokenErrorMessage(Exception('boom')),
        contains('Unexpected error'),
      );
    });
  });
}
