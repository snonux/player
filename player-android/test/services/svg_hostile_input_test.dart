// Time and memory regression tests for crafted SVGs, run through the real
// IsolateSvgCompiler and rasteriser as the app does.
//
// Reviews of this code found documents of a few hundred bytes to a few
// hundred kilobytes that made the compiler isolate, the picture decoder or
// the rasteriser allocate from 600 MB to several gigabytes, or take many
// seconds to paint. The first group must be refused, and every case names
// the reason, so a case cannot pass because some other stage happened to
// object as well.
//
// The second group holds documents that are accepted: unusual but harmless
// ones, documents that are expensive per pixel (which are therefore given
// a smaller bitmap), and the most expensive ones the budgets allow. Each
// runs the whole pipeline (compile, decode, rasterise) for a 1440x3120
// screen and for a 160x160 tile, and must stay within the bounds below.
// The test rasteriser is the software one, slower than a phone's GPU.
//
// `ProcessInfo.maxRss` is the peak for the whole test process, so its
// growth across one case bounds what that case allocated.

import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/providers/svg_image_provider.dart';
import 'package:player_android/services/svg_compiler.dart';
import 'package:player_android/services/svg_document.dart';
import 'package:player_android/services/svg_raster.dart';

import '../support/svg_test_support.dart';

const _megabyte = 1024 * 1024;

/// A rejection takes milliseconds and a few megabytes.
const _refusal = (Duration(seconds: 1), 100 * _megabyte);

/// The bound for showing an accepted document for the first time. The
/// raster budget aims at about one second; this leaves room for a loaded
/// machine.
const _display = (Duration(seconds: 2), 150 * _megabyte);

/// A drawing with the proportions of the screen it is rasterised for.
const _box = 'width="144" height="312" viewBox="0 0 1440 3120"';
const _square = 'width="100" height="100" viewBox="0 0 100 100"';
const _full = 'width="1440" height="3120"';
const _rect = '<rect $_full fill="#f00"/>';

/// A path of about [bytes] bytes of data made of short segments.
String _path(int bytes, {String id = 'p'}) =>
    '<path id="$id" d="M0 0${' l1 1 l-1 0' * (bytes ~/ 11)}"/>';

/// Path data crossing the whole drawing [crossings] times.
String _zigzag(int crossings) => 'M0 0${[
      for (var i = 0; i < crossings; i++) 'L${i % 1440} ${i.isEven ? 3120 : 0}',
    ].join()}';

/// A points list of at most [maxChars] characters that wanders over the
/// drawing in steps of a few units.
String _meander(int maxChars) {
  final points = StringBuffer();
  for (var i = 0;; i++) {
    final next = '${i % 1440},${(i ~/ 1440) * 10 + (i.isEven ? 0 : 5)} ';
    if (points.length + next.length > maxChars) break;
    points.write(next);
  }
  return points.toString().trimRight();
}

/// [count] `<use>` elements copying one path of [pathBytes].
String _usedPath(int count, int pathBytes) => svgDocument(
    size: _box,
    body: '<defs>${_path(pathBytes)}</defs>${'<use href="#p"/>' * count}');

/// [count] elements clipped by one clip path of [pathBytes].
String _clippedBy(int count, int pathBytes) => svgDocument(
    size: _box,
    body: '<defs><clipPath id="c">${_path(pathBytes)}</clipPath></defs>'
        '${'<rect width="9" height="9" clip-path="url(#c)"/>' * count}');

/// A stroke 1000 wide on a centre line a thousandth of a unit long: it
/// paints the whole drawing while its geometry is next to nothing.
const _wideDot = '<line x1="50" y1="50" x2="50.001" y2="50" stroke="#00f" '
    'stroke-opacity="0.5" stroke-width="1000" stroke-linecap="round"/>';

/// The same with the centre line outside the drawing.
const _wideOutside = '<line x1="-400" y1="-400" x2="-399" y2="-400" '
    'stroke="#00f" stroke-opacity="0.5" stroke-width="1000" '
    'stroke-linecap="square"/>';

/// 97 glyphs, each far larger than the drawing.
final _hugeGlyphs = '<text id="t" font-size="400" fill-opacity="0.5">'
    '${'<tspan x="0" y="90">W</tspan>' * 97}</text>';

/// A clip path whose even-odd outline crosses the drawing [crossings]
/// times, spread evenly over its width.
String _zigzagClip(int crossings, {String id = 'c'}) =>
    '<clipPath id="$id"><path clip-rule="evenodd" d="M0 0${[
      for (var i = 0; i < crossings; i++)
        'L${(i * 1440 / crossings).round()} ${i.isEven ? 3120 : 0}',
    ].join()}Z"/></clipPath>';

