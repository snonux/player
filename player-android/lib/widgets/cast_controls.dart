import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers/cast_provider.dart';
import '../services/cast/cast_device.dart';
import '../services/cast/cast_session.dart';
import '../utils/duration_formatter.dart';

/// How long the picker searches before it explains why nothing shows up.
const kCastSearchHintDelay = Duration(seconds: 8);

const _kCastSkip = Duration(seconds: 15);

/// App bar button that casts the current item. Shows the "connected" icon
/// while [casting]; tapping it then stops casting.
class CastButton extends StatelessWidget {
  const CastButton({
    super.key,
    required this.casting,
    required this.busy,
    required this.onCast,
    required this.onStop,
  });

  final bool casting;
  final bool busy;
  final VoidCallback onCast;
  final VoidCallback onStop;

  @override
  Widget build(BuildContext context) {
    if (busy) {
      return const Padding(
        padding: EdgeInsets.all(14),
        child: SizedBox(
          key: Key('cast_button_busy'),
          width: 20,
          height: 20,
          child: CircularProgressIndicator(strokeWidth: 2),
        ),
      );
    }
    return IconButton(
      key: const Key('cast_button'),
      icon: Icon(casting ? Icons.cast_connected : Icons.cast),
      tooltip: casting ? 'Stop casting' : 'Cast to a device',
      onPressed: casting ? onStop : onCast,
    );
  }
}

/// Lets the user pick a cast device; null when dismissed.
Future<CastDevice?> showCastDevicePicker(
  BuildContext context,
  CastDiscovery discovery,
) =>
    showModalBottomSheet<CastDevice>(
      context: context,
      showDragHandle: true,
      builder: (context) => _CastDevicePicker(discovery: discovery),
    );

class _CastDevicePicker extends StatefulWidget {
  const _CastDevicePicker({required this.discovery});

  final CastDiscovery discovery;

  @override
  State<_CastDevicePicker> createState() => _CastDevicePickerState();
}

class _CastDevicePickerState extends State<_CastDevicePicker> {
  late final Stream<List<CastDevice>> _devices = widget.discovery.devices();
  Timer? _hintTimer;
  bool _showHint = false;

  @override
  void initState() {
    super.initState();
    _hintTimer = Timer(kCastSearchHintDelay, () {
      if (mounted) setState(() => _showHint = true);
    });
  }

  @override
  void dispose() {
    _hintTimer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      child: StreamBuilder<List<CastDevice>>(
        stream: _devices,
        builder: (context, snapshot) {
          final devices = snapshot.data ?? const <CastDevice>[];
          return Column(
            key: const Key('cast_device_picker'),
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(24, 0, 24, 8),
                child: Text('Cast to',
                    style: Theme.of(context).textTheme.titleLarge),
              ),
              for (final device in devices)
                ListTile(
                  key: Key('cast_device_${device.id}'),
                  leading: const Icon(Icons.tv),
                  title: Text(device.name),
                  subtitle: device.model == null ? null : Text(device.model!),
                  onTap: () => Navigator.of(context).pop(device),
                ),
              if (snapshot.hasError)
                const _PickerNote(
                  'This phone cannot search the network for cast devices.',
                )
              else if (devices.isEmpty) ...[
                const ListTile(
                  leading: SizedBox(
                    width: 24,
                    height: 24,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  ),
                  title: Text('Looking for cast devices…'),
                ),
                if (_showHint)
                  const _PickerNote(
                    'No devices found yet. The cast device must be switched '
                    'on and on the same Wi-Fi network as this phone.',
                  ),
              ],
              const SizedBox(height: 16),
            ],
          );
        },
      ),
    );
  }
}

class _PickerNote extends StatelessWidget {
  const _PickerNote(this.text);

  final String text;

  @override
  Widget build(BuildContext context) => Padding(
        key: const Key('cast_picker_note'),
        padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 8),
        child: Text(text),
      );
}

/// Remote controls shown in place of the local player while the current
/// item plays on a cast device.
class CastPlaybackView extends ConsumerStatefulWidget {
  const CastPlaybackView({super.key, required this.onStop});

  /// Ends casting; the screen continues on the phone.
  final VoidCallback onStop;

  @override
  ConsumerState<CastPlaybackView> createState() => _CastPlaybackViewState();
}

class _CastPlaybackViewState extends ConsumerState<CastPlaybackView> {
  // The receiver reports positions only on changes, so the bar is redrawn
  // from the controller's estimate on this clock.
  late final Timer _ticker;

  // Where the slider is held while dragging; the seek is sent on release.
  double? _dragMs;

