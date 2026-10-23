import 'dart:async';

import 'package:audio_session/audio_session.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../services/headset_preferences.dart';

/// The composition root supplies the same preference owner to native players.
final headsetPreferencesProvider = Provider<HeadsetPreferences>((ref) {
  final preferences = HeadsetPreferences();
  unawaited(preferences.restore());
  ref.onDispose(preferences.dispose);
  return preferences;
});

final headsetAudioSessionProvider =
    Provider<Future<AudioSession>>((ref) => AudioSession.instance);
