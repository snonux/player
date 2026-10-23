import '../l10n/music_localizations.dart';
import 'package:flutter/material.dart';
import '../services/music_shuffle.dart';

/// Optional filters affect this resolved collection, never an existing queue.
class MusicShuffleOptionsDialog extends StatefulWidget {
  const MusicShuffleOptionsDialog({super.key, required this.run});
  final Future<bool> Function(
      MusicShuffleOptions, bool Function(), void Function(String)) run;
  @override
  State<MusicShuffleOptionsDialog> createState() => _OptionsState();
}

class _OptionsState extends State<MusicShuffleOptionsDialog> {
  final _fields = List.generate(4, (_) => TextEditingController());
  bool _busy = false;
  String _message = '';
  @override
  void dispose() {
    for (final field in _fields) {
      field.dispose();
    }
    super.dispose();
  }

  Future<void> _submit() async {
    if (_busy) return;
    MusicShuffleOptions options;
    try {
      options = MusicShuffleOptions.parse(
          genre: _fields[0].text,
          from: _fields[1].text,
          to: _fields[2].text,
          maximum: _fields[3].text);
    } on FormatException catch (error) {
      setState(() => _message = error.message);
      return;
    }
    setState(() => _busy = true);
    bool current() => mounted && (ModalRoute.of(context)?.isCurrent ?? false);
    final success = await widget.run(options, current, (message) {
      if (current()) setState(() => _message = message);
    });
    if (!mounted || !current()) return;
    if (success) {
      Navigator.pop(context);
    } else {
      setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
          title: Text(context.tr('Shuffle options')),
          content: SingleChildScrollView(
              child: Column(mainAxisSize: MainAxisSize.min, children: [
            for (var i = 0; i < _fields.length; i++)
              TextField(
                  controller: _fields[i],
                  enabled: !_busy,
                  keyboardType:
                      i == 0 ? TextInputType.text : TextInputType.number,
                  decoration: InputDecoration(
                      labelText: '${[
                    'Genre',
                    'From year',
                    'To year',
                    'Maximum songs'
                  ][i]} (optional)')),
            const Padding(
                padding: EdgeInsets.symmetric(vertical: 12),
                child: Text(
                    'Blank includes every song. Applies to this collection only.')),
            Semantics(liveRegion: true, child: Text(_message)),
          ])),
          actions: [
            TextButton(
                onPressed: () => Navigator.pop(context),
                child: Text(context.tr('Cancel'))),
            FilledButton(
                onPressed: _busy ? null : _submit,
                child: Text(context.tr('Shuffle')))
          ]);
}
