// Device enumeration is experimental in the pinned audio_session version.
// ignore_for_file: experimental_member_use

import 'package:audio_session/audio_session.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:player_android/services/headset_policy.dart';
import 'package:player_android/services/headset_preferences.dart';

class _Playback {
  bool playing = true, owned = true;
  int pauses = 0, resumes = 0;
  double volume = .8;
  late final preferences = HeadsetPreferences();
  late final policy = HeadsetPolicy(
    preferences: preferences,
    isPlaying: () => playing,
    canResume: () => owned,
    pausePlayback: () async {
      pauses++;
      playing = false;
    },
    resumePlayback: () async {
      resumes++;
      playing = true;
    },
    volume: () => volume,
    setVolume: (value) async {
      volume = value;
    },
  );
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(() => SharedPreferences.setMockInitialValues({}));

  test('fresh install pauses on unplug and never autostarts on connect',
      () async {
    final p = _Playback();
    await p.preferences.restore();
    expect(p.preferences.pauseOnDisconnect, isTrue);
    expect(p.preferences.resumeOnReconnect, isFalse);
    await p.policy.connected();
    expect(p.resumes, 0);
    await p.policy.disconnected();
    expect(p.pauses, 1);
    await p.policy.connected();
    expect(p.resumes, 0);
  });

  test('preferences survive restart as one coherent pair', () async {
    final prefs = HeadsetPreferences();
    await prefs.restore();
    await prefs.save(pauseOnDisconnect: false, resumeOnReconnect: true);
    final restored = HeadsetPreferences();
    await restored.restore();
    expect(restored.pauseOnDisconnect, isFalse);
    expect(restored.resumeOnReconnect, isTrue);
    SharedPreferences.setMockInitialValues({'headset.policy': 'invalid,true'});
    final corrupt = HeadsetPreferences();
    await corrupt.restore();
    expect(corrupt.pauseOnDisconnect, isTrue);
    expect(corrupt.resumeOnReconnect, isFalse);
    expect(corrupt.error, isNotNull);
  });

  test('enabled reconnect resumes only once after a playing unplug', () async {
    final p = _Playback();
    p.preferences.resumeOnReconnect = true;
    await p.policy.connected();
    expect(p.resumes, 0);
    await p.policy.disconnected();
    await p.policy.connected();
    await p.policy.connected();
    expect(p.resumes, 1);
  });

  test('paused source and disabled unplug never arm reconnect', () async {
    final p = _Playback();
    p.preferences.resumeOnReconnect = true;
    p.playing = false;
    await p.policy.disconnected();
    await p.policy.connected();
    expect(p.resumes, 0);
    p.playing = true;
    p.preferences.pauseOnDisconnect = false;
    await p.policy.disconnected();
    await p.policy.connected();
    expect(p.playing, isTrue);
    expect(p.pauses, 1);
    expect(p.resumes, 0);
  });

  test('manual pause/source/sleep cancellation prevents reconnect resume',
      () async {
    final p = _Playback();
    p.preferences.resumeOnReconnect = true;
    await p.policy.disconnected();
    p.policy.cancelResume();
    await p.policy.connected();
    expect(p.resumes, 0);
  });

  test('lost ownership at reconnect prevents resume', () async {
    final p = _Playback();
    p.preferences.resumeOnReconnect = true;
    await p.policy.disconnected();
    p.owned = false;
    await p.policy.connected();
    expect(p.resumes, 0);
  });

  test('transient focus loss pauses and resumes an owned playing source',
      () async {
    final p = _Playback();
    await p.policy
        .interrupted(AudioInterruptionEvent(true, AudioInterruptionType.pause));
    expect(p.playing, isFalse);
    await p.policy.interrupted(
        AudioInterruptionEvent(false, AudioInterruptionType.pause));
    expect(p.resumes, 1);
  });

  for (final cancel in ['manual', 'unplug', 'lock', 'unknown']) {
    test('$cancel prevents focus resume', () async {
      final p = _Playback();
      await p.policy.interrupted(
          AudioInterruptionEvent(true, AudioInterruptionType.pause));
      if (cancel == 'manual') p.policy.cancelResume();
      if (cancel == 'unplug') await p.policy.disconnected();
      if (cancel == 'lock') p.owned = false;
      await p.policy.interrupted(AudioInterruptionEvent(
          false,
          cancel == 'unknown'
              ? AudioInterruptionType.unknown
              : AudioInterruptionType.pause));
      expect(p.resumes, 0);
    });
  }

  test('connect during a call cancels old unplug resume', () async {
    final p = _Playback();
    p.preferences.resumeOnReconnect = true;
    await p.policy.disconnected();
    await p.policy
        .interrupted(AudioInterruptionEvent(true, AudioInterruptionType.pause));
    await p.policy.connected();
    await p.policy.interrupted(
        AudioInterruptionEvent(false, AudioInterruptionType.pause));
    expect(p.resumes, 0);
  });

  test('permanent focus loss cannot resume on a later focus gain', () async {
    final p = _Playback();
    await p.policy.interrupted(
        AudioInterruptionEvent(true, AudioInterruptionType.unknown));
    await p.policy.interrupted(
        AudioInterruptionEvent(false, AudioInterruptionType.pause));
    expect(p.resumes, 0);
    expect(p.policy.allowsAutomaticPlayback, isFalse);
    p.policy.userPlaybackIntent();
    p.playing = true;
    p.preferences.resumeOnReconnect = true;
    await p.policy.disconnected();
    await p.policy.connected();
    expect(p.resumes, 1);
  });

  test('duck preserves music volume and restores only its own game attenuation',
      () async {
    final p = _Playback();
    final begin = AudioInterruptionEvent(true, AudioInterruptionType.duck);
    final end = AudioInterruptionEvent(false, AudioInterruptionType.duck);
    await p.policy.interrupted(begin);
    await p.policy.interrupted(end);
    expect(p.playing, isTrue);
    expect(p.volume, .8);
    await p.policy.interrupted(begin, duckGameAudio: true);
    expect(p.volume, .4);
    await p.policy.interrupted(end);
    expect(p.volume, .8);
    await p.policy.interrupted(begin, duckGameAudio: true);
    p.volume = .25;
    await p.policy.interrupted(end);
    expect(p.volume, .25);
    expect(p.pauses, 0);
  });

  test('connection filter excludes speakers, microphones and SCO call routes',
      () {
    for (final type in AudioDeviceType.values) {
      final device = AudioDevice(
          id: type.name,
          name: type.name,
          isInput: false,
          isOutput: true,
          type: type);
      expect(
          HeadsetPolicy.isHeadphoneOutput(device),
          {
            AudioDeviceType.wiredHeadphones,
            AudioDeviceType.wiredHeadset,
            AudioDeviceType.bluetoothA2dp
          }.contains(type));
    }
    expect(
        HeadsetPolicy.isHeadphoneOutput(AudioDevice(
            id: 'input',
            name: 'input',
            isInput: true,
            isOutput: false,
            type: AudioDeviceType.wiredHeadset)),
        isFalse);
  });
}
