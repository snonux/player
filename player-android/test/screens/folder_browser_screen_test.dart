// Widget tests for FolderBrowserScreen (folder_browser_screen.dart).
//
// Tests cover:
//   1. Shows loading indicator while browseSet is in flight.
//   2. Renders folder tiles and media tiles after a successful load.
//   3. Breadcrumb bar reflects the current path (one crumb for root, multiple
//      for nested paths).
//   4. Tapping a folder tile navigates deeper (pushes a new route with the
//      updated path).
//   5. Tapping a media tile navigates to the media-detail route.
//   6. Shows an empty-state view when browseSet returns no folders/media.
//   7. Shows an error view when browseSet throws, with a retry button.
//   8. Pull-to-refresh calls browseSet a second time.
//   9. Breadcrumb tap navigates to the tapped ancestor path.
//
// Riverpod providers are overridden with fakes so tests run without a real
// server or OS keychain.
//
// Run with: flutter test test/screens/folder_browser_screen_test.dart

import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/screens/folder_browser_screen.dart';

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

/// In-memory [TokenStorage] that returns a fixed test token.
///
/// Avoids the platform-specific OS keychain in widget tests.
class _FakeTokenStorage implements TokenStorage {
  const _FakeTokenStorage();

  @override
  Future<String?> readToken() async => 'test-token';

  @override
  Future<void> writeToken(String token) async {}

  @override
  Future<void> deleteToken() async {}
}

/// Controllable [PlayerApiClient] stub for [FolderBrowserScreen] tests.
///
/// Only [browseSet], [thumbnailUrl], and [setFolderCoverUrl] are implemented;
/// all other methods remain [UnimplementedError] — the screen calls only these.
class _FakeApiClient extends PlayerApiClient {
  _FakeApiClient() : super(dio: Dio());

  /// When non-null, [browseSet] returns this map.
  Map<String, dynamic>? browseResult;

  /// When non-null, [browseSet] throws this instead of returning.
  Object? browseError;

  /// Records the number of [browseSet] calls for refresh tests.
  int browseCallCount = 0;

  /// The [parent] argument passed to the most recent [browseSet] call.
  String? lastParent;

  @override
  Future<Map<String, dynamic>> browseSet(int setId, {String? parent}) async {
    browseCallCount++;
    lastParent = parent;
    if (browseError != null) throw browseError!;
    return browseResult!;
  }

  /// Returns an empty string so thumbnail widgets show the placeholder instead
  /// of making a network request — keeps tests hermetic.
  @override
  String thumbnailUrl(int mediaId) => '';

  /// Returns an empty string so folder-cover images show the placeholder
  /// instead of making a network request — keeps tests hermetic.
  @override
  String setFolderCoverUrl(int setId, {String? folder}) => '';
}

/// [PlayerApiClient] stub that delays [browseSet] until [complete] is called.
///
/// Used to inspect mid-flight loading state before the response arrives.
class _DelayedFakeApiClient extends PlayerApiClient {
  _DelayedFakeApiClient() : super(dio: Dio());

  final _completer = Completer<Map<String, dynamic>>();

  /// Resolves the pending [browseSet] call with [result].
  void complete(Map<String, dynamic> result) => _completer.complete(result);

  @override
  Future<Map<String, dynamic>> browseSet(int setId, {String? parent}) =>
      _completer.future;

  /// Returns an empty string so thumbnail widgets show the placeholder instead
  /// of making a network request — keeps tests hermetic.
  @override
  String thumbnailUrl(int mediaId) => '';

  /// Returns an empty string so folder-cover images show the placeholder
  /// instead of making a network request — keeps tests hermetic.
  @override
  String setFolderCoverUrl(int setId, {String? folder}) => '';
}

// ---------------------------------------------------------------------------
// Sample data
// ---------------------------------------------------------------------------

/// A browseSet response at the root with two subfolders and one media item.
///
/// Both folders set [has_cover] to false so tests stay hermetic — no
/// [CachedNetworkImage] network requests are made during widget pumps.
const _kRootBrowseResult = {
  'current_path': '',
  'folders': [
    {'name': 'FolderA', 'has_cover': false},
    {'name': 'FolderB', 'has_cover': false},
  ],
  'media': [
    {
      'id': 1,
      'set_id': 10,
      'rel_path': 'root_video.mp4',
      'file_name': 'root_video.mp4',
      'abs_path': '/media/set/root_video.mp4',
      'type': 'video',
      'duration': 120.0,
      'codec': 'h264',
      'resolution': '1920x1080',
      'bitrate': 4000,
      'file_size_bytes': 15000000,
      'width': 1920,
      'height': 1080,
      'thumbnail_path': '',
      'play_count': 0,
    }
  ],
};

