import '../widgets/offline_music_browser.dart';
import '../l10n/music_localizations.dart';
import '../widgets/music_pin_button.dart';
import '../widgets/music_download_dialog.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../app_routes.dart';
import '../models/offline_media.dart';
import '../providers/offline_downloads_provider.dart';
import '../providers/personal_media_provider.dart';
import '../widgets/personal_media_actions.dart';
import '../widgets/keep_offline_button.dart';
import '../widgets/music_metadata_details.dart';

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
                'Play saved files without signing in or connecting. Removing a download keeps the server file.')),
        TextButton.icon(
            icon: const Icon(Icons.library_music),
            label: Text(context.tr('Browse offline music')),
            onPressed: () => Navigator.of(context).push(MaterialPageRoute<void>(
                builder: (_) => const OfflineMusicBrowser()))),
        const OfflinePinnedCollections(),
        if (downloads.batch case final batch?)
          ListTile(
            title: Text(
                'Music downloads · ${batch.completed} of ${batch.songs.length} saved'),
            subtitle: Text(
                '${batch.failed.length} failed${batch.cancelled ? ' · Canceled' : batch.running ? ' · Keep Player open' : ' · Finished'}'),
            trailing: batch.running
                ? TextButton(
                    onPressed: downloads.cancel,
                    child: const Text('Cancel all'))
                : TextButton(
                    onPressed: batch.authorized()
                        ? () =>
                            showMusicDownloadDialog(context, ref, batch.songs)
                        : null,
                    child: const Text('Retry selection')),
          ),
        if (job != null)
          ListTile(
            title: Text(job.title),
            subtitle:
                Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              if (job.downloadProfile.isNotEmpty)
                Text(
                    'Download quality: ${offlineDownloadQualityLabel(job.downloadProfile)}'),
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
                    _OfflineDownloadTile(
                      item: item,
                      removing: _removing.contains(item.key),
                      personalRevision: _personalRevision,
                      onOpen: () => _open(item),
                      onRemove: () => _remove(item),
                      onPersonalChanged: () {
                        if (mounted) setState(() => _personalRevision++);
                      },
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
              title: const Text('Remove download?'),
              content: Text(
                  'Remove “${item.title}” from this device? The server file stays available.'),
              actions: [
                TextButton(
                    onPressed: () => Navigator.pop(context, false),
                    child: const Text('Cancel')),
                FilledButton(
                    onPressed: () => Navigator.pop(context, true),
                    child: const Text('Remove download')),
              ],
            ));
    if (confirmed != true || !mounted) return;
    setState(() => _removing.add(item.key));
    try {
      await ref.read(offlineDownloadsProvider).remove(item.key);
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(const SnackBar(
            content: Text('Could not remove the download. Please try again.')));
      }
    } finally {
      if (mounted) setState(() => _removing.remove(item.key));
    }
  }
}

enum _OfflineDownloadAction { details, notes, favorite, remove }

/// Compact download row: title, size/type, and one overflow menu for secondary actions.
class _OfflineDownloadTile extends ConsumerStatefulWidget {
  const _OfflineDownloadTile({
    required this.item,
    required this.removing,
    required this.personalRevision,
    required this.onOpen,
    required this.onRemove,
    required this.onPersonalChanged,
  });

  final OfflineMedia item;
  final bool removing;
  final int personalRevision;
  final VoidCallback onOpen;
  final VoidCallback onRemove;
  final VoidCallback onPersonalChanged;

  @override
  ConsumerState<_OfflineDownloadTile> createState() =>
      _OfflineDownloadTileState();
}

class _OfflineDownloadTileState extends ConsumerState<_OfflineDownloadTile> {
  bool? _favorite;
  bool _favoriteBusy = false;

  @override
  void initState() {
    super.initState();
    if (widget.item.type == 'image') _loadFavorite();
  }

  @override
  void didUpdateWidget(covariant _OfflineDownloadTile oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (widget.item.type == 'image' &&
        (oldWidget.item.key != widget.item.key ||
            oldWidget.personalRevision != widget.personalRevision)) {
      _loadFavorite();
    }
  }

