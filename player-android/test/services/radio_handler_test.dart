import 'dart:async';
import 'package:audio_service/audio_service.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:just_audio/just_audio.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:player_android/services/audio_handler.dart';
import 'package:player_android/services/playback_request.dart';

class _Player extends AudioPlayer {
  final entered = Completer<void>(), release = Completer<void>();
  int plays = 0, loads = 0, stops = 0, seeks = 0;
  bool active = false;
  ProcessingState state = ProcessingState.ready;
  final events = StreamController<PlaybackEvent>.broadcast();
  @override
  bool get playing => active;
  @override
  ProcessingState get processingState => state;
  @override
  Stream<bool> get playingStream => const Stream.empty();
  @override
  Stream<PlaybackEvent> get playbackEventStream => events.stream;
  @override
  Stream<ProcessingState> get processingStateStream => const Stream.empty();
  @override
  Stream<Duration> get positionStream => const Stream.empty();
  @override
  Duration get position => const Duration(seconds: 20);
  @override
  Duration? get duration => const Duration(minutes: 2);
  @override
  Future<Duration?> setAudioSource(AudioSource source,
      {bool preload = true,
      int? initialIndex,
      Duration? initialPosition}) async {
    loads++;
    if (!entered.isCompleted) entered.complete();
    await release.future;
    state = ProcessingState.ready;
    return duration;
  }

  @override
  Future<void> setSpeed(double value) async {}
  @override
  Future<void> setVolume(double value) async {}
  @override
  Future<void> pause() async {
    active = false;
  }

  @override
  Future<void> play() async {
    active = true;
    state = ProcessingState.ready;
    plays++;
  }

  @override
  Future<void> stop() async {
    active = false;
    state = ProcessingState.idle;
    stops++;
    events.add(PlaybackEvent());
  }

  @override
  Future<void> seek(Duration? position, {int? index}) async {
    seeks++;
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(() => SharedPreferences.setMockInitialValues({}));
  LiveRadioPlaybackRequest request(int id) => LiveRadioPlaybackRequest(
      stationId: id,
      serverOrigin: 'http://local',
      userId: 1,
      sourceUri: Uri.parse('http://local/api/v1/radio/$id/stream'),
      title: 'Station $id');

  for (final boundary in ['unplug', 'manual pause', 'sleep']) {
    test('live held load preserves $boundary and explicit live-edge resume',
        () async {
      final player = _Player();
      final live = PlayerAudioHandler(player);
      live.headsetPreferences.resumeOnReconnect = true;
      final generation = live.beginSourceSession();
      live.setMediaRequest(request(1));
      final loading = live.loadLiveSourceForSession(
          AudioSource.uri(request(1).sourceUri),
          generation,
          live.transportRevision);
      await player.entered.future;
      if (boundary == 'unplug') {
        await live.headset.disconnected();
      } else if (boundary == 'manual pause') {
        await live.pause();
      } else {
        live.sleepTimer.start(const Duration(microseconds: 1));
        await Future<void>.delayed(const Duration(milliseconds: 5));
        live.sleepTimer.check();
      }
      player.release.complete();
      await loading;
      await Future<void>.delayed(Duration.zero);
      await live.headset.connected();
      expect(player.plays, 0);
      expect(live.isSourceSessionCurrent(generation), isTrue,
          reason:
              'policy-blocked live load still exposes usable paused controls');
      expect(player.stops, greaterThan(0),
          reason: 'live pause releases its network source');
      await live.play();
      expect(player.loads, 2,
          reason: 'explicit Play reconnects live, never resumes stale buffer');
      expect(player.plays, 1);
      await live.stop();
    });
  }
  test(
      'opted-in reconnect reloads a stopped live source, manual pause cancels it',
      () async {
    final player = _Player()..release.complete();
    final handler = PlayerAudioHandler(player);
    handler.headsetPreferences.resumeOnReconnect = true;
    final generation = handler.beginSourceSession();
    handler.setMediaRequest(request(1));
    await handler.loadLiveSourceForSession(
        AudioSource.uri(request(1).sourceUri),
        generation,
        handler.transportRevision);
    await Future<void>.delayed(Duration.zero);
    expect(player.plays, 1);
    await handler.headset.disconnected();
    expect(player.processingState, ProcessingState.idle);
    await Future<void>.delayed(Duration.zero);
    expect(
        handler.playbackState.value.processingState, AudioProcessingState.ready,
        reason:
            'live Pause retains usable notification controls while decoder is stopped');
    await handler.headset.connected();
    expect(player.loads, 2);
    expect(player.plays, 2);
    await handler.headset.disconnected();
    await handler.pause();
    await handler.headset.connected();
    expect(player.plays, 2);
    await handler.stop();
  });
  test(
      'live session advertises no duration/seek/skip and restores normal controls',
      () async {
    final player = _Player()..release.complete();
    final handler = PlayerAudioHandler(player);
    final generation = handler.beginSourceSession();
    handler.setMediaRequest(request(1));
    await handler.loadLiveSourceForSession(
        AudioSource.uri(request(1).sourceUri),
        generation,
        handler.transportRevision);
    player.events.add(PlaybackEvent());
    await Future<void>.delayed(Duration.zero);
    expect(handler.mediaItem.value!.duration, isNull);
    expect(handler.playbackState.value.controls,
        isNot(contains(MediaControl.skipToNext)));
    expect(handler.playbackState.value.systemActions,
        isNot(contains(MediaAction.seek)));
    await handler.seek(const Duration(seconds: 10));
    await handler.skipToNext();
    await handler.skipToPrevious();
    expect(player.seeks, 0);
    handler.beginSourceSession();
    handler.setMediaRequest(ServerPlaybackRequest(
        mediaId: 1,
        serverOrigin: 'http://local',
        userId: 1,
        sourceUri: Uri.parse('http://local/song'),
        title: 'Song'));
    player.events.add(PlaybackEvent());
    await Future<void>.delayed(Duration.zero);
    expect(handler.mediaItem.value!.duration, isNotNull);
    expect(
        handler.playbackState.value.systemActions, contains(MediaAction.seek));
    await handler.stop();
  });
}
