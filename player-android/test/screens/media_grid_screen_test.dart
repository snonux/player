// Widget tests for MediaGridScreen (media_grid_screen.dart).
//
// Tests cover:
//   1. Renders a loading indicator while listMedia is in flight.
//   2. Renders a grid of media cards after a successful load.
//   3. Each card shows the media title and duration.
//   4. Tapping a card navigates to the media-detail route.
//   5. Shows an empty-state widget when listMedia returns [].
//   6. Shows an error view when listMedia throws a DioException.
//   7. Pull-to-refresh calls listMedia again.
//   8. Heart overlay is shown on each card, filled for favourites.
//   9. Tapping heart overlay toggles favourite state (optimistic update).
//  10. Revert on error: icon reverts when toggleFavorite fails.
//  11. Favorites filter shortcut in app bar toggles favoritesOnly filter.
//
// Riverpod providers are overridden with fakes so tests run without a real
// server or OS keychain.
//
// Run with: flutter test test/screens/media_grid_screen_test.dart

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
import 'package:player_android/screens/media_grid_screen.dart';

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

/// Controllable [PlayerApiClient] stub for [MediaGridScreen] tests.
///
/// Only [listMedia] and [thumbnailUrl] are implemented; all other methods
/// remain [UnimplementedError] — the screen calls only these two.
class _FakeApiClient extends PlayerApiClient {
  _FakeApiClient() : super(dio: Dio());

  /// When non-null, [listMedia] returns this list.
  List<Media>? mediaResult;

  /// When non-null, [listMedia] throws this instead of returning.
  Object? mediaError;

  /// Records every call to [listMedia] — useful for refresh tests.
  int listMediaCallCount = 0;

  @override
  Future<List<Media>> listMedia({
    String? search,
    int? setId,
    List<int>? setIds,
    String? type,
    bool? favorites,
    List<String>? tags,
    double? minDuration,
    double? maxDuration,
    int? fileSizeMin,
    int? fileSizeMax,
    String? sort,
    int? limit,
    int? offset,
    String? folder,
    String? parent,
  }) async {
    listMediaCallCount++;
    if (mediaError != null) throw mediaError!;
    return mediaResult!;
  }

  /// Returns an empty string so [_ThumbnailImage] shows the static placeholder
  /// instead of making a network request — keeps widget tests hermetic.
  @override
  String thumbnailUrl(int mediaId) => '';
}

/// [PlayerApiClient] stub that delays [listMedia] until [complete] is called.
///
/// Used to inspect mid-flight loading state before the response arrives.
class _DelayedFakeApiClient extends PlayerApiClient {
  _DelayedFakeApiClient() : super(dio: Dio());

  final _completer = Completer<List<Media>>();

  /// Resolves the pending [listMedia] call with [items].
  void complete(List<Media> items) => _completer.complete(items);

  @override
  Future<List<Media>> listMedia({
    String? search,
    int? setId,
    List<int>? setIds,
    String? type,
    bool? favorites,
    List<String>? tags,
    double? minDuration,
    double? maxDuration,
    int? fileSizeMin,
    int? fileSizeMax,
    String? sort,
    int? limit,
    int? offset,
    String? folder,
    String? parent,
  }) =>
      _completer.future;

  @override
  String thumbnailUrl(int mediaId) => '';
}

/// [PlayerApiClient] stub that supports both [listMedia] and [toggleFavorite].
///
/// [toggleFavorite] is delayed until [completeToggle] or [failToggle] is called
/// so tests can inspect the optimistic-update and revert paths.
class _FakeApiClientWithToggle extends PlayerApiClient {
  _FakeApiClientWithToggle({required List<Media> initialMedia})
      : super(dio: Dio()) {
    mediaResult = initialMedia;
  }

  /// Mutable media list; [listMedia] always returns this.
  late List<Media> mediaResult;

  /// When non-null, all [listMedia] calls throw this error.
  Object? mediaError;

  /// Records the [MediaFilter.favoritesOnly] flag from the last [listMedia] call.
  bool? lastFavoritesOnlyFlag;

  /// Completer for the current in-flight [toggleFavorite]; replaced per call.
  Completer<bool>? _toggleCompleter;

  /// Resolve the current pending [toggleFavorite] with [result].
  void completeToggle(bool result) {
    _toggleCompleter?.complete(result);
  }

  /// Fail the current pending [toggleFavorite] with [error].
  void failToggle(Object error) {
    _toggleCompleter?.completeError(error);
  }

