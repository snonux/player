// Tests for rasterising SVGs once (svg_raster.dart), the bitmap cache
// (svg_cache.dart) and the count of drawing operations that decides how
// large a bitmap may be (svg_raster_cost.dart).

import 'dart:ui' as ui;

import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/svg_cache.dart';
import 'package:player_android/services/svg_document.dart';
import 'package:player_android/services/svg_raster.dart';

import '../support/svg_test_support.dart';

final _source = SvgRequest(uri: Uri.parse('https://player.example/a.svg'));

SvgImageRequest _box(double width, double height, {bool cover = false}) =>
    SvgImageRequest(
        source: _source, width: width, height: height, cover: cover);

/// Compiles [size]/[body] and rasterises it for [request].
Future<SvgRaster> _raster(SvgImageRequest request,
    {String size = 'width="20" height="10"', String? body}) {
  final document = body == null
      ? svgDocument(size: size)
      : svgDocument(size: size, body: body);
  return rasterizeSvg(compileSvg(svgBytes(document)), request);
}

Matcher _rejects(String message) => throwsA(
    isA<SvgException>().having((e) => e.message, 'message', contains(message)));

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  group('SvgImageRequest', _requestTests);
  group('rasterizeSvg', _rasterTests);
  group('SvgImageCache', _cacheTests);
  group('drawing operations', _costTests);
  group('raster budget', _budgetTests);
}

void _requestTests() {
  test('rounds box sizes up to a few steps', () {
    expect(SvgImageRequest.bucket(1), 64);
    expect(SvgImageRequest.bucket(144), 192);
    expect(SvgImageRequest.bucket(192), 192);
    expect(SvgImageRequest.bucket(193), 256);
    expect(SvgImageRequest.bucket(1440), 1536);
    expect(SvgImageRequest.bucket(3120), 2048);
    expect(SvgImageRequest.bucket(1e9), SvgImageRequest.maxSide);
  });

  test('an unbounded or senseless box gets a middle size', () {
    for (final pixels in [double.infinity, double.nan, 0.0, -5.0]) {
      expect(SvgImageRequest.bucket(pixels), 512);
    }
  });

  test('boxes of nearly the same size share a request', () {
    expect(_box(130, 130), _box(144, 144));
    expect(_box(130, 130).hashCode, _box(144, 144).hashCode);
  });

  test('a list tile and the full screen never share a bitmap', () {
    expect(_box(144, 144), isNot(_box(1440, 3120)));
    expect(_box(144, 144), isNot(_box(144, 144, cover: true)));
    final other = SvgRequest(uri: Uri.parse('https://player.example/b.svg'));
    expect(_box(144, 144),
        isNot(SvgImageRequest(source: other, width: 144, height: 144)));
  });
}

void _rasterTests() {
  test('fits the drawing into the box and keeps its aspect ratio', () async {
    final raster = await _raster(_box(192, 192));
    expect((raster.image.width, raster.image.height), (192, 96));
    expect(raster.size, const ui.Size(20, 10));
    raster.dispose();
  });

  test('covers the box when asked to', () async {
    final raster = await _raster(_box(192, 192, cover: true));
    expect((raster.image.width, raster.image.height), (384, 192));
    raster.dispose();
  });

  test('a tile gets a small bitmap, the screen a large one', () async {
    final tile = await _raster(_box(144, 144));
    final screen = await _raster(_box(1440, 3120));
    expect(tile.bytes, lessThan(100 * 1024));
    expect(screen.image.width, 1536);
    tile.dispose();
    screen.dispose();
  });

  test('no side exceeds the cap, whatever the box or the fit', () async {
    final raster = await _raster(_box(1e6, 1e6, cover: true));
    expect((raster.image.width, raster.image.height), (2048, 1024));
    expect(raster.bytes, lessThanOrEqualTo(2048 * 2048 * 4));
    raster.dispose();
  });

  test('the bitmap size ignores the size the document declares', () async {
    final raster =
        await _raster(_box(64, 64), size: 'width="20000" height="20000"');
    expect((raster.image.width, raster.image.height), (64, 64));
    raster.dispose();
  });

  test('an extreme aspect ratio still gives at least one pixel', () async {
    final raster = await _raster(_box(64, 64),
        size: 'width="1000" height="0.5"',
        body: '<rect width="1000" height="0.5" fill="#f00"/>');
    expect((raster.image.width, raster.image.height), (64, 1));
    raster.dispose();
  });
}

