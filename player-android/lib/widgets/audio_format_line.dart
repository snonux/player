import 'package:flutter/material.dart';

import '../models/audio_format.dart';

/// Shows the codec and quality of the playing audio in one quiet line.
///
/// Loading is independent of playback: a failed or empty answer shows
/// nothing. Recreate with a source key when the playback request changes.
class AudioFormatLine extends StatefulWidget {
  const AudioFormatLine({super.key, required this.load});

  final Future<AudioFormat> Function() load;

  @override
  State<AudioFormatLine> createState() => _AudioFormatLineState();
}

class _AudioFormatLineState extends State<AudioFormatLine> {
  String _summary = '';

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final format = await widget.load();
      if (mounted) setState(() => _summary = format.summary);
    } catch (_) {
      // Format details are decoration and never replace playback controls.
    }
  }

  @override
  Widget build(BuildContext context) {
    if (_summary.isEmpty) return const SizedBox.shrink();
    return Padding(
      padding: const EdgeInsets.only(top: 4),
      child: Semantics(
        label: 'Audio format: $_summary',
        excludeSemantics: true,
        child: Text(
          _summary,
          key: const Key('audio_player_format'),
          textAlign: TextAlign.center,
          style: const TextStyle(color: Colors.white70, fontSize: 12),
        ),
      ),
    );
  }
}
