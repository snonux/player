import 'dart:async';

import 'package:audio_session/audio_session.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:just_audio/just_audio.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:player_android/services/audio_handler.dart';
import 'package:player_android/services/playback_request.dart';

class _Player extends AudioPlayer {
  bool active = true;
  int plays = 0;
  final processing = StreamController<ProcessingState>.broadcast();
  @override
  bool get playing => active;
  @override
  ProcessingState get processingState => ProcessingState.ready;
  @override
  Stream<bool> get playingStream => const Stream.empty();
  @override
  Stream<PlaybackEvent> get playbackEventStream => const Stream.empty();
  @override
  Stream<ProcessingState> get processingStateStream => processing.stream;
  @override
  Stream<Duration> get positionStream => const Stream.empty();
  @override
  Future<Duration?> setAudioSource(AudioSource source,
          {bool preload = true,
          int? initialIndex,
          Duration? initialPosition}) async =>
      const Duration(seconds: 30);
  @override
  Future<void> seek(Duration? position, {int? index}) async {}
  @override
  Future<void> pause() async {
    active = false;
  }

  @override
  Future<void> play() async {
    active = true;
    plays++;
  }

  @override
  Future<void> stop() async {
    active = false;
  }

  @override
  Future<void> setVolume(double value) async {}
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(() => SharedPreferences.setMockInitialValues({}));

  for (final action in ['pause', 'source', 'lock', 'stop']) {
    test('handler $action cancels unplug resume', () async {
      final player = _Player();
      final handler = PlayerAudioHandler(player);
      bool owned = true;
      handler.activateSourceSession(
          handler.beginSourceSession(ownsSource: () => owned));
      handler.headsetPreferences.resumeOnReconnect = true;
      await handler.headset.disconnected();
      switch (action) {
        case 'pause':
          await handler.pause();
        case 'source':
          handler.beginSourceSession();
        case 'lock':
          owned = false;
        case 'stop':
          await handler.stop();
      }
      await handler.headset.connected();
      expect(player.plays, 0);
    });
  }

  test('explicit pause beats reconnect play pending loudness refresh',
      () async {
    final player = _Player();
    final handler = PlayerAudioHandler(player);
    handler.activateSourceSession(handler.beginSourceSession());
    handler.headsetPreferences.resumeOnReconnect = true;
    await handler.headset.disconnected();
    final reconnect = handler.headset.connected();
    await handler.pause();
    await reconnect;
    expect(player.plays, 0);
  });

  test('unplug blocks a deferred automatic start until an explicit Play',
      () async {
    final player = _Player()..active = false;
    final handler = PlayerAudioHandler(player);
    handler.activateSourceSession(handler.beginSourceSession());
    await handler.headset.disconnected();
    await handler.play(automatic: true);
    expect(player.plays, 0);
    await handler.play();
    expect(player.plays, 1);
  });

  test('manual pause during a call prevents focus resume', () async {
    final player = _Player();
    final handler = PlayerAudioHandler(player);
    handler.activateSourceSession(handler.beginSourceSession());
    await handler.headset
        .interrupted(AudioInterruptionEvent(true, AudioInterruptionType.pause));
    await handler.pause();
    await handler.headset.interrupted(
        AudioInterruptionEvent(false, AudioInterruptionType.pause));
    expect(player.plays, 0);
  });

