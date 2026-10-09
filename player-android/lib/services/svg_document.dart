import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:vector_graphics_compiler/vector_graphics_compiler.dart';

// Turns the bytes of an SVG file into the vector_graphics binary format.
//
// Everything here is plain Dart without Flutter bindings, because it runs
// in a background isolate (see `svg_compiler.dart`). An SVG can come from a
// public share, so the document is treated as hostile input: every limit
// below turns an oversized or nonsensical drawing into an [SvgException],
// which the image widgets show as their error state.
//
// Limits of the compiler that cannot be fixed here:
//  * `<style>` blocks (CSS classes) are ignored, so a drawing styled only
//    through classes is painted with the default black fill.
//  * A root element with neither `width`/`height` nor `viewBox` is rejected
//    ("SVG did not specify dimensions") instead of getting the 300x150
//    default a browser would use.
//  * `<image>` elements pointing at a URL are skipped; only embedded
//    `data:` images are drawn. This is also what keeps a shared SVG from
//    making the app contact third-party servers.
//  * Nested `<svg>` elements, scripts, animation and filters are ignored.

/// Largest SVG accepted, both as downloaded and after gzip decompression.
const int kMaxSvgBytes = 5 * 1024 * 1024;

/// Largest accepted width or height in SVG user units.
const double kMaxSvgDimension = 100000;

/// Most drawing commands accepted. The picture is replayed on the UI thread
/// whenever it repaints, so an unbounded count would freeze the app.
const int kMaxSvgCommands = 100000;

/// An SVG that cannot or must not be shown. [message] holds no document
/// content beyond what the compiler itself reports.
class SvgException implements Exception {
  const SvgException(this.message);

  final String message;

  @override
  String toString() => 'SvgException: $message';
}

/// Result of compiling an SVG.
class CompiledSvg {
  const CompiledSvg(this.data, this.images);

  /// The drawing in the vector_graphics binary format.
  final Uint8List data;

  /// Encoded bitmaps embedded in the drawing. They are decoded on the UI
  /// isolate, so the caller checks their pixel size before using [data].
  final List<Uint8List> images;
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
bool isSvgSource({String? fileName, required String url}) {
  bool hasSvgExtension(String? name) {
    final lower = name?.trim().toLowerCase() ?? '';
    return lower.endsWith('.svg') || lower.endsWith('.svgz');
  }

  return hasSvgExtension(fileName) || hasSvgExtension(Uri.tryParse(url)?.path);
}

/// Decodes the bytes of an SVG file to text.
///
/// Handles gzip (`.svgz`), UTF-8 with or without a byte order mark, and
/// UTF-16 with a byte order mark. Throws [SvgException] when the result does
/// not start like an SVG document, so bitmaps, JSON error bodies and HTML
/// pages are rejected before the parser sees them.
String decodeSvgText(Uint8List bytes) {
  if (bytes.length > kMaxSvgBytes) throw const SvgException('SVG too large');
  final isGzip = bytes.length > 2 && bytes[0] == 0x1f && bytes[1] == 0x8b;
  final text = _decodeText(isGzip ? _gunzip(bytes) : bytes);
  if (!_startsLikeSvg(text)) throw const SvgException('Not an SVG document');
  return text;
}

/// Decompresses in slices and stops at [kMaxSvgBytes], so a small file that
/// expands to gigabytes cannot exhaust memory.
Uint8List _gunzip(Uint8List bytes) {
  final output = BytesBuilder(copy: false);
  final sink = ByteConversionSink.withCallback((chunk) => output.add(chunk));
  final input = gzip.decoder.startChunkedConversion(_LimitedSink(sink));
  const slice = 16 * 1024;
  try {
    for (var start = 0; start < bytes.length; start += slice) {
      final end = (start + slice).clamp(0, bytes.length);
      input.add(Uint8List.sublistView(bytes, start, end));
    }
    input.close();
  } on FormatException {
    throw const SvgException('Corrupt gzip data');
  }
  return output.takeBytes();
}

/// Passes chunks on until their total exceeds [kMaxSvgBytes].
class _LimitedSink extends ByteConversionSinkBase {
  _LimitedSink(this._target);

  final ByteConversionSink _target;
  int _total = 0;

  @override
  void add(List<int> chunk) {
    _total += chunk.length;
    if (_total > kMaxSvgBytes) throw const SvgException('SVG too large');
    _target.add(chunk);
  }

  @override
  void close() => _target.close();
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

/// True when an `<svg` tag opens within the first few kilobytes, which
/// leaves room for an XML declaration, a doctype and a licence comment.
bool _startsLikeSvg(String text) {
  final head = text.length > 4096 ? text.substring(0, 4096) : text;
  return RegExp(r'<svg[\s>]').hasMatch(head);
}

/// Parses and validates the SVG in [bytes]; throws when it must not be shown.
///
/// The compiler's optimizers stay off: they need native libraries that only
/// exist in build-time tooling. The document is parsed twice (once to
/// inspect it, once inside `encodeSvg`) because the compiler has no public
/// way to encode already parsed instructions.
///
/// [maxCommands] exists so tests can reach the limit with a small document.
CompiledSvg compileSvg(Uint8List bytes, {int maxCommands = kMaxSvgCommands}) {
  final xml = decodeSvgText(bytes);
  final VectorInstructions instructions;
  try {
    instructions = parseWithoutOptimizers(xml, key: 'network svg');
  } on SvgException {
    rethrow;
  } catch (error) {
    // The parser throws assorted StateError/FormatException/XML exceptions.
    throw SvgException('Invalid SVG: $error');
  }
  _validate(instructions, maxCommands);
  final data = encodeSvg(
    xml: xml,
    debugName: 'network svg',
    enableMaskingOptimizer: false,
    enableClippingOptimizer: false,
    enableOverdrawOptimizer: false,
  );
  return CompiledSvg(data, [for (final i in instructions.images) i.data]);
}

/// Rejects drawings that would paint nothing or cost too much to paint.
///
/// A zero, negative or non-finite size cannot be laid out, and a drawing
/// without any visible command (an empty `<svg>`, or one whose only content
/// is an external image) would otherwise show as a blank area.
void _validate(VectorInstructions instructions, int maxCommands) {
  bool validExtent(double value) =>
      value.isFinite && value > 0 && value <= kMaxSvgDimension;
  if (!validExtent(instructions.width) || !validExtent(instructions.height)) {
    throw const SvgException('SVG has an unusable size');
  }
  if (instructions.commands.length > maxCommands) {
    throw const SvgException('SVG is too complex');
  }
  const visible = {
    DrawCommandType.path,
    DrawCommandType.vertices,
    DrawCommandType.text,
    DrawCommandType.image,
  };
  if (!instructions.commands.any((c) => visible.contains(c.type))) {
    throw const SvgException('SVG has nothing to draw');
  }
}
