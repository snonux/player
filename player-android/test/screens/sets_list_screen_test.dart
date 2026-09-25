// Widget tests for SetsListScreen (home_screen.dart).
//
// Tests cover:
//   1. Renders a loading indicator while the data is in flight.
//   2. Renders a list/grid of sets after a successful load.
//   3. Shows a podcast badge (microphone icon) on podcast sets.
//   4. Navigates to the media-grid screen on set-card tap.
//   5. Shows an empty-state widget when listSets returns [].
//   6. Shows an error view when listSets throws.
//   7. Pull-to-refresh calls listSets again.
//
// Riverpod providers are overridden with fakes so tests run without a real
// server or OS keychain.
//
// Run with: flutter test test/screens/sets_list_screen_test.dart

import 'dart:async';

import 'package:cached_network_image/cached_network_image.dart';
import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:player_android/app_routes.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/models/models.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/screens/home_screen.dart';
import 'package:player_android/utils/error_mappers.dart';

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

/// In-memory [TokenStorage] used to avoid the platform-specific OS keychain.
class _FakeTokenStorage implements TokenStorage {
  const _FakeTokenStorage([this.token = 'test-token']);

  final String? token;

  @override
  Future<String?> readToken() async => token;

  @override
  Future<void> writeToken(String token) async {}

  @override
  Future<void> deleteToken() async {}
}

/// Controllable [PlayerApiClient] stub for [SetsListScreen] tests.
///
/// The [listSets] behaviour is configured per-test via [setsResult] or
/// [setsError].  Every other method remains [UnimplementedError] — the screen
/// only calls [listSets] and [setFolderCoverUrl].
///
/// [setFolderCoverUrl] is overridden so [_CoverImage] does not make real HTTP
/// requests during widget tests.  The default returns an empty string, which
/// [_CoverImage] short-circuits to the folder-icon placeholder.  Tests that
/// want to assert the cover URL pattern can set [coverUrlBuilder] to a custom
/// function.
class _FakeApiClient extends PlayerApiClient {
  _FakeApiClient() : super(dio: Dio());

  /// When non-null, [listSets] returns this list.
  List<MediaSet>? setsResult;

  /// When non-null, [listSets] throws this instead of returning.
  Object? setsError;

  /// Number of times [listSets] has been called; useful for refresh tests.
  int listSetsCallCount = 0;

  /// Builds the cover URL returned to [_CoverImage].  Defaults to an empty
  /// string so the placeholder is shown without any network I/O.  Tests can
  /// override this to assert URL construction or to drive the network branch.
  String Function(int setId, {String? folder})? coverUrlBuilder;

  @override
  Future<List<MediaSet>> listSets() async {
    listSetsCallCount++;
    if (setsError != null) throw setsError!;
    return setsResult!;
  }

  @override
  String setFolderCoverUrl(int setId, {String? folder}) {
    final builder = coverUrlBuilder;
    return builder == null ? '' : builder(setId, folder: folder);
  }
}

/// [PlayerApiClient] stub that delays [listSets] until [complete] is called.
///
/// Used to inspect mid-flight loading state.  Like [_FakeApiClient], the cover
/// URL defaults to '' so no real HTTP traffic is generated during the pump.
class _DelayedFakeApiClient extends PlayerApiClient {
  _DelayedFakeApiClient() : super(dio: Dio());

  final _completer = Completer<List<MediaSet>>();

  /// Resolves the pending [listSets] call with [sets].
  void complete(List<MediaSet> sets) => _completer.complete(sets);

  @override
  Future<List<MediaSet>> listSets() => _completer.future;

  @override
  String setFolderCoverUrl(int setId, {String? folder}) => '';
}

// ---------------------------------------------------------------------------
// Sample data
// ---------------------------------------------------------------------------

/// A regular (non-podcast) media set used across tests.
const _kMovies = MediaSet(
  id: 1,
  name: 'Movies',
  rootPath: 'movies',
  coverThumbnailPath: '',
  isPodcast: false,
);

/// A podcast set — should display the podcast badge.
const _kPodcasts = MediaSet(
  id: 2,
  name: 'Tech Podcasts',
  rootPath: 'podcasts',
  coverThumbnailPath: '',
  isPodcast: true,
);

// ---------------------------------------------------------------------------
// Helper: pump SetsListScreen inside a minimal ProviderScope.
// ---------------------------------------------------------------------------

