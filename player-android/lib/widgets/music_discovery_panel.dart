import 'package:flutter/material.dart';
import '../services/music_discovery.dart';
import '../services/music_collections.dart';
import 'music_collection_actions.dart';

/// Compact discovery sections share the catalog's queue actions and identities.
class MusicDiscoveryPanel extends StatelessWidget {
  const MusicDiscoveryPanel(
      {super.key,
      required this.collections,
      required this.onViewAll,
      required this.onOpenAlbum,
      required this.onBrowse,
      required this.onRefresh,
      this.filtered = false});
  final Map<String, List<Map<String, dynamic>>> collections;
  final void Function(String, List<Map<String, dynamic>>) onViewAll;
  final ValueChanged<String> onOpenAlbum;
  final VoidCallback onBrowse;
  final Future<void> Function() onRefresh;
  final bool filtered;
  @override
  Widget build(BuildContext context) => RefreshIndicator(
      onRefresh: onRefresh,
      child: ListView(padding: const EdgeInsets.all(16), children: [
        Text('Discover albums', style: Theme.of(context).textTheme.titleLarge),
        if (filtered) const Text('From albums matching your current filters.'),
        Align(
            alignment: Alignment.centerLeft,
            child: TextButton(
                onPressed: onBrowse, child: const Text('View all albums'))),
        for (final entry in albumCollections.entries)
          _section(context, entry.key, entry.value),
      ]));
  Widget _section(BuildContext context, String key, List<String> copy) {
    final items = collections[key] ?? [];
    return Card(
        child: Padding(
            padding: const EdgeInsets.all(16),
            child:
                Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Semantics(
                  header: true,
                  child: Text(copy[0],
                      style: Theme.of(context).textTheme.titleMedium)),
              Text(copy[1]),
              if (items.isEmpty)
                Padding(
                    padding: const EdgeInsets.symmetric(vertical: 8),
                    child: Text(copy[2])),
              for (final album in items.take(4))
                ListTile(
                    contentPadding: EdgeInsets.zero,
                    title: Text('${album['title']}',
                        maxLines: 2, overflow: TextOverflow.ellipsis),
                    subtitle: Text('${album['artist']}',
                        maxLines: 1, overflow: TextOverflow.ellipsis),
                    trailing: const Icon(Icons.chevron_right),
                    onTap: () => onOpenAlbum(album['album_key'] as String)),
              MusicCollectionActions(
                  key: ValueKey(key),
                  playOnly: true,
                  collections: [
                    for (final a in items)
                      MusicCollection('album', a['album_key'] as String)
                  ],
                  playLabel: 'Play all ${items.length} albums',
                  sourceLabel: copy[0],
                  enabled: items.isNotEmpty),
              TextButton(
                  onPressed: () => onViewAll(key, items),
                  child: Text('View all ${copy[0].toLowerCase()}')),
            ])));
  }
}
