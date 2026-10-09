import 'dart:math' as math;

import 'package:vector_graphics_compiler/vector_graphics_compiler.dart';

import 'svg_limits.dart';

// An estimate of what it costs to rasterise a compiled SVG.
//
// A drawing is rasterised once, into a bitmap (see svg_image_provider.dart).
// That one pass can still be slow for a document that is small in bytes:
// hundreds of shapes each covering the whole drawing, or one path whose
// outline crosses the drawing thousands of times. Neither shows in element
// counts or path sizes, so the compiled drawing commands are measured:
//
//  * coverage: how much area the commands paint, added up, in units of the
//    drawing's own area;
//  * outline length: how much edge the rasteriser has to scan, in units of
//    the drawing's diagonal.
//
// Both are estimates from bounding boxes and control points, not exact
// geometry, and they bound time, not memory. All coordinates are clamped
// to the drawing first, so geometry far outside it, which costs nothing to
// rasterise, counts for nothing here either.

/// Throws [SvgException] when [instructions] exceed the coverage or outline
/// budget of [limits]. The compiler has already applied all transforms, so
/// paths and stroke widths are in the units of the drawing's size.
void checkRasterCost(VectorInstructions instructions, SvgLimits limits) {
  final meter = _PathMeter(instructions.width, instructions.height);
  final measured = <int, _PathSize>{};
  var coverage = 0.0;
  var outline = 0.0;
  for (final command in instructions.commands) {
    switch (command.type) {
      case DrawCommandType.saveLayer:
      case DrawCommandType.mask:
        // An offscreen buffer of up to the whole drawing, drawn back once.
        coverage += 1;
      case DrawCommandType.path:
      case DrawCommandType.clip:
        final id = command.objectId!;
        final size = measured[id] ??= meter.measure(instructions.paths[id]);
        final paint = command.paintId == null
            ? null
            : instructions.paints[command.paintId!];
        final strokeWidth = paint?.stroke?.width ?? 0;
        // A clip has no paint but is filled into the clip mask.
        if (paint?.fill != null || paint == null) coverage += size.boxArea;
        coverage += size.length * strokeWidth / meter.area;
        // A stroke has two sides.
        outline += size.length / meter.diagonal * (strokeWidth > 0 ? 3 : 1);
      default:
        break;
    }
  }
  if (coverage > limits.maxCoverage || outline > limits.maxOutlineLength) {
    throw const SvgException('SVG is too expensive to draw');
  }
}

/// What one path contributes: the area of its bounding box as a fraction
/// of the drawing, and the length of its outline in drawing units.
class _PathSize {
  const _PathSize(this.boxArea, this.length);

  final double boxArea;
  final double length;
}

/// Measures paths against a drawing of [width] x [height].
class _PathMeter {
  _PathMeter(this.width, this.height);

  final double width;
  final double height;

  double get area => width * height;
  double get diagonal => math.sqrt(width * width + height * height);

  double _left = 0;
  double _top = 0;
  double _right = 0;
  double _bottom = 0;
  double _x = 0;
  double _y = 0;
  double _length = 0;

  /// Walks the control points of [path]. For a curve the polygon through
  /// its control points is at least as long as the curve and contains it.
  _PathSize measure(Path path) {
    _left = _top = double.infinity;
    _right = _bottom = double.negativeInfinity;
    _length = 0;
    var startX = 0.0;
    var startY = 0.0;
    for (final command in path.commands) {
      if (command is MoveToCommand) {
        _moveTo(command.x, command.y);
        startX = _x;
        startY = _y;
      } else if (command is LineToCommand) {
        _lineTo(command.x, command.y);
      } else if (command is CubicToCommand) {
        _lineTo(command.x1, command.y1);
        _lineTo(command.x2, command.y2);
        _lineTo(command.x3, command.y3);
      } else if (command is CloseCommand) {
        _lineTo(startX, startY);
      }
    }
    if (_right < _left) return const _PathSize(0, 0);
    return _PathSize((_right - _left) * (_bottom - _top) / area, _length);
  }

  /// Clamps a coordinate into the drawing; NaN counts as the origin.
  double _clamp(double value, double max) =>
      value.isNaN ? 0 : value.clamp(0, max).toDouble();

  void _moveTo(double x, double y) {
    _x = _clamp(x, width);
    _y = _clamp(y, height);
    _left = math.min(_left, _x);
    _right = math.max(_right, _x);
    _top = math.min(_top, _y);
    _bottom = math.max(_bottom, _y);
  }

  void _lineTo(double x, double y) {
    final fromX = _x;
    final fromY = _y;
    _moveTo(x, y);
    final dx = _x - fromX;
    final dy = _y - fromY;
    _length += math.sqrt(dx * dx + dy * dy);
  }
}
