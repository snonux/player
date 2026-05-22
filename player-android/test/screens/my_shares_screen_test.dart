// Widget tests for MySharesScreen (my_shares_screen.dart).
//
// Tests cover:
//   1. Renders a loading indicator while listMyShares is in flight.
//   2. Renders a list of shares after a successful load.
//   3. Copy-link writes the share URL to the clipboard and shows a SnackBar.
//   4. Revoke removes the share from the list optimistically.
//   5. Revoke reverts the optimistic removal on API error.
//   6. Empty state is shown when listMyShares returns [].
//   7. Error state is shown when listMyShares throws.
//   8. Retry button re-calls listMyShares.
//
// Riverpod providers are overridden with fakes so tests run without a real
// server or OS keychain.
//
// Run with: flutter test test/screens/my_shares_screen_test.dart

import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/models/models.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/screens/my_shares_screen.dart';
import 'package:player_android/utils/error_mappers.dart';

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

/// In-memory [TokenStorage] used to avoid the platform-specific OS keychain.
class _FakeTokenStorage implements TokenStorage {
  const _FakeTokenStorage();

  @override
  Future<String?> readToken() async => 'test-token';

  @override
  Future<void> writeToken(String token) async {}

  @override
  Future<void> deleteToken() async {}
}

/// Controllable [PlayerApiClient] stub for [MySharesScreen] tests.
///
/// [listMyShares] and [revokeShare] are the primary subjects.  [shareUrl] is
/// also overridden so copy-link tests can assert the produced URL without
/// depending on the Dio base URL.  All other methods remain [UnimplementedError].
class _FakeApiClient extends PlayerApiClient {
  _FakeApiClient()
      : super(dio: Dio(BaseOptions(baseUrl: 'http://test.local')));

  // ---- listMyShares ----

  /// When non-null, [listMyShares] returns this list.
  List<Share>? sharesResult;

  /// When non-null, [listMyShares] throws this instead of returning.
  Object? sharesError;

  /// Number of times [listMyShares] has been called.
  int listMySharesCallCount = 0;

  @override
  Future<List<Share>> listMyShares() async {
    listMySharesCallCount++;
    if (sharesError != null) throw sharesError!;
    return sharesResult!;
  }

  // ---- revokeShare ----

  /// When non-null, [revokeShare] throws this instead of returning normally.
  Object? revokeError;

  /// The token passed to the last [revokeShare] call.
  String? revokedToken;

  @override
  Future<void> revokeShare(String token) async {
    revokedToken = token;
    if (revokeError != null) throw revokeError!;
  }

  // ---- shareUrl ----

  @override
  String shareUrl(String token) => 'http://test.local/s/$token';
}

/// Controllable [PlayerApiClient] stub that delays [listMyShares] until
/// [complete] is called — used to inspect the mid-flight loading state.
class _DelayedFakeApiClient extends PlayerApiClient {
  _DelayedFakeApiClient() : super(dio: Dio());

  final _completer = Completer<List<Share>>();

  /// Resolves the pending [listMyShares] with [shares].
  void complete(List<Share> shares) => _completer.complete(shares);

  @override
  Future<List<Share>> listMyShares() => _completer.future;
}

// ---------------------------------------------------------------------------
// Sample data
// ---------------------------------------------------------------------------

/// A share with all optional fields set.
final _kShareA = Share(
  token: 'tok_aaa',
  mediaId: 1,
  createdBy: 1,
  usedCount: 2,
  maxUses: 10,
  expiresAt: DateTime(2026, 12, 31),
  fileName: 'movie.mp4',
  mediaType: 'video',
);

/// A share without an expiry or max-uses limit.
const _kShareB = Share(
  token: 'tok_bbb',
  mediaId: 2,
  createdBy: 1,
  usedCount: 0,
  fileName: 'podcast.mp3',
  mediaType: 'audio',
);

// ---------------------------------------------------------------------------
// Helper: pump MySharesScreen inside a minimal ProviderScope.
// ---------------------------------------------------------------------------