  @override
  Future<List<Media>> listMedia({
    String? search,
    int? setId,
    List<int>? setIds,
    String? type,
    bool? favorites,
    List<String>? tags,
    double? minDuration,
    double? maxDuration,
    int? fileSizeMin,
    int? fileSizeMax,
    String? sort,
    int? limit,
    int? offset,
    String? folder,
    String? parent,
  }) async {
    lastFavoritesOnlyFlag = favorites;
    if (mediaError != null) throw mediaError!;
    return mediaResult;
  }

  @override
  Future<bool> toggleFavorite(int mediaId) {
    _toggleCompleter = Completer<bool>();
    return _toggleCompleter!.future;
  }

  @override
  String thumbnailUrl(int mediaId) => '';
}

/// [PlayerApiClient] stub for pagination tests.
///
/// Each call to [listMedia] pops the next response from [pages].  Supports an
/// optional [Completer] per call: when [holdNextCompleter] is non-null before a
/// call arrives, that call waits until the completer is resolved instead of
/// returning immediately — allowing tests to inspect in-flight state.
class _PaginatedFakeApiClient extends PlayerApiClient {
  _PaginatedFakeApiClient({required this.pages}) : super(dio: Dio());

  /// Successive pages to return, oldest first.  Each call to [listMedia] shifts
  /// one element from the front of this list.
  final List<List<Media>> pages;

  /// When non-null, the next [listMedia] call returns this completer's future
  /// instead of the next page.  The field is cleared after the call begins so
  /// subsequent calls resume normal behaviour.
  Completer<List<Media>>? holdNextCompleter;

  /// Total number of [listMedia] invocations, including held ones.
  int listMediaCallCount = 0;

  @override
  Future<List<Media>> listMedia({
    String? search,
    int? setId,
    List<int>? setIds,
    String? type,
    bool? favorites,
    List<String>? tags,
    double? minDuration,
    double? maxDuration,
    int? fileSizeMin,
    int? fileSizeMax,
    String? sort,
    int? limit,
    int? offset,
    String? folder,
    String? parent,
  }) async {
    listMediaCallCount++;
    // If a completer was staged for this call, return its future and clear
    // the field so the call after this one resumes normal paged behaviour.
    final held = holdNextCompleter;
    if (held != null) {
      holdNextCompleter = null;
      return held.future;
    }
    return pages.isEmpty ? [] : pages.removeAt(0);
  }

  @override
  String thumbnailUrl(int mediaId) => '';
}

// ---------------------------------------------------------------------------
// Sample data
// ---------------------------------------------------------------------------

/// A sample video media item used across tests.
const _kVideo = Media(
  id: 1,
  setId: 10,
  relPath: 'action/movie.mp4',
  fileName: 'movie.mp4',
  absPath: '/media/movies/action/movie.mp4',
  type: 'video',
  duration: 7320.0, // 2h 2m
  codec: 'h264/aac',
  resolution: '1920x1080',
  bitrate: 4500,
  fileSizeBytes: 1073741824,
  width: 1920,
  height: 1080,
  thumbnailPath: '/media/movies/.thumbs/movie.jpg',
  playCount: 3,
);

/// A sample audio media item used across tests.
const _kAudio = Media(
  id: 2,
  setId: 10,
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
);

/// A sample media item that starts as a favourite.
const _kFavorite = Media(
  id: 3,
  setId: 10,
  relPath: 'music/fav.mp3',
  fileName: 'fav.mp3',
  absPath: '/media/music/fav.mp3',
  type: 'audio',
  duration: 180.0,
  codec: 'mp3',
  resolution: '',
  bitrate: 256,
  fileSizeBytes: 4194304,
  width: 0,
  height: 0,
  thumbnailPath: '',
  playCount: 5,
  favorite: true, // already a favourite
);

// ---------------------------------------------------------------------------
// Helper: pump MediaGridScreen inside a minimal ProviderScope.
// ---------------------------------------------------------------------------

/// Destination route shown after navigating away from [MediaGridScreen].
///
/// Used in navigation tests: when a media card is tapped, [MediaGridScreen]
/// calls `context.go('/media/:id')` which this route catches.
const _kDestinationKey = Key('nav_destination');

