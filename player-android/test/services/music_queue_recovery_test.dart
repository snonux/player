import 'dart:async';
import 'dart:convert';
import 'package:cookie_jar/cookie_jar.dart';
import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:just_audio/just_audio.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/models/media.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/audio_handler_provider.dart';
import 'package:player_android/providers/music_queue_persistence_provider.dart';
import 'package:player_android/services/audio_handler.dart';
import 'package:player_android/services/music_queue.dart';
import 'package:player_android/services/music_queue_snapshot.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/services/playback_session_coordinator.dart';

class _Player extends AudioPlayer {
  Duration elapsed = Duration.zero;
  int plays = 0, loads = 0;
  bool running = false;
  @override
  Stream<Duration> get positionStream => const Stream.empty();
  @override
  Stream<bool> get playingStream => const Stream.empty();
  @override
  Duration get position => elapsed;
  @override
  Duration get duration => const Duration(seconds: 12);
  @override
  ProcessingState get processingState => ProcessingState.ready;
  @override
  bool get playing => running;
  @override
  Future<void> pause() async {
    running = false;
  }

  @override
  Future<void> stop() async {
    running = false;
  }

  @override
  Future<void> play() async {
    plays++;
    running = true;
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

class _Client extends PlayerApiClient {
  _Client() : super(dio: Dio());
  Future<Media> Function(int)? answer;
  @override
  Future<Media> getMedia(int id, {CancelToken? cancelToken}) async => answer ==
          null
      ? Media.fromJson(
          {'id': id, 'type': 'audio', 'file_name': '$id.mp3', 'duration': 12})
      : await answer!(id);
}

const snapshot = MusicQueueSnapshot(
    entries: [(id: 71, mediaId: 1), (id: 72, mediaId: 2), (id: 73, mediaId: 1)],
    index: 2,
    position: 7.25,
    label: 'Repeated album',
    upNext: {72});
Future<void> settle() => Future<void>.delayed(const Duration(milliseconds: 40));
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test('repeat snapshot supports legacy default and all explicit modes', () {
    final legacy = jsonDecode(snapshot.encode()) as Map;
    legacy.remove('repeat');
    expect(
        MusicQueueSnapshot.decode(jsonEncode(legacy)).repeat, MusicRepeat.off);
    for (final mode in MusicRepeat.values) {
      expect(
          MusicQueueSnapshot.decode(
              jsonEncode({...legacy, 'repeat': mode.name})).repeat,
          mode);
    }
    for (final bad in [null, 0, false, 'invalid']) {
      expect(
          () =>
              MusicQueueSnapshot.decode(jsonEncode({...legacy, 'repeat': bad})),
          throwsFormatException);
    }
  });
  test(
      'codec rejects corrupt and unknown data and preserves repeated identities',
      () {
    expect(
        MusicQueueSnapshot.decode(snapshot.encode()).entries, snapshot.entries);
    for (final raw in [
      'bad',
      '{}',
      jsonEncode({...jsonDecode(snapshot.encode()) as Map, 'version': 2}),
      jsonEncode({...jsonDecode(snapshot.encode()) as Map, 'position': -1}),
      jsonEncode({
        ...jsonDecode(snapshot.encode()) as Map,
        'entries': [
          {'id': 71, 'mediaId': 1},
          {'id': 71, 'mediaId': 2}
        ]
      })
    ]) {
      expect(() => MusicQueueSnapshot.decode(raw), throwsFormatException);
    }
    expect(snapshot.encode(), isNot(contains('http')));
    expect(MusicQueueSnapshot.key('http://one', 1),
        isNot(MusicQueueSnapshot.key('http://two', 1)));
    expect(MusicQueueSnapshot.key('http://one', 1),
        isNot(MusicQueueSnapshot.key('http://one', 2)));
  });
  test(
      'paused handler recovery follows repeated occurrence, resumes exact offset and clears durably',
      () async {
    final player = _Player(), coordinator = PlaybackSessionCoordinator();
    final handler = PlayerAudioHandler(player);
    final requests = [1, 2, 1]
        .map((id) => ServerPlaybackRequest(
            mediaId: id,
            serverOrigin: 'http://one',
            userId: 1,
            title: 'Song $id',
            sourceUri: Uri.parse('http://one/$id'),
            startPosition: 0,
            savePosition: (_) async {},
            markFinished: () async {}))
        .toList();
    handler.restoreMusicQueue(
        repeat: MusicRepeat.one,
        entries: [
          for (var i = 0; i < 3; i++) MusicQueueEntry(71 + i, requests[i])
        ],
        index: 2,
        label: 'Repeated album',
        position: 7.25,
        upNext: {72},
        claim: (r) => coordinator.claim(
            kind: r.kind, identity: r.identity, stop: handler.stop),
        load: (r, current) async => AudioSource.uri(r.sourceUri));
    expect(handler.musicQueue.repeat, MusicRepeat.one);
    expect(player.plays, 0);
    expect(player.loads, 0);
    expect(handler.musicQueue.restored, isTrue);
    expect(handler.musicQueue.position, 7.25);
    handler.moveQueue(2, -1);
    expect(handler.musicQueue.index, 1);
    expect(handler.musicQueue.position, 7.25);
    final writes = <MusicQueueSnapshot>[];
    handler.persistMusicQueue = () => writes.add(handler.queueSnapshot);
    await handler.jumpQueue(1);
    expect(player.elapsed, const Duration(milliseconds: 7250));
    expect(player.plays, 1);
    expect(handler.musicQueue.entries[handler.musicQueue.index].id, 73);
    player.elapsed = const Duration(milliseconds: 8250);
    await handler.pause();
    expect(writes.last.position, 8.25);
    await handler.clearQueue();
    expect(writes.last.entries, isEmpty);
    expect(writes.last.repeat, MusicRepeat.one);
    await handler.undoQueue();
    expect(writes.last.entries.length, 3);
    expect(player.running, isFalse);
    handler.clearRetainedQueue();
    expect(handler.musicQueue.label, isEmpty);
    expect(handler.persistMusicQueue, isNull);
    await handler.stop();
  });
  test(
      'recovered Resume retains future Play next FIFO, explicit jump invalidates it',
      () async {
    final player = _Player();
    final handler = PlayerAudioHandler(player);
    final coordinator = PlaybackSessionCoordinator();
    ServerPlaybackRequest request(int id) => ServerPlaybackRequest(
        mediaId: id,
        serverOrigin: 'http://one',
        userId: 1,
        title: '$id',
        sourceUri: Uri.parse('http://one/$id'),
        startPosition: 0,
        savePosition: (_) async {},
        markFinished: () async {});
    Future<PlaybackSessionLease?> claim(PlaybackRequest r) => coordinator.claim(
        kind: r.kind, identity: r.identity, stop: handler.stop);
    Future<AudioSource?> load(
            PlaybackRequest r, bool Function() current) async =>
        AudioSource.uri(r.sourceUri);
    handler.restoreMusicQueue(
        entries: [
          for (final id in [1, 2, 3, 4]) MusicQueueEntry(90 + id, request(id))
        ],
        index: 0,
        label: 'FIFO',
        position: 4,
        upNext: {92, 93},
        claim: claim,
        load: load);
    expect(player.plays, 0);
    await handler.resumeQueue();
    expect(handler.musicQueue.upNextIds, {92, 93});
    await handler.insertQueue(
        entries: [request(5)],
        action: QueueInsertion.next,
        claim: claim,
        load: load);
    expect(
        handler.musicQueue.entries
            .map((e) => (e.request as ServerPlaybackRequest).mediaId),
        [1, 2, 3, 5, 4]);
    await handler.jumpQueue(1);
    expect(handler.musicQueue.upNextIds, isEmpty);
    await handler.stop();
    expect(player.plays, greaterThan(0));
  });
  for (final scenario in [
    'normal',
    'denied',
    'missing-current',
    'offline',
    'corrupt',
    'scope',
    'replacement'
  ]) {
    test('scoped persisted recovery $scenario', () async {
      final key = MusicQueueSnapshot.key('http://one', 1);
      SharedPreferences.setMockInitialValues(
          {key: scenario == 'corrupt' ? 'broken' : snapshot.encode()});
      final client = _Client(), player = _Player();
      final handler = PlayerAudioHandler(player);
      final container = ProviderContainer(overrides: [
        audioHandlerProvider.overrideWithValue(handler),
        playerBaseUrlProvider.overrideWithValue(Uri.parse('http://one')),
        apiClientProvider.overrideWithValue(client),
        cookieJarProvider.overrideWithValue(CookieJar()),
      ]);
      Completer<Media>? pending;
      if (scenario == 'denied' || scenario == 'missing-current') {
        client.answer = (id) async {
          if (id == (scenario == 'denied' ? 2 : 1)) {
            throw DioException(
                requestOptions: RequestOptions(),
                response: Response(
                    requestOptions: RequestOptions(), statusCode: 403));
          }
          return Media.fromJson({'id': id, 'type': 'audio', 'duration': 12});
        };
      }
      if (scenario == 'offline') {
        client.answer = (_) async => throw StateError('offline');
      }
      if (scenario == 'scope' || scenario == 'replacement') {
        pending = Completer<Media>();
        client.answer = (_) => pending!.future;
      }
      await container.read(musicQueuePersistenceProvider).initialize(1);
      await settle();
      if (scenario == 'scope') {
        handler.clearRetainedQueue();
        await container.read(musicQueuePersistenceProvider).initialize(2);
        pending!.complete(Media.fromJson({'id': 1, 'type': 'audio'}));
        await settle();
        expect(handler.musicQueue.entries, isEmpty);
        expect(handler.musicQueue.label, isEmpty);
      } else if (scenario == 'replacement') {
        handler.activateSourceSession(handler.beginSourceSession());
        final request = ServerPlaybackRequest(
            mediaId: 99,
            serverOrigin: 'http://one',
            userId: 1,
            title: 'New',
            sourceUri: Uri.parse('http://one/99'));
        handler.startMusicSequence(
            entries: [request],
            index: 0,
            wrap: false,
            queueLabel: 'Replacement',
            expand: () async => [request],
            changed: (_) {},
            load: (r, c) async => AudioSource.uri(r.sourceUri));
        pending!.complete(Media.fromJson({'id': 1, 'type': 'audio'}));
        await settle();
        expect(handler.musicQueue.label, 'Replacement');
        expect(
            MusicQueueSnapshot.decode(
                    (await SharedPreferences.getInstance()).getString(key)!)
                .entries
                .single
                .mediaId,
            99);
      } else if (scenario == 'offline') {
        expect(handler.musicQueue.restoreFailed, isTrue);
        expect((await SharedPreferences.getInstance()).getString(key),
            snapshot.encode());
        client.answer = null;
        await handler.retryQueueRestore!();
        await settle();
        expect(handler.musicQueue.entries.length, 3);
        expect(handler.musicQueue.position, 7.25);
      } else if (scenario == 'corrupt') {
        expect(handler.musicQueue.status, contains('unreadable'));
        expect((await SharedPreferences.getInstance()).getString(key), isNull);
      } else {
        expect(
            handler.musicQueue.entries.length,
            scenario == 'normal'
                ? 3
                : scenario == 'denied'
                    ? 2
                    : 1);
        expect(
            handler.musicQueue.index,
            scenario == 'normal'
                ? 2
                : scenario == 'denied'
                    ? 1
                    : 0);
        expect(handler.musicQueue.position,
            scenario == 'missing-current' ? 0 : 7.25);
        expect(handler.musicQueue.upNextIds,
            scenario == 'denied' ? isEmpty : {72});
      }
      expect(player.plays, 0);
      expect(player.loads, 0);
      expect(handler.musicQueue.active, scenario == 'replacement');
      handler.clearRetainedQueue();
      await handler.stop();
      container.dispose();
    });
  }
}
