import 'dart:async';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter/services.dart';
import 'package:just_audio/just_audio.dart';
import 'package:player_android/services/loudness_effect.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:player_android/models/music_metadata.dart';
import 'package:player_android/services/replay_gain.dart';

class _Player extends AudioPlayer {
  double level = 0.6;
  bool active = true;
  int transportCalls = 0;
  int? session;
  final sessions = StreamController<int?>.broadcast(sync: true);
  @override
  int? get androidAudioSessionId => session;
  @override
  Stream<int?> get androidAudioSessionIdStream => sessions.stream;
  @override
  double get volume => level;
  @override
  Future<void> setVolume(double value) async {
    level = value;
  }

  @override
  bool get playing => active;
  @override
  Future<void> play() async {
    transportCalls++;
    active = true;
  }

  @override
  Future<void> pause() async {
    transportCalls++;
    active = false;
  }

  @override
  Future<void> stop() async {
    transportCalls++;
    active = false;
  }
}

class _Enhancer extends LoudnessEffect {
  bool active = false, fail = false;
  double gain = 0;
  @override
  bool get enabled => active;
  @override
  Future<void> setEnabled(bool value) async {
    active = value;
  }

  @override
  Future<void> setTargetGain(double value) async {
    if (fail) throw StateError('Effect unavailable');
    gain = value;
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  final music = MusicMetadata.fromJson({
    'album': 'A',
    'album_artist': 'Artist',
    'replaygain_track_gain_db': -8,
    'replaygain_album_gain_db': -4,
    'replaygain_track_peak': 0.5
  });
  test('defaults, modes and device settings bounds', () {
    expect(LoudnessSettings.fromJson({}).mode, LoudnessMode.off);
    expect(LoudnessSettings.fromJson({'preamp': 90}).preamp, 12);
    expect(calculateReplayGain(music, const LoudnessSettings()).db, 0);
    expect(
        calculateReplayGain(music,
                const LoudnessSettings(mode: LoudnessMode.track, preamp: 2))
            .db,
        -6);
    expect(
        calculateReplayGain(
                music, const LoudnessSettings(mode: LoudnessMode.automatic),
                albumContext: true)
            .db,
        -4);
    expect(
        calculateReplayGain(
                music, const LoudnessSettings(mode: LoudnessMode.automatic))
            .db,
        -8);
  });
  test('boost capped by known peak; unknown peak and untagged stay safe', () {
    final boost = MusicMetadata.fromJson(
        {'replaygain_track_gain_db': 12, 'replaygain_track_peak': 0.5});
    expect(
        calculateReplayGain(
                boost, const LoudnessSettings(mode: LoudnessMode.track))
            .db,
        closeTo(6.020599913, 0.00001));
    expect(
        calculateReplayGain(
                MusicMetadata.fromJson({'replaygain_track_gain_db': 9}),
                const LoudnessSettings(mode: LoudnessMode.track))
            .db,
        0);
    expect(
        calculateReplayGain(null,
                const LoudnessSettings(mode: LoudnessMode.track, untagged: 12))
            .db,
        0);
    expect(
        calculateReplayGain(
                null,
                const LoudnessSettings(
                    mode: LoudnessMode.album, untagged: -5, preamp: 12))
            .db,
        -5);
  });
  test('automatic album identity needs multiple identified matching songs', () {
    expect(sameLoudnessAlbum([music, music]), true);
    expect(sameLoudnessAlbum([music]), false);
    expect(sameLoudnessAlbum([null, null]), false);
    expect(
        sameLoudnessAlbum([
          music,
          MusicMetadata.fromJson({'album': 'A', 'album_artist': 'Other'})
        ]),
        false);
  });
  test(
      'native stage preserves user volume, resets boost and persists device preferences',
      () async {
    SharedPreferences.setMockInitialValues({});
    final player = _Player(), enhancer = _Enhancer();
    final controller = ReplayGainController(player, enhancer);
    await controller.setTrack(music, albumContext: false);
    await controller.update(const LoudnessSettings(mode: LoudnessMode.track));
    expect(player.level, closeTo(0.6 * 0.39810717, 0.000001));
    await controller.update(const LoudnessSettings());
    expect(player.level, closeTo(0.6, 0.000001));
    final boost = MusicMetadata.fromJson(
        {'replaygain_track_gain_db': 3, 'replaygain_track_peak': 0.5});
    await controller.setTrack(boost, albumContext: false);
    await controller.update(const LoudnessSettings(mode: LoudnessMode.track));
    expect(enhancer.enabled, true);
    expect(enhancer.gain, 3);
    expect(player.level, closeTo(0.6, 0.000001));
    await controller.setTrack(null, albumContext: false);
    expect(enhancer.enabled, false);
    final restored = ReplayGainController(_Player(), _Enhancer());
    await restored.restore();
    expect(restored.settings.mode, LoudnessMode.track);
  });
  test(
      'effect failure recovers playback volume and newer gain updates finish last',
      () async {
    final player = _Player(), enhancer = _Enhancer()..fail = true;
    final controller = ReplayGainController(player, enhancer);
    await controller.setTrack(
        MusicMetadata.fromJson(
            {'replaygain_track_gain_db': 3, 'replaygain_track_peak': 0.5}),
        albumContext: false);
    await controller.update(const LoudnessSettings(mode: LoudnessMode.track));
    expect(controller.status, contains('unavailable'));
    expect(player.level, closeTo(0.6, 0.000001));
    expect(enhancer.enabled, false);
    final first = controller.setTrack(music, albumContext: false);
    final second = controller.update(const LoudnessSettings());
    await Future.wait([first, second]);
    expect(player.level, closeTo(0.6, 0.000001));
    expect(enhancer.enabled, false);
    expect(controller.status, 'Loudness off.');
  });
  test(
      'automatic album identity follows canonical releases, compilations and normalized text',
      () {
    const releaseA = '12345678-abcd-1234-abcd-123456789abc';
    const releaseB = '12345678-abcd-1234-abcd-123456789abd';
    MusicMetadata song(Map<String, dynamic> json) =>
        MusicMetadata.fromJson(json);
    expect(
        sameLoudnessAlbum([
          song({...music.toJson(), 'release_mbid': releaseA}),
          song({...music.toJson(), 'release_mbid': releaseB})
        ]),
        false);
    expect(
        sameLoudnessAlbum([
          song({'release_mbid': releaseA}),
          song({'release_mbid': ' ${releaseA.toUpperCase()} '})
        ]),
        true);
    expect(
        sameLoudnessAlbum([
          song({'album': 'Compilation', 'compilation': true, 'artist': 'One'}),
          song({'album': 'Compilation', 'compilation': true, 'artist': 'Two'})
        ]),
        true);
    expect(
        sameLoudnessAlbum([
          song({'album': ' A  Name ', 'album_artist': ' ARTIST  Name '}),
          song({'album': 'a name', 'album_artist': 'artist name'})
        ]),
        true);
    expect(
        sameLoudnessAlbum([
          song({'album': 'A', 'release_mbid': 'invalid'}),
          song({'album': 'A', 'release_mbid': 'invalid'})
        ]),
        false);
  });
  test('native bridge never allocates Off and follows existing player sessions',
      () async {
    const channel = MethodChannel('test/loudness');
    final calls = <Map<dynamic, dynamic>>[];
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
      calls.add(call.arguments as Map);
      return null;
    });
    addTearDown(() => TestDefaultBinaryMessengerBinding
        .instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null));
    final player = _Player();
    final effect = SessionLoudnessEffect(player, channel: channel);
    final controller = ReplayGainController(player, effect);
    addTearDown(player.sessions.close);
    addTearDown(effect.close);
    await controller.apply();
    expect(calls, isEmpty);
    await controller.setTrack(
        MusicMetadata.fromJson(
            {'replaygain_track_gain_db': 6, 'replaygain_track_peak': 0.5}),
        albumContext: false);
    await controller.update(const LoudnessSettings(mode: LoudnessMode.track));
    expect(calls.last['gainMb'], 0);
    player.session = 11;
    player.sessions.add(11);
    await controller.apply();
    expect(calls.last, {'sessionId': 11, 'gainMb': 600});
    player.session = 12;
    player.sessions.add(12);
    await controller.apply();
    expect(calls.last, {'sessionId': 12, 'gainMb': 600});
    expect(calls.any((call) => call['sessionId'] == 12 && call['gainMb'] == 0),
        true);
    await controller.update(const LoudnessSettings());
    expect(calls.last['gainMb'], 0);
    final before = calls.length;
    player.session = null;
    player.sessions.add(null);
    await controller.apply();
    expect(calls.length, before);
  });
  test('native effect allocation error restores unity without failing playback',
      () async {
    const channel = MethodChannel('test/loudness-failure');
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
      if ((call.arguments as Map)['gainMb'] > 0) {
        throw PlatformException(code: 'loudness_unavailable');
      }
      return null;
    });
    addTearDown(() => TestDefaultBinaryMessengerBinding
        .instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null));
    final player = _Player()..session = 11;
    final effect = SessionLoudnessEffect(player, channel: channel);
    final controller = ReplayGainController(player, effect);
    addTearDown(player.sessions.close);
    addTearDown(effect.close);
    await controller.setTrack(
        MusicMetadata.fromJson(
            {'replaygain_track_gain_db': 3, 'replaygain_track_peak': 0.5}),
        albumContext: false);
    await controller.update(const LoudnessSettings(mode: LoudnessMode.track));
    expect(controller.status, contains('unavailable'));
    expect(player.level, 0.6);
    expect(effect.enabled, false);
  });
  test(
      'older Android reports fixed positive gain capability and keeps playback',
      () async {
    const channel = MethodChannel('test/loudness-old-android');
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
      if ((call.arguments as Map)['gainMb'] > 0) {
        throw PlatformException(
            code: 'loudness_unsupported_android',
            message: 'arbitrary native details must not enter the UI');
      }
      return null;
    });
    addTearDown(() => TestDefaultBinaryMessengerBinding
        .instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null));
    final player = _Player()..session = 11;
    final effect = SessionLoudnessEffect(player, channel: channel);
    final controller = ReplayGainController(player, effect);
    addTearDown(player.sessions.close);
    addTearDown(effect.close);
    await controller.setTrack(
        MusicMetadata.fromJson(
            {'replaygain_track_gain_db': 3, 'replaygain_track_peak': 0.5}),
        albumContext: false);
    await controller.update(const LoudnessSettings(mode: LoudnessMode.track));
    expect(controller.status,
        'Positive loudness gain requires Android 9 or later. Attenuation remains available.');
    expect(player.level, 0.6);
    expect(effect.enabled, false);
    expect(player.playing, true);
    expect(player.transportCalls, 0);
    await controller.setTrack(music, albumContext: false);
    expect(player.level, closeTo(0.6 * 0.39810717, 0.000001));
    expect(controller.status, startsWith('Track tags'));
    expect(player.playing, true);
    expect(player.transportCalls, 0);
  });
}
