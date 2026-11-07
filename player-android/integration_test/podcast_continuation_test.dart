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
import 'package:player_android/providers/local_library_provider.dart';
import 'package:player_android/providers/podcast_library_provider.dart';
import 'package:player_android/providers/settings_provider.dart';
import 'package:player_android/screens/audio_player_screen.dart';
import 'package:player_android/services/podcast_downloads.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/services/podcast_repository.dart';
import 'package:player_android/services/playback_session_coordinator.dart';

class _SignedOut extends AuthStateNotifier {
  @override
  Future<AuthState> build() async => const AuthState.unauthenticated();
}

class _DeviceSettings extends SettingsNotifier {
  @override
  Future<AppSettings> build() async => const AppSettings(
      serverBaseUrl: 'https://offline-podcast.invalid',
      destination: LibraryDestination.local);
}

Uint8List _audio() {
  final wav = ByteData(44 + 8000 * 3 * 2);
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
      'native podcast completion continues locally then online and stops on failure',
      (tester) async {
    SharedPreferences.setMockInitialValues({});
    final folder = await Directory.systemTemp.createTemp('podcast-continue-');
    final publisher = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final origin = 'http://127.0.0.1:${publisher.port}';
    final traffic = <String>[];
    final credentialHeaders = <String>[];
    publisher.listen((request) async {
      for (final name in ['authorization', 'cookie']) {
        if (request.headers.value(name) != null) credentialHeaders.add(name);
      }
      traffic.add(request.uri.path);
      if (request.uri.path == '/feed') {
        request.response.write('<rss><channel><title>Continue podcast</title>'
            '<item><title>First episode</title><guid>one</guid><pubDate>Wed, 04 Nov 2026 00:00:00 GMT</pubDate><enclosure url="$origin/one.wav"/><duration>3</duration></item>'
            '<item><title>Second episode</title><guid>two</guid><pubDate>Tue, 03 Nov 2026 00:00:00 GMT</pubDate><enclosure url="$origin/two.wav"/><duration>3</duration></item>'
            '<item><title>Broken episode</title><guid>three</guid><pubDate>Mon, 02 Nov 2026 00:00:00 GMT</pubDate><enclosure url="$origin/broken.wav"/><duration>3</duration></item>'
            '</channel></rss>');
      } else if (request.uri.path == '/broken.wav') {
        request.response.statusCode = 404;
      } else {
        request.response.headers.contentType = ContentType('audio', 'wav');
        request.response.add(_audio());
      }
      await request.response.close();
    });
    final repository =
        PodcastRepository(databasePath: '${folder.path}/podcasts.db');
    final downloads = PodcastDownloads(repository, PlaybackSessionCoordinator(),
        directory: Directory('${folder.path}/audio'));
    final feed = await repository.subscribe('local', '$origin/feed');
    final episodes =
        await repository.browseEpisodes('local', feed: feed, filter: 'all');
    await downloads.download(feed, episodes.first['episode_key'] as String);
    final base = await downloads.playbackRequest('local', feed, episodes.first);
    final expansionGate = Completer<void>();
    var expansions = 0;
    final request = PodcastPlaybackRequest(
      identity: base.identity,
      title: base.title,
      sourceUri: base.sourceUri,
      sourceName: base.sourceName,
      podcast: base.podcast,
      readPosition: base.readPosition,
      readPositionForDuration: base.readPositionForDuration,
      savePosition: base.savePosition,
      markFinished: base.markFinished,
      finishAtPosition: base.finishAtPosition,
      synchronize: base.synchronize,
      continuation: (offline) async {
        if (++expansions == 1) await expansionGate.future;
        return base.continuation!(offline);
      },
    );
    final player = AudioPlayer();
    final handler = PlayerAudioHandler(player);
    final container = ProviderContainer(overrides: [
      audioHandlerProvider.overrideWithValue(handler),
      authStateProvider.overrideWith(_SignedOut.new),
      settingsProvider.overrideWith(_DeviceSettings.new),
      localMediaListProvider.overrideWith((ref) async => []),
      podcastRepositoryProvider.overrideWithValue(repository),
      podcastDownloadsProvider.overrideWith((ref) => downloads),
      apiClientProvider.overrideWith((ref) =>
          throw StateError('Device podcasts used server credentials/API')),
    ]);
    Future<void> until(bool Function() condition) async {
      final end = DateTime.now().add(const Duration(seconds: 20));
      while (!condition() && DateTime.now().isBefore(end)) {
        await tester.pump(const Duration(milliseconds: 100));
      }
      expect(condition(), isTrue,
          reason:
              'player=${player.playerState} position=${player.position} sequence=${handler.musicState?.request.title} error=${handler.musicState?.error} traffic=$traffic');
    }

    try {
      await container.read(settingsProvider.future);
      await container.read(authStateProvider.future);
      await tester.pumpWidget(UncontrolledProviderScope(
          container: container,
          child: MaterialApp(
              home: AudioPlayerScreen(
                  mediaId: request.identity, request: request))));
      await until(() =>
          player.playing &&
          player.position > const Duration(milliseconds: 200));
      expect(find.byTooltip('Skip forward 15 seconds'), findsOneWidget);
      expect(find.byTooltip('Skip back 15 seconds'), findsOneWidget);
      await handler.pause();
      await handler.seek(player.duration!);
      await tester.pump(const Duration(milliseconds: 500));
      expect(traffic.where((p) => p == '/two.wav'), isEmpty,
          reason: 'paused seek to end must not continue');
      expect(
          (await repository.browseEpisodes('local',
              feed: feed, filter: 'completed')),
          isEmpty);
      await handler.seek(Duration.zero);
      unawaited(handler.play());
      await until(() => expansions == 1);
      await handler.seek(Duration.zero);
      expansionGate.complete();
      await tester.pump(const Duration(milliseconds: 250));
      expect(handler.musicState?.request.title, 'First episode');
      expect(traffic.where((path) => path == '/two.wav'), isEmpty,
          reason:
              'seek back during pending expansion retains the current episode');
      await until(() =>
          handler.musicState?.request.title == 'Second episode' &&
          player.playing);
      expect(traffic, contains('/two.wav'));
      expect(
          (await repository.browseEpisodes('local',
                  feed: feed, filter: 'completed'))
              .map((e) => e['title']),
          contains('First episode'));
      await until(() => handler.musicState?.error != null);
      expect(traffic, contains('/broken.wav'));
      expect(credentialHeaders, isEmpty,
          reason: 'publisher receives no account credentials');
      expect(player.playing, isFalse);
      expect(
          (await repository.browseEpisodes('local',
                  feed: feed, filter: 'completed'))
              .map((e) => e['title']),
          containsAll(['First episode', 'Second episode']));
      expect(
          (await repository.browseEpisodes('local',
                  feed: feed, filter: 'uncompleted'))
              .map((e) => e['title']),
          contains('Broken episode'));
      expect(tester.takeException(), isNull);
    } finally {
      if (!expansionGate.isCompleted) expansionGate.complete();
      await tester.pumpWidget(const SizedBox());
      await handler.stop();
      container.dispose();
      await player.dispose();
      downloads.dispose();
      await repository.close();
      await publisher.close(force: true);
      await folder.delete(recursive: true);
    }
  });
}
