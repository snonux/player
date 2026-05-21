// Widget tests for VideoPlayerScreen (video_player_screen.dart).
//
// Tests cover:
//   1. Loading indicator shown during the initial build before initState fires.
//   2. Error view rendered when VideoPlayerController.initialize() throws.
//   3. Retry button re-triggers initialisation and ends in an error state.
//   4. Screen renders the AppBar title containing the mediaId.
//   5. Stream URL resolution (route-extra URL and client.streamUrl fallback).
//
// VideoPlayerController relies on native platform channels (ExoPlayer /
// AVPlayer) that are unavailable in the Flutter test harness.  We exploit the
// fact that VideoPlayerController.initialize() throws a MissingPluginException,
// turning every initialisation attempt into a predictable error path — which
// is exactly what we need for error-state coverage.
//
// Timing notes:
//   - [VideoPlayerScreen] uses addPostFrameCallback to start _initPlayer so
//     that Riverpod provider overrides are fully applied before the first read.
//   - pumpWidget() renders the first frame with _isLoading = true.
//   - pump() processes addPostFrameCallback → _initPlayer() → throws → error.
//   - Therefore the loading spinner is visible immediately after pumpWidget()
//     but NOT after a subsequent pump(); check it before pumping.
//
// Run with: flutter test test/screens/video_player_screen_test.dart

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/progress_queue_provider.dart';
import 'package:player_android/screens/video_player_screen.dart';
import 'package:player_android/services/progress_queue.dart';

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

/// Controllable [PlayerApiClient] stub for [VideoPlayerScreen] tests.
///
/// Only the progress methods and [streamUrl] are implemented; all other
/// methods throw [UnimplementedError] to catch unexpected usage immediately.
class _FakeApiClient extends PlayerApiClient {
  _FakeApiClient() : super(dio: Dio());

  /// Records how many times [getMediaProgress] was called.
  int getMediaProgressCallCount = 0;

  /// When non-null, [getMediaProgress] returns this value.
  double? progressResult;

  /// Records how many times [updateProgress] was called.
  int updateProgressCallCount = 0;

  /// Records how many times [updateProgressStatus] was called.
  int updateProgressStatusCallCount = 0;

  @override
  Future<double?> getMediaProgress(int mediaId) async {
    getMediaProgressCallCount++;
    return progressResult;
  }

  @override
  Future<void> updateProgress({
    required int mediaId,
    required double positionSeconds,
  }) async {
    updateProgressCallCount++;
  }

  @override
  Future<void> updateProgressStatus({
    required int mediaId,
    required String status,
  }) async {
    updateProgressStatusCallCount++;
  }

  /// Returns a synthetic stream URL so [VideoPlayerController.networkUrl] can
  /// be constructed even though it will fail to initialise (no platform).
  @override
  String streamUrl(int mediaId) =>
      'http://localhost:8080/api/v1/media/$mediaId/stream';
}

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

/// No-op [ProgressQueueBase] stub for widget tests.
///
/// Implements [ProgressQueueBase] directly rather than extending [ProgressQueue]
/// so no real SQLite database is opened and no connectivity subscription is
/// created in the test harness (Liskov Substitution — any [ProgressQueueBase]
/// can be injected wherever the interface is required).
class _FakeProgressQueue implements ProgressQueueBase {
  @override
  Future<void> init() async {}

  @override
  Future<void> enqueue(
    int mediaId,
    double positionSeconds, {
    bool finished = false,
  }) async {}

