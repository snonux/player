import 'dart:async';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:integration_test/integration_test.dart';
import 'package:just_audio/just_audio.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:player_android/podcast_routes.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/audio_handler_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/local_library_provider.dart';
import 'package:player_android/providers/podcast_library_provider.dart';
import 'package:player_android/providers/playback_session_provider.dart';
import 'package:player_android/providers/settings_provider.dart';
import 'package:player_android/screens/standalone_podcasts_screen.dart';
import 'package:player_android/services/audio_handler.dart';
import 'package:player_android/services/playback_session_coordinator.dart';
import 'package:player_android/services/podcast_downloads.dart';
import 'package:player_android/services/podcast_repository.dart';
import 'package:player_android/services/podcast_sync_store.dart';

class _SignedOut extends AuthStateNotifier {
  @override
  Future<AuthState> build() async => const AuthState.unauthenticated();
}

class _DeviceSettings extends SettingsNotifier {
  @override
  Future<AppSettings> build() async => const AppSettings(
      serverBaseUrl: null, destination: LibraryDestination.local);
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
    wav.setInt16(i, i % 40 < 20 ? 4000 : -4000, Endian.little);
  }
  return wav.buffer.asUint8List();
}

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();
  testWidgets(
      'native streaming stays playable while saving, scoped cancel and offline retry',
      (tester) async {
    SharedPreferences.setMockInitialValues({});
    final directory =
        await Directory.systemTemp.createTemp('podcast-stream-download-');
    final publisher = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    var publisherClosed = false;
    final origin = 'http://127.0.0.1:${publisher.port}';
    final audio = _audio();
    final saveStarted = Completer<void>();
    final saveDisconnected = Completer<void>();
    var saveReleased = false;
    var audioRequests = 0;
    var savedBytesSent = 0;
    var refusePlayback = false;
    final credentialHeaders = <String>[];
    publisher.listen((request) async {
      for (final name in ['authorization', 'cookie']) {
        if (request.headers.value(name) != null) credentialHeaders.add(name);
      }
      final response = request.response;
      if (request.uri.path == '/feed') {
        response.write('<rss><channel><title>Download podcast</title><item>'
            '<guid>one</guid><title>A long episode title with enough readable words to check podcast controls</title>'
            '<enclosure url="$origin/audio.wav"/><duration>60</duration>'
            '</item></channel></rss>');
        await response.close();
        return;
      }
      final number = ++audioRequests;
      if (refusePlayback && number != 1) {
        response.statusCode = HttpStatus.forbidden;
        await response.close();
        return;
      }
      response.headers.contentType = ContentType('audio', 'wav');
      // Native playback may request a range when resuming the publisher URL.
      var start = 0;
      final range = request.headers.value(HttpHeaders.rangeHeader);
      if (range != null) {
        final match = RegExp(r'^bytes=(\d+)-').firstMatch(range);
        if (match != null) start = int.parse(match.group(1)!);
        start = start.clamp(0, audio.length - 1);
        response.statusCode = HttpStatus.partialContent;
        response.headers.set(HttpHeaders.contentRangeHeader,
            'bytes $start-${audio.length - 1}/${audio.length}');
      }
      response.headers.set(HttpHeaders.acceptRangesHeader, 'bytes');
      response.contentLength = audio.length - start;
      if (number == 1) {
        // Observe the actual peer disconnect on the held save socket. The
        // HttpResponse.done future only settles when the server closes its
        // response and cannot prove a client cancellation by itself.
        final socket = await response.detachSocket(writeHeaders: false);
        void disconnected() {
          if (!saveReleased && !saveDisconnected.isCompleted) {
            saveDisconnected.complete();
          }
          socket.destroy();
        }

        socket.listen((_) {},
            onDone: disconnected, onError: (Object _) => disconnected());
        try {
          socket.write('HTTP/1.1 200 OK\r\n'
              'Content-Type: audio/wav\r\n'
              'Content-Length: ${audio.length}\r\n'
              'Connection: close\r\n\r\n');
          savedBytesSent = 4096;
          socket.add(audio.sublist(0, savedBytesSent));
          await socket.flush();
          saveStarted.complete();
          while (!saveReleased &&
              !saveDisconnected.isCompleted &&
              savedBytesSent < audio.length - 512) {
            await Future<void>.delayed(const Duration(milliseconds: 50));
            socket.add(audio.sublist(savedBytesSent, savedBytesSent + 512));
            savedBytesSent += 512;
            await socket.flush();
          }
          if (saveReleased) socket.add(audio.sublist(savedBytesSent));
          await socket.close();
        } catch (_) {
          disconnected();
        }
      } else {
        response.add(audio.sublist(start));
        try {
          await response.close();
        } catch (_) {
          // Native decoders can close a range after buffering sufficient audio.
        }
      }
    });
    final repository =
        PodcastRepository(databasePath: '${directory.path}/podcasts.db');
    final coordinator = PlaybackSessionCoordinator();
    final downloads = PodcastDownloads(repository, coordinator,
        directory: Directory('${directory.path}/audio'));
    final feed = await repository.subscribe('local', '$origin/feed');
    final episode = (await repository.episodes(feed)).single;
    final key = episode['episode_key'] as String;
    final store = PodcastSyncStore(repository);
    await store.saveEpisode('local', feed, key, seconds: 5);
    final player = AudioPlayer();
    final handler = PlayerAudioHandler(player);
    final container = ProviderContainer(overrides: [
      audioHandlerProvider.overrideWithValue(handler),
      playbackSessionCoordinatorProvider.overrideWithValue(coordinator),
      authStateProvider.overrideWith(_SignedOut.new),
      settingsProvider.overrideWith(_DeviceSettings.new),
      localMediaListProvider.overrideWith((ref) async => []),
      podcastRepositoryProvider.overrideWithValue(repository),
      podcastDownloadsProvider.overrideWith((ref) => downloads),
      apiClientProvider.overrideWith((ref) =>
          throw StateError('Publisher playback used server credentials/API')),
    ]);
    container.read(podcastDownloadsProvider);
    final surface = ValueNotifier(const Size(390, 844));
    final router = GoRouter(routes: [
      GoRoute(
          path: '/',
          builder: (_, __) => LocalPodcastEpisodesScreen(feedKey: feed)),
      ...podcastRoutes(),
    ]);
    Future<void> until(FutureOr<bool> Function() ready) async {
      final end = DateTime.now().add(const Duration(seconds: 20));
      while (!await ready() && DateTime.now().isBefore(end)) {
        await tester.pump(const Duration(milliseconds: 100));
      }
      expect(await ready(), isTrue,
          reason: 'player=${player.playerState} position=${player.position} '
              'requests=$audioRequests saveBytes=$savedBytesSent '
              'error=${handler.musicState?.error}');
    }

    Future<void> tap(Finder finder) async {
      await tester.ensureVisible(finder);
      await tester.pump(const Duration(milliseconds: 200));
      await tester.tap(finder);
      await tester.pump(const Duration(milliseconds: 200));
    }

    Finder play() => find.widgetWithIcon(FilledButton, Icons.play_arrow);
    try {
      await container.read(settingsProvider.future);
      await container.read(authStateProvider.future);
      await tester.binding.setSurfaceSize(surface.value);
      await tester.pumpWidget(UncontrolledProviderScope(
          container: container,
          child: MaterialApp.router(
              routerConfig: router,
              builder: (context, child) => ValueListenableBuilder<Size>(
                  valueListenable: surface,
                  builder: (_, size, __) => MediaQuery(
                      data: MediaQuery.of(context).copyWith(
                          size: size, textScaler: const TextScaler.linear(1.6)),
                      child: child!)))));
      await tester.pumpAndSettle();
      await tap(find.text('Download'));
      await until(() => saveStarted.isCompleted);
      expect((await downloads.copy(feed, key))!['state'], 'downloading');
      expect(saveReleased, isFalse);
      expect(savedBytesSent, lessThan(audio.length));
      for (final size in [const Size(390, 844), const Size(844, 390)]) {
        surface.value = size;
        await tester.binding.setSurfaceSize(size);
        await tester.pumpAndSettle();
        for (final layout in ['List', 'Grid']) {
          await tap(find.text(layout));
          for (final action in [play(), find.text('Cancel download')]) {
            await tester.ensureVisible(action);
            await tester.pumpAndSettle();
            expect(action.hitTestable(), findsOneWidget);
            final button = tester.widget(action) is ButtonStyleButton
                ? action
                : find.ancestor(of: action, matching: find.byType(TextButton));
            expect(tester.getSize(button).height, greaterThanOrEqualTo(48));
          }
          expect(tester.takeException(), isNull);
        }
      }
      surface.value = const Size(390, 844);
      await tester.binding.setSurfaceSize(surface.value);
      await tester.pumpAndSettle();
      // Failed native playback is recoverable while the independent save stays
      // active. No fake player or source callback stands in for this boundary.
      refusePlayback = true;
      await tap(play());
      await until(() =>
          find.byKey(const Key('audio_player_error')).evaluate().isNotEmpty);
      expect((await downloads.copy(feed, key))!['state'], 'downloading');
      refusePlayback = false;
      await tap(find.byKey(const Key('audio_player_retry')));
      await until(
          () => player.playing && player.position > const Duration(seconds: 5));
      expect((await downloads.copy(feed, key))!['state'], 'downloading');
      expect(saveReleased, isFalse);
      expect(savedBytesSent, lessThan(audio.length));
      debugPrint(
          'STREAM_WHILE_DOWNLOADING position_ms=${player.position.inMilliseconds} save_bytes=$savedBytesSent/${audio.length}');
      await tester.pageBack();
      await tester.pump(const Duration(milliseconds: 500));
      await until(() => find.text('Cancel download').evaluate().isNotEmpty);
      final beforeCancel = player.position;
      await tap(find.text('Cancel download'));
      await until(
          () async => (await downloads.copy(feed, key))!['state'] == 'missing');
      await until(() => saveDisconnected.isCompleted);
      await until(() async =>
          (await Directory('${directory.path}/audio').list().toList()).isEmpty);
      expect(
          await Directory('${directory.path}/audio').list().toList(), isEmpty);
      await until(() =>
          player.playing &&
          player.position > beforeCancel + const Duration(milliseconds: 200));
      final history =
          (await (await repository.database).query('podcast_episode_state'))
              .single;
      expect(history['position_seconds'] as num, greaterThanOrEqualTo(5));
      expect(history['completed'], 0);
      expect(history['deleted'], 0);
      debugPrint(
          'CANCEL_PRESERVES_STREAM position_ms=${player.position.inMilliseconds} disconnected=${saveDisconnected.isCompleted}');
      await tap(find.text('Download'));
      await until(() async =>
          (await downloads.copy(feed, key))!['state'] == 'available');
      expect(player.playing, isTrue);
      final saved = await downloads.playbackRequest('local', feed, episode,
          allowStreaming: true);
      expect(saved.sourceUri.scheme, 'file');
      expect(await File.fromUri(saved.sourceUri).length(), audio.length);
      await handler.stop();
      final beforeOffline = audioRequests;
      await publisher.close(force: true);
      publisherClosed = true;
      await tap(play());
      await until(() => player.playing && player.position > Duration.zero);
      expect(audioRequests, beforeOffline);
      expect(handler.musicState?.request.sourceUri.scheme, 'file');
      expect(credentialHeaders, isEmpty);
      expect(tester.takeException(), isNull);
    } finally {
      saveReleased = true;
      await tester.pumpWidget(const SizedBox());
      await handler.stop();
      container.dispose();
      await player.dispose();
      router.dispose();
      surface.dispose();
      await tester.binding.setSurfaceSize(null);
      if (!publisherClosed) await publisher.close(force: true);
      await repository.close();
      await directory.delete(recursive: true);
    }
  });
}
