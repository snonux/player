import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/providers/podcast_library_provider.dart';
import 'package:player_android/screens/standalone_podcasts_screen.dart';
import 'package:player_android/services/podcast_downloads.dart';
import 'package:player_android/services/podcast_repository.dart';
import 'package:player_android/services/playback_session_coordinator.dart';

const episode = <String, Object?>{
  'episode_key': 'guid:one',
  'title': 'Episode one',
  'description': 'Description',
  'feed_key': 'feed:example/feed',
  'scope_feed_key': 'feed:example/feed',
  'scope_episode_key': 'guid:one',
  'completed': 0,
  'position_seconds': 0.0,
  'duration_seconds': 0,
  'enclosure_url': '',
  'podcast_title': 'Podcast'
};

class _Repository extends PodcastRepository {
  @override
  Future<List<Map<String, Object?>>> subscriptions(String scope) async => [
        {
          'feed_key': 'feed:example/feed',
          'title': 'Podcast',
          'fetch_url': 'https://example/feed'
        }
      ];
  @override
  Future<List<Map<String, Object?>>> episodes(String feedKey) async =>
      [episode];
  @override
  Future<List<Map<String, Object?>>> browseEpisodes(String scope,
          {String? feed,
          String filter = 'uncompleted',
          int offset = 0,
          int limit = 40}) async =>
      [episode];
}

class _Downloads extends PodcastDownloads {
  _Downloads(PodcastRepository repository)
      : super(repository, PlaybackSessionCoordinator());
  String state = 'missing';
  var attempts = 0;
  Completer<void>? pending;
  @override
  Future<Map<String, Object?>?> copy(String feed, String episode) async =>
      {'state': state, 'error': 'Publisher unavailable. Retry download.'};
  @override
  Future<void> download(String feed, String episode) async {
    attempts++;
    state = 'pending';
    pending = Completer<void>();
    notifyListeners();
    await pending!.future;
  }

  void finish(String value) {
    state = value;
    pending?.complete();
    pending = null;
    notifyListeners();
  }

  @override
  void cancel() {
    if (pending != null) finish('missing');
  }

  @override
  Future<void> remove(String feed, String episode) async {
    state = 'missing';
    notifyListeners();
  }
}

void main() {
  testWidgets(
      'reachable cached list offers labeled download, cancel, retry, play and remove',
      (tester) async {
    final repository = _Repository();
    final downloads = _Downloads(repository);
    await tester.pumpWidget(ProviderScope(overrides: [
      podcastRepositoryProvider.overrideWithValue(repository),
      podcastScopeProvider.overrideWith((ref) async => 'local'),
      podcastDownloadsProvider.overrideWith((ref) => downloads),
    ], child: const MaterialApp(home: StandalonePodcastsScreen())));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Podcast'));
    await tester.pumpAndSettle();
    expect(find.text('Episode one'), findsOneWidget);
    await tester.tap(find.text('Download'));
    await tester.pumpAndSettle();
    expect(find.text('Download pending'), findsOneWidget);
    await tester.tap(find.text('Cancel download'));
    await tester.pumpAndSettle();
    expect(find.text('Download'), findsOneWidget);
    await tester.tap(find.text('Download'));
    await tester.pumpAndSettle();
    downloads.finish('failed');
    await tester.pumpAndSettle();
    expect(find.text('Retry download'), findsOneWidget);
    await tester.tap(find.text('Retry download'));
    await tester.pumpAndSettle();
    downloads.finish('available');
    await tester.pumpAndSettle();
    expect(find.text('Available on this device'), findsOneWidget);
    expect(find.text('Play'), findsOneWidget);
    await tester.tap(find.text('Delete downloaded file'));
    await tester.pumpAndSettle();
    expect(find.text('Download'), findsOneWidget);
    expect(find.text('Episode one'), findsOneWidget);
    expect(downloads.attempts, 3);
    await tester.pageBack();
    await tester.pumpAndSettle();
    expect(find.text('Podcasts on this device'), findsOneWidget);
  });

  testWidgets(
      'parent download notifyListeners does not leave Download disabled',
      (tester) async {
    final repository = _Repository();
    final downloads = _Downloads(repository);
    downloads.state = 'missing';
    await tester.pumpWidget(ProviderScope(overrides: [
      podcastRepositoryProvider.overrideWithValue(repository),
      podcastScopeProvider.overrideWith((ref) async => 'local'),
      podcastDownloadsProvider.overrideWith((ref) => downloads),
    ], child: const MaterialApp(home: StandalonePodcastsScreen())));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Podcast'));
    await tester.pumpAndSettle();
    final download = find.widgetWithText(TextButton, 'Download');
    expect(tester.widget<TextButton>(download).onPressed, isNotNull);
    // Spurious provider notifies must not restart copy() into a permanent
    // waiting FutureBuilder that greys out Download.
    downloads.notifyListeners();
    await tester.pump();
    downloads.notifyListeners();
    await tester.pump();
    expect(tester.widget<TextButton>(download).onPressed, isNotNull);
  });
}
