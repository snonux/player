import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers/thumbnail_size_provider.dart';

/// Fits previews to the available width; phones retain two columns at 100%.
int thumbnailColumns(double width, double scale) =>
    (width / (180 * scale)).round().clamp(1, 8).toInt();

class ThumbnailZoom extends ConsumerStatefulWidget {
  const ThumbnailZoom({super.key, required this.builder});

  final Widget Function(BuildContext context, double scale) builder;

  @override
  ConsumerState<ThumbnailZoom> createState() => _ThumbnailZoomState();
}

class _ThumbnailZoomState extends ConsumerState<ThumbnailZoom> {
  int? _pinchIndex;
  double _startScale = 1;

  Future<void> _save(int index) async {
    try {
      await ref.read(thumbnailSizeProvider.notifier).setSize(index);
    } catch (_) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(const SnackBar(
        content: Text('Could not save preview size. It applies this session.'),
      ));
    }
  }

  void _start() {
    final index =
        ref.read(thumbnailSizeProvider).valueOrNull ?? defaultThumbnailSize;
    _startScale = thumbnailScales[index];
    _pinchIndex = index;
  }

  void _update(double factor) {
    final desired = _startScale * factor;
    var nearest = 0;
    for (var i = 1; i < thumbnailScales.length; i++) {
      if ((thumbnailScales[i] - desired).abs() <
          (thumbnailScales[nearest] - desired).abs()) {
        nearest = i;
      }
    }
    if (nearest != _pinchIndex) setState(() => _pinchIndex = nearest);
  }

  void _end() {
    final index = _pinchIndex;
    if (index == null) return;
    unawaited(_save(index));
    setState(() => _pinchIndex = null);
  }

  @override
  Widget build(BuildContext context) {
    final index = _pinchIndex ??
        ref.watch(thumbnailSizeProvider).valueOrNull ??
        defaultThumbnailSize;
    final scale = thumbnailScales[index];
    return Column(
      children: [
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 12),
          child: Align(
            alignment: Alignment.centerRight,
            child: Wrap(
              alignment: WrapAlignment.end,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: [
                const Text('Previews'),
                IconButton(
                  tooltip: 'Smaller previews',
                  onPressed:
                      index == 0 ? null : () => unawaited(_save(index - 1)),
                  icon: const Icon(Icons.remove),
                ),
                TextButton(
                  onPressed: () => unawaited(_save(defaultThumbnailSize)),
                  child: Text('${(scale * 100).round()}%',
                      semanticsLabel:
                          'Preview size ${(scale * 100).round()} percent. Reset to 100 percent'),
                ),
                IconButton(
                  tooltip: 'Larger previews',
                  onPressed: index == thumbnailScales.length - 1
                      ? null
                      : () => unawaited(_save(index + 1)),
                  icon: const Icon(Icons.add),
                ),
              ],
            ),
          ),
        ),
        Expanded(
          child: RawGestureDetector(
            key: const Key('library_preview_gestures'),
            gestures: {
              _TwoFingerZoom:
                  GestureRecognizerFactoryWithHandlers<_TwoFingerZoom>(
                _TwoFingerZoom.new,
                (recognizer) => recognizer
                  ..onStart = _start
                  ..onUpdate = _update
                  ..onEnd = _end,
              ),
            },
            child: widget.builder(context, scale),
          ),
        ),
      ],
    );
  }
}

/// Joins the gesture arena but accepts only after a second finger arrives.
/// One-finger scrolls, taps and long presses retain their normal recognizers.
class _TwoFingerZoom extends OneSequenceGestureRecognizer {
  final Map<int, Offset> _positions = {};
  VoidCallback? onStart;
  ValueChanged<double>? onUpdate;
  VoidCallback? onEnd;
  double? _distance;

  @override
  void addAllowedPointer(PointerDownEvent event) {
    super.addAllowedPointer(event);
    _positions[event.pointer] = event.position;
    if (_positions.length >= 2) {
      resolve(GestureDisposition.accepted);
      if (_distance == null) {
        final points = _positions.values.take(2).toList();
        _distance = math.max(1.0, (points.first - points.last).distance);
        onStart?.call();
      }
    }
  }

  @override
  void handleEvent(PointerEvent event) {
    if (!_positions.containsKey(event.pointer)) return;
    if (event is PointerMoveEvent) {
      _positions[event.pointer] = event.position;
      if (_distance != null && _positions.length >= 2) {
        final points = _positions.values.take(2).toList();
        onUpdate?.call((points.first - points.last).distance / _distance!);
      }
    } else if (event is PointerUpEvent || event is PointerCancelEvent) {
      _positions.remove(event.pointer);
      if (_distance == null) {
        resolvePointer(event.pointer, GestureDisposition.rejected);
      }
      stopTrackingPointer(event.pointer);
      _finishIfNeeded();
    }
  }

  void _finishIfNeeded() {
    if (_distance != null && _positions.length < 2) {
      _distance = null;
      onEnd?.call();
    }
  }

  @override
  void rejectGesture(int pointer) {
    if (_positions.remove(pointer) != null) stopTrackingPointer(pointer);
    _finishIfNeeded();
  }

  @override
  void didStopTrackingLastPointer(int pointer) {
    resolve(GestureDisposition.rejected);
  }

  @override
  String get debugDescription => 'two-finger preview zoom';
}
