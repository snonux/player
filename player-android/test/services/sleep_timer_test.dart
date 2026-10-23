import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/sleep_timer.dart';

void main() {
  test('countdown replaces, cancels, expires once and clears on restart',
      () async {
    var now = Duration.zero;
    var pauses = 0;
    final timer = SleepTimer(
        now: () => now,
        onExpire: () async {
          pauses++;
        });
    expect(timer.remaining, isNull);
    timer.start(const Duration(minutes: 1));
    now = const Duration(seconds: 15);
    expect(timer.remaining, const Duration(seconds: 45));
    timer.start(const Duration(seconds: 10));
    now = const Duration(seconds: 24);
    timer.check();
    expect(pauses, 0);
    now = const Duration(seconds: 25);
    timer.check();
    timer.check();
    expect(pauses, 1);
    expect(timer.expired, isTrue);
    timer.resume();
    expect(timer.expired, isFalse);
    timer.start(const Duration(seconds: 5));
    timer.cancel();
    now = const Duration(hours: 10);
    timer.check();
    expect(pauses, 1);
    timer.dispose();
    final restarted = SleepTimer(onExpire: () async {});
    expect(restarted.remaining, isNull);
    restarted.dispose();
  });
  test(
      'expiry fences pending work while new selection and Cast intents may play',
      () {
    var now = Duration.zero;
    final timer = SleepTimer(now: () => now, onExpire: () async {});
    final sourceA = timer.beginPlaybackIntent();
    timer.start(const Duration(seconds: 1));
    now = const Duration(seconds: 2);
    timer.check();
    expect(timer.allowsPlayback(sourceA), isFalse);
    final sourceB = timer.beginPlaybackIntent();
    expect(timer.allowsPlayback(sourceB), isTrue);
    expect(timer.allowsPlayback(sourceA), isFalse);
    timer.start(const Duration(seconds: 1));
    now = const Duration(seconds: 4);
    timer.check();
    expect(timer.allowsPlayback(sourceB), isFalse);
    final freshCast = timer.beginPlaybackIntent();
    expect(timer.allowsPlayback(freshCast), isTrue);
    timer.start(const Duration(seconds: 5));
    timer.cancel();
    expect(timer.allowsPlayback(freshCast), isTrue);
    expect(timer.allowsPlayback(sourceB), isFalse);
    expect(timer.allowsPlayback(sourceA), isFalse);
    timer.dispose();
  });
}
