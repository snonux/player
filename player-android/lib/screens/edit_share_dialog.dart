import 'package:flutter/material.dart';

import '../api/player_api_client.dart';
import '../l10n/music_localizations.dart';
import '../models/share.dart';
import '../widgets/share_quality_picker.dart';

/// Edits the same link, retaining its exact expiry unless a picker changes it.
class EditShareDialog extends StatefulWidget {
  const EditShareDialog({super.key, required this.share, required this.client});
  final Share share;
  final PlayerApiClient client;
  @override
  State<EditShareDialog> createState() => _EditShareDialogState();
}

class _EditShareDialogState extends State<EditShareDialog> {
  late final _description =
      TextEditingController(text: widget.share.description);
  late DateTime? _expiry = widget.share.expiresAt?.toLocal();
  bool _saving = false, _expiryChanged = false;
  String? _error;
  late String _audioQuality = widget.share.audioQuality;

  @override
  void dispose() {
    _description.dispose();
    super.dispose();
  }

  Future<void> _pickExpiry() async {
    final now = DateTime.now();
    final initial = _expiry != null && _expiry!.isAfter(now) ? _expiry! : now;
    final date = await showDatePicker(
      context: context,
      initialDate: initial,
      firstDate: DateTime(now.year, now.month, now.day),
      lastDate: DateTime(initial.year + 100),
    );
    if (date == null || !mounted) return;
    final time = await showTimePicker(
        context: context, initialTime: TimeOfDay.fromDateTime(initial));
    if (time == null || !mounted) return;
    setState(() {
      _expiry =
          DateTime(date.year, date.month, date.day, time.hour, time.minute);
      _expiryChanged = true;
    });
  }

  Future<void> _save() async {
    if (_saving) return;
    if (_description.text.runes.length > 1000) {
      setState(() => _error = 'Description must be at most 1000 characters.');
      return;
    }
    if (_expiryChanged && !_expiry!.isAfter(DateTime.now())) {
      setState(() => _error = 'Choose a future expiry.');
      return;
    }
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      final updated = await widget.client.updateShare(widget.share.token,
          description: _description.text,
          audioQuality:
              widget.share.mediaType == 'audio' ? _audioQuality : null,
          expiresAt: _expiryChanged ? _expiry : null);
      if (mounted) Navigator.of(context).pop(updated);
    } catch (_) {
      if (mounted) {
        setState(() {
          _saving = false;
          _error =
              'Could not save share. Check your connection and edit permission, then retry.';
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) => PopScope(
        canPop: !_saving,
        child: AlertDialog(
          title: Text(context.tr('Edit share')),
          content: SingleChildScrollView(
              child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text(widget.share.fileName ?? '',
                  maxLines: 2, overflow: TextOverflow.ellipsis),
              TextField(
                key: const Key('edit_share_description'),
                controller: _description,
                autofocus: true,
                enabled: !_saving,
                minLines: 2,
                maxLines: 5,
                decoration:
                    InputDecoration(labelText: context.tr('Description')),
              ),
              const SizedBox(height: 16),
              Text(context.tr('Link expires (local time)')),
              Text(_expiry?.toString() ?? ''),
              TextButton.icon(
                key: const Key('edit_share_expiry'),
                onPressed: _saving ? null : _pickExpiry,
                icon: const Icon(Icons.event),
                label: Text(context.tr('Change expiry')),
              ),
              if (widget.share.mediaType == 'audio')
                ShareQualityPicker(
                    client: widget.client,
                    value: _audioQuality,
                    enabled: !_saving,
                    onChanged: (value) =>
                        setState(() => _audioQuality = value)),
              Text(context.tr('The link and viewing limits stay the same.')),
              if (_error != null)
                Semantics(
                    liveRegion: true,
                    child: Text(context.tr(_error!),
                        style: TextStyle(
                            color: Theme.of(context).colorScheme.error))),
            ],
          )),
          actions: [
            TextButton(
                onPressed: _saving ? null : () => Navigator.of(context).pop(),
                child: Text(context.tr('Cancel'))),
            FilledButton(
                key: const Key('edit_share_save'),
                onPressed: _saving ? null : _save,
                child: Text(context.tr(_saving ? 'Saving…' : 'Save'))),
          ],
        ),
      );
}
