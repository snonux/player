import 'dart:async';
import 'dart:io';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';
import 'package:player_android/services/podcast_feed_fetcher.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/screens/podcast_discovery_screen.dart';
import 'package:player_android/services/podcast_discovery.dart';
import 'package:player_android/providers/podcast_library_provider.dart';
import 'package:player_android/services/podcast_repository.dart';

class Directory extends PodcastDiscovery {
  final queries = <String>[];
  Completer<List<DiscoveredPodcast>>? gate;
  bool fail = false;
  final countries = <String>[];
  @override
  Future<List<DiscoveredPodcast>> popular(String country) async {
    countries.add(country);
    return [
      const DiscoveredPodcast(
          'Popular', 'Publisher', 'https://example.org/popular')
    ];
  }

  @override
  Future<List<DiscoveredPodcast>> search(String query) async {
    queries.add(query);
    if (fail) throw StateError('offline');
    return gate?.future ??
        Future.value([
          const DiscoveredPodcast(
              'Space', 'Publisher', 'https://example.org/feed')
        ]);
  }
}

class Repository extends PodcastRepository {
  final added = <(String, String)>[];
  @override
  Future<String> subscribe(String scope, String url) async {
    added.add((scope, url));
    return url;
  }

  @override
  Future<List<Map<String, Object?>>> subscriptions(String scope) async => [];
}

class OfflinePublisher extends PodcastFeedFetcher {
  @override
  Future<PublisherFeed> fetch(String url,
      {String etag = '', String lastModified = ''}) async {
    throw const SocketException('publisher offline');
  }
}

void main() {
  sqfliteFfiInit();
  databaseFactory = databaseFactoryFfiNoIsolate;

  testWidgets(
      'publisher failure reveals the subscription saved by the real repository',
      (tester) async {
    final repository = PodcastRepository(
        databasePath: inMemoryDatabasePath, fetcher: OfflinePublisher());
    final container = ProviderContainer(overrides: [
      podcastDiscoveryProvider.overrideWithValue(Directory()),
      podcastRepositoryProvider.overrideWithValue(repository),
      podcastScopeProvider.overrideWith((ref) async => 'local'),
    ]);
    try {
      // Cache the real provider's empty result before attempting a subscription.
      final initial = await tester
          .runAsync(() => container.read(podcastSubscriptionsProvider.future));
      expect(initial, isEmpty);
      await tester.pumpWidget(UncontrolledProviderScope(
          container: container,
          child: const MaterialApp(home: PodcastDiscoveryScreen())));
      await tester.enterText(find.byType(TextField), 'space');
      await tester.tap(find.text('Search'));
      await tester.pumpAndSettle();
      await tester.tap(find.byTooltip('Subscribe to Space'));
      // SQLite completes outside Flutter's fake clock. Let its event loop run
      // between frames while waiting for the actual publisher-error UI.
      for (var attempt = 0; attempt < 100; attempt++) {
        await tester.runAsync(
            () => Future<void>.delayed(const Duration(milliseconds: 10)));
        await tester.pump();
        if (find
            .textContaining('Could not fetch this feed.')
            .evaluate()
            .isNotEmpty) {
          break;
        }
      }
      expect(find.textContaining('Could not fetch this feed.'), findsOneWidget);
      final rows = await tester
          .runAsync(() => container.read(podcastSubscriptionsProvider.future));
      expect(rows!.single['fetch_url'], 'https://example.org/feed');
      expect(rows.single['last_error'], isNotEmpty);
      expect(find.textContaining('Could not fetch this feed.'), findsOneWidget);
      expect(find.byTooltip('Subscribe to Space'), findsOneWidget);
      final stored =
          await tester.runAsync(() => repository.subscriptions('local'));
      expect(stored!.single['subscribed'], 1);
    } finally {
      await tester.pumpWidget(const SizedBox());
      container.dispose();
      await tester.runAsync(repository.close);
    }
  });
  testWidgets(
      'explicit search, loading, subscribe, error retry and URL remain usable',
      (tester) async {
    final directory = Directory()..gate = Completer();
    final repository = Repository();
    await tester.pumpWidget(ProviderScope(overrides: [
      podcastDiscoveryProvider.overrideWithValue(directory),
      podcastRepositoryProvider.overrideWithValue(repository),
      podcastScopeProvider.overrideWith((ref) async => 'local'),
    ], child: const MaterialApp(home: PodcastDiscoveryScreen())));
    expect(directory.queries, isEmpty);
    await tester.enterText(find.byType(TextField), 'space');
    await tester.tap(find.text('Search'));
    await tester.pump();
    expect(find.text('Searching Apple Podcasts…'), findsOneWidget);
    directory.gate!.complete([
      const DiscoveredPodcast('Space', 'Publisher', 'https://example.org/feed')
    ]);
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('Subscribe to Space'));
    await tester.pumpAndSettle();
    expect(repository.added, [('local', 'https://example.org/feed')]);
    expect(find.text('Subscribed to Space.'), findsOneWidget);
    directory.fail = true;
    await tester.tap(find.text('Search'));
    await tester.pumpAndSettle();
    expect(find.textContaining('Search unavailable.'), findsOneWidget);
    await tester.tap(find.byTooltip('Podcast options'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Add feed URL'));
    await tester.pumpAndSettle();
    await tester.enterText(
        find.byType(TextField).last, 'https://manual.example/feed');
    await tester.tap(find.text('Subscribe'));
    await tester.pumpAndSettle();
    expect(repository.added.last, ('local', 'https://manual.example/feed'));
    await tester.pump(const Duration(seconds: 1));
    await tester.ensureVisible(find.text('Popular podcasts'));
    await tester.tap(find.text('Popular podcasts'));
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.text('Load popular podcasts'));
    await tester.tap(find.text('Load popular podcasts'));
    await tester.pumpAndSettle();
    expect(directory.countries, ['US']);
    expect(find.text('Popular'), findsOneWidget);
  });
}
