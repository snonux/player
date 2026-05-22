// Widget tests for PodcastEpisodesScreen (podcast_episodes_screen.dart).
//
// Tests cover:
//   1. Renders a loading indicator while listEpisodes is in flight.
//   2. Renders episode rows after a successful load.
//   3. Shows an empty-state widget when listEpisodes returns [].
//   4. Shows an error view when listEpisodes throws a DioException.
//   5. Pull-to-refresh calls listEpisodes again.
//   6. Played toggle: tapping fires onToggleComplete (optimistic update).
//   7. Progress bar is shown when positionSeconds > 0 and not completed.
//   8. Progress bar is hidden when episode is completed.
//   9. Progress bar is hidden when positionSeconds is 0.
//  10. Revert on error: played icon reverts when toggleEpisodeComplete fails.
//  11. episodeListErrorMessage and episodeToggleErrorMessage helper unit tests.
//  12. Play button visible when mediaId is non-null; tapping navigates to audio player.
//  13. Download button visible when mediaId is null; tapping calls downloadEpisode.
//  14. Download success updates row to show play button.
//  15. Download error shows SnackBar.
//  16. episodeDownloadErrorMessage helper unit tests.
//
// Riverpod providers are overridden with fakes so tests run without a real
// server or OS keychain.
//
// Run with: flutter test test/screens/podcast_episodes_screen_test.dart

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
import 'package:player_android/screens/podcast_episodes_screen.dart';
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

/// Controllable [PlayerApiClient] stub for [PodcastEpisodesScreen] tests.
///
/// Implements [listEpisodes], [toggleEpisodeComplete], and [downloadEpisode];
/// all other methods remain [UnimplementedError] — the screen calls only these.
class _FakeApiClient extends PlayerApiClient {
  _FakeApiClient() : super(dio: Dio());

  /// When non-null, [listEpisodes] returns this list.
  List<PodcastEpisode>? episodesResult;

  /// When non-null, [listEpisodes] throws this instead of returning.
  Object? episodesError;

  /// Records every call to [listEpisodes] — useful for refresh tests.
  int listEpisodesCallCount = 0;

  /// Completer for the current in-flight [toggleEpisodeComplete]; replaced
  /// per call so each test can control one toggle at a time.
  Completer<void>? _toggleCompleter;

  /// Resolves the current pending [toggleEpisodeComplete] successfully.
  void completeToggle() => _toggleCompleter?.complete();

  /// Rejects the current pending [toggleEpisodeComplete] with [error].
  void failToggle(Object error) => _toggleCompleter?.completeError(error);

  /// When non-null, [downloadEpisode] returns this [Media] result.
  Media? downloadResult;

  /// When non-null, [downloadEpisode] throws this error.
  Object? downloadError;

  /// Records every call to [downloadEpisode] — useful for download tests.
  int downloadEpisodeCallCount = 0;

  @override
  Future<List<PodcastEpisode>> listEpisodes(
    int podcastSetId, {
    int? limit,
    int? offset,
  }) async {
    listEpisodesCallCount++;
    if (episodesError != null) throw episodesError!;
    return episodesResult!;
  }

  @override
  Future<void> toggleEpisodeComplete(int episodeId) {
    _toggleCompleter = Completer<void>();
    return _toggleCompleter!.future;
  }

  @override
  Future<Media> downloadEpisode(int episodeId) async {
    downloadEpisodeCallCount++;
    if (downloadError != null) throw downloadError!;
    return downloadResult!;
  }
}

/// [PlayerApiClient] stub that delays [listEpisodes] until [complete] is
/// called.  Used to inspect the mid-flight loading state.
class _DelayedFakeApiClient extends PlayerApiClient {
  _DelayedFakeApiClient() : super(dio: Dio());

  final _completer = Completer<List<PodcastEpisode>>();

  /// Resolves the pending [listEpisodes] call with [episodes].
  void complete(List<PodcastEpisode> episodes) =>
      _completer.complete(episodes);

  @override
  Future<List<PodcastEpisode>> listEpisodes(
    int podcastSetId, {
    int? limit,
    int? offset,
  }) =>
      _completer.future;
}

// ---------------------------------------------------------------------------
// Sample data
// ---------------------------------------------------------------------------

