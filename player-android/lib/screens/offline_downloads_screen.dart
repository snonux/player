import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../app_routes.dart';
import '../models/offline_media.dart';
import '../providers/offline_downloads_provider.dart';
import '../providers/personal_media_provider.dart';
import '../widgets/personal_media_actions.dart';
import '../widgets/keep_offline_button.dart';

class OfflineDownloadsScreen extends ConsumerStatefulWidget {
  const OfflineDownloadsScreen({super.key});

  @override
  ConsumerState<OfflineDownloadsScreen> createState() =>
      _OfflineDownloadsScreenState();
}

class _OfflineDownloadsScreenState
    extends ConsumerState<OfflineDownloadsScreen> {
  final Set<String> _removing = {};
  int _personalRevision = 0;

  @override
  Widget build(BuildContext context) {
    final downloads = ref.watch(offlineDownloadsProvider);
    final items = ref.watch(offlineMediaListProvider);
    final job = downloads.active;
    return Scaffold(
      appBar: AppBar(title: const Text('Downloads'), actions: [
        IconButton(
            tooltip: 'On this device',
            icon: const Icon(Icons.phone_android),
            onPressed: () => context.go(AppRoutes.localLibrary)),
      ]),
      body: Column(children: [
        const Padding(
            padding: EdgeInsets.all(16),
            child: Text(
                'Play saved files without signing in or connecting. Deleting an offline copy keeps the server file.')),
        if (job != null)
          ListTile(
            title: Text(job.title),
            subtitle:
                Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text(
                  '${formatOfflineBytes(job.received)}${job.total == null ? '' : ' of ${formatOfflineBytes(job.total!)}'} downloaded'),
              LinearProgressIndicator(
                  value: job.total == null || job.total == 0
                      ? null
                      : job.received / job.total!),
            ]),
            trailing: IconButton(
                tooltip: 'Cancel download',
                onPressed: downloads.cancel,
                icon: const Icon(Icons.cancel_outlined)),
          ),
        if (downloads.error != null)
          Padding(
              padding: const EdgeInsets.all(16),
              child: Text(downloads.error!,
                  style:
                      TextStyle(color: Theme.of(context).colorScheme.error))),
        Expanded(
            child: items.when(
          loading: () => const Center(child: CircularProgressIndicator()),
          error: (_, __) => Center(
              child: TextButton(
                  onPressed: () => ref.invalidate(offlineMediaListProvider),
                  child: const Text('Could not load downloads. Retry'))),
          data: (items) => items.isEmpty
              ? const Center(
                  child: Text(
                      'No offline copies yet. Use Keep offline on a server file.'))
              : ListView(children: [
                  Padding(
                      padding: const EdgeInsets.all(16),
                      child: Text(
                          '${items.length} files · ${formatOfflineBytes(items.fold(0, (sum, item) => sum + item.sizeBytes))} on this device')),
                  for (final item in items)
                    ListTile(
                      key: ValueKey('offline_${item.key}'),
                      leading: Icon(switch (item.type) {
                        'audio' => Icons.headphones,
                        'video' => Icons.movie_outlined,
                        _ => Icons.image_outlined,
                      }),
                      title: Text(item.title,
                          maxLines: 2, overflow: TextOverflow.ellipsis),
                      subtitle: Text(
                          '${formatOfflineBytes(item.sizeBytes)} · ${item.origin}\nAccount ${item.userId}'),
                      isThreeLine: true,
                      onTap: _removing.contains(item.key)
                          ? null
                          : () => _open(item),
                      trailing: Row(mainAxisSize: MainAxisSize.min, children: [
                        IconButton(
                            tooltip: 'Notes',
                            icon: const Icon(Icons.notes_outlined),
                            onPressed: _removing.contains(item.key)
                                ? null
                                : () => openPersonalNotes(
                                    context,
                                    offlinePersonalStore(ref, item.key),
                                    item.title)),
                        if (item.type == 'image')
                          FavoriteButton(
                              store: offlinePersonalStore(ref, item.key),
                              revision: _personalRevision),
                        IconButton(
                            tooltip: 'Delete offline copy',
                            onPressed: _removing.contains(item.key)
                                ? null
                                : () => _remove(item),
                            icon: const Icon(Icons.delete_outline)),
                      ]),
                    ),
                ]),
        )),
      ]),
    );
  }

  Future<void> _open(OfflineMedia item) async {
    await context.push(AppRoutes.offlinePlayerPath(item.key));
    if (mounted) setState(() => _personalRevision++);
  }

  Future<void> _remove(OfflineMedia item) async {
    final confirmed = await showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
              title: const Text('Delete offline copy?'),
              content: Text(
                  'Delete “${item.title}” from this device? The server file stays available.'),
              actions: [
                TextButton(
                    onPressed: () => Navigator.pop(context, false),
                    child: const Text('Cancel')),
                FilledButton(
                    onPressed: () => Navigator.pop(context, true),
                    child: const Text('Delete offline copy')),
              ],
            ));
    if (confirmed != true || !mounted) return;
    setState(() => _removing.add(item.key));
    try {
      await ref.read(offlineDownloadsProvider).remove(item.key);
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(const SnackBar(
            content:
                Text('Could not delete the offline copy. Please try again.')));
      }
    } finally {
      if (mounted) setState(() => _removing.remove(item.key));
    }
  }
}
