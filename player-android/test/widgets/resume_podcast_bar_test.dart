import 'dart:io';

import 'package:audio_service/audio_service.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:player_android/app_routes.dart';
import 'package:player_android/providers/audio_handler_provider.dart';
import 'package:player_android/providers/podcast_library_provider.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/services/playback_session_coordinator.dart';
import 'package:player_android/services/podcast_downloads.dart';
import 'package:player_android/services/podcast_feed_fetcher.dart';
import 'package:player_android/services/podcast_repository.dart';
import 'package:player_android/services/podcast_sync_store.dart';
import 'package:player_android/widgets/in_progress_podcasts_sheet.dart';
import 'package:player_android/widgets/music_mini_player.dart';
import 'package:player_android/widgets/resume_podcast_bar.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';

import '../support/idle_playback.dart';

class _Feed extends PodcastFeedFetcher {
  @override
  Future<PublisherFeed> fetch(String url,
          {String etag = '', String lastModified = ''}) async =>
      parsePodcastFeed(
          '<rss><channel><title>Daily show</title>'
          '<item><guid>one</guid><title>Episode one</title><pubDate>Mon, 02 Nov 2026 10:00:00 GMT</pubDate><enclosure url="https://publisher.invalid/one.mp3"/></item>'
          '<item><guid>two</guid><title>Episode two</title><pubDate>Tue, 03 Nov 2026 10:00:00 GMT</pubDate><enclosure url="https://publisher.invalid/two.mp3"/></item>'
          '</channel></rss>',
          fetchUrl: url);
}

