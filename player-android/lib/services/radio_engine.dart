import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:just_audio/just_audio.dart';

import '../models/radio_station.dart';
import 'audio_handler.dart';
import 'playback_request.dart';
import 'playback_session_coordinator.dart';
import 'radio_controller.dart';

/// Longest wait for a station's stream to load before it counts as failed.
const radioLoadTimeout = Duration(seconds: 20);

/// Plays radio through the shared background [PlayerAudioHandler], so radio
/// gets the media notification, lock-screen controls and audio focus like
/// library audio, and claims the [PlaybackSessionCoordinator] so starting a
/// station stops library playback (and library playback stops radio).
class AudioHandlerRadioEngine implements RadioPlaybackEngine {
  AudioHandlerRadioEngine({
    required PlayerAudioHandler handler,
    required PlaybackSessionCoordinator coordinator,
    required String Function(String path) urlFor,
    required Future<Map<String, String>> Function(Uri uri) headersFor,
  })  : _handler = handler,
        _coordinator = coordinator,
        _urlFor = urlFor,
        _headersFor = headersFor;

  final PlayerAudioHandler _handler;
  final PlaybackSessionCoordinator _coordinator;
  final String Function(String) _urlFor;
  final Future<Map<String, String>> Function(Uri) _headersFor;

  int _attempt = 0;
  PlaybackSessionLease? _lease;
  int? _sourceGeneration;

  bool get _active =>
      _lease?.isCurrent == true &&
      _sourceGeneration != null &&
      _handler.isSourceSessionCurrent(_sourceGeneration!);

  @override
  Future<void> start(
    RadioStation station, {
    required VoidCallback onStoppedElsewhere,
  }) async {
    final attempt = ++_attempt;
    _sourceGeneration = null;
    final uri = Uri.parse(_urlFor(station.streamUrl));
    final lease = await _coordinator.claim(
      kind: PlaybackSourceKind.server,
      identity: 'radio:${uri.origin}:${station.uuid}',
      sourceUri: uri.toString(),
      stop: () {
        // Our own start/stop bumps the attempt first, so this only reports
        // takeovers by something else.
        if (attempt == _attempt) {
          _attempt++;
          _lease = null;
          onStoppedElsewhere();
        }
        return _handler.stop();
      },
    );
    if (lease == null || attempt != _attempt) {
      await lease?.release();
      throw StateError('superseded');
    }
    _lease = lease;
    final generation =
        _handler.beginSourceSession(ownsSource: () => lease.isCurrent);
    final headers = await _headersFor(uri);
    if (attempt != _attempt || !lease.isCurrent) throw StateError('superseded');
    try {
      await _handler
          .loadSourceForSession(
            AudioSource.uri(uri, headers: headers.isEmpty ? null : headers),
            generation,
          )
          .timeout(radioLoadTimeout);
    } catch (_) {
      if (attempt == _attempt && lease.isCurrent) {
        // Leave the player idle instead of retrying a dead stream.
        _attempt++;
        _lease = null;
        await lease.stop();
      }
      rethrow;
    }
    if (attempt != _attempt || !_handler.activateSourceSession(generation)) {
      throw StateError('superseded');
    }
    _sourceGeneration = generation;
    _handler.setLiveMediaItem(
      id: 'radio:${station.uuid}',
      title: station.name,
      artist: station.summary.isEmpty ? 'Web radio' : station.summary,
    );
    // AudioPlayer.play completes only when playback pauses or stops.
    unawaited(_handler.play());
  }

  @override
  Future<void> pause() async {
    if (_active) await _handler.pause();
  }

  @override
  Future<bool> resume() async {
    if (!_active) return false;
    unawaited(_handler.play());
    return true;
  }

  @override
  Future<void> stop() async {
    _attempt++;
    final lease = _lease;
    _lease = null;
    _sourceGeneration = null;
    await lease?.stop();
  }

  @override
  Stream<bool> get playing => _handler.player.playerStateStream
      .map((state) =>
          _active &&
          state.playing &&
          state.processingState == ProcessingState.ready)
      .distinct();

  @override
  Stream<Object> get errors =>
      _handler.playbackErrors.where((_) => _lease?.isCurrent == true);
}
