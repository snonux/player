// Widget tests for AdminPermissionsScreen (admin_permissions_screen.dart).
//
// Tests cover:
//   1. Permission matrix renders with user rows and set columns.
//   2. Checking a checkbox calls grantPermission.
//   3. Unchecking a checkbox calls revokePermission.
//   4. Admin rows are disabled (cannot toggle).
//   5. Optimistic toggle reverts on error.
//   6. Loading spinner shown before first data fetch completes.
//   7. Empty-state shown when no users or sets are returned.
//   8. Error-state shown when the load throws.
//
// Riverpod providers are overridden with fakes so tests run without a real
// server or OS keychain.
//
// Run with: flutter test test/screens/admin_permissions_screen_test.dart

import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/models/models.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/screens/admin_permissions_screen.dart';

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

/// In-memory [TokenStorage] that returns a fixed username without hitting
/// the OS keychain.
class _FakeTokenStorage implements TokenStorage {
  @override
  Future<String?> readToken() async => 'admin';

  @override
  Future<void> writeToken(String token) async {}

  @override
  Future<void> deleteToken() async {}
}

/// Controllable [PlayerApiClient] stub for [AdminPermissionsScreen] tests.
///
/// [listUsers], [listSets], [listPermissions], [grantPermission], and
/// [revokePermission] are the primary subjects.  All other methods remain
/// [UnimplementedError] — the screen calls only these.
class _FakeApiClient extends PlayerApiClient {
  _FakeApiClient() : super(dio: Dio(BaseOptions(baseUrl: 'http://test.local')));

  // ---- listUsers ----

  List<User>? usersResult;
  Object? usersError;

  @override
  Future<List<User>> listUsers() async {
    if (usersError != null) throw usersError!;
    return usersResult!;
  }

  // ---- listSets ----

  List<MediaSet>? setsResult;
  Object? setsError;

  @override
  Future<List<MediaSet>> listSets() async {
    if (setsError != null) throw setsError!;
    return setsResult!;
  }

  // ---- listPermissions ----

  Map<String, dynamic>? permsResult;
  Object? permsError;

  @override
  Future<Map<String, dynamic>> listPermissions() async {
    if (permsError != null) throw permsError!;
    return permsResult!;
  }

  // ---- grantPermission ----

  Object? grantError;
  int? grantedUserId;
  int? grantedSetId;
  int grantCallCount = 0;

  @override
  Future<void> grantPermission({
    required int setId,
    required int userId,
    required String role,
  }) async {
    grantedUserId = userId;
    grantedSetId = setId;
    grantCallCount++;
    if (grantError != null) throw grantError!;
  }

  // ---- revokePermission ----

  Object? revokeError;
  int? revokedUserId;
  int? revokedSetId;
  int revokeCallCount = 0;

  @override
  Future<void> revokePermission({
    required int setId,
    required int userId,
  }) async {
    revokedUserId = userId;
    revokedSetId = setId;
    revokeCallCount++;
    if (revokeError != null) throw revokeError!;
  }
}

/// [PlayerApiClient] stub that suspends all three parallel loads until
/// [complete] is called — used to inspect the mid-flight loading state.
class _DelayedFakeApiClient extends PlayerApiClient {
  _DelayedFakeApiClient() : super(dio: Dio());

  final _usersCompleter = Completer<List<User>>();
  final _setsCompleter = Completer<List<MediaSet>>();
  final _permsCompleter = Completer<Map<String, dynamic>>();

  void complete({
    required List<User> users,
    required List<MediaSet> sets,
    required Map<String, dynamic> perms,
  }) {
    _usersCompleter.complete(users);
    _setsCompleter.complete(sets);
    _permsCompleter.complete(perms);
  }

  @override
  Future<List<User>> listUsers() => _usersCompleter.future;

  @override
  Future<List<MediaSet>> listSets() => _setsCompleter.future;

  @override
  Future<Map<String, dynamic>> listPermissions() => _permsCompleter.future;
}

// ---------------------------------------------------------------------------
// Sample data helpers
// ---------------------------------------------------------------------------

/// Builds a minimal [User] with the given [id], [username], and [isAdmin] flag.
User _makeUser({required int id, required String username, bool isAdmin = false}) {
  return User(id: id, username: username, isAdmin: isAdmin);
}

/// Builds a minimal [MediaSet] with the given [id] and [name].
MediaSet _makeSet({required int id, required String name}) {
  return MediaSet(
    id: id,
    name: name,
    rootPath: '/media/$name',
    coverThumbnailPath: '',
    isPodcast: false,
  );
}

/// Builds an empty permission-matrix response (no explicit permissions).
Map<String, dynamic> _emptyPerms() => {'permissions': <dynamic>[]};

/// Builds a permission-matrix response with a single (userId, setId) pair.
Map<String, dynamic> _permsWithGrant({required int userId, required int setId}) {
  return {
    'permissions': [
      {'user_id': userId, 'set_id': setId, 'role': 'viewer'},
    ],
  };
}

