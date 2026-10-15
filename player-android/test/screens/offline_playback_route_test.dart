import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:just_audio/just_audio.dart';
import 'package:player_android/models/offline_media.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/audio_handler_provider.dart';
import 'package:player_android/providers/offline_downloads_provider.dart';
import 'package:player_android/providers/playback_session_provider.dart';
import 'package:player_android/screens/audio_player_screen.dart';
import 'package:player_android/screens/offline_playback_route.dart';
import 'package:player_android/services/audio_handler.dart';
import 'package:player_android/services/offline_download_repository.dart';
import 'package:player_android/services/offline_downloads.dart';
import 'package:player_android/services/playback_session_coordinator.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';

class _ReadyAudio extends AudioPlayer {
  int sourceLoads = 0;
  bool isPlaying = false;
  Duration elapsed = Duration.zero;

  @override
  Future<Duration?> setAudioSource(AudioSource source,
      {bool preload = true,
      int? initialIndex,
      Duration? initialPosition}) async {
    expect((source as UriAudioSource).uri.scheme, 'file');
    sourceLoads++;
    return const Duration(seconds: 100);
  }

  @override
  Duration get position => elapsed;
  @override
  Duration? get duration => const Duration(seconds: 100);
  @override
  bool get playing => isPlaying;
  @override
  Stream<bool> get playingStream => const Stream.empty();
  @override
  Stream<PlaybackEvent> get playbackEventStream => const Stream.empty();
  @override
  Stream<ProcessingState> get processingStateStream => const Stream.empty();
  @override
  Future<void> seek(Duration? position, {int? index}) async {
    elapsed = position ?? Duration.zero;
  }

  @override
  Future<void> play() async {
    isPlaying = true;
  }

  @override
  Future<void> pause() async {
    isPlaying = false;
  }

  @override
  Future<void> stop() async {
    isPlaying = false;
  }
}

class _CountingRepository extends OfflineDownloadRepository {
  _CountingRepository(Directory directory) : super(directory: directory);
  int metadataReads = 0;
  @override
  Future<OfflineMedia?> get(String key) {
    metadataReads++;
    return super.get(key);
  }
}

class _DownloadProgress extends OfflineDownloads {
  _DownloadProgress(super.repository, super.playback);
  void progress(int bytes) {
    active ??= OfflineDownloadJob(
        offlineMediaKey('https://server.example', 7, 12), 'B.mp3');
    active!.received = bytes;
    notifyListeners();
  }
}

void main() {
  setUpAll(() {
    sqfliteFfiInit();
    databaseFactory = databaseFactoryFfiNoIsolate;
  });

  testWidgets(
      'another download progress and metadata refresh preserve the playing screen',
      (tester) async {
    final repository = (await tester.runAsync(() async {
      final directory =
          await Directory.systemTemp.createTemp('player-offline-route-');
      return _CountingRepository(directory);
    }))!;
    final key = offlineMediaKey('https://server.example', 7, 11);
    final player = _ReadyAudio();
    final handler = PlayerAudioHandler(player);
    final playback = PlaybackSessionCoordinator();
    final downloads = _DownloadProgress(repository, playback);
    final container = ProviderContainer(overrides: [
      offlineDownloadRepositoryProvider.overrideWithValue(repository),
      offlineDownloadsProvider.overrideWith((ref) => downloads),
      playbackSessionCoordinatorProvider.overrideWithValue(playback),
      audioHandlerProvider.overrideWithValue(handler),
      apiClientProvider.overrideWith(
          (ref) => throw StateError('Offline route used server API')),
    ]);
    await tester.runAsync(() async {
      await repository.database;
      await repository.file(key).writeAsBytes([1, 2, 3, 4]);
      await repository.add(OfflineMedia(
          key: key,
          origin: 'https://server.example',
          userId: 7,
          mediaId: 11,
          title: 'A.mp3',
          type: 'audio',
          sizeBytes: 4,
          addedAt: DateTime.now()));
    });
    addTearDown(() async {
      await tester.runAsync(handler.stop);
      container.dispose();
      await tester.runAsync(repository.close);
      repository.directory.deleteSync(recursive: true);
    });
    await tester.pumpWidget(UncontrolledProviderScope(
        container: container,
        child: MaterialApp(home: OfflinePlaybackRoute(downloadKey: key))));
    for (var i = 0;
        i < 100 &&
            find.byKey(const Key('audio_player_view')).evaluate().isEmpty;
        i++) {
      await tester.pump(const Duration(milliseconds: 10));
      await tester.runAsync(
          () => Future<void>.delayed(const Duration(milliseconds: 10)));
    }
    expect(find.byKey(const Key('audio_player_view')), findsOneWidget);
    expect(player.sourceLoads, 1);
    final element = tester.element(find.byType(AudioPlayerScreen));
    final reads = repository.metadataReads;
    for (final bytes in [100, 200, 300]) {
      downloads.progress(bytes);
      await tester.pump(const Duration(milliseconds: 150));
      expect(tester.element(find.byType(AudioPlayerScreen)), same(element));
      expect(repository.metadataReads, reads);
      expect(player.sourceLoads, 1);
      expect(player.playing, isTrue);
    }
    // A genuine metadata refresh may still occur on collection updates; keep
    // the already-mounted player while the refreshed row is being read.
    final refreshing = container.refresh(offlineMediaProvider(key));
    expect(refreshing.isLoading, isTrue);
    await tester.pump();
    expect(tester.element(find.byType(AudioPlayerScreen)), same(element));
    await tester
        .runAsync(() => Future<void>.delayed(const Duration(milliseconds: 50)));
    await tester.pump();
    expect(tester.element(find.byType(AudioPlayerScreen)), same(element));
    expect(player.sourceLoads, 1);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
    await tester.runAsync(handler.stop);
  });
}
