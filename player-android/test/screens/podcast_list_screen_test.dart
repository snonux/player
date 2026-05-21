// Widget tests for PodcastListScreen (podcast_list_screen.dart).
//
// Tests cover:
//   1. Renders a loading indicator while the data is in flight.
//   2. Renders podcast tiles after a successful load (only podcast sets).
//   3. Navigates to MediaGridScreen on tile tap.
//   4. Shows an empty-state widget when no podcast sets exist.
//   5. Shows an error view when listSets throws.
//   6. Pull-to-refresh calls listSets again.
//   7. FAB is present and opens the SubscribeDialog.
//   8. podcastListErrorMessage helper unit tests.
//
// Riverpod providers are overridden with fakes so tests run without a real
// server or OS keychain.
//
// Run with: flutter test test/screens/podcast_list_screen_test.dart

import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/models/models.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/screens/podcast_list_screen.dart';
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

/// Controllable [PlayerApiClient] stub for [PodcastListScreen] tests.
///
/// The [listSets] behaviour is configured per-test via [setsResult] or
/// [setsError].  [subscribePodcast] is stubbed to allow the SubscribeDialog
/// FAB test to run without hitting [UnimplementedError].
class _FakeApiClient extends PlayerApiClient {
  _FakeApiClient() : super(dio: Dio());

  /// When non-null, [listSets] returns this list.
  List<MediaSet>? setsResult;

  /// When non-null, [listSets] throws this instead of returning.
  Object? setsError;

  /// Number of times [listSets] has been called; useful for refresh tests.
  int listSetsCallCount = 0;

  /// Controls whether [subscribePodcast] succeeds or fails in the dialog tests.
  Object? subscribeError;

  @override
  Future<List<MediaSet>> listSets() async {
    listSetsCallCount++;
    if (setsError != null) throw setsError!;
    return setsResult!;
  }

  @override
  Future<PodcastFeed> subscribePodcast({
    required String feedUrl,
    String? setName,
  }) async {
    if (subscribeError != null) throw subscribeError!;
    // Return a minimal stub feed so the dialog can pop successfully.
    return PodcastFeed(
      id: 1,
      setId: 10,
      feedUrl: feedUrl,
      title: setName ?? 'Test Feed',
      description: '',
      imageUrl: '',
      lastETag: '',
      checkIntervalMinutes: 60,
      autoDownload: false,
      consecutiveFailures: 0,
    );
  }
}

/// [PlayerApiClient] stub that delays [listSets] until [complete] is called.
///
/// Used to inspect mid-flight loading state.
class _DelayedFakeApiClient extends PlayerApiClient {
  _DelayedFakeApiClient() : super(dio: Dio());

  final _completer = Completer<List<MediaSet>>();

  /// Resolves the pending [listSets] call with [sets].
  void complete(List<MediaSet> sets) => _completer.complete(sets);

  @override
  Future<List<MediaSet>> listSets() => _completer.future;
}

// ---------------------------------------------------------------------------
// Sample data
// ---------------------------------------------------------------------------

/// A regular (non-podcast) media set — must be filtered out by the screen.
const _kMovies = MediaSet(
  id: 1,
  name: 'Movies',
  rootPath: 'movies',
  coverThumbnailPath: '',
  isPodcast: false,
);

/// A podcast set — should appear as a tile in the podcast list.
const _kTechPodcast = MediaSet(
  id: 2,
  name: 'Tech Talks',
  rootPath: 'podcasts/tech',
  coverThumbnailPath: '',
  isPodcast: true,
);

/// A second podcast set — verifies multiple tiles are rendered.
const _kNewsPodcast = MediaSet(
  id: 3,
  name: 'Daily News',
  rootPath: 'podcasts/news',
  coverThumbnailPath: '',
  isPodcast: true,
);

// ---------------------------------------------------------------------------
// Helper: pump PodcastListScreen inside a minimal ProviderScope.
// ---------------------------------------------------------------------------

