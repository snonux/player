// Widget tests for AdminLibrariesScreen.
//
// Run with: flutter test test/screens/admin_libraries_screen_test.dart

import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/models/models.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/screens/admin_libraries_screen.dart';
import 'package:player_android/utils/error_mappers.dart';

class _FakeTokenStorage implements TokenStorage {
  @override
  Future<String?> readToken() async => 'admin';

  @override
  Future<void> writeToken(String token) async {}

  @override
  Future<void> deleteToken() async {}
}

class _FakeApiClient extends PlayerApiClient {
  _FakeApiClient() : super(dio: Dio(BaseOptions(baseUrl: 'http://test.local')));

  List<MusicLibrary>? librariesResult;
  Object? librariesError;
  List<User> usersResult = const [];
  Object? usersError;

  MusicLibrary? createdLibrary;
  String? createdName;
  String? createdPath;
  Object? createError;

  int? renamedId;
  String? renamedName;
  MusicLibrary? renameResult;
  Object? renameError;

  int? deletedId;
  Object? deleteError;

  int? grantLibraryId;
  int? grantUserId;
  String? grantRole;
  Object? grantError;

  int? revokeLibraryId;
  int? revokeUserId;
  Object? revokeError;

  @override
  Future<List<MusicLibrary>> listAdminLibraries() async {
    if (librariesError != null) throw librariesError!;
    return librariesResult!;
  }

  @override
  Future<List<User>> listUsers() async {
    if (usersError != null) throw usersError!;
    return usersResult;
  }

  @override
  Future<MusicLibrary> createLibrary({
    required String name,
    required String rootPath,
  }) async {
    createdName = name;
    createdPath = rootPath;
    if (createError != null) throw createError!;
    return createdLibrary!;
  }

  @override
  Future<MusicLibrary> renameLibrary({
    required int libraryId,
    required String name,
  }) async {
    renamedId = libraryId;
    renamedName = name;
    if (renameError != null) throw renameError!;
    return renameResult!;
  }

  @override
  Future<void> deleteLibrary(int libraryId) async {
    deletedId = libraryId;
    if (deleteError != null) throw deleteError!;
  }

  @override
  Future<void> grantLibraryAccess({
    required int libraryId,
    required int userId,
    required String role,
  }) async {
    grantLibraryId = libraryId;
    grantUserId = userId;
    grantRole = role;
    if (grantError != null) throw grantError!;
  }

  @override
  Future<void> revokeLibraryAccess({
    required int libraryId,
    required int userId,
  }) async {
    revokeLibraryId = libraryId;
    revokeUserId = userId;
    if (revokeError != null) throw revokeError!;
  }
}

class _DelayedFakeApiClient extends PlayerApiClient {
  _DelayedFakeApiClient() : super(dio: Dio());

  final _completer = Completer<List<MusicLibrary>>();

  void complete(List<MusicLibrary> items) => _completer.complete(items);

  @override
  Future<List<MusicLibrary>> listAdminLibraries() => _completer.future;

  @override
  Future<List<User>> listUsers() async => const [];
}

const _kLibA = MusicLibrary(
  id: 1,
  name: 'Main',
  rootPath: '/media/main',
  stats: MusicLibraryStats(setCount: 2, songCount: 10),
);
const _kLibB = MusicLibrary(
  id: 2,
  name: 'Extra',
  rootPath: '/media/extra',
);

Future<void> _pump(WidgetTester tester, PlayerApiClient client) async {
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        tokenStorageProvider.overrideWithValue(_FakeTokenStorage()),
        apiClientProvider.overrideWithValue(client),
      ],
      child: const MaterialApp(home: AdminLibrariesScreen()),
    ),
  );
}

