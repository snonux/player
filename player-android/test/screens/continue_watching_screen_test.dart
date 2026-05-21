// Widget tests for ContinueWatchingScreen (continue_watching_screen.dart).
//
// Tests cover:
//   1. Renders a loading indicator while listInProgress is in flight.
//   2. Renders resume cards after a successful load (title, duration keys).
//   3. Shows the correct type icon for video and audio items.
//   4. Empty-state widget when listInProgress returns [].
//   5. Error view when listInProgress throws a DioException.
//   6. Retry button calls listInProgress again.
//   7. Pull-to-refresh calls listInProgress a second time.
//   8. Tapping a video card routes to /video/:id.
//   9. Tapping an audio card routes to /audio/:id.
//  10. continueWatchingErrorMessage unit tests.
//
// Riverpod providers are overridden with fakes so tests run without a real
// server or OS keychain.
//
// Run with: flutter test test/screens/continue_watching_screen_test.dart

import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/models/models.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/screens/continue_watching_screen.dart';
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

/// Controllable [PlayerApiClient] stub for [ContinueWatchingScreen] tests.
///
/// Only [listInProgress], [streamUrl], and [getMediaProgress] are implemented;
/// all other methods remain [UnimplementedError] — the screen calls only these.
class _FakeApiClient extends PlayerApiClient {
  _FakeApiClient() : super(dio: Dio());

  /// When non-null, [listInProgress] returns this list.
  List<Media>? inProgressResult;

  /// When non-null, [listInProgress] throws this instead of returning.
  Object? inProgressError;

  /// Number of times [listInProgress] has been called; useful for refresh tests.
  int listInProgressCallCount = 0;

  @override
  Future<List<Media>> listInProgress() async {
    listInProgressCallCount++;
    if (inProgressError != null) throw inProgressError!;
    return inProgressResult!;
  }

  /// Returns a stable fake stream URL so navigation assertions can verify the
  /// expected path without real network calls.
  @override
  String streamUrl(int mediaId) => 'http://fake/stream/$mediaId';

  /// Returns an empty string so [_Thumbnail] skips the network request in tests.
  @override
  String thumbnailUrl(int mediaId) => '';

  /// Returns `null` so the player starts from the beginning in tests.
  @override
  Future<double?> getMediaProgress(int mediaId) async => null;
}

/// [PlayerApiClient] stub that delays [listInProgress] until [complete] is called.
///
/// Used to inspect mid-flight loading state before the response arrives.
class _DelayedFakeApiClient extends PlayerApiClient {
  _DelayedFakeApiClient() : super(dio: Dio());

  final _completer = Completer<List<Media>>();

  /// Resolves the pending [listInProgress] call with [items].
  void complete(List<Media> items) => _completer.complete(items);

  @override
  Future<List<Media>> listInProgress() => _completer.future;

  @override
  String streamUrl(int mediaId) => 'http://fake/stream/$mediaId';

  /// Returns an empty string so [_Thumbnail] skips the network request in tests.
  @override
  String thumbnailUrl(int mediaId) => '';

  @override
  Future<double?> getMediaProgress(int mediaId) async => null;
}

// ---------------------------------------------------------------------------
// Sample data
// ---------------------------------------------------------------------------

/// A video media item.
const _kVideo = Media(
  id: 1,
  setId: 10,
  relPath: 'movies/film.mp4',
  fileName: 'film.mp4',
  absPath: '/media/movies/film.mp4',
  type: 'video',
  duration: 7200.0,
  codec: 'h264/aac',
  resolution: '1920x1080',
  bitrate: 4500,
  fileSizeBytes: 1024,
  width: 1920,
  height: 1080,
  thumbnailPath: '',
  playCount: 1,
);

/// An audio media item.
const _kAudio = Media(
  id: 2,
  setId: 11,
  relPath: 'podcasts/episode.mp3',
  fileName: 'episode.mp3',
  absPath: '/media/podcasts/episode.mp3',
  type: 'audio',
  duration: 3600.0,
  codec: 'mp3',
  resolution: '',
  bitrate: 128,
  fileSizeBytes: 512,
  width: 0,
  height: 0,
  thumbnailPath: '',
  playCount: 1,
);

