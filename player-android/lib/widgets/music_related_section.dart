import 'music_mix_actions.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../app_routes.dart';
import '../models/media.dart';
import '../providers/api_client_provider.dart';
import '../services/music_collections.dart';
import '../services/music_queue.dart';
import '../screens/media_playback_navigation.dart';
import 'music_collection_actions.dart';
import 'authenticated_network_image.dart';
import 'server_song_actions.dart';

/// Explained library suggestions follow the primary detail content.
class MusicRelatedSection extends ConsumerStatefulWidget {
  const MusicRelatedSection(
      {super.key, required this.kind, required this.entityKey});
  final String kind, entityKey;
  @override
  ConsumerState<MusicRelatedSection> createState() =>
      _MusicRelatedSectionState();
}

class _MusicRelatedSectionState extends ConsumerState<MusicRelatedSection> {
  List<Map<String, dynamic>>? _items;
  bool _failed = false;
  int _generation = 0;
  @override
  void initState() {
    super.initState();
    Future.microtask(_load);
  }

  @override
  void didUpdateWidget(covariant MusicRelatedSection oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.kind != widget.kind ||
        oldWidget.entityKey != widget.entityKey) {
      _load();
    }
  }

  Future<void> _load() async {
    final generation = ++_generation, api = ref.read(apiClientProvider);
    setState(() {
      _items = null;
      _failed = false;
    });
    try {
      final items = await api.relatedMusic(widget.kind, widget.entityKey);
      if (!mounted ||
          generation != _generation ||
          ref.read(apiClientProvider) != api) {
        return;
      }
      setState(() => _items = items);
    } catch (_) {
      if (!mounted ||
          generation != _generation ||
          ref.read(apiClientProvider) != api) {
        return;
      }
      setState(() => _failed = true);
    }
  }

  void _open(Map<String, dynamic> item) {
    final key = item['key'] as String;
    if (widget.kind == 'song') {
      openMediaPlaybackById(context, ref, int.parse(key));
      return;
    }
    context.push(
        '${widget.kind == 'album' ? AppRoutes.musicAlbums : AppRoutes.musicArtists}?${widget.kind == 'album' ? 'album' : 'artist'}=${Uri.encodeComponent(key)}');
  }

  @override
  Widget build(BuildContext context) {
    ref.listen(apiClientProvider, (previous, next) {
      if (previous != next) {
        _generation++;
        _items = null;
        _failed = false;
        Future.microtask(_load);
      }
    });
    return Padding(
        padding: const EdgeInsets.symmetric(vertical: 16),
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          MusicMixActions(
              key: ValueKey('${widget.kind}:${widget.entityKey}'),
              kind: widget.kind,
              entityKey: widget.entityKey),
          Semantics(
              header: true,
              child: Text('More like this',
                  style: Theme.of(context).textTheme.titleLarge)),
          const Text('From your library · shared music tags'),
          if (_failed) ...[
            const Text(
                'Could not load related music. Check your connection or library access.'),
            TextButton(onPressed: _load, child: const Text('Retry')),
          ] else if (_items == null)
            const Padding(
                padding: EdgeInsets.all(16),
                child: CircularProgressIndicator(
                    semanticsLabel: 'Finding related music'))
          else if (_items!.isEmpty)
            const Text(
                'No related music found. Add genre or artist tags to find more matches.')
          else
            for (final item in _items!)
              Card(
                  child: Padding(
                      padding: const EdgeInsets.all(12),
                      child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            if (item['cover_media_id'] != null)
                              AuthenticatedNetworkImage(
                                  imageUrl: ref
                                      .read(apiClientProvider)
                                      .thumbnailUrl(
                                          item['cover_media_id'] as int),
                                  width: 80,
                                  height: 80,
                                  fit: BoxFit.cover,
                                  placeholder: (_, url) =>
                                      const Icon(Icons.album_outlined),
                                  errorWidget: (_, url, error) =>
                                      const Icon(Icons.album_outlined)),
                            Text(item['title'] as String,
                                style: Theme.of(context).textTheme.titleMedium),
                            Text((item['basis'] as List).join(' · ')),
                            TextButton(
                                onPressed: () => _open(item),
                                child: Text('Open ${widget.kind}')),
                            if (widget.kind == 'song')
                              FilledButton.icon(
                                  onPressed: () => insertServerSongs(
                                      context,
                                      ref,
                                      [
                                        Media.fromJson(item['song']
                                            as Map<String, dynamic>)
                                      ],
                                      QueueInsertion.now,
                                      replace: true,
                                      sourceLabel: item['title'] as String),
                                  icon: const Icon(Icons.play_arrow),
                                  label: const Text('Play'))
                            else
                              MusicCollectionActions(
                                  compact: true,
                                  collections: [
                                    MusicCollection(
                                        widget.kind, item['key'] as String)
                                  ],
                                  playLabel: 'Play',
                                  sourceLabel: item['title'] as String),
                          ]))),
        ]));
  }
}