/// Pumps [SetsListScreen] inside a [ProviderScope] that overrides
/// [apiClientProvider] and [tokenStorageProvider] with fakes.
///
/// Returns the pumped [WidgetTester] for further interaction.
Future<void> _pumpSetsListScreen(
    WidgetTester tester, PlayerApiClient fakeClient,
    {TokenStorage storage = const _FakeTokenStorage()}) async {
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        // Avoid OS keychain in tests.
        tokenStorageProvider.overrideWithValue(storage),
        credentialMutationQueueProvider.overrideWithValue(
            CredentialMutationQueue(credentialsEnabled: true)),
        playerBaseUrlProvider
            .overrideWithValue(Uri.parse('http://test.invalid')),
        // Use the controllable fake instead of a real HTTP client.
        apiClientProvider.overrideWithValue(fakeClient),
      ],
      child: const MaterialApp(
        home: SetsListScreen(),
      ),
    ),
  );
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

void main() {
  testWidgets('Library menu reaches each feature and Back returns to Library',
      (tester) async {
    final client = _FakeApiClient()..setsResult = [];
    final destinations = {
      'Continue Watching': AppRoutes.continueWatching,
      'Podcasts': AppRoutes.podcasts,
      'My Shares': AppRoutes.shares,
      'Settings': AppRoutes.settings,
    };
    final router = GoRouter(initialLocation: AppRoutes.home, routes: [
      GoRoute(path: AppRoutes.home, builder: (_, __) => const SetsListScreen()),
      for (final entry in destinations.entries)
        GoRoute(
            path: entry.value,
            builder: (_, __) => Scaffold(
                  appBar: AppBar(title: Text('Destination ${entry.key}')),
                )),
    ]);
    addTearDown(router.dispose);
    await tester.pumpWidget(ProviderScope(overrides: [
      apiClientProvider.overrideWithValue(client),
      tokenStorageProvider.overrideWithValue(const _FakeTokenStorage()),
      credentialMutationQueueProvider
          .overrideWithValue(CredentialMutationQueue(credentialsEnabled: true)),
      playerBaseUrlProvider.overrideWithValue(Uri.parse('http://test.invalid')),
    ], child: MaterialApp.router(routerConfig: router)));
    await tester.pumpAndSettle();
    for (final entry in destinations.entries) {
      await tester.tap(find.byTooltip('Open navigation menu'));
      await tester.pumpAndSettle();
      await tester.tap(find.descendant(
          of: find.byType(Drawer), matching: find.text(entry.key)));
      await tester.pumpAndSettle();
      expect(find.text('Destination ${entry.key}'), findsOneWidget);
      await tester.tap(find.byTooltip('Back'));
      await tester.pumpAndSettle();
      expect(find.text('Library'), findsOneWidget);
      expect(find.byType(Drawer), findsNothing);
    }
  });

  // --------------------------------------------------------------------------
  // Loading state
  // --------------------------------------------------------------------------

  group('loading state', () {
    testWidgets('shows loading indicator while listSets is in flight',
        (tester) async {
      // Delay the response so we can inspect mid-flight state.
      final fakeClient = _DelayedFakeApiClient();

      await _pumpSetsListScreen(tester, fakeClient);

      // Pump a single frame: initState → addPostFrameCallback fires, but the
      // Future has not resolved yet.
      await tester.pump();

      // Before the first frame the widget shows a loading spinner.
      expect(find.byKey(const Key('sets_loading')), findsOneWidget);
      expect(find.byType(CircularProgressIndicator), findsOneWidget);

      // Resolve the fake to avoid "async work pending" warnings.
      fakeClient.complete([_kMovies]);
      await tester.pumpAndSettle();
    });
  });

  // --------------------------------------------------------------------------
  // Renders sets
  // --------------------------------------------------------------------------

  group('renders sets', () {
    testWidgets('shows a card for each set returned by listSets',
        (tester) async {
      final fakeClient = _FakeApiClient()..setsResult = [_kMovies, _kPodcasts];

      await _pumpSetsListScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Both set names must be visible.
      expect(find.text('Movies'), findsOneWidget);
      expect(find.text('Tech Podcasts'), findsOneWidget);

      // A card widget is rendered for each set.
      expect(find.byKey(const Key('set_card_1')), findsOneWidget);
      expect(find.byKey(const Key('set_card_2')), findsOneWidget);
    });

    testWidgets('renders the sets grid after a successful load',
        (tester) async {
      final fakeClient = _FakeApiClient()..setsResult = [_kMovies];

      await _pumpSetsListScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // The grid widget is present once data is loaded.
      expect(find.byKey(const Key('sets_grid')), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Podcast badge
  // --------------------------------------------------------------------------

  group('podcast badge', () {
    testWidgets('shows podcast badge for podcast sets', (tester) async {
      final fakeClient = _FakeApiClient()..setsResult = [_kMovies, _kPodcasts];

      await _pumpSetsListScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // The podcast badge (microphone icon container) is rendered exactly once:
      // on the podcast set card only.
      expect(find.byKey(const Key('podcast_badge')), findsOneWidget);
    });

    testWidgets('does not show podcast badge for non-podcast sets',
        (tester) async {
      final fakeClient = _FakeApiClient()..setsResult = [_kMovies];

      await _pumpSetsListScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // No podcast badge when all sets are regular media.
      expect(find.byKey(const Key('podcast_badge')), findsNothing);
    });
  });

  // --------------------------------------------------------------------------
  // Cover image
  // --------------------------------------------------------------------------

  group('cover image', () {
    testWidgets('cover cache is isolated across bearer credentials',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..setsResult = [_kMovies]
        ..coverUrlBuilder = (int setId, {String? folder}) =>
            'http://test.invalid/api/v1/sets/$setId/cover';

      Future<String?> cacheKeyFor(String token) async {
        await _pumpSetsListScreen(tester, fakeClient,
            storage: _FakeTokenStorage(token));
        await tester.pump();
        await tester.pump();
        return tester
            .widget<CachedNetworkImage>(find.byType(CachedNetworkImage))
            .cacheKey;
      }

      final firstKey = await cacheKeyFor('pt-account-a');
      await tester.pumpWidget(const SizedBox.shrink());
      final secondKey = await cacheKeyFor('pt-account-b');

      expect(firstKey, isNotNull);
      expect(secondKey, isNot(equals(firstKey)));
    });

    testWidgets('does not send cover credentials to another origin',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..setsResult = [_kMovies]
        ..coverUrlBuilder = (int setId, {String? folder}) =>
            'https://other.example/api/v1/sets/$setId/cover';

      await _pumpSetsListScreen(tester, fakeClient);
      await tester.pump();
      await tester.pump();

      expect(find.byType(CachedNetworkImage), findsNothing);
    });

    testWidgets('uses restored bearer for covers when cookie jar is empty',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..setsResult = [_kMovies]
        ..coverUrlBuilder = (int setId, {String? folder}) =>
            'http://test.invalid/api/v1/sets/$setId/cover';

      await _pumpSetsListScreen(tester, fakeClient);
      await tester.pump();
      await tester.pump();

      final cover =
          tester.widget<CachedNetworkImage>(find.byType(CachedNetworkImage));
      expect(cover.httpHeaders?['Authorization'], 'Bearer test-token');
      expect(cover.httpHeaders?.containsKey('Cookie'), isFalse);
    });

    testWidgets('omits bearer header when no token is stored', (tester) async {
      final fakeClient = _FakeApiClient()
        ..setsResult = [_kMovies]
        ..coverUrlBuilder = (int setId, {String? folder}) =>
            'http://test.invalid/api/v1/sets/$setId/cover';

      await _pumpSetsListScreen(tester, fakeClient,
          storage: const _FakeTokenStorage(null));
      await tester.pump();
      await tester.pump();

      final cover =
          tester.widget<CachedNetworkImage>(find.byType(CachedNetworkImage));
      expect(cover.httpHeaders?.containsKey('Authorization'), isFalse);
    });

    testWidgets(
        'requests <base>/api/v1/sets/{id}/cover for every set with a builder',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..setsResult = [_kMovies, _kPodcasts]
        // Custom builder that yields a stable URL per set id; the test asserts
        // that this exact URL ends up on a CachedNetworkImage widget.
        ..coverUrlBuilder = (int setId, {String? folder}) =>
            'http://test.invalid/api/v1/sets/$setId/cover';

      await _pumpSetsListScreen(tester, fakeClient);
      // One pump frame is enough for initState's post-frame callback to run
      // and for the widget tree to settle without waiting for the network
      // request (which would hang the test).
      await tester.pump();
      await tester.pump();

      // Every CachedNetworkImage in the tree must point at the cover endpoint
      // for one of the sets.
      final images = tester
          .widgetList<CachedNetworkImage>(find.byType(CachedNetworkImage))
          .toList();
      expect(images, hasLength(2));
      expect(
        images.map((i) => i.imageUrl).toSet(),
        equals({
          'http://test.invalid/api/v1/sets/1/cover',
          'http://test.invalid/api/v1/sets/2/cover',
        }),
      );
    });

    testWidgets('renders folder-icon placeholder when no cover URL is provided',
        (tester) async {
      // Default fake returns '' for setFolderCoverUrl — same outcome as a 404.
      final fakeClient = _FakeApiClient()..setsResult = [_kMovies];

      await _pumpSetsListScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // No CachedNetworkImage is created when the URL is empty (the widget
      // short-circuits to the placeholder, identical to a 404 errorWidget).
      expect(find.byType(CachedNetworkImage), findsNothing);
      // The folder-icon placeholder is shown in its place.
      expect(find.byIcon(Icons.folder_outlined), findsOneWidget);
    });

    testWidgets('podcast badge is overlaid on the cover image for podcast sets',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..setsResult = [_kPodcasts]
        ..coverUrlBuilder = (int setId, {String? folder}) =>
            'http://test.invalid/api/v1/sets/$setId/cover';

      await _pumpSetsListScreen(tester, fakeClient);
      await tester.pump();
      await tester.pump();

      // Both the network cover and the podcast badge coexist for podcast sets.
      expect(find.byType(CachedNetworkImage), findsOneWidget);
      expect(find.byKey(const Key('podcast_badge')), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Empty state
  // --------------------------------------------------------------------------

  group('empty state', () {
    testWidgets('shows empty-state widget when listSets returns []',
        (tester) async {
      final fakeClient = _FakeApiClient()..setsResult = [];

      await _pumpSetsListScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // The empty-state text is shown; the grid and loading indicator are not.
      expect(find.byKey(const Key('sets_empty')), findsOneWidget);
      expect(find.byKey(const Key('sets_grid')), findsNothing);
      expect(find.byKey(const Key('sets_loading')), findsNothing);
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

      await _pumpSetsListScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Error widget is visible; grid and loading indicator are not.
      expect(find.byKey(const Key('sets_error')), findsOneWidget);
      expect(find.byKey(const Key('sets_grid')), findsNothing);

      // The error message mentions the server/connection.
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

      await _pumpSetsListScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Retry button is present.
      expect(find.byKey(const Key('sets_retry')), findsOneWidget);

      // Fix the error before tapping retry so the second call succeeds.
      fakeClient
        ..setsError = null
        ..setsResult = [_kMovies];

      await tester.tap(find.byKey(const Key('sets_retry')));
      await tester.pumpAndSettle();

      // After a successful retry the grid is shown.
      expect(find.byKey(const Key('sets_grid')), findsOneWidget);
      // listSets was called twice: once on init, once on retry.
      expect(fakeClient.listSetsCallCount, equals(2));
    });
  });

  // --------------------------------------------------------------------------
  // Pull-to-refresh
  // --------------------------------------------------------------------------

  group('pull-to-refresh', () {
    testWidgets('pull-to-refresh calls listSets a second time', (tester) async {
      final fakeClient = _FakeApiClient()..setsResult = [_kMovies];

      await _pumpSetsListScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Verify initial load.
      expect(fakeClient.listSetsCallCount, equals(1));

      // Simulate pull-to-refresh by dragging down on the grid.
      await tester.drag(
        find.byKey(const Key('sets_grid')),
        const Offset(0, 300),
      );
      await tester.pumpAndSettle();

      // listSets must have been called a second time.
      expect(fakeClient.listSetsCallCount, equals(2));
    });
  });

  // --------------------------------------------------------------------------
  // setsErrorMessage helper
  // --------------------------------------------------------------------------

  group('setsErrorMessage', () {
    test('returns connectivity message for connectionError', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/api/v1/sets'),
        type: DioExceptionType.connectionError,
      );
      expect(setsErrorMessage(err), contains('Could not reach the server'));
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
      expect(setsErrorMessage(err), contains('500'));
    });

    test('returns session-expired message for 401 badResponse', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/api/v1/sets'),
        response: Response(
          requestOptions: RequestOptions(path: '/api/v1/sets'),
          statusCode: 401,
        ),
        type: DioExceptionType.badResponse,
      );
      expect(setsErrorMessage(err), contains('Session expired'));
    });

    test('returns generic message for unknown error type', () {
      expect(setsErrorMessage(Exception('boom')), contains('Unexpected error'));
    });
  });
}
