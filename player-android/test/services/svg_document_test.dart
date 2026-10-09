// Unit tests for SVG detection, text decoding and validation
// (svg_document.dart). No Flutter binding is needed: this code runs in a
// background isolate in the app.

import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/svg_document.dart';

import '../support/svg_test_support.dart';

Matcher _rejects(String message) => throwsA(
    isA<SvgException>().having((e) => e.message, 'message', contains(message)));

void _expectRejected(String document, String message) =>
    expect(() => compileSvg(svgBytes(document)), _rejects(message));

void main() {
  group('isSvgSource', _isSvgSourceTests);
  group('decodeSvgText', _decodeTests);
  group('compileSvg accepts', _acceptTests);
  group('compileSvg rejects sizes and empty drawings', _rejectShapeTests);
  group('compileSvg rejects risky features', _rejectFeatureTests);
  group('compileSvg complexity limits', _complexityTests);
}

void _isSvgSourceTests() {
  const url = 'https://player.example/api/v1/media/1/thumbnail';

  test('recognises the file name extension in any case', () {
    expect(isSvgSource(fileName: 'logo.svg', url: url), isTrue);
    expect(isSvgSource(fileName: 'LOGO.SVG ', url: url), isTrue);
  });

  test('recognises the URL path extension, ignoring the query', () {
    expect(isSvgSource(url: 'https://player.example/logo.svg?v=2'), isTrue);
  });

  test('rejects bitmaps, .svgz and names that merely contain svg', () {
    expect(isSvgSource(fileName: 'photo.jpg', url: url), isFalse);
    expect(isSvgSource(fileName: 'logo.svgz', url: url), isFalse);
    expect(isSvgSource(fileName: 'svg-export.png', url: url), isFalse);
    expect(isSvgSource(fileName: null, url: url), isFalse);
    expect(isSvgSource(url: 'https://player.example/a.png?n=x.svg'), isFalse);
  });
}

void _decodeTests() {
  Uint8List utf16(String text, Endian endian) {
    final data = ByteData(2 + text.length * 2)..setUint16(0, 0xfeff, endian);
    for (var i = 0; i < text.length; i++) {
      data.setUint16(2 + i * 2, text.codeUnitAt(i), endian);
    }
    return data.buffer.asUint8List();
  }

  test('decodes UTF-8 with and without a byte order mark', () {
    expect(decodeSvgText(svgBytes(kValidSvg)), kValidSvg);
    final withBom =
        Uint8List.fromList([0xef, 0xbb, 0xbf, ...svgBytes(kValidSvg)]);
    expect(decodeSvgText(withBom), kValidSvg);
  });

  test('decodes UTF-16 in both byte orders', () {
    expect(decodeSvgText(utf16(kValidSvg, Endian.little)), kValidSvg);
    expect(decodeSvgText(utf16(kValidSvg, Endian.big)), kValidSvg);
  });

  test('accepts an XML declaration and doctype before the root', () {
    final document = '<?xml version="1.0"?>\n<!DOCTYPE svg>\n$kValidSvg';
    expect(decodeSvgText(svgBytes(document)), document);
  });

  test('rejects content that is not SVG', () {
    const png = [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 13];
    const gzip = [0x1f, 0x8b, 0x08, 0, 0, 0, 0, 0, 0, 3];
    for (final bytes in [
      Uint8List.fromList(png),
      Uint8List.fromList(gzip),
      svgBytes('{"error":"not found"}'),
      svgBytes('<html><body>Login</body></html>'),
      Uint8List(0),
    ]) {
      expect(looksLikeSvg(bytes), isFalse);
      expect(() => decodeSvgText(bytes), _rejects('Not an SVG'));
    }
  });

  test('rejects oversized input', () {
    final big = Uint8List(kMaxSvgBytes + 1)..setAll(0, svgBytes(kValidSvg));
    expect(() => decodeSvgText(big), _rejects('too large'));
  });
}

void _acceptTests() {
  test('a valid drawing compiles to vector_graphics data', () {
    expect(compileSvg(svgBytes(kValidSvg)), isNotEmpty);
  });

  test('a viewBox alone gives the drawing its size', () {
    final document = svgDocument(size: 'viewBox="0 0 24 24"');
    expect(compileSvg(svgBytes(document)), isNotEmpty);
  });

  test('a very large but finite size is fine: nothing is rasterised', () {
    final document = svgDocument(size: 'width="20000" height="20000"');
    expect(compileSvg(svgBytes(document)), isNotEmpty);
  });

  test('gradients, group opacity and text are supported', () {
    final document = svgDocument(
        body: '<defs><linearGradient id="g"><stop offset="0" '
            'stop-color="#f00"/><stop offset="1" stop-color="#00f"/>'
            '</linearGradient></defs>'
            '<g opacity="0.5"><rect width="10" height="10" fill="url(#g)"/></g>'
            '<text x="1" y="8" font-size="4">Hi</text>');
    expect(compileSvg(svgBytes(document)), isNotEmpty);
  });

  test('CSS classes are ignored by the compiler, not an error', () {
    // Documented limit: the rect is drawn with the default fill.
    final document = svgDocument(
        body: '<style>.a{fill:#00f}</style>'
            '<rect class="a" width="10" height="10"/>');
    expect(compileSvg(svgBytes(document)), isNotEmpty);
  });
}

