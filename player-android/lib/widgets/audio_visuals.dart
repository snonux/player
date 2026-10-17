import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:just_audio/just_audio.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../services/audio_visuals.dart';

/// Samples only while this player is visible, ready and playing. Native
/// capture never owns playback, so permissions/effect failures cannot mute it.
class AudioVisuals extends StatefulWidget {
  const AudioVisuals(
      {super.key,
      required this.player,
      required this.title,
      required this.artwork,
      this.service = const AudioVisualsService()});

  final AudioPlayer player;
  final String title;
  final Widget artwork;
  final AudioVisualsService service;

  @override
  State<AudioVisuals> createState() => _AudioVisualsState();
}

class _AudioVisualsState extends State<AudioVisuals>
    with WidgetsBindingObserver {
  static const _preferenceKey = 'player-audio-visual';
  AudioVisualMode _mode = AudioVisualMode.off;
  AudioVisualFrame? _frame;
  StreamSubscription<AudioVisualFrame>? _capture;
  final _subscriptions = <StreamSubscription<dynamic>>[];
  Future<void> _operations = Future.value();
  int _generation = 0;
  int? _session;
  bool _playing = false;
  bool _ready = false;
  bool _foreground = true;
  bool _routeVisible = true;
  bool _permission = false;
  bool _choosing = false;
  int _selection = 0;
  bool _reducedMotion = false;
  String? _message;

  bool get _book => isAudiobook(widget.title);

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    _foreground = WidgetsBinding.instance.lifecycleState == null ||
        WidgetsBinding.instance.lifecycleState == AppLifecycleState.resumed;
    _session = widget.player.androidAudioSessionId;
    _playing = widget.player.playing;
    _ready = widget.player.processingState == ProcessingState.ready;
    _subscriptions.add(widget.player.androidAudioSessionIdStream.listen((id) {
      _session = id;
      _frame = null;
      _refresh();
    }));
    _subscriptions.add(widget.player.playingStream.listen((playing) {
      _playing = playing;
      _refresh();
    }));
    _subscriptions.add(widget.player.processingStateStream.listen((state) {
      _ready = state == ProcessingState.ready;
      _refresh();
    }));
    unawaited(_restore());
  }

  Future<void> _restore() async {
    AudioVisualMode? mode;
    try {
      final prefs = await SharedPreferences.getInstance();
      final saved = prefs.getString(_preferenceKey);
      mode = AudioVisualMode.values
          .where((value) => value.name == saved)
          .firstOrNull;
    } catch (_) {
      // Optional preferences must not interrupt playback; keep Off.
      return;
    }
    var permitted = false;
    if (mode != null && mode != AudioVisualMode.off) {
      try {
        permitted = await widget.service.permission();
      } catch (_) {}
    }
    if (!mounted || _selection != 0) return;
    setState(() {
      // New installs start Off so opening playback never requests permission.
      _mode =
          _reducedMotion ? AudioVisualMode.off : mode ?? AudioVisualMode.off;
      _permission = permitted;
    });
    _refresh();
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    _routeVisible = (ModalRoute.of(context)?.isCurrent ?? true) &&
        TickerMode.valuesOf(context).enabled;
    _reducedMotion = MediaQuery.disableAnimationsOf(context);
    _refresh();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    _foreground = state == AppLifecycleState.resumed;
    _refresh();
  }

  void _refresh() {
    final generation = ++_generation;
    _operations = _operations.then((_) async {
      if (generation != _generation) return;
      await _capture?.cancel();
      _capture = null;
      if (!mounted ||
          generation != _generation ||
          _mode == AudioVisualMode.off ||
          _book ||
          !_permission ||
          !_playing ||
          !_ready ||
          !_foreground ||
          !_routeVisible ||
          (_session ?? 0) <= 0) {
        return;
      }
      _capture = widget.service.frames(_session!).listen((frame) {
        if (mounted && generation == _generation) {
          setState(() => _frame = frame);
        }
      }, onError: (Object error) {
        if (mounted && generation == _generation) {
          setState(() {
            _frame = null;
            _message =
                'Visuals unavailable on this device. Playback continues.';
          });
        }
      });
    });
  }

  Future<void> _choose(AudioVisualMode? mode) async {
    if (mode == null) return;
    ++_selection;
    setState(() {
      _choosing = true;
      _message = null;
    });
    var permitted = _permission;
    try {
      if (mode != AudioVisualMode.off) {
        permitted = await widget.service.permission(request: true);
      }
    } catch (_) {
      permitted = false;
    }
    try {
      final prefs = await SharedPreferences.getInstance();
      await prefs.setString(_preferenceKey, mode.name);
    } catch (_) {}
    if (!mounted) return;
    setState(() {
      _mode = mode;
      _permission = permitted;
      _frame = null;
      _choosing = false;
      if (!permitted && mode != AudioVisualMode.off) {
        _message =
            'Allow audio access to enable music visuals. Playback continues.';
      }
    });
    _refresh();
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    ++_generation;
    for (final subscription in _subscriptions) {
      unawaited(subscription.cancel());
    }
    unawaited(_capture?.cancel());
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final show = !_book && _mode != AudioVisualMode.off && _frame != null;
    return Column(mainAxisSize: MainAxisSize.min, children: [
      if (show)
        RepaintBoundary(
            child: SizedBox(
          key: const Key('audio_visual_canvas'),
          height: 220,
          width: double.infinity,
          child: CustomPaint(
              painter: AudioVisualPainter(
                  _mode, _frame!, Theme.of(context).colorScheme.primary)),
        ))
      else
        widget.artwork,
      const SizedBox(height: 12),
      if (_book)
        const Text('Audiobook · visuals off',
            style: TextStyle(color: Colors.white70))
      else ...[
        const Text('Music visuals', style: TextStyle(color: Colors.white70)),
        DropdownButton<AudioVisualMode>(
          key: const Key('audio_visual_mode'),
          value: _mode,
          dropdownColor: Colors.grey[900],
          style: const TextStyle(color: Colors.white),
          isExpanded: true,
          items: AudioVisualMode.values
              .map((mode) =>
                  DropdownMenuItem(value: mode, child: Text(mode.label)))
              .toList(),
          onChanged: _choosing ? null : _choose,
        ),
        if (_mode != AudioVisualMode.off && !_permission && _message == null)
          TextButton(
              onPressed: _choosing ? null : () => _choose(_mode),
              child: const Text('Enable music visuals')),
        if (_message != null)
          Text(_message!, style: const TextStyle(color: Colors.white70)),
      ],
    ]);
  }
}