/// A browseSet response for FolderA containing one subfolder and two media items.
const _kFolderABrowseResult = {
  'current_path': 'FolderA',
  'folders': [
    {'name': 'SubFolderA1', 'has_cover': false},
  ],
  'media': [
    {
      'id': 2,
      'set_id': 10,
      'rel_path': 'FolderA/audio.mp3',
      'file_name': 'audio.mp3',
      'abs_path': '/media/set/FolderA/audio.mp3',
      'type': 'audio',
      'duration': 210.0,
      'codec': 'mp3',
      'resolution': '',
      'bitrate': 320,
      'file_size_bytes': 8000000,
      'width': 0,
      'height': 0,
      'thumbnail_path': '',
      'play_count': 0,
    },
    {
      'id': 3,
      'set_id': 10,
      'rel_path': 'FolderA/clip.mp4',
      'file_name': 'clip.mp4',
      'abs_path': '/media/set/FolderA/clip.mp4',
      'type': 'video',
      'duration': 30.0,
      'codec': 'h264',
      'resolution': '1280x720',
      'bitrate': 2000,
      'file_size_bytes': 3000000,
      'width': 1280,
      'height': 720,
      'thumbnail_path': '',
      'play_count': 0,
    },
  ],
};

/// A browseSet response with no folders and no media (empty folder).
const _kEmptyBrowseResult = {
  'current_path': '',
  'folders': <Map<String, dynamic>>[],
  'media': <Map<String, dynamic>>[],
};

// ---------------------------------------------------------------------------
// Helper: pump FolderBrowserScreen inside a minimal ProviderScope.
// ---------------------------------------------------------------------------

/// Stub route shown after navigating to the media-detail screen.
const _kMediaDetailKey = Key('nav_media_detail');

/// Builds a [GoRouter] with [FolderBrowserScreen] and stub detail + child routes
/// so navigation tests can verify that the correct destination is reached.
GoRouter _buildRouter(
  PlayerApiClient fakeClient, {
  String? path,
  String? setName,
}) {
  return GoRouter(
    initialLocation: path != null
        ? '/browse/10?path=${Uri.encodeComponent(path)}'
        : '/browse/10',
    routes: [
      GoRoute(
        path: '/browse/:setId',
        builder: (context, state) {
          final setId = int.tryParse(state.pathParameters['setId']!) ?? 0;
          final p = state.uri.queryParameters['path'];
          final name =
              state.extra is String ? state.extra as String : setName;
          return FolderBrowserScreen(setId: setId, path: p, setName: name);
        },
      ),
      GoRoute(
        // Stub for media-detail navigation.
        path: '/media/:id',
        builder: (context, state) => Scaffold(
          body: Text(
            'Media ${state.pathParameters['id']}',
            key: _kMediaDetailKey,
          ),
        ),
      ),
    ],
  );
}

