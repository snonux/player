import 'package:flutter/material.dart';
import '../api/player_api_client.dart';
import '../services/stream_quality.dart';
import 'download_quality_picker.dart';

Future<void> showStreamQualitySheet(
        BuildContext context, PlayerApiClient client) =>
    showModalBottomSheet<void>(
        context: context,
        isScrollControlled: true,
        useSafeArea: true,
        builder: (_) => _QualitySheet(client: client));

class _QualitySheet extends StatefulWidget {
  const _QualitySheet({required this.client});
  final PlayerApiClient client;
  @override
  State<_QualitySheet> createState() => _QualitySheetState();
}

class _QualitySheetState extends State<_QualitySheet> {
  List<Map<String, dynamic>> profiles = [];
  String status = 'Loading server profiles…';
  bool saving = false;
  String get server => widget.client.rawDio.options.baseUrl;
  late String selected;
  String network = 'wifi';
  @override
  void initState() {
    super.initState();
    selected = StreamQuality.forNetwork(server, network);
    load();
  }

  Future<void> load() async {
    setState(() {
      status = 'Loading server profiles…';
    });
    try {
      final result = await widget.client.streamProfiles();
      if (mounted) {
        setState(() {
          profiles = result;
          status = 'Server profiles ready.';
        });
      }
    } catch (_) {
      if (mounted) {
        setState(() {
          status =
              'Server profiles unavailable. Auto and Original remain available.';
        });
      }
    }
  }

  Future<void> choose(String? value) async {
    if (value == null) return;
    setState(() {
      saving = true;
    });
    try {
      await StreamQuality.save(server, value, network: network);
      if (mounted) {
        setState(() {
          selected = value;
          status = 'Saved. Applies when you next open a track.';
        });
      }
    } catch (_) {
      if (mounted) {
        setState(() {
          status = 'Could not save stream quality. Try again.';
        });
      }
    } finally {
      if (mounted) {
        setState(() {
          saving = false;
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final choices = <String, String>{
      'auto': 'Auto (compatibility when needed)',
      'original': 'Original (no conversion)',
      for (final p in profiles) p['id'] as String: p['label'] as String
    };
    choices.putIfAbsent(selected, () => 'Unavailable profile ($selected)');
    return SingleChildScrollView(
        child: Padding(
            padding: const EdgeInsets.all(24),
            child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text('Stream quality',
                      style: Theme.of(context).textTheme.titleLarge),
                  const SizedBox(height: 16),
                  DropdownButtonFormField<String>(
                      initialValue: network,
                      decoration: const InputDecoration(
                          labelText: 'Network settings to edit'),
                      items: const [
                        DropdownMenuItem(value: 'wifi', child: Text('Wi-Fi')),
                        DropdownMenuItem(
                            value: 'mobile', child: Text('Mobile / unknown'))
                      ],
                      onChanged: saving
                          ? null
                          : (value) {
                              if (value != null) {
                                setState(() {
                                  network = value;
                                  selected =
                                      StreamQuality.forNetwork(server, network);
                                });
                              }
                            }),
                  const SizedBox(height: 16),
                  DropdownButtonFormField<String>(
                      key: ValueKey('$network:$selected'),
                      initialValue: selected,
                      isExpanded: true,
                      decoration: const InputDecoration(labelText: 'Quality'),
                      items: choices.entries
                          .map((p) => DropdownMenuItem(
                              value: p.key,
                              child: Text(p.value,
                                  overflow: TextOverflow.ellipsis)))
                          .toList(),
                      onChanged: saving ? null : choose),
                  const SizedBox(height: 16),
                  const Text(
                      'Separate Wi-Fi and Mobile quality, saved for this server on this device. Music network in Settings chooses which applies. Applies next time you open a track. Original may fail if the device cannot decode it. Local files, offline downloads, shared links and Cast keep their own playback policy.'),
                  const SizedBox(height: 16),
                  Text(status, semanticsLabel: status),
                  TextButton(
                      onPressed: load, child: const Text('Reload profiles')),
                  TextButton(
                      onPressed: () =>
                          showDownloadQualitySheet(context, widget.client),
                      child: const Text('Download quality…')),
                ])));
  }
}