/// 97 glyphs about twice as tall as the drawing, to be drawn under a clip.
String _clippedGlyphs({String stroke = ''}) =>
    '<text id="t" font-size="6000" fill="#f00"$stroke>'
    '${'<tspan x="0" y="3120">W</tspan>' * 97}</text>';

const _translucentRect = '<rect $_full fill="#f00" fill-opacity="0.5"/>';

const _radial = '<defs><radialGradient id="r" spreadMethod="repeat" r="0.1">'
    '<stop offset="0" stop-color="#f00"/><stop offset="1" stop-color="#00f"/>'
    '</radialGradient></defs>';
const _gradientRect = '<rect $_full fill="url(#r)" fill-opacity="0.5"/>';

/// Documents that must be refused, each with the reason.
final _refused = <String, (String, String)>{
  'a 200-byte dash array on a long line': (
    svgDocument(
        size: 'width="100" height="100" viewBox="0 0 1000000 100"',
        body: '<line x1="0" y1="50" x2="1000000" y2="50" stroke="#000" '
            'stroke-width="1" stroke-dasharray="0.001 0.001"/>'),
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
  'a 16000x16000 pattern tile': (
    svgDocument(
        size: 'width="100" height="100" viewBox="0 0 16000 16000"',
        body: '<defs><pattern id="p" width="16000" height="16000" '
            'patternUnits="userSpaceOnUse">$_rect</pattern></defs>'
            '<rect width="16000" height="16000" fill="url(#p)"/>'),
    'element <pattern> is not supported',
  ),
  '100 uses of a 60 KB path': (
    _usedPath(100, 60 * 1024),
    'references multiply its content too often',
  ),
  '1000 clip-path references to a 60 KB clip path': (
    _clippedBy(1000, 60 * 1024),
    'references multiply its content too often',
  ),
  'use chains hidden behind style ids with a second colon': (
    svgDocument(
        size: _box,
        body: '<defs><g style="id:g0:x">${_path(60 * 1024)}</g>${[
          for (var level = 1; level <= 3; level++)
            '<g style="id:g$level:x">${'<use href="#g${level - 1}"/>' * 5}</g>',
        ].join()}</defs>${'<use href="#g3"/>' * 5}'),
    'style property id is not supported',
  ),
  'a 150 KB zigzag path filled even-odd': (
    svgDocument(
        size: _box, body: '<path fill-rule="evenodd" d="${_zigzag(13000)}"/>'),
    'oversized path',
  ),
  // Drawing under a complex clip cost ten times what was counted; only
  // simple clips are accepted now.
  '4,268 huge glyphs under a 250-crossing clip': (
    svgDocument(
        size: _box,
        body: '<defs>${_zigzagClip(250)}${_clippedGlyphs()}</defs>'
            '<g clip-path="url(#c)">${'<use href="#t"/>' * 44}</g>'),
    'clip path is too complex',
  ),
  'the same glyphs with a stroke 1000 wide': (
    svgDocument(
        size: _box,
        body: '<defs>${_zigzagClip(250)}'
            '${_clippedGlyphs(stroke: ' stroke="#00f" stroke-width="1000"')}'
            '</defs><g clip-path="url(#c)">${'<use href="#t"/>' * 44}</g>'),
    'clip path is too complex',
  ),
  '1,900 rectangles under a 5,500-crossing clip': (
    svgDocument(
        size: _box,
        body: '<defs>${_zigzagClip(5500)}</defs>'
            '<g clip-path="url(#c)">${_translucentRect * 1900}</g>'),
    'clip path is too complex',
  ),
  '2,870 rectangles through use under a 400-crossing clip': (
    svgDocument(
        size: _box,
        body: '<defs>${_zigzagClip(400)}<g id="g">${_translucentRect * 41}</g>'
            '</defs><g clip-path="url(#c)">${'<use href="#g"/>' * 70}</g>'),
    'clip path is too complex',
  ),
  '617 rectangles under a 700-crossing clip': (
    svgDocument(
        size: _box,
        body: '<defs>${_zigzagClip(700)}</defs>'
            '<g clip-path="url(#c)">${_translucentRect * 617}</g>'),
    'clip path is too complex',
  ),
  'a clip path with few crossings but many commands': (
    svgDocument(
        size: _box,
        body: '<defs><clipPath id="c"><path d="M0 0${' h1 v1' * 40}"/>'
            '</clipPath></defs><g clip-path="url(#c)">$_rect</g>'),
    'clip path is too complex',
  ),
  'too many glyphs under a simple clip': (
    svgDocument(
        size: _box,
        body: '<defs>${_zigzagClip(32)}${_clippedGlyphs()}</defs>'
            '<g clip-path="url(#c)">${'<use href="#t"/>' * 44}</g>'),
    'too expensive to draw',
  ),
  // Cheap to describe, slow to paint: refused by the count of operations.
  '1,999 wide strokes on tiny centre lines': (
    svgDocument(size: _square, body: _wideDot * 1999),
    'too expensive to draw',
  ),
  '1,999 wide strokes with centre lines outside the drawing': (
    svgDocument(size: _square, body: _wideOutside * 1999),
    'too expensive to draw',
  ),
  '97 huge glyphs used 99 times': (
    svgDocument(
        size: _square,
        body: '<defs>$_hugeGlyphs</defs>${'<use href="#t"/>' * 99}'),
    'too expensive to draw',
  ),
  'glyphs far larger than the drawing': (
    svgDocument(size: _square, body: '<text font-size="10000">W</text>'),
    'unusable font size',
  ),
  '1,990 full-size gradient rectangles': (
    svgDocument(size: _box, body: '$_radial${_gradientRect * 1990}'),
    'too expensive to draw',
  ),
};