void _cacheTests() {
  Future<SvgRaster> image() => _raster(_box(64, 64));
  SvgImageRequest key(int id) => SvgImageRequest(
      source: SvgRequest(uri: Uri.parse('https://player.example/$id')),
      width: 64,
      height: 64);

  test('keeps its own handle: the caller may dispose first', () async {
    final cache = SvgImageCache();
    final raster = await image();
    cache.put(key(1), raster);
    raster.dispose();

    final again = cache.get(key(1))!;
    expect(again.image.debugDisposed, isFalse);
    expect(again.image.width, 64);
    again.dispose();
    // The handle given out was the caller's; the cache still has its own.
    expect(cache.get(key(1)), isNotNull);
  });

  test('is bounded by the number of images', () async {
    final cache = SvgImageCache(maxImages: 2);
    final raster = await image();
    for (var id = 1; id <= 3; id++) {
      cache.put(key(id), raster);
    }
    expect(cache.length, 2);
    expect(cache.get(key(1)), isNull);
    expect(cache.get(key(3)), isNotNull);
  });

  test('is bounded by bytes', () async {
    final raster = await image();
    final cache = SvgImageCache(maxBytes: raster.bytes * 2);
    for (var id = 1; id <= 5; id++) {
      cache.put(key(id), raster);
    }
    expect(cache.length, 2);
  });

  test('evicts the least recently used image', () async {
    final cache = SvgImageCache(maxImages: 2);
    final raster = await image();
    cache
      ..put(key(1), raster)
      ..put(key(2), raster);
    cache.get(key(1))!.dispose();
    cache.put(key(3), raster);
    expect(cache.get(key(2)), isNull);
    expect(cache.get(key(1)), isNotNull);
  });

  test('clear releases every bitmap', () async {
    final cache = SvgImageCache();
    final raster = await image();
    cache.put(key(1), raster);
    final held = cache.get(key(1))!;
    cache.clear();
    expect(cache.length, 0);
    expect(cache.get(key(1)), isNull);
    // Handles already given out stay valid for their owners.
    expect(held.image.debugDisposed, isFalse);
  });
}