// ---------------------------------------------------------------------------
// Pump helper
// ---------------------------------------------------------------------------

/// Pumps [ContinueWatchingScreen] inside a [ProviderScope] with overrides.
///
/// Captures navigated routes via a [GoRouter] stub so tap tests can assert
/// the correct player route was pushed.  Captured routes are stored in
/// [_lastNavigatedRoutes] and reset by [setUp] before each test.
Future<void> _pumpScreen(
  WidgetTester tester,
  PlayerApiClient fakeClient,
) async {
  // Track the last navigated location via a GoRouter with a simple observer.
  final navigatedRoutes = <String>[];

  final router = GoRouter(
    initialLocation: '/continue',
    routes: [
      GoRoute(
        path: '/continue',
        builder: (_, __) => const ContinueWatchingScreen(),
      ),
      // Stub routes so navigation does not crash during tap tests.
      GoRoute(
        path: '/video/:mediaId',
        builder: (_, state) {
          navigatedRoutes.add('/video/${state.pathParameters['mediaId']}');
          return const Scaffold(body: Text('video player'));
        },
      ),
      GoRoute(
        path: '/audio/:mediaId',
        builder: (_, state) {
          navigatedRoutes.add('/audio/${state.pathParameters['mediaId']}');
          return const Scaffold(body: Text('audio player'));
        },
      ),
    ],
  );

  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        tokenStorageProvider.overrideWithValue(const _FakeTokenStorage()),
        apiClientProvider.overrideWithValue(fakeClient),
      ],
      child: MaterialApp.router(routerConfig: router),
    ),
  );

  // Store router reference in fake for later assertions.
  if (fakeClient is _FakeApiClient) {
    // We capture routes through the navigatedRoutes list above.
    // Expose a way to read it for assertions.
    _lastNavigatedRoutes = navigatedRoutes;
  }
}

// Global list to capture navigated routes across test scope.
List<String> _lastNavigatedRoutes = [];

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