  for (final boundary in [
    'disconnect',
    'transient focus',
    'permanent focus',
    'manual pause'
  ]) {
    test('held track loader respects $boundary and explicit Next', () async {
      final player = _Player();
      final handler = PlayerAudioHandler(player);
      handler.activateSourceSession(handler.beginSourceSession());
      final entered = Completer<void>(), source = Completer<AudioSource?>();
      PlaybackRequest song(int id) => ServerPlaybackRequest(
          mediaId: id,
          serverOrigin: 'http://local',
          userId: 1,
          title: 'Song $id',
          sourceUri: Uri.parse('http://local/$id'),
          startPosition: 0,
          savePosition: (_) async {},
          markFinished: () async {});
      final entries = [song(1), song(2), song(3)];
      handler.startMusicSequence(
          wrap: false,
          entries: entries,
          index: 0,
          load: (request, current) async {
            if (request.identity == entries[1].identity) {
              entered.complete();
              return source.future;
            }
            return AudioSource.uri(request.sourceUri);
          },
          changed: (_) {},
          expand: () async => entries);
      final next = handler.skipToNext();
      await entered.future;
      if (boundary == 'disconnect') {
        await handler.headset.disconnected();
      } else if (boundary == 'manual pause') {
        await handler.pause();
      } else {
        await handler.headset.interrupted(AudioInterruptionEvent(
            true,
            boundary == 'transient focus'
                ? AudioInterruptionType.pause
                : AudioInterruptionType.unknown));
      }
      source.complete(AudioSource.uri(entries[1].sourceUri));
      await next;
      expect(player.plays, 0,
          reason: 'finishing a held load must not start speaker/call audio');
      expect(player.active, isFalse);
      expect(handler.musicQueue.index, 1);
      if (boundary.contains('focus')) {
        await handler.headset.interrupted(
            AudioInterruptionEvent(false, AudioInterruptionType.pause));
        expect(player.plays, 0,
            reason: 'loading source had no focus resume ticket');
      }
      // A new, deliberate Next still chooses and plays the following song.
      await handler.skipToNext();
      expect(handler.musicQueue.index, 2);
      expect(player.plays, 1);
      await handler.stop();
    });
  }

  test('held natural completion keeps a blocked playback intent', () async {
    final player = _Player();
    final handler = PlayerAudioHandler(player);
    handler.activateSourceSession(handler.beginSourceSession());
    final entered = Completer<void>(), changed = Completer<void>();
    final source = Completer<AudioSource?>();
    PlaybackRequest song(int id) => ServerPlaybackRequest(
        mediaId: id,
        serverOrigin: 'http://local',
        userId: 1,
        title: 'Song $id',
        sourceUri: Uri.parse('http://local/$id'),
        startPosition: 0,
        savePosition: (_) async {},
        markFinished: () async {});
    final entries = [song(1), song(2)];
    handler.startMusicSequence(
        wrap: false,
        entries: entries,
        index: 0,
        load: (request, current) {
          entered.complete();
          return source.future;
        },
        changed: (_) => changed.complete(),
        expand: () async => entries);
    handler.startProgress(
        savePosition: (_) async {}, markFinished: () async {});
    final sleepIntent = handler.sleepTimer.revision;
    // A terminal callback may already be queued when unplug pauses playback.
    await handler.headset.disconnected();
    player.processing.add(ProcessingState.completed);
    await entered.future;
    expect(handler.headset.allowsAutomaticPlayback, isFalse);
    source.complete(AudioSource.uri(entries[1].sourceUri));
    await changed.future;
    await Future<void>.delayed(Duration.zero);
    expect(handler.sleepTimer.revision, sleepIntent);
    expect(handler.musicQueue.index, 1);
    expect(player.plays, 0);
    expect(player.active, isFalse);
    await handler.stop();
  });

  testWidgets('expired sleep timer cancels unplug and focus resume',
      (tester) async {
    final player = _Player();
    final handler = PlayerAudioHandler(player);
    handler.activateSourceSession(handler.beginSourceSession());
    handler.headsetPreferences.resumeOnReconnect = true;
    await handler.headset.disconnected();
    handler.sleepTimer.start(const Duration(milliseconds: 1));
    await tester
        .runAsync(() => Future<void>.delayed(const Duration(milliseconds: 5)));
    handler.sleepTimer.check();
    await handler.headset.connected();
    expect(player.plays, 0);
    expect(handler.sleepTimer.expired, isTrue);
    handler.sleepTimer.dispose();
  });
}