void _rejectShapeTests() {
  test('malformed XML', () => _expectRejected(kMalformedSvg, 'Invalid SVG'));

  test('zero, negative and oversized dimensions', () {
    for (final size in [
      'width="0" height="0"',
      'width="-5" height="10"',
      'width="10" height="-5"',
      'width="200000" height="10"',
    ]) {
      _expectRejected(svgDocument(size: size), 'unusable size');
    }
  });

  test('a size that becomes zero when stored as a 32-bit float', () {
    _expectRejected(
        svgDocument(size: 'viewBox="0 0 1e-300 1e-300"'), 'unusable size');
    _expectRejected(
        svgDocument(size: 'width="0.001" height="10"'), 'unusable size');
  });

  test('a root without width, height or viewBox (compiler limit)', () {
    _expectRejected(svgDocument(size: ''), 'Invalid SVG');
  });

  test('a drawing with nothing to draw', () {
    _expectRejected(svgDocument(body: ''), 'nothing to draw');
    _expectRejected(svgDocument(body: '<g></g>'), 'nothing to draw');
  });

  test('a drawing whose only content is an external image', () {
    const image = '<image width="10" height="10" '
        'href="http://tracker.example/pixel.png"/>';
    _expectRejected(svgDocument(body: image), 'nothing to draw');
  });
}

void _rejectFeatureTests() {
  test('a pattern fill, whose tile the renderer would rasterise', () {
    // The renderer allocates a 16000x16000 bitmap for this tile.
    final document = svgDocument(
        size: 'width="100" height="100" viewBox="0 0 16000 16000"',
        body: '<defs><pattern id="p" width="16000" height="16000" '
            'patternUnits="userSpaceOnUse">'
            '<rect width="8000" height="8000" fill="#f00"/></pattern></defs>'
            '<rect width="16000" height="16000" fill="url(#p)"/>');
    _expectRejected(document, 'pattern fills are not supported');
  });

  test('a small pattern fill is rejected as well', () {
    final document = svgDocument(
        body: '<defs><pattern id="p" width="2" height="2" '
            'patternUnits="userSpaceOnUse">'
            '<rect width="1" height="1" fill="#f00"/></pattern></defs>'
            '<rect width="10" height="10" fill="url(#p)"/>');
    _expectRejected(document, 'pattern fills are not supported');
  });

  test('an embedded bitmap, alone or next to vector shapes', () {
    final image = embeddedImage(Uint8List.fromList([1, 2, 3, 4]));
    _expectRejected(
        svgDocument(body: image), 'embedded bitmaps are not supported');
    _expectRejected(svgDocument(body: '<rect width="5" height="5"/>$image'),
        'embedded bitmaps are not supported');
  });

  test('text with an absurd font size', () {
    _expectRejected(
        svgDocument(body: '<text x="1" y="8" font-size="1000000">Hi</text>'),
        'unusable font size');
  });
}

void _complexityTests() {
  Uint8List compile(String body, SvgLimits limits) =>
      compileSvg(svgBytes(svgDocument(body: body)), limits: limits);

  test('more drawing commands than allowed', () {
    final body = '<rect width="1" height="1"/>' * 5;
    expect(() => compile(body, const SvgLimits(maxCommands: 4)),
        _rejects('too complex'));
    expect(compile(body, const SvgLimits(maxCommands: 5)), isNotEmpty);
  });

  test('more opacity layers than allowed', () {
    const layer = '<g opacity="0.5"><rect width="1" height="1"/>'
        '<rect width="2" height="2"/></g>';
    expect(() => compile(layer * 3, const SvgLimits(maxLayers: 2)),
        _rejects('too complex'));
    expect(compile(layer * 2, const SvgLimits(maxLayers: 2)), isNotEmpty);
  });

  test('a single path too large once compiled', () {
    // One command, many segments: only the compiled size bounds it.
    final path = '<path d="M0 0${' l1 1 l-1 0' * 2000}" fill="#f00"/>';
    expect(() => compile(path, const SvgLimits(maxCompiledBytes: 4096)),
        _rejects('too complex'));
    expect(compile(path, const SvgLimits()), isNotEmpty);
  });

  test('nested <use> references that multiply are refused quickly', () {
    // 10^20 shapes if expanded.
    final levels = [
      '<g id="l0"><rect width="1" height="1"/></g>',
      for (var i = 1; i <= 20; i++)
        '<g id="l$i">${'<use href="#l${i - 1}"/>' * 10}</g>',
    ];
    final watch = Stopwatch()..start();
    expect(
        () => compile('<defs>${levels.join()}</defs><use href="#l20"/>',
            const SvgLimits()),
        throwsA(isA<SvgException>()));
    expect(watch.elapsed, lessThan(const Duration(seconds: 5)));
  });

  test('the default compiled size limit matches a cacheable entry', () {
    expect(const SvgLimits().maxCompiledBytes, lessThanOrEqualTo(4 << 20));
  });
}