/// An unplayed episode with no saved position.
const _kEpisode1 = PodcastEpisode(
  id: 1,
  feedId: 10,
  guid: 'ep-1',
  title: 'Introduction to Flutter',
  description: 'Episode 1',
  episodeUrl: 'https://example.com/ep1.mp3',
  fileName: 'ep1.mp3',
  isDownloaded: false,
  isCompleted: false,
  positionSeconds: 0,
  durationSeconds: 1800.0, // 30 min
);

/// An episode that has been partially played (halfway through).
const _kEpisodeInProgress = PodcastEpisode(
  id: 2,
  feedId: 10,
  guid: 'ep-2',
  title: 'Advanced Dart',
  description: 'Episode 2',
  episodeUrl: 'https://example.com/ep2.mp3',
  fileName: 'ep2.mp3',
  isDownloaded: false,
  isCompleted: false,
  positionSeconds: 900.0, // 15 min of 30 min
  durationSeconds: 1800.0,
);

/// An episode that has been fully played / marked as completed.
const _kEpisodeCompleted = PodcastEpisode(
  id: 3,
  feedId: 10,
  guid: 'ep-3',
  title: 'State Management',
  description: 'Episode 3',
  episodeUrl: 'https://example.com/ep3.mp3',
  fileName: 'ep3.mp3',
  isDownloaded: false,
  isCompleted: true,
  positionSeconds: 1800.0,
  durationSeconds: 1800.0,
);

/// An episode that has been downloaded (mediaId is non-null) and is playable.
const _kEpisodeDownloaded = PodcastEpisode(
  id: 4,
  feedId: 10,
  mediaId: 99,
  guid: 'ep-4',
  title: 'Async Await Deep Dive',
  description: 'Episode 4',
  episodeUrl: 'https://example.com/ep4.mp3',
  fileName: 'ep4.mp3',
  isDownloaded: true,
  isCompleted: false,
  positionSeconds: 0,
  durationSeconds: 2700.0,
);

// ---------------------------------------------------------------------------
// Stub Media used as the downloadEpisode response.
// ---------------------------------------------------------------------------

/// Minimal [Media] stub returned by the fake [downloadEpisode] implementation.
///
/// Only [id] is relevant to [PodcastEpisodesScreen]; the remaining required
/// fields are filled with zero-values so the const constructor compiles.
const _kDownloadedMedia = Media(
  id: 55,
  setId: 10,
  relPath: 'podcasts/ep1.mp3',
  fileName: 'ep1.mp3',
  absPath: '/media/podcasts/ep1.mp3',
  type: 'audio',
  duration: 1800.0,
  codec: 'mp3',
  resolution: '',
  bitrate: 128,
  fileSizeBytes: 0,
  width: 0,
  height: 0,
  thumbnailPath: '',
  playCount: 0,
);

// ---------------------------------------------------------------------------
// Helpers: pump PodcastEpisodesScreen inside a minimal ProviderScope.
// ---------------------------------------------------------------------------

/// Key used by the stub audio-player destination route in navigation tests.
const _kAudioPlayerDestKey = Key('nav_audio_player');

/// Pumps [PodcastEpisodesScreen] (set 10, "Tech Talks") inside a
/// [ProviderScope] that overrides [apiClientProvider] and
/// [tokenStorageProvider] with fakes.
///
/// Uses plain [MaterialApp] (no GoRouter) — suited for tests that do not
/// exercise navigation (i.e. no episode rows with [mediaId] non-null).
Future<void> _pumpScreen(
  WidgetTester tester,
  PlayerApiClient fakeClient,
) async {
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        tokenStorageProvider.overrideWithValue(const _FakeTokenStorage()),
        apiClientProvider.overrideWithValue(fakeClient),
      ],
      child: const MaterialApp(
        home: PodcastEpisodesScreen(setId: 10, setName: 'Tech Talks'),
      ),
    ),
  );
}

