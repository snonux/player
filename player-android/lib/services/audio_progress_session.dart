import 'dart:async';

/// Reports one audio item's position independently of any player route.
/// The owning media handler keeps this session alive during background audio.
class AudioProgressSession {
  AudioProgressSession({
    required this.position,
    required this.duration,
    required this.playing,
    required this.savePosition,
    required this.markFinished,
    this.interval = const Duration(seconds: 5),
    this.naturalEndOnly = false,
    this.finishAtPosition,
  }) {
    _timer = Timer.periodic(interval, (_) => unawaited(record()));
  }

  final Duration Function() position;
  final Duration? Function() duration;
  final bool Function() playing;
  final Future<void> Function(double) savePosition;
  final Future<void> Function() markFinished;
  final Duration interval;
  final bool naturalEndOnly;
  final Future<void> Function(double)? finishAtPosition;
  bool get finished => _finishedEmitted;

  late final Timer _timer;
  Future<void> _pending = Future.value();
  int? _lastPositionMs;
  bool _finishedEmitted = false;
  bool _completionReceived = false;
  bool _closed = false;

  /// A regular tick skips paused audio; transitions force one final sample.
  Future<void> record(
      {bool force = false,
      bool completed = false,
      Duration? positionOverride,
      Duration? durationOverride}) {
    if (_closed || (!force && !playing())) return _pending;
    if (completed) _completionReceived = true;
    final elapsed = positionOverride ?? position();
    final total = durationOverride ?? duration();
    final elapsedMs = elapsed.inMilliseconds;
    if (elapsedMs < 0) return _pending;

    final reachedFinish = _completionReceived ||
        (!naturalEndOnly &&
            total != null &&
            total.inMilliseconds > 0 &&
            elapsedMs / total.inMilliseconds >= 0.95);

    // Serialize snapshots: an older in-flight request must never be sent
    // after a later pause/stop position for the same media item.
    _pending = _pending.then((_) async {
      // A later position would reset the server's finished flag, so the
      // durable completion command must be this session's final write.
      if (_finishedEmitted) return;
      if (reachedFinish && finishAtPosition != null) {
        try {
          await finishAtPosition!((total ?? elapsed).inMilliseconds / 1000.0);
          _finishedEmitted = true;
        } catch (_) {}
        return;
      }
      if (elapsedMs != _lastPositionMs) {
        try {
          await savePosition(elapsedMs / 1000.0);
          _lastPositionMs = elapsedMs;
        } catch (_) {}
      }
      if (reachedFinish && !_finishedEmitted) {
        try {
          await markFinished();
          _finishedEmitted = true;
        } catch (_) {}
      }
    });
    return _pending;
  }

  /// Cancels future ticks and saves the final position before source changes.
  Future<void> close({bool completed = false, Duration? finalPosition}) async {
    if (_closed) return _pending;
    _timer.cancel();
    final pending = record(
        force: true,
        completed: completed,
        positionOverride: finalPosition,
        durationOverride: naturalEndOnly && !completed ? null : finalPosition);
    _closed = true;
    await pending;
    await _pending;
  }
}