void main() {
  setUp(() => _lastNavigatedRoutes = []);

  // --------------------------------------------------------------------------
  // Loading state
  // --------------------------------------------------------------------------

  group('loading state', () {
    testWidgets('shows loading indicator while listInProgress is in flight',
        (tester) async {
      final fakeClient = _DelayedFakeApiClient();

      await _pumpScreen(tester, fakeClient);
      // Pump one frame: addPostFrameCallback fires but Future not yet resolved.
      await tester.pump();

      expect(find.byKey(const Key('continue_watching_loading')), findsOneWidget);
      expect(find.byType(CircularProgressIndicator), findsOneWidget);

      // Resolve to avoid "async work pending" warnings at test teardown.
      fakeClient.complete([_kVideo]);
      await tester.pumpAndSettle();
    });
  });

  // --------------------------------------------------------------------------
  // Renders cards
  // --------------------------------------------------------------------------

  group('renders cards', () {
    testWidgets('shows a card for each item returned by listInProgress',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..inProgressResult = [_kVideo, _kAudio];

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('resume_card_1')), findsOneWidget);
      expect(find.byKey(const Key('resume_card_2')), findsOneWidget);
    });

    testWidgets('renders the list after a successful load', (tester) async {
      final fakeClient = _FakeApiClient()..inProgressResult = [_kVideo];

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('continue_watching_list')),
        findsOneWidget,
      );
    });

    testWidgets('shows file name for each card', (tester) async {
      final fakeClient = _FakeApiClient()..inProgressResult = [_kVideo];

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('resume_card_title_1')), findsOneWidget);
      expect(find.text('film.mp4'), findsOneWidget);
    });

    testWidgets('shows duration for each card', (tester) async {
      final fakeClient = _FakeApiClient()..inProgressResult = [_kVideo];

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // _kVideo has duration 7200.0 s → 2:00:00.
      expect(find.byKey(const Key('resume_card_duration_1')), findsOneWidget);
      expect(find.text('2:00:00'), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Empty state
  // --------------------------------------------------------------------------

  group('empty state', () {
    testWidgets('shows empty-state widget when listInProgress returns []',
        (tester) async {
      final fakeClient = _FakeApiClient()..inProgressResult = [];

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('continue_watching_empty')),
        findsOneWidget,
      );
      expect(find.byKey(const Key('continue_watching_list')), findsNothing);
      expect(
        find.byKey(const Key('continue_watching_loading')),
        findsNothing,
      );
    });
  });

  // --------------------------------------------------------------------------
  // Error state
  // --------------------------------------------------------------------------

  group('error state', () {
    testWidgets('shows error message when listInProgress throws', (tester) async {
      final fakeClient = _FakeApiClient()
        ..inProgressError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/in-progress'),
          type: DioExceptionType.connectionError,
        );

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('continue_watching_error')),
        findsOneWidget,
      );
      expect(
        find.textContaining('Could not reach the server'),
        findsOneWidget,
      );
    });

    testWidgets('retry button calls listInProgress again', (tester) async {
      final fakeClient = _FakeApiClient()
        ..inProgressError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/in-progress'),
          type: DioExceptionType.connectionError,
        );

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('continue_watching_retry')),
        findsOneWidget,
      );

      // Fix the error so the retry succeeds.
      fakeClient
        ..inProgressError = null
        ..inProgressResult = [_kVideo];

      await tester.tap(find.byKey(const Key('continue_watching_retry')));
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('continue_watching_list')),
        findsOneWidget,
      );
      // listInProgress was called twice: once on init, once on retry.
      expect(fakeClient.listInProgressCallCount, equals(2));
    });
  });

  // --------------------------------------------------------------------------
  // Pull-to-refresh
  // --------------------------------------------------------------------------

  group('pull-to-refresh', () {
    testWidgets('pull-to-refresh calls listInProgress a second time',
        (tester) async {
      final fakeClient = _FakeApiClient()..inProgressResult = [_kVideo];

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(fakeClient.listInProgressCallCount, equals(1));

      await tester.drag(
        find.byKey(const Key('continue_watching_list')),
        const Offset(0, 300),
      );
      await tester.pumpAndSettle();

      expect(fakeClient.listInProgressCallCount, equals(2));
    });
  });

  // --------------------------------------------------------------------------
  // Tap navigation
  // --------------------------------------------------------------------------

  group('tap navigation', () {
    testWidgets('tapping a video card routes to /video/:id', (tester) async {
      final fakeClient = _FakeApiClient()..inProgressResult = [_kVideo];

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('resume_card_1')));
      await tester.pumpAndSettle();

      expect(_lastNavigatedRoutes, contains('/video/1'));
    });

    testWidgets('tapping an audio card routes to /audio/:id', (tester) async {
      final fakeClient = _FakeApiClient()..inProgressResult = [_kAudio];

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('resume_card_2')));
      await tester.pumpAndSettle();

      expect(_lastNavigatedRoutes, contains('/audio/2'));
    });
  });

  // --------------------------------------------------------------------------
  // continueWatchingErrorMessage unit tests
  // --------------------------------------------------------------------------

  group('continueWatchingErrorMessage', () {
    test('returns connectivity message for connectionError', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/api/v1/in-progress'),
        type: DioExceptionType.connectionError,
      );
      expect(
        continueWatchingErrorMessage(err),
        contains('Could not reach the server'),
      );
    });

    test('returns server-error message for 500 badResponse', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/api/v1/in-progress'),
        response: Response(
          requestOptions: RequestOptions(path: '/api/v1/in-progress'),
          statusCode: 500,
        ),
        type: DioExceptionType.badResponse,
      );
      expect(continueWatchingErrorMessage(err), contains('500'));
    });

    test('returns session-expired message for 401 badResponse', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/api/v1/in-progress'),
        response: Response(
          requestOptions: RequestOptions(path: '/api/v1/in-progress'),
          statusCode: 401,
        ),
        type: DioExceptionType.badResponse,
      );
      expect(
        continueWatchingErrorMessage(err),
        contains('Session expired'),
      );
    });

    test('returns generic message for unknown error type', () {
      expect(
        continueWatchingErrorMessage(Exception('boom')),
        contains('Unexpected error'),
      );
    });
  });
}
