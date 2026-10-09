import 'dart:math' as math;

import 'package:vector_graphics_compiler/vector_graphics_compiler.dart';

import 'svg_limits.dart';

// How much work it is to rasterise a compiled SVG.
//
// A drawing is rasterised once into a bitmap (see svg_raster.dart). This
// file counts its drawing operations, weighted so that the time for that
// pass is roughly
//
//     (weighted operations) x (pixels of the bitmap) x constant,
//
// and svg_raster.dart picks a bitmap small enough that the product stays
// within `kSvgRasterBudget`. The count does not try to work out which
// pixels an operation really touches: every operation is charged the whole
// bitmap. Earlier attempts to estimate the painted area were wrong for
// strokes much wider than their centre line and for text, and a wrong
// guess there means seconds of rasterisation.
//
// The unit is one fill of the whole bitmap with a flat colour. Counted, on
// the commands the compiler produced after expanding every `<use>`:
//
//   * a filled path: 1, a stroked path: 1 (both: 2);
//   * an offscreen layer, a mask: 1 each;
//   * a clip path applied: 1 plus the term for its outline, like a fill;
//   * everything drawn while a clip is in force: times [kSvgClipFactor]
//     per active clip ([kSvgClippedTextFactor] for text);
//   * a character of text: 1, and 1 more when the text is also stroked;
//   * twice that when the paint is a gradient;
//   * per path, a term for its outline (see [_PathMeter]): a fill whose
//     outline crosses the drawing a thousand times costs hundreds of plain
//     fills;
//   * everything drawn while a clip is in force, times a factor per clip
//     (see [kSvgClipFactor]), and only simple clip paths are accepted.
//
// What this is not: a proof. The weights are measured, not derived, and
// were found wrong before (drawing under a complex clip cost ten times its
// count). Known simplifications: glyph outlines are not measured, only
// counted; a stroke's joins and caps are charged at most the stroke width
// each, so a miter spike longer than that is undercounted; nested clips
// are overcounted. The hostile-input tests measure the worst documents
// found so far against the time bound.

/// One span between two edge crossings on a scan line costs about this
/// fraction of filling the whole line, measured on an even-odd zigzag.
const double _spanCost = 0.25;

/// What drawing under a clip costs compared with drawing unclipped, per
/// clip that is active: every span of the shape is intersected with the
/// spans of the clip. With the most complex clip that is accepted (see
/// [kMaxSvgClipCrossings]) a clipped rectangle measured up to 2.2 times an
/// unclipped one and clipped text up to 2.3 times the budget's unit, most
/// at small bitmaps where the work per shape outweighs the work per pixel.
/// The factors leave a margin over that, text more because it is drawn
/// glyph by glyph. Nested clips multiply, which overstates them.
const double kSvgClipFactor = 4;
const double kSvgClippedTextFactor = 8;

/// The weighted number of drawing operations in [instructions]; throws for
/// a clip path that is not a simple shape. The compiler has already
/// applied all transforms, so paths and stroke widths are in the units of
/// the drawing's size.
double svgDrawOperations(VectorInstructions instructions) {
  final counter = _OperationCounter(instructions);
  instructions.commands.forEach(counter.add);
  return counter.operations;
}

double _paintFactor(Gradient? shader) => shader == null ? 1 : 2;

/// Walks the drawing commands, keeping track of the clips in force.
class _OperationCounter {
  _OperationCounter(this._instructions)
      : _meter = _PathMeter(_instructions.height);

  final VectorInstructions _instructions;
  final _PathMeter _meter;
  final Map<int, _Outline> _measured = {};

  /// One entry per open scope (clip, layer or mask); true for a clip. The
  /// next unmatched `restore` closes the innermost scope.
  final List<bool> _scopes = [];
  double operations = 0;

  int get _activeClips => _scopes.where((isClip) => isClip).length;

  _Outline _outline(int pathId) =>
      _measured[pathId] ??= _meter.measure(_instructions.paths[pathId]);

  void add(DrawCommand command) {
    final paint =
        command.paintId == null ? null : _instructions.paints[command.paintId!];
    switch (command.type) {
      case DrawCommandType.saveLayer:
      case DrawCommandType.mask:
        operations += math.pow(kSvgClipFactor, _activeClips);
        _scopes.add(false);
      case DrawCommandType.clip:
        operations += _clipOperations(_outline(command.objectId!));
        _scopes.add(true);
      case DrawCommandType.restore:
        if (_scopes.isNotEmpty) _scopes.removeLast();
      case DrawCommandType.path:
        operations += _pathOperations(_outline(command.objectId!), paint) *
            math.pow(kSvgClipFactor, _activeClips);
      case DrawCommandType.text:
        final text = _instructions.text[command.objectId!].text;
        operations += text.length *
            _textFactor(paint) *
            math.pow(kSvgClippedTextFactor, _activeClips);
      default:
        break;
    }
  }

  /// Only simple clips are accepted: what is drawn under a clip pays for
  /// every span of the clip, so a clip with a long outline multiplies the
  /// cost of everything inside it.
  double _clipOperations(_Outline outline) {
    if (outline.fillCrossings > kMaxSvgClipCrossings ||
        outline.commands > kMaxSvgClipCommands) {
      throw const SvgException('SVG clip path is too complex');
    }
    return 1 + outline.fillCrossings * _spanCost;
  }

  /// Fill and stroke of text are separate passes over every glyph.
  double _textFactor(Paint? paint) {
    final fill = paint?.fill;
    final stroke = paint?.stroke;
    final filled = fill == null ? 0.0 : _paintFactor(fill.shader);
    final stroked = stroke == null ? 0.0 : _paintFactor(stroke.shader);
    return math.max(1, filled + stroked);
  }

  double _pathOperations(_Outline outline, Paint? paint) {
    var count = 0.0;
    final fill = paint?.fill;
    if (fill != null) {
      count +=
          _paintFactor(fill.shader) * (1 + outline.fillCrossings * _spanCost);
    }
    final stroke = paint?.stroke;
    if (stroke != null) {
      final crossings = _meter.strokeCrossings(outline, stroke.width ?? 1);
      count += _paintFactor(stroke.shader) * (1 + crossings * _spanCost);
    }
    return count;
  }
}

/// What the outline of one path contributes, in "crossings": how many times
/// its edges cross the full height of the drawing, added up.
class _Outline {
  const _Outline(
    this.commands,
    this.segments,
    this.fillCrossings,
    this.strokeCrossings,
  );

  /// Path commands (moves, lines, curves) and the edges they amount to.
  final int commands;
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
    return _Outline(
      path.commands.length,
      segments,
      inside / height,
      anywhere / height,
    );
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
