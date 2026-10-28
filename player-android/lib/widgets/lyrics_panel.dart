import 'package:flutter/material.dart';

import '../models/lyrics.dart';

/// Bridges provider lyrics actions to the player overflow menu so the panel
/// itself stays a reader, not a second control strip.
class LyricsPanelController extends ChangeNotifier {
  bool _canFindOnline = false;
  bool _canRemoveProvider = false;
  Future<void> Function()? _findOnline;
  Future<void> Function()? _removeProvider;

  bool get canFindOnline => _canFindOnline;
  bool get canRemoveProvider => _canRemoveProvider;

  Future<void> findOnline() async {
    final action = _findOnline;
    if (action != null) await action();
  }

  Future<void> removeProvider() async {
    final action = _removeProvider;
    if (action != null) await action();
  }

  void bind({
    required bool canFindOnline,
    required bool canRemoveProvider,
    Future<void> Function()? findOnline,
    Future<void> Function()? removeProvider,
  }) {
    final changed = _canFindOnline != canFindOnline ||
        _canRemoveProvider != canRemoveProvider;
    _canFindOnline = canFindOnline;
    _canRemoveProvider = canRemoveProvider;
    _findOnline = findOnline;
    _removeProvider = removeProvider;
    if (changed) notifyListeners();
  }

  void clear() => bind(
        canFindOnline: false,
        canRemoveProvider: false,
        findOnline: null,
        removeProvider: null,
      );
}

/// Optional lyrics loading stays independent of audio setup and playback.
/// Recreate with a source key when the playback request changes.
class LyricsPanel extends StatefulWidget {
  const LyricsPanel({
    super.key,
    required this.load,
    required this.positionStream,
    required this.position,
    required this.onSeek,
    this.lookup,
    this.removeProvider,
    this.saved = false,
    this.controller,
  });

  final Future<Lyrics> Function() load;
  final Future<Lyrics> Function()? lookup;
  final Future<Lyrics> Function()? removeProvider;
  final bool saved;
  final Stream<Duration> positionStream;
  final Duration Function() position;
  final Future<void> Function(Duration) onSeek;
  final LyricsPanelController? controller;

  @override
  State<LyricsPanel> createState() => _LyricsPanelState();
}

class _LyricsPanelState extends State<LyricsPanel> {
  bool _open = false;
  bool _loading = false;
  bool _failed = false;
  Lyrics? _lyrics;
  String _language = '';

  @override
  void initState() {
    super.initState();
    _syncController();
  }

