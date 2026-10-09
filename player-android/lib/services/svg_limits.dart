/// Largest SVG download accepted. Vector drawings of the kind this app
/// shows are far smaller; the cap bounds the memory a list of thumbnails
/// can hold while their downloads wait to be compiled.
const int kMaxSvgBytes = 2 * 1024 * 1024;

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
class SvgLimits {
  const SvgLimits({
    this.maxElements = 5000,
    this.maxElementDepth = 32,
    this.maxLayerDepth = 4,
    this.maxUses = 200,
    this.maxTextChars = 2000,
    this.maxTextElements = 100,
    this.maxPathDataChars = 512 * 1024,
    this.maxStrokeWidth = 1000,
    this.maxCommands = 100000,
    this.maxCompiledBytes = 4 * 1024 * 1024,
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

  /// Most characters of path data (`d` and `points` attributes) in total.
  final int maxPathDataChars;

  /// Widest accepted stroke in user units.
  final double maxStrokeWidth;

  /// Most drawing commands. The picture is replayed on the raster thread
  /// for every frame that repaints it, so the count must stay bounded.
  final int maxCommands;

  /// Largest compiled drawing. It equals the largest entry the memory cache
  /// keeps: anything accepted is cached and not compiled again each time it
  /// appears.
  final int maxCompiledBytes;
}
