import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers/thumbnail_size_provider.dart';
import 'overflow_menu.dart';

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
    return Stack(
      children: [
        Positioned.fill(
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
        if (_pinchIndex != null)
          Positioned(
            top: 8,
            left: 0,
            right: 0,
            child: IgnorePointer(
              child: Center(
                child: Chip(
                  key: const Key('preview_size_badge'),
                  label: Text('${(scale * 100).round()}%'),
                ),
              ),
            ),
          ),
      ],
    );
  }
}

/// Preview size rows for a screen's [OverflowMenuButton]; pinching the grid
/// with two fingers changes the size too.
class PreviewSizeMenuItems extends ConsumerWidget {
  const PreviewSizeMenuItems({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final index =
        ref.watch(thumbnailSizeProvider).valueOrNull ?? defaultThumbnailSize;
    final percent = '${(thumbnailScales[index] * 100).round()}%';
    // The menu closes before an item runs, so capture what it needs now.
    final messenger = ScaffoldMessenger.of(context);
    final sizes = ref.read(thumbnailSizeProvider.notifier);
    void save(int size) => unawaited(sizes.setSize(size).catchError((Object _) {
          messenger.showSnackBar(const SnackBar(
            content:
                Text('Could not save preview size. It applies this session.'),
          ));
        }));
    return Column(mainAxisSize: MainAxisSize.min, children: [
      OverflowMenuItem(
        key: const Key('preview_size_larger'),
        icon: Icons.zoom_in,
        label: 'Larger previews',
        subtitle: 'Now $percent',
        onSelected:
            index == thumbnailScales.length - 1 ? null : () => save(index + 1),
      ),
      OverflowMenuItem(
        key: const Key('preview_size_smaller'),
        icon: Icons.zoom_out,
        label: 'Smaller previews',
        onSelected: index == 0 ? null : () => save(index - 1),
      ),
      if (index != defaultThumbnailSize)
        OverflowMenuItem(
          key: const Key('preview_size_reset'),
          icon: Icons.restart_alt,
          label: 'Reset previews to 100%',
          onSelected: () => save(defaultThumbnailSize),
        ),
    ]);
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
