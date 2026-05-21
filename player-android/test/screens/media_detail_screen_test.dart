// Widget tests for MediaDetailScreen (media_detail_screen.dart).
//
// Tests cover:
//   1. Shows a loading indicator while getMedia is in flight.
//   2. Renders title, metadata row, tag chips, and thumbnail after a
//      successful load.
//   3. Play button routes to /video/:id for a video item.
//   4. Play button routes to /audio/:id for an audio item.
//   5. Favourite toggle button flips the icon and calls toggleFavorite.
//   6. Shows an error view when getMedia throws a DioException.
//   7. Retry button triggers a fresh getMedia call.
//   8. 404 error is mapped to the "not found" message.
//   9. Optimistic update: icon flips immediately before toggleFavorite returns.
//  10. Revert on error: icon reverts and SnackBar shown when toggleFavorite fails.
//
// Riverpod providers are overridden with fakes so tests run without a real
// server or OS keychain.
//
// Run with: flutter test test/screens/media_detail_screen_test.dart

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
import 'package:player_android/screens/media_detail_screen.dart';

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

/// Controllable [PlayerApiClient] stub for [MediaDetailScreen] tests.
///
/// Implements [getMedia], [toggleFavorite], [listTags], [addTag],
/// [removeTag], and [thumbnailUrl]; all other methods remain
/// [UnimplementedError].  Tag methods return empty/no-op defaults so tests
/// that do not exercise tag behaviour do not need to configure them.
class _FakeApiClient extends PlayerApiClient {
  _FakeApiClient() : super(dio: Dio());

  /// When non-null, [getMedia] returns this value.
  Media? mediaResult;

  /// When non-null, [getMedia] throws this instead of returning.
  Object? mediaError;

  /// When non-null, [toggleFavorite] returns this bool.
  bool? toggleResult;

  /// When non-null, [toggleFavorite] throws this instead of returning.
  Object? toggleError;

  /// Records how many times [getMedia] was called.
  int getMediaCallCount = 0;

  /// Records how many times [toggleFavorite] was called.
  int toggleCallCount = 0;

  @override
  Future<Media> getMedia(int mediaId) async {
    getMediaCallCount++;
    if (mediaError != null) throw mediaError!;
    return mediaResult!;
  }

  @override
  Future<bool> toggleFavorite(int mediaId) async {
    toggleCallCount++;
    if (toggleError != null) throw toggleError!;
    return toggleResult!;
  }

  /// Returns an empty list so the autocomplete in [TagPicker] shows no
  /// suggestions — keeps existing tests hermetic without extra setup.
  @override
  Future<List<Tag>> listTags() async => [];

  /// No-op: tag add operations succeed silently in these tests.
  @override
  Future<void> addTag(int mediaId, String tag) async {}

  /// No-op: tag remove operations succeed silently in these tests.
  @override
  Future<void> removeTag(int mediaId, String tag) async {}

  /// Returns an empty string so [_ThumbnailBanner] shows the static
  /// placeholder instead of making a network request — keeps tests hermetic.
  @override
  String thumbnailUrl(int mediaId) => '';
}

/// [PlayerApiClient] stub that delays [getMedia] until [complete] is called.
///
/// Used to inspect mid-flight loading state before the response arrives.
/// Tag methods return empty/no-op defaults so tests stay hermetic.
class _DelayedFakeApiClient extends PlayerApiClient {
  _DelayedFakeApiClient() : super(dio: Dio());

  final _completer = Completer<Media>();

  /// Resolves the pending [getMedia] call with [media].
  void complete(Media media) => _completer.complete(media);

  @override
  Future<Media> getMedia(int mediaId) => _completer.future;

  @override
  Future<List<Tag>> listTags() async => [];

  @override
  Future<void> addTag(int mediaId, String tag) async {}

  @override
  Future<void> removeTag(int mediaId, String tag) async {}

  @override
  String thumbnailUrl(int mediaId) => '';
}

/// [PlayerApiClient] stub where [toggleFavorite] is delayed until
/// [completeToggle] is called.
///
/// Allows tests to inspect the UI state between the tap and the API response
/// (the optimistic update window).  Tag methods return empty/no-op defaults.
class _DelayedToggleFakeApiClient extends PlayerApiClient {
  _DelayedToggleFakeApiClient({required this.mediaResult})
      : super(dio: Dio());

  /// The media item returned synchronously by [getMedia].
  final Media mediaResult;

  final _toggleCompleter = Completer<bool>();

  /// Resolves [toggleFavorite] with [result].
  void completeToggle(bool result) => _toggleCompleter.complete(result);

