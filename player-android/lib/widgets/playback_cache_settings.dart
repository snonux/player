import '../providers/auth_state_provider.dart';
import '../providers/api_client_provider.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/playback_cache_provider.dart';

class PlaybackCacheSettings extends ConsumerStatefulWidget {
  const PlaybackCacheSettings({super.key});
  @override
  ConsumerState<PlaybackCacheSettings> createState() =>
      _PlaybackCacheSettingsState();
}

class _PlaybackCacheSettingsState extends ConsumerState<PlaybackCacheSettings> {
  String? error;
  bool loading = true;
  @override
  void initState() {
    super.initState();
    Future.microtask(() async {
      try {
        await ref.read(playbackCacheProvider).initialize();
      } catch (_) {
        error = 'Could not open playback cache. Check device storage.';
      }
      if (mounted) setState(() => loading = false);
    });
  }

  Future<void> run(Future<void> Function() action) async {
    try {
      await action();
      if (mounted) setState(() => error = null);
    } catch (e) {
      if (mounted) {
        setState(() => error = e is StateError
            ? e.message.toString()
            : 'Could not update cache. Check device storage.');
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final cache = ref.watch(playbackCacheProvider);
    final user = ref.watch(authStateProvider).valueOrNull?.user?.id;
    final origin = ref.watch(playerBaseUrlProvider).origin;
    final items = cache.items
        .where((row) => row['user'] == user && row['origin'] == origin)
        .toList();
    final bytes = items.fold<int>(0, (n, row) => n + (row['size'] as int));
    return ExpansionTile(
        tilePadding: EdgeInsets.zero,
        title: const Text('Playback cache'),
        subtitle: Text(
            '${(bytes / 1048576).toStringAsFixed(1)} MB · ${items.length} songs'),
        children: [
          const Text(
              'Disposable song copies for replay while this server account is unlocked. Kept downloads are separate. Cached songs can be evicted automatically.'),
          if (loading) const LinearProgressIndicator(),
          if (error != null)
            Text(error!,
                style: TextStyle(color: Theme.of(context).colorScheme.error)),
          DropdownButtonFormField<int>(
              initialValue: cache.budget,
              decoration: const InputDecoration(labelText: 'Cache budget'),
              items: const [
                DropdownMenuItem(value: 0, child: Text('Off')),
                DropdownMenuItem(value: 128, child: Text('128 MB')),
                DropdownMenuItem(value: 512, child: Text('512 MB')),
                DropdownMenuItem(value: 1024, child: Text('1 GB'))
              ],
              onChanged: loading
                  ? null
                  : (v) {
                      if (v != null) run(() => cache.configure(megabytes: v));
                    }),
          SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('Prefetch next queued song'),
              value: cache.prefetch,
              onChanged:
                  loading ? null : (v) => run(() => cache.configure(next: v))),
          SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('Cache whole song before playing'),
              subtitle: const Text(
                  'Wait for a complete copy instead of starting the network stream immediately. Large songs and cache failures use streaming.'),
              value: cache.completeBuffer,
              onChanged: loading
                  ? null
                  : (v) => run(() => cache.configure(buffer: v))),
          SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('Use app external storage'),
              subtitle: const Text(
                  'Uses Android’s app directory on external storage when available. Stop playback first; changing location clears disposable copies. Uninstalling Player removes them.'),
              value: cache.external,
              onChanged: loading
                  ? null
                  : (v) => run(() => cache.configure(useExternal: v))),
          if (cache.status.isNotEmpty)
            Text(cache.status, semanticsLabel: cache.status),
          Wrap(spacing: 8, children: [
            TextButton(
                onPressed: cache.busy ? cache.cancel : null,
                child: const Text('Cancel transfer')),
            TextButton(
                onPressed: loading ? null : () => run(cache.clear),
                child: const Text('Clear cache'))
          ]),
          if (items.isEmpty) const ListTile(title: Text('No cached songs.')),
          for (final row in items)
            ListTile(
                title: Text(row['title'] as String,
                    maxLines: 2, overflow: TextOverflow.ellipsis),
                subtitle: Text(
                    '${((row['size'] as int) / 1048576).toStringAsFixed(1)} MB')),
        ]);
  }
}
