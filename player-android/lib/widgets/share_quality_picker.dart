import 'package:flutter/material.dart';
import '../api/player_api_client.dart';

String shareQualityLabel(String id) =>
    const {
      '': 'Auto (compatibility when needed)',
      'original': 'Original (no conversion)',
      'aac128': 'Balanced · AAC 128 kbps',
      'aac256': 'High · AAC 256 kbps',
      'mp3192': 'Compatible · MP3 192 kbps',
      'opus96': 'Data saver · Opus 96 kbps',
    }[id] ??
    'Unavailable profile ($id)';

/// Per-link policy; never changes this device's private playback preferences.
class ShareQualityPicker extends StatefulWidget {
  const ShareQualityPicker(
      {super.key,
      required this.client,
      required this.value,
      required this.onChanged,
      this.enabled = true});
  final PlayerApiClient client;
  final String value;
  final ValueChanged<String> onChanged;
  final bool enabled;
  @override
  State<ShareQualityPicker> createState() => _ShareQualityPickerState();
}

class _ShareQualityPickerState extends State<ShareQualityPicker> {
  List<Map<String, dynamic>> _profiles = [];
  String _status = 'Loading server profiles…';
  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final profiles = await widget.client.streamProfiles();
      if (mounted) {
        setState(() {
          _profiles = profiles;
          _status = 'Profiles ready.';
        });
      }
    } catch (_) {
      if (mounted) {
        setState(() => _status =
            'Profiles unavailable. Auto and Original remain available. Reload to retry.');
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final choices = <String, String>{
      '': shareQualityLabel(''),
      'original': shareQualityLabel('original'),
      for (final profile in _profiles)
        profile['id'] as String: profile['label'] as String
    };
    choices.putIfAbsent(widget.value, () => shareQualityLabel(widget.value));
    return ExpansionTile(
        title: const Text('Audio quality'),
        tilePadding: EdgeInsets.zero,
        children: [
          DropdownButtonFormField<String>(
              key: ValueKey(widget.value),
              initialValue: widget.value,
              isExpanded: true,
              decoration: const InputDecoration(
                  labelText: 'Shared playback and downloads'),
              items: choices.entries
                  .map((choice) => DropdownMenuItem(
                      value: choice.key,
                      child:
                          Text(choice.value, overflow: TextOverflow.ellipsis)))
                  .toList(),
              onChanged: widget.enabled
                  ? (value) {
                      if (value != null) widget.onChanged(value);
                    }
                  : null),
          const Text(
              'Auto converts playback only when needed. Original needs a compatible player. Selected encodes apply to playback and downloads. Bitrates are nominal targets.'),
          Semantics(liveRegion: true, child: Text(_status)),
          TextButton(
              onPressed: widget.enabled ? _load : null,
              child: const Text('Reload profiles')),
        ]);
  }
}
