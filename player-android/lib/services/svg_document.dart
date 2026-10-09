import 'dart:convert';
import 'dart:typed_data';

import 'package:vector_graphics_compiler/vector_graphics_compiler.dart';

// Turns the bytes of an SVG file into the vector_graphics binary format.
//
// Everything here is plain Dart without Flutter bindings, because it runs
// in a background isolate (see `svg_compiler.dart`). An SVG can come from a
// public share, so the document is treated as hostile input. The app shows
// simple vector drawings only: a document that is oversized, nonsensical or
// uses a feature that would allocate memory at a size the file chooses is
// rejected with an [SvgException], which the image widgets show as their
// error state.
//
// Rejected on purpose (see [_validate]):
//  * `<pattern>` fills: the renderer turns every pattern tile into a bitmap
//    of the size the file declares.
//  * Embedded bitmaps (`<image href="data:...">`): they are decoded at
//    their full pixel size, however small the drawing is shown.
//
// Limits of the compiler that cannot be fixed here:
//  * `<style>` blocks (CSS classes) are ignored, so a drawing styled only
//    through classes is painted with the default black fill.
//  * A root element with neither `width`/`height` nor `viewBox` is rejected
//    ("SVG did not specify dimensions") instead of getting the 300x150
//    default a browser would use.
//  * `<image>` elements pointing at a URL are skipped, so a shared SVG
//    cannot make the app contact third-party servers.
//  * Nested `<svg>` elements, scripts, animation and filters are ignored.
//  * Gzip-compressed `.svgz` is not supported; the server does not serve it.

/// Largest SVG download accepted. Vector drawings of the kind this app
/// shows are far smaller; the cap bounds the memory a list of thumbnails
/// can hold while their downloads wait to be compiled.
const int kMaxSvgBytes = 2 * 1024 * 1024;

/// Bounds for one SVG. The defaults apply in the app; tests pass smaller
/// values to reach a limit with a small document.
class SvgLimits {
  const SvgLimits({
    this.maxCommands = 100000,
    this.maxLayers = 64,
    this.maxCompiledBytes = 4 * 1024 * 1024,
  });

  /// Most drawing commands. The picture is replayed on the raster thread
  /// for every frame that repaints it, so the count must stay bounded.
  final int maxCommands;

  /// Most group opacity layers and masks. Each one is an offscreen buffer
  /// of up to screen size while the picture is rasterised.
  final int maxLayers;

  /// Largest compiled drawing. Commands are counted, path segments are not,
  /// so this is what bounds a single path with hundreds of thousands of
  /// segments. It equals the largest entry the memory cache keeps: anything
  /// accepted is cached and not compiled again each time it appears.
  final int maxCompiledBytes;
}

/// Smallest and largest accepted width or height in SVG user units.
const double kMinSvgDimension = 0.01;
const double kMaxSvgDimension = 100000;

/// Largest accepted font size in SVG user units.
const double kMaxSvgFontSize = 10000;

/// An SVG that cannot or must not be shown. [message] holds no document
/// content beyond what the compiler itself reports.
class SvgException implements Exception {
  const SvgException(this.message);

  final String message;

  @override
  String toString() => 'SvgException: $message';
}

/// True when [fileName] or the path of [url] names an SVG document.
///
/// The server picks the media type from the file extension and returns the
/// original SVG for both the stream and the thumbnail of such a file. Those
/// endpoint URLs carry no extension, so callers pass the media file name;
/// the URL path is checked as well for links that do end in `.svg`. Knowing
/// the type up front skips a bitmap decode that is bound to fail. Images
/// whose name is unknown (folder and set covers) are recognised by content
/// instead, see `bitmapErrorOrSvg`.
///
/// The name is trusted: a bitmap that was wrongly named `.svg` is rejected
/// as "not an SVG" and shows the error state. It is not retried as a
/// bitmap, because the server labels such a file as SVG too.
bool isSvgSource({String? fileName, required String url}) {
  bool hasSvgExtension(String? name) =>
      name != null && name.trim().toLowerCase().endsWith('.svg');
  return hasSvgExtension(fileName) || hasSvgExtension(Uri.tryParse(url)?.path);
}

/// True when an `<svg` tag opens within the first few kilobytes of [bytes],
/// which leaves room for an XML declaration, a doctype and a licence
/// comment. Bitmaps, JSON error bodies and HTML pages fail this test. Only
/// the head is decoded, so the check is cheap enough for the UI isolate.
bool looksLikeSvg(Uint8List bytes) {
  const headBytes = 8192;
  final head = bytes.length > headBytes
      ? Uint8List.sublistView(bytes, 0, headBytes)
      : bytes;
  return RegExp(r'<svg[\s>]').hasMatch(_decodeText(head));
}

