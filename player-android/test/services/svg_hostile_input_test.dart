// Time and memory regression tests for crafted SVGs, run through the real
// IsolateSvgCompiler as the app does.
//
// Reviews of this code found documents of a few hundred bytes to a few
// hundred kilobytes that made the compiler isolate, the picture decoder or
// the rasteriser allocate from 600 MB to several gigabytes. Each of them
// must be refused by the gate (svg_gate.dart), which runs before the
// compiler: every case asserts the gate's own rejection reason, so a case
// cannot pass because a later stage happened to object as well.
//
// The second group holds unusual documents the gate accepts. Those run the
// whole pipeline (compile, decode, rasterise at phone size) under the same
// time and memory bounds.
//
// `ProcessInfo.maxRss` is the peak for the whole test process, so its
// growth across one case bounds what that case allocated.

import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/providers/svg_picture_provider.dart';
import 'package:player_android/services/svg_compiler.dart';
import 'package:player_android/services/svg_document.dart';

import '../support/svg_test_support.dart';

const _megabyte = 1024 * 1024;

/// Generous: a rejection takes milliseconds and a few megabytes; the
/// behaviour these tests guard against took seconds and gigabytes.
const _maxElapsed = Duration(seconds: 1);
const _maxGrowth = 200 * _megabyte;

const _box = 'width="100" height="100" viewBox="0 0 1000 1000"';
const _rect = '<rect width="100" height="100" fill="#f00"/>';

/// A path of about [bytes] bytes of data.
String _path(int bytes, {String id = 'p'}) =>
    '<path id="$id" d="M0 0${' l1 1 l-1 0' * (bytes ~/ 11)}"/>';

/// [count] `<use>` elements copying one path of [pathBytes].
String _usedPath(int count, int pathBytes) => svgDocument(
    size: _box,
    body: '<defs>${_path(pathBytes)}</defs>${'<use href="#p"/>' * count}');

/// [count] elements clipped by one clip path of [pathBytes].
String _clippedBy(int count, int pathBytes) => svgDocument(
    size: _box,
    body: '<defs><clipPath id="c">${_path(pathBytes)}</clipPath></defs>'
        '${'<rect width="9" height="9" clip-path="url(#c)"/>' * count}');

/// Documents from the reviews, each with the gate's reason for refusing it.
final _refused = <String, (String, String)>{
  'a 200-byte dash array on a long line': (
    svgDocument(
        size: 'width="100" height="100" viewBox="0 0 1000000 100"',
        body: '<line x1="0" y1="50" x2="1000000" y2="50" stroke="#000" '
            'stroke-width="1" stroke-dasharray="0.001 0.001"/>'),
    'attribute stroke-dasharray is not supported',
  ),
  '10,000 dashed lines': (
    svgDocument(
        size: _box,
        body: '<line x2="1000" y2="9" stroke="#000" stroke-dasharray="0.01"/>'
                '\n' *
            10000),
    'attribute stroke-dasharray is not supported',
  ),
  '64 nested opacity groups': (
    svgDocument(
        size: _box, body: '${'<g opacity="0.99">$_rect' * 64}${'</g>' * 64}'),
    'nests too many translucent groups',
  ),
  '900 KB of text': (
    svgDocument(body: '<text font-size="4">${'A' * (900 * 1024)}</text>'),
    'too much text',
  ),
  'text hidden in a shape inside text': (
    svgDocument(
        body: '<text x="0" y="8" font-size="4"><rect>'
            '${'A<!---->' * 99000}</rect></text>'),
    '<rect> is not allowed inside <text>',
  ),
  'text after a self-closing clip path inside text': (
    svgDocument(body: '<text><clipPath id="c"/></text>${'A<!---->' * 99000}'),
    '<clipPath> is not allowed inside <text>',
  ),
  'text after a self-closing clip path inside a group': (
    svgDocument(body: '<g><clipPath id="c"/></g>$_rect${'A' * 99000}'),
    'empty clip path',
  ),
  'a 16000x16000 pattern tile': (
    svgDocument(
        size: 'width="100" height="100" viewBox="0 0 16000 16000"',
        body: '<defs><pattern id="p" width="16000" height="16000" '
            'patternUnits="userSpaceOnUse">$_rect</pattern></defs>'
            '<rect width="16000" height="16000" fill="url(#p)"/>'),
    'element <pattern> is not supported',
  ),
  for (final (uses, kilobytes) in [
    (200, 20),
    (200, 100),
    (50, 500),
    (200, 500)
  ])
    '$uses uses of a $kilobytes KB path': (
      _usedPath(uses, kilobytes * 1024),
      'references multiply its content too often',
    ),
  for (final (refs, kilobytes) in [(4990, 100), (1000, 500)])
    '$refs clip-path references to a $kilobytes KB clip path': (
      _clippedBy(refs, kilobytes * 1024),
      'references multiply its content too often',
    ),
  'use chains that multiply tenfold per level': (
    svgDocument(
        body: '<defs><g id="l0">$_rect</g>${[
      for (var i = 1; i <= 15; i++)
        '<g id="l$i">${'<use href="#l${i - 1}"/>' * 10}</g>',
    ].join()}</defs><use href="#l15"/>'),
    '<use> may not reference content that contains <use>',
  ),
  'a use that references itself': (
    svgDocument(body: '$_rect<use id="u" href="#u"/>'),
    '<use> may not reference content that contains <use>',
  ),
  'a use that references its ancestor': (
    svgDocument(body: '<g id="a">$_rect<use href="#a"/></g>'),
    '<use> may not reference content that contains <use>',
  ),
  'a gradient with 5,000 stops': (
    svgDocument(
        body: '<defs><linearGradient id="g">'
            '${'<stop offset="0.5" stop-color="#f00"/>' * 5000}'
            '</linearGradient></defs><rect width="9" height="9" '
            'fill="url(#g)"/>'),
    'too many gradient stops',
  ),
  'a default namespace that is not SVG': (
    '<svg xmlns="http://www.w3.org/1999/xhtml" width="9" height="9">'
        '$_rect</svg>',
    'namespace declarations are not supported here',
  ),
  'a namespace redefined on a child': (
    svgDocument(body: '<g xmlns="urn:other">$_rect</g>'),
    'namespace declarations are not supported here',
  ),
};

