import 'dart:convert';
import 'dart:math' as math;

import 'package:flutter/foundation.dart';
import 'package:just_audio/just_audio.dart';
import 'loudness_effect.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../models/music_metadata.dart';

enum LoudnessMode { off, track, album, automatic }

class LoudnessSettings {
  final LoudnessMode mode;
  final double preamp, untagged;
  const LoudnessSettings(
      {this.mode = LoudnessMode.off, this.preamp = 0, this.untagged = 0});
  factory LoudnessSettings.fromJson(Map<String, dynamic> json) {
    double bounded(dynamic n) =>
        n is num && n.isFinite ? n.toDouble().clamp(-12, 12) : 0;
    return LoudnessSettings(
        mode: LoudnessMode.values
                .where((m) => m.name == json['mode'])
                .firstOrNull ??
            LoudnessMode.off,
        preamp: bounded(json['preamp']),
        untagged: bounded(json['untagged']));
  }
  Map<String, dynamic> toJson() =>
      {'mode': mode.name, 'preamp': preamp, 'untagged': untagged};
}

bool sameLoudnessAlbum(Iterable<MusicMetadata?> items) {
  String text(dynamic value) => value is String ? value.trim() : '';
  String normalized(dynamic value) =>
      text(value).split(RegExp(r'\s+')).join(' ').toLowerCase();
  final releaseId = RegExp(
      r'^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$');
  final identities = items.map((music) {
    if (music == null) return null;
    final json = music.toJson(), mbid = text(music.toJson()['release_mbid']);
    if (releaseId.hasMatch(mbid)) return 'mbid:${mbid.toLowerCase()}';
    final album = normalized(json['album']);
    final albumArtist = text(json['album_artist']);
    final artist = normalized(albumArtist.isNotEmpty
        ? albumArtist
        : json['compilation'] == true
            ? 'Various Artists'
            : json['artist']);
    // Unidentified/folder-only albums conservatively use track mode.
    return album.isNotEmpty && artist.isNotEmpty
        ? jsonEncode([album, artist])
        : null;
  }).toList();
  return identities.length > 1 &&
      identities.first != null &&
      identities.every((id) => id == identities.first);
}

({double db, String message}) calculateReplayGain(
    MusicMetadata? music, LoudnessSettings settings,
    {bool albumContext = false}) {
  if (settings.mode == LoudnessMode.off) {
    return (db: 0, message: 'Loudness off.');
  }
  final mode = settings.mode == LoudnessMode.automatic
      ? (albumContext ? LoudnessMode.album : LoudnessMode.track)
      : settings.mode;
  final json = music?.toJson();
  final gain = json?['replaygain_${mode.name}_gain_db'],
      peak = json?['replaygain_${mode.name}_peak'];
  final tagged = gain is num && gain.isFinite;
  final knownPeak = peak is num && peak.isFinite && peak > 0;
  final requested =
      tagged ? gain.toDouble() + settings.preamp : settings.untagged;
  final ceiling = tagged && knownPeak ? -20 * math.log(peak) / math.ln10 : 0.0;
  final db = math.min(24.0, math.min(requested, ceiling));
  return (
    db: db,
    message:
        '${tagged ? '${mode == LoudnessMode.album ? 'Album' : 'Track'} tags' : 'No selected gain tag'} · ${db.toStringAsFixed(1)} dB.'
            '${requested > db ? ' Gain limited to avoid clipping.' : ''}${!knownPeak || !tagged ? ' Peak unknown; positive boost disabled.' : ''}'
  );
}

/// Device preferences and the one player's gain stage. Serialized updates keep
/// stale asynchronous effect calls from overriding a newer track or setting.
class ReplayGainController extends ChangeNotifier {
  ReplayGainController(this.player, this.enhancer) {
    enhancer?.onSessionChanged = () {
      apply();
    };
  }
  final AudioPlayer player;
  final LoudnessEffect? enhancer;
  LoudnessSettings settings = const LoudnessSettings();
  MusicMetadata? _music;
  bool _albumContext = false;
  double _attenuation = 1, _baseVolume = 1;
  String status = 'Loudness off.';
  Future<void> _tail = Future.value();
  static const preferenceKey = 'player-loudness-v1';

  Future<void> restore() async {
    try {
      final raw =
          (await SharedPreferences.getInstance()).getString(preferenceKey);
      if (raw != null) {
        settings =
            LoudnessSettings.fromJson(jsonDecode(raw) as Map<String, dynamic>);
      }
    } catch (_) {/* Invalid device preferences use the safe Off default. */}
    await apply();
  }

  Future<void> update(LoudnessSettings value) async {
    settings = LoudnessSettings.fromJson(value.toJson());
    await apply();
    try {
      if (!await (await SharedPreferences.getInstance())
          .setString(preferenceKey, jsonEncode(settings.toJson()))) {
        throw StateError('Preference write failed');
      }
    } catch (_) {
      status += ' Could not save loudness on this device.';
      notifyListeners();
    }
  }

  Future<void> setTrack(MusicMetadata? music, {required bool albumContext}) {
    _music = music;
    _albumContext = albumContext;
    return apply();
  }

  Future<void> apply() {
    _tail = _tail.then((_) async {
      final result =
          calculateReplayGain(_music, settings, albumContext: _albumContext);
      if (result.db == 0 && _attenuation == 1 && enhancer?.enabled != true) {
        status = result.message;
        notifyListeners();
        return;
      }
      final base = _attenuation > 0
          ? (player.volume / _attenuation).clamp(0.0, 1.0)
          : _baseVolume;
      _baseVolume = base;
      try {
        // Disable old boost before changing attenuation or the selected gain.
        await enhancer?.setEnabled(false);
        _attenuation = math.pow(10, math.min(0, result.db) / 20).toDouble();
        await player.setVolume(base * _attenuation);
        if (result.db > 0) {
          if (enhancer == null) throw StateError('Audio effect unavailable');
          await enhancer!.setTargetGain(result.db);
          await enhancer!.setEnabled(true);
        }
        status = result.message;
      } catch (_) {
        // Keep playback available with unity normalization on effect failure.
        try {
          await enhancer?.setEnabled(false);
        } catch (_) {}
        try {
          await player.setVolume(base);
          _attenuation = 1;
        } catch (_) {}
        status =
            'Loudness processing unavailable. Try changing the mode again.';
      }
      notifyListeners();
    });
    return _tail;
  }
}
