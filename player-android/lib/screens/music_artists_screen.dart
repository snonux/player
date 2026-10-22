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
  String _role = 'artists';
  bool _loading = true;
  String? _error;
  List<Map<String, dynamic>> _people = [];
  Map<String, dynamic>? _artist;
  int _generation = 0;
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
    try {
      final client = ref.read(apiClientProvider);
      final key = widget.artistKey;
      final data = key == null
          ? await client.musicArtists(role: _role)
          : await client.musicArtist(key);
      if (!mounted || generation != _generation) return;
      setState(() {
        if (key == null) {
          _people = data as List<Map<String, dynamic>>;
        } else {
          _artist = data as Map<String, dynamic>;
        }
        _loading = false;
      });
    } catch (e) {
      if (!mounted || generation != _generation) return;
      setState(() {
        _error =
            e is DioException && [403, 404].contains(e.response?.statusCode)
                ? 'Artist is unavailable or you no longer have permission.'
                : setsErrorMessage(e);
        _loading = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
        bottomNavigationBar: const MusicMiniPlayer(),
        appBar: AppBar(
            title: Text(widget.artistKey == null
                ? 'Music · Artists'
                : _artist?['name'] as String? ?? 'Artist'),
            actions: const [MusicSearchButton(), MusicQueueButton()]),
        body: _loading
            ? const Center(child: CircularProgressIndicator())
            : _error != null
                ? Center(
                    child: Padding(
                        padding: const EdgeInsets.all(24),
                        child:
                            Column(mainAxisSize: MainAxisSize.min, children: [
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
  Widget _catalog() => MusicCollectionCatalog(
      key: ValueKey('artists-$_role-$_generation'),
      emptyMessage:
          'No artists available for this role. Choose another role or ask an administrator to rescan music tags.',
      items: _people,
      kind: 'artist',
      keyName: 'artist_key',
      nameName: 'name',
      onRefresh: _load,
      scrollController: _scroll,
      header: Padding(
          padding: const EdgeInsets.all(16),
          child: DropdownButtonFormField<String>(
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
              '${creditRoleLabels(person['roles'])}\n${person['album_count']} albums · ${person['song_count']} songs'),
          isThreeLine: true,
          trailing: const Icon(Icons.chevron_right),
          onTap: toggle ??
              () => context.push(
                  '${AppRoutes.musicArtists}?artist=${Uri.encodeComponent(person['artist_key'] as String)}')));
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
