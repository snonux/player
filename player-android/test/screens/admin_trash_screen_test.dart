// Widget tests for AdminTrashScreen (admin_trash_screen.dart).
//
// Tests cover:
//   1. Trash list renders items from listTrash.
//   2. Restore action calls restoreMedia and removes item from the list.
//   3. Restore failure re-adds item (optimistic revert) and shows SnackBar.
//   4. Hard-delete shows a confirmation dialog before acting.
//   5. Confirming hard-delete calls deleteMedia and removes item from the list.
//   6. Cancelling confirmation does NOT call deleteMedia.
//   7. Loading spinner shown before first list fetch completes.
//   8. Empty-state shown when listTrash returns [].
//   9. Error-state shown when listTrash throws.
//
// Riverpod providers are overridden with fakes so tests run without a real
// server or OS keychain.
//
// Run with: flutter test test/screens/admin_trash_screen_test.dart

import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/models/models.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/screens/admin_trash_screen.dart';

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

/// Controllable [PlayerApiClient] stub for [AdminTrashScreen] tests.
///
/// [listTrash], [restoreMedia], and [deleteMedia] are the primary subjects.
/// All other methods remain [UnimplementedError] — the screen calls only these.
class _FakeApiClient extends PlayerApiClient {
  _FakeApiClient() : super(dio: Dio(BaseOptions(baseUrl: 'http://test.local')));

  // ---- listTrash ----

  /// When non-null, [listTrash] returns this list.
  List<Media>? trashResult;

  /// When non-null, [listTrash] throws this instead of returning.
  Object? trashError;

  /// Number of times [listTrash] has been called.
  int listTrashCallCount = 0;

  @override
  Future<List<Media>> listTrash() async {
    listTrashCallCount++;
    if (trashError != null) throw trashError!;
    return trashResult!;
  }

  // ---- restoreMedia ----

  /// When non-null, [restoreMedia] throws this instead of returning.
  Object? restoreError;

  /// The id passed to the last [restoreMedia] call.
  int? restoredMediaId;

  /// Number of times [restoreMedia] has been called.
  int restoreCallCount = 0;

  @override
  Future<void> restoreMedia(int mediaId) async {
    restoredMediaId = mediaId;
    restoreCallCount++;
    if (restoreError != null) throw restoreError!;
  }

  // ---- deleteMedia ----

  /// When non-null, [deleteMedia] throws this instead of returning.
  Object? deleteError;

  /// The id passed to the last [deleteMedia] call.
  int? deletedMediaId;

  /// Number of times [deleteMedia] has been called.
  int deleteCallCount = 0;

  @override
  Future<void> deleteMedia(int mediaId) async {
    deletedMediaId = mediaId;
    deleteCallCount++;
    if (deleteError != null) throw deleteError!;
  }
}

/// [PlayerApiClient] stub whose [listTrash] is controlled by an external
/// [Completer] — lets tests inspect the loading state before the fetch resolves.
class _DelayedFakeApiClient extends PlayerApiClient {
  _DelayedFakeApiClient() : super(dio: Dio());

  final _completer = Completer<List<Media>>();

  /// Resolves the pending [listTrash] with [items].
  void complete(List<Media> items) => _completer.complete(items);

  @override
  Future<List<Media>> listTrash() => _completer.future;
}

// ---------------------------------------------------------------------------
// Sample data
// ---------------------------------------------------------------------------

/// Builds a minimal [Media] suitable for trash-screen tests.
///
/// Only the fields that the trash screen reads are populated; all other fields
/// use sensible zero values.
Media _makeMedia({required int id, required String fileName, String type = 'video'}) {
  return Media(
    id: id,
    setId: 1,
    relPath: 'path/$fileName',
    fileName: fileName,
    absPath: '/media/$fileName',
    type: type,
    duration: 120.0,
    codec: 'h264',
    resolution: '1920x1080',
    bitrate: 3000,
    fileSizeBytes: 50000000,
    width: 1920,
    height: 1080,
    thumbnailPath: '',
    playCount: 0,
  );
}

final _kVideoA = _makeMedia(id: 1, fileName: 'video_a.mp4');
final _kVideoB = _makeMedia(id: 2, fileName: 'video_b.mp4');
final _kAudioC = _makeMedia(id: 3, fileName: 'audio_c.mp3', type: 'audio');