  Future<void> _loadFavorite() async {
    setState(() => _favoriteBusy = true);
    try {
      final favorite =
          await offlinePersonalStore(ref, widget.item.key).getFavorite();
      if (mounted) setState(() => _favorite = favorite);
    } catch (_) {
      if (mounted) setState(() => _favorite = null);
    } finally {
      if (mounted) setState(() => _favoriteBusy = false);
    }
  }

  Future<void> _toggleFavorite() async {
    if (_favoriteBusy) return;
    setState(() => _favoriteBusy = true);
    try {
      final favorite =
          await offlinePersonalStore(ref, widget.item.key).toggleFavorite();
      if (mounted) {
        setState(() => _favorite = favorite);
        widget.onPersonalChanged();
      }
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(const SnackBar(
            content: Text('Could not save favorite. Please try again.')));
      }
    } finally {
      if (mounted) setState(() => _favoriteBusy = false);
    }
  }

  void _showDetails() {
    final item = widget.item;
    showDialog<void>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Details'),
        content: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text('Origin: ${item.origin}'),
              Text('Account ${item.userId}'),
              Text('Size: ${formatOfflineBytes(item.sizeBytes)}'),
              Text('Type: ${_typeLabel(item.type)}'),
              Text(item.downloadQualityLabel),
              if (item.contentType.isNotEmpty) Text(item.contentType),
              if (item.type == 'audio') ...[
                const SizedBox(height: 12),
                const Text('Saved offline snapshot'),
                MusicMetadataDetails(
                    music: item.music, filename: item.sourceName),
              ],
            ],
          ),
        ),
        actions: [
          TextButton(
              onPressed: () => Navigator.pop(context),
              child: const Text('Close'))
        ],
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final item = widget.item;
    final buttonStyle =
        IconButton.styleFrom(minimumSize: const Size(48, 48));
    final favoriteLabel = _favorite == true
        ? 'Remove from favorites'
        : 'Add to favorites';

    return ListTile(
      key: ValueKey('offline_${item.key}'),
      leading: Icon(switch (item.type) {
        'audio' => Icons.headphones,
        'video' => Icons.movie_outlined,
        _ => Icons.image_outlined,
      }),
      title: Text(item.title, maxLines: 2, overflow: TextOverflow.ellipsis),
      subtitle: Text(
          '${formatOfflineBytes(item.sizeBytes)} · ${_typeLabel(item.type)}'),
      onTap: widget.removing ? null : widget.onOpen,
      trailing: PopupMenuButton<_OfflineDownloadAction>(
        key: Key('offline_menu_${item.key}'),
        tooltip: 'Actions for ${item.title}',
        style: buttonStyle,
        icon: const Icon(Icons.menu),
        enabled: !widget.removing,
        onSelected: (action) {
          switch (action) {
            case _OfflineDownloadAction.details:
              _showDetails();
            case _OfflineDownloadAction.notes:
              openPersonalNotes(context,
                  offlinePersonalStore(ref, item.key), item.title);
            case _OfflineDownloadAction.favorite:
              _toggleFavorite();
            case _OfflineDownloadAction.remove:
              widget.onRemove();
          }
        },
        itemBuilder: (_) => [
          PopupMenuItem(
            key: Key('offline_details_${item.key}'),
            value: _OfflineDownloadAction.details,
            child: const Text('Details'),
          ),
          PopupMenuItem(
            key: Key('offline_notes_${item.key}'),
            value: _OfflineDownloadAction.notes,
            child: const Text('Notes'),
          ),
          if (item.type == 'image')
            PopupMenuItem(
              key: Key('offline_favorite_${item.key}'),
              value: _OfflineDownloadAction.favorite,
              enabled: !_favoriteBusy,
              child: Text(favoriteLabel),
            ),
          PopupMenuItem(
            key: Key('offline_remove_${item.key}'),
            value: _OfflineDownloadAction.remove,
            child: const Text('Remove download'),
          ),
        ],
      ),
    );
  }
}

String _typeLabel(String type) => switch (type) {
      'audio' => 'Audio',
      'video' => 'Video',
      'image' => 'Image',
      _ => type,
    };