  @override
  Future<void> dispose() async {}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/// Pumps [VideoPlayerScreen] for [mediaId] inside a [ProviderScope] with
/// overridden providers, backed by a minimal [GoRouter] for navigation.
///
/// [mediaUrl] may be supplied to exercise the route-extra URL path; when null
/// the screen falls back to [PlayerApiClient.streamUrl].
///
/// The returned widget is rendered after the first frame but BEFORE
/// addPostFrameCallback fires, so _isLoading is still true.
Future<void> _pumpScreen(
  WidgetTester tester,
  _FakeApiClient fakeClient, {
  String mediaId = '42',
  String? mediaUrl,
}) async {
  final router = GoRouter(
    initialLocation: '/video/$mediaId',
    routes: [
      GoRoute(
        path: '/video/:mediaId',
        builder: (context, state) => VideoPlayerScreen(
          mediaId: state.pathParameters['mediaId']!,
          mediaUrl: mediaUrl,
        ),
      ),
    ],
  );

  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        tokenStorageProvider.overrideWithValue(const _FakeTokenStorage()),
        apiClientProvider.overrideWithValue(fakeClient),
        // Override progressQueueProvider so no real SQLite DB is opened and
        // no connectivity subscription is created during widget tests.
        progressQueueProvider.overrideWithValue(_FakeProgressQueue()),
      ],
      child: MaterialApp.router(routerConfig: router),
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
    testWidgets(
        'shows a loading indicator immediately after pumpWidget (before initState callback fires)',
        (tester) async {
      final fakeClient = _FakeApiClient();
      // pumpWidget renders the first frame with _isLoading == true but does
      // NOT fire addPostFrameCallback yet — that fires on the next pump.
      await _pumpScreen(tester, fakeClient);

      // Immediately after pumpWidget the initial build has completed with
      // _isLoading = true; the spinner must be visible at this point.
      expect(
        find.byKey(const Key('video_player_loading')),
        findsOneWidget,
      );
      expect(find.byType(CircularProgressIndicator), findsOneWidget);

      // Drain remaining async work to avoid "pending timers" warnings.
      await tester.pumpAndSettle();
    });
  });

  // --------------------------------------------------------------------------
  // Error state
  // --------------------------------------------------------------------------

  group('error state', () {
    testWidgets(
        'shows error view when VideoPlayerController fails to initialise',
        (tester) async {
      final fakeClient = _FakeApiClient();
      await _pumpScreen(tester, fakeClient);
      // pumpAndSettle processes addPostFrameCallback → _initPlayer → throws →
      // error state is rendered.
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('video_player_error')), findsOneWidget);
    });

    testWidgets('error view contains a human-readable message', (tester) async {
      final fakeClient = _FakeApiClient();
      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // The error message key should be present and contain non-empty text.
      expect(
        find.byKey(const Key('video_player_error_message')),
        findsOneWidget,
      );
      // Verify the text widget is present with a non-empty string inside the
      // error_message keyed slot.
      final textWidget = tester.widget<Text>(
        find.byKey(const Key('video_player_error_message')),
      );
      expect(textWidget.data, isNotEmpty);
    });

    testWidgets('error view contains a retry button', (tester) async {
      final fakeClient = _FakeApiClient();
      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('video_player_retry')), findsOneWidget);
    });

    testWidgets(
        'tapping retry eventually shows the error state again after re-initialisation',
        (tester) async {
      final fakeClient = _FakeApiClient();
      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Confirm we start in the error state.
      expect(find.byKey(const Key('video_player_error')), findsOneWidget);

      // Tap Retry — this calls _onRetry which calls setState then _initPlayer.
      await tester.tap(find.byKey(const Key('video_player_retry')));

      // Let the second initialisation attempt complete (also fails in test
      // harness due to no platform plugin) and settle back into error state.
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('video_player_error')), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // AppBar
  // --------------------------------------------------------------------------

  group('app bar', () {
    testWidgets('renders title containing the mediaId', (tester) async {
      final fakeClient = _FakeApiClient();
      await _pumpScreen(tester, fakeClient, mediaId: '99');
      await tester.pump();

      // The title includes the mediaId string somewhere in the widget tree.
      expect(find.textContaining('99'), findsWidgets);
    });
  });

  // --------------------------------------------------------------------------
  // Stream URL resolution
  // --------------------------------------------------------------------------

  group('stream URL resolution', () {
    testWidgets('transitions through loading to error when mediaUrl is null',
        (tester) async {
      // When mediaUrl is null the screen calls client.streamUrl(mediaId).
      // We verify the full lifecycle: starts loading → error after init fails.
      final fakeClient = _FakeApiClient();
      await _pumpScreen(tester, fakeClient, mediaUrl: null);

      // Immediately after pumpWidget the loading state is visible.
      expect(find.byKey(const Key('video_player_loading')), findsOneWidget);

      // After settling, error state is shown (platform plugin missing).
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('video_player_error')), findsOneWidget);
    });

    testWidgets(
        'transitions through loading to error when an explicit mediaUrl is given',
        (tester) async {
      final fakeClient = _FakeApiClient();
      await _pumpScreen(
        tester,
        fakeClient,
        mediaUrl: 'http://localhost:8080/api/v1/media/42/stream',
      );

      // Loading state visible immediately after pumpWidget.
      expect(find.byKey(const Key('video_player_loading')), findsOneWidget);

      // Settles to error state (VideoPlayerController fails in test harness).
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('video_player_error')), findsOneWidget);
    });
  });
}
