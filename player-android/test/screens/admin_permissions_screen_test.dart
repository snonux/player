// Widget tests for AdminPermissionsScreen (admin_permissions_screen.dart).
//
// Tests cover:
//   1. User editor renders with user picker and searchable set roles.
//   2. Selecting Viewer calls grantPermission with role viewer.
//   3. Selecting Owner calls grantPermission with role owner.
//   4. Selecting — calls revokePermission.
//   5. Admin users show a banner; role dropdowns are disabled.
//   6. Optimistic role change reverts on error.
//   7. Loading spinner shown before first data fetch completes.
//   8. Empty-state shown when no users or sets are returned.
//   9. Error-state shown when the load throws.
//  10. Advanced matrix is reachable from the overflow menu.
//  11. Set search filters the set list.
//  12. Set search text survives Advanced matrix round-trip.
//  13. User picker filters by username.
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
class _FakeApiClient extends PlayerApiClient {
  _FakeApiClient() : super(dio: Dio(BaseOptions(baseUrl: 'http://test.local')));

  List<User>? usersResult;
  Object? usersError;

  @override
  Future<List<User>> listUsers() async {
    if (usersError != null) throw usersError!;
    return usersResult!;
  }

  List<MediaSet>? setsResult;
  Object? setsError;

  @override
  Future<List<MediaSet>> listSets() async {
    if (setsError != null) throw setsError!;
    return setsResult!;
  }

  Map<String, dynamic>? permsResult;
  Object? permsError;

  @override
  Future<Map<String, dynamic>> listPermissions() async {
    if (permsError != null) throw permsError!;
    return permsResult!;
  }

  Object? grantError;
  int? grantedUserId;
  int? grantedSetId;
  String? grantedRole;
  int grantCallCount = 0;

