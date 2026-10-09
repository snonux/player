/// Largest SVG accepted, as a download and as a document. Vector drawings
/// of the kind this app shows are far smaller; the cap bounds the memory a
/// list of thumbnails can hold while their downloads wait to be compiled.
const int kMaxSvgBytes = 1024 * 1024;

/// Smallest and largest accepted width or height in SVG user units.
const double kMinSvgDimension = 0.01;
const double kMaxSvgDimension = 100000;

/// Largest accepted font size in SVG user units.
const double kMaxSvgFontSize = 10000;

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

/// Bounds for one SVG. The defaults apply in the app; tests pass smaller
/// values to reach a limit with a small document.
///
/// The first group is checked on the raw XML before the compiler runs (see
/// `svg_gate.dart`), the second on the compiler's output.
///
/// Two purposes are mixed here and should not be confused. The grammar and
/// the reference budgets keep the compiler from allocating without bound;
/// that is safety. The sizes below them (elements, path data, coverage,
/// outline length) are sized for the simple drawings this app shows and
/// bound the one-off cost of rasterising a drawing into its bitmap. They
/// do not make that cost negligible: the most expensive accepted document
/// in svg_hostile_input_test.dart takes about 1.3 s to rasterise for a
/// 1440x3120 screen with the software rasteriser of `flutter test`, and
/// documents at a single budget take 0.3 to 1 s. That time is spent once,
/// on the raster thread, when the drawing first appears at a given size.
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
    this.maxCoverage = 80,
    this.maxOutlineLength = 1000,
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

  /// How often the drawing may paint over its own area: the areas touched
  /// by all drawing commands, added up, as a multiple of the drawing's
  /// area. A filled shape counts with its bounding box inside the drawing,
  /// a stroke with its length times its width, an offscreen layer with the
  /// whole drawing.
  final double maxCoverage;

  /// Total length of all outlines, as a multiple of the drawing's
  /// diagonal. The rasteriser's work grows with the length of the edges it
  /// has to scan, so one path zigzagging across the drawing thousands of
  /// times is far more expensive than its size in bytes suggests.
  final double maxOutlineLength;
}
