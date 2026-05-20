// Unit tests for json_helpers.dart.
//
// `dateTimeFromJson` is the shared deserialization helper used by every
// model that has a nullable DateTime field. These tests pin down its
// defensive contract: only well-formed ISO-8601 strings produce a
// DateTime; everything else (null, non-string, empty string, malformed
// string) silently degrades to null without throwing.
//
// The malformed-string case is the regression guard for the original
// crash bug: prior to the fix, `DateTime.parse('not a date')` propagated
// a FormatException out of fromJson() and crashed the app.

import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/models/json_helpers.dart';

void main() {
  group('dateTimeFromJson', () {
    test('returns null for null input', () {
      expect(dateTimeFromJson(null), isNull);
    });

    test('returns null for empty string', () {
      expect(dateTimeFromJson(''), isNull);
    });

    test('returns null for non-string input (int)', () {
      expect(dateTimeFromJson(12345), isNull);
    });

    test('returns null for non-string input (bool)', () {
      expect(dateTimeFromJson(true), isNull);
    });

    test('parses a valid ISO-8601 UTC string', () {
      final dt = dateTimeFromJson('2026-05-20T12:34:56.000Z');
      expect(dt, isNotNull);
      expect(dt!.toUtc().year, 2026);
      expect(dt.toUtc().month, 5);
      expect(dt.toUtc().day, 20);
      expect(dt.toUtc().hour, 12);
      expect(dt.toUtc().minute, 34);
      expect(dt.toUtc().second, 56);
    });

    test('parses a valid ISO-8601 date-only string', () {
      final dt = dateTimeFromJson('2026-01-15');
      expect(dt, isNotNull);
      expect(dt!.year, 2026);
      expect(dt.month, 1);
      expect(dt.day, 15);
    });

    test('returns null for malformed string instead of throwing', () {
      // Regression guard: this used to throw FormatException and crash
      // the entire model deserialization.
      expect(() => dateTimeFromJson('not a date'), returnsNormally);
      expect(dateTimeFromJson('not a date'), isNull);
    });

    test('returns null for ISO-like string with non-numeric junk', () {
      // DateTime.parse is surprisingly tolerant of out-of-range numeric
      // components (it rolls them over), so we use a clearly non-numeric
      // value to force a FormatException.
      expect(dateTimeFromJson('2026-XX-YYTzz:zz:zzZ'), isNull);
    });

    test('returns null for garbage tokens', () {
      expect(dateTimeFromJson('@@@'), isNull);
    });
  });

  group('dateTimeToJson', () {
    test('returns null for null input', () {
      expect(dateTimeToJson(null), isNull);
    });

    test('serializes DateTime to ISO-8601 string', () {
      final dt = DateTime.utc(2026, 5, 20, 12, 34, 56);
      expect(dateTimeToJson(dt), '2026-05-20T12:34:56.000Z');
    });
  });
}
