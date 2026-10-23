import '../widgets/music_favorite_button.dart';
import '../widgets/music_filter_sheet.dart';
import '../widgets/music_search_button.dart';
import '../widgets/music_mini_player.dart';
import '../services/music_collections.dart';
import '../widgets/music_collection_actions.dart';
import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import '../app_routes.dart';
import '../providers/api_client_provider.dart';
import '../utils/error_mappers.dart';
import '../widgets/music_queue_sheet.dart';

const artistRoles = {
  'artists': 'Artists (track and album)',
  'track_artist': 'Track artist',
  'album_artist': 'Album artist',
  'composer': 'Composer',
  'performer': 'Performer',
};
String creditRoleLabels(dynamic roles) =>
    (roles as List? ?? []).map((role) => artistRoles[role] ?? role).join(' · ');

/// A catalog of explicitly tagged credits over the permitted music library.
class MusicArtistsScreen extends ConsumerStatefulWidget {
  const MusicArtistsScreen({super.key, this.artistKey});
  final String? artistKey;
  @override
  ConsumerState<MusicArtistsScreen> createState() => _MusicArtistsScreenState();
}

class _MusicArtistsScreenState extends ConsumerState<MusicArtistsScreen> {
  bool _favoritesOnly = false;
  Widget _heart(String kind, Map<String, dynamic> entity) => MusicFavoriteButton(
      key: ValueKey(
          '$kind-${entity[kind == 'playlist' ? 'playlist_id' : '${kind}_key']}'),
      kind: kind,
      entity: entity,
      onChanged: () => setState(() {}));
  Widget _favoritesFilter() => IconButton(
      tooltip: _favoritesOnly ? 'Show all artists' : 'Favorites only',
      isSelected: _favoritesOnly,
      icon: const Icon(Icons.favorite_border),
      selectedIcon: const Icon(Icons.favorite),
      onPressed: () => setState(() => _favoritesOnly = !_favoritesOnly));

  String _role = 'artists';
  bool _loading = true;
  String? _error;
  List<Map<String, dynamic>> _people = [];
  Map<String, dynamic>? _artist;
  int _generation = 0;
  Map<String, String> _facets = {};
  void _filtersChanged(Map<String, String> value) {
    _facets = value;
    _role = value['role'] ?? 'artists';
    _load();
  }

  final _scroll = ScrollController();
  @override
  void initState() {
    super.initState();
    Future.microtask(_load);
  }