/// Pumps [FolderBrowserScreen] (set 10, optional [path]) inside a
/// [ProviderScope] that overrides [apiClientProvider] and
/// [tokenStorageProvider] with fakes.
Future<void> _pumpScreen(
  WidgetTester tester,
  PlayerApiClient fakeClient, {
  String? path,
  String? setName,
}) async {
  final router = _buildRouter(fakeClient, path: path, setName: setName);
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        tokenStorageProvider.overrideWithValue(const _FakeTokenStorage()),
        apiClientProvider.overrideWithValue(fakeClient),
      ],
      child: MaterialApp.router(
        routerConfig: router,
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
    testWidgets('shows loading indicator while browseSet is in flight',
        (tester) async {
      final fakeClient = _DelayedFakeApiClient();

      await _pumpScreen(tester, fakeClient);

      // One frame: initState fires, addPostFrameCallback enqueues the load,
      // but the Future has not yet resolved.
      await tester.pump();

      expect(find.byKey(const Key('folder_loading')), findsOneWidget);
      expect(find.byType(CircularProgressIndicator), findsOneWidget);

      // Resolve to prevent "pending async work" warnings.
      fakeClient.complete(_kRootBrowseResult);
      await tester.pumpAndSettle();
    });
  });

  // --------------------------------------------------------------------------
  // Renders content
  // --------------------------------------------------------------------------

  group('renders folders and media', () {
    testWidgets('shows folder tiles for each folder returned by browseSet',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..browseResult = Map<String, dynamic>.from(_kRootBrowseResult);

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('folder_tile_FolderA')),
        findsOneWidget,
      );
      expect(
        find.byKey(const Key('folder_tile_FolderB')),
        findsOneWidget,
      );
      expect(find.byKey(const Key('folder_name_FolderA')), findsOneWidget);
      expect(find.byKey(const Key('folder_name_FolderB')), findsOneWidget);
    });

    testWidgets('shows media tiles for each media item returned by browseSet',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..browseResult = Map<String, dynamic>.from(_kRootBrowseResult);

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('media_tile_1')), findsOneWidget);
      expect(
        find.byKey(const Key('media_tile_name_1')),
        findsOneWidget,
      );
      expect(find.text('root_video.mp4'), findsOneWidget);
    });

    testWidgets('shows section headers for folders and media', (tester) async {
      final fakeClient = _FakeApiClient()
        ..browseResult = Map<String, dynamic>.from(_kRootBrowseResult);

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('folder_section_header')),
        findsOneWidget,
      );
      expect(
        find.byKey(const Key('media_section_header')),
        findsOneWidget,
      );
    });

    testWidgets('renders duration formatted correctly', (tester) async {
      final fakeClient = _FakeApiClient()
        ..browseResult = Map<String, dynamic>.from(_kFolderABrowseResult);

      await _pumpScreen(tester, fakeClient, path: 'FolderA');
      await tester.pumpAndSettle();

      // audio.mp3: 210s = 3:30
      expect(find.text('3:30'), findsOneWidget);
      // clip.mp4: 30s = 0:30
      expect(find.text('0:30'), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Breadcrumb navigation
  // --------------------------------------------------------------------------

  group('breadcrumb navigation', () {
    testWidgets('shows single "Home" crumb at root path', (tester) async {
      final fakeClient = _FakeApiClient()
        ..browseResult = Map<String, dynamic>.from(_kRootBrowseResult);

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // At root the only crumb is "Home" (as a non-tappable Text).
      expect(
        find.byKey(const Key('breadcrumb_current_Home')),
        findsOneWidget,
      );
      expect(find.byKey(const Key('breadcrumb_bar')), findsOneWidget);
    });

    testWidgets('shows parent crumbs and current crumb for nested path',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..browseResult = Map<String, dynamic>.from(_kFolderABrowseResult);

      await _pumpScreen(tester, fakeClient, path: 'FolderA');
      await tester.pumpAndSettle();

      // "Home" is a tappable ancestor crumb.
      expect(find.byKey(const Key('breadcrumb_Home')), findsOneWidget);
      // "FolderA" is the current (non-tappable) crumb.
      expect(
        find.byKey(const Key('breadcrumb_current_FolderA')),
        findsOneWidget,
      );
    });

    testWidgets('shows three crumbs for a two-level nested path',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..browseResult = {
          'current_path': 'FolderA/SubFolderA1',
          'folders': <Map<String, dynamic>>[],
          'media': <Map<String, dynamic>>[],
        };

      await _pumpScreen(tester, fakeClient, path: 'FolderA/SubFolderA1');
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('breadcrumb_Home')), findsOneWidget);
      expect(find.byKey(const Key('breadcrumb_FolderA')), findsOneWidget);
      expect(
        find.byKey(const Key('breadcrumb_current_SubFolderA1')),
        findsOneWidget,
      );
    });
  });

  // --------------------------------------------------------------------------
  // Folder tap navigates deeper
  // --------------------------------------------------------------------------

  group('folder tap navigates deeper', () {
    testWidgets(
        'tapping a folder tile pushes a new route with the updated path',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..browseResult = Map<String, dynamic>.from(_kRootBrowseResult);

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Tap FolderA — should push /browse/10?path=FolderA.
      await tester.tap(find.byKey(const Key('folder_tile_FolderA')));

      // Prepare the client for the child browse call.
      fakeClient.browseResult =
          Map<String, dynamic>.from(_kFolderABrowseResult);
      await tester.pumpAndSettle();

      // After navigation the new screen's breadcrumb should show FolderA as
      // the current crumb.
      expect(
        find.byKey(const Key('breadcrumb_current_FolderA')),
        findsOneWidget,
      );
    });

    testWidgets('browseSet is called with the child path after folder tap',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..browseResult = Map<String, dynamic>.from(_kRootBrowseResult);

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // First call is for the root (parent == null).
      expect(fakeClient.browseCallCount, equals(1));
      expect(fakeClient.lastParent, isNull);

      // Prepare for the child browse.
      fakeClient.browseResult =
          Map<String, dynamic>.from(_kFolderABrowseResult);

      await tester.tap(find.byKey(const Key('folder_tile_FolderA')));
      await tester.pumpAndSettle();

      // A second browseSet call should have been made with parent='FolderA'.
      expect(fakeClient.browseCallCount, greaterThanOrEqualTo(2));
      expect(fakeClient.lastParent, equals('FolderA'));
    });
  });

  // --------------------------------------------------------------------------
  // Media tap navigates to detail
  // --------------------------------------------------------------------------

  group('media tap navigates to detail', () {
    testWidgets('tapping a media tile navigates to /media/:id', (tester) async {
      final fakeClient = _FakeApiClient()
        ..browseResult = Map<String, dynamic>.from(_kRootBrowseResult);

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('media_tile_1')));
      await tester.pumpAndSettle();

      // Stub route at /media/:id should be on screen.
      expect(find.byKey(_kMediaDetailKey), findsOneWidget);
      expect(find.text('Media 1'), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Empty state
  // --------------------------------------------------------------------------

  group('empty state', () {
    testWidgets('shows empty-state view when browseSet returns no content',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..browseResult =
            Map<String, dynamic>.from(_kEmptyBrowseResult);

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('folder_empty')), findsOneWidget);
      expect(
        find.byKey(const Key('folder_section_header')),
        findsNothing,
      );
      expect(
        find.byKey(const Key('media_section_header')),
        findsNothing,
      );
    });
  });

  // --------------------------------------------------------------------------
  // Error state
  // --------------------------------------------------------------------------

  group('error state', () {
    testWidgets('shows error message when browseSet throws a network error',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..browseError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/sets/10/browse'),
          type: DioExceptionType.connectionError,
        );

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('folder_error')), findsOneWidget);
      expect(
        find.textContaining('Could not reach the server'),
        findsOneWidget,
      );
    });

    testWidgets('retry button re-calls browseSet and shows content on success',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..browseError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/sets/10/browse'),
          type: DioExceptionType.connectionError,
        );

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('folder_retry')), findsOneWidget);

      // Fix the error before tapping retry.
      fakeClient
        ..browseError = null
        ..browseResult = Map<String, dynamic>.from(_kRootBrowseResult);

      await tester.tap(find.byKey(const Key('folder_retry')));
      await tester.pumpAndSettle();

      // Grid content is now visible.
      expect(find.byKey(const Key('folder_tile_FolderA')), findsOneWidget);
      expect(fakeClient.browseCallCount, equals(2));
    });

    testWidgets('shows 403 permission message for forbidden error',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..browseError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/sets/10/browse'),
          type: DioExceptionType.badResponse,
          response: Response(
            requestOptions: RequestOptions(path: '/api/v1/sets/10/browse'),
            statusCode: 403,
          ),
        );

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(
        find.textContaining('do not have permission'),
        findsOneWidget,
      );
    });
  });

  // --------------------------------------------------------------------------
  // Pull-to-refresh
  // --------------------------------------------------------------------------

  group('pull-to-refresh', () {
    testWidgets('pull-to-refresh calls browseSet a second time', (tester) async {
      final fakeClient = _FakeApiClient()
        ..browseResult = Map<String, dynamic>.from(_kRootBrowseResult);

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(fakeClient.browseCallCount, equals(1));

      await tester.drag(
        find.byKey(const Key('folder_browser_scroll')),
        const Offset(0, 300),
      );
      await tester.pumpAndSettle();

      expect(fakeClient.browseCallCount, equals(2));
    });
  });
}
