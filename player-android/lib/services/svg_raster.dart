import 'dart:math' as math;
import 'dart:ui' as ui;

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:vector_graphics/vector_graphics.dart';

import 'svg_cache.dart';
import 'svg_limits.dart';

/// Turns compiled SVG bytes into a picture clipped to the SVG's view box.
///
/// Decoding only records drawing commands: the features that would decode
/// or allocate bitmaps here (patterns, embedded images) cannot get this
/// far, see `svg_document.dart`.
Future<PictureInfo> decodeSvgPicture(ByteData data) =>
    vg.loadPicture(_CompiledSvgLoader(data), null);

/// Rasterises a compiled SVG once, for the box described by [request].
///
/// The app paints this bitmap from then on and never replays the drawing
/// itself: replaying happens on the raster thread for every frame that
/// repaints, and a drawing within all budgets can take a second per pass.
/// As a bitmap that cost is paid once, off the UI isolate, and scrolling or
/// zooming afterwards costs what any image costs.
///
/// The bitmap has the aspect ratio of the drawing. Its size is the smaller
/// of what the box asks for (see [svgRasterScale]) and what the drawing's
/// operations allow (see [svgRasterSizeInBudget]).
Future<SvgRaster> rasterizeSvg(
  CompiledSvg compiled,
  SvgImageRequest request,
) async {
  final info = await decodeSvgPicture(ByteData.sublistView(compiled.data));
  try {
    final size = info.size;
    final wanted = size * svgRasterScale(size, request);
    final pixels = svgRasterSizeInBudget(wanted, compiled.drawOperations);
    final width = math.max(1, pixels.width.round());
    final height = math.max(1, pixels.height.round());
    final recorder = ui.PictureRecorder();
    ui.Canvas(recorder)
      ..scale(pixels.width / size.width, pixels.height / size.height)
      ..drawPicture(info.picture);
    final scaled = recorder.endRecording();
    try {
      return SvgRaster(await scaled.toImage(width, height), size);
    } finally {
      scaled.dispose();
    }
  } finally {
    info.picture.dispose();
  }
}

/// Pixels per SVG unit for a drawing of [size] shown in [request]'s box:
/// scaled to fit the box (or to cover it), with neither side above
/// [SvgImageRequest.maxSide].
@visibleForTesting
double svgRasterScale(ui.Size size, SvgImageRequest request) {
  final horizontal = request.width / size.width;
  final vertical = request.height / size.height;
  final wanted = request.cover
      ? math.max(horizontal, vertical)
      : math.min(horizontal, vertical);
  final cap = SvgImageRequest.maxSide / math.max(size.width, size.height);
  return math.min(wanted, cap);
}

/// Shrinks a bitmap of [wanted] pixels until [operations] times its pixel
/// count is within [kSvgRasterBudget].
///
/// The product tracks the time to rasterise: the operation weights are
/// measured, not proven (see svg_raster_cost.dart for what they simplify).
/// A drawing with few operations
/// keeps the size its box asks for; one with many is made at the largest
/// of the usual size steps that fits the budget, but not below
/// [kMinSvgRasterSide] on its longer side: the compiler has already
/// refused drawings that would be over budget even there.
@visibleForTesting
ui.Size svgRasterSizeInBudget(ui.Size wanted, double operations) {
  bool affordable(ui.Size size) =>
      operations * size.width * size.height <= kSvgRasterBudget;
  if (affordable(wanted) || wanted.longestSide <= kMinSvgRasterSide) {
    return wanted;
  }
  final smaller = SvgImageRequest.steps.reversed
      .where((step) => step < wanted.longestSide && step >= kMinSvgRasterSide)
      .map((step) => wanted * (step / wanted.longestSide));
  return smaller.firstWhere(affordable, orElse: () => smaller.last);
}

/// Hands already compiled bytes to vector_graphics. Identity-based equality
/// keeps the decoder's bookkeeping for different drawings apart.
class _CompiledSvgLoader extends BytesLoader {
  const _CompiledSvgLoader(this.data);

  final ByteData data;

  @override
  Future<ByteData> loadBytes(BuildContext? context) =>
      SynchronousFuture<ByteData>(data);
}