Uint8List _utf16(String text) {
  final data = ByteData(2 + text.length * 2)
    ..setUint16(0, 0xfeff, Endian.little);
  for (var i = 0; i < text.length; i++) {
    data.setUint16(2 + i * 2, text.codeUnitAt(i), Endian.little);
  }
  return data.buffer.asUint8List();
}

/// Unusual but harmless documents the gate lets through.
final _accepted = <String, Uint8List>{
  'UTF-8 with a byte order mark': Uint8List.fromList(
      [0xef, 0xbb, 0xbf, ...svgBytes(svgDocument(size: _box, body: _rect))]),
  'UTF-16 with a byte order mark': _utf16(svgDocument(size: _box, body: _rect)),
  'an extreme transform scale': svgBytes(svgDocument(
      size: _box,
      body: '<g transform="scale(1e30)">$_rect</g>'
          '<g transform="scale(1e-30)">$_rect</g>$_rect')),
  // The largest documents of their kind that the budgets still allow.
  'at the budget: 3 uses of a 500 KB path': svgBytes(_usedPath(3, 500 * 1024)),
  'at the budget: 3 clip references to a 500 KB path':
      svgBytes(_clippedBy(3, 500 * 1024)),
  'at the budget: 4,990 rectangles': svgBytes(svgDocument(
      size: _box,
      body: '<rect x="5" y="5" width="9" height="9" fill="#f00"/>' * 4990)),
  'at the budget: 2,000 characters of text used 4 times': svgBytes(svgDocument(
      size: _box,
      body: '<defs><text id="t" y="20" font-size="4">${'A' * 2000}</text>'
          '</defs>${'<use href="#t"/>' * 4}')),
  'at the budget: 4 nested translucent groups': svgBytes(svgDocument(
      size: _box, body: '${'<g opacity="0.9">$_rect' * 4}${'</g>' * 4}')),
  'coordinates of 1e30': svgBytes(svgDocument(
      size: _box,
      body: '<path d="M-1e30 -1e30 L1e30 1e30 L1e30 -1e30 Z" fill="#f00" '
          'stroke="#00f" stroke-width="1000"/>'
          '<rect x="1e30" y="-1e30" width="1e30" height="1e30"/>'
          '<circle cx="0" cy="0" r="1e30"/>')),
};

/// Runs [body] and checks how long it took and how much the process grew.
Future<void> _bounded(String name, Future<void> Function() body) async {
  final rssBefore = ProcessInfo.maxRss;
  final watch = Stopwatch()..start();
  await body();
  final elapsed = watch.elapsed;
  final growth = ProcessInfo.maxRss - rssBefore;
  // Printed so that the measured numbers can be quoted.
  // ignore: avoid_print
  print('MEASURED $name: ${elapsed.inMilliseconds} ms, '
      'peak RSS +${growth ~/ _megabyte} MB');
  expect(elapsed, lessThan(_maxElapsed));
  expect(growth, lessThan(_maxGrowth));
}

/// Compiles [bytes] in an isolate and returns the rejection message.
Future<String> _rejection(Uint8List bytes) async {
  try {
    await IsolateSvgCompiler().call(bytes);
  } on SvgException catch (error) {
    return error.message;
  }
  return 'accepted';
}

/// Compiles, decodes and rasterises [bytes] at the size of a phone screen.
Future<void> _showFullScreen(Uint8List bytes) async {
  final compiled = await IsolateSvgCompiler().call(bytes);
  final info = await decodeSvgPicture(ByteData.sublistView(compiled));
  final image = await info.picture.toImage(1080, 1920);
  image.dispose();
  info.picture.dispose();
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  // Starting the first isolate of a process is slow; do it outside the
  // timed cases.
  setUpAll(() => IsolateSvgCompiler().call(svgBytes(kValidSvg)));

  group('refused by the gate', () {
    for (final MapEntry(key: name, value: (document, reason))
        in _refused.entries) {
      test(name, () async {
        final bytes = svgBytes(document);
        expect(bytes.length, lessThanOrEqualTo(kMaxSvgBytes));
        await _bounded(name,
            () async => expect(await _rejection(bytes), contains(reason)));
      });
    }
  });

  group('accepted and cheap to show', () {
    for (final MapEntry(key: name, value: bytes) in _accepted.entries) {
      test(name, () => _bounded(name, () => _showFullScreen(bytes)));
    }
  });
}
