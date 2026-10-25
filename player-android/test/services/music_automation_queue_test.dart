import 'package:flutter_test/flutter_test.dart';
import 'dart:io';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';
import 'package:player_android/models/offline_media.dart';
import 'package:player_android/services/offline_download_repository.dart';
import 'package:player_android/services/music_automation.dart';
import 'package:just_audio/just_audio.dart';
import 'package:player_android/services/audio_handler.dart';
import 'package:player_android/services/music_automation_queue.dart';
import 'package:player_android/services/music_queue.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/services/playback_session_coordinator.dart';

class _Player extends AudioPlayer {
  Duration elapsed = const Duration(seconds: 27);
  bool running = false;
  int loads = 0;
  @override
  Stream<PlaybackEvent> get playbackEventStream => const Stream.empty();
  @override
  Stream<ProcessingState> get processingStateStream => const Stream.empty();
  @override
  Stream<bool> get playingStream => const Stream.empty();
  @override
  Stream<Duration> get positionStream => const Stream.empty();
  @override
  Duration get position => elapsed;
  @override
  Duration get duration => const Duration(seconds: 120);
  @override
  bool get playing => running;
  @override
  ProcessingState get processingState => ProcessingState.ready;
  @override
  Future<void> play() async {
    running = true;
  }

  @override
  Future<void> pause() async {
    running = false;
  }

  @override
  Future<void> stop() async {
    running = false;
  }

  @override
  Future<void> seek(Duration? position, {int? index}) async {
    elapsed = position ?? Duration.zero;
  }

  @override
  Future<Duration?> setAudioSource(AudioSource source,
      {bool preload = true,
      int? initialIndex,
      Duration? initialPosition}) async {
    loads++;
    return duration;
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
      'offline Start preflight retains current listening session and later qualification; missing copies mutate no session',
      () async {
    sqfliteFfiInit();
    databaseFactory = databaseFactoryFfi;
    final dir = await Directory.systemTemp.createTemp('automation-listening-');
    final repo = OfflineDownloadRepository(directory: dir);
    final handler = PlayerAudioHandler(_Player());
    addTearDown(() async {
      await handler.stop();
      await repo.close();
      await dir.delete(recursive: true);
    });
    final item = OfflineMedia(
        key: offlineMediaKey('https://one', 1, 1),
        origin: 'https://one',
        userId: 1,
        mediaId: 1,
        title: 'Song',
        type: 'audio',
        sizeBytes: 3,
        addedAt: DateTime.utc(2026),
        automationMusicEligible: true);
    await repo.add(item);
    await repo.file(item.key).writeAsBytes([1, 2, 3]);
    final request = await repo.playbackRequest(item.key);
    final coordinator = PlaybackSessionCoordinator();
    handler.restoreMusicQueue(
        entries: [MusicQueueEntry(71, request)],
        index: 0,
        label: 'Saved music',
        position: 0,
        upNext: {},
        claim: (r) => coordinator.claim(
            kind: r.kind, identity: r.identity, stop: handler.stop),
        load: (r, _) async => AudioSource.uri(r.sourceUri));
    await handler.resumeQueue();
    final decoder = handler.player as _Player;
    for (final seconds in [10, 20]) {
      decoder.elapsed = Duration(seconds: seconds);
      await handler.saveProgress();
    }
    final db = await repo.database;
    final before = (await db.query('sync_music_plays')).single;
    expect(before['accumulated_seconds'], 20);
    await handler.pause();
    for (var i = 0; i < 2; i++) {
      await resolveAutomationMusic(
          [request],
          (r) => repo.playbackRequest((r as OfflinePlaybackRequest).downloadKey,
              startListeningSession: false),
          () => true);
      await startAutomationQueue(handler, shuffle: false, current: () => true);
      await Future<void>.delayed(Duration.zero);
    }
    expect((await db.query('sync_music_plays')).single['id'], before['id']);
    for (final seconds in [30, 40, 50, 60]) {
      decoder.elapsed = Duration(seconds: seconds);
      await handler.saveProgress();
    }
    expect(
        ((await repo.syncPayload(item.key))['music_plays'] as List)
            .single['accumulated_seconds'],
        60);
    final rows = await db.query('sync_music_plays');
    final missing = OfflinePlaybackRequest(
        downloadKey: 'missing',
        title: 'Missing',
        sourceUri: Uri.file('/missing'));
    await expectLater(
        resolveAutomationMusic(
            [request, missing],
            (r) => repo.playbackRequest(
                (r as OfflinePlaybackRequest).downloadKey,
                startListeningSession: false),
            () => true),
        throwsStateError);
    expect(await db.query('sync_music_plays'), rows);
    await repo.setAutomationMusicEligibility(item.key, 'https://one', 2, false);
    expect((await repo.get(item.key))!.automationMusicEligible, isTrue);
    await repo.setAutomationMusicEligibility(item.key, 'https://one', 1, false);
    expect((await repo.get(item.key))!.automationMusicEligible, isFalse);
    await repo.setAutomationMusicEligibility(item.key, 'https://one', 1, true);
    expect((await repo.get(item.key))!.automationMusicEligible, isTrue);
    expect(await repo.file(item.key).readAsBytes(), [1, 2, 3]);
    expect(await db.query('sync_music_plays'), rows);
    final deferred =
        await repo.playbackRequest(item.key, startListeningSession: false);
    expect(await db.query('sync_music_plays'), rows);
    // The replacement loader invokes this hook only after file/intent checks.
    await deferred.beginListening!();
    await deferred.savePosition!(70);
    final started = await db.query('sync_music_plays');
    expect(started.length, 2);
    expect(
        started
            .where((r) => r['id'] != before['id'])
            .single['accumulated_seconds'],
        10);
  });
  test(
      'Start resumes paused audio repeatedly without source, label or queue resets',
      () async {
    final player = _Player(), coordinator = PlaybackSessionCoordinator();
    final handler = PlayerAudioHandler(player);
    final request = ServerPlaybackRequest(
        mediaId: 1,
        serverOrigin: 'https://one',
        userId: 1,
        sourceUri: Uri.parse('https://one/1'),
        title: 'One',
        savePosition: (_) async {},
        markFinished: () async {});
    handler.restoreMusicQueue(
        entries: [
          MusicQueueEntry(71, request),
          MusicQueueEntry(72, request),
          MusicQueueEntry(73, request)
        ],
        index: 0,
        label: 'My album',
        position: 27,
        upNext: {},
        repeat: MusicRepeat.all,
        claim: (r) => coordinator.claim(
            kind: r.kind, identity: r.identity, stop: handler.stop),
        load: (r, _) async => AudioSource.uri(r.sourceUri));
    await handler.resumeQueue();
    await handler.pause();
    player.elapsed = const Duration(seconds: 27);
    final loads = player.loads;
    await startAutomationQueue(handler, shuffle: false, current: () => true);
    await Future<void>.delayed(Duration.zero);
    await startAutomationQueue(handler, shuffle: false, current: () => true);
    await Future<void>.delayed(Duration.zero);
    expect(player.running, isTrue);
    expect(player.position, const Duration(seconds: 27));
    expect(player.loads, loads);
    expect(handler.musicQueue.entries.first.id, 71);
    expect(handler.musicQueue.label, 'My album');
    expect(handler.musicQueue.repeat, MusicRepeat.all);
    await startAutomationQueue(handler, shuffle: true, current: () => true);
    await startAutomationQueue(handler, shuffle: true, current: () => true);
    expect(handler.musicQueue.shuffle, isTrue);
    expect(handler.musicQueue.entries.first.id, 71);
    expect(handler.queueSnapshot.canonical, [71, 72, 73]);
    expect(handler.musicQueue.label, 'My album');
    await handler.stop();
  });

