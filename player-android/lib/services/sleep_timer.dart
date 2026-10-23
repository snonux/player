import 'dart:async';
import 'package:flutter/foundation.dart';

/// Session-only countdown, independent of screens and wall-clock changes.
class SleepTimer extends ChangeNotifier {
  SleepTimer({required this.onExpire, Duration Function()? now}) : _now = now;
  final Future<void> Function() onExpire;
  final Stopwatch _clock = Stopwatch()..start();
  final Duration Function()? _now;
  Duration _startedAt = Duration.zero;
  Duration get _elapsed => _now?.call() ?? _clock.elapsed;
  Duration? _duration;
  Timer? _ticker;
  bool expired = false;
  String? errorMessage;
  int revision = 0;

  Duration? get remaining {
    final duration = _duration;
    if (duration == null) return null;
    final left = duration - (_elapsed - _startedAt);
    return left.isNegative ? Duration.zero : left;
  }

  void start(Duration duration) {
    if (duration <= Duration.zero) return;
    cancel();
    expired = false;
    errorMessage = null;
    _duration = duration;
    _startedAt = _elapsed;
    _ticker = Timer.periodic(const Duration(milliseconds: 250), (_) => check());
    notifyListeners();
  }

  void check() {
    if (_duration == null) return;
    if (remaining == Duration.zero) {
      cancel();
      expired = true;
      revision++;
      notifyListeners();
      unawaited(onExpire().catchError((Object error) {
        errorMessage =
            'Could not pause playback. Check the player or cast device.';
        notifyListeners();
      }));
    } else {
      notifyListeners();
    }
  }

  void cancel() {
    _ticker?.cancel();
    _ticker = null;
    _duration = null;
    notifyListeners();
  }

  /// Capture at the start of an explicit selection/play, before async work.
  int beginPlaybackIntent() {
    check();
    revision++;
    expired = false;
    errorMessage = null;
    notifyListeners();
    return revision;
  }

  bool allowsPlayback(int intent) {
    check();
    return intent == revision;
  }

  void resume() {
    beginPlaybackIntent();
  }

  @override
  void dispose() {
    _ticker?.cancel();
    super.dispose();
  }
}
