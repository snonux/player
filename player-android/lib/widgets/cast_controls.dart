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
      tooltip: casting ? 'Return to this device' : 'Play on a device',
      onPressed: casting ? onStop : onCast,
    );
  }
}

/// Lets the user pick a cast device; null when dismissed.
Future<CastDevice?> showCastDevicePicker(
  BuildContext context,
  CastDiscovery discovery, {
  bool includeRenderers = true,
}) =>
    showModalBottomSheet<CastDevice>(
      context: context,
      showDragHandle: true,
      builder: (context) => _CastDevicePicker(
          discovery: discovery, includeRenderers: includeRenderers),
    );

class _CastDevicePicker extends StatefulWidget {
  const _CastDevicePicker(
      {required this.discovery, required this.includeRenderers});

  final CastDiscovery discovery;
  final bool includeRenderers;

  @override
  State<_CastDevicePicker> createState() => _CastDevicePickerState();
}

class _CastDevicePickerState extends State<_CastDevicePicker> {
  late final Stream<List<CastDevice>> _devices = widget.discovery.devices().map(
      (devices) => devices
          .where((device) => widget.includeRenderers || !device.isRenderer)
          .toList());
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
                child: Text('Play on',
                    style: Theme.of(context).textTheme.titleLarge),
              ),
              for (final device in devices)
                ListTile(
                  key: Key('cast_device_${device.id}'),
                  leading: Icon(device.isJukebox
                      ? Icons.speaker_group
                      : device.isRenderer
                          ? Icons.speaker
                          : Icons.tv),
                  title: Text(device.name),
                  subtitle: device.model == null ? null : Text(device.model!),
                  onTap: () => Navigator.of(context).pop(device),
                ),
              if (snapshot.hasError)
                const _PickerNote(
                  'Cannot search for speakers. Check the Player server network and reopen Play on to retry.',
                )
              else if (devices.isEmpty) ...[
                const ListTile(
                  leading: SizedBox(
                    width: 24,
                    height: 24,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  ),
                  title: Text('Looking for devices…'),
                ),
                if (_showHint)
                  const _PickerNote(
                    'No devices found yet. DLNA speakers must be on the same '
                    'network as the Player server; Cast devices use this phone’s Wi-Fi.',
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
/// item plays on a cast device. Disconnect stays on the app-bar [CastButton]
/// (and the screen hamburger); the renderer queue uses [MusicQueueButton].
class CastPlaybackView extends ConsumerStatefulWidget {
  const CastPlaybackView({super.key});

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
    final trackSkip = cast.device.isRenderer &&
        (controller.hasPrevious || controller.hasNext);
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
            // One transport row: renderer tracks use prev/play/next (±15 via
            // long-press); Chromecast keeps visible ±15 beside play.
            Row(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                IconButton(
                  key: const Key('cast_skip_back'),
                  icon: Icon(
                    trackSkip ? Icons.skip_previous : Icons.fast_rewind,
                    color: Colors.white,
                    size: 36,
                  ),
                  constraints:
                      const BoxConstraints(minWidth: 48, minHeight: 48),
                  tooltip: trackSkip
                      ? 'Previous track · hold −15s'
                      : 'Skip back 15 seconds',
                  // Keep onPressed non-null at queue ends so Flutter still
                  // delivers onLongPress (±15s), matching local music.
                  onPressed: trackSkip
                      ? () {
                          if (controller.hasPrevious) {
                            unawaited(controller.moveRendererQueue(-1));
                          }
                        }
                      : () => skip(-_kCastSkip),
                  onLongPress: trackSkip ? () => skip(-_kCastSkip) : null,
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
                  icon: Icon(
                    trackSkip ? Icons.skip_next : Icons.fast_forward,
                    color: Colors.white,
                    size: 36,
                  ),
                  constraints:
                      const BoxConstraints(minWidth: 48, minHeight: 48),
                  tooltip: trackSkip
                      ? 'Next track · hold +15s'
                      : 'Skip forward 15 seconds',
                  onPressed: trackSkip
                      ? () {
                          if (controller.hasNext) {
                            unawaited(controller.moveRendererQueue(1));
                          }
                        }
                      : () => skip(_kCastSkip),
                  onLongPress: trackSkip ? () => skip(_kCastSkip) : null,
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
          ],
        ),
      ),
    );
  }
}
