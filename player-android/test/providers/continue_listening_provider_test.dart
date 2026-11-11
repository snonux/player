import 'dart:io';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/continue_listening_provider.dart';
import 'package:player_android/providers/local_library_provider.dart';
import 'package:player_android/providers/offline_downloads_provider.dart';
import 'package:player_android/providers/podcast_library_provider.dart';
import 'package:player_android/providers/settings_provider.dart';
import 'package:player_android/services/local_library_repository.dart';
import 'package:player_android/services/podcast_downloads.dart';
import 'package:player_android/services/podcast_feed_fetcher.dart';
import 'package:player_android/services/podcast_repository.dart';
import 'package:player_android/services/podcast_sync_store.dart';
import 'package:player_android/services/playback_session_coordinator.dart';
import 'package:player_android/widgets/continue_listening.dart';

class _Locked extends AuthStateNotifier {
  @override
  Future<AuthState> build() async => const AuthState.unauthenticated();
}

class _Settings extends SettingsNotifier {
  @override
  Future<AppSettings> build() async =>
      const AppSettings(serverBaseUrl: 'https://private.invalid');
}

class _DeviceMode extends SettingsNotifier {
  @override
  Future<AppSettings> build() async => const AppSettings(
      serverBaseUrl: 'https://private.invalid',
      destination: LibraryDestination.local);
}

class _Feed extends PodcastFeedFetcher {
  @override
  Future<PublisherFeed> fetch(String url,
          {String etag = '', String lastModified = ''}) async =>
      parsePodcastFeed(
          '<rss><channel><title>Podcast</title><item><guid>one</guid><title>Episode</title><enclosure url="https://publisher.invalid/one.wav"/></item></channel></rss>',
          fetchUrl: url);
}