  @override
  Future<void> grantPermission({
    required int setId,
    required int userId,
    required String role,
  }) async {
    grantedUserId = userId;
    grantedSetId = setId;
    grantedRole = role;
    grantCallCount++;
    if (grantError != null) throw grantError!;
  }

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

/// Suspends all three parallel loads until [complete] is called.
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

User _makeUser({required int id, required String username, bool isAdmin = false}) {
  return User(id: id, username: username, isAdmin: isAdmin);
}

MediaSet _makeSet({required int id, required String name}) {
  return MediaSet(
    id: id,
    name: name,
    rootPath: '/media/$name',
    coverThumbnailPath: '',
    isPodcast: false,
  );
}

Map<String, dynamic> _emptyPerms() => {'permissions': <dynamic>[]};

Map<String, dynamic> _permsWithGrant({
  required int userId,
  required int setId,
  String role = 'viewer',
}) {
  return {
    'permissions': [
      {'user_id': userId, 'set_id': setId, 'role': role},
    ],
  };
}

final _kAdmin = _makeUser(id: 1, username: 'alice', isAdmin: true);
final _kBob = _makeUser(id: 2, username: 'bob');
final _kSetA = _makeSet(id: 10, name: 'Movies');
final _kSetB = _makeSet(id: 20, name: 'Music');

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

/// Finds the [DropdownButton] under the role widget keyed by [permKey].
Finder _roleDropdown(Key permKey) {
  return find.descendant(
    of: find.byKey(permKey),
    matching: find.byType(DropdownButton<String>),
  );
}

/// Opens the role dropdown keyed by [permKey] and taps the menu item with
/// [label] (—, Viewer, or Owner).
Future<void> _selectRole(
  WidgetTester tester, {
  required Key permKey,
  required String label,
}) async {
  await tester.tap(_roleDropdown(permKey));
  await tester.pumpAndSettle();
  await tester.tap(find.text(label).last);
  await tester.pumpAndSettle();
}

void main() {
  group('loading state', () {
    testWidgets('shows loading spinner while parallel loads are in flight',
        (tester) async {
      final fakeClient = _DelayedFakeApiClient();

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pump();

      expect(find.byKey(const Key('admin_perms_loading')), findsOneWidget);
      expect(find.byType(CircularProgressIndicator), findsOneWidget);

      fakeClient.complete(
        users: [_kAdmin, _kBob],
        sets: [_kSetA],
        perms: _emptyPerms(),
      );
      await tester.pumpAndSettle();
    });
  });

  group('user editor', () {
    testWidgets('renders user picker and set roles for the default user',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kAdmin, _kBob]
        ..setsResult = [_kSetA, _kSetB]
        ..permsResult = _emptyPerms();

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('admin_perms_user_select')), findsOneWidget);
      expect(find.byKey(const Key('admin_perms_set_search')), findsOneWidget);
      // Default selection prefers the first non-admin (bob).
      expect(find.byKey(const Key('perm_2_10')), findsOneWidget);
      expect(find.byKey(const Key('perm_2_20')), findsOneWidget);
      expect(find.text('Movies'), findsOneWidget);
      expect(find.text('Music'), findsOneWidget);
      // Matrix is not shown by default.
      expect(find.byKey(const Key('admin_perms_table')), findsNothing);
    });

    testWidgets('set search filters the list', (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kBob]
        ..setsResult = [_kSetA, _kSetB]
        ..permsResult = _emptyPerms();

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.enterText(
        find.byKey(const Key('admin_perms_set_search')),
        'mus',
      );
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('admin_perms_set_tile_20')), findsOneWidget);
      expect(find.byKey(const Key('admin_perms_set_tile_10')), findsNothing);
      expect(find.text('Music'), findsWidgets);
    });

    testWidgets('set search text survives Advanced matrix round-trip',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kBob]
        ..setsResult = [_kSetA, _kSetB]
        ..permsResult = _emptyPerms();

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.enterText(
        find.byKey(const Key('admin_perms_set_search')),
        'mus',
      );
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('admin_perms_menu')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Advanced matrix'));
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('admin_perms_table')), findsOneWidget);

      await tester.tap(find.byKey(const Key('admin_perms_exit_matrix')));
      await tester.pumpAndSettle();

      final search = tester.widget<TextField>(
        find.byKey(const Key('admin_perms_set_search')),
      );
      expect(search.controller?.text, equals('mus'));
      expect(find.byKey(const Key('admin_perms_set_tile_20')), findsOneWidget);
      expect(find.byKey(const Key('admin_perms_set_tile_10')), findsNothing);
    });

    testWidgets('user picker filters by username', (tester) async {
      final carol = _makeUser(id: 3, username: 'carol');
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kAdmin, _kBob, carol]
        ..setsResult = [_kSetA]
        ..permsResult = _emptyPerms();

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('admin_perms_user_search')));
      await tester.pumpAndSettle();
      await tester.enterText(
        find.byKey(const Key('admin_perms_user_search')),
        'car',
      );
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('admin_perms_user_option_3')), findsOneWidget);
      expect(find.byKey(const Key('admin_perms_user_option_2')), findsNothing);
      expect(find.byKey(const Key('admin_perms_user_option_1')), findsNothing);

      await tester.tap(find.byKey(const Key('admin_perms_user_option_3')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('perm_3_10')), findsOneWidget);
      expect(find.byKey(const Key('perm_2_10')), findsNothing);
    });

    testWidgets('role dropdown shows viewer when permission exists',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kBob]
        ..setsResult = [_kSetA]
        ..permsResult = _permsWithGrant(userId: 2, setId: 10);

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      final dropdown = tester.widget<DropdownButton<String>>(
        _roleDropdown(const Key('perm_2_10')),
      );
      expect(dropdown.value, equals('viewer'));
    });

    testWidgets('role dropdown shows owner when owner permission exists',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kBob]
        ..setsResult = [_kSetA]
        ..permsResult =
            _permsWithGrant(userId: 2, setId: 10, role: 'owner');

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      final dropdown = tester.widget<DropdownButton<String>>(
        _roleDropdown(const Key('perm_2_10')),
      );
      expect(dropdown.value, equals('owner'));
    });
  });

  group('grant permission', () {
    testWidgets('selecting Viewer calls grantPermission with viewer',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kBob]
        ..setsResult = [_kSetA]
        ..permsResult = _emptyPerms();

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await _selectRole(
        tester,
        permKey: const Key('perm_2_10'),
        label: 'Viewer',
      );

      expect(fakeClient.grantCallCount, equals(1));
      expect(fakeClient.grantedUserId, equals(2));
      expect(fakeClient.grantedSetId, equals(10));
      expect(fakeClient.grantedRole, equals('viewer'));
    });

    testWidgets('selecting Owner calls grantPermission with owner',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kBob]
        ..setsResult = [_kSetA]
        ..permsResult = _emptyPerms();

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await _selectRole(
        tester,
        permKey: const Key('perm_2_10'),
        label: 'Owner',
      );

      expect(fakeClient.grantCallCount, equals(1));
      expect(fakeClient.grantedRole, equals('owner'));
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

      await _selectRole(
        tester,
        permKey: const Key('perm_2_10'),
        label: 'Viewer',
      );

      final dropdown = tester.widget<DropdownButton<String>>(
        _roleDropdown(const Key('perm_2_10')),
      );
      expect(dropdown.value, equals(''));

      expect(
        find.byKey(const Key('admin_perms_error_snackbar')),
        findsOneWidget,
      );
      expect(find.textContaining('permission'), findsOneWidget);
    });
  });

  group('revoke permission', () {
    testWidgets('selecting — calls revokePermission', (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kBob]
        ..setsResult = [_kSetA]
        ..permsResult = _permsWithGrant(userId: 2, setId: 10);

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await _selectRole(
        tester,
        permKey: const Key('perm_2_10'),
        label: '—',
      );

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

      await _selectRole(
        tester,
        permKey: const Key('perm_2_10'),
        label: '—',
      );

      final dropdown = tester.widget<DropdownButton<String>>(
        _roleDropdown(const Key('perm_2_10')),
      );
      expect(dropdown.value, equals('viewer'));

      expect(
        find.byKey(const Key('admin_perms_error_snackbar')),
        findsOneWidget,
      );
      expect(find.textContaining('not found'), findsOneWidget);
    });
  });

  group('admin users', () {
    testWidgets('admin selection shows banner and disabled role dropdown',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kAdmin]
        ..setsResult = [_kSetA]
        ..permsResult = _emptyPerms();

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('admin_perms_admin_banner')), findsOneWidget);

      final dropdown = tester.widget<DropdownButton<String>>(
        _roleDropdown(const Key('perm_1_10')),
      );
      expect(dropdown.onChanged, isNull);
      expect(dropdown.value, equals('owner'));
    });

    testWidgets('disabled admin dropdown does not call grantPermission',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kAdmin]
        ..setsResult = [_kSetA]
        ..permsResult = _emptyPerms();

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(_roleDropdown(const Key('perm_1_10')));
      await tester.pumpAndSettle();

      expect(fakeClient.grantCallCount, equals(0));
      expect(fakeClient.revokeCallCount, equals(0));
    });
  });

  group('advanced matrix', () {
    testWidgets('overflow menu opens the matrix with role dropdowns',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kAdmin, _kBob]
        ..setsResult = [_kSetA, _kSetB]
        ..permsResult = _emptyPerms();

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('admin_perms_menu')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Advanced matrix'));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('admin_perms_table')), findsOneWidget);
      expect(find.text('Permissions matrix'), findsOneWidget);
      // Both users appear in the matrix.
      expect(find.text('alice'), findsOneWidget);
      expect(find.text('bob'), findsWidgets);
      expect(find.byKey(const Key('perm_2_10')), findsOneWidget);
      expect(find.byKey(const Key('perm_1_10')), findsOneWidget);
    });

    testWidgets('matrix grant still calls grantPermission', (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kBob]
        ..setsResult = [_kSetA]
        ..permsResult = _emptyPerms();

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('admin_perms_menu')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Advanced matrix'));
      await tester.pumpAndSettle();

      await _selectRole(
        tester,
        permKey: const Key('perm_2_10'),
        label: 'Owner',
      );

      expect(fakeClient.grantCallCount, equals(1));
      expect(fakeClient.grantedRole, equals('owner'));
    });
  });

  group('empty state', () {
    testWidgets('shows empty-state when listUsers returns []', (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = []
        ..setsResult = [_kSetA]
        ..permsResult = _emptyPerms();

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('admin_perms_empty')), findsOneWidget);
      expect(find.byKey(const Key('admin_perms_user_select')), findsNothing);
    });

    testWidgets('shows empty-state when listSets returns []', (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kBob]
        ..setsResult = []
        ..permsResult = _emptyPerms();

      await _pumpPermsScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('admin_perms_empty')), findsOneWidget);
    });
  });

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

      fakeClient
        ..usersError = null
        ..usersResult = [_kBob];

      await tester.tap(find.byKey(const Key('admin_perms_retry')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('admin_perms_user_select')), findsOneWidget);
    });
  });
}