/// Pumps [MySharesScreen] inside a [ProviderScope] that overrides
/// [apiClientProvider] and [tokenStorageProvider] with fakes.
Future<void> _pumpMySharesScreen(
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
        home: MySharesScreen(),
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
    testWidgets('shows loading indicator while listMyShares is in flight',
        (tester) async {
      final fakeClient = _DelayedFakeApiClient();

      await _pumpMySharesScreen(tester, fakeClient);

      // Pump a single frame: initState → addPostFrameCallback fires, but the
      // Future has not resolved yet.
      await tester.pump();

      expect(find.byKey(const Key('shares_loading')), findsOneWidget);
      expect(find.byType(CircularProgressIndicator), findsOneWidget);

      // Resolve to avoid "async work pending" warnings.
      fakeClient.complete([_kShareA]);
      await tester.pumpAndSettle();
    });
  });

  // --------------------------------------------------------------------------
  // Renders share list
  // --------------------------------------------------------------------------

  group('renders share list', () {
    testWidgets('shows a tile for each share returned by listMyShares',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..sharesResult = [_kShareA, _kShareB];

      await _pumpMySharesScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Both filenames must be visible.
      expect(find.text('movie.mp4'), findsOneWidget);
      expect(find.text('podcast.mp3'), findsOneWidget);

      // Tile widgets keyed by token.
      expect(find.byKey(const Key('share_tile_tok_aaa')), findsOneWidget);
      expect(find.byKey(const Key('share_tile_tok_bbb')), findsOneWidget);
    });

    testWidgets('renders list widget after a successful load', (tester) async {
      final fakeClient = _FakeApiClient()..sharesResult = [_kShareA];

      await _pumpMySharesScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('shares_list')), findsOneWidget);
    });

    testWidgets('shows formatted expiry date when expiresAt is set',
        (tester) async {
      final fakeClient = _FakeApiClient()..sharesResult = [_kShareA];

      await _pumpMySharesScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // _kShareA.expiresAt == DateTime(2026, 12, 31).
      expect(find.textContaining('2026-12-31'), findsOneWidget);
    });

    testWidgets('shows "No expiry" when expiresAt is null', (tester) async {
      final fakeClient = _FakeApiClient()..sharesResult = [_kShareB];

      await _pumpMySharesScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.textContaining('No expiry'), findsOneWidget);
    });

    testWidgets('shows used count and max uses when maxUses is set',
        (tester) async {
      final fakeClient = _FakeApiClient()..sharesResult = [_kShareA];

      await _pumpMySharesScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // _kShareA.usedCount=2, maxUses=10 → "2/10 uses".
      expect(find.textContaining('2/10 uses'), findsOneWidget);
    });

    testWidgets('shows used count without denominator when maxUses is null',
        (tester) async {
      final fakeClient = _FakeApiClient()..sharesResult = [_kShareB];

      await _pumpMySharesScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // _kShareB.usedCount=0, maxUses=null → "0 uses".
      expect(find.textContaining('0 uses'), findsOneWidget);
    });

    testWidgets('shows "All shares loaded" footer after a successful load',
        (tester) async {
      // The footer is always rendered (MySharesScreen has no pagination) once
      // the list is non-empty.  Verify the key is present in the widget tree.
      final fakeClient = _FakeApiClient()..sharesResult = [_kShareA, _kShareB];

      await _pumpMySharesScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('shares_no_more')), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Copy-link action
  // --------------------------------------------------------------------------

  group('copy-link action', () {
    testWidgets('copy-link writes share URL to clipboard and shows SnackBar',
        (tester) async {
      final fakeClient = _FakeApiClient()..sharesResult = [_kShareA];

      // Intercept clipboard calls.
      final List<MethodCall> clipboardCalls = [];
      tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        SystemChannels.platform,
        (call) async {
          clipboardCalls.add(call);
          return null;
        },
      );

      await _pumpMySharesScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Tap the copy-link button for _kShareA.
      await tester.tap(find.byKey(const Key('share_copy_tok_aaa')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 300));

      // Clipboard.setData was called with the correct URL.
      final setDataCall = clipboardCalls.firstWhere(
        (c) => c.method == 'Clipboard.setData',
        orElse: () => throw TestFailure('Clipboard.setData was not called'),
      );
      final text = (setDataCall.arguments as Map)['text'] as String?;
      expect(text, equals('http://test.local/s/tok_aaa'));

      // A SnackBar confirming the copy is visible.
      expect(find.byKey(const Key('shares_copy_snackbar')), findsOneWidget);
      expect(
        find.textContaining('http://test.local/s/tok_aaa'),
        findsOneWidget,
      );

      // Restore the default handler.
      tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        SystemChannels.platform,
        null,
      );
    });
  });

  // --------------------------------------------------------------------------
  // Revoke action
  // --------------------------------------------------------------------------

  group('revoke action', () {
    testWidgets('revoke removes the share from the list optimistically',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..sharesResult = [_kShareA, _kShareB];

      await _pumpMySharesScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Both tiles visible before revoke.
      expect(find.byKey(const Key('share_tile_tok_aaa')), findsOneWidget);
      expect(find.byKey(const Key('share_tile_tok_bbb')), findsOneWidget);

      // Tap revoke for _kShareA.
      await tester.tap(find.byKey(const Key('share_revoke_tok_aaa')));
      await tester.pumpAndSettle();

      // _kShareA is gone; _kShareB remains.
      expect(find.byKey(const Key('share_tile_tok_aaa')), findsNothing);
      expect(find.byKey(const Key('share_tile_tok_bbb')), findsOneWidget);

      // Confirmation SnackBar is shown.
      expect(find.byKey(const Key('shares_revoke_snackbar')), findsOneWidget);

      // revokeShare was called with the correct token.
      expect(fakeClient.revokedToken, equals('tok_aaa'));
    });

    testWidgets('revoke reverts optimistic removal on API error', (tester) async {
      final fakeClient = _FakeApiClient()
        ..sharesResult = [_kShareA, _kShareB]
        ..revokeError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/shares/tok_aaa'),
          type: DioExceptionType.connectionError,
        );

      await _pumpMySharesScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Tap revoke for _kShareA.
      await tester.tap(find.byKey(const Key('share_revoke_tok_aaa')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 300));

      // _kShareA is re-inserted after the error.
      expect(find.byKey(const Key('share_tile_tok_aaa')), findsOneWidget);

      // Error SnackBar is shown.
      expect(
        find.byKey(const Key('shares_revoke_error_snackbar')),
        findsOneWidget,
      );
      expect(
        find.textContaining('Could not reach the server'),
        findsOneWidget,
      );
    });
  });

  // --------------------------------------------------------------------------
  // Empty state
  // --------------------------------------------------------------------------

  group('empty state', () {
    testWidgets('shows empty-state widget when listMyShares returns []',
        (tester) async {
      final fakeClient = _FakeApiClient()..sharesResult = [];

      await _pumpMySharesScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('shares_empty')), findsOneWidget);
      expect(find.byKey(const Key('shares_list')), findsNothing);
      expect(find.byKey(const Key('shares_loading')), findsNothing);
    });
  });

  // --------------------------------------------------------------------------
  // Error state
  // --------------------------------------------------------------------------

  group('error state', () {
    testWidgets('shows error message when listMyShares throws a network error',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..sharesError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/shares'),
          type: DioExceptionType.connectionError,
        );

      await _pumpMySharesScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('shares_error')), findsOneWidget);
      expect(find.byKey(const Key('shares_list')), findsNothing);
      expect(
        find.textContaining('Could not reach the server'),
        findsOneWidget,
      );
    });

    testWidgets('shows retry button on error and retry calls listMyShares again',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..sharesError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/shares'),
          type: DioExceptionType.connectionError,
        );

      await _pumpMySharesScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('shares_retry')), findsOneWidget);

      // Fix the error before tapping retry so the second call succeeds.
      fakeClient
        ..sharesError = null
        ..sharesResult = [_kShareA];

      await tester.tap(find.byKey(const Key('shares_retry')));
      await tester.pumpAndSettle();

      // After a successful retry the list is shown.
      expect(find.byKey(const Key('shares_list')), findsOneWidget);
      // listMyShares was called twice: once on init, once on retry.
      expect(fakeClient.listMySharesCallCount, equals(2));
    });
  });

  // --------------------------------------------------------------------------
  // sharesErrorMessage helper
  // --------------------------------------------------------------------------

  group('sharesErrorMessage', () {
    test('returns connectivity message for connectionError', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/api/v1/shares'),
        type: DioExceptionType.connectionError,
      );
      expect(sharesErrorMessage(err), contains('Could not reach the server'));
    });

    test('returns not-found message for 404', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/api/v1/shares/tok_abc'),
        response: Response(
          requestOptions: RequestOptions(path: '/api/v1/shares/tok_abc'),
          statusCode: 404,
        ),
        type: DioExceptionType.badResponse,
      );
      expect(sharesErrorMessage(err), contains('Share not found'));
    });

    test('returns permission message for 403', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/api/v1/shares/tok_abc'),
        response: Response(
          requestOptions: RequestOptions(path: '/api/v1/shares/tok_abc'),
          statusCode: 403,
        ),
        type: DioExceptionType.badResponse,
      );
      expect(sharesErrorMessage(err), contains('permission'));
    });

    test('returns server-error message for 500 badResponse', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/api/v1/shares'),
        response: Response(
          requestOptions: RequestOptions(path: '/api/v1/shares'),
          statusCode: 500,
        ),
        type: DioExceptionType.badResponse,
      );
      expect(sharesErrorMessage(err), contains('500'));
    });

    test('returns generic message for non-Dio error', () {
      expect(sharesErrorMessage(Exception('boom')), contains('Unexpected error'));
    });
  });
}