  @override
  void didUpdateWidget(covariant MusicArtistsScreen oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.artistKey != widget.artistKey) _load();
  }

  @override
  void dispose() {
    _generation++;
    _scroll.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    final generation = ++_generation;
    setState(() {
      _loading = true;
      _error = null;
    });
    final client = ref.read(apiClientProvider);
    try {
      final key = widget.artistKey;
      final data = key == null
          ? (_facets.isEmpty
              ? await client.musicArtists(role: _role)
              : ((await client.filteredMusic('artists', _facets,
                      query: {'role': _role})) as List)
                  .cast<Map<String, dynamic>>())
          : await client.musicArtist(key);
      if (!mounted ||
          generation != _generation ||
          ref.read(apiClientProvider) != client) {
        return;
      }
      setState(() {
        if (key == null) {
          _people = data as List<Map<String, dynamic>>;
        } else {
          _artist = data as Map<String, dynamic>;
        }
        _loading = false;
      });
    } catch (e) {
      if (!mounted ||
          generation != _generation ||
          ref.read(apiClientProvider) != client) {
        return;
      }
      setState(() {
        _error =
            e is DioException && [403, 404].contains(e.response?.statusCode)
                ? 'Artist is unavailable or you no longer have permission.'
                : setsErrorMessage(e);
        _loading = false;
      });
    }
  }

  Future<void> _openArtist(String key) async {
    await context
        .push('${AppRoutes.musicArtists}?artist=${Uri.encodeComponent(key)}');
    if (mounted) await _load();
  }

  @override
  Widget build(BuildContext context) {
    ref.listen(apiClientProvider, (previous, next) {
      if (previous != next) {
        _generation++;
        _artist = null;
        _loading = true;
        _error = null;
        _people = [];
        _favoritesOnly = false;
        Future.microtask(_load);
      }
    });
    return Scaffold(
      bottomNavigationBar: const MusicMiniPlayer(),
      appBar: AppBar(
          bottom: widget.artistKey == null
              ? MusicFilterBar(onChanged: _filtersChanged)
              : null,
          title: Text(widget.artistKey == null
              ? 'Music · Artists'
              : _artist?['name'] as String? ?? 'Artist'),
          actions: [
            if (widget.artistKey == null) _favoritesFilter(),
            if (widget.artistKey != null &&
                _artist != null &&
                !_loading &&
                _error == null)
              _heart('artist', _artist!),
            const MusicSearchButton(),
            const MusicQueueButton()
          ]),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : _error != null
              ? Center(
                  child: Padding(
                      padding: const EdgeInsets.all(24),
                      child: Column(mainAxisSize: MainAxisSize.min, children: [
                        Text('Could not load artists. $_error',
                            textAlign: TextAlign.center),
                        const SizedBox(height: 16),
                        FilledButton(
                            onPressed: _load, child: const Text('Retry')),
                      ])))
              : widget.artistKey != null
                  ? _detail()
                  : _catalog(),
    );
  }

  Widget _catalog() => MusicCollectionCatalog(
      key: ValueKey('artists-$_role-$_generation'),
      emptyMessage: _favoritesOnly
          ? 'No favorite artists. Show all artists to favorite one.'
          : 'No artists available for this role. Choose another role or ask an administrator to rescan music tags.',
      items: _people
          .where((a) => !_favoritesOnly || a['is_favorite'] == true)
          .toList(),
      kind: 'artist',
      keyName: 'artist_key',
      nameName: 'name',
      onRefresh: _load,
      scrollController: _scroll,
      header: Padding(
          padding: const EdgeInsets.all(16),
          child: _facets.containsKey('role')
              ? Text('Role: ${artistRoles[_facets['role']] ?? _facets['role']}')
              : DropdownButtonFormField<String>(
                  initialValue: _role,
                  decoration: const InputDecoration(labelText: 'Role'),
                  isExpanded: true,
                  items: [
                    for (final role in artistRoles.entries)
                      DropdownMenuItem(value: role.key, child: Text(role.value))
                  ],
                  onChanged: (value) {
                    if (value != null) {
                      _role = value;
                      if (_scroll.hasClients) _scroll.jumpTo(0);
                      _load();
                    }
                  })),
      itemBuilder: (person, checkbox, toggle) => ListTile(
          leading: checkbox,
          title: Text(person['name'] as String),
          subtitle: Text(
              '${creditRoleLabels(person['roles'])}\n${person['album_count']} albums · ${person['song_count']} ${_facets.isEmpty ? 'songs' : 'matching songs'}'),
          isThreeLine: true,
          trailing: _heart('artist', person),
          onTap: toggle ?? () => _openArtist(person['artist_key'] as String)));
  Widget _detail() {
    final artist = _artist!;
    final albums =
        (artist['albums'] as List? ?? []).cast<Map<String, dynamic>>();
    return MusicCollectionCatalog(
        key: ValueKey('artist-${widget.artistKey}-$_generation'),
        items: albums,
        kind: 'album',
        keyName: 'album_key',
        nameName: 'title',
        onRefresh: _load,
        scrollController: _scroll,
        header: Padding(
            padding: const EdgeInsets.all(16),
            child:
                Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text(artist['name'] as String,
                  style: Theme.of(context).textTheme.titleLarge),
              Text(creditRoleLabels(artist['roles'])),
              Text(
                  '${artist['album_count']} albums · ${artist['song_count']} songs'),
              MusicCollectionActions(
                  collections: [MusicCollection('artist', widget.artistKey!)],
                  playLabel: 'Play artist',
                  sourceLabel: artist['name'] as String,
                  enabled: albums.isNotEmpty),
            ])),
        itemBuilder: (album, checkbox, toggle) => Card(
            child: ListTile(
                leading: checkbox ?? const Icon(Icons.album_outlined),
                title: Text(album['title'] as String),
                subtitle: Text(
                    '${album['artist']}\n${creditRoleLabels(album['roles'])}'),
                isThreeLine: true,
                trailing: const Icon(Icons.chevron_right),
                onTap: toggle ??
                    () => context.push(
                        '${AppRoutes.musicAlbums}?album=${Uri.encodeComponent(album['album_key'] as String)}'))));
  }
}