/// Documents that are accepted and must be affordable at any size.
final _accepted = <String, String>{
  'coordinates of 1e30': svgDocument(
      size: _box,
      body: '<path d="M-1e30 -1e30 L1e30 1e30 L1e30 -1e30 Z" fill="#f00" '
          'stroke="#00f" stroke-width="10"/>'
          '<rect x="1e30" y="-1e30" width="1e30" height="1e30"/>'
          '<circle cx="0" cy="0" r="1e30"/>'
          '<g transform="scale(1e30)">$_rect</g>'),
  'an extreme stroke miter limit on sharp joins': svgDocument(
      size: _box,
      body: '<path fill="none" stroke="#000" stroke-width="20" '
          'stroke-miterlimit="1e30" d="${_zigzag(100)}"/>'),
  'a repeating gradient with a tiny gradient transform': svgDocument(
      size: _box,
      body: '<defs><linearGradient id="g" spreadMethod="repeat" '
          'gradientUnits="userSpaceOnUse" x2="1" '
          'gradientTransform="scale(1e-6) rotate(33)">'
          '<stop offset="0" stop-color="#f00"/>'
          '<stop offset="1" stop-color="#00f"/></linearGradient></defs>'
          '<rect $_full fill="url(#g)"/>'),
  'arcs with huge radii and clipPathUnits': svgDocument(
      size: _box,
      body: '<defs><clipPath id="c" clipPathUnits="objectBoundingBox">'
          '<rect width="1e9" height="1e9"/></clipPath></defs>'
          '<path fill="#f00" clip-path="url(#c)" d="M0 0 A1e30 1e30 0 1 1 '
          '1440 3120 A1e-30 1e30 45 0 0 0 0 Z"/>'),
  'a use of a group with clipped shapes': svgDocument(
      size: _box,
      body: '<defs><clipPath id="c"><circle cx="700" cy="700" r="600"/>'
          '</clipPath><g id="g">${'<rect width="500" height="500" '
              'clip-path="url(#c)"/>' * 10}</g></defs>'
          '${'<use href="#g"/>' * 10}'),
  // Drawing under the most complex clip that is accepted.
  'at the clip limit: 700 full-size rectangles under a 32-crossing clip':
      svgDocument(
          size: _box,
          body: '<defs>${_zigzagClip(32)}</defs>'
              '<g clip-path="url(#c)">${_translucentRect * 700}</g>'),
  'at the clip limit: 170 rectangles under two nested clips': svgDocument(
      size: _box,
      body: '<defs>${_zigzagClip(32)}${_zigzagClip(30, id: 'd')}</defs>'
          '<g clip-path="url(#c)"><g clip-path="url(#d)">'
          '${_translucentRect * 170}</g></g>'),
  'at the clip limit: 97 huge glyphs used 5 times under a clip': svgDocument(
      size: _box,
      body: '<defs>${_zigzagClip(32)}${_clippedGlyphs()}</defs>'
          '<g clip-path="url(#c)">${'<use href="#t"/>' * 5}</g>'),
  'at the clip limit: 97 huge stroked glyphs used twice under a clip':
      svgDocument(
          size: _box,
          body: '<defs>${_zigzagClip(32)}'
              '${_clippedGlyphs(stroke: ' stroke="#00f" stroke-width="1000"')}'
              '</defs><g clip-path="url(#c)">${'<use href="#t"/>' * 2}</g>'),
  'a 64 KB points list of short segments': svgDocument(
      size: _box,
      body: '<polyline fill="none" stroke="#000" '
          'points="${_meander(64 * 1024)}"/>'),
  '1,990 small rectangles': svgDocument(
      size: _box,
      body: '<rect x="5" y="5" width="90" height="90" fill="#f00"/>' * 1990),
  // Expensive per pixel: shown, but as a smaller bitmap.
  '200 wide strokes on tiny centre lines':
      svgDocument(size: _square, body: _wideDot * 200),
  '100 wide strokes in a group used 10 times': svgDocument(
      size: _square,
      body: '<defs><g id="g">${_wideDot * 100}</g></defs>'
          '${'<use href="#g"/>' * 10}'),
  '200 wide strokes with centre lines outside the drawing':
      svgDocument(size: _square, body: _wideOutside * 200),
  '97 huge glyphs used 10 times': svgDocument(
      size: _square,
      body: '<defs>$_hugeGlyphs</defs>${'<use href="#t"/>' * 10}'),
  '300 translucent full-size rectangles': svgDocument(
      size: _box, body: '<rect $_full fill="#f00" fill-opacity="0.5"/>' * 300),
  '300 full-size repeating radial gradients':
      svgDocument(size: _box, body: '$_radial${_gradientRect * 300}'),
  'a 60 KB zigzag path filled even-odd': svgDocument(
      size: _box, body: '<path fill-rule="evenodd" d="${_zigzag(5500)}"/>'),
  'a zigzag stroked 1000 wide with round joins': svgDocument(
      size: _box,
      body: '<path fill="none" stroke="#00f" stroke-width="1000" '
          'stroke-linejoin="round" d="${_zigzag(600)}"/>'),
  // Calibration: gradient fills of the whole bitmap, as many as the budget
  // allows at full resolution and at the smallest bitmap.
  'at the budget: 51 full-size gradients at full resolution':
      svgDocument(size: _box, body: '$_radial${_gradientRect * 51}'),
  'at the budget: 1,500 full-size gradients at the smallest bitmap':
      svgDocument(size: _box, body: '$_radial${_gradientRect * 1500}'),
  'at the budget: 4 nested layers of gradients and a zigzag': svgDocument(
      size: _box,
      body: '$_radial${'<g opacity="0.9">' * 4}${_gradientRect * 20}'
          '<path fill-rule="evenodd" fill="url(#r)" d="${_zigzag(150)}"/>'
          '${'</g>' * 4}'),
};

