import 'dart:async';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:just_audio/just_audio.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/audio_handler_provider.dart';
import 'package:player_android/services/audio_handler.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/first_run_provider.dart';
import 'package:player_android/providers/offline_downloads_provider.dart';
import 'package:player_android/providers/local_library_provider.dart';
import 'package:player_android/providers/podcast_library_provider.dart';
import 'package:player_android/providers/settings_provider.dart';
import 'package:player_android/models/user.dart';
import 'package:player_android/router.dart';
import 'package:player_android/services/podcast_downloads.dart';
import 'package:player_android/services/podcast_repository.dart';
import 'package:player_android/services/podcast_sync_store.dart';
import 'package:player_android/services/playback_session_coordinator.dart';

const _origin = 'https://offline-podcast.invalid';

class _Account extends AuthStateNotifier {
  @override
  Future<AuthState> build() async => const AuthState.authenticated(
      user: User(id: 7, username: 'listener', isAdmin: false), isOffline: true);
}

class _Settings extends SettingsNotifier {
  @override
  Future<AppSettings> build() async =>
      const AppSettings(serverBaseUrl: _origin);
}

class _SignedOut extends AuthStateNotifier {
  @override
  Future<AuthState> build() async => const AuthState.unauthenticated();
}

class _DeviceSettings extends SettingsNotifier {
  @override
  Future<AppSettings> build() async => const AppSettings(
      serverBaseUrl: _origin, destination: LibraryDestination.local);
}

