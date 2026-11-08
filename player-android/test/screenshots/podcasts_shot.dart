import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:player_android/podcast_routes.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/global_sync_provider.dart';
import 'package:player_android/providers/podcast_library_provider.dart';
import 'package:player_android/providers/theme_provider.dart';
import 'package:player_android/screens/podcast_list_screen.dart';

import '../screens/podcast_browsing_fakes.dart';
import 'screenshot_support.dart';

class ShotRepository extends BrowsingRepository {
  @override
  Future<List<Map<String, Object?>>> subscriptions(String scope) async => [
        for (final (key, title) in [
          ('a', 'The Engineering Hour'),
          ('b', 'Daily World News'),
          ('c', 'History Unplugged: Long Stories from the Past'),
        ])
          {
            'feed_key': 'feed:$key',
            'title': title,
            'fetch_url': 'https://example.com/$key/feed.xml',
            'browse_scope': 'local',
          }
      ];
  @override
  Future<List<Map<String, Object?>>> browseEpisodes(String scope,
      {String? feed,
      String filter = 'uncompleted',
      int offset = 0,
      int limit = 40}) async {
    final titles = [
      (
        'Episode 212: Building resilient distributed systems with boring tech',
        'The Engineering Hour',
        3600,
        1260.0,
        0
      ),
      ('Morning briefing, 8 November', 'Daily World News', 900, 0.0, 0),
      (
        'The fall of Constantinople, part 3',
        'History Unplugged: Long Stories from the Past',
        5400,
        0.0,
        0
      ),
      (
        'Episode 211: What we learned migrating to SQLite',
        'The Engineering Hour',
        3300,
        3300.0,
        1
      ),
      ('Evening briefing, 7 November', 'Daily World News', 840, 200.0, 0),
    ];
    return [
      for (final (i, t) in titles.indexed)
        if (filter == 'all' || (filter == 'completed') == (t.$5 == 1))
          {
            'feed_key': 'feed:$i',
            'episode_key': 'guid:$i',
            'scope_feed_key': 'feed:$i',
            'scope_episode_key': 'guid:$i',
            'browse_scope': 'local',
            'title': t.$1,
            'podcast_title': t.$2,
            'description':
                '<p>In this episode we talk about things. https://example.com/notes</p>',
            'duration_seconds': t.$3,
            'position_seconds': t.$4,
            'completed': t.$5,
            'published_at':
                DateTime.utc(2026, 11, 8 - i).millisecondsSinceEpoch,
            'enclosure_url': 'https://example.com/$i.mp3',
          }
    ];
  }
}

Widget shotApp(Widget screen, BrowsingRepository repository,
    {ThemeData? theme}) {
  final router = GoRouter(initialLocation: '/test', routes: [
    GoRoute(path: '/test', builder: (_, __) => screen),
    ...podcastRoutes(),
  ]);
  addTearDown(router.dispose);
  return ProviderScope(
      overrides: [
        podcastRepositoryProvider.overrideWithValue(repository),
        podcastRetentionProvider
            .overrideWithValue(BrowsingRetention(repository)),
        podcastServerMetadataProvider.overrideWithValue(null),
        podcastScopeProvider.overrideWith((ref) => Future.value('local')),
        podcastSyncStoreProvider.overrideWithValue(BrowsingStore(repository)),
        podcastDownloadsProvider
            .overrideWith((ref) => BrowsingDownloads(repository)),
        apiClientProvider.overrideWith((ref) => throw StateError('no server')),
        credentialMutationQueueProvider,
      ],
      child: MaterialApp.router(
          debugShowCheckedModeBanner: false,
          theme: theme ?? buildLightTheme(),
          routerConfig: router));
}

void main() {
  setUpAll(loadScreenshotFonts);
  moreShots();
  testWidgets('podcast episodes', (tester) async {
    usePhone(tester);
    await tester
        .pumpWidget(shotApp(const PodcastListScreen(), ShotRepository()));
    await tester.pumpAndSettle();
    await expectLater(find.byType(MaterialApp),
        matchesGoldenFile('out/podcast_episodes.png'));
    await tester.tap(find.text('Podcasts').first);
    await tester.pumpAndSettle();
    await expectLater(
        find.byType(MaterialApp), matchesGoldenFile('out/podcast_feeds.png'));
  });
}

void moreShots() {
  testWidgets('podcast grid and sheet', (tester) async {
    usePhone(tester);
    await tester
        .pumpWidget(shotApp(const PodcastListScreen(), ShotRepository()));
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('More episode actions').first);
    await tester.pumpAndSettle();
    await expectLater(
        find.byType(MaterialApp), matchesGoldenFile('out/podcast_sheet.png'));
    Navigator.of(tester.element(find.byKey(const Key('podcast_episode_sheet'))))
        .pop();
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('Podcast views'));
    await tester.pumpAndSettle();
    await expectLater(
        find.byType(MaterialApp), matchesGoldenFile('out/podcast_menu.png'));
    await tester.tap(find.text('Show as grid'));
    await tester.pumpAndSettle();
    await expectLater(
        find.byType(MaterialApp), matchesGoldenFile('out/podcast_grid.png'));
    await tester.tap(find.text('Podcasts').first);
    await tester.pumpAndSettle();
    await expectLater(find.byType(MaterialApp),
        matchesGoldenFile('out/podcast_feeds_grid.png'));
  });
}