// ---------------------------------------------------------------------------
// Helper: pump AdminTrashScreen inside a minimal ProviderScope.
// ---------------------------------------------------------------------------

/// Pumps [AdminTrashScreen] with a [ProviderScope] that overrides
/// [apiClientProvider] with [fakeClient] and [tokenStorageProvider] with an
/// in-memory fake.  Using [MaterialApp] is sufficient because the screen does
/// not navigate away — it only shows dialogs and SnackBars.
Future<void> _pumpTrashScreen(
  WidgetTester tester,
  PlayerApiClient fakeClient,
) async {
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        tokenStorageProvider.overrideWithValue(_FakeTokenStorage()),
        apiClientProvider.overrideWithValue(fakeClient),
      ],
      child: const MaterialApp(home: AdminTrashScreen()),
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
    testWidgets('shows loading spinner while listTrash is in flight',
        (tester) async {
      final fakeClient = _DelayedFakeApiClient();

      await _pumpTrashScreen(tester, fakeClient);
      // One pump so addPostFrameCallback fires but Future has not resolved.
      await tester.pump();

      expect(find.byKey(const Key('admin_trash_loading')), findsOneWidget);
      expect(find.byType(CircularProgressIndicator), findsOneWidget);

      // Resolve to avoid dangling-async warnings.
      fakeClient.complete([]);
      await tester.pumpAndSettle();
    });
  });

  // --------------------------------------------------------------------------
  // Renders trash list
  // --------------------------------------------------------------------------

  group('renders trash list', () {
    testWidgets('shows a tile for each item returned by listTrash',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..trashResult = [_kVideoA, _kVideoB, _kAudioC];

      await _pumpTrashScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('admin_trash_list')), findsOneWidget);
      expect(find.byKey(const Key('admin_trash_tile_1')), findsOneWidget);
      expect(find.byKey(const Key('admin_trash_tile_2')), findsOneWidget);
      expect(find.byKey(const Key('admin_trash_tile_3')), findsOneWidget);
      expect(find.text('video_a.mp4'), findsOneWidget);
      expect(find.text('video_b.mp4'), findsOneWidget);
      expect(find.text('audio_c.mp3'), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Restore action
  // --------------------------------------------------------------------------

  group('restore action', () {
    testWidgets('restore calls restoreMedia and removes item from list',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..trashResult = [_kVideoA, _kVideoB];

      await _pumpTrashScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Tap restore on video_a (id=1).
      await tester.tap(find.byKey(const Key('admin_trash_restore_1')));
      await tester.pumpAndSettle();

      // restoreMedia should have been called with the correct id.
      expect(fakeClient.restoredMediaId, equals(1));
      expect(fakeClient.restoreCallCount, equals(1));

      // The restored item should no longer appear in the list.
      expect(find.byKey(const Key('admin_trash_tile_1')), findsNothing);
      // The other item should still be present.
      expect(find.byKey(const Key('admin_trash_tile_2')), findsOneWidget);
    });

    testWidgets('restore failure re-adds item and shows error SnackBar',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..trashResult = [_kVideoA, _kVideoB]
        ..restoreError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/admin/media/1/restore'),
          type: DioExceptionType.connectionError,
        );

      await _pumpTrashScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Tap restore on video_a (id=1); the API will fail.
      await tester.tap(find.byKey(const Key('admin_trash_restore_1')));
      await tester.pumpAndSettle();

      // Item should be re-appended (reverted) after the failure.
      expect(find.byKey(const Key('admin_trash_tile_1')), findsOneWidget);

      // Error SnackBar should be visible.
      expect(
        find.byKey(const Key('admin_trash_error_snackbar')),
        findsOneWidget,
      );
      expect(find.textContaining('Could not reach the server'), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Hard-delete action
  // --------------------------------------------------------------------------

  group('hard-delete action', () {
    testWidgets('tapping hard-delete shows confirmation dialog', (tester) async {
      final fakeClient = _FakeApiClient()..trashResult = [_kVideoA];

      await _pumpTrashScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('admin_trash_delete_1')));
      await tester.pumpAndSettle();

      // Confirmation dialog should be visible.
      expect(find.text('Permanently delete?'), findsOneWidget);
      // Both action buttons should be present.
      expect(find.byKey(const Key('admin_trash_confirm_cancel')), findsOneWidget);
      expect(find.byKey(const Key('admin_trash_confirm_delete')), findsOneWidget);
    });

    testWidgets('cancelling confirmation does NOT call deleteMedia',
        (tester) async {
      final fakeClient = _FakeApiClient()..trashResult = [_kVideoA];

      await _pumpTrashScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('admin_trash_delete_1')));
      await tester.pumpAndSettle();

      // Cancel the dialog.
      await tester.tap(find.byKey(const Key('admin_trash_confirm_cancel')));
      await tester.pumpAndSettle();

      // Dialog dismissed; deleteMedia was never called.
      expect(find.text('Permanently delete?'), findsNothing);
      expect(fakeClient.deleteCallCount, equals(0));
      expect(fakeClient.deletedMediaId, isNull);

      // Item is still in the list.
      expect(find.byKey(const Key('admin_trash_tile_1')), findsOneWidget);
    });

    testWidgets('confirming hard-delete calls deleteMedia and removes item',
        (tester) async {
      final fakeClient = _FakeApiClient()..trashResult = [_kVideoA, _kVideoB];

      await _pumpTrashScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Hard-delete video_a (id=1).
      await tester.tap(find.byKey(const Key('admin_trash_delete_1')));
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('admin_trash_confirm_delete')));
      await tester.pumpAndSettle();

      // deleteMedia called with the correct id.
      expect(fakeClient.deletedMediaId, equals(1));
      expect(fakeClient.deleteCallCount, equals(1));

      // video_a removed from the list.
      expect(find.byKey(const Key('admin_trash_tile_1')), findsNothing);
      // video_b still present.
      expect(find.byKey(const Key('admin_trash_tile_2')), findsOneWidget);

      // Success SnackBar shown.
      expect(
        find.byKey(const Key('admin_trash_delete_snackbar')),
        findsOneWidget,
      );
    });

    testWidgets('hard-delete failure re-adds item and shows error SnackBar',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..trashResult = [_kVideoA]
        ..deleteError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/admin/media/1'),
          response: Response(
            requestOptions: RequestOptions(path: '/api/v1/admin/media/1'),
            statusCode: 403,
          ),
          type: DioExceptionType.badResponse,
        );

      await _pumpTrashScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('admin_trash_delete_1')));
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('admin_trash_confirm_delete')));
      await tester.pumpAndSettle();

      // Item re-appended after error.
      expect(find.byKey(const Key('admin_trash_tile_1')), findsOneWidget);

      // Error SnackBar shown.
      expect(
        find.byKey(const Key('admin_trash_error_snackbar')),
        findsOneWidget,
      );
      expect(find.textContaining('permission'), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Empty state
  // --------------------------------------------------------------------------

  group('empty state', () {
    testWidgets('shows empty-state widget when listTrash returns []',
        (tester) async {
      final fakeClient = _FakeApiClient()..trashResult = [];

      await _pumpTrashScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('admin_trash_empty')), findsOneWidget);
      expect(find.byKey(const Key('admin_trash_list')), findsNothing);
    });
  });

  // --------------------------------------------------------------------------
  // Error state
  // --------------------------------------------------------------------------

  group('error state', () {
    testWidgets('shows error message when listTrash throws a network error',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..trashError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/admin/trash'),
          type: DioExceptionType.connectionError,
        );

      await _pumpTrashScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('admin_trash_error')), findsOneWidget);
      expect(find.byKey(const Key('admin_trash_list')), findsNothing);
      expect(find.textContaining('Could not reach the server'), findsOneWidget);
    });

    testWidgets('retry button re-calls listTrash after an error', (tester) async {
      final fakeClient = _FakeApiClient()
        ..trashError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/admin/trash'),
          type: DioExceptionType.connectionError,
        );

      await _pumpTrashScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('admin_trash_retry')), findsOneWidget);

      // Fix the error so the retry succeeds.
      fakeClient
        ..trashError = null
        ..trashResult = [_kVideoA];

      await tester.tap(find.byKey(const Key('admin_trash_retry')));
      await tester.pumpAndSettle();

      // List visible after successful retry.
      expect(find.byKey(const Key('admin_trash_list')), findsOneWidget);
      // listTrash called twice: once on init, once on retry.
      expect(fakeClient.listTrashCallCount, equals(2));
    });
  });
}
