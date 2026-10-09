// Property-style test: every document that the gate and the compiler
// accept also decodes and rasterises into a bitmap.
//
// Documents are generated from a fixed seed, mixing allowed elements with
// the occasional forbidden one, so both outcomes occur. What matters is the
// implication "accepted => decodes and paints": an accepted document that
// then failed to decode would be cached and shown as a broken image.

import 'dart:math';

import 'package:flutter/foundation.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/providers/svg_image_provider.dart';
import 'package:player_android/services/svg_document.dart';
import 'package:player_android/services/svg_raster.dart';

import '../support/svg_test_support.dart';

/// Builds random SVG documents from one [Random].
class _Generator {
  _Generator(int seed) : _random = Random(seed);

  final Random _random;

  T _pick<T>(List<T> values) => values[_random.nextInt(values.length)];
  String _number([int max = 100]) => (_random.nextDouble() * max).toString();
  String _color() => _pick(['#f00', '#00ff00', 'blue', 'none', 'url(#grad)']);

  String _paint() => [
        'fill="${_color()}"',
        if (_random.nextBool()) 'stroke="${_color()}"',
        if (_random.nextBool()) 'stroke-width="${_number(20)}"',
        if (_random.nextInt(4) == 0) 'opacity="${_number(1)}"',
        if (_random.nextInt(6) == 0) 'style="fill-opacity:${_number(1)}"',
        if (_random.nextInt(6) == 0) 'transform="rotate(${_number(360)})"',
        if (_random.nextInt(8) == 0) 'clip-path="url(#clip)"',
        // Forbidden now and then, so rejections are exercised as well.
        if (_random.nextInt(25) == 0) 'stroke-dasharray="1 2"',
      ].join(' ');

  String _shape() => _pick([
        '<rect x="${_number()}" y="${_number()}" width="${_number()}" '
            'height="${_number()}" ${_paint()}/>',
        '<circle cx="${_number()}" cy="${_number()}" r="${_number(50)}" '
            '${_paint()}/>',
        '<ellipse cx="50" cy="50" rx="${_number(50)}" ry="${_number(50)}" '
            '${_paint()}/>',
        '<line x2="${_number()}" y2="${_number()}" ${_paint()}/>',
        '<polygon points="0,0 ${_number()},${_number()} 50,0" ${_paint()}/>',
        '<polyline points="0,0 ${_number()},${_number()}" ${_paint()}/>',
        '<path d="M${_number()} ${_number()} L${_number()} ${_number()} '
            'Q10 10 ${_number()} ${_number()} Z" ${_paint()}/>',
        '<text x="${_number()}" y="${_number()}" font-size="${_number(30)}" '
            '${_paint()}>T${_random.nextInt(99)}<tspan>s</tspan></text>',
        '<use href="#shared" x="${_number()}" ${_paint()}/>',
        if (_random.nextInt(20) == 0) '<image href="#shared"/>',
      ]);

  String _group(int depth) {
    final children = [
      for (var i = _random.nextInt(4) + 1; i > 0; i--)
        depth < 5 && _random.nextInt(3) == 0 ? _group(depth + 1) : _shape(),
    ];
    return '<g ${_paint()}>${children.join()}</g>';
  }

  String document() => svgDocument(
      size: 'width="100" height="100" viewBox="0 0 100 100"',
      body: '<defs><linearGradient id="grad"><stop offset="0" '
          'stop-color="#f00"/><stop offset="1" stop-color="#00f"/>'
          '</linearGradient><clipPath id="clip"><circle cx="50" cy="50" '
          'r="40"/></clipPath><g id="shared"><rect width="5" height="5"/>'
          '</g></defs>${_group(0)}');
}

final _box = SvgImageRequest(
  source: SvgRequest(uri: Uri.parse('https://player.example/x.svg')),
  width: 64,
  height: 64,
);

/// Decodes and rasterises [compiled] the way the app does.
Future<void> _decodeAndPaint(Uint8List compiled) async {
  final raster = await rasterizeSvg(ByteData.sublistView(compiled), _box);
  expect(raster.size.width, 100);
  expect(raster.image.width, 64);
  raster.dispose();
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('every accepted document decodes and paints', () async {
    final generator = _Generator(20261009);
    var accepted = 0;
    var rejected = 0;
    for (var i = 0; i < 400; i++) {
      final document = generator.document();
      final Uint8List compiled;
      try {
        compiled = compileSvg(svgBytes(document));
      } on SvgException {
        rejected++;
        continue;
      }
      accepted++;
      try {
        await _decodeAndPaint(compiled);
      } catch (error) {
        fail('Accepted document does not paint: $error\n$document');
      }
    }
    // Both outcomes must be well represented for the test to mean anything.
    expect(accepted, greaterThan(100));
    expect(rejected, greaterThan(50));
  });
}
