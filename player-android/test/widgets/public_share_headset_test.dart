// The device enum is experimental in the installed audio_session version.
// ignore_for_file: experimental_member_use

import 'dart:async';
import 'package:audio_session/audio_session.dart';
import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:just_audio/just_audio.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/providers/headset_preferences_provider.dart';
import 'package:player_android/providers/playback_session_provider.dart';
import 'package:player_android/providers/public_api_client_provider.dart';
import 'package:player_android/services/headset_preferences.dart';
import 'package:player_android/services/playback_preparer.dart';
import 'package:player_android/services/playback_session_coordinator.dart';
import 'package:player_android/widgets/public_share_collection.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _Session implements AudioSession {
  final noisy = StreamController<void>.broadcast();
  final interruptions = StreamController<AudioInterruptionEvent>.broadcast();
  final devices = StreamController<AudioDevicesChangedEvent>.broadcast();
  @override
  Stream<void> get becomingNoisyEventStream => noisy.stream;
  @override
  Stream<AudioInterruptionEvent> get interruptionEventStream =>
      interruptions.stream;
  @override
  Stream<AudioDevicesChangedEvent> get devicesChangedEventStream =>
      devices.stream;
  @override
  AndroidAudioAttributes? get androidAudioAttributes =>
      const AndroidAudioAttributes(
          usage: AndroidAudioUsage.media,
          contentType: AndroidAudioContentType.music);
  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
  void connect() => devices.add(AudioDevicesChangedEvent(devicesAdded: {
        AudioDevice(
            id: 'headphones',
            name: 'Headphones',
            isInput: false,
            isOutput: true,
            type: AudioDeviceType.wiredHeadphones),
      }));
}

class _Player extends AudioPlayer {
  bool active = false;
  int plays = 0;
  final states = StreamController<PlayerState>.broadcast();
  void publish() => states.add(PlayerState(active, ProcessingState.ready));
  @override
  bool get playing => active;
  @override
  ProcessingState get processingState => ProcessingState.ready;
  @override
  Stream<PlayerState> get playerStateStream => states.stream;
  @override
  Stream<PlaybackEvent> get playbackEventStream => const Stream.empty();
  @override
  Stream<Duration> get positionStream => const Stream.empty();
  @override
  Duration? get duration => const Duration(seconds: 30);
  @override
  Future<Duration?> setUrl(String url,
          {Map<String, String>? headers,
          Duration? initialPosition,
          bool preload = true,
          dynamic tag}) async =>
      duration;
  @override
  Future<void> pause() async {
    active = false;
    publish();
  }

  @override
  Future<void> play() async {
    active = true;
    plays++;
    publish();
  }

  @override
  Future<void> stop() async {
    active = false;
    publish();
  }

  @override
  Future<void> dispose() async {
    await states.close();
  }
}

class _Api extends PlayerApiClient {
  _Api() : super(dio: Dio(BaseOptions(baseUrl: 'http://test.local')));
  @override
  Future<PlaybackProbe> probePlayback(Uri uri,
          {CancelToken? cancelToken}) async =>
      const PlaybackProbe(statusCode: 200);
}

void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));
  for (final boundary in [
    'reconnect',
    'manual pause',
    'source replacement',
    'close'
  ]) {
    testWidgets('guest headset policy respects $boundary', (tester) async {
      final player = _Player(), session = _Session();
      final prefs = HeadsetPreferences();
      await prefs.restore();
      await prefs.save(resumeOnReconnect: true);
      final coordinator = PlaybackSessionCoordinator();
      await tester.pumpWidget(ProviderScope(
          overrides: [
            headsetPreferencesProvider.overrideWithValue(prefs),
            headsetAudioSessionProvider
                .overrideWithValue(Future.value(session)),
            playbackSessionCoordinatorProvider.overrideWithValue(coordinator),
            publicApiClientProvider.overrideWithValue(_Api()),
          ],
          child: MaterialApp(
              home: Scaffold(
                  body: PublicShareCollection(
                      player: player,
                      token: 'public-token',
                      title: 'Shared album',
                      items: const [
                PublicShareItem('Song', '/s/public-token/items/0/stream', false)
              ])))));
      await tester.pumpAndSettle();
      session.connect();
      await tester.pumpAndSettle();
      expect(player.plays, 0);
      await tester.tap(find.text('Play'));
      await tester.pumpAndSettle();
      expect(player.plays, 1);
      session.noisy.add(null);
      await tester.pumpAndSettle();
      expect(player.active, isFalse);
      if (boundary == 'manual pause') {
        // Resume deliberately, then pause: the old unplug ticket is obsolete.
        await tester.tap(find.text('Play'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('Pause'));
        await tester.pumpAndSettle();
      }
      if (boundary == 'source replacement') await coordinator.stopCurrent();
      if (boundary == 'close') await tester.pumpWidget(const SizedBox());
      final before = player.plays;
      session.connect();
      await tester.pumpAndSettle();
      expect(player.plays, before + (boundary == 'reconnect' ? 1 : 0));
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox());
      await session.noisy.close();
      await session.interruptions.close();
      await session.devices.close();
    });
  }
}
