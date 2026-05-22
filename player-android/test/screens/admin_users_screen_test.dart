// Widget tests for AdminUsersScreen (admin_users_screen.dart).
//
// Tests cover:
//   1. Loading state: spinner shown while listUsers is in flight.
//   2. Renders user list after a successful load.
//   3. Self-row: delete button is hidden for the current user's own row.
//   4. Non-self row: delete button is visible for other users.
//   5. Create dialog: opens on FAB tap and submits a new user.
//   6. Create dialog: cancel closes without calling createUser.
//   7. Create dialog: validation — empty username and short password are rejected.
//   8. Create optimistic UI: placeholder visible while in-flight, replaced on success.
//   9. Create optimistic UI: placeholder reverted and error SnackBar shown on failure.
//  10. Delete: confirmation dialog appears; cancel leaves the row; confirm removes it.
//  11. Delete optimistic UI: row removed immediately, reinserted on API error.
//  12. Empty state: shown when listUsers returns [].
//  13. Error state: shown when listUsers throws.
//  14. Retry button re-calls listUsers after an error.
//  15. adminUserErrorMessage unit tests (400, 403, 409, connection, generic).
//
// Riverpod providers are overridden with fakes so tests run without a real
// server or OS keychain.
//
// Run with: flutter test test/screens/admin_users_screen_test.dart

import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/models/models.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/current_user_provider.dart';
import 'package:player_android/screens/admin_users_screen.dart';
import 'package:player_android/utils/error_mappers.dart';

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

/// In-memory [TokenStorage] that returns a fixed username without hitting
/// the OS keychain.
class _FakeTokenStorage implements TokenStorage {
  _FakeTokenStorage([this._token = 'alice']);
  final String? _token;

  @override
  Future<String?> readToken() async => _token;

  @override
  Future<void> writeToken(String token) async {}

  @override
  Future<void> deleteToken() async {}
}

/// Controllable [PlayerApiClient] stub for [AdminUsersScreen] tests.
///
/// [listUsers], [createUser], and [deleteUser] are the primary subjects.
/// All other methods remain [UnimplementedError] — the screen calls only these.
class _FakeApiClient extends PlayerApiClient {
  _FakeApiClient() : super(dio: Dio(BaseOptions(baseUrl: 'http://test.local')));

  // ---- listUsers ----

  /// When non-null, [listUsers] returns this list.
  List<User>? usersResult;

  /// When non-null, [listUsers] throws this instead of returning.
  Object? usersError;

  /// Number of times [listUsers] has been called.
  int listUsersCallCount = 0;

  @override
  Future<List<User>> listUsers() async {
    listUsersCallCount++;
    if (usersError != null) throw usersError!;
    return usersResult!;
  }

  // ---- createUser ----

  /// When non-null, [createUser] returns this user.
  User? createResult;

  /// When non-null, [createUser] throws this instead of returning.
  Object? createError;

  /// Captures the last call arguments to [createUser].
  String? createdUsername;
  bool? createdIsAdmin;

  @override
  Future<User> createUser({
    required String username,
    required String password,
    required bool isAdmin,
  }) async {
    createdUsername = username;
    createdIsAdmin = isAdmin;
    if (createError != null) throw createError!;
    return createResult!;
  }

  // ---- deleteUser ----

  /// When non-null, [deleteUser] throws this instead of returning.
  Object? deleteError;

  /// The ID passed to the last [deleteUser] call.
  int? deletedUserId;

  @override
  Future<void> deleteUser(int userId) async {
    deletedUserId = userId;
    if (deleteError != null) throw deleteError!;
  }
}

/// Controllable [PlayerApiClient] stub that delays [listUsers] until
/// [complete] is called — used to inspect the mid-flight loading state.
class _DelayedFakeApiClient extends PlayerApiClient {
  _DelayedFakeApiClient() : super(dio: Dio());

  final _completer = Completer<List<User>>();

  /// Resolves the pending [listUsers] with [users].
  void complete(List<User> users) => _completer.complete(users);

  @override
  Future<List<User>> listUsers() => _completer.future;
}

/// [PlayerApiClient] stub whose [createUser] call is controlled by an external
/// [Completer] — lets tests inspect the optimistic placeholder while the
/// network request is still in flight.
class _DelayedCreateApiClient extends PlayerApiClient {
  _DelayedCreateApiClient({required List<User> initialUsers})
      : _initialUsers = initialUsers,
        super(dio: Dio());

