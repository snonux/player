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
  test(
      'rich intervals, translations and voices recompute after forward and back seeks',
      () {
    final lyrics = Lyrics.fromJson({
      'language': 'en',
      'synced': true,
      'lines': [
        {
          'text': 'Hello world',
          'start': 1,
          'end': 5,
          'voice': 'Lead',
          'translations': {'fr': 'Bonjour monde'},
          'words': [
            {'text': 'Hello ', 'start': 1, 'end': 2},
            {'text': 'world', 'start': 2, 'end': 5}
          ]
        },
        {'text': 'Harmony', 'start': 1, 'end': 3, 'voice': 'Harmony'},
        {'text': 'Next', 'start': 6},
      ],
    });
    expect(lyrics.language, 'en');
    expect(lyrics.languages, ['fr']);
    expect(lyrics.activeLines(const Duration(milliseconds: 2500)), {0, 1});
    expect(lyrics.activeLines(const Duration(seconds: 4)), {0});
    expect(lyrics.activeLines(const Duration(seconds: 5)), isEmpty);
    expect(lyrics.activeLines(const Duration(seconds: 1)), {0, 1});
    expect(lyrics.lines[0].currentWord(const Duration(milliseconds: 2500)), 1);
    expect(lyrics.lines[0].currentWord(const Duration(milliseconds: 1500)), 0);
    expect(lyrics.lines[0].currentWord(const Duration(seconds: 5)), -1);
    expect(lyrics.lines[0].displayText('fr'), 'Bonjour monde');
    expect(lyrics.lines[1].displayText('fr'), 'Harmony');
  });

  test('invalid optional rich metadata leaves complete readable lines', () {
    for (final fields in [
      {
        'words': [
          {'text': 'Hello world', 'start': -1}
        ]
      },
      {
        'words': [
          {'text': 'wrong text', 'start': 1}
        ]
      },
      {
        'words': [
          {'text': 'Hello world', 'start': 1, 'end': 'bad'}
        ]
      },
      {
        'words': [
          {'text': 'Hello ', 'start': 3},
          {'text': 'world', 'start': 2}
        ]
      },
      {
        'words': 'bad',
        'end': -1,
        'voice': [],
        'translations': {'fr': []}
      },
    ]) {
      final lyrics = Lyrics.fromJson({
        'synced': true,
        'lines': [
          {'text': 'Hello world', 'start': 1, ...fields}
        ]
      });
      expect(lyrics.synced, isTrue);
      expect(lyrics.lines.single.text, 'Hello world');
      expect(lyrics.lines.single.words, isEmpty);
    }
  });
}