Uint8List _audio() {
  final wav = ByteData(44 + 8000 * 60 * 2);
  void text(int at, String value) {
    for (var i = 0; i < value.length; i++) {
      wav.setUint8(at + i, value.codeUnitAt(i));
    }
  }

  text(0, 'RIFF');
  wav.setUint32(4, wav.lengthInBytes - 8, Endian.little);
  text(8, 'WAVEfmt ');
  wav.setUint32(16, 16, Endian.little);
  wav.setUint16(20, 1, Endian.little);
  wav.setUint16(22, 1, Endian.little);
  wav.setUint32(24, 8000, Endian.little);
  wav.setUint32(28, 16000, Endian.little);
  wav.setUint16(32, 2, Endian.little);
  wav.setUint16(34, 16, Endian.little);
  text(36, 'data');
  wav.setUint32(40, wav.lengthInBytes - 44, Endian.little);
  for (var i = 44; i < wav.lengthInBytes; i += 2) {
    wav.setInt16(i, (i % 40 < 20) ? 4000 : -4000, Endian.little);
  }
  return wav.buffer.asUint8List();
}

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();
  testWidgets(
      'offline account reopens cached podcasts and resumes native audio after restart',
      (tester) async {
    SharedPreferences.setMockInitialValues({});
    final folder = await Directory.systemTemp.createTemp('offline-podcast-');
    final publisher = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final publisherOrigin = 'http://127.0.0.1:${publisher.port}';
    publisher.listen((request) async {
      if (request.uri.path == '/feed') {
        request.response.write(
            '<rss><channel><title>Saved podcast</title><item><title>Saved episode</title><guid>one</guid><description>Cached episode show notes.</description><enclosure url="$publisherOrigin/audio.wav"/><duration>60</duration></item></channel></rss>');
      } else {
        request.response.headers.contentType = ContentType('audio', 'wav');
        request.response.add(_audio());
      }
      await request.response.close();
    });
    final scope = podcastAccountScope(_origin, 7);
    final otherScope = podcastAccountScope(_origin, 8);
    PodcastRepository repository =
        PodcastRepository(databasePath: '${folder.path}/podcasts.db');
    PodcastDownloads downloads = PodcastDownloads(
        repository, PlaybackSessionCoordinator(),
        directory: Directory('${folder.path}/audio'));
    final feed = await repository.subscribe(scope, '$publisherOrigin/feed');
    final episode = (await repository.episodes(feed)).single;
    final key = episode['episode_key'] as String;
    await downloads.download(feed, key);
    await PodcastSyncStore(repository)
        .saveEpisode(scope, feed, key, seconds: 12);
    await PodcastSyncStore(repository)
        .saveEpisode(otherScope, feed, key, seconds: 3);
    downloads.dispose();
    await repository.close();
    await publisher.close(force: true);

    // Recreate disk-backed services, with neither publisher nor Player available.
    repository = PodcastRepository(databasePath: '${folder.path}/podcasts.db');
    downloads = PodcastDownloads(repository, PlaybackSessionCoordinator(),
        directory: Directory('${folder.path}/audio'));
    final container = ProviderContainer(overrides: [
      authStateProvider.overrideWith(_Account.new),
      settingsProvider.overrideWith(_Settings.new),
      firstRunProvider.overrideWith((ref) async => false),
      offlineMediaListProvider.overrideWith((ref) async => []),
      podcastRepositoryProvider.overrideWithValue(repository),
      podcastDownloadsProvider.overrideWith((ref) => downloads),
      apiClientProvider.overrideWith(
          (ref) => throw StateError('Offline podcast requested server API')),
    ]);
    final player = AudioPlayer();
    try {
      await container.read(settingsProvider.future);
      await container.read(authStateProvider.future);
      final router = container.read(routerProvider);
      await tester.pumpWidget(UncontrolledProviderScope(
          container: container,
          child: MaterialApp.router(routerConfig: router)));
      await tester.pumpAndSettle();
      expect(find.text('Offline downloads'), findsOneWidget);
      await tester.tap(find.byTooltip('Offline account'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Podcasts'));
      await tester.pumpAndSettle();
      expect(find.text('Saved episode'), findsOneWidget);
      expect(find.text('Resume'), findsOneWidget);
      expect(await container.read(podcastScopeProvider.future), scope);
      final request = await downloads.playbackRequest(scope, feed, episode);
      await player.setFilePath(File.fromUri(request.sourceUri).path);
      final saved = await request.readPosition!();
      expect(saved, 12);
      await player.seek(Duration(milliseconds: (saved! * 1000).round()));
      unawaited(player.play());
      final deadline = DateTime.now().add(const Duration(seconds: 10));
      while (player.position.inMilliseconds < 13000 &&
          DateTime.now().isBefore(deadline)) {
        await tester.pump(const Duration(milliseconds: 100));
      }
      await player.pause();
      expect(player.position.inMilliseconds, greaterThanOrEqualTo(13000));
      await request.savePosition!(player.position.inMilliseconds / 1000);
      final resumed = await downloads.playbackRequest(scope, feed, episode);
      expect(await resumed.readPosition!(), greaterThanOrEqualTo(13));
      final other = await downloads.playbackRequest(otherScope, feed, episode);
      expect(await other.readPosition!(), 3);
      await tester.pageBack();
      await tester.pumpAndSettle();
      expect(find.text('Offline downloads'), findsOneWidget);
      expect(tester.takeException(), isNull);

      // Reopen through "Use files on this device", with no unlocked account.
      final handler = PlayerAudioHandler(player);
      final deviceDownloads = PodcastDownloads(
          repository, PlaybackSessionCoordinator(),
          directory: Directory('${folder.path}/audio'));
      final device = ProviderContainer(overrides: [
        audioHandlerProvider.overrideWithValue(handler),
        authStateProvider.overrideWith(_SignedOut.new),
        settingsProvider.overrideWith(_DeviceSettings.new),
        localMediaListProvider.overrideWith((ref) async => []),
        podcastRepositoryProvider.overrideWithValue(repository),
        podcastDownloadsProvider.overrideWith((ref) => deviceDownloads),
        apiClientProvider.overrideWith(
            (ref) => throw StateError('Signed-out podcasts used server API')),
      ]);
      try {
        await device.read(settingsProvider.future);
        await device.read(authStateProvider.future);
        await tester.pumpWidget(const SizedBox());
        await tester.pumpWidget(UncontrolledProviderScope(
            container: device,
            child:
                MaterialApp.router(routerConfig: device.read(routerProvider))));
        await tester.pumpAndSettle();
        await tester.tap(find.byTooltip('More options'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('Podcasts on this device'));
        await tester.pumpAndSettle();
        expect(find.text('Saved episode'), findsOneWidget);
        expect(find.text('Resume'), findsOneWidget);
        expect(await device.read(podcastScopeProvider.future), scope);
        expect(device.read(authStateProvider).requireValue.isUnauthenticated,
            isTrue);
        await tester.tap(find.byTooltip('Choose podcast library'));
        await tester.pumpAndSettle();
        await tester.tap(find.widgetWithText(
            CheckedPopupMenuItem<String>, 'Device-only podcasts'));
        await tester.pumpAndSettle();
        expect(find.text('Saved episode'), findsNothing);
        expect(await device.read(podcastScopeProvider.future), 'local');
        await tester.tap(find.byTooltip('Choose podcast library'));
        await tester.pumpAndSettle();
        await tester.tap(find.widgetWithText(CheckedPopupMenuItem<String>,
            'listener · offline-podcast.invalid'));
        await tester.pumpAndSettle();
        expect(find.text('Resume'), findsOneWidget);
        final localRequest = await deviceDownloads.playbackRequest(
            await device.read(podcastScopeProvider.future), feed, episode);
        final position = await localRequest.readPosition!();
        expect(position, greaterThanOrEqualTo(13));
        await tester.tap(find.text('Resume'));
        await tester.pumpAndSettle();
        expect(find.text('Show notes'), findsOneWidget);
        expect(find.text('Cached episode show notes.'), findsOneWidget);
        final resumedDeadline = DateTime.now().add(const Duration(seconds: 10));
        while (
            player.position.inMilliseconds < (position! * 1000).round() + 500 &&
                DateTime.now().isBefore(resumedDeadline)) {
          await tester.pump(const Duration(milliseconds: 100));
        }
        await player.pause();
        expect(player.position.inMilliseconds,
            greaterThan((position * 1000).round()));
        expect(tester.takeException(), isNull);
      } finally {
        await tester.pumpWidget(const SizedBox());
        await handler.stop();
        device.dispose();
      }
    } finally {
      await player.dispose();
      await tester.pumpWidget(const SizedBox());
      container.dispose();
      await repository.close();
      await folder.delete(recursive: true);
    }
  });
}