  @override
  void initState() {
    super.initState();
    _ticker = Timer.periodic(const Duration(milliseconds: 500), (_) {
      if (mounted) setState(() {});
    });
  }

  @override
  void dispose() {
    _ticker.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final controller = ref.watch(castControllerProvider);
    final cast = controller.active;
    if (cast == null) return const SizedBox.shrink();
    final status = cast.status;
    final duration = status.duration ?? Duration.zero;
    final totalMs = duration.inMilliseconds.toDouble();
    final positionMs = _dragMs ??
        cast.position.inMilliseconds
            .toDouble()
            .clamp(0.0, totalMs > 0 ? totalMs : 1.0);
    final playing = status.playerState == CastPlayerState.playing;
    const labelStyle = TextStyle(color: Colors.white70, fontSize: 12);

    void skip(Duration delta) {
      var target = cast.position + delta;
      if (target < Duration.zero) target = Duration.zero;
      if (duration > Duration.zero && target > duration) target = duration;
      unawaited(controller.seek(target));
    }

    return Center(
      key: const Key('cast_playback_view'),
      child: SingleChildScrollView(
        padding: const EdgeInsets.symmetric(horizontal: 24, vertical: 16),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(Icons.cast_connected, color: Colors.white70, size: 96),
            const SizedBox(height: 16),
            Text(
              'Playing on ${cast.device.name}',
              key: const Key('cast_device_name'),
              style: const TextStyle(color: Colors.white, fontSize: 18),
              textAlign: TextAlign.center,
            ),
            if (status.playerState == CastPlayerState.buffering) ...[
              const SizedBox(height: 12),
              const SizedBox(
                width: 160,
                child: LinearProgressIndicator(minHeight: 2),
              ),
            ],
            const SizedBox(height: 24),
            Slider(
              key: const Key('cast_seek_bar'),
              value: positionMs,
              min: 0,
              max: totalMs > 0 ? totalMs : 1.0,
              onChanged:
                  totalMs > 0 ? (v) => setState(() => _dragMs = v) : null,
              onChangeEnd: totalMs > 0
                  ? (v) {
                      setState(() => _dragMs = null);
                      unawaited(
                          controller.seek(Duration(milliseconds: v.round())));
                    }
                  : null,
              activeColor: Colors.white,
              inactiveColor: Colors.white24,
            ),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 8),
              child: Row(
                mainAxisAlignment: MainAxisAlignment.spaceBetween,
                children: [
                  Text(formatDuration(positionMs / 1000), style: labelStyle),
                  Text(formatDuration(totalMs / 1000), style: labelStyle),
                ],
              ),
            ),
            const SizedBox(height: 8),
            Row(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                IconButton(
                  key: const Key('cast_skip_back'),
                  icon: const Icon(Icons.fast_rewind,
                      color: Colors.white, size: 36),
                  tooltip: 'Skip back 15 seconds',
                  onPressed: () => skip(-_kCastSkip),
                ),
                const SizedBox(width: 16),
                IconButton(
                  key: const Key('cast_play_pause'),
                  icon: Icon(
                    playing
                        ? Icons.pause_circle_filled
                        : Icons.play_circle_filled,
                    color: Colors.white,
                    size: 64,
                  ),
                  tooltip: playing ? 'Pause' : 'Play',
                  onPressed: () => unawaited(
                      playing ? controller.pause() : controller.play()),
                ),
                const SizedBox(width: 16),
                IconButton(
                  key: const Key('cast_skip_forward'),
                  icon: const Icon(Icons.fast_forward,
                      color: Colors.white, size: 36),
                  tooltip: 'Skip forward 15 seconds',
                  onPressed: () => skip(_kCastSkip),
                ),
              ],
            ),
            if (status.volume != null)
              Row(
                children: [
                  const Icon(Icons.volume_down, color: Colors.white70),
                  Expanded(
                    child: Slider(
                      key: const Key('cast_volume'),
                      value: status.volume!.clamp(0.0, 1.0),
                      onChanged: (v) => unawaited(controller.setVolume(v)),
                      activeColor: Colors.white,
                      inactiveColor: Colors.white24,
                    ),
                  ),
                  const Icon(Icons.volume_up, color: Colors.white70),
                ],
              ),
            const SizedBox(height: 16),
            FilledButton.icon(
              key: const Key('cast_stop'),
              onPressed: widget.onStop,
              icon: const Icon(Icons.stop),
              label: const Text('Stop casting'),
            ),
          ],
        ),
      ),
    );
  }
}
