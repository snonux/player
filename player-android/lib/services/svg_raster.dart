import 'dart:math' as math;
import 'dart:ui' as ui;

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:vector_graphics/vector_graphics.dart';

import 'svg_cache.dart';

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
/// repaints, and a drawing within all budgets can take a second or more per
/// pass. As a bitmap that cost is paid once, off the UI isolate, and
/// scrolling or zooming afterwards costs what any image costs.
///
/// The bitmap has the aspect ratio of the drawing, scaled to fit the box
/// (or to cover it), with neither side above [SvgImageRequest.maxSide].
Future<SvgRaster> rasterizeSvg(ByteData data, SvgImageRequest request) async {
  final info = await decodeSvgPicture(data);
  try {
    final size = info.size;
    final scale = svgRasterScale(size, request);
    final width = math.max(1, (size.width * scale).round());
    final height = math.max(1, (size.height * scale).round());
    final recorder = ui.PictureRecorder();
    ui.Canvas(recorder)
      ..scale(scale)
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

/// Pixels per SVG unit for a drawing of [size] shown in [request]'s box.
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

/// Hands already compiled bytes to vector_graphics. Identity-based equality
/// keeps the decoder's bookkeeping for different drawings apart.
class _CompiledSvgLoader extends BytesLoader {
  const _CompiledSvgLoader(this.data);

  final ByteData data;

  @override
  Future<ByteData> loadBytes(BuildContext? context) =>
      SynchronousFuture<ByteData>(data);
}