  final List<User> _initialUsers;
  final _createCompleter = Completer<User>();

  /// Resolves the pending [createUser] with [user].
  void completeCreate(User user) => _createCompleter.complete(user);

  @override
  Future<List<User>> listUsers() async => _initialUsers;

  @override
  Future<User> createUser({
    required String username,
    required String password,
    required bool isAdmin,
  }) =>
      _createCompleter.future;
}

// ---------------------------------------------------------------------------
// Sample data
// ---------------------------------------------------------------------------

/// Admin user (the one "logged in" — alice with id=1).
const _kAlice = User(id: 1, username: 'alice', isAdmin: true);

/// Regular user.
const _kBob = User(id: 2, username: 'bob', isAdmin: false);

/// Another regular user.
const _kCarol = User(id: 3, username: 'carol', isAdmin: false);

// ---------------------------------------------------------------------------
// Helper: pump AdminUsersScreen inside a minimal ProviderScope.
// ---------------------------------------------------------------------------

/// Pumps [AdminUsersScreen] with a [ProviderScope] that overrides:
///   - [apiClientProvider] with [fakeClient].
///   - [tokenStorageProvider] with an in-memory fake.
///   - [currentUserProvider] with [currentUser] if provided, so the screen
///     knows which row is "self" and hides the delete button for it.
///
/// Using [MaterialApp] (not [MaterialApp.router]) is sufficient here because
/// [AdminUsersScreen] does not call [context.go]; it only shows dialogs and
/// SnackBars.
Future<void> _pumpAdminUsersScreen(
  WidgetTester tester,
  PlayerApiClient fakeClient, {
  User? currentUser = _kAlice,
}) async {
  final overrides = <Override>[
    tokenStorageProvider.overrideWithValue(
      _FakeTokenStorage(currentUser?.username),
    ),
    apiClientProvider.overrideWithValue(fakeClient),
    // Override currentUserProvider so the screen's self-detection works
    // without a real listUsers round-trip inside the provider itself.
    if (currentUser != null)
      currentUserProvider.overrideWith(
        (ref) async => currentUser,
      ),
  ];

  await tester.pumpWidget(
    ProviderScope(
      overrides: overrides,
      child: const MaterialApp(
        home: AdminUsersScreen(),
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
    testWidgets('shows loading indicator while listUsers is in flight',
        (tester) async {
      final fakeClient = _DelayedFakeApiClient();

      await _pumpAdminUsersScreen(tester, fakeClient);

      // Pump a single frame so initState's addPostFrameCallback fires but the
      // Future has not resolved yet.
      await tester.pump();

      expect(find.byKey(const Key('admin_users_loading')), findsOneWidget);
      expect(find.byType(CircularProgressIndicator), findsOneWidget);

      // Resolve to avoid dangling-async warnings.
      fakeClient.complete([_kAlice]);
      await tester.pumpAndSettle();
    });
  });

  // --------------------------------------------------------------------------
  // Renders user list
  // --------------------------------------------------------------------------

  group('renders user list', () {
    testWidgets('shows a tile for each user returned by listUsers',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kAlice, _kBob, _kCarol];

      await _pumpAdminUsersScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('admin_users_list')), findsOneWidget);
      expect(find.text('alice'), findsOneWidget);
      expect(find.text('bob'), findsOneWidget);
      expect(find.text('carol'), findsOneWidget);
    });

    testWidgets('hides delete button for the current user\'s own row',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kAlice, _kBob];

      // Current user is alice (id=1); her tile should not have a delete button.
      await _pumpAdminUsersScreen(tester, fakeClient, currentUser: _kAlice);
      await tester.pumpAndSettle();

      // Alice tile: no delete button.
      expect(
        find.byKey(const Key('admin_user_delete_1')),
        findsNothing,
      );
      // Bob tile: delete button present.
      expect(find.byKey(const Key('admin_user_delete_2')), findsOneWidget);
    });

    testWidgets('shows delete button for users other than the current user',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kAlice, _kBob, _kCarol];

      await _pumpAdminUsersScreen(tester, fakeClient, currentUser: _kAlice);
      await tester.pumpAndSettle();

      // Both non-self users have delete buttons.
      expect(find.byKey(const Key('admin_user_delete_2')), findsOneWidget);
      expect(find.byKey(const Key('admin_user_delete_3')), findsOneWidget);
    });

    testWidgets('shows "(you)" subtitle on the current user\'s own row',
        (tester) async {
      final fakeClient = _FakeApiClient()..usersResult = [_kAlice, _kBob];

      await _pumpAdminUsersScreen(tester, fakeClient, currentUser: _kAlice);
      await tester.pumpAndSettle();

      expect(find.text('(you)'), findsOneWidget);
    });

    testWidgets('renders Admin badge for admin users', (tester) async {
      final fakeClient = _FakeApiClient()..usersResult = [_kAlice, _kBob];

      await _pumpAdminUsersScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // _kAlice is admin — chip label reads 'Admin'.
      // _kBob is a regular user — chip label reads 'User'.
      // Using text finders avoids key-collision when multiple users share a role.
      expect(find.text('Admin'), findsOneWidget);
      expect(find.text('User'), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Create user dialog
  // --------------------------------------------------------------------------

  group('create user dialog', () {
    testWidgets('opens on FAB tap', (tester) async {
      final fakeClient = _FakeApiClient()..usersResult = [_kAlice];

      await _pumpAdminUsersScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('admin_users_fab')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('admin_create_user_dialog')), findsOneWidget);
    });

    testWidgets('cancel closes the dialog without calling createUser',
        (tester) async {
      final fakeClient = _FakeApiClient()..usersResult = [_kAlice];

      await _pumpAdminUsersScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('admin_users_fab')));
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('admin_create_cancel')));
      await tester.pumpAndSettle();

      // Dialog dismissed.
      expect(find.byKey(const Key('admin_create_user_dialog')), findsNothing);
      // createUser was never called.
      expect(fakeClient.createdUsername, isNull);
    });

    testWidgets('shows validation error for empty username', (tester) async {
      final fakeClient = _FakeApiClient()..usersResult = [_kAlice];

      await _pumpAdminUsersScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('admin_users_fab')));
      await tester.pumpAndSettle();

      // Leave username empty, fill a valid password, then submit.
      await tester.enterText(
        find.byKey(const Key('admin_create_password')),
        'password123',
      );
      await tester.tap(find.byKey(const Key('admin_create_submit')));
      await tester.pumpAndSettle();

      expect(find.text('Username is required.'), findsOneWidget);
      // Dialog still open — createUser not called.
      expect(find.byKey(const Key('admin_create_user_dialog')), findsOneWidget);
    });

    testWidgets('shows validation error for password shorter than 8 chars',
        (tester) async {
      final fakeClient = _FakeApiClient()..usersResult = [_kAlice];

      await _pumpAdminUsersScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('admin_users_fab')));
      await tester.pumpAndSettle();

      await tester.enterText(
        find.byKey(const Key('admin_create_username')),
        'newuser',
      );
      await tester.enterText(
        find.byKey(const Key('admin_create_password')),
        'short',
      );
      await tester.tap(find.byKey(const Key('admin_create_submit')));
      await tester.pumpAndSettle();

      expect(
        find.text('Password must be at least 8 characters.'),
        findsOneWidget,
      );
    });

    testWidgets('submits and adds user to the list on success', (tester) async {
      const newUser = User(id: 99, username: 'newuser', isAdmin: false);
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kAlice]
        ..createResult = newUser;

      await _pumpAdminUsersScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('admin_users_fab')));
      await tester.pumpAndSettle();

      await tester.enterText(
        find.byKey(const Key('admin_create_username')),
        'newuser',
      );
      await tester.enterText(
        find.byKey(const Key('admin_create_password')),
        'securepassword',
      );
      await tester.tap(find.byKey(const Key('admin_create_submit')));
      await tester.pumpAndSettle();

      // Dialog dismissed after successful submit.
      expect(find.byKey(const Key('admin_create_user_dialog')), findsNothing);

      // createUser was called with the right username.
      expect(fakeClient.createdUsername, equals('newuser'));
      expect(fakeClient.createdIsAdmin, isFalse);

      // The new user's tile appears in the list.
      expect(find.text('newuser'), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Create optimistic UI
  // --------------------------------------------------------------------------

  group('create optimistic UI', () {
    testWidgets(
        'placeholder row visible while createUser is in flight, replaced on success',
        (tester) async {
      const newUser = User(id: 42, username: 'newuser', isAdmin: false);
      final fakeClient = _DelayedCreateApiClient(initialUsers: [_kAlice]);

      await _pumpAdminUsersScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Open the create dialog, fill in the form, and submit.
      await tester.tap(find.byKey(const Key('admin_users_fab')));
      await tester.pumpAndSettle();

      await tester.enterText(
        find.byKey(const Key('admin_create_username')),
        'newuser',
      );
      await tester.enterText(
        find.byKey(const Key('admin_create_password')),
        'securepassword',
      );
      await tester.tap(find.byKey(const Key('admin_create_submit')));

      // Drive the event loop until the dialog is dismissed and the optimistic
      // placeholder setState has fired, but stop before createUser completes
      // (the completer is not yet resolved).
      // pumpAndSettle would spin forever waiting for the pending Future, so
      // we pump through the dialog pop animation (300 ms) manually.
      await tester.pump(const Duration(milliseconds: 300));
      await tester.pump(const Duration(milliseconds: 300));

      // Dialog should be gone.
      expect(find.byKey(const Key('admin_create_user_dialog')), findsNothing);

      // Placeholder row is visible by tile key (id=0) and username text.
      expect(find.byKey(const Key('admin_user_tile_0')), findsOneWidget);
      expect(find.text('newuser'), findsOneWidget);

      // Now resolve the pending createUser call.
      fakeClient.completeCreate(newUser);
      await tester.pumpAndSettle();

      // Placeholder (id=0) replaced by the real user (id=42).
      expect(find.byKey(const Key('admin_user_tile_0')), findsNothing);
      expect(find.byKey(const Key('admin_user_tile_42')), findsOneWidget);
      expect(find.text('newuser'), findsOneWidget);
    });

    testWidgets('reverts placeholder and shows error SnackBar on createUser failure',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kAlice]
        ..createError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/admin/users'),
          response: Response(
            requestOptions: RequestOptions(path: '/api/v1/admin/users'),
            statusCode: 409,
          ),
          type: DioExceptionType.badResponse,
        );

      await _pumpAdminUsersScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('admin_users_fab')));
      await tester.pumpAndSettle();

      await tester.enterText(
        find.byKey(const Key('admin_create_username')),
        'alice',
      );
      await tester.enterText(
        find.byKey(const Key('admin_create_password')),
        'password123',
      );
      await tester.tap(find.byKey(const Key('admin_create_submit')));
      await tester.pumpAndSettle();

      // Placeholder was optimistically added then removed after the error.
      // The list should still contain only alice (id=1).
      expect(find.byKey(const Key('admin_user_tile_1')), findsOneWidget);

      // Error SnackBar visible.
      expect(find.byKey(const Key('admin_users_error_snackbar')), findsOneWidget);
      expect(
        find.textContaining('already exists'),
        findsOneWidget,
      );
    });
  });

  // --------------------------------------------------------------------------
  // Delete user action
  // --------------------------------------------------------------------------

  group('delete user action', () {
    testWidgets('confirmation dialog cancel leaves the row intact',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kAlice, _kBob];

      await _pumpAdminUsersScreen(tester, fakeClient, currentUser: _kAlice);
      await tester.pumpAndSettle();

      // Tap delete for bob.
      await tester.tap(find.byKey(const Key('admin_user_delete_2')));
      await tester.pumpAndSettle();

      // Cancel the confirmation.
      await tester.tap(find.byKey(const Key('admin_users_confirm_cancel')));
      await tester.pumpAndSettle();

      // Bob's row is still present.
      expect(find.byKey(const Key('admin_user_tile_2')), findsOneWidget);
      expect(fakeClient.deletedUserId, isNull);
    });

    testWidgets('confirmation dialog confirm removes the user row',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kAlice, _kBob];

      await _pumpAdminUsersScreen(tester, fakeClient, currentUser: _kAlice);
      await tester.pumpAndSettle();

      // Tap delete for bob.
      await tester.tap(find.byKey(const Key('admin_user_delete_2')));
      await tester.pumpAndSettle();

      // Confirm deletion.
      await tester.tap(find.byKey(const Key('admin_users_confirm_delete')));
      await tester.pumpAndSettle();

      // Bob's row removed.
      expect(find.byKey(const Key('admin_user_tile_2')), findsNothing);
      // alice still present.
      expect(find.byKey(const Key('admin_user_tile_1')), findsOneWidget);

      expect(fakeClient.deletedUserId, equals(2));

      // Success SnackBar shown.
      expect(
        find.byKey(const Key('admin_users_delete_snackbar')),
        findsOneWidget,
      );
    });
  });

  // --------------------------------------------------------------------------
  // Delete optimistic UI
  // --------------------------------------------------------------------------

  group('delete optimistic UI', () {
    testWidgets('reverts row and shows error SnackBar on deleteUser failure',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersResult = [_kAlice, _kBob]
        ..deleteError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/admin/users/2'),
          type: DioExceptionType.connectionError,
        );

      await _pumpAdminUsersScreen(tester, fakeClient, currentUser: _kAlice);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('admin_user_delete_2')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('admin_users_confirm_delete')));
      await tester.pumpAndSettle();

      // Bob's row should be re-inserted after the error.
      expect(find.byKey(const Key('admin_user_tile_2')), findsOneWidget);

      // Error SnackBar shown.
      expect(
        find.byKey(const Key('admin_users_error_snackbar')),
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
    testWidgets('shows empty-state widget when listUsers returns []',
        (tester) async {
      final fakeClient = _FakeApiClient()..usersResult = [];

      await _pumpAdminUsersScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('admin_users_empty')), findsOneWidget);
      expect(find.byKey(const Key('admin_users_list')), findsNothing);
      expect(find.byKey(const Key('admin_users_loading')), findsNothing);
    });
  });

  // --------------------------------------------------------------------------
  // Error state
  // --------------------------------------------------------------------------

  group('error state', () {
    testWidgets('shows error message when listUsers throws a network error',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/admin/users'),
          type: DioExceptionType.connectionError,
        );

      await _pumpAdminUsersScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('admin_users_error')), findsOneWidget);
      expect(find.byKey(const Key('admin_users_list')), findsNothing);
      expect(
        find.textContaining('Could not reach the server'),
        findsOneWidget,
      );
    });

    testWidgets('retry button re-calls listUsers after an error',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..usersError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/admin/users'),
          type: DioExceptionType.connectionError,
        );

      await _pumpAdminUsersScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('admin_users_retry')), findsOneWidget);

      // Fix the error before retry so the second call succeeds.
      fakeClient
        ..usersError = null
        ..usersResult = [_kAlice];

      await tester.tap(find.byKey(const Key('admin_users_retry')));
      await tester.pumpAndSettle();

      // After successful retry the list is visible.
      expect(find.byKey(const Key('admin_users_list')), findsOneWidget);
      // listUsers called twice: once on init, once on retry.
      expect(fakeClient.listUsersCallCount, equals(2));
    });
  });

  // --------------------------------------------------------------------------
  // adminUserErrorMessage unit tests
  // --------------------------------------------------------------------------

  group('adminUserErrorMessage', () {
    test('returns connectivity message for connectionError', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/api/v1/admin/users'),
        type: DioExceptionType.connectionError,
      );
      expect(adminUserErrorMessage(err), contains('Could not reach the server'));
    });

    test('returns server-message from body for 400 when body has message field',
        () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/api/v1/admin/users'),
        response: Response(
          requestOptions: RequestOptions(path: '/api/v1/admin/users'),
          statusCode: 400,
          data: <String, dynamic>{'message': 'password too short'},
        ),
        type: DioExceptionType.badResponse,
      );
      expect(adminUserErrorMessage(err), equals('password too short'));
    });

    test('returns generic invalid-request message for 400 without body', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/api/v1/admin/users'),
        response: Response(
          requestOptions: RequestOptions(path: '/api/v1/admin/users'),
          statusCode: 400,
        ),
        type: DioExceptionType.badResponse,
      );
      expect(adminUserErrorMessage(err), contains('Invalid request'));
    });

    test('returns permission message for 403', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/api/v1/admin/users'),
        response: Response(
          requestOptions: RequestOptions(path: '/api/v1/admin/users'),
          statusCode: 403,
        ),
        type: DioExceptionType.badResponse,
      );
      expect(adminUserErrorMessage(err), contains('permission'));
    });

    test('returns already-exists message for 409', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/api/v1/admin/users'),
        response: Response(
          requestOptions: RequestOptions(path: '/api/v1/admin/users'),
          statusCode: 409,
        ),
        type: DioExceptionType.badResponse,
      );
      expect(adminUserErrorMessage(err), contains('already exists'));
    });

    test('returns server-error message for 500', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/api/v1/admin/users'),
        response: Response(
          requestOptions: RequestOptions(path: '/api/v1/admin/users'),
          statusCode: 500,
        ),
        type: DioExceptionType.badResponse,
      );
      expect(adminUserErrorMessage(err), contains('500'));
    });

    test('returns generic message for non-Dio error', () {
      expect(adminUserErrorMessage(Exception('boom')), contains('Unexpected error'));
    });
  });
}
