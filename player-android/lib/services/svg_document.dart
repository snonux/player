import 'dart:convert';
import 'dart:typed_data';

import 'package:vector_graphics_compiler/vector_graphics_compiler.dart';

import 'svg_gate.dart';
import 'svg_limits.dart';

export 'svg_limits.dart';

// Turns the bytes of an SVG file into the vector_graphics binary format.
//
// Everything here is plain Dart without Flutter bindings, because it runs
// in a background isolate (see `svg_compiler.dart`). An SVG can come from a
// public share, so the document is treated as hostile input. The app shows
// simple vector drawings only. A document passes three stages, and failing
// any of them throws an [SvgException], which the image widgets show as
// their error state:
//
//  1. [decodeSvgText]: size cap, text encoding, "is this SVG at all".
//  2. `checkSvgAllowed` (svg_gate.dart): a strict grammar of elements,
//     attribute rules and budgets, checked on the raw XML. The compiler
//     only ever sees documents that passed it.
//  3. [_validate]: checks on the compiler's output. Partly independent
//     (size, compiled size), partly defence in depth for stage 2 (patterns,
//     bitmaps, layer nesting), in case the compiler derives something from
//     an input the gate did not anticipate.
//
// Not supported, and therefore shown as an error rather than drawn wrongly:
// `<style>` sheets, `<pattern>`, `<image>`, `<mask>`, `<filter>`, `<symbol>`,
// `<marker>`, `<a>`, `<switch>`, `<foreignObject>`, scripts and animation,
// dashed strokes, blend modes, references to anything outside the document,
// a `<use>` of content that itself contains `<use>`, nested `<svg>`.
//
// Limits of the compiler that remain:
//  * A root element with neither `width`/`height` nor `viewBox` is rejected
//    ("SVG did not specify dimensions") instead of getting the 300x150
//    default a browser would use.
//  * Gzip-compressed `.svgz` is not supported; the server does not serve it.

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

/// What the first bytes of a download say about it.
enum SvgStart {
  /// Nothing but white space (or nothing at all) so far.
  undecided,

  /// Starts with `<` like every XML document.
  markup,

  /// Starts with something else: a bitmap, JSON, plain text.
  other,
}

/// Classifies the beginning of a response without decoding it.
///
/// Skips a byte order mark and white space (and the zero bytes that pad
/// UTF-16), then looks at the first real byte. This lets the downloader
/// stop after the first chunk of a file that cannot be SVG.
SvgStart classifySvgStart(List<int> head) {
  const whitespaceAndPadding = {0x00, 0x09, 0x0a, 0x0d, 0x20};
  const byteOrderMarks = {0xef, 0xbb, 0xbf, 0xff, 0xfe};
  for (var i = 0; i < head.length; i++) {
    final byte = head[i];
    if (whitespaceAndPadding.contains(byte)) continue;
    if (i < 3 && byteOrderMarks.contains(byte)) continue;
    return byte == 0x3c ? SvgStart.markup : SvgStart.other;
  }
  return SvgStart.undecided;
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
  return classifySvgStart(head) == SvgStart.markup &&
      RegExp(r'<svg[\s>]').hasMatch(_decodeText(head));
}

/// Decodes the bytes of an SVG file to text: UTF-8 with or without a byte
/// order mark, or UTF-16 with a byte order mark. Throws [SvgException] for
/// input that is too large or does not start like an SVG document.
String decodeSvgText(Uint8List bytes) {
  if (bytes.length > kMaxSvgBytes) throw const SvgException('SVG too large');
  if (!looksLikeSvg(bytes)) throw const NotSvgException();
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

/// Checks and compiles the SVG in [bytes] and returns it in the
/// vector_graphics binary format; throws when it must not be shown.
///
/// The compiler's optimizers stay off: they need native libraries that only
/// exist in build-time tooling. The document is parsed twice by the
/// compiler (once to inspect the result, once inside `encodeSvg`) because
/// it has no public way to encode already parsed instructions.
Uint8List compileSvg(Uint8List bytes, {SvgLimits limits = const SvgLimits()}) {
  final xml = decodeSvgText(bytes);
  checkSvgAllowed(xml, limits: limits);
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

/// Rejects compiler output that would paint nothing, cost too much to
/// paint, or use a feature the renderer implements with bitmaps.
void _validate(VectorInstructions instructions, SvgLimits limits) {
  _validateSize(instructions);
  _validateFeatures(instructions);
  final commands = instructions.commands;
  if (commands.length > limits.maxCommands ||
      _layerDepth(commands) > limits.maxLayerDepth) {
    throw const SvgException('SVG is too complex');
  }
  // An empty `<svg>` would otherwise show as a blank area.
  const visible = {
    DrawCommandType.path,
    DrawCommandType.vertices,
    DrawCommandType.text,
  };
  if (!commands.any((c) => visible.contains(c.type))) {
    throw const SvgException('SVG has nothing to draw');
  }
}

/// The deepest nesting of offscreen layers in [commands].
///
/// Layers, masks and clips each open a scope that the next unmatched
/// `restore` closes; layers and masks are the scopes that allocate a
/// buffer. Unlike the gate's count on the XML, this sees nesting that only
/// arises when `<use>` copies one group into another.
int _layerDepth(List<DrawCommand> commands) {
  const opening = {
    DrawCommandType.saveLayer,
    DrawCommandType.mask,
    DrawCommandType.clip,
  };
  final scopes = <bool>[];
  var depth = 0;
  var deepest = 0;
  for (final command in commands) {
    if (opening.contains(command.type)) {
      final isLayer = command.type != DrawCommandType.clip;
      scopes.add(isLayer);
      if (isLayer && ++depth > deepest) deepest = depth;
    } else if (command.type == DrawCommandType.restore && scopes.isNotEmpty) {
      if (scopes.removeLast()) depth--;
    }
  }
  return deepest;
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

/// Defence in depth behind the gate: the renderer turns every pattern tile
/// into a bitmap of the size the file declares and decodes embedded
/// bitmaps at their full pixel size, so neither may reach it.
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
