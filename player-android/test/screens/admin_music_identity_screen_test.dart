// Widget tests for AdminMusicIdentityScreen.
//
// Run with: flutter test test/screens/admin_music_identity_screen_test.dart

import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/models/models.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/screens/admin_music_identity_screen.dart';
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

  List<MusicIdentityConflict>? conflictsResult;
  Object? conflictsError;

  int? resolvedId;
  String? resolvedAction;
  int? resolvedCandidate;
  Object? resolveError;

  @override
  Future<List<MusicIdentityConflict>> listMusicIdentityConflicts() async {
    if (conflictsError != null) throw conflictsError!;
    return conflictsResult!;
  }

  @override
  Future<void> resolveMusicIdentityConflict({
    required int conflictId,
    required String action,
    int? candidateMediaId,
  }) async {
    resolvedId = conflictId;
    resolvedAction = action;
    resolvedCandidate = candidateMediaId;
    if (resolveError != null) throw resolveError!;
  }
}

class _DelayedFakeApiClient extends PlayerApiClient {
  _DelayedFakeApiClient() : super(dio: Dio());

  final _completer = Completer<List<MusicIdentityConflict>>();

  void complete(List<MusicIdentityConflict> items) =>
      _completer.complete(items);

  @override
  Future<List<MusicIdentityConflict>> listMusicIdentityConflicts() =>
      _completer.future;
}

const _kConflict = MusicIdentityConflict(
  id: 7,
  libraryId: 1,
  missingMediaId: 100,
  candidateMediaIds: [200, 201],
  missingPath: 'old/song.mp3',
  candidatePaths: ['new/a.mp3', 'new/b.mp3'],
  reason: 'ambiguous fingerprint',
);

Future<void> _pump(WidgetTester tester, PlayerApiClient client) async {
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        tokenStorageProvider.overrideWithValue(_FakeTokenStorage()),
        apiClientProvider.overrideWithValue(client),
      ],
      child: const MaterialApp(home: AdminMusicIdentityScreen()),
    ),
  );
}

void main() {
  group('loading / empty / error', () {
    testWidgets('shows loading spinner while conflicts load', (tester) async {
      final client = _DelayedFakeApiClient();
      await _pump(tester, client);
      await tester.pump();
      expect(find.byKey(const Key('admin_identity_loading')), findsOneWidget);
      client.complete([]);
      await tester.pumpAndSettle();
    });

    testWidgets('shows empty state when no conflicts', (tester) async {
      final client = _FakeApiClient()..conflictsResult = [];
      await _pump(tester, client);
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('admin_identity_empty')), findsOneWidget);
      expect(find.textContaining('Moved songs keep playlists'), findsOneWidget);
    });

    testWidgets('shows error state when load fails', (tester) async {
      final client = _FakeApiClient()
        ..conflictsError = DioException(
          requestOptions: RequestOptions(path: '/'),
          type: DioExceptionType.connectionError,
        );
      await _pump(tester, client);
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('admin_identity_error')), findsOneWidget);
      expect(find.byKey(const Key('admin_identity_retry')), findsOneWidget);
    });
  });

  group('resolve actions', () {
    testWidgets('renders conflict and keep separate removes it',
        (tester) async {
      final client = _FakeApiClient()..conflictsResult = [_kConflict];
      await _pump(tester, client);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('admin_identity_tile_7')), findsOneWidget);
      expect(find.text('old/song.mp3'), findsOneWidget);
      expect(find.byKey(const Key('admin_identity_keep_7')), findsNothing);
      await tester.tap(find.byKey(const Key('admin_identity_tile_7')));
      await tester.pumpAndSettle();
      expect(find.textContaining('Candidates: new/a.mp3, new/b.mp3'),
          findsOneWidget);

      await tester.tap(find.byKey(const Key('admin_identity_keep_7')));
      await tester.pumpAndSettle();

      expect(client.resolvedId, 7);
      expect(client.resolvedAction, 'keep_separate');
      expect(find.byKey(const Key('admin_identity_tile_7')), findsNothing);
      expect(find.text('Kept separate'), findsOneWidget);
    });

    testWidgets('merge uses candidate media id', (tester) async {
      final client = _FakeApiClient()..conflictsResult = [_kConflict];
      await _pump(tester, client);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('admin_identity_tile_7')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('admin_identity_merge_7_200')));
      await tester.pumpAndSettle();

      expect(client.resolvedId, 7);
      expect(client.resolvedAction, 'merge');
      expect(client.resolvedCandidate, 200);
      expect(find.byKey(const Key('admin_identity_tile_7')), findsNothing);
    });

    testWidgets('resolve failure shows snackbar and keeps tile',
        (tester) async {
      final client = _FakeApiClient()
        ..conflictsResult = [_kConflict]
        ..resolveError = DioException(
          requestOptions: RequestOptions(path: '/'),
          response: Response(
            requestOptions: RequestOptions(path: '/'),
            statusCode: 404,
          ),
          type: DioExceptionType.badResponse,
        );
      await _pump(tester, client);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('admin_identity_tile_7')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('admin_identity_keep_7')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('admin_identity_tile_7')), findsOneWidget);
      expect(find.byKey(const Key('admin_identity_error_snackbar')),
          findsOneWidget);
    });
  });

  group('model parsing', () {
    test('parses candidate_media_ids JSON string', () {
      final conflict = MusicIdentityConflict.fromJson({
        'id': 1,
        'library_id': 2,
        'missing_media_id': 3,
        'candidate_media_ids': '[10, 11]',
        'missing_path': 'a.mp3',
        'candidate_paths': ['b.mp3', 'c.mp3'],
        'reason': 'ambiguous',
      });
      expect(conflict.candidateMediaIds, [10, 11]);
      expect(conflict.candidatePaths, ['b.mp3', 'c.mp3']);
    });
  });

  group('adminIdentityErrorMessage', () {
    test('maps 403 and 501', () {
      expect(
        adminIdentityErrorMessage(DioException(
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
        adminIdentityErrorMessage(DioException(
          requestOptions: RequestOptions(path: '/'),
          response: Response(
            requestOptions: RequestOptions(path: '/'),
            statusCode: 501,
          ),
          type: DioExceptionType.badResponse,
        )),
        contains('unavailable'),
      );
    });
  });
}