/// Builds a [GoRouter] with [PodcastEpisodesScreen] at its root and a stub
/// `/audio/:mediaId` route so navigation tests can verify the correct
/// destination is reached when the play button is tapped.
GoRouter _buildRouterWithAudioStub(PlayerApiClient fakeClient) {
  return GoRouter(
    initialLocation: '/podcasts/10/episodes',
    routes: [
      GoRoute(
        path: '/podcasts/:setId/episodes',
        builder: (context, state) {
          final setId = int.tryParse(state.pathParameters['setId']!) ?? 0;
          return PodcastEpisodesScreen(setId: setId, setName: 'Tech Talks');
        },
      ),
      GoRoute(
        path: '/audio/:mediaId',
        builder: (context, state) => Scaffold(
          body: Text(
            'Audio ${state.pathParameters['mediaId']}',
            key: _kAudioPlayerDestKey,
          ),
        ),
      ),
    ],
  );
}

/// Pumps [PodcastEpisodesScreen] inside a [GoRouter] so that navigation via
/// `context.go('/audio/:mediaId')` resolves correctly in play-button tests.
Future<void> _pumpScreenWithRouter(
  WidgetTester tester,
  PlayerApiClient fakeClient,
) async {
  final router = _buildRouterWithAudioStub(fakeClient);
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
    testWidgets('shows loading indicator while listEpisodes is in flight',
        (tester) async {
      final fakeClient = _DelayedFakeApiClient();

      await _pumpScreen(tester, fakeClient);

      // Pump a single frame: initState → addPostFrameCallback fires, but the
      // Future has not resolved yet.
      await tester.pump();

      expect(find.byKey(const Key('episodes_loading')), findsOneWidget);
      expect(find.byType(CircularProgressIndicator), findsAtLeastNWidgets(1));

      // Resolve the fake to avoid "async work pending" warnings.
      fakeClient.complete([_kEpisode1]);
      await tester.pumpAndSettle();
    });
  });

  // --------------------------------------------------------------------------
  // Renders episodes
  // --------------------------------------------------------------------------

  group('renders episode rows', () {
    testWidgets('shows a row for each episode returned by listEpisodes',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..episodesResult = [_kEpisode1, _kEpisodeInProgress];

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.text('Introduction to Flutter'), findsOneWidget);
      expect(find.text('Advanced Dart'), findsOneWidget);
    });

    testWidgets('renders the episodes list widget after a successful load',
        (tester) async {
      final fakeClient = _FakeApiClient()..episodesResult = [_kEpisode1];

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('episodes_list')), findsOneWidget);
    });

    testWidgets('renders individual episode row keys', (tester) async {
      final fakeClient = _FakeApiClient()
        ..episodesResult = [_kEpisode1, _kEpisodeInProgress];

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('episode_row_1')), findsOneWidget);
      expect(find.byKey(const Key('episode_row_2')), findsOneWidget);
    });

    testWidgets('shows set name in app bar when provided', (tester) async {
      final fakeClient = _FakeApiClient()..episodesResult = [];

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.text('Tech Talks'), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Played toggle — optimistic update
  // --------------------------------------------------------------------------

  group('played toggle — optimistic update', () {
    testWidgets(
        'tapping toggle on unplayed episode immediately shows completed icon',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..episodesResult = [_kEpisode1]; // isCompleted = false

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Before tap: check_circle_outline (unplayed).
      final toggleKey = find.byKey(const Key('episode_played_toggle_1'));
      expect(toggleKey, findsOneWidget);
      expect(
        find.descendant(
          of: toggleKey,
          matching: find.byIcon(Icons.check_circle_outline),
        ),
        findsOneWidget,
      );

      // Tap the toggle — optimistic update fires immediately.
      await tester.tap(toggleKey);
      await tester.pump(); // one frame: setState applied

      // After tap: check_circle (played), still before API call resolves.
      expect(
        find.descendant(
          of: toggleKey,
          matching: find.byIcon(Icons.check_circle),
        ),
        findsOneWidget,
      );

      // Resolve the API call to prevent "async work pending" warnings.
      fakeClient.completeToggle();
      await tester.pumpAndSettle();
    });

    testWidgets(
        'tapping toggle on completed episode immediately shows outline icon',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..episodesResult = [_kEpisodeCompleted]; // isCompleted = true

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      final toggleKey = find.byKey(const Key('episode_played_toggle_3'));

      // Before tap: check_circle (played).
      expect(
        find.descendant(
          of: toggleKey,
          matching: find.byIcon(Icons.check_circle),
        ),
        findsOneWidget,
      );

      await tester.tap(toggleKey);
      await tester.pump();

      // After tap: check_circle_outline (unplayed) — optimistic flip.
      expect(
        find.descendant(
          of: toggleKey,
          matching: find.byIcon(Icons.check_circle_outline),
        ),
        findsOneWidget,
      );

      fakeClient.completeToggle();
      await tester.pumpAndSettle();
    });
  });

  // --------------------------------------------------------------------------
  // Played toggle — revert on error
  // --------------------------------------------------------------------------

  group('played toggle — revert on error', () {
    testWidgets('reverts to original state when toggleEpisodeComplete fails',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..episodesResult = [_kEpisode1]; // isCompleted = false

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      final toggleKey = find.byKey(const Key('episode_played_toggle_1'));

      // Tap to trigger optimistic update.
      await tester.tap(toggleKey);
      await tester.pump();

      // Optimistic state: check_circle (played).
      expect(
        find.descendant(
          of: toggleKey,
          matching: find.byIcon(Icons.check_circle),
        ),
        findsOneWidget,
      );

      // Fail the API call — the screen should revert.
      fakeClient.failToggle(
        DioException(
          requestOptions: RequestOptions(path: '/api/v1/podcasts/episodes/1/complete'),
          type: DioExceptionType.connectionError,
        ),
      );
      await tester.pumpAndSettle();

      // After revert: check_circle_outline (unplayed) again.
      expect(
        find.descendant(
          of: toggleKey,
          matching: find.byIcon(Icons.check_circle_outline),
        ),
        findsOneWidget,
      );

      // A SnackBar error message is shown.
      expect(find.byType(SnackBar), findsOneWidget);
      expect(
        find.textContaining('Could not reach'),
        findsOneWidget,
      );
    });
  });

  // --------------------------------------------------------------------------
  // Progress bar visibility
  // --------------------------------------------------------------------------

  group('progress bar visibility', () {
    testWidgets(
        'shows progress bar when positionSeconds > 0 and not completed',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..episodesResult = [_kEpisodeInProgress]; // pos=900, dur=1800, not completed

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('episode_progress_bar')), findsOneWidget);
    });

    testWidgets('hides progress bar when episode is completed', (tester) async {
      final fakeClient = _FakeApiClient()
        ..episodesResult = [_kEpisodeCompleted]; // isCompleted = true

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('episode_progress_bar')), findsNothing);
    });

    testWidgets('hides progress bar when positionSeconds is 0', (tester) async {
      final fakeClient = _FakeApiClient()
        ..episodesResult = [_kEpisode1]; // positionSeconds = 0

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('episode_progress_bar')), findsNothing);
    });

    testWidgets('progress bar value reflects fraction of duration played',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..episodesResult = [_kEpisodeInProgress]; // 900/1800 = 0.5

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      final progressBar = tester.widget<LinearProgressIndicator>(
        find.byKey(const Key('episode_progress_bar')),
      );
      // 900 / 1800 = 0.5; allow floating-point tolerance.
      expect(progressBar.value, closeTo(0.5, 0.001));
    });
  });

  // --------------------------------------------------------------------------
  // Empty state
  // --------------------------------------------------------------------------

  group('empty state', () {
    testWidgets('shows empty-state widget when listEpisodes returns []',
        (tester) async {
      final fakeClient = _FakeApiClient()..episodesResult = [];

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('episodes_empty')), findsOneWidget);
      expect(find.byKey(const Key('episodes_list')), findsNothing);
      expect(find.byKey(const Key('episodes_loading')), findsNothing);
    });
  });

  // --------------------------------------------------------------------------
  // Error state
  // --------------------------------------------------------------------------

  group('error state', () {
    testWidgets('shows error message when listEpisodes throws a network error',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..episodesError = DioException(
          requestOptions:
              RequestOptions(path: '/api/v1/podcasts/10/episodes'),
          type: DioExceptionType.connectionError,
        );

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('episodes_error')), findsOneWidget);
      expect(find.byKey(const Key('episodes_list')), findsNothing);
      expect(
        find.textContaining('Could not reach the server'),
        findsOneWidget,
      );
    });

    testWidgets('shows retry button and a successful retry renders the list',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..episodesError = DioException(
          requestOptions:
              RequestOptions(path: '/api/v1/podcasts/10/episodes'),
          type: DioExceptionType.connectionError,
        );

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('episodes_retry')), findsOneWidget);

      // Fix the error before tapping retry.
      fakeClient
        ..episodesError = null
        ..episodesResult = [_kEpisode1];

      await tester.tap(find.byKey(const Key('episodes_retry')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('episodes_list')), findsOneWidget);
      // listEpisodes was called twice: once on init, once on retry.
      expect(fakeClient.listEpisodesCallCount, equals(2));
    });
  });

  // --------------------------------------------------------------------------
  // Pull-to-refresh
  // --------------------------------------------------------------------------

  group('pull-to-refresh', () {
    testWidgets('pull-to-refresh calls listEpisodes a second time',
        (tester) async {
      final fakeClient = _FakeApiClient()..episodesResult = [_kEpisode1];

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(fakeClient.listEpisodesCallCount, equals(1));

      // Simulate pull-to-refresh by dragging down on the list.
      await tester.drag(
        find.byKey(const Key('episodes_list')),
        const Offset(0, 300),
      );
      await tester.pumpAndSettle();

      expect(fakeClient.listEpisodesCallCount, equals(2));
    });
  });

  // --------------------------------------------------------------------------
  // episodeListErrorMessage helper
  // --------------------------------------------------------------------------

  group('episodeListErrorMessage', () {
    test('returns connectivity message for connectionError', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/api/v1/podcasts/10/episodes'),
        type: DioExceptionType.connectionError,
      );
      expect(
        episodeListErrorMessage(err),
        contains('Could not reach the server'),
      );
    });

    test('returns not-found message for 404', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/api/v1/podcasts/10/episodes'),
        response: Response(
          requestOptions: RequestOptions(path: '/api/v1/podcasts/10/episodes'),
          statusCode: 404,
        ),
        type: DioExceptionType.badResponse,
      );
      expect(episodeListErrorMessage(err), contains('not found'));
    });

    test('returns generic message for unknown error type', () {
      expect(
        episodeListErrorMessage(Exception('boom')),
        contains('Unexpected error'),
      );
    });
  });

  // --------------------------------------------------------------------------
  // episodeToggleErrorMessage helper
  // --------------------------------------------------------------------------

  group('episodeToggleErrorMessage', () {
    test('returns connectivity message for connectionError', () {
      final err = DioException(
        requestOptions:
            RequestOptions(path: '/api/v1/podcasts/episodes/1/complete'),
        type: DioExceptionType.connectionError,
      );
      expect(
        episodeToggleErrorMessage(err),
        contains('Could not reach'),
      );
    });

    test('returns not-found message for 404', () {
      final err = DioException(
        requestOptions:
            RequestOptions(path: '/api/v1/podcasts/episodes/1/complete'),
        response: Response(
          requestOptions:
              RequestOptions(path: '/api/v1/podcasts/episodes/1/complete'),
          statusCode: 404,
        ),
        type: DioExceptionType.badResponse,
      );
      expect(episodeToggleErrorMessage(err), contains('not found'));
    });

    test('returns permission message for 403', () {
      final err = DioException(
        requestOptions:
            RequestOptions(path: '/api/v1/podcasts/episodes/1/complete'),
        response: Response(
          requestOptions:
              RequestOptions(path: '/api/v1/podcasts/episodes/1/complete'),
          statusCode: 403,
        ),
        type: DioExceptionType.badResponse,
      );
      expect(episodeToggleErrorMessage(err), contains('permission'));
    });

    test('returns generic message for non-Dio error', () {
      expect(
        episodeToggleErrorMessage(Exception('boom')),
        contains('Could not update episode'),
      );
    });
  });

  // --------------------------------------------------------------------------
  // Play button
  // --------------------------------------------------------------------------

  group('play button', () {
    testWidgets(
        'shows play button when episode has a mediaId (is downloaded)',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..episodesResult = [_kEpisodeDownloaded]; // mediaId: 99

      await _pumpScreenWithRouter(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('episode_play_button_4')),
        findsOneWidget,
      );
      // Download button must not appear when mediaId is set.
      expect(
        find.byKey(const Key('episode_download_button_4')),
        findsNothing,
      );
    });

    testWidgets(
        'tapping play button navigates to audio player screen',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..episodesResult = [_kEpisodeDownloaded]; // mediaId: 99

      await _pumpScreenWithRouter(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('episode_play_button_4')));
      await tester.pumpAndSettle();

      // Navigation should have pushed the stub audio-player route.
      expect(find.byKey(_kAudioPlayerDestKey), findsOneWidget);
      expect(find.text('Audio 99'), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Download button
  // --------------------------------------------------------------------------

  group('download button', () {
    testWidgets(
        'shows download button when episode has no mediaId (not downloaded)',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..episodesResult = [_kEpisode1]; // mediaId: null

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('episode_download_button_1')),
        findsOneWidget,
      );
      // Play button must not appear when mediaId is null.
      expect(
        find.byKey(const Key('episode_play_button_1')),
        findsNothing,
      );
    });

    testWidgets(
        'tapping download button calls downloadEpisode',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..episodesResult = [_kEpisode1]
        ..downloadResult = _kDownloadedMedia;

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('episode_download_button_1')));
      await tester.pumpAndSettle();

      expect(fakeClient.downloadEpisodeCallCount, equals(1));
    });

    testWidgets(
        'successful download replaces download button with play button',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..episodesResult = [_kEpisode1]
        ..downloadResult = _kDownloadedMedia; // returns mediaId 55

      await _pumpScreenWithRouter(tester, fakeClient);
      await tester.pumpAndSettle();

      // Before download: download button visible, play button absent.
      expect(
        find.byKey(const Key('episode_download_button_1')),
        findsOneWidget,
      );

      await tester.tap(find.byKey(const Key('episode_download_button_1')));
      await tester.pumpAndSettle();

      // After successful download: play button visible, download button absent.
      expect(find.byKey(const Key('episode_play_button_1')), findsOneWidget);
      expect(
        find.byKey(const Key('episode_download_button_1')),
        findsNothing,
      );
    });

    testWidgets(
        'download error shows SnackBar with error message',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..episodesResult = [_kEpisode1]
        ..downloadError = DioException(
          requestOptions: RequestOptions(
            path: '/api/v1/podcasts/episodes/1/download',
          ),
          type: DioExceptionType.connectionError,
        );

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('episode_download_button_1')));
      await tester.pumpAndSettle();

      expect(find.byType(SnackBar), findsOneWidget);
      expect(find.textContaining('Could not reach'), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // episodeDownloadErrorMessage helper
  // --------------------------------------------------------------------------

  group('episodeDownloadErrorMessage', () {
    test('returns connectivity message for connectionError', () {
      final err = DioException(
        requestOptions: RequestOptions(
          path: '/api/v1/podcasts/episodes/1/download',
        ),
        type: DioExceptionType.connectionError,
      );
      expect(
        episodeDownloadErrorMessage(err),
        contains('Could not reach'),
      );
    });

    test('returns not-found message for 404', () {
      final err = DioException(
        requestOptions: RequestOptions(
          path: '/api/v1/podcasts/episodes/1/download',
        ),
        response: Response(
          requestOptions: RequestOptions(
            path: '/api/v1/podcasts/episodes/1/download',
          ),
          statusCode: 404,
        ),
        type: DioExceptionType.badResponse,
      );
      expect(episodeDownloadErrorMessage(err), contains('not found'));
    });

    test('returns permission message for 403', () {
      final err = DioException(
        requestOptions: RequestOptions(
          path: '/api/v1/podcasts/episodes/1/download',
        ),
        response: Response(
          requestOptions: RequestOptions(
            path: '/api/v1/podcasts/episodes/1/download',
          ),
          statusCode: 403,
        ),
        type: DioExceptionType.badResponse,
      );
      expect(episodeDownloadErrorMessage(err), contains('permission'));
    });

    test('returns already-downloaded message for 409', () {
      final err = DioException(
        requestOptions: RequestOptions(
          path: '/api/v1/podcasts/episodes/1/download',
        ),
        response: Response(
          requestOptions: RequestOptions(
            path: '/api/v1/podcasts/episodes/1/download',
          ),
          statusCode: 409,
        ),
        type: DioExceptionType.badResponse,
      );
      expect(episodeDownloadErrorMessage(err), contains('already downloaded'));
    });

    test('returns generic message for non-Dio error', () {
      expect(
        episodeDownloadErrorMessage(Exception('boom')),
        contains('Could not download episode'),
      );
    });
  });
}
