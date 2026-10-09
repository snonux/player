import 'dart:math' as math;

import 'package:vector_graphics_compiler/vector_graphics_compiler.dart';

// How much work it is to rasterise a compiled SVG.
//
// A drawing is rasterised once into a bitmap (see svg_raster.dart). No
// single drawing operation can touch more than the whole bitmap, so the
// time for that pass is at most
//
//     (number of drawing operations) x (pixels of the bitmap) x constant.
//
// This file counts the operations; svg_raster.dart then picks a bitmap
// small enough that the product stays within `kSvgRasterBudget`. The count
// does not try to work out which pixels an operation really touches: every
// operation is charged the whole bitmap. Earlier attempts to estimate the
// painted area were wrong for strokes much wider than their centre line
// and for text, and a wrong guess there means seconds of rasterisation.
//
// One operation is one fill of the whole bitmap with a flat colour, the
// unit the budget was calibrated in. What is counted, on the commands the
// compiler produced after expanding every `<use>`:
//
//   * a filled path: 1, a stroked path: 1 (both: 2);
//   * a clip path applied, an offscreen layer, a mask: 1 each;
//   * a character of text: 1;
//   * twice that when the paint is a gradient;
//   * plus, per path, a term for its outline (see [_PathMeter]): a fill
//     whose outline crosses the drawing a thousand times costs hundreds of
//     plain fills.

/// One span between two edge crossings on a scan line costs about this
/// fraction of filling the whole line, measured on an even-odd zigzag.
const double _spanCost = 0.25;

/// The weighted number of drawing operations in [instructions]. The
/// compiler has already applied all transforms, so paths and stroke widths
/// are in the units of the drawing's size.
double svgDrawOperations(VectorInstructions instructions) {
  final meter = _PathMeter(instructions.height);
  final measured = <int, _Outline>{};
  var operations = 0.0;
  for (final command in instructions.commands) {
    final paint =
        command.paintId == null ? null : instructions.paints[command.paintId!];
    switch (command.type) {
      case DrawCommandType.saveLayer:
      case DrawCommandType.mask:
        operations += 1;
      case DrawCommandType.clip:
        final outline = measured[command.objectId!] ??=
            meter.measure(instructions.paths[command.objectId!]);
        operations += 1 + outline.fillCrossings * _spanCost;
      case DrawCommandType.path:
        final outline = measured[command.objectId!] ??=
            meter.measure(instructions.paths[command.objectId!]);
        operations += _pathOperations(outline, paint, meter);
      case DrawCommandType.text:
        final characters = instructions.text[command.objectId!].text.length;
        operations += characters * _paintFactor(paint?.fill?.shader);
      default:
        break;
    }
  }
  return operations;
}

double _paintFactor(Gradient? shader) => shader == null ? 1 : 2;

double _pathOperations(_Outline outline, Paint? paint, _PathMeter meter) {
  var operations = 0.0;
  final fill = paint?.fill;
  if (fill != null) {
    operations +=
        _paintFactor(fill.shader) * (1 + outline.fillCrossings * _spanCost);
  }
  final stroke = paint?.stroke;
  if (stroke != null) {
    final crossings = meter.strokeCrossings(outline, stroke.width ?? 1);
    operations += _paintFactor(stroke.shader) * (1 + crossings * _spanCost);
  }
  return operations;
}

/// What the outline of one path contributes, in "crossings": how many times
/// its edges cross the full height of the drawing, added up.
class _Outline {
  const _Outline(this.segments, this.fillCrossings, this.strokeCrossings);

  final int segments;

  /// For a fill: only the parts of edges inside the drawing count.
  final double fillCrossings;

  /// For the centre line of a stroke: every edge counts, with at most one
  /// full height each, wherever it lies.
  final double strokeCrossings;
}

/// Measures how much edge a scan-line rasteriser has to walk for a path.
///
/// The work per scan line grows with the number of edges crossing it. Added
/// up over all scan lines that is the vertical extent of all edges, here in
/// units of the drawing's [height]. A curve is measured by the polygon
/// through its control points, whose vertical extent is at least the
/// curve's.
class _PathMeter {
  _PathMeter(this.height);

  final double height;

  _Outline measure(Path path) {
    var segments = 0;
    var inside = 0.0;
    var anywhere = 0.0;
    var y = 0.0;
    var startY = 0.0;
    void edgeTo(double nextY) {
      if (nextY.isNaN) return;
      segments++;
      inside += (_clamp(nextY) - _clamp(y)).abs();
      final rise = (nextY - y).abs();
      anywhere += rise.isFinite ? math.min(rise, height) : height;
      y = nextY;
    }

    for (final command in path.commands) {
      if (command is MoveToCommand) {
        y = startY = command.y.isNaN ? 0 : command.y;
      } else if (command is LineToCommand) {
        edgeTo(command.y);
      } else if (command is CubicToCommand) {
        edgeTo(command.y1);
        edgeTo(command.y2);
        edgeTo(command.y3);
      } else if (command is CloseCommand) {
        edgeTo(startY);
      }
    }
    return _Outline(segments, inside / height, anywhere / height);
  }

  double _clamp(double y) => y.clamp(0, height).toDouble();

  /// A stroke has two sides, each following the centre line, and every
  /// join and cap adds an edge of up to the stroke [width]. The centre line
  /// is not clamped to the drawing: a wide stroke reaches into the drawing
  /// from a centre line outside it.
  double strokeCrossings(_Outline outline, double width) {
    final reach = (width.isFinite ? width.abs() : height) / height;
    final joinsAndCaps = (outline.segments + 2) * math.min(reach, 1);
    return 2 * (outline.strokeCrossings + joinsAndCaps);
  }
}
