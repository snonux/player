import 'dart:async';
import 'package:just_audio/just_audio.dart';
import 'audio_handler.dart';
import 'music_queue.dart';
import 'playback_request.dart';
import 'playback_session_coordinator.dart';

/// Rebind sources through the normal recovery API, retaining occurrence/order
/// semantics. Only the source variant changes when entering offline playback.
Future<int> restoreAutomationQueueSources(
  PlayerAudioHandler handler,
  List<PlaybackRequest> sources, {
  required bool Function() current,
  required Future<PlaybackSessionLease?> Function(PlaybackRequest) claim,
  required Future<AudioSource?> Function(PlaybackRequest, bool Function()) load,
}) async {
  final state = handler.musicQueue, snapshot = handler.queueSnapshot;
  if (sources.length != state.entries.length || !current()) {
    throw StateError('Music queue changed. Retry.');
  }
  final position = state.active
      ? handler.player.position.inMilliseconds / 1000
      : state.position;
  final stopping = handler.stop();
  final transport = handler.transportRevision;
  await stopping;
  if (!current() || handler.transportRevision != transport) {
    throw StateError('Music action superseded. Retry.');
  }
  handler.restoreMusicQueue(
      entries: [
        for (var i = 0; i < sources.length; i++)
          MusicQueueEntry(state.entries[i].id, sources[i],
              autoplayRelated: state.entries[i].autoplayRelated)
      ],
      index: state.index,
      label: state.label,
      position: position,
      upNext: snapshot.upNext,
      repeat: snapshot.repeat,
      shuffle: snapshot.shuffle,
      shuffleMode: snapshot.shuffleMode,
      canonical: snapshot.canonical,
      claim: claim,
      load: load);
  handler.setQueueAutoplay(state.autoplayEnabled);
  return transport;
}

/// Explicit reshuffle is idempotent as a mode, never a toggle. Play on an
/// active paused queue keeps the current source and position.
Future<void> startAutomationQueue(PlayerAudioHandler handler,
    {required bool shuffle,
    required bool Function() current,
    void Function(int revision)? onResume}) async {
  final transport = handler.transportRevision;
  if (shuffle) await handler.shuffleQueue(reshuffle: true, mode: 'tracks');
  if (!current() || transport != handler.transportRevision) {
    throw StateError('Music action superseded. Retry.');
  }
  if (handler.musicQueue.active && handler.musicState?.error == null) {
    unawaited(handler.play());
    onResume?.call(handler.transportRevision);
  } else {
    await handler.resumeQueue();
  }
}
