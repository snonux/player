import 'dart:io';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/podcast_library_provider.dart';
import 'package:player_android/providers/settings_provider.dart';
import 'package:player_android/screens/standalone_podcasts_screen.dart';
import 'package:player_android/services/podcast_repository.dart';
import 'package:player_android/services/podcast_sync_store.dart';
import 'package:player_android/services/podcast_downloads.dart';
import 'package:player_android/services/playback_session_coordinator.dart';

class _SignedOut extends AuthStateNotifier {
  @override
  Future<AuthState> build() async => const AuthState.unauthenticated();
}

class _DeviceSettings extends SettingsNotifier {
  @override
  Future<AppSettings> build() async => const AppSettings(
      serverBaseUrl: null, destination: LibraryDestination.local);
}

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();
  testWidgets(
      'native completion, week cleanup, history and device-copy removal',
      (tester) async {
    SharedPreferences.setMockInitialValues({});
    final directory =
        await Directory.systemTemp.createTemp('native-retention-');
    final repository = PodcastRepository(databasePath: '${directory.path}/db');
    final db = await repository.database;
    const feed = 'feed:example.com/rss', episode = 'guid:one';
    const id = '0123456789abcdef0123456789abcdef';
    await db.insert('podcast_subscriptions', {
      'scope': 'local',
      'feed_key': feed,
      'subscribed': 1,
      'subscription_ms': 1,
      'title': 'Retention show'
    });
    await db.insert('podcast_feeds', {
      'feed_key': feed,
      'title': 'Retention show',
      'fetch_url': 'https://example.com/rss'
    });
    await db.insert('podcast_episodes', {
      'feed_key': feed,
      'episode_key': episode,
      'title': 'Retention episode',
      'description': '',
      'enclosure_url': 'https://example.com/one.mp3',
      'enclosure_key': 'enc:example.com/one.mp3',
      'published_at': 1,
      'duration_seconds': 120
    });
    final playback = PlaybackSessionCoordinator();
    final downloads =
        PodcastDownloads(repository, playback, directory: directory);
    await downloads.ready;
    final file = File('${directory.path}/$id.audio');
    await file.writeAsBytes([1, 2, 3]);
    await db.insert('podcast_downloads', {
      'id': id,
      'feed_key': feed,
      'episode_key': episode,
      'state': 'available',
      'size_bytes': 3
    });
    final store = PodcastSyncStore(repository);
    await store.saveEpisode('local', feed, episode,
        seconds: 37, observedMs: 10);
    final container = ProviderContainer(overrides: [
      authStateProvider.overrideWith(_SignedOut.new),
      settingsProvider.overrideWith(_DeviceSettings.new),
      podcastRepositoryProvider.overrideWithValue(repository),
      podcastDownloadsProvider.overrideWith((ref) => downloads),
    ]);
    container.read(podcastDownloadsProvider);
    try {
      await container.read(settingsProvider.future);
      await container.read(authStateProvider.future);
      await tester.pumpWidget(UncontrolledProviderScope(
          container: container,
          child: const MaterialApp(
              home: LocalPodcastEpisodesScreen(
                  feedKey: feed, title: 'Retention show'))));
      await tester.pumpAndSettle();
      expect(find.text('Retention episode'), findsOneWidget);
      await tester.tap(find.byTooltip('More episode actions').first);
      await tester.pumpAndSettle();
      await tester.tap(find.text('Mark completed'));
      await tester.pumpAndSettle();
      expect(find.text('Retention episode'), findsNothing);
      await tester.tap(find.text('Completed'));
      await tester.pumpAndSettle();
      expect(find.text('Retention episode'), findsOneWidget);
      expect(await file.exists(), isTrue);
      // Cached rendering does not await optional maintenance. Join a pass to
      // settle native SQLite observation before advancing this fixture's age.
      await container
          .read(podcastRetentionProvider)
          .sweep('local', current: () => true);
      // Move only the retained observation backwards to exercise actual native
      // SQLite/filesystem expiry without changing completion/history clocks.
      await db.update(
          'podcast_episode_state',
          {
            'retention_seen_ms': DateTime.now()
                    .subtract(const Duration(days: 7))
                    .millisecondsSinceEpoch -
                1
          },
          where: 'scope=?',
          whereArgs: ['local']);
      container.invalidate(podcastEpisodePageProvider);
      final expired = await container
          .read(podcastEpisodePageProvider((feed, 'completed', 0)).future);
      expect(expired, isEmpty,
          reason: 'Native maintenance/page did not settle');
      await container
          .read(podcastRetentionProvider)
          .sweep('local', current: () => true);
      await tester.pumpAndSettle();
      expect(find.text('Retention episode'), findsNothing);
      expect(await file.exists(), isFalse);
      final row = (await db.query('podcast_episode_state',
              where: 'scope=?', whereArgs: ['local']))
          .single;
      expect(row['deleted'], 0);
      expect(row['deletion_pending_edit_id'], isNull);
      final pending = await store.payload('local');
      for (final edit in pending['episodes'] as List) {
        expect(edit['deletion'], isNull,
            reason: 'Device expiry must never send a timer deletion');
      }
      expect(row['completed'], 1);
      expect(row['position_seconds'], 37);
      expect(tester.takeException(), isNull);
    } finally {
      await tester.pumpWidget(const SizedBox());
      container.dispose();
      await repository.close();
      await directory.delete(recursive: true);
    }
  });
}