SvgImageRequest _request(double width, double height) => SvgImageRequest(
      source: SvgRequest(uri: Uri.parse('https://player.example/x.svg')),
      width: width,
      height: height,
    );

final _screen = _request(1440, 3120);
final _tile = _request(160, 160);

/// Runs [body] and checks how long it took and how much the process grew.
Future<void> _bounded(
  String name,
  (Duration, int) bounds,
  Future<String> Function() body,
) async {
  final rssBefore = ProcessInfo.maxRss;
  final watch = Stopwatch()..start();
  final detail = await body();
  final elapsed = watch.elapsed;
  final growth = ProcessInfo.maxRss - rssBefore;
  // Printed so that the measured numbers can be quoted.
  // ignore: avoid_print
  print('MEASURED $name: $detail ${elapsed.inMilliseconds} ms, '
      'peak RSS +${growth ~/ _megabyte} MB');
  expect(elapsed, lessThan(bounds.$1));
  expect(growth, lessThan(bounds.$2));
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

/// Compiles and rasterises [bytes] for [request] and describes the result.
Future<String> _show(Uint8List bytes, SvgImageRequest request) async {
  final compiled = await IsolateSvgCompiler().call(bytes);
  final raster = await rasterizeSvg(compiled, request);
  final image = raster.image;
  final pixels = image.width * image.height;
  expect(compiled.drawOperations * pixels,
      lessThanOrEqualTo(kSvgRasterBudget * 1.01));
  raster.dispose();
  return '${compiled.drawOperations.round()} ops, '
      '${image.width}x${image.height},';
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  // Starting the first isolate of a process is slow; do it outside the
  // timed cases.
  setUpAll(() => IsolateSvgCompiler().call(svgBytes(kValidSvg)));

  group('refused', () {
    for (final MapEntry(key: name, value: (document, reason))
        in _refused.entries) {
      test(name, () async {
        final bytes = svgBytes(document);
        expect(bytes.length, lessThanOrEqualTo(kMaxSvgBytes));
        await _bounded(name, _refusal, () async {
          expect(await _rejection(bytes), contains(reason));
          return 'refused,';
        });
      });
    }
  });

  for (final (label, request) in [('screen', _screen), ('tile', _tile)]) {
    group('accepted and affordable on a $label', () {
      for (final MapEntry(key: name, value: document) in _accepted.entries) {
        test(
            name,
            () => _bounded('[$label] $name', _display,
                () => _show(svgBytes(document), request)));
      }
    });
  }
}
