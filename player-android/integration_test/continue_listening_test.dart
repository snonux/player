import 'dart:io';
import 'dart:typed_data';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:just_audio/just_audio.dart';
import 'package:player_android/models/user.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/audio_handler_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/continue_listening_provider.dart';
import 'package:player_android/providers/first_run_provider.dart';
import 'package:player_android/providers/podcast_library_provider.dart';
import 'package:player_android/providers/settings_provider.dart';
import 'package:player_android/router.dart';
import 'package:player_android/services/audio_handler.dart';
import 'package:player_android/services/podcast_downloads.dart';
import 'package:player_android/services/podcast_repository.dart';
import 'package:player_android/services/podcast_sync_store.dart';
import 'package:player_android/services/playback_session_coordinator.dart';

class _Auth extends AuthStateNotifier {
  @override
  Future<AuthState> build() async => const AuthState.unauthenticated();
  void unlock() => state = const AsyncData(AuthState.authenticated(
      user: User(id: 7, username: 'listener', isAdmin: false),
      isOffline: true));
}

class _Settings extends SettingsNotifier {
  @override
  Future<AppSettings> build() async => const AppSettings(
      serverBaseUrl: 'https://unavailable.invalid',
      savedServers: ['https://unavailable.invalid'],
      selectionRequired: true);
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

Future<void> _resumeWithinThreeSeconds(
    WidgetTester tester, AudioPlayer player, String title, int seconds) async {
  final elapsed = Stopwatch()..start();
  await tester.tap(find.text(title));
  while ((player.processingState != ProcessingState.ready ||
          player.position.inMilliseconds < seconds * 1000 ||
          player.position.inMilliseconds >= (seconds + 3) * 1000) &&
      elapsed.elapsed < const Duration(seconds: 3)) {
    await tester.pump(const Duration(milliseconds: 50));
  }
  expect(elapsed.elapsed, lessThan(const Duration(seconds: 3)),
      reason:
          'The saved position must be reached by seeking, not by playing from zero.');
  expect(player.position.inMilliseconds, greaterThanOrEqualTo(seconds * 1000));
  expect(player.position.inMilliseconds, lessThan((seconds + 3) * 1000));
  debugPrint(
      'CONTINUE_RESUME saved=$seconds actual_ms=${player.position.inMilliseconds} elapsed_ms=${elapsed.elapsedMilliseconds}');
}

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();
  testWidgets(
      'cold offline resume shortcuts protect locked account history and seek native audio',
      (tester) async {
    final folder = await Directory.systemTemp.createTemp('continue-listening-');
    final publisher = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final origin = 'http://127.0.0.1:${publisher.port}';
    publisher.listen((request) async {
      if (request.uri.path == '/feed' || request.uri.path == '/device-feed') {
        final title = request.uri.path == '/feed'
            ? 'Private saved episode'
            : 'Device saved episode';
        request.response.write(
            '<rss><channel><title>Private podcast</title><item><guid>one</guid><title>$title</title><enclosure url="$origin/audio.wav"/><duration>60</duration></item></channel></rss>');
      } else {
        request.response.headers.contentType = ContentType('audio', 'wav');
        request.response.add(_audio());
      }
      await request.response.close();
    });
    const scope = 'account:https://unavailable.invalid#7';
    var repository =
        PodcastRepository(databasePath: '${folder.path}/podcasts.db');
    var downloads = PodcastDownloads(repository, PlaybackSessionCoordinator(),
        directory: Directory('${folder.path}/audio'));
    final feed = await repository.subscribe(scope, '$origin/feed');
    final episode = (await repository.episodes(feed)).single;
    await downloads.download(feed, episode['episode_key'] as String);
    await PodcastSyncStore(repository).saveEpisode(
        scope, feed, episode['episode_key'] as String,
        seconds: 12);
    final deviceFeed =
        await repository.subscribe('local', '$origin/device-feed');
    final deviceEpisode = (await repository.episodes(deviceFeed)).single;
    await downloads.download(
        deviceFeed, deviceEpisode['episode_key'] as String);
    await PodcastSyncStore(repository).saveEpisode(
        'local', deviceFeed, deviceEpisode['episode_key'] as String,
        seconds: 25);
    downloads.dispose();
    await repository.close();
    await publisher.close(force: true);
    repository = PodcastRepository(databasePath: '${folder.path}/podcasts.db');
    downloads = PodcastDownloads(repository, PlaybackSessionCoordinator(),
        directory: Directory('${folder.path}/audio'));
    final player = AudioPlayer();
    final handler = PlayerAudioHandler(player);
    final container = ProviderContainer(overrides: [
      authStateProvider.overrideWith(_Auth.new),
      settingsProvider.overrideWith(_Settings.new),
      firstRunProvider.overrideWith((ref) async => false),
      audioHandlerProvider.overrideWithValue(handler),
      podcastRepositoryProvider.overrideWithValue(repository),
      podcastDownloadsProvider.overrideWith((ref) => downloads),
      apiClientProvider.overrideWith(
          (ref) => throw StateError('Resume attempted server network')),
    ]);
    try {
      await container.read(settingsProvider.future);
      final router = container.read(routerProvider);
      router.go(AppRoutes.server);
      await tester.pumpWidget(UncontrolledProviderScope(
          container: container,
          child: MaterialApp.router(routerConfig: router)));
      await container.read(continueListeningProvider(true).future);
      await tester.pumpAndSettle();
      expect(find.text('Choose a server'), findsOneWidget);
      expect(find.widgetWithText(ListTile, 'https://unavailable.invalid'),
          findsOneWidget);
      expect(find.text('Private saved episode'), findsNothing);
      expect(find.text('Device saved episode'), findsOneWidget);
      expect(container.exists(authStateProvider), isFalse);
      await tester.ensureVisible(find.text('Device saved episode'));
      await _resumeWithinThreeSeconds(
          tester, player, 'Device saved episode', 25);
      await player.pause();
      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      expect(find.text('Choose a server'), findsOneWidget);
      expect(container.exists(authStateProvider), isFalse);
      await container.read(authStateProvider.future);
      (container.read(authStateProvider.notifier) as _Auth).unlock();
      router.go(AppRoutes.offlineDownloads);
      await tester.pumpAndSettle();
      expect(find.text('Continue listening'), findsOneWidget);
      expect(find.text('Private saved episode'), findsOneWidget);
      expect(find.textContaining('Resume at 0:12'), findsOneWidget);
      await _resumeWithinThreeSeconds(
          tester, player, 'Private saved episode', 12);
      await player.pause();
      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      expect(find.text('Offline downloads'), findsOneWidget);
      await PodcastSyncStore(repository).saveEpisode(
          scope, feed, episode['episode_key'] as String,
          completed: true);
      container.invalidate(continueListeningProvider(false));
      expect(await container.read(continueListeningProvider(false).future),
          isEmpty);
      await tester.pumpAndSettle();
      expect(find.text('Private saved episode'), findsNothing);
      expect(tester.takeException(), isNull);
    } finally {
      await tester.pumpWidget(const SizedBox());
      await handler.stop();
      container.dispose();
      await player.dispose();
      await repository.close();
      await folder.delete(recursive: true);
    }
  });
}