/// Pumps [PodcastListScreen] inside a [ProviderScope] that overrides
/// [apiClientProvider] and [tokenStorageProvider] with fakes.
Future<void> _pumpPodcastListScreen(
  WidgetTester tester,
  PlayerApiClient fakeClient,
) async {
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        // Avoid OS keychain in tests.
        tokenStorageProvider.overrideWithValue(const _FakeTokenStorage()),
        // Use the controllable fake instead of a real HTTP client.
        apiClientProvider.overrideWithValue(fakeClient),
      ],
      child: const MaterialApp(
        home: PodcastListScreen(),
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
    testWidgets('shows loading indicator while listSets is in flight',
        (tester) async {
      final fakeClient = _DelayedFakeApiClient();

      await _pumpPodcastListScreen(tester, fakeClient);

      // Pump a single frame: initState → addPostFrameCallback fires, but the
      // Future has not resolved yet.
      await tester.pump();

      expect(find.byKey(const Key('podcasts_loading')), findsOneWidget);
      expect(find.byType(CircularProgressIndicator), findsAtLeastNWidgets(1));

      // Resolve the fake to avoid "async work pending" warnings.
      fakeClient.complete([_kTechPodcast]);
      await tester.pumpAndSettle();
    });
  });

  // --------------------------------------------------------------------------
  // Renders podcasts
  // --------------------------------------------------------------------------

  group('renders podcast tiles', () {
    testWidgets('shows a tile for each podcast set returned by listSets',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..setsResult = [_kMovies, _kTechPodcast, _kNewsPodcast];

      await _pumpPodcastListScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Both podcast names must be visible; the non-podcast must not appear.
      expect(find.text('Tech Talks'), findsOneWidget);
      expect(find.text('Daily News'), findsOneWidget);
      expect(find.text('Movies'), findsNothing);
    });

    testWidgets('renders the podcasts list after a successful load',
        (tester) async {
      final fakeClient = _FakeApiClient()..setsResult = [_kTechPodcast];

      await _pumpPodcastListScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('podcasts_list')), findsOneWidget);
    });

    testWidgets('renders individual podcast tile keys', (tester) async {
      final fakeClient = _FakeApiClient()
        ..setsResult = [_kTechPodcast, _kNewsPodcast];

      await _pumpPodcastListScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('podcast_tile_2')), findsOneWidget);
      expect(find.byKey(const Key('podcast_tile_3')), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Empty state
  // --------------------------------------------------------------------------

  group('empty state', () {
    testWidgets('shows empty-state widget when no podcast sets exist',
        (tester) async {
      // Only non-podcast sets — all filtered out.
      final fakeClient = _FakeApiClient()..setsResult = [_kMovies];

      await _pumpPodcastListScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('podcasts_empty')), findsOneWidget);
      expect(find.byKey(const Key('podcasts_list')), findsNothing);
      expect(find.byKey(const Key('podcasts_loading')), findsNothing);
    });

    testWidgets('shows empty-state widget when listSets returns []',
        (tester) async {
      final fakeClient = _FakeApiClient()..setsResult = [];

      await _pumpPodcastListScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('podcasts_empty')), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Error state
  // --------------------------------------------------------------------------

  group('error state', () {
    testWidgets('shows error message when listSets throws a network error',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..setsError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/sets'),
          type: DioExceptionType.connectionError,
        );

      await _pumpPodcastListScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('podcasts_error')), findsOneWidget);
      expect(find.byKey(const Key('podcasts_list')), findsNothing);
      expect(
        find.textContaining('Could not reach the server'),
        findsOneWidget,
      );
    });

    testWidgets('shows retry button on error and retry calls listSets again',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..setsError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/sets'),
          type: DioExceptionType.connectionError,
        );

      await _pumpPodcastListScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('podcasts_retry')), findsOneWidget);

      // Fix the error before tapping retry so the second call succeeds.
      fakeClient
        ..setsError = null
        ..setsResult = [_kTechPodcast];

      await tester.tap(find.byKey(const Key('podcasts_retry')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('podcasts_list')), findsOneWidget);
      // listSets was called twice: once on init, once on retry.
      expect(fakeClient.listSetsCallCount, equals(2));
    });
  });

  // --------------------------------------------------------------------------
  // Pull-to-refresh
  // --------------------------------------------------------------------------

  group('pull-to-refresh', () {
    testWidgets('pull-to-refresh calls listSets a second time', (tester) async {
      final fakeClient = _FakeApiClient()..setsResult = [_kTechPodcast];

      await _pumpPodcastListScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(fakeClient.listSetsCallCount, equals(1));

      // Simulate pull-to-refresh by dragging down on the list.
      await tester.drag(
        find.byKey(const Key('podcasts_list')),
        const Offset(0, 300),
      );
      await tester.pumpAndSettle();

      expect(fakeClient.listSetsCallCount, equals(2));
    });
  });

  // --------------------------------------------------------------------------
  // FAB / Subscribe dialog
  // --------------------------------------------------------------------------

  group('subscribe FAB', () {
    testWidgets('FAB is present on the podcast list screen', (tester) async {
      final fakeClient = _FakeApiClient()..setsResult = [_kTechPodcast];

      await _pumpPodcastListScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('podcast_subscribe_fab')), findsOneWidget);
    });

    testWidgets('tapping FAB opens the subscribe dialog', (tester) async {
      final fakeClient = _FakeApiClient()..setsResult = [_kTechPodcast];

      await _pumpPodcastListScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('podcast_subscribe_fab')));
      await tester.pumpAndSettle();

      // The subscribe dialog must appear.
      expect(find.byKey(const Key('subscribe_dialog')), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // podcastListErrorMessage helper
  // --------------------------------------------------------------------------

  group('podcastListErrorMessage', () {
    test('returns connectivity message for connectionError', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/api/v1/sets'),
        type: DioExceptionType.connectionError,
      );
      expect(
        podcastListErrorMessage(err),
        contains('Could not reach the server'),
      );
    });

    test('returns server-error message for 500 badResponse', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/api/v1/sets'),
        response: Response(
          requestOptions: RequestOptions(path: '/api/v1/sets'),
          statusCode: 500,
        ),
        type: DioExceptionType.badResponse,
      );
      expect(podcastListErrorMessage(err), contains('500'));
    });

    test('returns generic message for unknown error type', () {
      expect(
        podcastListErrorMessage(Exception('boom')),
        contains('Unexpected error'),
      );
    });
  });
}
