import '../widgets/music_layout_panel.dart';
import '../widgets/music_filter_sheet.dart';
import '../widgets/music_search_button.dart';
import '../widgets/music_mini_player.dart';
import '../services/music_collections.dart';
import '../widgets/music_collection_actions.dart';
import '../services/music_queue.dart';
import '../widgets/server_song_actions.dart';
import '../widgets/music_queue_sheet.dart';
import 'dart:typed_data';
import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../app_routes.dart';
import '../models/media.dart';
import '../providers/api_client_provider.dart';
import '../utils/error_mappers.dart';

/// The album lens shares file identities and playback with the ordinary library.
class MusicAlbumsScreen extends ConsumerStatefulWidget {
  const MusicAlbumsScreen(
      {super.key, this.albumKey, this.artistKey, this.artistName});
  final String? albumKey, artistKey, artistName;
  @override
  ConsumerState<MusicAlbumsScreen> createState() => _MusicAlbumsScreenState();
}

class _MusicAlbumsScreenState extends ConsumerState<MusicAlbumsScreen> {
  List<Map<String, dynamic>>? _albums;
  Map<String, dynamic>? _album;
  String? _error;
  bool _loading = true, _selecting = false;
  final _selected = <int>{};
  int _generation = 0;
  Map<String, String> _facets = {};
  void _filtersChanged(Map<String, String> value) {
    _facets = value;
    _load();
  }

  final _artwork = <int, Future<Uint8List>>{};
  @override
  void initState() {
    super.initState();
    Future.microtask(_load);
  }