  test(
      'offline rebinding preserves occurrences, position, canonical order, repeat and label',
      () async {
    final handler = PlayerAudioHandler(_Player());
    // Use a single real handler and its normal restore path, no parallel queue.
    final requests = List.generate(
        3,
        (i) => ServerPlaybackRequest(
            mediaId: i + 1,
            serverOrigin: 'https://one',
            userId: 1,
            title: 'Song $i',
            sourceUri: Uri.parse('https://one/$i')));
    handler.restoreMusicQueue(
        entries: [
          for (var i = 0; i < 3; i++) MusicQueueEntry(70 + i, requests[i])
        ],
        index: 1,
        label: 'Road trip',
        position: 27.5,
        upNext: {72},
        repeat: MusicRepeat.one,
        shuffle: true,
        canonical: [72, 70, 71],
        claim: (_) async => null,
        load: (_, __) async => null);
    await restoreAutomationQueueSources(
        handler,
        [
          for (var i = 0; i < 3; i++)
            OfflinePlaybackRequest(
                downloadKey: 'saved-$i',
                serverOrigin: 'https://one',
                userId: 1,
                sourceUri: Uri.file('/private/$i'),
                title: 'Song $i')
        ],
        current: () => true,
        claim: (_) async => null,
        load: (r, _) async => AudioSource.uri(r.sourceUri));
    final state = handler.musicQueue, snapshot = handler.queueSnapshot;
    expect(state.entries.map((e) => e.id), [70, 71, 72]);
    expect(state.entries.every((e) => e.request is OfflinePlaybackRequest),
        isTrue);
    expect(state.index, 1);
    expect(state.position, 27.5);
    expect(state.label, 'Road trip');
    expect(state.repeat, MusicRepeat.one);
    expect(state.shuffle, isTrue);
    expect(snapshot.canonical, [72, 70, 71]);
    expect(snapshot.upNext, {72});
    await handler.stop();
  });
}
