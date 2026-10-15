import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/models/lyrics.dart';

void main() {
  test('fractional timestamps track forwards and backwards, including ties',
      () {
    final lyrics = Lyrics.fromJson({
      'source': 'embedded',
      'synced': true,
      'lines': [
        {'text': 'first', 'start': 1.25},
        {'text': 'second', 'start': 3},
        {'text': 'together', 'start': 3},
      ],
    });
    expect(lyrics.source, 'embedded');
    expect(lyrics.synced, isTrue);
    expect(lyrics.currentLine(Duration.zero), -1);
    expect(lyrics.currentLine(const Duration(milliseconds: 1250)), 0);
    expect(lyrics.currentLine(const Duration(seconds: 10)), 2);
    expect(lyrics.currentLine(const Duration(seconds: 2)), 0);
  });

  test('plain, empty and malformed responses remain readable and never seek',
      () {
    for (final data in [
      null,
      [],
      {},
      {'lines': 'wrong'},
      {'lines': []}
    ]) {
      final lyrics = Lyrics.fromJson(data);
      expect(lyrics.lines, isEmpty);
      expect(lyrics.currentLine(Duration.zero), -1);
    }
    for (final invalidTime in [
      -1,
      double.nan,
      double.infinity,
      1e99,
      '1',
      null
    ]) {
      final lyrics = Lyrics.fromJson({
        'synced': true,
        'lines': [
          null,
          {'text': 42},
          {'text': 'still readable', 'start': invalidTime}
        ],
      });
      expect(lyrics.synced, isFalse);
      expect(lyrics.lines.single.text, 'still readable');
      expect(lyrics.lines.single.start, isNull);
    }
    final unordered = Lyrics.fromJson({
      'synced': true,
      'lines': [
        {'text': 'a', 'start': 2},
        {'text': 'b', 'start': 1}
      ],
    });
    expect(unordered.synced, isFalse);
    expect(unordered.currentLine(const Duration(seconds: 3)), -1);
  });
}
