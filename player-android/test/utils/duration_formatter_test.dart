import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/utils/duration_formatter.dart';

void main() {
  group('formatDuration', () {
    test('formats minutes and hours without leading zeros', () {
      expect(formatDuration(30), '0:30');
      expect(formatDuration(210.5), '3:30');
      expect(formatDuration(7320), '2:02:00');
    });
  });

  group('mediaDurationLabel', () {
    test('shows durations for audio and video', () {
      expect(mediaDurationLabel('audio', 210), '3:30');
      expect(mediaDurationLabel('video', 7320), '2:02:00');
    });

    test('hides still images, including their tiny probe duration', () {
      expect(hasShownDuration('image', 0.04), isFalse);
      expect(mediaDurationLabel('image', 12), isNull);
    });

    test('hides unknown, zero and negative durations', () {
      expect(mediaDurationLabel('audio', null), isNull);
      expect(mediaDurationLabel('audio', 0), isNull);
      expect(mediaDurationLabel('video', -1), isNull);
    });
  });
}