/// Records which episode was resumed instead of touching files or network.
class _Downloads extends PodcastDownloads {
  _Downloads() : super(PodcastRepository(), PlaybackSessionCoordinator());
  final resumed = <(String, String, bool)>[];
  @override
  Future<PodcastPlaybackRequest> playbackRequest(
      String scope, String feed, Map<String, Object?> episode,
      {bool fromBeginning = false,
      bool allowStreaming = false,
      bool continuePlayback = true}) async {
    resumed.add((scope, episode['episode_key'] as String, allowStreaming));
    return PodcastPlaybackRequest(
        identity: 'podcast:${episode['episode_key']}',
        sourceUri: Uri.parse('https://publisher.invalid/one.mp3'),
        title: episode['title'] as String);
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  sqfliteFfiInit();
  databaseFactory = databaseFactoryFfi;

  test('offers the most recently listened unfinished episode', () async {
    final folder = await Directory.systemTemp.createTemp('resume-podcast-');
    final podcasts = PodcastRepository(
        databasePath: '${folder.path}/podcasts.db', fetcher: _Feed());
    final container = ProviderContainer(overrides: [
      podcastScopeProvider.overrideWith((ref) async => 'local'),
      podcastRepositoryProvider.overrideWithValue(podcasts),
    ]);
    final subscription = container.listen(resumePodcastProvider, (_, __) {});
    try {
      expect(await container.read(resumePodcastProvider.future), isNull);

      final feed =
          await podcasts.subscribe('local', 'https://publisher.invalid/feed');
      final keys = {
        for (final e in await podcasts.episodes(feed))
          e['title']: e['episode_key'] as String
      };
      final store = PodcastSyncStore(podcasts);
      await store.saveEpisode('local', feed, keys['Episode two']!, seconds: 30);
      // Saved later, so it is the one the listener was on last.
      await Future<void>.delayed(const Duration(milliseconds: 5));
      await store.saveEpisode('local', feed, keys['Episode one']!,
          seconds: 754);
      container.invalidate(resumePodcastProvider);
      var resumable = await container.read(resumePodcastProvider.future);
      expect(resumable?.scope, 'local');
      expect(resumable?.episode['title'], 'Episode one');
      expect(resumable?.episode['podcast_title'], 'Daily show');
      expect(resumable?.episode['position_seconds'], 754);

      // A finished episode is no longer offered.
      await store.saveEpisode('local', feed, keys['Episode one']!,
          completed: true);
      container.invalidate(resumePodcastProvider);
      resumable = await container.read(resumePodcastProvider.future);
      expect(resumable?.episode['title'], 'Episode two');
    } finally {
      subscription.close();
      container.dispose();
      await podcasts.close();
      await folder.delete(recursive: true);
    }
  });

  testWidgets('idle mini player resumes the podcast in one tap',
      (tester) async {
    final downloads = _Downloads();
    Object? opened;
    final router = GoRouter(initialLocation: '/test', routes: [
      GoRoute(
          path: '/test',
          builder: (_, __) =>
              const Scaffold(bottomNavigationBar: NowPlayingBar())),
      GoRoute(
          path: AppRoutes.podcastPlayer,
          builder: (_, state) {
            opened = state.extra;
            return const Text('Podcast player');
          }),
    ]);
    addTearDown(router.dispose);
    await tester.pumpWidget(ProviderScope(overrides: [
      ...idlePlaybackOverrides(),
      podcastScopeProvider.overrideWith((ref) async => 'local'),
      podcastDownloadsProvider.overrideWith((ref) => downloads),
      resumePodcastProvider.overrideWith((ref) async => (
            scope: 'local',
            episode: <String, Object?>{
              'feed_key': 'feed',
              'episode_key': 'one',
              'title': 'Episode one',
              'podcast_title': 'Daily show',
              'position_seconds': 754.0,
            }
          )),
    ], child: MaterialApp.router(routerConfig: router)));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('podcast_resume_bar')), findsOneWidget);
    expect(find.text('Episode one'), findsOneWidget);
    expect(find.text('Daily show · Resume at 12:34'), findsOneWidget);
    expect(find.bySemanticsLabel(RegExp('^Resume podcast: Episode one')),
        findsOneWidget);

    await tester.tap(find.byTooltip('Resume podcast'));
    await tester.pumpAndSettle();
    expect(find.text('Podcast player'), findsOneWidget);
    expect(downloads.resumed, [('local', 'one', true)]);
    expect((opened as PodcastPlaybackRequest).title, 'Episode one');

    // While the episode plays, the regular podcast mini player replaces it.
    router.go('/test');
    await tester.pumpAndSettle();
    final container =
        ProviderScope.containerOf(tester.element(find.byType(NowPlayingBar)));
    final handler = container.read(audioHandlerProvider);
    handler.setMediaRequest(opened! as PodcastPlaybackRequest);
    handler.playbackState.add(PlaybackState(
        processingState: AudioProcessingState.ready, playing: true));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('podcast_mini_player')), findsOneWidget);
    expect(find.byKey(const Key('podcast_resume_bar')), findsNothing);

    // Once it stops, the shortcut is back.
    handler.playbackState
        .add(PlaybackState(processingState: AudioProcessingState.idle));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('podcast_mini_player')), findsNothing);
    expect(find.byKey(const Key('podcast_resume_bar')), findsOneWidget);
  });

  testWidgets('lists every podcast in progress and resumes the picked one',
      (tester) async {
    final downloads = _Downloads();
    Map<String, Object?> episode(String key, String title, double seconds) => {
          'feed_key': 'feed',
          'episode_key': key,
          'title': title,
          'podcast_title': 'Daily show',
          'position_seconds': seconds,
        };
    final router = GoRouter(initialLocation: '/test', routes: [
      GoRoute(
          path: '/test',
          builder: (_, __) =>
              const Scaffold(bottomNavigationBar: NowPlayingBar())),
      GoRoute(
          path: AppRoutes.podcastPlayer,
          builder: (_, __) => const Text('Podcast player')),
    ]);
    addTearDown(router.dispose);
    await tester.pumpWidget(ProviderScope(overrides: [
      ...idlePlaybackOverrides(),
      podcastScopeProvider.overrideWith((ref) async => 'local'),
      podcastDownloadsProvider.overrideWith((ref) => downloads),
      resumePodcastProvider.overrideWith((ref) async =>
          (scope: 'local', episode: episode('one', 'Episode one', 754))),
      inProgressPodcastsProvider.overrideWith((ref) async => (
            scope: 'local',
            rows: [
              episode('one', 'Episode one', 754),
              episode('two', 'Episode two', 30),
            ]
          )),
    ], child: MaterialApp.router(routerConfig: router)));
    await tester.pumpAndSettle();

    await tester.tap(find.byTooltip('Podcasts in progress'));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('in_progress_podcasts_sheet')), findsOneWidget);
    expect(find.text('Daily show · Resume at 0:30'), findsOneWidget);
    await tester.tap(find.text('Episode two'));
    await tester.pumpAndSettle();
    expect(find.text('Podcast player'), findsOneWidget);
    expect(downloads.resumed, [('local', 'two', true)]);
  });

  testWidgets('nothing in progress keeps the slot empty', (tester) async {
    await tester.pumpWidget(ProviderScope(
        overrides: idlePlaybackOverrides(),
        child: const MaterialApp(
            home: Scaffold(bottomNavigationBar: NowPlayingBar()))));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('podcast_resume_bar')), findsNothing);
  });
}
