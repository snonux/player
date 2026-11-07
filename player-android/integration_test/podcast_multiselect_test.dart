import 'dart:io';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/settings_provider.dart';
import 'package:player_android/providers/global_sync_provider.dart';
import 'package:player_android/providers/podcast_library_provider.dart';
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

// Only the failure boundary is injected. Successful writes use native SQLite
// through the production store, including edit clocks and pending payloads.
class _FailOnceStore extends PodcastSyncStore {
  _FailOnceStore(super.repository);
  bool fail = true;
  @override
  Future<void> saveEpisode(String scope, String feed, String episode,
      {double? seconds,
      bool? completed,
      bool? deleted,
      String source = 'manual',
      String enclosureUrl = '',
      int? observedMs}) async {
    if (fail && completed == true && episode == 'guid:1') {
      fail = false;
      throw StateError('Controlled write failure');
    }
    await super.saveEpisode(scope, feed, episode,
        seconds: seconds,
        completed: completed,
        deleted: deleted,
        source: source,
        enclosureUrl: enclosureUrl,
        observedMs: observedMs);
  }
}

Future<void> _action(WidgetTester tester, String name) async {
  await tester.tap(find.byTooltip('Selected episode actions'));
  await tester.pumpAndSettle();
  await tester.tap(find.text(name));
  if (name.startsWith('Delete')) {
    await tester.pump(const Duration(milliseconds: 400));
  } else {
    await tester.pumpAndSettle();
  }
}

Future<void> _selectAll(WidgetTester tester) async {
  await tester.longPress(find.text('Native batch 0'));
  await tester.pumpAndSettle();
  await tester.tap(find.byTooltip('Select all visible episodes'));
  await tester.pumpAndSettle();
}

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();
  for (final scope in ['local', 'account:https://offline.invalid#7']) {
    testWidgets('native batch SQLite/history/files/failure/retry $scope',
        (tester) async {
      SharedPreferences.setMockInitialValues({});
      final directory =
          await Directory.systemTemp.createTemp('native-multiselect-');
      final repository =
          PodcastRepository(databasePath: '${directory.path}/db');
      final db = await repository.database;
      const feed = 'feed:example.com/rss';
      await db.insert('podcast_subscriptions', {
        'scope': scope,
        'feed_key': feed,
        'subscribed': 1,
        'subscription_ms': 1,
        'title': 'Native batch'
      });
      await db.insert('podcast_feeds', {
        'feed_key': feed,
        'title': 'Native batch',
        'fetch_url': 'https://example.com/rss'
      });
      final downloads = PodcastDownloads(
          repository, PlaybackSessionCoordinator(),
          directory: directory);
      await downloads.ready;
      final files = <File>[];
      final store = _FailOnceStore(repository);
      for (var i = 0; i < 2; i++) {
        await db.insert('podcast_episodes', {
          'feed_key': feed,
          'episode_key': 'guid:$i',
          'title': 'Native batch $i',
          'description': '',
          'enclosure_url': 'https://example.com/$i.mp3',
          'enclosure_key': 'enc:example.com/$i.mp3',
          'published_at': 2 - i,
          'duration_seconds': 120
        });
        final id = i.toRadixString(16).padLeft(32, '0');
        final file = File('${directory.path}/$id.audio');
        await file.writeAsBytes([1, 2, 3]);
        files.add(file);
        await db.insert('podcast_downloads', {
          'id': id,
          'feed_key': feed,
          'episode_key': 'guid:$i',
          'state': 'available',
          'size_bytes': 3
        });
        await store.saveEpisode(scope, feed, 'guid:$i',
            seconds: 37 + i.toDouble());
      }
      final container = ProviderContainer(overrides: [
        authStateProvider.overrideWith(_SignedOut.new),
        settingsProvider.overrideWith(_DeviceSettings.new),
        podcastScopeProvider.overrideWith((ref) async => scope),
        podcastRepositoryProvider.overrideWithValue(repository),
        podcastSyncStoreProvider.overrideWithValue(store),
        podcastDownloadsProvider.overrideWith((ref) => downloads),
      ]);
      container.read(podcastDownloadsProvider);
      try {
        await container.read(settingsProvider.future);
        await container.read(authStateProvider.future);
        await tester.pumpWidget(UncontrolledProviderScope(
            container: container,
            child: const MaterialApp(
                home: LocalPodcastEpisodesScreen(feedKey: feed))));
        await tester.pumpAndSettle();
        await _selectAll(tester);
        await _action(tester, 'Mark completed');
        expect(find.text('1 selected'), findsOneWidget);
        var rows = await db.query('podcast_episode_state',
            where: 'scope=?', whereArgs: [scope], orderBy: 'episode_key');
        expect(rows.map((r) => r['completed']).toList(), [1, 0]);
        expect(rows.map((r) => r['position_seconds']).toList(), [37.0, 38.0]);
        await _action(tester, 'Mark completed');
        rows = await db.query('podcast_episode_state',
            where: 'scope=?', whereArgs: [scope]);
        expect(rows.every((r) => r['completed'] == 1), isTrue);
        await tester.tap(find.text('Completed'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('Grid'));
        await tester.pumpAndSettle();
        await _selectAll(tester);
        await _action(tester, 'Mark uncompleted');
        rows = await db.query('podcast_episode_state',
            where: 'scope=?', whereArgs: [scope]);
        expect(rows.every((r) => r['completed'] == 0), isTrue);
        await tester.tap(find.text('All'));
        await tester.pumpAndSettle();
        await _selectAll(tester);
        await _action(tester, 'Delete downloaded files');
        await tester.tap(find.text('Delete'));
        await tester.pumpAndSettle();
        expect(await files[0].exists(), isFalse);
        expect(await files[1].exists(), isFalse);
        expect(
            (await db.query('podcast_downloads'))
                .every((r) => r['state'] == 'missing'),
            isTrue);
        expect(
            (await db.query('podcast_episode_state'))
                .every((r) => r['deleted'] == 0),
            isTrue);
        await _selectAll(tester);
        await _action(tester, 'Delete episodes');
        await tester.tap(find.text('Cancel'));
        await tester.pumpAndSettle();
        expect(
            (await db.query('podcast_episode_state'))
                .every((r) => r['deleted'] == 0),
            isTrue);
        await _action(tester, 'Delete episodes');
        await tester.tap(find.text('Delete'));
        await tester.pumpAndSettle();
        rows = await db.query('podcast_episode_state',
            where: 'scope=?', whereArgs: [scope], orderBy: 'episode_key');
        expect(rows.every((r) => r['deleted'] == 1), isTrue);
        expect(rows.map((r) => r['position_seconds']).toList(), [37.0, 38.0]);
        final payload = await store.payload(scope);
        expect(
            (payload['episodes'] as List)
                .every((e) => e['deletion']['deleted'] == true),
            isTrue);
        expect(
            await container
                .read(podcastEpisodePageProvider((feed, 'all', 0)).future),
            isEmpty);
        await tester.pumpAndSettle();
        expect(find.text('Native batch 0'), findsNothing);
        expect(tester.takeException(), isNull);
      } finally {
        await tester.pumpWidget(const SizedBox());
        container.dispose();
        await repository.close();
        await directory.delete(recursive: true);
      }
    });
  }
}
