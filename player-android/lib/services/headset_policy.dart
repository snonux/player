// Device enumeration is experimental in the pinned audio_session version.
// ignore_for_file: experimental_member_use

import 'dart:async';

import 'package:audio_session/audio_session.dart';
import 'package:flutter/foundation.dart';
import 'headset_preferences.dart';

/// Native output policy. Reconnect can only resume a source paused by unplug;
/// it never starts an unopened file, a restored queue, or a new account.
class HeadsetPolicy extends ChangeNotifier {
  HeadsetPolicy({
    required this.preferences,
    required this.isPlaying,
    required this.canResume,
    required this.pausePlayback,
    required this.resumePlayback,
    required this.volume,
    required this.setVolume,
  }) {
    preferences.addListener(cancelResume);
  }

  final HeadsetPreferences preferences;
  final bool Function() isPlaying, canResume;
  final Future<void> Function() pausePlayback, resumePlayback;
  final double Function() volume;
  final Future<void> Function(double) setVolume;
  String? error;
  bool _unplugResume = false, _focusResume = false, _interrupted = false;
  bool _blocked = false;
  bool _disposed = false;
  bool get allowsAutomaticPlayback => !_blocked;
  double? _duckVolume;
  final _subscriptions = <StreamSubscription<dynamic>>[];

  /// Called before any explicit transport action, source change or timer expiry.
  void cancelResume() {
    _unplugResume = false;
    _focusResume = false;
  }

  void userPlaybackIntent() {
    cancelResume();
    _blocked = false;
    // Explicit Play requests fresh focus through just_audio. Permanent loss
    // need not deliver an end event, so its old latch must not survive it.
    _interrupted = false;
  }

  void suspend() {
    cancelResume();
    _blocked = true;
  }

  Future<void> disconnected() async {
    _focusResume = false;
    if (!preferences.pauseOnDisconnect) return;
    _blocked = true;
    if (isPlaying()) {
      _unplugResume =
          preferences.resumeOnReconnect && !_interrupted && canResume();
    }
    await pausePlayback();
  }

  Future<void> connected() async {
    final resume = _unplugResume;
    _unplugResume = false;
    if (resume &&
        preferences.resumeOnReconnect &&
        !_interrupted &&
        canResume()) {
      _blocked = false;
      await resumePlayback();
    }
  }

  /// Keeps just_audio's focus semantics: duck leaves music playing; transient
  /// loss pauses and may resume, permanent/unknown loss never auto-resumes.
  Future<void> interrupted(AudioInterruptionEvent event,
      {bool duckGameAudio = false}) async {
    if (event.begin) {
      _interrupted = true;
      _unplugResume = false;
      _focusResume = false;
      if (event.type == AudioInterruptionType.duck) {
        if (duckGameAudio && _duckVolume == null) {
          _duckVolume = volume();
          await setVolume(_duckVolume! / 2);
        }
      } else {
        _blocked = true;
        _focusResume = event.type == AudioInterruptionType.pause &&
            isPlaying() &&
            canResume();
        await pausePlayback();
      }
    } else {
      _interrupted = false;
      final resume = _focusResume;
      _focusResume = false;
      if (event.type == AudioInterruptionType.duck) {
        final original = _duckVolume;
        _duckVolume = null;
        // A volume/gain change made during ducking belongs to the user or
        // loudness controller; do not overwrite it when focus returns.
        if (original != null && volume() == original / 2) {
          await setVolume(original);
        }
      } else if (event.type == AudioInterruptionType.pause &&
          resume &&
          canResume()) {
        _blocked = false;
        await resumePlayback();
      }
    }
  }

  void attach(AudioSession session) {
    _subscriptions.add(
        session.becomingNoisyEventStream.listen((_) => _run(disconnected())));
    _subscriptions.add(session.interruptionEventStream.listen((event) => _run(
        interrupted(event,
            duckGameAudio: session.androidAudioAttributes?.usage ==
                AndroidAudioUsage.game))));
    // Changes only: existing devices at startup never trigger playback.
    _subscriptions.add(session.devicesChangedEventStream.listen((event) {
      if (event.devicesAdded.any(isHeadphoneOutput)) _run(connected());
    }));
  }

  static bool isHeadphoneOutput(AudioDevice device) =>
      device.isOutput &&
      {
        AudioDeviceType.wiredHeadset,
        AudioDeviceType.wiredHeadphones,
        AudioDeviceType.bluetoothA2dp
      }.contains(device.type);

  void _run(Future<void> operation) {
    unawaited(operation.catchError((Object _) {
      if (_disposed) return;
      cancelResume();
      error = 'Headphone action failed. Use the player controls to try again.';
      notifyListeners();
    }));
  }

  @override
  void dispose() {
    _disposed = true;
    suspend();
    preferences.removeListener(cancelResume);
    for (final subscription in _subscriptions) {
      unawaited(subscription.cancel());
    }
    super.dispose();
  }
}