/// Decodes the bytes of an SVG file to text: UTF-8 with or without a byte
/// order mark, or UTF-16 with a byte order mark. Throws [SvgException] for
/// input that is too large or does not start like an SVG document.
String decodeSvgText(Uint8List bytes) {
  if (bytes.length > kMaxSvgBytes) throw const SvgException('SVG too large');
  if (!looksLikeSvg(bytes)) throw const SvgException('Not an SVG document');
  return _decodeText(bytes);
}

String _decodeText(Uint8List bytes) {
  final hasUtf16Bom = bytes.length >= 2 &&
      ((bytes[0] == 0xff && bytes[1] == 0xfe) ||
          (bytes[0] == 0xfe && bytes[1] == 0xff));
  if (!hasUtf16Bom) {
    // The UTF-8 decoder drops a UTF-8 byte order mark by itself.
    return utf8.decode(bytes, allowMalformed: true);
  }
  final endian = bytes[0] == 0xff ? Endian.little : Endian.big;
  final data = ByteData.sublistView(bytes, 2);
  return String.fromCharCodes([
    for (var i = 0; i + 1 < data.lengthInBytes; i += 2)
      data.getUint16(i, endian),
  ]);
}

/// Parses and validates the SVG in [bytes] and returns it in the
/// vector_graphics binary format; throws when it must not be shown.
///
/// The compiler's optimizers stay off: they need native libraries that only
/// exist in build-time tooling. The document is parsed twice (once to
/// inspect it, once inside `encodeSvg`) because the compiler has no public
/// way to encode already parsed instructions.
Uint8List compileSvg(Uint8List bytes, {SvgLimits limits = const SvgLimits()}) {
  final xml = decodeSvgText(bytes);
  final VectorInstructions instructions;
  try {
    instructions = parseWithoutOptimizers(xml, key: 'network svg');
  } catch (error) {
    // The parser throws assorted StateError/FormatException/XML exceptions.
    throw SvgException('Invalid SVG: $error');
  }
  _validate(instructions, limits);
  final data = encodeSvg(
    xml: xml,
    debugName: 'network svg',
    enableMaskingOptimizer: false,
    enableClippingOptimizer: false,
    enableOverdrawOptimizer: false,
  );
  if (data.length > limits.maxCompiledBytes) {
    throw const SvgException('SVG is too complex');
  }
  return data;
}

/// Rejects drawings that would paint nothing, cost too much to paint, or
/// use a feature the renderer implements with file-sized bitmaps.
void _validate(VectorInstructions instructions, SvgLimits limits) {
  _validateSize(instructions);
  _validateFeatures(instructions);
  final commands = instructions.commands;
  int count(Set<DrawCommandType> types) =>
      commands.where((c) => types.contains(c.type)).length;
  const layers = {DrawCommandType.saveLayer, DrawCommandType.mask};
  if (commands.length > limits.maxCommands ||
      count(layers) > limits.maxLayers) {
    throw const SvgException('SVG is too complex');
  }
  // An empty `<svg>`, or one whose only content is an external image, would
  // otherwise show as a blank area.
  const visible = {
    DrawCommandType.path,
    DrawCommandType.vertices,
    DrawCommandType.text,
  };
  if (count(visible) == 0) {
    throw const SvgException('SVG has nothing to draw');
  }
}

/// A zero, negative, non-finite or absurd size cannot be laid out. The
/// binary format stores sizes as 32-bit floats, so the value is checked
/// after that narrowing: 1e-300 is positive as a double but 0 once stored.
void _validateSize(VectorInstructions instructions) {
  bool validExtent(double value) {
    final stored = Float32List(1)..[0] = value;
    return stored[0] >= kMinSvgDimension && stored[0] <= kMaxSvgDimension;
  }

  if (!validExtent(instructions.width) || !validExtent(instructions.height)) {
    throw const SvgException('SVG has an unusable size');
  }
}

/// See the file comment for why patterns and embedded bitmaps are refused.
/// Text is allowed with a bounded font size.
void _validateFeatures(VectorInstructions instructions) {
  final usesPattern = instructions.patternData.isNotEmpty ||
      instructions.commands.any((c) => c.type == DrawCommandType.pattern);
  if (usesPattern) {
    throw const SvgException('SVG pattern fills are not supported');
  }
  final usesBitmap = instructions.images.isNotEmpty ||
      instructions.drawImages.isNotEmpty ||
      instructions.commands.any((c) => c.type == DrawCommandType.image);
  if (usesBitmap) {
    throw const SvgException('SVG embedded bitmaps are not supported');
  }
  final fontSizes = instructions.text.map((t) => t.fontSize);
  if (fontSizes.any((s) => !(s > 0 && s <= kMaxSvgFontSize))) {
    throw const SvgException('SVG has an unusable font size');
  }
}