  @override
  void didUpdateWidget(covariant LyricsPanel oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.controller != widget.controller) {
      oldWidget.controller?.clear();
    }
    _syncController();
  }

  @override
  void dispose() {
    widget.controller?.clear();
    super.dispose();
  }

  Future<void> _load([Future<Lyrics> Function()? action]) async {
    setState(() {
      _loading = true;
      _failed = false;
    });
    try {
      final lyrics = await (action ?? widget.load)();
      if (!mounted) return;
      setState(() {
        _lyrics = lyrics;
        _language = '';
      });
    } catch (_) {
      if (!mounted) return;
      setState(() => _failed = true);
    } finally {
      if (mounted) {
        setState(() => _loading = false);
        _syncController();
      }
    }
  }

  Future<void> _runProvider(Future<Lyrics> Function()? action) async {
    if (!_open) setState(() => _open = true);
    await _load(action);
  }

  void _syncController() {
    final controller = widget.controller;
    if (controller == null) return;
    final saved = widget.saved || _lyrics?.saved == true;
    final lyrics = _lyrics;
    // Offer Find before the first load so the hamburger stays discoverable;
    // hide it once sidecar/embedded lyrics prove a provider lookup is wrong.
    final canFind = !saved &&
        widget.lookup != null &&
        (lyrics == null ||
            (lyrics.providerEnabled &&
                !{'sidecar', 'embedded'}.contains(lyrics.source)));
    final canRemove = !saved &&
        widget.removeProvider != null &&
        lyrics != null &&
        lyrics.providerEnabled &&
        lyrics.source == 'provider';
    controller.bind(
      canFindOnline: canFind,
      canRemoveProvider: canRemove,
      findOnline: canFind ? () => _runProvider(widget.lookup) : null,
      removeProvider:
          canRemove ? () => _runProvider(widget.removeProvider) : null,
    );
  }

  @override
  Widget build(BuildContext context) {
    final saved = widget.saved || _lyrics?.saved == true;
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
            if (_open && _lyrics == null && !_loading) {
              _load().then((_) {
                if (mounted) _syncController();
              });
            }
          },
          icon: Icon(_open ? Icons.expand_less : Icons.lyrics_outlined),
          label: Text(_open ? 'Hide lyrics' : 'Lyrics'),
        ),
        if (_open)
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 12),
            child: Column(children: [
              if (_lyrics?.source.isNotEmpty == true ||
                  saved && _lyrics != null)
                Text(
                    saved
                        ? (_lyrics?.lines.isNotEmpty == true
                            ? 'Saved lyrics'
                            : 'No saved lyrics')
                        : 'Lyrics: ${_lyrics!.source}',
                    style: const TextStyle(color: Colors.white70)),
              if (_lyrics?.providerState == 'error' ||
                  _lyrics?.providerState == 'busy')
                const Text('Lyric lookup failed. Try again.',
                    style: TextStyle(color: Colors.white70)),
              if (_lyrics?.providerState == 'empty')
                const Text('No provider match.',
                    style: TextStyle(color: Colors.white70)),
            ]),
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
            TextButton(
                onPressed: () => _load(), child: const Text('Retry lyrics')),
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
        final position = snapshot.data ?? Duration.zero;
        final active = lyrics.activeLines(position);
        final current = active.isEmpty ? -1 : active.first;
        return Column(
          children: [
            if (lyrics.languages.isNotEmpty)
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 12),
                child: DropdownButton<String>(
                  key: const Key('lyrics_language'),
                  value: _language,
                  isExpanded: true,
                  dropdownColor: Colors.grey.shade900,
                  style: const TextStyle(color: Colors.white),
                  items: ['', ...lyrics.languages]
                      .map((code) => DropdownMenuItem(
                            value: code,
                            child: Text(code.isEmpty
                                ? 'Language: Original${lyrics.language.isEmpty ? '' : ' (${lyrics.language})'}'
                                : 'Language: $code'),
                          ))
                      .toList(),
                  onChanged: (value) {
                    if (value != null) setState(() => _language = value);
                  },
                ),
              ),
            if (current >= 0)
              ConstrainedBox(
                constraints: const BoxConstraints(maxHeight: 104),
                child: SingleChildScrollView(
                  key: ValueKey('current:$current'),
                  child: Padding(
                    padding: const EdgeInsets.all(12),
                    child: _lineText(lyrics.lines[current], position, true,
                        prefix:
                            'Current: ${lyrics.lines[current].voice.isEmpty ? '' : 'Voice: ${lyrics.lines[current].voice} · '}',
                        currentBanner: true),
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
                  selected: active.contains(index),
                  child: ListTile(
                    key: Key('lyrics_line_$index'),
                    selected: active.contains(index),
                    selectedTileColor: Colors.white12,
                    textColor: Colors.white70,
                    selectedColor: Colors.white,
                    title: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        if (line.voice.isNotEmpty)
                          Text('Voice: ${line.voice}',
                              style: const TextStyle(
                                  color: Colors.white70, fontSize: 12)),
                        _lineText(line, position, active.contains(index)),
                        if (_language.isNotEmpty &&
                            !line.translations.containsKey(_language))
                          Text(
                              'Original${lyrics.language.isEmpty ? '' : ': ${lyrics.language}'}',
                              style: const TextStyle(
                                  color: Colors.white70, fontSize: 12)),
                      ],
                    ),
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

  Widget _lineText(LyricsLine line, Duration position, bool active,
      {String prefix = '', bool currentBanner = false}) {
    final key = currentBanner ? const Key('lyrics_current_line') : null;
    final style = currentBanner
        ? const TextStyle(color: Colors.white, fontWeight: FontWeight.bold)
        : null;
    final fallback = currentBanner &&
            _language.isNotEmpty &&
            !line.translations.containsKey(_language)
        ? ' (Original${_lyrics!.language.isEmpty ? '' : ': ${_lyrics!.language}'})'
        : '';
    if (line.words.isEmpty || line.translations.containsKey(_language)) {
      return Text('$prefix${line.displayText(_language)}$fallback',
          key: key, style: style);
    }
    final current = active ? line.currentWord(position) : -1;
    return Text.rich(
      TextSpan(children: [
        if (prefix.isNotEmpty) TextSpan(text: prefix),
        for (var i = 0; i < line.words.length; i++)
          TextSpan(
              text: line.words[i].text,
              style: i == current
                  ? const TextStyle(
                      fontWeight: FontWeight.bold,
                      decoration: TextDecoration.underline,
                    )
                  : null),
        if (fallback.isNotEmpty) TextSpan(text: fallback),
      ]),
      key: key,
      style: style,
    );
  }
}