void main() {
  group('loading / empty / error', () {
    testWidgets('shows loading spinner while libraries load', (tester) async {
      final client = _DelayedFakeApiClient();
      await _pump(tester, client);
      await tester.pump();
      expect(find.byKey(const Key('admin_libraries_loading')), findsOneWidget);
      client.complete([]);
      await tester.pumpAndSettle();
    });

    testWidgets('shows empty state when no libraries', (tester) async {
      final client = _FakeApiClient()..librariesResult = [];
      await _pump(tester, client);
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('admin_libraries_empty')), findsOneWidget);
    });

    testWidgets('shows error state when load fails', (tester) async {
      final client = _FakeApiClient()
        ..librariesError = DioException(
          requestOptions: RequestOptions(path: '/'),
          type: DioExceptionType.connectionError,
        );
      await _pump(tester, client);
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('admin_libraries_error')), findsOneWidget);
      expect(find.byKey(const Key('admin_libraries_retry')), findsOneWidget);
    });
  });

  group('list and mutations', () {
    testWidgets('renders library tiles', (tester) async {
      final client = _FakeApiClient()
        ..librariesResult = [_kLibA, _kLibB]
        ..usersResult = const [
          User(id: 9, username: 'bob', isAdmin: false),
        ];
      await _pump(tester, client);
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('admin_libraries_list')), findsOneWidget);
      expect(find.byKey(const Key('admin_libraries_tile_1')), findsOneWidget);
      expect(find.text('Main'), findsOneWidget);
      expect(find.textContaining('/media/main'), findsOneWidget);
      expect(find.textContaining('2 sets'), findsOneWidget);
    });

    testWidgets('add library submits name and path', (tester) async {
      final client = _FakeApiClient()
        ..librariesResult = []
        ..createdLibrary = const MusicLibrary(
          id: 3,
          name: 'Other',
          rootPath: '/media/other',
        );
      await _pump(tester, client);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('admin_libraries_add')));
      await tester.pumpAndSettle();
      await tester.enterText(
          find.byKey(const Key('admin_libraries_name_field')), 'Other');
      await tester.enterText(
          find.byKey(const Key('admin_libraries_path_field')), '/media/other');
      await tester.tap(find.byKey(const Key('admin_libraries_form_submit')));
      await tester.pumpAndSettle();

      expect(client.createdName, 'Other');
      expect(client.createdPath, '/media/other');
      expect(find.byKey(const Key('admin_libraries_tile_3')), findsOneWidget);
    });

    testWidgets('rename updates the tile title', (tester) async {
      final client = _FakeApiClient()
        ..librariesResult = [_kLibA]
        ..renameResult = const MusicLibrary(
          id: 1,
          name: 'Renamed',
          rootPath: '/media/main',
        );
      await _pump(tester, client);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('admin_libraries_menu_1')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Rename'));
      await tester.pumpAndSettle();
      await tester.enterText(
          find.byKey(const Key('admin_libraries_name_field')), 'Renamed');
      await tester.tap(find.byKey(const Key('admin_libraries_form_submit')));
      await tester.pumpAndSettle();

      expect(client.renamedId, 1);
      expect(client.renamedName, 'Renamed');
      expect(find.text('Renamed'), findsOneWidget);
    });

    testWidgets('delete removes tile after confirm', (tester) async {
      final client = _FakeApiClient()..librariesResult = [_kLibA, _kLibB];
      await _pump(tester, client);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('admin_libraries_menu_1')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Remove'));
      await tester.pumpAndSettle();
      await tester
          .tap(find.byKey(const Key('admin_libraries_confirm_delete_1')));
      await tester.pumpAndSettle();

      expect(client.deletedId, 1);
      expect(find.byKey(const Key('admin_libraries_tile_1')), findsNothing);
      expect(find.byKey(const Key('admin_libraries_tile_2')), findsOneWidget);
    });

    testWidgets('manage access grants viewer role', (tester) async {
      final client = _FakeApiClient()
        ..librariesResult = [_kLibA]
        ..usersResult = const [
          User(id: 9, username: 'bob', isAdmin: false),
        ];
      await _pump(tester, client);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('admin_libraries_menu_1')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Manage access'));
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('admin_libraries_access_1_9')));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Viewer').last);
      await tester.pumpAndSettle();

      expect(client.grantLibraryId, 1);
      expect(client.grantUserId, 9);
      expect(client.grantRole, 'viewer');
    });
  });

  group('adminLibraryErrorMessage', () {
    test('maps 403 and 409', () {
      expect(
        adminLibraryErrorMessage(DioException(
          requestOptions: RequestOptions(path: '/'),
          response: Response(
            requestOptions: RequestOptions(path: '/'),
            statusCode: 403,
          ),
          type: DioExceptionType.badResponse,
        )),
        contains('permission'),
      );
      expect(
        adminLibraryErrorMessage(DioException(
          requestOptions: RequestOptions(path: '/'),
          response: Response(
            requestOptions: RequestOptions(path: '/'),
            statusCode: 409,
            data: {'error': 'library still has sets'},
          ),
          type: DioExceptionType.badResponse,
        )),
        contains('sets'),
      );
    });
  });
}
