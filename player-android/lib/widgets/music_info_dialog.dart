import 'package:flutter/material.dart';

import '../models/media.dart';
import '../utils/duration_formatter.dart';
import 'music_metadata_details.dart';

/// Read-only server information; loading and closing never touch the player.
class MusicInfoDialog extends StatefulWidget {
  const MusicInfoDialog(
      {super.key, required this.load, required this.isCurrent});
  final Future<Media> Function() load;
  final bool Function() isCurrent;

  @override
  State<MusicInfoDialog> createState() => _MusicInfoDialogState();
}

class _MusicInfoDialogState extends State<MusicInfoDialog> {
  Media? _media;
  bool _loading = true;
  bool _failed = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _failed = false;
      _media = null;
    });
    try {
      if (!widget.isCurrent()) throw StateError('Source changed');
      final media = await widget.load();
      if (!mounted) return;
      if (!widget.isCurrent()) throw StateError('Source changed');
      setState(() {
        _media = media;
        _loading = false;
      });
    } catch (_) {
      if (mounted) {
        setState(() {
          _failed = true;
          _loading = false;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final media = _media;
    return AlertDialog(
      title: const Text('Music and file information'),
      content: SizedBox(
          width: double.maxFinite,
          child: SingleChildScrollView(
            child: _loading
                ? const Text('Loading information…')
                : _failed
                    ? const Text(
                        'Could not load information. Check your connection and access, then retry.')
                    : media == null
                        ? const SizedBox.shrink()
                        : MusicMetadataDetails(
                            music: media.music,
                            filename: media.fileName,
                            path: media.relPath,
                            fileDetails: {
                                if (media.codec.isNotEmpty)
                                  'Codec': media.codec,
                                if (media.bitrate > 0)
                                  'Bitrate':
                                      '${(media.bitrate / 1000).round()} kbps (${media.bitrate} bps)',
                                if (media.fileSizeBytes > 0)
                                  'File size': '${media.fileSizeBytes} bytes',
                                if (media.duration > 0)
                                  'Duration': formatDuration(media.duration),
                              }),
          )),
      actions: [
        if (_failed) TextButton(onPressed: _load, child: const Text('Retry')),
        TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('Close')),
      ],
    );
  }
}