  /// Fails [toggleFavorite] with [error].
  void failToggle(Object error) => _toggleCompleter.completeError(error);

  @override
  Future<Media> getMedia(int mediaId) async => mediaResult;

  @override
  Future<bool> toggleFavorite(int mediaId) => _toggleCompleter.future;

  @override
  Future<List<Tag>> listTags() async => [];

  @override
  Future<void> addTag(int mediaId, String tag) async {}

  @override
  Future<void> removeTag(int mediaId, String tag) async {}

  @override
  String thumbnailUrl(int mediaId) => '';
}

// ---------------------------------------------------------------------------
// Sample data
// ---------------------------------------------------------------------------

/// A sample video item used across tests.
const _kVideo = Media(
  id: 42,
  setId: 10,
  relPath: 'movies/action/hero.mp4',
  fileName: 'hero.mp4',
  absPath: '/media/movies/action/hero.mp4',
  type: 'video',
  duration: 7320.0, // 2h 2m
  codec: 'h264/aac',
  resolution: '1920x1080',
  bitrate: 4500,
  fileSizeBytes: 1073741824, // 1 GiB
  width: 1920,
  height: 1080,
  thumbnailPath: '/media/.thumbs/hero.jpg',
  playCount: 3,
  favorite: false,
  tags: ['action', 'english'],
);

/// A sample audio item used across tests.
const _kAudio = Media(
  id: 7,
  setId: 5,
  relPath: 'music/song.mp3',
  fileName: 'song.mp3',
  absPath: '/media/music/song.mp3',
  type: 'audio',
  duration: 210.0, // 3m 30s
  codec: 'mp3',
  resolution: '',
  bitrate: 320,
  fileSizeBytes: 8388608,
  width: 0,
  height: 0,
  thumbnailPath: '',
  playCount: 12,
  favorite: true,
  tags: [],
);

// ---------------------------------------------------------------------------
// Helper: pump MediaDetailScreen inside a minimal ProviderScope + GoRouter
// ---------------------------------------------------------------------------

/// Key used by the navigation-destination stub route.
const _kDestinationKey = Key('nav_destination');

/// Builds a [GoRouter] with [MediaDetailScreen] at `/media/:id` and stub
/// routes at `/video/:mediaId` and `/audio/:mediaId` for navigation tests.
GoRouter _buildRouter(PlayerApiClient fakeClient, String mediaId) {
  return GoRouter(
    initialLocation: '/media/$mediaId',
    routes: [
      GoRoute(
        path: '/media/:id',
        builder: (context, state) =>
            MediaDetailScreen(mediaId: state.pathParameters['id']!),
      ),
      GoRoute(
        path: '/video/:mediaId',
        builder: (context, state) => Scaffold(
          body: Text(
            'Video ${state.pathParameters['mediaId']}',
            key: _kDestinationKey,
          ),
        ),
      ),
      GoRoute(
        path: '/audio/:mediaId',
        builder: (context, state) => Scaffold(
          body: Text(
            'Audio ${state.pathParameters['mediaId']}',
            key: _kDestinationKey,
          ),
        ),
      ),
    ],
  );
}