  @override
  void didUpdateWidget(covariant MusicAlbumsScreen oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.albumKey != widget.albumKey ||
        oldWidget.artistKey != widget.artistKey) {
      _load();
    }
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    _artwork.clear();
    _selected.clear();
    _selecting = false;
    final generation = ++_generation;
    final key = widget.albumKey;
    try {
      final client = ref.read(apiClientProvider);
      if (key != null) {
        final album = await client.musicAlbum(key);
        if (!mounted || generation != _generation) return;
        setState(() {
          _album = album;
          _loading = false;
        });
      } else {
        final albums = _facets.isEmpty
            ? await client.musicAlbums(artist: widget.artistKey)
            : ((await client.filteredMusic('albums', _facets, query: {
                if (widget.artistKey != null) 'artist': widget.artistKey!
              })) as List)
                .cast<Map<String, dynamic>>();
        if (!mounted || generation != _generation) return;
        setState(() {
          _albums = albums;
          _loading = false;
        });
      }
    } catch (e) {
      if (mounted && generation == _generation) {
        setState(() {
          _error =
              e is DioException && [403, 404].contains(e.response?.statusCode)
                  ? 'Album is unavailable or you no longer have permission.'
                  : setsErrorMessage(e);
          _loading = false;
        });
      }
    }
  }

  void _play(List<Media> songs, int index) {
    final song = songs[index], client = ref.read(apiClientProvider);
    context.push(AppRoutes.audioPlayerPath('${song.id}'), extra: {
      'mediaUrl': client.playbackUrl(song),
      'title': song.displayTitle,
      'sequence': songs,
      'albumCollection': true,
      if (song.duration <= 1200) 'position': 0.0,
    });
  }

  @override
  Widget build(BuildContext context) {
    return PopScope(
        canPop: !_selecting,
        onPopInvokedWithResult: (didPop, _) {
          if (!didPop && _selecting) {
            setState(() {
              _selecting = false;
              _selected.clear();
            });
          }
        },
        child: Scaffold(
          bottomNavigationBar: const MusicMiniPlayer(),
          appBar: AppBar(
              bottom: widget.albumKey == null
                  ? MusicFilterBar(onChanged: _filtersChanged)
                  : null,
              actions: const [MusicSearchButton(), MusicQueueButton()],
              title: Text(widget.albumKey != null
                  ? (_album?['title'] as String? ?? 'Album')
                  : widget.artistKey != null
                      ? 'Albums by ${widget.artistName ?? 'artist'}'
                      : 'Music · Albums')),
          body: _loading
              ? const Center(child: CircularProgressIndicator())
              : _error != null
                  ? Center(
                      child: Padding(
                          padding: const EdgeInsets.all(24),
                          child:
                              Column(mainAxisSize: MainAxisSize.min, children: [
                            Text('Could not load albums. $_error',
                                textAlign: TextAlign.center),
                            const SizedBox(height: 16),
                            FilledButton(
                                onPressed: _load, child: const Text('Retry'))
                          ])))
                  : widget.albumKey != null
                      ? _detail()
                      : MusicLayoutPanel(kind: 'albums', builder: _catalog),
        ));
  }

  Widget _cover(Map<String, dynamic> album, {double? size}) {
    final id = album['cover_media_id'] as int?;
    // Route through the authenticated API transport rather than an unscoped cache.
    return SizedBox(
        width: size,
        height: size,
        child: id == null
            ? const _AlbumPlaceholder()
            : FutureBuilder<Uint8List>(
                future: _artwork.putIfAbsent(
                    id, () => ref.read(apiClientProvider).getThumbnail(id)),
                builder: (context, snapshot) {
                  if (!snapshot.hasData) return const _AlbumPlaceholder();
                  final data = snapshot.data;
                  return data == null
                      ? const _AlbumPlaceholder()
                      : Image.memory(data,
                          fit: BoxFit.cover,
                          errorBuilder: (_, __, ___) =>
                              const _AlbumPlaceholder(),
                          semanticLabel: 'Artwork for ${album['title']}');
                }));
  }

  Widget _catalog(MusicLayout layout) {
    final albums = layout.sorted(_albums ?? []);
    if (albums.isEmpty) {
      return Center(
          child: Padding(
              padding: const EdgeInsets.all(24),
              child: Column(mainAxisSize: MainAxisSize.min, children: [
                const Text(
                    'Nothing matches these filters. Clear filters or browse Library for more music.'),
                TextButton(
                    onPressed: () => context.pop(),
                    child: const Text('Back to Library'))
              ])));
    }
    return MusicCollectionCatalog(
        key: ValueKey('albums-${widget.artistKey ?? "all"}-$_generation'),
        items: albums,
        header: _facets.isEmpty
            ? null
            : Padding(
                padding: const EdgeInsets.all(16),
                child: Text(
                    '${albums.length} matching albums · ${albums.fold<int>(0, (sum, album) => sum + (album['song_count'] as int? ?? 0))} matching songs')),
        kind: 'album',
        keyName: 'album_key',
        nameName: 'title',
        grid: !layout.table,
        onRefresh: _load,
        itemBuilder: (album, checkbox, toggle) => layout.table
            ? ListTile(
                leading: checkbox,
                title: Text(album['title'] as String,
                    maxLines: 2, overflow: TextOverflow.ellipsis),
                subtitle: layout.columns.isEmpty
                    ? null
                    : Text(layout.subtitle(album)),
                trailing: const Icon(Icons.chevron_right),
                onTap: toggle ??
                    () => context.push(
                        '${AppRoutes.musicAlbums}?album=${Uri.encodeComponent(album['album_key'] as String)}'),
              )
            : Card(
                clipBehavior: Clip.antiAlias,
                child: InkWell(
                    onTap: toggle ??
                        () => context.push(
                            '${AppRoutes.musicAlbums}?album=${Uri.encodeComponent(album['album_key'] as String)}'),
                    child: Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [
                          if (checkbox != null)
                            Align(
                                alignment: Alignment.centerLeft,
                                child: checkbox),
                          Expanded(child: _cover(album)),
                          Padding(
                              padding: const EdgeInsets.all(8),
                              child: Column(
                                  crossAxisAlignment: CrossAxisAlignment.start,
                                  children: [
                                    Text(album['title'] as String,
                                        maxLines: 1,
                                        overflow: TextOverflow.ellipsis,
                                        style: Theme.of(context)
                                            .textTheme
                                            .titleSmall),
                                    Text(layout.subtitle(album),
                                        maxLines: 2,
                                        overflow: TextOverflow.ellipsis),
                                    if (album['from_folder'] == true)
                                      const Text('From folder'),
                                  ])),
                        ]))));
  }

  Widget _detail() {
    final album = _album!;
    final songs = (album['songs'] as List? ?? [])
        .cast<Map<String, dynamic>>()
        .map(Media.fromJson)
        .toList();
    final discs = (album['discs'] as List? ?? []).cast<Map<String, dynamic>>();
    final grouped = discs.length > 1 ||
        discs.any((d) =>
            (d['number'] as int? ?? 0) > 1 ||
            (d['subtitle'] as String? ?? '').isNotEmpty) ||
        songs.any((s) => (s.music?.toJson()['disc_total'] as int? ?? 0) > 1);
    final multi = discs.length > 1 ||
        discs.any((d) => (d['number'] as int? ?? 0) > 1) ||
        songs.any((s) => (s.music?.toJson()['disc_total'] as int? ?? 0) > 1);
    final headings = {
      for (final d in discs)
        if ((d['song_ids'] as List? ?? []).isNotEmpty)
          (d['song_ids'] as List).first: d
    };
    final selectedSongs =
        songs.where((song) => _selected.contains(song.id)).toList();
    PopupMenuButton<QueueInsertion> actions(List<Media> items, String label) =>
        PopupMenuButton<QueueInsertion>(
            tooltip: label,
            enabled: items.isNotEmpty,
            onSelected: (action) =>
                insertServerSongs(context, ref, items, action),
            itemBuilder: (_) => [
                  for (final action in QueueInsertion.values)
                    PopupMenuItem(
                        value: action, child: Text(queueActionLabel(action)))
                ]);
    return ListView(padding: const EdgeInsets.all(16), children: [
      Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
        _cover(album, size: 112),
        const SizedBox(width: 16),
        Expanded(
            child:
                Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Text(album['title'] as String,
              style: Theme.of(context).textTheme.titleLarge),
          _creditLinks(
              album['credits'], 'album_artist', album['artist'] as String),
          if (album['year'] != null) Text('${album['year']}'),
          if (album['from_folder'] == true) const Text('From folder'),
          Text(
              '${album['song_count']} songs · ${_duration((album['duration'] as num).toDouble())}')
        ]))
      ]),
      const SizedBox(height: 16),
      MusicCollectionActions(
          key: ValueKey('album-${widget.albumKey}-$_generation'),
          collections: [MusicCollection('album', widget.albumKey!)],
          playLabel: 'Play album',
          sourceLabel: album['title'] as String,
          enabled: songs.isNotEmpty),
      Wrap(crossAxisAlignment: WrapCrossAlignment.center, children: [
        TextButton(
            onPressed: () => setState(() {
                  _selecting = !_selecting;
                  if (!_selecting) _selected.clear();
                }),
            child: Text(_selecting ? 'Cancel selection' : 'Select songs')),
        if (_selecting) ...[
          Text('${_selected.length} selected'),
          actions(selectedSongs, 'Selected song actions')
        ],
      ]),
      const SizedBox(height: 16),
      for (var i = 0; i < songs.length; i++) ...[
        if (grouped && headings.containsKey(songs[i].id))
          _discHeading(headings[songs[i].id]!),
        ListTile(
            contentPadding: EdgeInsets.zero,
            leading: _selecting
                ? Checkbox(
                    value: _selected.contains(songs[i].id),
                    semanticLabel: 'Select ${songs[i].displayTitle}',
                    onChanged: (checked) => setState(() {
                          if (checked == true) {
                            _selected.add(songs[i].id);
                          } else {
                            _selected.remove(songs[i].id);
                          }
                        }))
                : SizedBox(width: 42, child: Text(_number(songs[i], multi))),
            title: Text(songs[i].displayTitle,
                maxLines: 2, overflow: TextOverflow.ellipsis),
            subtitle: _creditLinks(
                (album['song_credits'] as Map?)?['${songs[i].id}'],
                'track_artist',
                songs[i].music?.artist ?? 'Unknown artist'),
            onTap: () {
              if (_selecting) {
                setState(() {
                  if (!_selected.add(songs[i].id)) {
                    _selected.remove(songs[i].id);
                  }
                });
              } else {
                _play(songs, i);
              }
            },
            trailing: Row(mainAxisSize: MainAxisSize.min, children: [
              Text(_duration(songs[i].duration)),
              PopupMenuButton<String>(
                tooltip: 'Actions for ${songs[i].displayTitle}',
                onSelected: (value) {
                  if (value == 'details') {
                    context.push(AppRoutes.mediaDetailPath(songs[i].id));
                  } else {
                    insertServerSongs(context, ref, [songs[i]],
                        QueueInsertion.values.byName(value));
                  }
                },
                itemBuilder: (_) => [
                  for (final action in QueueInsertion.values)
                    PopupMenuItem(
                        value: action.name,
                        child: Text(queueActionLabel(action))),
                  const PopupMenuItem(
                      value: 'details', child: Text('Song details')),
                ],
              )
            ])),
      ],
    ]);
  }

  Widget _discHeading(Map<String, dynamic> disc) {
    final number = disc['number'] as int? ?? 0;
    final subtitle = disc['subtitle'] as String? ?? '';
    final title =
        '${number > 0 ? 'Disc $number' : 'Unnumbered disc'}${subtitle.isNotEmpty ? ' — $subtitle' : ''}';
    return Padding(
        padding: const EdgeInsets.symmetric(vertical: 16),
        child: Row(children: [
          _cover({'title': title, 'cover_media_id': disc['cover_media_id']},
              size: 48),
          const SizedBox(width: 12),
          Expanded(
              child: Semantics(
                  header: true,
                  child: Text(title,
                      style: Theme.of(context).textTheme.titleMedium))),
        ]));
  }

  Widget _creditLinks(dynamic credits, String role, String fallback) {
    final people = (credits as List? ?? [])
        .cast<Map<String, dynamic>>()
        .where((person) => (person['roles'] as List).contains(role))
        .toList();
    if (people.isEmpty) return Text(fallback);
    return Wrap(children: [
      for (final person in people)
        TextButton(
          onPressed: () => context.push(
              '${AppRoutes.musicArtists}?artist=${Uri.encodeComponent(person['artist_key'] as String)}'),
          child: Text(person['name'] as String),
        )
    ]);
  }

  String _number(Media song, bool multi) {
    final data = song.music?.toJson() ?? {};
    return '${multi ? '${data['disc'] ?? '—'}-' : ''}${data['track'] ?? '—'}';
  }

  String _duration(double seconds) {
    final total = seconds.round();
    return '${total ~/ 60}:${(total % 60).toString().padLeft(2, '0')}';
  }
}

class _AlbumPlaceholder extends StatelessWidget {
  const _AlbumPlaceholder();
  @override
  Widget build(BuildContext context) => ColoredBox(
      color: Theme.of(context).colorScheme.surfaceContainerHighest,
      child: const Center(
          child: Icon(Icons.album_outlined,
              size: 48, semanticLabel: 'No album artwork')));
}
