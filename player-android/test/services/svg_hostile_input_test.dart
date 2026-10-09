// Memory and time regression tests for crafted SVGs, run through the real
// IsolateSvgCompiler as the app does.
//
// Before the allowlist gate each of these made the compiler isolate or the
// rasteriser allocate from one to several gigabytes. They must now be
// refused before the compiler sees them: quickly, and without the process
// growing. `ProcessInfo.maxRss` is the peak for the whole test process, so
// its growth across one case bounds what that case allocated.

import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/svg_compiler.dart';
import 'package:player_android/services/svg_document.dart';

import '../support/svg_test_support.dart';

const _megabyte = 1024 * 1024;

/// Generous: a rejection takes tens of milliseconds and a few megabytes,
/// the unfixed behaviour took seconds and gigabytes.
const _maxElapsed = Duration(seconds: 1);
const _maxGrowth = 200 * _megabyte;

/// A single long stroked line with a tiny dash array (about 200 bytes).
/// The compiler expands dashes into one segment each while parsing.
final _dashedLine = svgDocument(
    size: 'width="100" height="100" viewBox="0 0 1000000 100"',
    body: '<line x1="0" y1="50" x2="1000000" y2="50" stroke="#000" '
        'stroke-width="1" stroke-dasharray="0.001 0.001"/>');

/// 20,000 short dashed lines, about 1.6 MB.
final _manyDashedLines = svgDocument(
    size: 'width="100" height="100" viewBox="0 0 1000 1000"',
    body: [
      for (var i = 0; i < 20000; i++)
        '<line x1="0" y1="${i % 1000}" x2="1000" y2="${i % 1000}" '
            'stroke="#000" stroke-dasharray="0.01"/>\n',
    ].join());

/// 64 nested translucent groups, each with a full-size rectangle: 64
/// screen-sized buffers alive at once when painted.
final _nestedLayers = svgDocument(
    size: 'width="100" height="100"',
    body: '${'<g opacity="0.99"><rect width="100" height="100" '
        'fill="#f00"/>' * 64}${'</g>' * 64}');

/// 1.9 MB of text in one element; compiled, it could not be decoded.
final _hugeText = svgDocument(
    body: '<text x="0" y="8" font-size="4">${'A' * (1900 * 1024)}</text>');

/// The 16000x16000 pattern tile of the previous review.
final _hugePattern = svgDocument(
    size: 'width="100" height="100" viewBox="0 0 16000 16000"',
    body: '<defs><pattern id="p" width="16000" height="16000" '
        'patternUnits="userSpaceOnUse"><rect width="8000" height="8000" '
        'fill="#f00"/></pattern></defs>'
        '<rect width="16000" height="16000" fill="url(#p)"/>');

/// The hostile documents, each with a fragment of the expected rejection.
final _hostile = <String, (String, String)>{
  'a 200-byte dash array on a long line': (
    _dashedLine,
    'stroke-dasharray is not supported',
  ),
  '20,000 dashed lines': (
    _manyDashedLines,
    'stroke-dasharray is not supported',
  ),
  '64 nested opacity groups': (_nestedLayers, 'too complex'),
  '1.9 MB of text': (_hugeText, 'too complex'),
  'a 16000x16000 pattern tile': (_hugePattern, 'element <pattern>'),
};

void main() {
  test('the documents have the sizes the scenarios describe', () {
    expect(_dashedLine.length, lessThan(300));
    expect(_manyDashedLines.length, greaterThan(1500000));
    expect(_manyDashedLines.length, lessThan(kMaxSvgBytes));
    expect(_hugeText.length, lessThan(kMaxSvgBytes));
  });

  // Starting the first isolate of a process is slow; do it outside the
  // timed cases.
  setUpAll(() => IsolateSvgCompiler().call(svgBytes(kValidSvg)));

  for (final MapEntry(key: name, value: (document, message))
      in _hostile.entries) {
    test('$name is refused quickly and without allocating', () async {
      final bytes = svgBytes(document);
      final rssBefore = ProcessInfo.maxRss;
      final watch = Stopwatch()..start();

      final result = await _compile(bytes);

      final elapsed = watch.elapsed;
      final growth = ProcessInfo.maxRss - rssBefore;
      // Printed so that the measured numbers can be quoted.
      // ignore: avoid_print
      print('MEASURED $name: ${elapsed.inMilliseconds} ms, '
          'peak RSS +${growth ~/ _megabyte} MB');
      expect(result, contains(message));
      expect(elapsed, lessThan(_maxElapsed));
      expect(growth, lessThan(_maxGrowth));
    });
  }
}

/// Compiles [bytes] in an isolate and returns the rejection message.
Future<String> _compile(Uint8List bytes) async {
  try {
    await IsolateSvgCompiler().call(bytes);
  } on SvgException catch (error) {
    return error.message;
  }
  return 'accepted';
}