/// Pumps [MediaDetailScreen] for [mediaId] inside a [ProviderScope] with
/// overridden providers, backed by a [GoRouter] for navigation tests.
Future<void> _pumpScreen(
  WidgetTester tester,
  PlayerApiClient fakeClient, {
  String mediaId = '42',
}) async {
  final router = _buildRouter(fakeClient, mediaId);
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        tokenStorageProvider.overrideWithValue(const _FakeTokenStorage()),
        apiClientProvider.overrideWithValue(fakeClient),
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
    testWidgets('shows a loading indicator while getMedia is in flight',
        (tester) async {
      final fakeClient = _DelayedFakeApiClient();

      await _pumpScreen(tester, fakeClient);

      // Pump one frame: initState fires, addPostFrameCallback enqueues the
      // load, and the Future has not resolved yet.
      await tester.pump();

      expect(
        find.byKey(const Key('media_detail_loading')),
        findsOneWidget,
      );
      expect(find.byType(CircularProgressIndicator), findsOneWidget);

      // Resolve to prevent "pending async work" warnings.
      fakeClient.complete(_kVideo);
      await tester.pumpAndSettle();
    });
  });

  // --------------------------------------------------------------------------
  // Successful render
  // --------------------------------------------------------------------------

  group('successful render', () {
    testWidgets('renders title after a successful load', (tester) async {
      final fakeClient = _FakeApiClient()..mediaResult = _kVideo;

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('media_detail_title')), findsOneWidget);
      expect(find.text('hero.mp4'), findsWidgets);
    });

    testWidgets('renders metadata row with codec, resolution, duration, size',
        (tester) async {
      final fakeClient = _FakeApiClient()..mediaResult = _kVideo;

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('media_detail_metadata')), findsOneWidget);
      // Codec.
      expect(find.textContaining('h264/aac'), findsOneWidget);
      // Resolution.
      expect(find.textContaining('1920x1080'), findsOneWidget);
      // Duration: 7320 s = 2h 2m 0s → "2:02:00".
      expect(find.textContaining('2:02:00'), findsOneWidget);
      // File size: 1 GiB → "1.0 GB".
      expect(find.textContaining('1.0 GB'), findsOneWidget);
    });

    testWidgets('renders tag chips when tags are present', (tester) async {
      final fakeClient = _FakeApiClient()..mediaResult = _kVideo;

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('media_detail_tags')), findsOneWidget);
      expect(find.text('action'), findsOneWidget);
      expect(find.text('english'), findsOneWidget);
    });

    testWidgets('shows tag picker (no chips) when tags list is empty',
        (tester) async {
      final fakeClient = _FakeApiClient()..mediaResult = _kAudio;

      await _pumpScreen(tester, fakeClient, mediaId: '7');
      await tester.pumpAndSettle();

      // TagPicker is always rendered so users can add tags to untagged items.
      expect(find.byKey(const Key('media_detail_tags')), findsOneWidget);
      // The chip row is empty — no individual tag chips should be present.
      expect(find.byKey(const Key('tag_picker_chips')), findsOneWidget);
    });

    testWidgets('renders play button', (tester) async {
      final fakeClient = _FakeApiClient()..mediaResult = _kVideo;

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('media_detail_play')), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Play button routing
  // --------------------------------------------------------------------------

  group('play button routing', () {
    testWidgets('tapping play on a video item routes to /video/:id',
        (tester) async {
      final fakeClient = _FakeApiClient()..mediaResult = _kVideo;

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // The play button may be below the visible area in the test viewport;
      // scroll it into view before tapping.
      await tester.ensureVisible(find.byKey(const Key('media_detail_play')));
      await tester.tap(find.byKey(const Key('media_detail_play')));
      await tester.pumpAndSettle();

      // The stub route at /video/:mediaId must be visible.
      expect(find.byKey(_kDestinationKey), findsOneWidget);
      expect(find.text('Video 42'), findsOneWidget);
    });

    testWidgets('tapping play on an audio item routes to /audio/:id',
        (tester) async {
      final fakeClient = _FakeApiClient()..mediaResult = _kAudio;

      await _pumpScreen(tester, fakeClient, mediaId: '7');
      await tester.pumpAndSettle();

      // The play button may be below the visible area in the test viewport;
      // scroll it into view before tapping.
      await tester.ensureVisible(find.byKey(const Key('media_detail_play')));
      await tester.tap(find.byKey(const Key('media_detail_play')));
      await tester.pumpAndSettle();

      // The stub route at /audio/:mediaId must be visible.
      expect(find.byKey(_kDestinationKey), findsOneWidget);
      expect(find.text('Audio 7'), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Favourite toggle
  // --------------------------------------------------------------------------

  group('favourite toggle', () {
    testWidgets('favourite icon is outlined when media is not a favourite',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..mediaResult = _kVideo // favorite: false
        ..toggleResult = true;

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // The outlined (unfilled) icon should be visible.
      expect(
        find.descendant(
          of: find.byKey(const Key('media_detail_favorite')),
          matching: find.byIcon(Icons.favorite_border),
        ),
        findsOneWidget,
      );
    });

    testWidgets('favourite icon is filled when media is already a favourite',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..mediaResult = _kAudio // favorite: true
        ..toggleResult = false;

      await _pumpScreen(tester, fakeClient, mediaId: '7');
      await tester.pumpAndSettle();

      expect(
        find.descendant(
          of: find.byKey(const Key('media_detail_favorite')),
          matching: find.byIcon(Icons.favorite),
        ),
        findsOneWidget,
      );
    });

    testWidgets('tapping favourite toggle calls toggleFavorite once',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..mediaResult = _kVideo
        ..toggleResult = true;

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('media_detail_favorite')));
      await tester.pumpAndSettle();

      expect(fakeClient.toggleCallCount, equals(1));
    });

    testWidgets('favourite icon updates to filled after a successful toggle',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..mediaResult = _kVideo // favorite: false
        ..toggleResult = true; // server confirms new state

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Starts as outlined (not favourite).
      expect(
        find.descendant(
          of: find.byKey(const Key('media_detail_favorite')),
          matching: find.byIcon(Icons.favorite_border),
        ),
        findsOneWidget,
      );

      await tester.tap(find.byKey(const Key('media_detail_favorite')));
      await tester.pumpAndSettle();

      // After toggle the icon should be filled.
      expect(
        find.descendant(
          of: find.byKey(const Key('media_detail_favorite')),
          matching: find.byIcon(Icons.favorite),
        ),
        findsOneWidget,
      );
    });

    testWidgets(
        'optimistic update: icon flips before toggleFavorite returns',
        (tester) async {
      // Use the delayed client so we can observe the UI before the API responds.
      final fakeClient = _DelayedToggleFakeApiClient(
        mediaResult: _kVideo, // favorite: false
      );

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Starts as outlined (not favourite).
      expect(
        find.descendant(
          of: find.byKey(const Key('media_detail_favorite')),
          matching: find.byIcon(Icons.favorite_border),
        ),
        findsOneWidget,
      );

      // Tap — the toggle future is still pending.
      await tester.tap(find.byKey(const Key('media_detail_favorite')));
      await tester.pump(); // one frame: setState for optimistic update

      // Icon must already show filled (optimistic) before the API responds.
      expect(
        find.descendant(
          of: find.byKey(const Key('media_detail_favorite')),
          matching: find.byIcon(Icons.favorite),
        ),
        findsOneWidget,
      );

      // Resolve the API call so the test doesn't leave pending async work.
      fakeClient.completeToggle(true);
      await tester.pumpAndSettle();
    });

    testWidgets(
        'revert on error: icon reverts and SnackBar shown when toggleFavorite fails',
        (tester) async {
      final fakeClient = _DelayedToggleFakeApiClient(
        mediaResult: _kVideo, // favorite: false
      );

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Starts as outlined.
      expect(
        find.descendant(
          of: find.byKey(const Key('media_detail_favorite')),
          matching: find.byIcon(Icons.favorite_border),
        ),
        findsOneWidget,
      );

      // Tap — the toggle future is still pending.
      await tester.tap(find.byKey(const Key('media_detail_favorite')));
      await tester.pump(); // optimistic flip applied

      // Optimistic: icon is now filled.
      expect(
        find.descendant(
          of: find.byKey(const Key('media_detail_favorite')),
          matching: find.byIcon(Icons.favorite),
        ),
        findsOneWidget,
      );

      // Fail the API call.
      fakeClient.failToggle(Exception('network error'));
      await tester.pumpAndSettle();

      // Icon must revert to outlined.
      expect(
        find.descendant(
          of: find.byKey(const Key('media_detail_favorite')),
          matching: find.byIcon(Icons.favorite_border),
        ),
        findsOneWidget,
      );

      // SnackBar must be visible.
      expect(
        find.text('Could not update favourite. Try again.'),
        findsOneWidget,
      );
    });
  });

  // --------------------------------------------------------------------------
  // Error state
  // --------------------------------------------------------------------------

  group('error state', () {
    testWidgets('shows error message when getMedia throws a network error',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..mediaError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/media/42'),
          type: DioExceptionType.connectionError,
        );

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('media_detail_error')), findsOneWidget);
      expect(
        find.textContaining('Could not reach the server'),
        findsOneWidget,
      );
    });

    testWidgets('shows "not found" message on 404', (tester) async {
      final fakeClient = _FakeApiClient()
        ..mediaError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/media/42'),
          type: DioExceptionType.badResponse,
          response: Response(
            requestOptions: RequestOptions(path: '/api/v1/media/42'),
            statusCode: 404,
          ),
        );

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('media_detail_error')), findsOneWidget);
      expect(
        find.textContaining('Media not found'),
        findsOneWidget,
      );
    });

    testWidgets('retry button triggers a fresh getMedia call', (tester) async {
      final fakeClient = _FakeApiClient()
        ..mediaError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/media/42'),
          type: DioExceptionType.connectionError,
        );

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('media_detail_retry')), findsOneWidget);

      // Fix the error before tapping retry so the second call succeeds.
      fakeClient
        ..mediaError = null
        ..mediaResult = _kVideo;

      await tester.tap(find.byKey(const Key('media_detail_retry')));
      await tester.pumpAndSettle();

      // After a successful retry the title is rendered.
      expect(find.byKey(const Key('media_detail_title')), findsOneWidget);
      // getMedia was called twice: once on init, once on retry.
      expect(fakeClient.getMediaCallCount, equals(2));
    });
  });
}
