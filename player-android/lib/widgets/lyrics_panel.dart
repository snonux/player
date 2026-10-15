import 'package:flutter/material.dart';

import '../models/lyrics.dart';

/// Optional lyrics loading stays independent of audio setup and playback.
/// Recreate with a source key when the playback request changes.
class LyricsPanel extends StatefulWidget {
  const LyricsPanel({
    super.key,
    required this.load,
    required this.positionStream,
    required this.position,
    required this.onSeek,
  });

  final Future<Lyrics> Function() load;
  final Stream<Duration> positionStream;
  final Duration Function() position;
  final Future<void> Function(Duration) onSeek;

  @override
  State<LyricsPanel> createState() => _LyricsPanelState();
}

class _LyricsPanelState extends State<LyricsPanel> {
  bool _open = false;
  bool _loading = false;
  bool _failed = false;
  Lyrics? _lyrics;

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _failed = false;
    });
    try {
      final lyrics = await widget.load();
      if (!mounted) return;
      setState(() => _lyrics = lyrics);
    } catch (_) {
      if (!mounted) return;
      setState(() => _failed = true);
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        TextButton.icon(
          key: const Key('audio_player_lyrics'),
          style: TextButton.styleFrom(
            foregroundColor: Colors.white,
            minimumSize: const Size(48, 48),
          ),
          onPressed: () {
            setState(() => _open = !_open);
            if (_open && _lyrics == null && !_loading) _load();
          },
          icon: Icon(_open ? Icons.expand_less : Icons.lyrics_outlined),
          label: Text(_open ? 'Hide lyrics' : 'Lyrics'),
        ),
        if (_open)
          SizedBox(
            key: const Key('lyrics_panel'),
            height: 240,
            child: Material(
              color: Colors.grey.shade900,
              borderRadius: BorderRadius.circular(12),
              clipBehavior: Clip.antiAlias,
              child: _content(),
            ),
          ),
      ],
    );
  }

  Widget _content() {
    if (_loading) {
      return const Center(child: CircularProgressIndicator());
    }
    if (_failed) {
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Text('Could not load lyrics.',
                style: TextStyle(color: Colors.white70)),
            TextButton(onPressed: _load, child: const Text('Retry lyrics')),
          ],
        ),
      );
    }
    final lyrics = _lyrics;
    if (lyrics == null || lyrics.lines.isEmpty) {
      return const Center(
        child: Text('No lyrics available.',
            style: TextStyle(color: Colors.white70)),
      );
    }
    return StreamBuilder<Duration>(
      stream: widget.positionStream,
      initialData: widget.position(),
      builder: (context, snapshot) {
        final current = lyrics.currentLine(snapshot.data ?? Duration.zero);
        return Column(
          children: [
            if (current >= 0)
              ConstrainedBox(
                constraints: const BoxConstraints(maxHeight: 104),
                child: SingleChildScrollView(
                  key: ValueKey('current:$current'),
                  child: Padding(
                    padding: const EdgeInsets.all(12),
                    child: Text(
                      'Current: ${lyrics.lines[current].text}',
                      key: const Key('lyrics_current_line'),
                      style: const TextStyle(
                        color: Colors.white,
                        fontWeight: FontWeight.bold,
                      ),
                    ),
                  ),
                ),
              ),
            Expanded(
                child: ListView.builder(
              padding: const EdgeInsets.symmetric(vertical: 8),
              itemCount: lyrics.lines.length,
              itemBuilder: (context, index) {
                final line = lyrics.lines[index];
                return Semantics(
                  selected: current == index,
                  child: ListTile(
                    key: Key('lyrics_line_$index'),
                    selected: current == index,
                    selectedTileColor: Colors.white12,
                    textColor: Colors.white70,
                    selectedColor: Colors.white,
                    title: Text(line.text),
                    subtitle: lyrics.synced
                        ? Text('Tap to seek',
                            style: TextStyle(color: Colors.grey.shade400))
                        : null,
                    onTap: lyrics.synced
                        ? () async {
                            try {
                              await widget.onSeek(line.start!);
                            } catch (_) {
                              // A failed seek must not replace playback controls.
                            }
                          }
                        : null,
                  ),
                );
              },
            )),
          ],
        );
      },
    );
  }
}
