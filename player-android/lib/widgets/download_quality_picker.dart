import 'package:flutter/material.dart';
import '../api/player_api_client.dart';
import '../services/download_quality.dart';

/// Shared touch-first preference control for download dialogs and Settings.
class DownloadQualityPicker extends StatefulWidget {
  const DownloadQualityPicker(
      {super.key,
      required this.client,
      this.enabled = true,
      this.onSelectionReady,
      this.loadPreference = DownloadQuality.selected,
      this.savePreference = DownloadQuality.save});
  final PlayerApiClient client;
  final bool enabled;

  /// Null blocks starting a download until the displayed selection is durable.
  final ValueChanged<String?>? onSelectionReady;
  final Future<String> Function(String server) loadPreference;
  final Future<void> Function(String server, String profile) savePreference;
  @override
  State<DownloadQualityPicker> createState() => _DownloadQualityPickerState();
}

class _DownloadQualityPickerState extends State<DownloadQualityPicker> {
  String selected = 'original';
  String status = 'Loading download profiles…';
  bool saving = false;
  bool initialized = false;
  List<Map<String, dynamic>> profiles = [];
  String get server => widget.client.rawDio.options.baseUrl;

  @override
  void initState() {
    super.initState();
    load();
  }

  Future<void> load({bool reload = false}) async {
    if (reload) {
      widget.onSelectionReady?.call(null);
      setState(() => initialized = false);
    }
    try {
      final saved = await widget.loadPreference(server);
      if (!mounted) return;
      setState(() {
        selected = saved;
        initialized = true;
      });
      widget.onSelectionReady?.call(saved);
      final result = await widget.client.streamProfiles();
      if (mounted) {
        setState(() {
          profiles = result;
          status = 'Profiles ready.';
        });
      }
    } catch (_) {
      if (mounted) {
        setState(() => status = initialized
            ? 'Profiles unavailable. Original remains available; reload to retry.'
            : 'Could not load download quality. Reload before downloading.');
      }
    }
  }

  Future<void> choose(String? value) async {
    if (value == null) return;
    final previous = selected;
    widget.onSelectionReady?.call(null);
    setState(() {
      saving = true;
      selected = value;
    });
    try {
      await widget.savePreference(server, value);
      if (!mounted) return;
      setState(() {
        selected = value;
        status = 'Saved for new downloads.';
      });
      widget.onSelectionReady?.call(value);
    } catch (_) {
      if (mounted) {
        setState(() {
          selected = previous;
          status =
              'Could not save download quality. Previous selection retained.';
        });
        widget.onSelectionReady?.call(previous);
      }
    } finally {
      if (mounted) setState(() => saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final choices = <String, String>{
      'original': 'Original (no conversion)',
      for (final p in profiles) p['id'] as String: p['label'] as String
    };
    choices.putIfAbsent(selected, () => 'Unavailable profile ($selected)');
    return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
      DropdownButtonFormField<String>(
          key: ValueKey(selected),
          initialValue: selected,
          isExpanded: true,
          decoration: const InputDecoration(labelText: 'Download quality'),
          items: choices.entries
              .map((p) => DropdownMenuItem(
                  value: p.key,
                  child: Text(p.value, overflow: TextOverflow.ellipsis)))
              .toList(),
          onChanged: widget.enabled && initialized && !saving ? choose : null),
      const SizedBox(height: 12),
      const Text(
          'Separate from streaming quality. Original preserves the source and may need a compatible player. Lower bitrates usually use less storage. Existing downloads stay unchanged; remove a copy to download it again at another quality.'),
      Semantics(liveRegion: true, child: Text(status)),
      TextButton(
          onPressed:
              widget.enabled && !saving ? () => load(reload: true) : null,
          child: const Text('Reload download profiles')),
    ]);
  }
}

Future<void> showDownloadQualitySheet(
        BuildContext context, PlayerApiClient client) =>
    showModalBottomSheet<void>(
        context: context,
        isScrollControlled: true,
        useSafeArea: true,
        builder: (_) => SingleChildScrollView(
            child: Padding(
                padding: const EdgeInsets.all(24),
                child: Column(
                    mainAxisSize: MainAxisSize.min,
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text('Download quality',
                          style: Theme.of(context).textTheme.titleLarge),
                      const SizedBox(height: 16),
                      DownloadQualityPicker(client: client),
                    ]))));
