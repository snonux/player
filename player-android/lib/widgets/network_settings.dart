import 'package:flutter/material.dart';
import '../services/network_policy.dart';

class MusicNetworkSettings extends StatefulWidget {
  const MusicNetworkSettings({super.key});
  @override
  State<MusicNetworkSettings> createState() => _MusicNetworkSettingsState();
}

class _MusicNetworkSettingsState extends State<MusicNetworkSettings> {
  bool loading = true;
  String? error;
  @override
  void initState() {
    super.initState();
    MusicNetworkPolicy.changes.addListener(changed);
    MusicNetworkPolicy.initialize().then((_) {
      if (mounted) setState(() => loading = false);
    }).catchError((Object _) {
      if (mounted) {
        setState(() {
          loading = false;
          error = 'Could not load network settings. Try reopening Settings.';
        });
      }
    });
  }

  void changed() {
    if (mounted) setState(() {});
  }

  @override
  void dispose() {
    MusicNetworkPolicy.changes.removeListener(changed);
    super.dispose();
  }

  Future<void> save(String name, Object value) async {
    try {
      await MusicNetworkPolicy.save(name, value);
      if (mounted) setState(() => error = null);
    } catch (_) {
      if (mounted) {
        setState(() => error = 'Could not save network settings. Try again.');
      }
    }
  }

  Widget toggle(String title, String key, {bool fallback = false}) =>
      SwitchListTile(
          contentPadding: EdgeInsets.zero,
          title: Text(title),
          value: MusicNetworkPolicy.flag(key, fallback: fallback),
          onChanged: loading ? null : (value) => save(key, value));
  @override
  Widget build(BuildContext context) => ExpansionTile(
          tilePadding: EdgeInsets.zero,
          title: const Text('Music network'),
          subtitle: Text(
              'Using ${MusicNetworkPolicy.network == 'wifi' ? 'Wi-Fi' : MusicNetworkPolicy.network == 'mobile' ? 'Mobile' : 'Mobile settings (network unknown)'}'),
          children: [
            const Text(
                'Auto follows the device network type. Unknown networks use Mobile settings. Wi-Fi includes Ethernet; mobile hotspots may still cost data. Choose a manual override when needed.'),
            if (loading) const LinearProgressIndicator(),
            DropdownButtonFormField<String>(
                key: ValueKey(MusicNetworkPolicy.mode),
                initialValue: MusicNetworkPolicy.mode,
                decoration: const InputDecoration(labelText: 'Network'),
                isExpanded: true,
                items: const [
                  DropdownMenuItem(value: 'auto', child: Text('Auto')),
                  DropdownMenuItem(
                      value: 'wifi', child: Text('Wi-Fi (manual)')),
                  DropdownMenuItem(
                      value: 'mobile', child: Text('Mobile (manual)'))
                ],
                onChanged: loading
                    ? null
                    : (value) {
                        if (value != null) save('mode', value);
                      }),
            toggle('Allow prefetch on Wi-Fi', 'wifi-prefetch', fallback: true),
            toggle('Allow prefetch on Mobile / unknown', 'mobile-prefetch'),
            toggle('Stream library audio on Wi-Fi only', 'stream-wifi'),
            toggle('Sync saved music session on Wi-Fi only', 'sync-wifi'),
            if (MusicNetworkPolicy.streamBlocked)
              const Text(
                  'New library audio streams wait for Wi-Fi. Current playback keeps its source.'),
            if (!MusicNetworkPolicy.syncAllowed)
              const Text(
                  'Session sync waits for Wi-Fi; queue edits stay on this device.'),
            const Text(
                'Choose quality for each network in the audio player’s Stream quality sheet. Prefetch also needs Playback cache prefetch enabled. Changes affect new tracks and transfers. Downloads, live radio, public shares, Cast, progress and podcast sync keep their own policy.'),
            if (error != null)
              Text(error!,
                  style: TextStyle(color: Theme.of(context).colorScheme.error)),
          ]);
}