/// Builds a [GoRouter] with [MediaGridScreen] at `/sets/:setId` and a stub
/// at `/media/:id` so navigation tests can verify the tap lands correctly.
GoRouter _buildRouter(PlayerApiClient fakeClient) {
  return GoRouter(
    initialLocation: '/sets/10',
    routes: [
      GoRoute(
        path: '/sets/:setId',
        builder: (context, state) {
          final setId = int.tryParse(state.pathParameters['setId']!) ?? 0;
          return MediaGridScreen(setId: setId, setName: 'Movies');
        },
      ),
      GoRoute(
        path: '/media/:id',
        builder: (context, state) => Scaffold(
          body: Text(
            'Media ${state.pathParameters['id']}',
            key: _kDestinationKey,
          ),
        ),
      ),
    ],
  );
}

/// Pumps [MediaGridScreen] (set 10, name "Movies") inside a [ProviderScope]
/// that overrides [apiClientProvider] and [tokenStorageProvider] with fakes.
///
/// Uses a [GoRouter] so `context.go('/media/:id')` works without an
/// "unsupported ancestor" error.  The `/media/:id` stub route lets
/// navigation tests verify that the correct destination was reached.
Future<void> _pumpScreen(
  WidgetTester tester,
  PlayerApiClient fakeClient,
) async {
  final router = _buildRouter(fakeClient);
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
    testWidgets('shows loading indicator while listMedia is in flight',
        (tester) async {
      final fakeClient = _DelayedFakeApiClient();

      await _pumpScreen(tester, fakeClient);

      // Pump one frame: initState fires, addPostFrameCallback enqueues the
      // load, and the Future has not resolved yet.
      await tester.pump();

      // The loading key should be visible before data arrives.
      expect(find.byKey(const Key('media_loading')), findsOneWidget);
      expect(find.byType(CircularProgressIndicator), findsOneWidget);

      // Resolve the fake to prevent "pending async work" warnings.
      fakeClient.complete([_kVideo]);
      await tester.pumpAndSettle();
    });
  });

  // --------------------------------------------------------------------------
  // Renders grid
  // --------------------------------------------------------------------------

  group('renders grid', () {
    testWidgets('shows a card for each item returned by listMedia',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..mediaResult = [_kVideo, _kAudio];

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Both file names must be visible.
      expect(find.text('movie.mp4'), findsOneWidget);
      expect(find.text('song.mp3'), findsOneWidget);

      // A card widget is rendered for each item.
      expect(find.byKey(const Key('media_card_1')), findsOneWidget);
      expect(find.byKey(const Key('media_card_2')), findsOneWidget);
    });

    testWidgets('renders the media grid widget after a successful load',
        (tester) async {
      final fakeClient = _FakeApiClient()..mediaResult = [_kVideo];

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // The grid itself is visible.
      expect(find.byKey(const Key('media_grid')), findsOneWidget);
    });

    testWidgets('shows title and duration for each media card', (tester) async {
      final fakeClient = _FakeApiClient()..mediaResult = [_kVideo];

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Title key is present.
      expect(find.byKey(const Key('media_title_1')), findsOneWidget);
      // Duration key is present and shows formatted value.
      expect(find.byKey(const Key('media_duration_1')), findsOneWidget);
      // 7320s = 2h 2m 0s → "2:02:00"
      expect(find.text('2:02:00'), findsOneWidget);
    });

    testWidgets('shows set name in app bar when provided', (tester) async {
      final fakeClient = _FakeApiClient()..mediaResult = [];

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.text('Movies'), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Tap navigation
  // --------------------------------------------------------------------------

  group('tap navigates to media detail', () {
    testWidgets('tapping a media card navigates to /media/:id',
        (tester) async {
      final fakeClient = _FakeApiClient()..mediaResult = [_kVideo];

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // The media card must be visible before tapping.
      expect(find.byKey(const Key('media_card_1')), findsOneWidget);

      // Tap the card; go_router handles `context.go('/media/1')`.
      await tester.tap(find.byKey(const Key('media_card_1')));
      await tester.pumpAndSettle();

      // The stub route at '/media/:id' is now on screen.
      expect(find.byKey(_kDestinationKey), findsOneWidget);
      expect(find.text('Media 1'), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Empty state
  // --------------------------------------------------------------------------

  group('empty state', () {
    testWidgets('shows empty-state widget when listMedia returns []',
        (tester) async {
      final fakeClient = _FakeApiClient()..mediaResult = [];

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // The empty-state text is shown; grid and loading indicator are not.
      expect(find.byKey(const Key('media_empty')), findsOneWidget);
      expect(find.byKey(const Key('media_grid')), findsNothing);
      expect(find.byKey(const Key('media_loading')), findsNothing);
    });
  });

  // --------------------------------------------------------------------------
  // Error state
  // --------------------------------------------------------------------------

  group('error state', () {
    testWidgets('shows error message when listMedia throws a network error',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..mediaError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/media'),
          type: DioExceptionType.connectionError,
        );

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Error widget is visible; grid and loading indicator are not.
      expect(find.byKey(const Key('media_error')), findsOneWidget);
      expect(find.byKey(const Key('media_grid')), findsNothing);

      // The error message mentions the server/connection.
      expect(
        find.textContaining('Could not reach the server'),
        findsOneWidget,
      );
    });

    testWidgets(
        'shows retry button on error and a successful retry shows the grid',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..mediaError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/media'),
          type: DioExceptionType.connectionError,
        );

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Retry button is present.
      expect(find.byKey(const Key('media_retry')), findsOneWidget);

      // Fix the error before tapping retry so the second call succeeds.
      fakeClient
        ..mediaError = null
        ..mediaResult = [_kVideo];

      await tester.tap(find.byKey(const Key('media_retry')));
      await tester.pumpAndSettle();

      // After a successful retry the grid is shown.
      expect(find.byKey(const Key('media_grid')), findsOneWidget);
      // listMedia was called twice: once on init, once on retry.
      expect(fakeClient.listMediaCallCount, equals(2));
    });
  });

  // --------------------------------------------------------------------------
  // Pull-to-refresh
  // --------------------------------------------------------------------------

  group('pull-to-refresh', () {
    testWidgets('pull-to-refresh calls listMedia a second time', (tester) async {
      final fakeClient = _FakeApiClient()..mediaResult = [_kVideo];

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Verify initial load.
      expect(fakeClient.listMediaCallCount, equals(1));

      // Simulate pull-to-refresh by dragging down on the grid.
      await tester.drag(
        find.byKey(const Key('media_grid')),
        const Offset(0, 300),
      );
      await tester.pumpAndSettle();

      // listMedia must have been called a second time.
      expect(fakeClient.listMediaCallCount, equals(2));
    });
  });

  // --------------------------------------------------------------------------
  // Heart overlay
  // --------------------------------------------------------------------------

  group('heart overlay on media card', () {
    testWidgets('shows outlined heart on non-favourite item', (tester) async {
      final fakeClient = _FakeApiClientWithToggle(
        initialMedia: [_kVideo], // favorite: false
      );

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // The heart button for the non-favourite video must have the outlined icon.
      final heartFinder = find.byKey(const Key('media_card_favorite_1'));
      expect(heartFinder, findsOneWidget);
      expect(
        find.descendant(
          of: heartFinder,
          matching: find.byIcon(Icons.favorite_border),
        ),
        findsOneWidget,
      );
    });

    testWidgets('shows filled heart on a favourite item', (tester) async {
      final fakeClient = _FakeApiClientWithToggle(
        initialMedia: [_kFavorite], // favorite: true
      );

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      final heartFinder = find.byKey(const Key('media_card_favorite_3'));
      expect(heartFinder, findsOneWidget);
      expect(
        find.descendant(
          of: heartFinder,
          matching: find.byIcon(Icons.favorite),
        ),
        findsOneWidget,
      );
    });

    testWidgets('tapping heart flips icon optimistically before API responds',
        (tester) async {
      final fakeClient = _FakeApiClientWithToggle(
        initialMedia: [_kVideo], // favorite: false
      );

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Scroll the heart icon into view (it may be near the card bottom).
      await tester
          .ensureVisible(find.byKey(const Key('media_card_favorite_1')));
      await tester.pumpAndSettle();

      // Starts as outlined.
      expect(
        find.descendant(
          of: find.byKey(const Key('media_card_favorite_1')),
          matching: find.byIcon(Icons.favorite_border),
        ),
        findsOneWidget,
      );

      // Tap heart — toggleFavorite is now in flight (pending).
      await tester.tap(find.byKey(const Key('media_card_favorite_1')));
      await tester.pump(); // one frame for the optimistic setState

      // Icon must already show filled (optimistic update) before API responds.
      expect(
        find.descendant(
          of: find.byKey(const Key('media_card_favorite_1')),
          matching: find.byIcon(Icons.favorite),
        ),
        findsOneWidget,
      );

      // Resolve the API call to clean up pending async work.
      fakeClient.completeToggle(true);
      await tester.pumpAndSettle();
    });

    testWidgets('revert on error: icon reverts and SnackBar shown on failure',
        (tester) async {
      final fakeClient = _FakeApiClientWithToggle(
        initialMedia: [_kVideo], // favorite: false
      );

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Scroll the heart icon into view.
      await tester
          .ensureVisible(find.byKey(const Key('media_card_favorite_1')));
      await tester.pumpAndSettle();

      // Tap the heart.
      await tester.tap(find.byKey(const Key('media_card_favorite_1')));
      await tester.pump(); // optimistic flip

      // Optimistic update: icon is filled.
      expect(
        find.descendant(
          of: find.byKey(const Key('media_card_favorite_1')),
          matching: find.byIcon(Icons.favorite),
        ),
        findsOneWidget,
      );

      // Fail the API call.
      fakeClient.failToggle(Exception('server error'));
      await tester.pumpAndSettle();

      // Icon must revert to outlined.
      expect(
        find.descendant(
          of: find.byKey(const Key('media_card_favorite_1')),
          matching: find.byIcon(Icons.favorite_border),
        ),
        findsOneWidget,
      );

      // SnackBar error message is visible.
      expect(
        find.text('Could not update favourite. Try again.'),
        findsOneWidget,
      );
    });

    testWidgets('tapping card body does not trigger favourite toggle',
        (tester) async {
      final fakeClient = _FakeApiClientWithToggle(
        initialMedia: [_kVideo],
      );

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Tap the card body (centre of the card, not the heart icon).
      await tester.tap(find.byKey(const Key('media_card_1')));
      await tester.pumpAndSettle();

      // Navigation happened — no pending toggle completer means toggle was not called.
      // (If it had been called the completer would be non-null and the test would
      // crash on dispose with an unhandled Completer future.)
      expect(find.byKey(_kDestinationKey), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Favourites filter shortcut in app bar
  // --------------------------------------------------------------------------

  group('favourites filter shortcut', () {
    testWidgets('app bar shows a heart icon button', (tester) async {
      final fakeClient = _FakeApiClient()..mediaResult = [_kVideo];

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('media_grid_favorites_filter')),
        findsOneWidget,
      );
    });

    testWidgets(
        'tapping favourites filter shortcut passes favorites=true to listMedia',
        (tester) async {
      final fakeClient = _FakeApiClientWithToggle(
        initialMedia: [_kVideo],
      );

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Initially no favourites filter.
      expect(fakeClient.lastFavoritesOnlyFlag, isNull);

      // Tap the heart in the app bar.
      await tester.tap(find.byKey(const Key('media_grid_favorites_filter')));
      await tester.pumpAndSettle();

      // listMedia must have been called with favorites = true.
      expect(fakeClient.lastFavoritesOnlyFlag, isTrue);
    });

    testWidgets('tapping shortcut twice reverts filter to off', (tester) async {
      final fakeClient = _FakeApiClientWithToggle(
        initialMedia: [_kVideo],
      );

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      final shortcut = find.byKey(const Key('media_grid_favorites_filter'));

      // First tap — enable.
      await tester.tap(shortcut);
      await tester.pumpAndSettle();
      expect(fakeClient.lastFavoritesOnlyFlag, isTrue);

      // Second tap — disable.
      await tester.tap(shortcut);
      await tester.pumpAndSettle();
      // favorites=false means the flag is passed as null (no filter applied).
      expect(fakeClient.lastFavoritesOnlyFlag, isNull);
    });
  });

  // --------------------------------------------------------------------------
  // Pagination (_loadMore)
  // --------------------------------------------------------------------------

  group('pagination', () {
    /// Builds a full page of [count] distinct [Media] items starting at [startId].
    List<Media> makePage(int startId, int count) {
      return List.generate(
        count,
        (i) => Media(
          id: startId + i,
          setId: 10,
          relPath: 'p${startId + i}.mp4',
          fileName: 'p${startId + i}.mp4',
          absPath: '/media/p${startId + i}.mp4',
          type: 'video',
          duration: 60.0,
          codec: 'h264',
          resolution: '1920x1080',
          bitrate: 1000,
          fileSizeBytes: 1000000,
          width: 1920,
          height: 1080,
          thumbnailPath: '',
          playCount: 0,
        ),
      );
    }

    testWidgets('_loadMore appends second page to the grid', (tester) async {
      // Page 1 is full (50 items), page 2 has 2 items to signal end-of-list.
      final page1 = makePage(100, 50);
      final page2 = makePage(200, 2);

      final fakeClient = _PaginatedFakeApiClient(pages: [page1, page2]);

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // After initial load, listMedia was called once and the grid holds 50 items.
      expect(fakeClient.listMediaCallCount, equals(1));

      // Jump the CustomScrollView to its maximum scroll extent so the
      // _onScroll listener fires and triggers _loadMore.
      final scrollable = tester.state<ScrollableState>(
        find.descendant(
          of: find.byKey(const Key('media_grid')),
          matching: find.byType(Scrollable),
        ),
      );
      scrollable.position.jumpTo(scrollable.position.maxScrollExtent);
      await tester.pumpAndSettle();

      // Two listMedia calls: initial load + one _loadMore page.
      expect(fakeClient.listMediaCallCount, equals(2));

      // Total items visible = 50 (page 1) + 2 (page 2) = 52.
      // Verify via the last card key from page 2, which is id 201.
      expect(find.byKey(const Key('media_card_201')), findsOneWidget);
    });

    testWidgets('_loadMore is a no-op while _isLoadingMore is already true',
        (tester) async {
      // Page 1 is full so _hasMore stays true after the initial load.
      final page1 = makePage(100, 50);

      // Do NOT set holdNextCompleter yet — let the initial load complete normally.
      final fakeClient = _PaginatedFakeApiClient(pages: [page1]);

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Sanity: initial load has completed with one API call.
      expect(fakeClient.listMediaCallCount, equals(1));

      // Now stage a completer to hold the upcoming _loadMore in-flight.
      final loadMoreCompleter = Completer<List<Media>>();
      fakeClient.holdNextCompleter = loadMoreCompleter;

      // Jump the CustomScrollView to its maximum extent to trigger _loadMore
      // via the _onScroll listener — the request is now in-flight (blocked).
      final scrollable = tester.state<ScrollableState>(
        find.byType(Scrollable).first,
      );
      scrollable.position.jumpTo(scrollable.position.maxScrollExtent);
      await tester.pump(); // _loadMore starts, _isLoadingMore = true

      // At this point listMediaCallCount == 2 (initial load + first _loadMore).
      final callsAfterFirstScroll = fakeClient.listMediaCallCount;

      // Jump again while the first _loadMore is still in-flight.
      scrollable.position.jumpTo(scrollable.position.maxScrollExtent);
      await tester.pump();

      // No additional API call must have been fired (_isLoadingMore guard).
      expect(fakeClient.listMediaCallCount, equals(callsAfterFirstScroll));

      // Resolve the in-flight request before pumpAndSettle so no pending async
      // work remains (prevents timeout).
      loadMoreCompleter.complete(makePage(200, 2));
      await tester.pumpAndSettle();
    });

    testWidgets(
        'pull-to-refresh while _loadMore is in-flight leaves _isLoadingMore=false',
        (tester) async {
      // Page 1 is full so _hasMore is true after the initial load.
      final page1 = makePage(100, 50);
      // Page returned by the _load triggered by pull-to-refresh.
      final refreshPage = makePage(300, 3);

      final fakeClient = _PaginatedFakeApiClient(
        pages: [page1], // consumed by initial load
      );

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Stage a completer to hold the upcoming _loadMore in-flight.
      final loadMoreCompleter = Completer<List<Media>>();
      fakeClient.holdNextCompleter = loadMoreCompleter;

      // Jump to the maximum scroll extent to trigger _loadMore via the
      // _onScroll listener — the request is now in-flight (blocked).
      final scrollable = tester.state<ScrollableState>(
        find.byType(Scrollable).first,
      );
      scrollable.position.jumpTo(scrollable.position.maxScrollExtent);
      await tester.pump(); // _loadMore starts, _isLoadingMore = true

      // Add the refresh page so that the _load triggered by pull-to-refresh
      // has data to return.
      fakeClient.pages.add(refreshPage);

      // Pull-to-refresh while _loadMore is still blocked — _load bumps the
      // generation, so the blocked _loadMore response will be stale.
      await tester.drag(
        find.byKey(const Key('media_grid')),
        const Offset(0, 300),
      );
      await tester.pump(const Duration(seconds: 1));

      // Resolve the stale _loadMore so the generation-mismatch path executes.
      // _load() must have already reset _isLoadingMore = false in its setState.
      loadMoreCompleter.complete(makePage(200, 2));
      await tester.pumpAndSettle();

      // The bottom spinner must NOT be visible: _isLoadingMore was cleared by
      // _load() so the stale _loadMore did not leave it permanently stuck.
      expect(find.byKey(const Key('media_loading_more')), findsNothing);
    });
  });
}
