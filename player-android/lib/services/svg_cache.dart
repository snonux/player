import 'dart:collection';
import 'dart:ui' as ui;

import 'package:flutter/foundation.dart';

import 'svg_limits.dart';

/// One SVG download: where from and with which credentials.
///
/// Two requests are equal only when URL and headers both match, so another
/// account (or a public share with another token) never reuses a picture.
/// [toString] is redacted because the URL may hold a share token and the
/// headers hold credentials, and Riverpod prints family arguments in logs.
@immutable
class SvgRequest {
  const SvgRequest({required this.uri, this.headers = const {}});

  final Uri uri;
  final Map<String, String> headers;

  @override
  bool operator ==(Object other) =>
      other is SvgRequest &&
      other.uri == uri &&
      mapEquals(other.headers, headers);

  @override
  int get hashCode => Object.hash(
        uri,
        Object.hashAllUnordered(
          headers.entries.map((e) => Object.hash(e.key, e.value)),
        ),
      );

  @override
  String toString() =>
      'SvgRequest(${uri.host}, #${hashCode.toRadixString(16)})';
}

/// Remembers, in memory, what recent SVG requests turned out to be.
///
/// Compiled drawings are kept least-recently-used first out: a grid card
/// that scrolls away releases its image, and without this scrolling back
/// would download and parse the file again. Every drawing the compiler
/// accepts fits (see `SvgLimits.maxCompiledBytes`). Compiled drawings are
/// much smaller than their bitmaps, so far more of them are kept than
/// [SvgImageCache] keeps images.
///
/// Requests whose content was not SVG at all are remembered too. Images
/// without a file name are probed for SVG content after the bitmap decoder
/// rejected them; without this note a corrupt bitmap would be downloaded
/// again every time it scrolls into view.
///
/// Download failures and rejected SVG documents are not remembered, so they
/// are retried on the next visit. A drawing is stored only after it was
/// decoded and rasterised successfully, so the cache never holds bytes
/// that cannot be shown.
///
/// There is deliberately no disk cache: the one used for bitmaps belongs to
/// a package this app does not depend on directly, and a second private
/// one would need its own eviction and per-account separation for small
/// files.
class SvgMemoryCache {
  SvgMemoryCache({this.maxBytes = 16 * 1024 * 1024, this.maxNotSvg = 512});

  final int maxBytes;
  final int maxNotSvg;
  int _bytes = 0;
  final LinkedHashMap<SvgRequest, CompiledSvg> _entries = LinkedHashMap();
  final LinkedHashSet<SvgRequest> _notSvg = LinkedHashSet();

  /// Counts how often the cache was cleared. Work that started before a
  /// clear (a logout) compares this before storing its result, so that it
  /// cannot put the previous account's drawing back.
  int get generation => _generation;
  int _generation = 0;

  /// Returns the entry and marks it as most recently used.
  CompiledSvg? get(SvgRequest request) {
    final compiled = _entries.remove(request);
    if (compiled != null) _entries[request] = compiled;
    return compiled;
  }

  void put(SvgRequest request, CompiledSvg compiled) {
    _bytes -= _entries.remove(request)?.data.length ?? 0;
    _entries[request] = compiled;
    _bytes += compiled.data.length;
    while (_bytes > maxBytes) {
      _bytes -= _entries.remove(_entries.keys.first)!.data.length;
    }
  }

  bool isKnownNotSvg(SvgRequest request) => _notSvg.contains(request);

  void markNotSvg(SvgRequest request) {
    _notSvg.add(request);
    if (_notSvg.length > maxNotSvg) _notSvg.remove(_notSvg.first);
  }

  /// Forgets everything.
  void clear() {
    _generation++;
    _entries.clear();
    _notSvg.clear();
    _bytes = 0;
  }
}

/// What an SVG is rasterised for: a box of [width] x [height] physical
/// pixels that the drawing must fit into, or cover when [cover] is set.
///
/// The sizes are rounded up to a few steps (see [bucket]), so that boxes of
/// nearly the same size share one bitmap while a small list tile and the
/// full-screen viewer never do: a thumbnail bitmap is not stretched over
/// the screen, and a screen-sized bitmap is not kept for a thumbnail.
@immutable
class SvgImageRequest {
  SvgImageRequest({
    required this.source,
    required double width,
    required double height,
    this.cover = false,
  })  : width = bucket(width),
        height = bucket(height);

  final SvgRequest source;
  final int width;
  final int height;
  final bool cover;

  /// No bitmap is wider or taller than this, whatever the box or the
  /// screen. The image viewer zooms into that bitmap.
  static const int maxSide = 2048;

  /// The bitmap sizes in use, smallest first.
  static const List<int> steps = [
    64, 96, 128, 192, 256, 384, 512, 768, 1024, 1536, maxSide, //
  ];

  /// The smallest step that is at least [pixels]; sizes that are not
  /// finite (an unbounded box) get a middle step.
  static int bucket(double pixels) {
    if (!pixels.isFinite || pixels <= 0) return 512;
    return steps.firstWhere((s) => s >= pixels, orElse: () => maxSide);
  }

  @override
  bool operator ==(Object other) =>
      other is SvgImageRequest &&
      other.source == source &&
      other.width == width &&
      other.height == height &&
      other.cover == cover;

  @override
  int get hashCode => Object.hash(source, width, height, cover);

  @override
  String toString() => 'SvgImageRequest($source, ${width}x$height)';
}

/// A rasterised SVG: the bitmap and the size the document declares, which
/// is what the drawing is laid out with when its box is unbounded.
class SvgRaster {
  const SvgRaster(this.image, this.size);

  final ui.Image image;
  final ui.Size size;

  int get bytes => image.width * image.height * 4;

  /// A second handle to the same pixels, to be disposed independently.
  SvgRaster clone() => SvgRaster(image.clone(), size);

  void dispose() => image.dispose();
}

/// Keeps the most recently used SVG bitmaps, bounded by count and by bytes.
///
/// Bitmaps are large (up to 16 MB for a full-screen drawing), so only a
/// few are kept beyond the ones on screen. A drawing that drops out is
/// rasterised again from its compiled form, without a download.
class SvgImageCache {
  SvgImageCache({this.maxImages = 32, this.maxBytes = 64 * 1024 * 1024});

  final int maxImages;
  final int maxBytes;
  int _bytes = 0;
  final LinkedHashMap<SvgImageRequest, SvgRaster> _entries = LinkedHashMap();

  int get length => _entries.length;

  /// A handle the caller owns and must dispose, or null.
  SvgRaster? get(SvgImageRequest request) {
    final raster = _entries.remove(request);
    if (raster == null) return null;
    _entries[request] = raster;
    return raster.clone();
  }

  /// Stores a handle of its own; [raster] stays the caller's.
  void put(SvgImageRequest request, SvgRaster raster) {
    _remove(request);
    _entries[request] = raster.clone();
    _bytes += raster.bytes;
    while (_entries.length > maxImages || _bytes > maxBytes) {
      _remove(_entries.keys.first);
    }
  }

  void _remove(SvgImageRequest request) {
    final raster = _entries.remove(request);
    if (raster == null) return;
    _bytes -= raster.bytes;
    raster.dispose();
  }

  /// Releases every bitmap.
  void clear() => _entries.keys.toList().forEach(_remove);

  /// Empties the cache and refuses whatever work in flight would store.
  void reset() {
    _generation++;
    clear();
  }

  /// See [SvgMemoryCache.generation].
  int get generation => _generation;
  int _generation = 0;
}
