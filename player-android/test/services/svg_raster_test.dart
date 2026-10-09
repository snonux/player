// Tests for rasterising SVGs once (svg_raster.dart), the bitmap cache
// (svg_cache.dart) and the raster cost estimate (svg_raster_cost.dart).

import 'dart:typed_data';
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
  final compiled = compileSvg(svgBytes(document));
  return rasterizeSvg(ByteData.sublistView(compiled), request);
}

Matcher _rejects(String message) => throwsA(
    isA<SvgException>().having((e) => e.message, 'message', contains(message)));

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  group('SvgImageRequest', _requestTests);
  group('rasterizeSvg', _rasterTests);
  group('SvgImageCache', _cacheTests);
  group('raster cost', _costTests);
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
  Uint8List compile(String body, SvgLimits limits) => compileSvg(
      svgBytes(svgDocument(size: 'width="100" height="100"', body: body)),
      limits: limits);

  test('coverage adds up the area every shape paints', () {
    expect(compile(full * 5, const SvgLimits(maxCoverage: 5)), isNotEmpty);
    expect(() => compile(full * 6, const SvgLimits(maxCoverage: 5)),
        _rejects('too expensive to draw'));
  });

  test('small shapes cost little, shapes outside the drawing nothing', () {
    const small = '<rect width="10" height="10"/>';
    const outside = '<rect x="1e6" y="1e6" width="1e9" height="1e9"/>';
    const limits = SvgLimits(maxCoverage: 1.5);
    expect(compile(full + small * 40, limits), isNotEmpty);
    expect(compile(full + outside * 500, limits), isNotEmpty);
  });

  test('a stroke costs its length times its width', () {
    // 100 long, 50 wide: half the drawing per line.
    const line = '<line y1="50" x2="100" y2="50" stroke="#000" '
        'stroke-width="50"/>';
    expect(compile(line * 4, const SvgLimits(maxCoverage: 2)), isNotEmpty);
    expect(() => compile(line * 5, const SvgLimits(maxCoverage: 2)),
        _rejects('too expensive to draw'));
  });

  test('an offscreen layer costs the whole drawing', () {
    const layer = '<g opacity="0.5"><rect width="1" height="1"/>'
        '<rect width="2" height="2"/></g>';
    expect(compile(layer * 3, const SvgLimits(maxCoverage: 3.5)), isNotEmpty);
    expect(() => compile(layer * 4, const SvgLimits(maxCoverage: 3.5)),
        _rejects('too expensive to draw'));
  });

  test('outline length counts how often edges cross the drawing', () {
    // Each crossing is about 100 units; the diagonal is 141.
    String zigzag(int crossings) => '<path d="M0 0${[
          for (var i = 0; i < crossings; i++) 'L$i ${i.isEven ? 100 : 0}',
        ].join()}"/>';
    const limits = SvgLimits(maxOutlineLength: 10);
    expect(compile(zigzag(12), limits), isNotEmpty);
    expect(
        () => compile(zigzag(20), limits), _rejects('too expensive to draw'));
  });

  test('copies made by use are measured like the original', () {
    final body = '<defs><g id="g">$full</g></defs>${'<use href="#g"/>' * 6}';
    expect(() => compile(body, const SvgLimits(maxCoverage: 5)),
        _rejects('too expensive to draw'));
  });
}
