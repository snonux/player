import 'dart:typed_data';

/// Largest SVG accepted, as a download and as a document. Vector drawings
/// of the kind this app shows are far smaller; the cap bounds the memory a
/// list of thumbnails can hold while their downloads wait to be compiled.
const int kMaxSvgBytes = 1024 * 1024;

/// Smallest and largest accepted width or height in SVG user units.
const double kMinSvgDimension = 0.01;
const double kMaxSvgDimension = 100000;

/// Largest accepted font size, as a multiple of the larger side of the
/// drawing. A glyph bigger than that is a shape, not text.
const double kMaxSvgFontScale = 4;

/// The budget for rasterising one drawing: weighted drawing operations
/// (see `svg_raster_cost.dart`) times pixels of the bitmap. Rasterising
/// costs at most a constant per operation and pixel, measured at 2 to 3 ns
/// with the software rasteriser of `flutter test`, so this budget keeps
/// the one pass that makes the bitmap between half a second and a second
/// there.
const double kSvgRasterBudget = 3e8;

/// A drawing over budget is rasterised smaller, down to this many pixels
/// on its longer side. A drawing that is over budget even then is refused.
const int kMinSvgRasterSide = 256;

/// An SVG that cannot or must not be shown. [message] holds no document
/// content beyond element and attribute names and what the compiler itself
/// reports.
class SvgException implements Exception {
  const SvgException(this.message);

  final String message;

  @override
  String toString() => 'SvgException: $message';
}

/// The response is not an SVG at all (a bitmap, an error page, ...). Kept
/// apart from other rejections because this outcome is remembered: a file
/// that is not SVG is not downloaded again to find that out a second time.
class NotSvgException extends SvgException {
  const NotSvgException() : super('Not an SVG document');
}

/// A drawing in the vector_graphics binary format, with the weighted number
/// of drawing operations it takes to rasterise (see svg_raster_cost.dart),
/// from which the size of its bitmap is chosen.
class CompiledSvg {
  const CompiledSvg(this.data, this.drawOperations);

  final Uint8List data;
  final double drawOperations;
}

/// Bounds for one SVG. The defaults apply in the app; tests pass smaller
/// values to reach a limit with a small document.
///
/// The first group is checked on the raw XML before the compiler runs (see
/// `svg_gate.dart`), the second on the compiler's output.
///
/// Two purposes are mixed here and should not be confused. The grammar and
/// the reference budgets keep the compiler from allocating without bound;
/// that is safety. The sizes below them (elements, path data) are sized for
/// the simple drawings this app shows. What bounds the time to rasterise a
/// drawing is [kSvgRasterBudget]: operations times pixels, with the bitmap
/// made smaller for drawings with many operations. That time is spent
/// once, on the raster thread, when a drawing first appears at a given
/// size; the worst cases are measured in svg_hostile_input_test.dart
/// (0.55 to 0.75 s for the most expensive accepted document).
class SvgLimits {
  const SvgLimits({
    this.maxElements = 2000,
    this.maxElementDepth = 32,
    this.maxLayerDepth = 4,
    this.maxUses = 100,
    this.maxTextChars = 2000,
    this.maxTextElements = 100,
    this.maxPathChars = 64 * 1024,
    this.maxPathDataChars = 256 * 1024,
    this.maxStrokeWidth = 1000,
    this.maxGradientStops = 256,
    this.maxAttributeChars = 4096,
    this.maxExpandedPathChars = 1024 * 1024,
    this.maxExpandedElements = 20000,
    this.maxExpandedTextChars = 10000,
    this.maxCommands = 100000,
    this.maxCompiledBytes = 4 * 1024 * 1024,
    this.maxDrawOperations =
        kSvgRasterBudget / (kMinSvgRasterSide * kMinSvgRasterSide),
  });

  /// Most elements in the document, drawn or not.
  final int maxElements;

  /// Deepest element nesting.
  final int maxElementDepth;

  /// Deepest nesting of groups that become offscreen layers (an opacity
  /// below 1 on a group). While the picture is rasterised every level is a
  /// buffer of up to screen size that exists at the same time as the levels
  /// around it, so it is the depth that costs memory, not the total.
  /// Checked before compiling on the XML nesting and after compiling on the
  /// drawing commands, which also covers nesting built through `<use>`.
  final int maxLayerDepth;

  /// Most `<use>` elements. Each one copies what it references.
  final int maxUses;

  /// Most characters of text and most `<text>`/`<tspan>` elements. Text is
  /// laid out on the UI isolate when the picture is decoded, and the binary
  /// format cannot hold long strings.
  final int maxTextChars;
  final int maxTextElements;

  /// Most characters of path data (`d` and `points` attributes) in one
  /// element and in the document.
  final int maxPathChars;
  final int maxPathDataChars;

  /// Widest accepted stroke in user units. Only plain numbers and `px` are
  /// accepted, so the value checked is the value the compiler uses.
  final double maxStrokeWidth;

  /// Most gradient `<stop>` elements. Every paint that uses a gradient
  /// carries a copy of its colours.
  final int maxGradientStops;

  /// Longest value of any attribute other than path data, which has its
  /// own budget.
  final int maxAttributeChars;

  /// Budgets for what references multiply. With `copies` being
  /// (1 + `<use>` elements) x (1 + `clip-path` references), the products
  /// copies x path data, copies x elements and copies x text characters
  /// must stay within these. The gate does not resolve references, so this
  /// deliberately overestimates what the compiler will copy.
  final int maxExpandedPathChars;
  final int maxExpandedElements;
  final int maxExpandedTextChars;

  /// Most drawing commands in the compiled drawing, which the rasteriser
  /// executes one by one when the bitmap is made.
  final int maxCommands;

  /// Largest compiled drawing. It equals the largest entry the memory cache
  /// keeps: anything accepted is cached and not compiled again each time it
  /// appears.
  final int maxCompiledBytes;

  /// Most weighted drawing operations (see `svg_raster_cost.dart`). The
  /// default is what [kSvgRasterBudget] allows for the smallest bitmap, a
  /// little under 4,600; a drawing with fewer operations is shown, at a
  /// lower resolution if need be, and one with more is refused.
  final double maxDrawOperations;
}