class _Downloads extends PodcastDownloads {
  _Downloads(super.repository, super.playback);
  @override
  Future<Map<String, Object?>?> copy(String feed, String episode) async =>
      {'state': 'available'};
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  sqfliteFfiInit();
  databaseFactory = databaseFactoryFfi;
  test('locked shortcuts expose device progress only and omit finished media',
      () async {
    final folder = await Directory.systemTemp.createTemp('continue-provider-');
    addTearDown(() => folder.delete(recursive: true));
    final local = LocalLibraryDatabase(databasePath: '${folder.path}/local.db');
    final media = SqliteLocalMediaRepository(local);
    final progress = SqliteLocalProgressRepository(local);
    for (final title in ['Audiobook', 'Finished book', 'Unstarted book']) {
      final item = await media.importDocument(
          uri: 'content://test/$title', title: title, mimeType: 'audio/mpeg');
      if (title != 'Unstarted book') await progress.savePosition(item.id, 42);
      if (title == 'Finished book') await progress.markFinished(item.id);
    }
    final podcasts = PodcastRepository(
        databasePath: '${folder.path}/podcasts.db', fetcher: _Feed());
    final feed =
        await podcasts.subscribe('local', 'https://publisher.invalid/feed');
    final episode = (await podcasts.episodes(feed)).single;
    final key = episode['episode_key'] as String;
    final store = PodcastSyncStore(podcasts);
    await store.saveEpisode('local', feed, key, seconds: 12);
    await podcasts.subscribe(
        'account:https://private.invalid#7', 'https://publisher.invalid/feed');
    await store.saveEpisode('account:https://private.invalid#7', feed, key,
        seconds: 99);
    final downloads = _Downloads(podcasts, PlaybackSessionCoordinator());
    final container = ProviderContainer(overrides: [
      authStateProvider.overrideWith(_Locked.new),
      settingsProvider.overrideWith(_Settings.new),
      localMediaRepositoryProvider.overrideWithValue(media),
      localProgressRepositoryProvider.overrideWithValue(progress),
      podcastRepositoryProvider.overrideWithValue(podcasts),
      podcastDownloadsProvider.overrideWith((ref) => downloads),
      apiClientProvider
          .overrideWith((ref) => throw StateError('Network forbidden')),
      offlineDownloadRepositoryProvider
          .overrideWith((ref) => throw StateError('Locked account read')),
    ]);
    try {
      await container.read(authStateProvider.future);
      await container.read(settingsProvider.future);
      final rows =
          await container.read(continueListeningProvider(false).future);
      expect(rows.map((e) => e.title), ['Audiobook', 'Episode']);
      expect(rows.map((e) => e.position), [42, 12]);
      expect(rows.last.scope, 'local');
      await store.saveEpisode('local', feed, key, completed: true);
      container.invalidate(continueListeningProvider(false));
      expect(
          (await container.read(continueListeningProvider(false).future))
              .map((e) => e.title),
          ['Audiobook']);
    } finally {
      container.dispose();
      await local.close();
      await podcasts.close();
    }
  });
  test('device shortcuts do not initialize auth or its server dependencies',
      () async {
    final folder = await Directory.systemTemp.createTemp('continue-device-');
    final local = LocalLibraryDatabase(databasePath: '${folder.path}/local.db');
    final podcasts =
        PodcastRepository(databasePath: '${folder.path}/podcasts.db');
    final downloads = _Downloads(podcasts, PlaybackSessionCoordinator());
    final container = ProviderContainer(overrides: [
      settingsProvider.overrideWith(_Settings.new),
      authStateProvider.overrideWith(
          () => throw StateError('Device shortcuts initialized auth')),
      apiClientProvider.overrideWith(
          (ref) => throw StateError('Device shortcuts initialized API')),
      localMediaRepositoryProvider
          .overrideWithValue(SqliteLocalMediaRepository(local)),
      localProgressRepositoryProvider
          .overrideWithValue(SqliteLocalProgressRepository(local)),
      podcastRepositoryProvider.overrideWithValue(podcasts),
      podcastDownloadsProvider.overrideWith((ref) => downloads),
    ]);
    final deviceSubscription =
        container.listen(continueListeningProvider(true), (_, __) {});
    final defaultSubscription =
        container.listen(continueListeningProvider(false), (_, __) {});
    try {
      expect(await container.read(continueListeningProvider(true).future),
          isEmpty);
      expect(container.exists(authStateProvider), isFalse);
      expect(container.exists(apiClientProvider), isFalse);
      // The generic panel is also safe before a server identity exists.
      expect(await container.read(continueListeningProvider(false).future),
          isEmpty);
      expect(container.exists(authStateProvider), isFalse);
    } finally {
      deviceSubscription.close();
      defaultSubscription.close();
      container.dispose();
      await local.close();
      await podcasts.close();
      await folder.delete(recursive: true);
    }
  });
  test(
      'started podcast shortcuts order by listening time and exclude deleted entries',
      () async {
    final repository =
        PodcastRepository(databasePath: inMemoryDatabasePath, fetcher: _Feed());
    try {
      final feed =
          await repository.subscribe('local', 'https://publisher.invalid/feed');
      final episode = (await repository.episodes(feed)).single;
      final db = await repository.database;
      await db.insert('podcast_episodes',
          {...episode, 'episode_key': 'guid:two', 'title': 'Second episode'});
      await db.insert('podcast_episode_state', {
        'scope': 'local',
        'feed_key': feed,
        'episode_key': episode['episode_key'],
        'position_seconds': 90,
        'position_ms': 10
      });
      await db.insert('podcast_episode_state', {
        'scope': 'local',
        'feed_key': feed,
        'episode_key': 'guid:two',
        'position_seconds': 2,
        'position_ms': 20
      });
      expect(
          (await repository.browseEpisodes('local', filter: 'started'))
              .map((e) => e['title']),
          ['Second episode', 'Episode']);
      await db.update('podcast_episode_state', {'deleted': 1},
          where: 'episode_key=?', whereArgs: ['guid:two']);
      expect(
          (await repository.browseEpisodes('local', filter: 'started'))
              .map((e) => e['title']),
          ['Episode']);
    } finally {
      await repository.close();
    }
  });
  testWidgets('failed resume lookup has retry and keeps long titles readable',
      (tester) async {
    var fail = true;
    await tester.binding.setSurfaceSize(const Size(320, 480));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    await tester.pumpWidget(ProviderScope(
        overrides: [
          continueListeningProvider(false).overrideWith((ref) async {
            if (fail) throw const FileSystemException('unavailable');
            return [
              const ContinueListeningItem(
                  title: 'A long audiobook title with many words and chapters',
                  source: 'On this device',
                  position: 42,
                  path: '/local/player/1')
            ];
          }),
        ],
        child: MaterialApp(
            builder: (context, child) => MediaQuery(
                data: MediaQuery.of(context)
                    .copyWith(textScaler: const TextScaler.linear(2)),
                child: child!),
            home: const Scaffold(
                body: SingleChildScrollView(child: ContinueListening())))));
    await tester.pumpAndSettle();
    expect(find.text('Retry continue listening'), findsOneWidget);
    fail = false;
    await tester.tap(find.text('Retry continue listening'));
    await tester.pumpAndSettle();
    expect(find.text('Continue listening'), findsOneWidget);
    expect(find.text('On this device · Resume at 0:42'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.binding.setSurfaceSize(const Size(640, 320));
    await tester.pump();
    expect(tester.takeException(), isNull);
  });
  test(
      'device mode resumes the account podcast progress the signed-in app uses',
      () async {
    const account = 'account:https://private.invalid#7';
    SharedPreferences.setMockInitialValues(
        {'podcast_device_scope': account});
    final folder = await Directory.systemTemp.createTemp('continue-offline-');
    addTearDown(() => folder.delete(recursive: true));
    final local = LocalLibraryDatabase(databasePath: '${folder.path}/local.db');
    final podcasts = PodcastRepository(
        databasePath: '${folder.path}/podcasts.db', fetcher: _Feed());
    final feed =
        await podcasts.subscribe('local', 'https://publisher.invalid/feed');
    final key =
        (await podcasts.episodes(feed)).single['episode_key'] as String;
    final store = PodcastSyncStore(podcasts);
    // An old device-only library still holds an earlier position.
    await store.saveEpisode('local', feed, key, seconds: 12);
    await podcasts.subscribe(account, 'https://publisher.invalid/feed');
    await store.saveEpisode(account, feed, key, seconds: 99);
    final downloads = _Downloads(podcasts, PlaybackSessionCoordinator());
    final container = ProviderContainer(overrides: [
      settingsProvider.overrideWith(_DeviceMode.new),
      authStateProvider.overrideWith(
          () => throw StateError('Device shortcuts initialized auth')),
      apiClientProvider
          .overrideWith((ref) => throw StateError('Network forbidden')),
      localMediaRepositoryProvider
          .overrideWithValue(SqliteLocalMediaRepository(local)),
      localProgressRepositoryProvider
          .overrideWithValue(SqliteLocalProgressRepository(local)),
      podcastRepositoryProvider.overrideWithValue(podcasts),
      podcastDownloadsProvider.overrideWith((ref) => downloads),
    ]);
    try {
      await container.read(settingsProvider.future);
      for (final deviceOnly in [true, false]) {
        final rows =
            await container.read(continueListeningProvider(deviceOnly).future);
        expect(rows.single.scope, account);
        expect(rows.single.position, 99);
      }
      expect(container.exists(authStateProvider), isFalse);
      // Playing it offline saves to that library, which the app resumes
      // from once it is back online.
      final row =
          (await container.read(continueListeningProvider(true).future)).single;
      await store.saveEpisode(
          row.scope!, row.episode!['feed_key'] as String, key,
          seconds: 140, source: 'auto');
      container.invalidate(continueListeningProvider(true));
      expect(
          (await container.read(continueListeningProvider(true).future))
              .single
              .position,
          140);
    } finally {
      container.dispose();
      await local.close();
      await podcasts.close();
    }
  });
}
