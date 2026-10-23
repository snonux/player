import '../l10n/music_localizations.dart';
import 'dart:math';
import '../services/music_discovery.dart';
import '../widgets/music_discovery_panel.dart';
import '../widgets/music_enrichment_section.dart';
import 'create_share_dialog.dart';
import '../widgets/music_download_dialog.dart';
import '../widgets/music_related_section.dart';
import '../widgets/music_listening_details.dart';
import '../widgets/music_rating_button.dart';
import '../widgets/music_rating_filter.dart';
import '../widgets/music_favorite_button.dart';
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
  bool _discover = false;
  String? _collection;
  List<String> _collectionOrder = [];
  int _randomSeed = DateTime.now().microsecondsSinceEpoch;
  bool _favoritesOnly = false;
  bool _ratedOnly = false, _highestRated = false;
  Widget _rating(String kind, Map<String, dynamic> entity) => MusicRatingButton(
      key: ValueKey(
          'rating-$kind-${entity[kind == 'song' ? 'id' : '${kind}_key']}'),
      kind: kind,
      entity: entity,
      onChanged: () => setState(() {}));
  Widget _ratingFilter() => MusicRatingFilter(
      rated: _ratedOnly,
      highest: _highestRated,
      onChanged: (rated, highest) => setState(() {
            _ratedOnly = rated;
            _highestRated = highest;
          }));
  Widget _personal(String kind, Map<String, dynamic> entity) => Row(
      mainAxisSize: MainAxisSize.min,
      children: [_heart(kind, entity), _rating(kind, entity)]);

  Widget _heart(String kind, Map<String, dynamic> entity) => MusicFavoriteButton(
      key: ValueKey(
          '$kind-${entity[kind == 'playlist' ? 'playlist_id' : '${kind}_key']}'),
      kind: kind,
      entity: entity,
      onChanged: () => setState(() {}));
  Widget _favoritesFilter() => IconButton(
      tooltip: _favoritesOnly
          ? context.tr('Show all albums')
          : context.tr('Favorites only'),
      isSelected: _favoritesOnly,
      icon: const Icon(Icons.favorite_border),
      selectedIcon: const Icon(Icons.favorite),
      onPressed: () => setState(() => _favoritesOnly = !_favoritesOnly));

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
      _discover = false;
      _collection = null;
      _collectionOrder = [];
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
    final client = ref.read(apiClientProvider);
    try {
      if (key != null) {
        final album = await client.musicAlbum(key);
        if (!mounted ||
            generation != _generation ||
            ref.read(apiClientProvider) != client) {
          return;
        }
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
        if (!mounted ||
            generation != _generation ||
            ref.read(apiClientProvider) != client) {
          return;
        }
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

  Future<void> _openAlbum(String key) async {
    await context
        .push('${AppRoutes.musicAlbums}?album=${Uri.encodeComponent(key)}');
    if (mounted) await _load();
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
    ref.listen(apiClientProvider, (previous, next) {
      if (previous != next) {
        _generation++;
        _album = null;
        _loading = true;
        _error = null;
        _albums = [];
        _favoritesOnly = false;
        _ratedOnly = false;
        _highestRated = false;
        _discover = false;
        _collection = null;
        _collectionOrder = [];
        _randomSeed = DateTime.now().microsecondsSinceEpoch;
        Future.microtask(_load);
      }
    });
    return PopScope(
        canPop: !_selecting && !_discover && _collection == null,
        onPopInvokedWithResult: (didPop, _) {
          if (!didPop && !_selecting && (_discover || _collection != null)) {
            setState(() {
              if (_collection != null) {
                _collection = null;
                _discover = true;
              } else {
                _discover = false;
              }
            });
          }
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
              actions: [
                if (widget.albumKey == null) ...[
                  _favoritesFilter(),
                  _ratingFilter()
                ],
                if (widget.albumKey != null &&
                    _album != null &&
                    !_loading &&
                    _error == null)
                  _personal('album', _album!),
                const MusicSearchButton(),
                const MusicQueueButton()
              ],
              title: Text(widget.albumKey != null
                  ? (_album?['title'] as String? ?? context.tr('Album'))
                  : _discover
                      ? context.tr('Discover albums')
                      : _collection != null
                          ? albumCollections[_collection]![0]
                          : widget.artistKey != null
                              ? 'Albums by ${widget.artistName ?? 'artist'}'
                              : context.tr('Music · Albums'))),
          body: _loading
              ? const Center(child: CircularProgressIndicator())
              : _error != null
                  ? Center(
                      child: Padding(
                          padding: const EdgeInsets.all(24),
                          child:
                              Column(mainAxisSize: MainAxisSize.min, children: [
                            Text(
                                context.tr('Could not load albums. {error}',
                                    {'error': _error}),
                                textAlign: TextAlign.center),
                            const SizedBox(height: 16),
                            FilledButton(
                                onPressed: _load,
                                child: Text(context.tr('Retry')))
                          ])))
                  : widget.albumKey != null
                      ? _detail()
                      : _discover
                          ? _catalog(MusicLayout('albums'))
                          : MusicLayoutPanel(
                              kind: 'albums',
                              fixedOrder: _collection != null,
                              builder: _catalog),
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
    final source = ratingFiltered(
        layout.sorted((_albums ?? [])
            .where((a) => !_favoritesOnly || a['is_favorite'] == true)
            .toList()),
        _ratedOnly,
        _highestRated);
    final collections = {
      for (final key in albumCollections.keys)
        key: discoverAlbums(source, key, random: Random(_randomSeed))
    };
    final albums = _collection == null ? source : collections[_collection]!;
    if (['random', 'liked_mix'].contains(_collection) &&
        _collectionOrder.isNotEmpty) {
      final ranks = {
        for (var i = 0; i < _collectionOrder.length; i++) _collectionOrder[i]: i
      };
      albums.sort((a, b) => (ranks[a['album_key']] ?? ranks.length)
          .compareTo(ranks[b['album_key']] ?? ranks.length));
    }
    if (_discover && widget.artistKey == null) {
      return MusicDiscoveryPanel(
          key: ValueKey('discovery-$_generation'),
          collections: collections,
          filtered: _facets.isNotEmpty || _favoritesOnly || _ratedOnly,
          onRefresh: () {
            _randomSeed = DateTime.now().microsecondsSinceEpoch;
            return _load();
          },
          onOpenAlbum: _openAlbum,
          onBrowse: () => setState(() {
                _discover = false;
                _collection = null;
              }),
          onViewAll: (key, items) => setState(() {
                _discover = false;
                _collection = key;
                _collectionOrder =
                    items.map((a) => a['album_key'] as String).toList();
              }));
    }
    if (albums.isEmpty) {
      return Column(children: [
        _discoveryHeader(albums),
        Expanded(
            child: Center(
                child: Padding(
                    padding: const EdgeInsets.all(24),
                    child: Column(mainAxisSize: MainAxisSize.min, children: [
                      Text(_collection != null
                          ? albumCollections[_collection]![2]
                          : _favoritesOnly
                              ? context.tr(
                                  'No favorite albums. Show all albums to favorite one.')
                              : _ratedOnly
                                  ? context.tr(
                                      'No rated albums. Turn off Rated only to rate one.')
                                  : 'Nothing matches these filters. Clear filters or browse Library for more music.'),
                      TextButton(
                          onPressed: () {
                            if (_collection != null) {
                              setState(() {
                                _collection = null;
                                _discover = true;
                              });
                            } else {
                              context.pop();
                            }
                          },
                          child: Text(_collection != null
                              ? 'Back to discovery'
                              : 'Back to Library'))
                    ]))))
      ]);
    }
    return MusicCollectionCatalog(
        key: ValueKey(
            'albums-${widget.artistKey ?? "all"}-$_collection-$_generation'),
        items: albums,
        header: _discoveryHeader(albums),
        listHeader: _facets.isEmpty
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
                subtitle: Text([
                  if (layout.columns.isNotEmpty)
                    layout.subtitle(album, context),
                  musicListeningText(album)
                ].join('\n')),
                trailing: _personal('album', album),
                onTap: toggle ?? () => _openAlbum(album['album_key'] as String),
              )
            : Card(
                clipBehavior: Clip.antiAlias,
                child: InkWell(
                    onTap: toggle ??
                        () => _openAlbum(album['album_key'] as String),
                    child: Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [
                          if (checkbox != null)
                            Align(
                                alignment: Alignment.centerLeft,
                                child: checkbox),
                          Expanded(
                              child: Stack(fit: StackFit.expand, children: [
                            _cover(album),
                            Align(
                                alignment: Alignment.topRight,
                                child: _personal('album', album))
                          ])),
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
                                    Text(layout.subtitle(album, context),
                                        maxLines: 2,
                                        overflow: TextOverflow.ellipsis),
                                    if (album['from_folder'] == true)
                                      Text(context.tr('From folder')),
                                  ])),
                        ]))));
  }

  Widget _discoveryHeader(List<Map<String, dynamic>> albums) {
    if (widget.artistKey != null) return const SizedBox.shrink();
    return Padding(
        padding: const EdgeInsets.symmetric(horizontal: 16),
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Wrap(children: [
            TextButton.icon(
                onPressed: () => setState(() {
                      _discover = true;
                      _collection = null;
                    }),
                icon: const Icon(Icons.explore_outlined),
                label: Text(context.tr('Discover albums'))),
            if (_collection != null)
              TextButton(
                  onPressed: () => setState(() {
                        _collection = null;
                        _collectionOrder = [];
                      }),
                  child: Text(context.tr('View all albums'))),
          ]),
          if (_collection != null) ...[
            Text(albumCollections[_collection]![1]),
            MusicCollectionActions(
                key: ValueKey('collection-$_collection-$_generation'),
                playOnly: true,
                collections: [
                  for (final album in albums)
                    MusicCollection('album', album['album_key'] as String)
                ],
                playLabel: 'Play all ${albums.length} albums',
                sourceLabel: albumCollections[_collection]![0],
                enabled: albums.isNotEmpty),
          ],
        ]));
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
    PopupMenuButton<String> actions(List<Media> items, String label) =>
        PopupMenuButton<String>(
            tooltip: label,
            enabled: items.isNotEmpty,
            onSelected: (action) => action == 'share'
                ? showCreateShareDialog(context,
                    client: ref.read(apiClientProvider),
                    mediaIds: items.map((song) => song.id).toList(),
                    collectionTitle:
                        label == context.tr('Selected song actions')
                            ? context.tr('Selected songs')
                            : 'Music selection')
                : action == 'download'
                    ? showMusicDownloadDialog(context, ref, items)
                    : insertServerSongs(context, ref, items,
                        QueueInsertion.values.byName(action)),
            itemBuilder: (_) => [
                  for (final action in QueueInsertion.values)
                    PopupMenuItem(
                        value: action.name,
                        child: Text(context.tr(queueActionLabel(action)))),
                  PopupMenuItem(
                      value: 'download',
                      child: Text(context.tr('Keep offline'))),
                  PopupMenuItem(
                      value: 'share', child: Text(context.tr('Share…')))
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
          if (album['from_folder'] == true) Text(context.tr('From folder')),
          Text(
              '${context.musicL10n.count(album['song_count'] as int, '{count} song', '{count} songs')} · ${_duration((album['duration'] as num).toDouble())}'),
          MusicListeningDetails(album)
        ]))
      ]),
      const SizedBox(height: 16),
      MusicCollectionActions(
          key: ValueKey('album-${widget.albumKey}-$_generation'),
          collections: [MusicCollection('album', widget.albumKey!)],
          playLabel: context.tr('Play album'),
          sourceLabel: album['title'] as String,
          enabled: songs.isNotEmpty),
      MusicEnrichmentSection(kind: 'album', entityKey: widget.albumKey!),
      Wrap(crossAxisAlignment: WrapCrossAlignment.center, children: [
        TextButton(
            onPressed: () => setState(() {
                  _selecting = !_selecting;
                  if (!_selecting) _selected.clear();
                }),
            child: Text(_selecting
                ? context.tr('Cancel selection')
                : context.tr('Select songs'))),
        if (_selecting) ...[
          Text(context.tr('{count} selected', {'count': _selected.length})),
          actions(selectedSongs, context.tr('Selected song actions'))
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
                    semanticLabel: context
                        .tr('Select {name}', {'name': songs[i].displayTitle}),
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
            subtitle:
                Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              _creditLinks(
                  (album['song_credits'] as Map?)?['${songs[i].id}'],
                  'track_artist',
                  songs[i].music?.artist ?? context.tr('Unknown artist')),
              MusicListeningDetails(songs[i].toJson()),
            ]),
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
              _rating(
                  'song',
                  (album['songs'] as List)
                      .cast<Map<String, dynamic>>()
                      .firstWhere((song) => song['id'] == songs[i].id)),
              Text(_duration(songs[i].duration)),
              PopupMenuButton<String>(
                tooltip: context
                    .tr('Actions for {name}', {'name': songs[i].displayTitle}),
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
                        child: Text(context.tr(queueActionLabel(action)))),
                  const PopupMenuItem(
                      value: 'details', child: Text('Song details')),
                ],
              )
            ])),
      ],
      MusicRelatedSection(kind: 'album', entityKey: widget.albumKey!),
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
      child: Center(
          child: Icon(Icons.album_outlined,
              size: 48, semanticLabel: context.tr('No album artwork'))));
}
