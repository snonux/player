import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../api/server_plugins_api.dart';
import '../providers/api_client_provider.dart';
import '../providers/auth_state_provider.dart';

/// Optional server administration, kept outside routine music screens.
class ServerPluginsScreen extends ConsumerWidget {
  const ServerPluginsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final auth = ref.watch(authStateProvider);
    final user = auth.valueOrNull?.user;
    final allowed = !auth.isLoading &&
        auth.valueOrNull?.isAuthenticated == true &&
        user?.isAdmin == true;
    final api = ref.watch(serverPluginsApiProvider);
    final origin = ref.watch(playerBaseUrlProvider).origin;
    final epoch = api.currentEpoch();
    return Scaffold(
        appBar: AppBar(title: const Text('Server plugins')),
        body: SafeArea(
            child: allowed
                ? _PluginList(
                    key: ValueKey((origin, user!.id, epoch, api)),
                    api: api,
                    epoch: epoch)
                : const Center(
                    child: Padding(
                    padding: EdgeInsets.all(24),
                    child: Text('Administrator access required.'),
                  ))));
  }
}

class _PluginList extends StatefulWidget {
  const _PluginList({super.key, required this.api, required this.epoch});
  final ServerPluginsApi api;
  final int epoch;
  @override
  State<_PluginList> createState() => _PluginListState();
}

class _PluginListState extends State<_PluginList> {
  List<ServerPlugin> _plugins = [];
  bool _busy = true;
  bool _forbidden = false;
  String? _error;
  String? _result;
  BuildContext? _dialogContext;

  bool get _current => mounted && widget.api.currentEpoch() == widget.epoch;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    final dialog = _dialogContext;
    if (dialog != null) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (dialog.mounted) Navigator.of(dialog).pop();
      });
    }
    super.dispose();
  }

  String _message(Object error) {
    if (error is DioException) {
      if (error.response?.statusCode == 403) {
        _forbidden = true;
        _plugins = [];
        return 'Administrator access required.';
      }
      if (error.response?.statusCode == 501) {
        return 'Server plugins are unavailable on this server.';
      }
      if (error.response == null) {
        return 'Cannot reach the server. Check your connection and refresh. Changes are not retried automatically.';
      }
    }
    return 'The server could not complete this action. Refresh to check its current status.';
  }

  Future<void> _load() async {
    try {
      final plugins = await widget.api.list(widget.epoch);
      if (!_current) return;
      setState(() {
        _plugins = plugins;
        _error = null;
        _forbidden = false;
      });
    } catch (error) {
      if (!_current) return;
      setState(() => _error = _message(error));
    } finally {
      if (_current) setState(() => _busy = false);
    }
  }

  Future<void> _refresh() async {
    if (!_current || _busy) return;
    setState(() {
      _busy = true;
      _plugins = [];
      _result = null;
    });
    await _load();
  }

  Future<void> _enable(ServerPlugin plugin) async {
    if (!_current || _busy || _dialogContext != null) return;
    final enable = !plugin.enabled;
    final accepted = await showDialog<bool>(
        context: context,
        builder: (context) {
          _dialogContext = context;
          return AlertDialog(
              scrollable: true,
              title: Text('${enable ? 'Enable' : 'Disable'} ${plugin.name}?'),
              content: Text(enable
                  ? 'This enables an optional server extension. ${plugin.hasProbe ? 'It can run a self-check, with no library, file or network access.' : 'Review its reported error before enabling.'}'
                  : 'This stops the extension. Music playback continues.'),
              actions: [
                TextButton(
                    onPressed: () => Navigator.pop(context, false),
                    child: const Text('Cancel')),
                FilledButton(
                    onPressed: () => Navigator.pop(context, true),
                    child: Text(enable ? 'Enable' : 'Disable')),
              ]);
        });
    _dialogContext = null;
    if (!_current || accepted != true) return;
    await _act(() async {
      await widget.api.setEnabled(plugin.id, enable, widget.epoch);
      return '${plugin.name}: ${enable ? 'enabled' : 'disabled'}.';
    });
  }

  Future<void> _act(Future<String> Function() action) async {
    if (!_current || _busy || _forbidden) return;
    setState(() {
      _busy = true;
      _error = null;
      _result = null;
    });
    String? failure;
    String? result;
    try {
      result = await action();
    } catch (error) {
      if (!_current) return;
      failure = _message(error);
    }
    if (!_current) return;
    // A lost response can still mean a completed mutation. Never replay it.
    if (!_forbidden) await _load();
    if (!_current) return;
    setState(() {
      _busy = false;
      _error = failure ?? _error;
      _result = result;
    });
  }

  @override
  Widget build(BuildContext context) {
    return ListView(padding: const EdgeInsets.all(16), children: [
      const Text(
          'Advanced server settings. Music playback does not require plugins.'),
      const SizedBox(height: 12),
      Align(
          alignment: Alignment.centerLeft,
          child: OutlinedButton.icon(
              onPressed: _busy ? null : _refresh,
              icon: const Icon(Icons.refresh),
              label: const Text('Refresh'))),
      if (_busy) ...[
        const LinearProgressIndicator(),
        const Text('Loading server plugins…'),
      ],
      if (_error != null) Semantics(liveRegion: true, child: Text(_error!)),
      if (_result != null) Semantics(liveRegion: true, child: Text(_result!)),
      if (!_busy && _error == null && _plugins.isEmpty)
        const Text('No server plugins installed. Normal playback needs none.'),
      if (!_forbidden)
        for (final plugin in _plugins)
          Card(
              child: Padding(
                  padding: const EdgeInsets.all(16),
                  child: Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        Text(plugin.name,
                            style: Theme.of(context).textTheme.titleMedium),
                        if (plugin.version.isNotEmpty)
                          Text('Version ${plugin.version}'),
                        if (plugin.description.isNotEmpty)
                          Text(plugin.description),
                        Text('Status: ${switch (plugin.status) {
                          'active' => 'Active',
                          'disabled' => plugin.enabled
                              ? 'Enabled · ready for self-check'
                              : 'Disabled',
                          _ => 'Error',
                        }}'),
                        if (plugin.error.isNotEmpty) Text(plugin.error),
                        Text(plugin.hasProbe
                            ? 'Permissions: Self-check only. No library, file or network access.'
                            : 'Permissions: No supported capability reported.'),
                        const SizedBox(height: 8),
                        Wrap(spacing: 8, runSpacing: 8, children: [
                          OutlinedButton(
                              onPressed: _busy ? null : () => _enable(plugin),
                              child:
                                  Text(plugin.enabled ? 'Disable' : 'Enable')),
                          if (plugin.canCheck)
                            FilledButton(
                                onPressed: _busy
                                    ? null
                                    : () => _act(() async =>
                                        '${plugin.name}: Self-check passed (result ${await widget.api.check(plugin.id, widget.epoch)}).'),
                                child: const Text('Run self-check')),
                        ])
                      ]))),
    ]);
  }
}
