// Unit tests for SVG detection, text decoding and validation
// (svg_document.dart). No Flutter binding is needed: this code runs in a
// background isolate in the app.

import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/svg_document.dart';

import '../support/svg_test_support.dart';

Matcher _rejects(String message) => throwsA(
    isA<SvgException>().having((e) => e.message, 'message', contains(message)));

void main() {
  group('isSvgSource', _isSvgSourceTests);
  group('decodeSvgText', _decodeTests);
  group('decodeSvgText', _decodeRejectTests);
  group('compileSvg accepts', _acceptTests);
  group('compileSvg rejects', _rejectTests);
}

void _isSvgSourceTests() {
  const url = 'https://player.example/api/v1/media/1/thumbnail';

  test('recognises the file name extension in any case', () {
    expect(isSvgSource(fileName: 'logo.svg', url: url), isTrue);
    expect(isSvgSource(fileName: 'LOGO.SVG ', url: url), isTrue);
    expect(isSvgSource(fileName: 'logo.svgz', url: url), isTrue);
  });

  test('recognises the URL path extension, ignoring the query', () {
    expect(isSvgSource(url: 'https://player.example/logo.svg?v=2'), isTrue);
  });

  test('rejects bitmaps and names that merely contain svg', () {
    expect(isSvgSource(fileName: 'photo.jpg', url: url), isFalse);
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

  test('decompresses gzip (.svgz)', () {
    final zipped = Uint8List.fromList(gzip.encode(utf8.encode(kValidSvg)));
    expect(decodeSvgText(zipped), kValidSvg);
  });

  test('accepts an XML declaration and doctype before the root', () {
    final document = '<?xml version="1.0"?>\n<!DOCTYPE svg>\n$kValidSvg';
    expect(decodeSvgText(svgBytes(document)), document);
  });
}

void _decodeRejectTests() {
  test('rejects content that is not SVG', () {
    const png = [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 13];
    expect(
        () => decodeSvgText(Uint8List.fromList(png)), _rejects('Not an SVG'));
    expect(() => decodeSvgText(svgBytes('{"error":"not found"}')),
        _rejects('Not an SVG'));
    expect(() => decodeSvgText(svgBytes('<html><body>Login</body></html>')),
        _rejects('Not an SVG'));
    expect(() => decodeSvgText(Uint8List(0)), _rejects('Not an SVG'));
  });

  test('rejects oversized input and gzip that expands past the limit', () {
    expect(() => decodeSvgText(Uint8List(kMaxSvgBytes + 1)),
        _rejects('too large'));
    // 64 MB of spaces compress to a few kilobytes.
    final bomb = Uint8List.fromList(gzip.encode(Uint8List(64 * 1024 * 1024)));
    expect(bomb.length, lessThan(kMaxSvgBytes));
    expect(() => decodeSvgText(bomb), _rejects('too large'));
  });
}

void _acceptTests() {
  test('a valid drawing compiles to vector_graphics data', () {
    final compiled = compileSvg(svgBytes(kValidSvg));
    expect(compiled.data, isNotEmpty);
    expect(compiled.images, isEmpty);
  });

  test('a viewBox alone gives the drawing its size', () {
    final document = svgDocument(size: 'viewBox="0 0 24 24"');
    expect(compileSvg(svgBytes(document)).data, isNotEmpty);
  });

  test('a very large but finite size is fine: nothing is rasterised', () {
    final document = svgDocument(size: 'width="20000" height="20000"');
    expect(compileSvg(svgBytes(document)).data, isNotEmpty);
  });

  test('embedded bitmaps are returned for inspection', () {
    final document =
        svgDocument(body: embeddedImage(Uint8List.fromList([1, 2, 3, 4])));
    expect(compileSvg(svgBytes(document)).images.single, [1, 2, 3, 4]);
  });

  test('CSS classes are ignored by the compiler, not an error', () {
    // Documented limit: the rect is drawn with the default fill.
    final document = svgDocument(
        body: '<style>.a{fill:#00f}</style>'
            '<rect class="a" width="10" height="10"/>');
    expect(compileSvg(svgBytes(document)).data, isNotEmpty);
  });
}

void _rejectTests() {
  void expectRejected(String document, String message) =>
      expect(() => compileSvg(svgBytes(document)), _rejects(message));

  test('malformed XML', () => expectRejected(kMalformedSvg, 'Invalid SVG'));

  test('truncated gzip data', () {
    // The decoder may hand back the part it could read; that part is then
    // an incomplete document, so the file is rejected either way.
    final zipped = gzip.encode(utf8.encode(kValidSvg));
    final cut = Uint8List.fromList(zipped.sublist(0, zipped.length - 12));
    expect(() => compileSvg(cut), throwsA(isA<SvgException>()));
  });

  test('zero, negative and oversized dimensions', () {
    for (final size in [
      'width="0" height="0"',
      'width="-5" height="10"',
      'width="10" height="-5"',
      'width="200000" height="10"',
    ]) {
      expectRejected(svgDocument(size: size), 'unusable size');
    }
  });

  test('a root without width, height or viewBox (compiler limit)', () {
    expectRejected(svgDocument(size: ''), 'Invalid SVG');
  });

  test('a drawing with nothing to draw', () {
    expectRejected(svgDocument(body: ''), 'nothing to draw');
    expectRejected(svgDocument(body: '<g></g>'), 'nothing to draw');
  });

  test('a drawing whose only content is an external image', () {
    const image = '<image width="10" height="10" '
        'href="http://tracker.example/pixel.png"/>';
    expectRejected(svgDocument(body: image), 'nothing to draw');
  });

  test('more drawing commands than allowed', () {
    final document = svgDocument(body: '<rect width="1" height="1"/>' * 5);
    expect(() => compileSvg(svgBytes(document), maxCommands: 4),
        _rejects('too complex'));
    expect(compileSvg(svgBytes(document), maxCommands: 5).data, isNotEmpty);
  });
}
