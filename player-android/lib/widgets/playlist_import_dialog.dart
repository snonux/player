import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers/api_client_provider.dart';
import '../utils/error_mappers.dart';

/// Guided import of a server filesystem source, with explicit base and mode.
class PlaylistImportDialog extends ConsumerStatefulWidget {
  const PlaylistImportDialog({super.key});
  @override
  ConsumerState<PlaylistImportDialog> createState() => _PlaylistImportState();
}

class _PlaylistImportState extends ConsumerState<PlaylistImportDialog> {
  final _path = TextEditingController();
  String _base = 'folder';
  bool _follow = false, _busy = false;
  Map<String, dynamic>? _preview;
  String? _error;
  @override
  void dispose() {
    _path.dispose();
    super.dispose();
  }

  void _invalidate() => setState(() {
        _preview = null;
        _error = null;
      });
  Future<void> _submit() async {
    if (_busy || _path.text.trim().isEmpty) return;
    final api = ref.read(apiClientProvider);
    final epoch = ref.read(credentialMutationQueueProvider).generation;
    final confirming = _preview != null;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final result = await api.importPlaylist({
        'path': _path.text.trim(),
        'base': _base,
        'follow': _follow,
        'preview': !confirming,
        'fingerprint': _preview?['fingerprint'] ?? '',
      });
      if (!mounted ||
          !identical(api, ref.read(apiClientProvider)) ||
          epoch != ref.read(credentialMutationQueueProvider).generation) {
        return;
      }
      if (result['playlist'] != null) {
        Navigator.of(context)
            .pop((result['playlist'] as Map)['playlist_id'] as String);
        return;
      }
      setState(() => _preview = result);
    } catch (e) {
      if (mounted &&
          identical(api, ref.read(apiClientProvider)) &&
          epoch == ref.read(credentialMutationQueueProvider).generation) {
        setState(() {
          _preview = null;
          _error = 'Could not import playlist. ${setsErrorMessage(e)}';
        });
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    ref.listen(apiClientProvider, (previous, next) {
      if (previous != next && mounted) Navigator.of(context).pop();
    });
    final preview = _preview;
    return AlertDialog(
      title: const Text('Import M3U'),
      content: SizedBox(
          width: 440,
          child: SingleChildScrollView(
              child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                TextField(
                    controller: _path,
                    enabled: !_busy,
                    autofocus: true,
                    onChanged: (_) => _invalidate(),
                    decoration: const InputDecoration(
                        labelText: 'Server library path',
                        hintText: 'Music/lists/trip.m3u8')),
                const Text(
                    'Choose a server file inside a set you can access. This does not upload a file from this device.'),
                const SizedBox(height: 16),
                DropdownButtonFormField<String>(
                    initialValue: _base,
                    isExpanded: true,
                    decoration:
                        const InputDecoration(labelText: 'Paths relative to'),
                    items: const [
                      DropdownMenuItem(
                          value: 'folder', child: Text('Playlist folder')),
                      DropdownMenuItem(
                          value: 'library',
                          child: Text('Library root (Player exports)'))
                    ],
                    onChanged: _busy
                        ? null
                        : (value) {
                            _base = value!;
                            _invalidate();
                          }),
                SwitchListTile(
                    contentPadding: EdgeInsets.zero,
                    title: const Text('Follow source changes'),
                    value: _follow,
                    onChanged: _busy
                        ? null
                        : (value) {
                            _follow = value;
                            _invalidate();
                          }),
                const Text(
                    'One-time import is editable. Following replaces songs on library rescans and when the owner opens playlists; stop following to edit songs. Name, comment and sharing stay manual.'),
                if (_busy)
                  const Padding(
                      padding: EdgeInsets.all(16),
                      child: Text('Reading source…')),
                if (preview != null) ...[
                  const SizedBox(height: 16),
                  Text(preview['name'] as String,
                      style: Theme.of(context).textTheme.titleMedium),
                  Text(
                      '${preview['available']} songs · ${preview['unavailable']} unavailable · ${preview['rejected']} unsafe entries skipped.'),
                  if ((preview['unavailable_lines'] as List).isNotEmpty)
                    Text(
                        'Unavailable source lines: ${(preview['unavailable_lines'] as List).join(', ')}${(preview['unavailable'] as int) > (preview['unavailable_lines'] as List).length ? '…' : ''}. Missing and inaccessible songs are skipped.'),
                  Text(_follow
                      ? 'Ongoing sync: source changes replace this playlist’s songs.'
                      : 'One-time import: later source changes do not change this playlist.'),
                ],
                if (_error != null)
                  Semantics(
                      liveRegion: true,
                      child: Text(_error!,
                          style: TextStyle(
                              color: Theme.of(context).colorScheme.error))),
              ]))),
      actions: [
        TextButton(
            onPressed: () => Navigator.of(context).pop(),
            child: const Text('Cancel')),
        FilledButton(
            onPressed: _busy ? null : _submit,
            child: Text(preview == null
                ? 'Preview import'
                : _follow
                    ? 'Import and follow'
                    : 'Import playlist'))
      ],
    );
  }
}
