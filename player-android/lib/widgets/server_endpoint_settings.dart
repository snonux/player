import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/server_endpoint_profile.dart';
import '../providers/settings_provider.dart';
import '../providers/server_endpoint_provider.dart';
import '../services/server_endpoint_router.dart';

/// Optional routing controls; changing these never changes the saved account.
class ServerEndpointSettings extends ConsumerStatefulWidget {
  const ServerEndpointSettings({super.key});
  @override
  ConsumerState<ServerEndpointSettings> createState() =>
      _ServerEndpointSettingsState();
}

class _ServerEndpointSettingsState
    extends ConsumerState<ServerEndpointSettings> {
  final _local = TextEditingController();
  final _wifi = TextEditingController();
  String? _seeded;
  String? _message;
  bool _preferLocal = true;
  bool _busy = false;
  bool _trusted = false;
  @override
  void dispose() {
    _local.dispose();
    _wifi.dispose();
    super.dispose();
  }

  Future<void> _save(String primary) async {
    setState(() {
      _busy = true;
      _message = 'Testing both addresses without login details…';
    });
    try {
      final notifier = ref.read(settingsProvider.notifier);
      if (_local.text.trim().isEmpty) {
        await notifier.saveEndpointProfile(primary, null);
        if (mounted) {
          setState(() =>
              _message = 'Paired address removed. Using primary address.');
        }
        return;
      }
      final local = parseServerBaseUrl(_local.text).origin;
      if (local == primary) {
        throw const FormatException('Use a different local address.');
      }
      if (!_trusted) {
        throw const FormatException('Confirm that you trust both addresses.');
      }
      final ids = await Future.wait([
        probeServerIdentity(Uri.parse(primary)),
        probeServerIdentity(Uri.parse(local))
      ]);
      if (ids[0] != ids[1]) throw const EndpointIdentityException();
      if (!mounted ||
          ref.read(settingsProvider).valueOrNull?.serverBaseUrl != primary) {
        return;
      }
      await notifier.saveEndpointProfile(
          primary,
          ServerEndpointProfile(
              primary: primary,
              local: local,
              serverId: ids[0],
              preferLocal: _preferLocal,
              wifiName: _wifi.text.trim()));
      if (mounted) {
        setState(() => _message =
            'Both addresses reach the same server. Saved for this account profile.');
      }
    } catch (error) {
      if (mounted) {
        setState(() => _message = error is FormatException
            ? error.message.toString()
            : error is EndpointIdentityException
                ? error.toString()
                : 'Could not test both addresses. Check the addresses and network, then retry.');
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _readWifi() async {
    setState(() => _busy = true);
    final name = await currentServerWifiName(requestPermission: true);
    if (!mounted) return;
    setState(() {
      _busy = false;
      if (name != null && name.isNotEmpty) {
        _wifi.text = name;
        _message = 'Wi-Fi name filled in. Test and save to apply.';
      } else {
        _message =
            'Wi-Fi name unavailable. Allow precise location and turn on Location and Wi-Fi, or enter the name. When unavailable, Player uses the primary address.';
      }
    });
  }

  @override
  Widget build(BuildContext context) {
    final settings = ref.watch(settingsProvider).valueOrNull;
    final primary = settings?.serverBaseUrl;
    if (primary == null) return const SizedBox.shrink();
    final profile = settings!.endpointProfiles[primary];
    if (_seeded != primary) {
      _seeded = primary;
      _local.text = profile?.local ?? '';
      _wifi.text = profile?.wifiName ?? '';
      _preferLocal = profile?.preferLocal ?? true;
      _trusted = profile != null;
    }
    return ExpansionTile(
        key: const Key('server_endpoint_advanced'),
        tilePadding: EdgeInsets.zero,
        title: const Text('Advanced connection settings'),
        subtitle: const Text('Optional local address · on this device'),
        children: [
          Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
            Text('Primary address: $primary'),
            const SizedBox(height: 12),
            TextField(
                key: const Key('server_local_url'),
                controller: _local,
                enabled: !_busy,
                keyboardType: TextInputType.url,
                autocorrect: false,
                decoration: const InputDecoration(
                    labelText: 'Local server address (optional)',
                    border: OutlineInputBorder(),
                    helperText:
                        'Leave blank to use only the primary address.')),
            SwitchListTile(
                contentPadding: EdgeInsets.zero,
                title: const Text('Prefer the local address'),
                subtitle: const Text(
                    'If unavailable, try the primary address. Authentication errors never switch servers.'),
                value: _preferLocal,
                onChanged:
                    _busy ? null : (v) => setState(() => _preferLocal = v)),
            TextField(
                key: const Key('server_wifi_name'),
                controller: _wifi,
                enabled: !_busy,
                decoration: const InputDecoration(
                    labelText: 'Only on Wi-Fi named (optional)',
                    border: OutlineInputBorder(),
                    helperText:
                        'Exact name. Blank allows local routing on any network.')),
            TextButton.icon(
                onPressed: _busy ? null : _readWifi,
                icon: const Icon(Icons.wifi),
                label: const Text('Use current Wi-Fi name')),
            const Text(
                'Reading Wi-Fi names requires precise location permission and Location enabled. Without a matching name, only the primary address is used.'),
            CheckboxListTile(
                contentPadding: EdgeInsets.zero,
                value: _trusted,
                onChanged:
                    _busy ? null : (v) => setState(() => _trusted = v ?? false),
                title: const Text('I trust both addresses for this account'),
                subtitle: const Text(
                    'HTTP sends login details and media without encryption. Use only a trusted network. Server IDs detect a different installation; they do not prove an address is safe.')),
            if (_message != null)
              Semantics(liveRegion: true, child: Text(_message!)),
            const SizedBox(height: 8),
            FilledButton.icon(
                key: const Key('server_endpoint_test_save'),
                onPressed: _busy ? null : () => _save(primary),
                icon: _busy
                    ? const SizedBox(
                        width: 18,
                        height: 18,
                        child: CircularProgressIndicator(strokeWidth: 2))
                    : const Icon(Icons.network_check),
                label: Text(_busy ? 'Testing…' : 'Test and save addresses')),
            if (profile != null)
              Semantics(
                  liveRegion: true,
                  child: Text(ref.watch(serverEndpointStatusProvider))),
            const SizedBox(height: 12),
          ])
        ]);
  }
}