void _costTests() {
  const full = '<rect width="100" height="100" fill="#f00"/>';
  const gradient = '<defs><linearGradient id="g"><stop offset="0" '
      'stop-color="#f00"/><stop offset="1" stop-color="#00f"/>'
      '</linearGradient></defs>';
  double operations(String body) => compileSvg(
          svgBytes(svgDocument(size: 'width="100" height="100"', body: body)))
      .drawOperations;

  test('a fill is one operation plus a term for its outline', () {
    // A rectangle has two edges crossing the full height: 2 x 0.25.
    expect(operations(full), closeTo(1.5, 0.01));
    expect(operations(full * 10), closeTo(15, 0.1));
  });

  test('a gradient costs twice a flat colour', () {
    final flat = operations(full);
    final shaded =
        operations('$gradient<rect width="100" height="100" fill="url(#g)"/>');
    expect(shaded, closeTo(2 * flat, 0.01));
  });

  test('a stroke is charged in addition to the fill', () {
    const stroked = '<rect width="100" height="100" fill="#f00" '
        'stroke="#000" stroke-width="2"/>';
    expect(operations(stroked), greaterThan(operations(full) + 1));
  });

  test('a wide stroke on a tiny or off-drawing line is a full operation', () {
    // The centre line has no length inside the drawing, yet the stroke
    // paints all of it.
    const dot = '<line x1="50" y1="50" x2="50.001" y2="50" stroke="#000" '
        'stroke-opacity="0.5" stroke-width="1000" stroke-linecap="round"/>';
    const outside = '<line x1="-400" y1="-400" x2="-300" y2="-400" '
        'stroke="#000" stroke-width="1000" stroke-linecap="square"/>';
    expect(operations(dot), greaterThanOrEqualTo(1));
    expect(operations(outside), greaterThanOrEqualTo(1));
    expect(operations(dot * 200), greaterThanOrEqualTo(200));
  });

  test('every character of text is an operation', () {
    final one = operations('$full<text y="50" font-size="40">a</text>');
    final many =
        operations('$full<text y="50" font-size="40">${'a' * 21}</text>');
    expect(many - one, closeTo(20, 0.01));
  });

  test('layers and clips are one operation each', () {
    const small = '<rect width="1" height="1"/>';
    final plain = operations('<g>$small$small</g>');
    expect(operations('<g opacity="0.5">$small$small</g>'),
        closeTo(plain + 1, 0.01));
    final clipped = operations('<defs><clipPath id="c">$small</clipPath>'
        '</defs><g>${small.replaceFirst('/>', ' clip-path="url(#c)"/>')}'
        '$small</g>');
    expect(clipped, greaterThanOrEqualTo(plain + 1));
  });

  test('copies made by use are counted like the original', () {
    final copies =
        operations('<defs><g id="g">$full</g></defs>${'<use href="#g"/>' * 6}');
    expect(copies, closeTo(6 * operations(full), 0.01));
  });

  test('an outline crossing the drawing many times costs many fills', () {
    String zigzag(int crossings) => '<path fill-rule="evenodd" d="M0 0${[
          for (var i = 0; i < crossings; i++) 'L$i ${i.isEven ? 100 : 0}',
        ].join()}"/>';
    // About a quarter of a fill per crossing.
    expect(operations(zigzag(900)), closeTo(226, 3));
    // Edges outside the drawing are not scanned and cost nothing.
    const outside = '<path d="M0 0 L1e6 1e6 L-1e6 1e6 L0 -1e6 Z"/>';
    expect(operations(outside), lessThan(3));
  });

  test('a drawing with more operations than allowed is refused', () {
    final bytes = svgBytes(
        svgDocument(size: 'width="100" height="100"', body: full * 10));
    expect(
        compileSvg(bytes, limits: const SvgLimits(maxDrawOperations: 15))
            .drawOperations,
        closeTo(15, 0.01));
    expect(
        () =>
            compileSvg(bytes, limits: const SvgLimits(maxDrawOperations: 14.9)),
        _rejects('too expensive to draw'));
  });
}

void _budgetTests() {
  const screen = ui.Size(1536, 1536);

  test('a drawing with few operations keeps the size its box asks for', () {
    expect(svgRasterSizeInBudget(screen, 100), screen);
    expect(svgRasterSizeInBudget(const ui.Size(2048, 2048), 71),
        const ui.Size(2048, 2048));
  });

  test('more operations give a smaller bitmap, at one of the size steps', () {
    expect(svgRasterSizeInBudget(screen, 200), const ui.Size(1024, 1024));
    expect(svgRasterSizeInBudget(screen, 1000), const ui.Size(512, 512));
    expect(svgRasterSizeInBudget(screen, 4500), const ui.Size(256, 256));
    expect(svgRasterSizeInBudget(const ui.Size(945, 2048), 400),
        const ui.Size(472.5, 1024));
  });

  test('the product of operations and pixels stays within the budget', () {
    for (final operations in <double>[1, 71, 72, 150, 999, 4577]) {
      for (final wanted in [screen, const ui.Size(2048, 945), screen / 4]) {
        final size = svgRasterSizeInBudget(wanted, operations);
        expect(operations * size.width * size.height,
            lessThanOrEqualTo(kSvgRasterBudget),
            reason: '$operations operations at $wanted');
      }
    }
  });

  test('a bitmap is never shrunk below the minimum side', () {
    expect(svgRasterSizeInBudget(const ui.Size(192, 192), 4577),
        const ui.Size(192, 192));
    expect(svgRasterSizeInBudget(screen, 4577).longestSide, 256);
  });

  test('rasterizeSvg applies the budget', () async {
    const full = '<rect width="100" height="100" fill="#f00"/>';
    final raster = await _raster(_box(1536, 1536),
        size: 'width="100" height="100"', body: full * 400);
    // 600 operations: 512 x 512 is the largest step within the budget.
    expect((raster.image.width, raster.image.height), (512, 512));
    raster.dispose();
  });
}