class AudioVisualPainter extends CustomPainter {
  AudioVisualPainter(this.mode, this.frame, this.color);
  final AudioVisualMode mode;
  final AudioVisualFrame frame;
  final Color color;

  @override
  void paint(Canvas canvas, Size size) {
    final paint = Paint()
      ..color = color
      ..strokeWidth = 2;
    if (mode == AudioVisualMode.off) return;
    if (mode == AudioVisualMode.wave) {
      final samples = frame.waveform;
      if (samples.length < 2) return;
      final path = Path();
      for (var i = 0; i < samples.length; i++) {
        final x = i / (samples.length - 1) * size.width;
        final y = (1 - samples[i]) * size.height / 2;
        if (i == 0) {
          path.moveTo(x, y);
        } else {
          path.lineTo(x, y);
        }
      }
      canvas.drawPath(path, paint..style = PaintingStyle.stroke);
      return;
    }
    final count = frame.spectrum.length;
    final center = size.center(Offset.zero);
    final radius = size.shortestSide * .2;
    for (var i = 0; i < count; i++) {
      final value = frame.spectrum[i];
      if (mode == AudioVisualMode.bars || mode == AudioVisualMode.mirror) {
        final mirror = mode == AudioVisualMode.mirror;
        final height = math.max(1.0, value * size.height * (mirror ? .45 : .9));
        canvas.drawRect(
            Rect.fromLTWH(
                i * size.width / count,
                mirror ? size.height / 2 - height : size.height - height,
                math.max(1.0, size.width / count - 2),
                height * (mirror ? 2 : 1)),
            paint);
      } else {
        final angle = i / count * math.pi * 2;
        final direction = Offset(math.cos(angle), math.sin(angle));
        final outer = radius + value * size.shortestSide * .25;
        if (mode == AudioVisualMode.radial) {
          canvas.drawLine(
              center + direction * radius, center + direction * outer, paint);
        } else {
          canvas.drawCircle(center + direction * outer, 2 + value * 6, paint);
        }
      }
    }
  }

  @override
  bool shouldRepaint(AudioVisualPainter oldDelegate) =>
      oldDelegate.mode != mode ||
      oldDelegate.frame != frame ||
      oldDelegate.color != color;
}