// Pre-built test fixtures.
final _kAdmin = _makeUser(id: 1, username: 'alice', isAdmin: true);
final _kBob = _makeUser(id: 2, username: 'bob');
final _kSetA = _makeSet(id: 10, name: 'Movies');
final _kSetB = _makeSet(id: 20, name: 'Music');

// ---------------------------------------------------------------------------
// Helper: pump AdminPermissionsScreen inside a minimal ProviderScope.
// ---------------------------------------------------------------------------

/// Pumps [AdminPermissionsScreen] with a [ProviderScope] that overrides
/// [apiClientProvider] with [fakeClient] and [tokenStorageProvider] with an
/// in-memory fake.  [MaterialApp] is sufficient because the screen does not
/// navigate away — it only shows SnackBars.
Future<void> _pumpPermsScreen(
  WidgetTester tester,
  PlayerApiClient fakeClient,
) async {
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        tokenStorageProvider.overrideWithValue(_FakeTokenStorage()),
        apiClientProvider.overrideWithValue(fakeClient),
      ],
      child: const MaterialApp(home: AdminPermissionsScreen()),
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
    testWidgets('shows loading spinner while parallel loads are in flight',
        (tester) async {
      final fakeClient = _DelayedFakeApiClient();

      await _pumpPermsScreen(tester, fakeClient);
      // One pump so addPostFrameCallback fires but Futures have not resolved.
      await tester.pump();

      expect(find.byKey(const Key('admin_perms_loading')), findsOneWidget);
      expect(find.byType(CircularProgressIndicator), findsOneWidget);

      // Resolve to avoid dangling-async warnings.
      fakeClient.complete(
        users: [_kAdmin, _kBob],
        sets: [_kSetA],
        perms: _emptyPerms(),
      );
      await tester.pumpAndSettle();
    });
  });

  // --------------------------------------------------------------------------
  // Matrix rendering
  // --------------------------------------------------------------------------

  group('matrix rendering', () {
    testWidgets('renders DataTable with user rows and set columns',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kAdmin, _kBob]
        ..setsResult = [_kSetA, _kSetB]
        ..permsResult = _emptyPerms();

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('admin_perms_table')), findsOneWidget);
      // User names visible in the table.
      expect(find.text('alice'), findsOneWidget);
      expect(find.text('bob'), findsOneWidget);
      // Set names visible as column headers.
      expect(find.text('Movies'), findsOneWidget);
      expect(find.text('Music'), findsOneWidget);
    });

    testWidgets('checkbox checked when permission exists', (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kAdmin, _kBob]
        ..setsResult = [_kSetA]
        ..permsResult = _permsWithGrant(userId: 2, setId: 10);

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // bob (id=2) has permission on setA (id=10) — checkbox should be checked.
      final checkbox = tester.widget<Checkbox>(
        find.byKey(const Key('perm_2_10')),
      );
      expect(checkbox.value, isTrue);
    });

    testWidgets('checkbox unchecked when no permission exists', (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kAdmin, _kBob]
        ..setsResult = [_kSetA]
        ..permsResult = _emptyPerms();

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // bob (id=2) has no permission on setA (id=10) — checkbox should be unchecked.
      final checkbox = tester.widget<Checkbox>(
        find.byKey(const Key('perm_2_10')),
      );
      expect(checkbox.value, isFalse);
    });
  });

  // --------------------------------------------------------------------------
  // Checking a checkbox (grant)
  // --------------------------------------------------------------------------

  group('grant permission', () {
    testWidgets('checking a checkbox calls grantPermission', (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kBob]
        ..setsResult = [_kSetA]
        ..permsResult = _emptyPerms();

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // bob has no permission on setA; tap the checkbox to grant.
      await tester.tap(find.byKey(const Key('perm_2_10')));
      await tester.pumpAndSettle();

      expect(fakeClient.grantCallCount, equals(1));
      expect(fakeClient.grantedUserId, equals(2));
      expect(fakeClient.grantedSetId, equals(10));
    });

    testWidgets('optimistic grant reverts on error and shows SnackBar',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kBob]
        ..setsResult = [_kSetA]
        ..permsResult = _emptyPerms()
        ..grantError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/admin/permissions'),
          response: Response(
            requestOptions: RequestOptions(path: '/api/v1/admin/permissions'),
            statusCode: 403,
          ),
          type: DioExceptionType.badResponse,
        );

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Tap the unchecked checkbox — the optimistic update will check it,
      // then revert it when the API returns 403.
      await tester.tap(find.byKey(const Key('perm_2_10')));
      await tester.pumpAndSettle();

      // After error: checkbox should be reverted to unchecked.
      final checkbox = tester.widget<Checkbox>(
        find.byKey(const Key('perm_2_10')),
      );
      expect(checkbox.value, isFalse);

      // Error SnackBar shown.
      expect(
        find.byKey(const Key('admin_perms_error_snackbar')),
        findsOneWidget,
      );
      expect(find.textContaining('permission'), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Unchecking a checkbox (revoke)
  // --------------------------------------------------------------------------

  group('revoke permission', () {
    testWidgets('unchecking a checkbox calls revokePermission', (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kBob]
        ..setsResult = [_kSetA]
        ..permsResult = _permsWithGrant(userId: 2, setId: 10);

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // bob has permission on setA; tap the checkbox to revoke.
      await tester.tap(find.byKey(const Key('perm_2_10')));
      await tester.pumpAndSettle();

      expect(fakeClient.revokeCallCount, equals(1));
      expect(fakeClient.revokedUserId, equals(2));
      expect(fakeClient.revokedSetId, equals(10));
    });

    testWidgets('optimistic revoke reverts on error and shows SnackBar',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kBob]
        ..setsResult = [_kSetA]
        ..permsResult = _permsWithGrant(userId: 2, setId: 10)
        ..revokeError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/admin/permissions'),
          response: Response(
            requestOptions: RequestOptions(path: '/api/v1/admin/permissions'),
            statusCode: 404,
          ),
          type: DioExceptionType.badResponse,
        );

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Tap the checked checkbox to revoke — the optimistic update unchecks it,
      // then reverts it when the API returns 404.
      await tester.tap(find.byKey(const Key('perm_2_10')));
      await tester.pumpAndSettle();

      // After error: checkbox should be reverted to checked.
      final checkbox = tester.widget<Checkbox>(
        find.byKey(const Key('perm_2_10')),
      );
      expect(checkbox.value, isTrue);

      // Error SnackBar shown.
      expect(
        find.byKey(const Key('admin_perms_error_snackbar')),
        findsOneWidget,
      );
      expect(find.textContaining('not found'), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Admin rows are disabled
  // --------------------------------------------------------------------------

  group('admin rows', () {
    testWidgets('admin user checkbox has null onChanged (disabled)',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kAdmin]
        ..setsResult = [_kSetA]
        ..permsResult = _emptyPerms();

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Admin checkbox for setA — should be disabled.
      final checkbox = tester.widget<Checkbox>(
        find.byKey(const Key('perm_1_10')),
      );
      expect(checkbox.onChanged, isNull);
    });

    testWidgets('admin user checkbox is always checked regardless of perms data',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kAdmin]
        ..setsResult = [_kSetA]
        // Admins have no explicit permission row — but should show as checked.
        ..permsResult = _emptyPerms();

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      final checkbox = tester.widget<Checkbox>(
        find.byKey(const Key('perm_1_10')),
      );
      // Admins always have access; the UI reflects this with a checked, disabled cell.
      expect(checkbox.value, isTrue);
    });

    testWidgets('tapping admin checkbox does NOT call grantPermission',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kAdmin]
        ..setsResult = [_kSetA]
        ..permsResult = _emptyPerms();

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Tapping a disabled Checkbox widget is a no-op — the tap should not
      // propagate to grantPermission.
      await tester.tap(find.byKey(const Key('perm_1_10')));
      await tester.pumpAndSettle();

      expect(fakeClient.grantCallCount, equals(0));
      expect(fakeClient.revokeCallCount, equals(0));
    });
  });

  // --------------------------------------------------------------------------
  // Empty state
  // --------------------------------------------------------------------------

  group('empty state', () {
    testWidgets('shows empty-state when listUsers returns []', (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = []
        ..setsResult = [_kSetA]
        ..permsResult = _emptyPerms();

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('admin_perms_empty')), findsOneWidget);
      expect(find.byKey(const Key('admin_perms_table')), findsNothing);
    });

    testWidgets('shows empty-state when listSets returns []', (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kBob]
        ..setsResult = []
        ..permsResult = _emptyPerms();

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('admin_perms_empty')), findsOneWidget);
      expect(find.byKey(const Key('admin_perms_table')), findsNothing);
    });
  });

  // --------------------------------------------------------------------------
  // Error state
  // --------------------------------------------------------------------------

  group('error state', () {
    testWidgets('shows error when any parallel load throws', (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/admin/users'),
          type: DioExceptionType.connectionError,
        )
        ..setsResult = [_kSetA]
        ..permsResult = _emptyPerms();

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('admin_perms_error')), findsOneWidget);
      expect(find.byKey(const Key('admin_perms_table')), findsNothing);
      expect(find.textContaining('Could not reach the server'), findsOneWidget);
    });

    testWidgets('retry button re-calls load after an error', (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/admin/users'),
          type: DioExceptionType.connectionError,
        )
        ..setsResult = [_kSetA]
        ..permsResult = _emptyPerms();

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('admin_perms_retry')), findsOneWidget);

      // Fix the error so the retry succeeds.
      fakeClient
        ..usersError = null
        ..usersResult = [_kBob];

      await tester.tap(find.byKey(const Key('admin_perms_retry')));
      await tester.pumpAndSettle();

      // After successful retry the matrix is visible.
      expect(find.byKey(const Key('admin_perms_table')), findsOneWidget);
    });
  });
}
