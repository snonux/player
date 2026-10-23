import '../api/playlist_rules_api.dart';
import 'music_playlist_rules_screen.dart';
import '../l10n/music_localizations.dart';
import '../api/music_artwork_api.dart';
import '../widgets/music_artwork_section.dart';
import '../widgets/music_favorite_button.dart';
import '../providers/current_user_provider.dart';
import '../widgets/playlist_import_dialog.dart';
import '../services/playlist_export.dart';
import '../widgets/music_search_button.dart';
import '../widgets/music_mini_player.dart';
import '../services/music_collections.dart';
import '../widgets/music_collection_actions.dart';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../app_routes.dart';
import '../api/player_api_client.dart';
import '../models/media.dart';
import '../models/user.dart';
import '../providers/api_client_provider.dart';
import '../utils/error_mappers.dart';
import '../widgets/music_queue_sheet.dart';

/// Account playlists use the same server media identities and music queue.
class MusicPlaylistsScreen extends ConsumerStatefulWidget {
  const MusicPlaylistsScreen({super.key, this.playlistId});
  final String? playlistId;
  @override
  ConsumerState<MusicPlaylistsScreen> createState() => _MusicPlaylistsState();
}

class _MusicPlaylistsState extends ConsumerState<MusicPlaylistsScreen> {
  bool _favoritesOnly = false;
  Widget _heart(String kind, Map<String, dynamic> entity) => MusicFavoriteButton(
      key: ValueKey(
          '$kind-${entity[kind == 'playlist' ? 'playlist_id' : '${kind}_key']}'),
      kind: kind,
      entity: entity,
      onChanged: () => setState(() {}));
  Widget _favoritesFilter() => IconButton(
      tooltip: _favoritesOnly
          ? context.tr('Show all playlists')
          : context.tr('Favorites only'),
      isSelected: _favoritesOnly,
      icon: const Icon(Icons.favorite_border),
      selectedIcon: const Icon(Icons.favorite),
      onPressed: () => setState(() => _favoritesOnly = !_favoritesOnly));

  List<Map<String, dynamic>> _list = [];
  Map<String, dynamic>? _playlist;
  bool _loading = true;
  String? _error;
  int _generation = 0;
  int get _accountEpoch => ref.read(credentialMutationQueueProvider).generation;
  final _find = TextEditingController();
  bool _editingSongs = false;
  bool _syncingSource = false;
  String? _songError;

  @override
  void dispose() {
    _find.dispose();
    super.dispose();
  }

  final _artwork = <int, Future<Uint8List>>{};
  final _customArtwork = <String, Future<Uint8List>>{};

  @override
  void initState() {
    super.initState();
    Future.microtask(_load);
  }

  @override
  void didUpdateWidget(covariant MusicPlaylistsScreen oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.playlistId != widget.playlistId) {
      _find.clear();
      _load();
    }
  }

  Future<void> _load() async {
    final generation = ++_generation;
    final accountEpoch = _accountEpoch;
    setState(() {
      _loading = true;
      _error = null;
    });
    _artwork.clear();
    _customArtwork.clear();
    try {
      final api = ref.read(apiClientProvider);
      final id = widget.playlistId;
      final data = id == null ? await api.playlists() : await api.playlist(id);
      if (!mounted ||
          generation != _generation ||
          accountEpoch != _accountEpoch) {
        return;
      }
      setState(() {
        if (id == null) {
          _list = data as List<Map<String, dynamic>>;
        } else {
          _playlist = data as Map<String, dynamic>;
        }
        _loading = false;
      });
    } catch (e) {
      if (!mounted ||
          generation != _generation ||
          accountEpoch != _accountEpoch) {
        return;
      }
      setState(() {
        _loading = false;
        _error = setsErrorMessage(e);
      });
    }
  }

  String _accessLabel(Map<String, dynamic> p) =>
      '${context.tr(p['visibility'] == 'shared' ? 'Shared with server' : 'Private')} · ${context.tr('Owner')}: ${p['owner_name'] ?? ''}${p['can_edit'] == false ? ' · ${context.tr('Read only')}' : ''}${p['source'] != null ? ' · ${context.tr('Following source')}' : ''}';
  String _totals(Map<String, dynamic> p) =>
      '${context.musicL10n.count(p['song_count'] as int, '{count} song', '{count} songs')} · ${_duration(p['duration'] as num)}';
  String _duration(num value) {
    final seconds = value.round();
    return '${seconds ~/ 60}:${(seconds % 60).toString().padLeft(2, '0')}';
  }

  Widget _cover(Map<String, dynamic> p, double size) {
    final revision = p['artwork_revision'] as String?;
    if (revision != null && revision.isNotEmpty) {
      final fallback = {...p}..remove('artwork_revision');
      return SizedBox(
          width: size,
          height: size,
          child: FutureBuilder<Uint8List>(
              future: _customArtwork.putIfAbsent(
                  '${p['playlist_id']}-$revision',
                  () => ref.read(apiClientProvider).musicArtworkImage(
                      'playlist', p['playlist_id'] as String, revision)),
              builder: (_, snapshot) => snapshot.hasData
                  ? Image.memory(snapshot.data!,
                      fit: BoxFit.cover,
                      semanticLabel: 'Artwork for ${p['name']}',
                      errorBuilder: (_, __, ___) => _cover(fallback, size))
                  : _cover(fallback, size)));
    }
    final id = p['cover_media_id'] as int?;
    final placeholder = ColoredBox(
        color: Theme.of(context).colorScheme.surfaceContainerHighest,
        child: Center(
            child: Icon(Icons.queue_music,
                semanticLabel: context.tr('No playlist artwork'))));
    return SizedBox(
        width: size,
        height: size,
        child: id == null
            ? placeholder
            : FutureBuilder<Uint8List>(
                future: _artwork.putIfAbsent(
                    id, () => ref.read(apiClientProvider).getThumbnail(id)),
                builder: (_, snapshot) => snapshot.hasData
                    ? Image.memory(snapshot.data!,
                        fit: BoxFit.cover,
                        semanticLabel: 'Artwork for ${p['name']}',
                        errorBuilder: (_, __, ___) => placeholder)
                    : placeholder));
  }

  @override
  Widget build(BuildContext context) {
    ref.listen(apiClientProvider, (previous, next) {
      if (previous != next) {
        _generation++;
        _playlist = null;
        _list = [];
        Future.microtask(_load);
      }
    });
    return Scaffold(
      bottomNavigationBar: const MusicMiniPlayer(),
      appBar: AppBar(
          title: Text(widget.playlistId == null
              ? context.tr('Music · Playlists')
              : _playlist?['name'] as String? ?? context.tr('Playlist')),
          actions: [
            if (widget.playlistId == null) _favoritesFilter(),
            if (widget.playlistId != null &&
                _playlist != null &&
                !_loading &&
                _error == null)
              _heart('playlist', _playlist!),
            const MusicSearchButton(),
            const MusicQueueButton(),
            if (widget.playlistId != null &&
                !_loading &&
                _error == null &&
                _playlist != null)
              PopupMenuButton<String>(
                  tooltip: context.tr('Playlist actions'),
                  onSelected: (action) =>
                      action == 'membership' ? _memberships() : _export(),
                  itemBuilder: (_) => [
                        PopupMenuItem(
                            value: 'export',
                            child: Text(context.tr('Export M3U8…'))),
                        PopupMenuItem(
                            value: 'membership',
                            child: Text(context.tr('Containing playlists…')))
                      ]),
            if (widget.playlistId == null)
              IconButton(
                  tooltip: context.tr('Create playlist'),
                  icon: const Icon(Icons.add),
                  onPressed: () => _edit()),
          ]),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : _error != null
              ? Center(
                  child: Padding(
                      padding: const EdgeInsets.all(24),
                      child: Column(mainAxisSize: MainAxisSize.min, children: [
                        Text(context.tr('Could not load playlists. {error}',
                            {'error': _error})),
                        FilledButton(
                            onPressed: _load, child: Text(context.tr('Retry')))
                      ])))
              : widget.playlistId == null
                  ? _catalog()
                  : _detail(),
    );
  }

  Widget _catalog() => MusicCollectionCatalog(
      key: ValueKey('playlists-$_generation'),
      emptyMessage: _favoritesOnly
          ? context
              .tr('No favorite playlists. Show all playlists to favorite one.')
          : context.tr(
              'No playlists yet. Create a playlist to save music for later.'),
      items: _list
          .where((a) => !_favoritesOnly || a['is_favorite'] == true)
          .toList(),
      kind: 'playlist',
      keyName: 'playlist_id',
      nameName: 'name',
      onRefresh: _load,
      header: Padding(
          padding: const EdgeInsets.all(16),
          child:
              Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            FilledButton.icon(
                onPressed: () => _edit(),
                icon: const Icon(Icons.add),
                label: Text(context.tr('Create playlist'))),
            OutlinedButton.icon(
                onPressed: _import,
                icon: const Icon(Icons.playlist_add),
                label: Text(context.tr('Import M3U…'))),
            OutlinedButton.icon(
                onPressed: () => _edit(null, true),
                icon: const Icon(Icons.rule),
                label: Text(context.tr('Create smart playlist'))),
          ])),
      itemBuilder: (p, checkbox, toggle) => ListTile(
          leading: checkbox ?? _cover(p, 56),
          title: Text(p['name'] as String,
              maxLines: 2, overflow: TextOverflow.ellipsis),
          subtitle: Text('${_totals(p)}\n${_accessLabel(p)}'),
          trailing: _heart('playlist', p),
          onTap: toggle ??
              () async {
                await context.push(
                    '${AppRoutes.musicPlaylists}?playlist=${Uri.encodeComponent(p['playlist_id'] as String)}');
                if (mounted) _load();
              }));
  Widget _detail() {
    final p = _playlist!;
    final canSyncSource = p['source'] != null &&
        p['owner_id'] == ref.watch(currentUserProvider).valueOrNull?.id;
    final songs = (p['songs'] as List)
        .cast<Map<String, dynamic>>()
        .map(Media.fromJson)
        .toList();
    void play(int index) {
      final song = songs[index], api = ref.read(apiClientProvider);
      context.push(AppRoutes.audioPlayerPath('${song.id}'), extra: {
        'mediaUrl': api.playbackUrl(song),
        'title': song.displayTitle,
        'sequence': songs,
        'sequenceIndex': index,
        'albumCollection': true,
        if (song.duration <= 1200) 'position': 0.0,
      });
    }

    return ListView(padding: const EdgeInsets.all(16), children: [
      Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
        SizedBox(
            width: 160,
            child: MusicArtworkSection(
                kind: 'playlist',
                entityKey: widget.playlistId!,
                fallback: _cover({...p}..remove('artwork_revision'), 112))),
        const SizedBox(width: 16),
        Expanded(
            child:
                Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Text(p['name'] as String,
              style: Theme.of(context).textTheme.titleLarge),
          Text(_totals(p)),
          Text(_accessLabel(p))
        ]))
      ]),
      const SizedBox(height: 16),
      Text(p['comment'] as String),
      if (p['rules'] is Map) ...[
        Text(playlistRulesSummary(context, p['rules'] as Map<String, dynamic>)),
        Text(context.tr(
            'Your plays, favorites and ratings use your account. Shared smart playlists can show different songs to each viewer.')),
        OutlinedButton.icon(
            onPressed: _load,
            icon: const Icon(Icons.refresh),
            label: Text(context.tr('Refresh results'))),
      ],
      if (p['source'] is Map) ...[
        Text(
            'Following ${(p['source'] as Map)['path']} · ${(p['source'] as Map)['base'] == 'library' ? 'Library root' : 'Playlist folder'} paths'),
        Text((p['source'] as Map)['status'] == 'ready'
            ? '${(p['source'] as Map)['unavailable']} unavailable · ${(p['source'] as Map)['rejected']} unsafe entries skipped.'
            : context.tr('Source unavailable. Last imported songs retained.')),
        Text(context.tr('Stop following to add, remove or move songs.')),
        if (_syncingSource) Text(context.tr('Updating source…')),
        Wrap(spacing: 12, children: [
          if (canSyncSource)
            OutlinedButton(
                onPressed: _syncingSource ? null : () => _source(false),
                child: Text(context.tr('Sync now'))),
          TextButton(
              onPressed: _syncingSource ? null : () => _source(true),
              child: Text(context.tr('Stop following…')))
        ]),
      ],
      Wrap(spacing: 12, runSpacing: 8, children: [
        MusicCollectionActions(
            key: ValueKey('playlist-${widget.playlistId}-$_generation'),
            collections: [MusicCollection('playlist', widget.playlistId!)],
            playLabel: context.tr('Play playlist'),
            sourceLabel: p['name'] as String,
            enabled: songs.isNotEmpty),
        if (p['can_edit'] != false) ...[
          if (p['source'] == null && p['rules'] == null)
            FilledButton.tonalIcon(
                onPressed: _addMusic,
                icon: const Icon(Icons.add),
                label: Text(context.tr('Add music'))),
          if (p['source'] == null && p['rules'] == null)
            FilledButton.tonalIcon(
                onPressed: () => _addSongs(),
                icon: const Icon(Icons.playlist_add),
                label: Text(context.tr('Add songs'))),
          if (p['source'] == null)
            TextButton(
                onPressed: () => _rules(p), child: Text(context.tr('Rules'))),
          TextButton(
              onPressed: () => _edit(p),
              child: Text(context.tr('Edit playlist'))),
          TextButton(
              onPressed: _delete, child: Text(context.tr('Delete playlist'))),
        ],
      ]),
      const SizedBox(height: 16),
      TextField(
          controller: _find,
          onChanged: (_) => setState(() {}),
          decoration: InputDecoration(
              labelText: context.tr('Find playlist songs'),
              suffixIcon: IconButton(
                  tooltip: context.tr('Clear search'),
                  icon: const Icon(Icons.clear),
                  onPressed: () {
                    _find.clear();
                    setState(() {});
                  }))),
      Text(context.tr('Showing {shown} of {count} songs', {
        'shown': songs.where((song) => _matchesSong(song, _find.text)).length,
        'count': songs.length
      })),
      if (_songError != null)
        Text(_songError!,
            style: TextStyle(color: Theme.of(context).colorScheme.error)),
      if (songs.isEmpty)
        Padding(
            padding: const EdgeInsets.all(16),
            child: Text(context.tr(p['rules'] != null
                ? 'No songs match these rules.'
                : 'This playlist is empty.'))),
      if (songs.isNotEmpty &&
          !songs.any((song) => _matchesSong(song, _find.text)))
        Padding(
            padding: const EdgeInsets.all(16),
            child: Text(context
                .tr('No matching songs. Clear the search to see all songs.'))),
      for (var i = 0; i < songs.length; i++)
        if (_matchesSong(songs[i], _find.text))
          ListTile(
              key: ValueKey((p['entries'] as List)[i]['entry_id']),
              contentPadding: EdgeInsets.zero,
              leading: Text('${i + 1}'),
              title: Text(songs[i].displayTitle),
              subtitle: Text(
                  '${songs[i].music?.artist ?? 'Unknown artist'} · ${songs[i].music?.album ?? ''}'),
              onTap: () => play(i),
              trailing: p['can_edit'] == false ||
                      p['source'] != null ||
                      p['rules'] != null
                  ? null
                  : PopupMenuButton<String>(
                      tooltip: context.tr('Song actions for {name}',
                          {'name': songs[i].displayTitle}),
                      enabled: !_editingSongs,
                      onSelected: (action) => _changeSong(action, i, songs),
                      itemBuilder: (_) => [
                        PopupMenuItem(
                            value: 'up',
                            enabled: i > 0,
                            child: Text(context.tr('Move up'))),
                        PopupMenuItem(
                            value: 'down',
                            enabled: i < songs.length - 1,
                            child: Text(context.tr('Move down'))),
                        PopupMenuItem(
                            value: 'remove',
                            child: Text(context.tr('Remove from playlist'))),
                      ],
                    )),
    ]);
  }

  Future<void> _import() async {
    final epoch = _accountEpoch, generation = _generation;
    final id = await showDialog<String>(
        context: context, builder: (_) => const PlaylistImportDialog());
    if (!mounted ||
        epoch != _accountEpoch ||
        generation != _generation ||
        id == null) {
      return;
    }
    await context.push(
        '${AppRoutes.musicPlaylists}?playlist=${Uri.encodeComponent(id)}');
    if (mounted && epoch == _accountEpoch) _load();
  }

  Future<void> _source(bool detach) async {
    if (_syncingSource) return;
    final epoch = _accountEpoch, generation = _generation;
    if (detach) {
      final yes = await showDialog<bool>(
          context: context,
          builder: (context) => AlertDialog(
                  title: Text(context.tr('Stop following source?')),
                  content: Text(
                      context.tr('Current songs remain and can be edited.')),
                  actions: [
                    TextButton(
                        onPressed: () => Navigator.pop(context, false),
                        child: Text(context.tr('Cancel'))),
                    FilledButton(
                        onPressed: () => Navigator.pop(context, true),
                        child: Text(context.tr('Stop following')))
                  ]));
      if (yes != true ||
          !mounted ||
          epoch != _accountEpoch ||
          generation != _generation) {
        return;
      }
    }
    setState(() => _syncingSource = true);
    try {
      final result = await ref
          .read(apiClientProvider)
          .syncPlaylistSource(widget.playlistId!, detach: detach);
      if (!mounted || epoch != _accountEpoch || generation != _generation) {
        return;
      }
      setState(() => _playlist = result);
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(
          content: Text(result['source'] is Map &&
                  (result['source'] as Map)['status'] == 'unavailable'
              ? context.tr('Source unavailable. Last imported songs retained.')
              : detach
                  ? context.tr('Stopped following source.')
                  : context.tr('Playlist synchronized.'))));
    } catch (e) {
      if (mounted && epoch == _accountEpoch && generation == _generation) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(
            content: Text('Could not update source. ${setsErrorMessage(e)}')));
      }
    } finally {
      if (mounted) setState(() => _syncingSource = false);
    }
  }

  Future<Map<String, dynamic>?> _saveSongs(Map<String, dynamic> body) async {
    if (_editingSongs || _playlist?['can_edit'] == false) return null;
    final generation = _generation, epoch = _accountEpoch;
    final api = ref.read(apiClientProvider), id = widget.playlistId!;
    setState(() {
      _editingSongs = true;
      _songError = null;
    });
    try {
      final result = await api.editPlaylistSongs(id, body);
      if (!mounted ||
          generation != _generation ||
          epoch != _accountEpoch ||
          !identical(api, ref.read(apiClientProvider))) {
        return null;
      }
      setState(() {
        _playlist = result;
        _artwork.clear();
        _customArtwork.clear();
      });
      return result;
    } catch (e) {
      if (mounted && generation == _generation && epoch == _accountEpoch) {
        setState(() =>
            _songError = 'Could not update playlist. ${setsErrorMessage(e)}');
      }
      return null;
    } finally {
      if (mounted) setState(() => _editingSongs = false);
    }
  }

  Future<void> _changeSong(String action, int index, List<Media> songs) async {
    final entries = _playlist!['entries'] as List;
    final mediaID = songs[index].id;
    final before = index + 1 < entries.length
        ? entries[index + 1]['entry_id'] as String
        : '';
    final epoch = _accountEpoch, generation = _generation;
    final result = await _saveSongs(
        {'action': action, 'entry_id': entries[index]['entry_id']});
    if (result == null || !mounted) return;
    ScaffoldMessenger.of(context).hideCurrentSnackBar();
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(
      content: Text(action == 'remove'
          ? context.tr('Song removed from playlist.')
          : context.tr('Song moved.')),
      action: action != 'remove'
          ? null
          : SnackBarAction(
              label: context.tr('Undo'),
              onPressed: () {
                if (epoch == _accountEpoch && generation == _generation) {
                  _saveSongs({
                    'action': 'add',
                    'media_ids': [mediaID],
                    'before_entry_id': before
                  });
                }
              }),
    ));
  }

  Future<void> _addSongs() async {
    final generation = _generation, epoch = _accountEpoch;
    final saved = await showDialog<bool>(
        context: context,
        builder: (_) => _AddSongsDialog(playlistId: widget.playlistId!));
    if (saved == true &&
        mounted &&
        generation == _generation &&
        epoch == _accountEpoch) {
      _load();
    }
  }

  Future<void> _edit([Map<String, dynamic>? p, bool smart = false]) async {
    final generation = _generation;
    final accountEpoch = _accountEpoch;
    final api = ref.read(apiClientProvider);
    var visibility = p?['visibility'] as String? ?? 'private';
    var ownerId = p?['owner_id'] as int?;
    List<User> owners = [];
    if (p?['can_transfer'] == true) {
      try {
        owners = await api.listUsers();
      } catch (e) {
        if (mounted &&
            generation == _generation &&
            accountEpoch == _accountEpoch) {
          ScaffoldMessenger.of(context).showSnackBar(SnackBar(
              content: Text('Could not load owners. ${setsErrorMessage(e)}')));
        }
        return;
      }
      if (!mounted ||
          generation != _generation ||
          accountEpoch != _accountEpoch) {
        return;
      }
    }
    final name = TextEditingController(text: p?['name'] as String? ?? '');
    final comment = TextEditingController(text: p?['comment'] as String? ?? '');
    final form = GlobalKey<FormState>();
    var saving = false;
    String? error;
    String? created;
    await showDialog<void>(
        context: context,
        barrierDismissible: false,
        builder: (dialogContext) => StatefulBuilder(
            builder: (context, update) => PopScope(
                canPop: true,
                child: AlertDialog(
                  title: Text(p == null
                      ? context.tr(
                          smart ? 'Create smart playlist' : 'Create playlist')
                      : context.tr('Edit playlist')),
                  content: SingleChildScrollView(
                      child: Form(
                          key: form,
                          child:
                              Column(mainAxisSize: MainAxisSize.min, children: [
                            TextFormField(
                                controller: name,
                                autofocus: true,
                                decoration: InputDecoration(
                                    labelText: context.tr('Name'),
                                    helperText: '1–200 characters'),
                                validator: (text) {
                                  final n = (text ?? '').trim().runes.length;
                                  return n < 1 || n > 200
                                      ? 'Enter 1–200 characters.'
                                      : null;
                                }),
                            TextFormField(
                                controller: comment,
                                minLines: 2,
                                maxLines: 5,
                                decoration: InputDecoration(
                                    labelText: context.tr('Comment'),
                                    helperText: 'Up to 2000 characters'),
                                validator: (text) =>
                                    (text ?? '').runes.length > 2000
                                        ? 'Enter at most 2000 characters.'
                                        : null),
                            DropdownButtonFormField<String>(
                              isExpanded: true,
                              initialValue: visibility,
                              decoration: InputDecoration(
                                  labelText: context.tr('Visibility')),
                              items: [
                                DropdownMenuItem(
                                    value: 'private',
                                    child: Text(context.tr('Private'))),
                                DropdownMenuItem(
                                    value: 'shared',
                                    child:
                                        Text(context.tr('Shared with server')))
                              ],
                              onChanged: saving
                                  ? null
                                  : (value) =>
                                      update(() => visibility = value!),
                            ),
                            Text(context.tr(
                                'Shared playlists can be read by signed-in accounts on this server. This does not create a public link or grant access to music.')),
                            if (p?['source'] != null &&
                                p?['can_transfer'] == true)
                              Text(context.tr(
                                  'Changing owner stops following the source and keeps the current songs.')),
                            if (p?['can_transfer'] == true)
                              DropdownButtonFormField<int>(
                                isExpanded: true,
                                initialValue: ownerId,
                                decoration: InputDecoration(
                                    labelText: context.tr('Owner')),
                                items: owners
                                    .map((u) => DropdownMenuItem(
                                        value: u.id, child: Text(u.username)))
                                    .toList(),
                                onChanged: saving
                                    ? null
                                    : (value) => update(() => ownerId = value),
                              ),
                            if (error != null)
                              Text(error!,
                                  style: TextStyle(
                                      color:
                                          Theme.of(context).colorScheme.error)),
                          ]))),
                  actions: [
                    TextButton(
                        onPressed: () => Navigator.pop(dialogContext),
                        child: Text(context.tr('Cancel'))),
                    FilledButton(
                        onPressed: saving
                            ? null
                            : () async {
                                if (!form.currentState!.validate()) {
                                  return;
                                }
                                if (generation != _generation ||
                                    accountEpoch != _accountEpoch ||
                                    !identical(
                                        api, ref.read(apiClientProvider))) {
                                  update(() => error = context.tr(
                                      'Account changed. Close this dialog and try again.'));
                                  return;
                                }
                                if (ownerId != null &&
                                    ownerId != p?['owner_id']) {
                                  final confirmed = await showDialog<bool>(
                                      context: dialogContext,
                                      builder: (confirmationContext) =>
                                          AlertDialog(
                                              title: Text(context
                                                  .tr('Transfer ownership?')),
                                              content: Text(context.tr(
                                                  'The previous owner will lose edit access unless they are an administrator.')),
                                              actions: [
                                                TextButton(
                                                    onPressed: () =>
                                                        Navigator.pop(
                                                            confirmationContext,
                                                            false),
                                                    child: Text(
                                                        context.tr('Cancel'))),
                                                FilledButton(
                                                    onPressed: () =>
                                                        Navigator.pop(
                                                            confirmationContext,
                                                            true),
                                                    child: Text(context.tr(
                                                        'Transfer ownership')))
                                              ]));
                                  if (confirmed != true ||
                                      !mounted ||
                                      !dialogContext.mounted ||
                                      generation != _generation ||
                                      accountEpoch != _accountEpoch) {
                                    return;
                                  }
                                }
                                update(() {
                                  saving = true;
                                  error = null;
                                });
                                try {
                                  final result = p == null
                                      ? visibility == 'private'
                                          ? await api.createPlaylist(
                                              name.text.trim(), comment.text)
                                          : await api.createPlaylistDetails(
                                              name.text.trim(),
                                              comment.text,
                                              visibility)
                                      : visibility ==
                                                  (p['visibility'] ??
                                                      'private') &&
                                              ownerId == p['owner_id']
                                          ? await api.updatePlaylist(
                                              p['playlist_id'] as String,
                                              name.text.trim(),
                                              comment.text)
                                          : await api.updatePlaylistDetails(
                                              p['playlist_id'] as String,
                                              name.text.trim(),
                                              comment.text,
                                              visibility,
                                              ownerId: ownerId != p['owner_id']
                                                  ? ownerId
                                                  : null);
                                  if (!mounted ||
                                      generation != _generation ||
                                      accountEpoch != _accountEpoch ||
                                      !dialogContext.mounted) {
                                    return;
                                  }
                                  if (p == null) {
                                    created = result['playlist_id'] as String;
                                  }
                                  Navigator.pop(dialogContext);
                                } catch (e) {
                                  if (dialogContext.mounted) {
                                    update(() {
                                      saving = false;
                                      error = context.tr(
                                          'Could not save playlist. {error}',
                                          {'error': setsErrorMessage(e)});
                                    });
                                  }
                                }
                              },
                        child: Text(saving
                            ? context.tr('Saving…')
                            : p == null
                                ? context.tr('Create playlist')
                                : context.tr('Save changes')))
                  ],
                ))));
    // Controllers are disposed after the route's closing animation finishes.
    await Future<void>.delayed(const Duration(milliseconds: 300));
    name.dispose();
    comment.dispose();
    if (!mounted ||
        generation != _generation ||
        accountEpoch != _accountEpoch) {
      return;
    }
    if (created != null) {
      if (smart) {
        await _rules({'playlist_id': created}, enable: true, reload: false);
        if (!mounted || accountEpoch != _accountEpoch) return;
      }
      await context.push(
          '${AppRoutes.musicPlaylists}?playlist=${Uri.encodeComponent(created!)}');
      if (mounted) {
        _load();
      }
    } else {
      _load();
    }
  }

  Future<void> _rules(Map<String, dynamic> playlist,
      {bool enable = false, bool reload = true}) async {
    final epoch = _accountEpoch, generation = _generation;
    final api = ref.read(apiClientProvider);
    final saved = await Navigator.of(context).push<bool>(MaterialPageRoute(
        builder: (_) => MusicPlaylistRulesScreen(
              rules: playlist['rules'] as Map<String, dynamic>?,
              enable: enable,
              save: (rules) async {
                if (!mounted ||
                    epoch != _accountEpoch ||
                    !identical(api, ref.read(apiClientProvider))) {
                  throw StateError('Account changed');
                }
                await api.savePlaylistRules(
                    playlist['playlist_id'] as String, rules);
                if (!mounted ||
                    epoch != _accountEpoch ||
                    !identical(api, ref.read(apiClientProvider))) {
                  throw StateError('Account changed');
                }
              },
            )));
    if (reload &&
        saved == true &&
        mounted &&
        epoch == _accountEpoch &&
        generation == _generation) {
      _load();
    }
  }

  Future<void> _memberships() async {
    final api = ref.read(apiClientProvider);
    final epoch = _accountEpoch;
    final generation = _generation;
    final songs = (_playlist?['songs'] as List? ?? [])
        .map((data) => Media.fromJson(data as Map<String, dynamic>))
        .toList();
    try {
      final lists = await api.playlistMembership();
      if (!mounted || epoch != _accountEpoch || generation != _generation) {
        return;
      }
      await showDialog<void>(
          context: context,
          builder: (context) => AlertDialog(
                  title: Text(context.tr('Containing playlists')),
                  scrollable: true,
                  content: Column(mainAxisSize: MainAxisSize.min, children: [
                    if (songs.isEmpty)
                      Text(context.tr('This playlist is empty.')),
                    for (final song in songs)
                      ListTile(
                          title: Text(song.displayTitle),
                          subtitle: Text(lists
                              .where((list) => (list['entries'] as List? ?? [])
                                  .any((entry) => entry['media_id'] == song.id))
                              .map((list) => list['name'])
                              .join(', ')))
                  ]),
                  actions: [
                    TextButton(
                        onPressed: () => Navigator.pop(context),
                        child: Text(context.tr('Close')))
                  ]));
    } catch (e) {
      if (mounted && epoch == _accountEpoch && generation == _generation) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(
            content:
                Text('Could not load memberships. ${setsErrorMessage(e)}')));
      }
    }
  }

  Future<void> _export() async {
    final p = _playlist!, generation = _generation;
    final epoch = _accountEpoch, api = ref.read(apiClientProvider);
    final filename = 'playlist-${p['playlist_id']}.m3u8';
    var saving = false;
    String? error;
    bool sameAccount() =>
        mounted &&
        generation == _generation &&
        epoch == _accountEpoch &&
        identical(api, ref.read(apiClientProvider));
    await showDialog<void>(
        context: context,
        builder: (dialogContext) => StatefulBuilder(
            builder: (context, update) => AlertDialog(
                    scrollable: true,
                    title: Text(context.tr('Export playlist')),
                    content: Column(
                        mainAxisSize: MainAxisSize.min,
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(p['name'] as String),
                          Text('UTF-8 M3U8 · $filename'),
                          const SizedBox(height: 12),
                          Text(context.tr(
                              'Paths are relative to the server’s media root. Save this file beside the corresponding library folders in the other tool.')),
                          const SizedBox(height: 12),
                          Text(context.tr(
                              'Only songs you can access with exportable paths are included; repeated songs keep their order. This file does not grant access to music.')),
                          if (saving) const Text('Preparing…'),
                          if (error != null)
                            Semantics(
                                liveRegion: true,
                                child: Text(error!,
                                    style: TextStyle(
                                        color: Theme.of(context)
                                            .colorScheme
                                            .error))),
                        ]),
                    actions: [
                      TextButton(
                          onPressed: () => Navigator.pop(dialogContext),
                          child: Text(context.tr('Cancel'))),
                      FilledButton(
                          onPressed: saving
                              ? null
                              : () async {
                                  if (!sameAccount()) {
                                    update(() => error = context.tr(
                                        'Account changed. Close this dialog and try again.'));
                                    return;
                                  }
                                  update(() {
                                    saving = true;
                                    error = null;
                                  });
                                  try {
                                    final content = await api.exportPlaylist(
                                        p['playlist_id'] as String);
                                    if (!mounted ||
                                        !dialogContext.mounted ||
                                        !sameAccount()) {
                                      return;
                                    }
                                    final saved = await PlaylistExport.save(
                                        filename, content);
                                    if (!mounted ||
                                        !dialogContext.mounted ||
                                        !sameAccount()) {
                                      return;
                                    }
                                    if (saved) {
                                      Navigator.pop(dialogContext);
                                      ScaffoldMessenger.of(this.context)
                                          .showSnackBar(SnackBar(
                                              content: Text(context
                                                  .tr('Playlist saved.'))));
                                    }
                                  } catch (e) {
                                    if (dialogContext.mounted &&
                                        sameAccount()) {
                                      update(() => error =
                                          'Could not export playlist. ${setsErrorMessage(e)}');
                                    }
                                  } finally {
                                    if (dialogContext.mounted) {
                                      update(() => saving = false);
                                    }
                                  }
                                },
                          child: Text(saving ? 'Preparing…' : 'Save M3U8')),
                    ])));
  }

  Future<void> _addMusic() async {
    final generation = _generation;
    final accountEpoch = _accountEpoch;
    final saved = await showDialog<bool>(
        context: context,
        barrierDismissible: false,
        builder: (_) => _AddAlbumDialog(playlistId: widget.playlistId!));
    if (saved == true &&
        mounted &&
        generation == _generation &&
        accountEpoch == _accountEpoch) {
      _load();
    }
  }

  Future<void> _delete() async {
    final p = _playlist!, generation = _generation;
    final accountEpoch = _accountEpoch;
    final api = ref.read(apiClientProvider);
    var saving = false;
    String? error;
    var deleted = false;
    await showDialog<void>(
        context: context,
        barrierDismissible: false,
        builder: (dialogContext) => StatefulBuilder(
            builder: (context, update) => PopScope(
                canPop: true,
                child: AlertDialog(
                  title: Text(context.tr('Delete playlist')),
                  content: Text(error ??
                      'Delete “${p['name']}”? Music files stay available in your library.'),
                  actions: [
                    TextButton(
                        onPressed: () => Navigator.pop(dialogContext),
                        child: Text(context.tr('Cancel'))),
                    FilledButton(
                        onPressed: saving
                            ? null
                            : () async {
                                if (generation != _generation ||
                                    accountEpoch != _accountEpoch ||
                                    !identical(
                                        api, ref.read(apiClientProvider))) {
                                  update(() => error = context.tr(
                                      'Account changed. Close this dialog and try again.'));
                                  return;
                                }
                                update(() => saving = true);
                                try {
                                  await api.deletePlaylist(
                                      p['playlist_id'] as String);
                                  if (!mounted ||
                                      generation != _generation ||
                                      accountEpoch != _accountEpoch ||
                                      !dialogContext.mounted) {
                                    return;
                                  }
                                  deleted = true;
                                  Navigator.pop(dialogContext);
                                } catch (e) {
                                  if (dialogContext.mounted) {
                                    update(() {
                                      saving = false;
                                      error =
                                          'Could not delete playlist. ${setsErrorMessage(e)}';
                                    });
                                  }
                                }
                              },
                        child: Text(context.tr('Delete playlist')))
                  ],
                ))));
    if (deleted &&
        mounted &&
        generation == _generation &&
        accountEpoch == _accountEpoch) {
      context.pop();
    }
  }
}

class _AddAlbumDialog extends ConsumerStatefulWidget {
  const _AddAlbumDialog({required this.playlistId});
  final String playlistId;
  @override
  ConsumerState<_AddAlbumDialog> createState() => _AddAlbumState();
}

class _AddAlbumState extends ConsumerState<_AddAlbumDialog> {
  late final PlayerApiClient _api;
  bool _accountChanged = false;
  late final int _accountEpoch;
  bool get _sameAccount =>
      _accountEpoch == ref.read(credentialMutationQueueProvider).generation;
  List<Map<String, dynamic>> _albums = [];
  bool _loading = true, _saving = false;
  String? _error;
  @override
  void initState() {
    super.initState();
    _api = ref.read(apiClientProvider);
    _accountEpoch = ref.read(credentialMutationQueueProvider).generation;
    Future.microtask(_load);
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final data = await _api.musicAlbums();
      if (mounted && !_accountChanged && _sameAccount) {
        setState(() {
          _albums = data;
          _loading = false;
        });
      }
    } catch (e) {
      if (mounted && !_accountChanged && _sameAccount) {
        setState(() {
          _loading = false;
          _error = 'Could not load albums. ${setsErrorMessage(e)}';
        });
      }
    }
  }

  Future<void> _add(String key) async {
    if (!_sameAccount ||
        _accountChanged ||
        !identical(_api, ref.read(apiClientProvider))) {
      return;
    }
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      await _api.addPlaylistAlbum(widget.playlistId, key);
      if (mounted && !_accountChanged && _sameAccount) {
        Navigator.pop(context, true);
      }
    } catch (e) {
      if (mounted && !_accountChanged && _sameAccount) {
        setState(() {
          _saving = false;
          _error = 'Could not add music. ${setsErrorMessage(e)}';
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    ref.listen(apiClientProvider, (previous, next) {
      if (!identical(_api, next)) {
        setState(() {
          _accountChanged = true;
          _saving = false;
          _loading = false;
          _albums = [];
          _error =
              context.tr('Account changed. Close this dialog and try again.');
        });
      }
    });
    return PopScope(
        canPop: true,
        child: AlertDialog(
          title: Text(context.tr('Add music')),
          content: SizedBox(
              width: double.maxFinite,
              child: _loading
                  ? const Center(
                      heightFactor: 2, child: CircularProgressIndicator())
                  : SingleChildScrollView(
                      child: Column(mainAxisSize: MainAxisSize.min, children: [
                      if (_error != null)
                        Text(_error!,
                            style: TextStyle(
                                color: Theme.of(context).colorScheme.error)),
                      if (_albums.isEmpty && _error == null)
                        Text(context.tr(
                            'No albums available. Browse Library or ask an administrator to rescan music tags.')),
                      if (_albums.isNotEmpty)
                        Text(context.tr(
                            'Choose an album to add its songs in album order.')),
                      for (final album in _albums)
                        ListTile(
                            title: Text(album['title'] as String),
                            subtitle: Text(album['artist'] as String),
                            enabled: !_saving && !_accountChanged,
                            onTap: () => _add(album['album_key'] as String)),
                    ]))),
          actions: [
            if (_error != null && _albums.isEmpty && !_accountChanged)
              TextButton(onPressed: _load, child: Text(context.tr('Retry'))),
            TextButton(
                onPressed: () => Navigator.pop(context, false),
                child: Text(context.tr('Cancel')))
          ],
        ));
  }
}

bool _matchesSong(Media song, String query) => [
      song.displayTitle,
      song.fileName,
      song.music?.artist,
      song.music?.album
    ].join(' ').toLowerCase().contains(query.trim().toLowerCase());

class _AddSongsDialog extends ConsumerStatefulWidget {
  const _AddSongsDialog({required this.playlistId});
  final String playlistId;
  @override
  ConsumerState<_AddSongsDialog> createState() => _AddSongsState();
}

class _AddSongsState extends ConsumerState<_AddSongsDialog> {
  late final PlayerApiClient _api;
  late final int _epoch;
  final _find = TextEditingController();
  final _selected = <int>{};
  List<Media> _songs = [];
  List<Map<String, dynamic>> _membership = [];
  final _targets = <String>{};
  bool _skipDuplicates = true;
  bool _loading = true, _saving = false, _changed = false;
  String? _error;
  bool get _same =>
      !_changed &&
      _epoch == ref.read(credentialMutationQueueProvider).generation &&
      identical(_api, ref.read(apiClientProvider));
  @override
  void initState() {
    super.initState();
    _api = ref.read(apiClientProvider);
    _epoch = ref.read(credentialMutationQueueProvider).generation;
    _targets.add(widget.playlistId);
    Future.microtask(_load);
  }

  @override
  void dispose() {
    _find.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final membership = await _api.playlistMembership();
      final albums = await _api.musicAlbums();
      final details = await Future.wait(
          albums.map((album) => _api.musicAlbum(album['album_key'] as String)));
      final songs = <int, Media>{};
      for (final detail in details) {
        for (final data in detail['songs'] as List) {
          final song = Media.fromJson(data as Map<String, dynamic>);
          songs[song.id] = song;
        }
      }
      if (mounted && _same) {
        setState(() {
          _membership = membership;
          _songs = songs.values.toList();
          _loading = false;
        });
      }
    } catch (e) {
      if (mounted && _same) {
        setState(() {
          _loading = false;
          _error = 'Could not load songs. ${setsErrorMessage(e)}';
        });
      }
    }
  }

  Future<void> _save() async {
    if (!_same || _selected.isEmpty || _saving) return;
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      await _api.addToPlaylists({
        'playlist_ids': _targets.toList(),
        'duplicate_policy': _skipDuplicates ? 'skip' : 'add',
        'media_ids': _songs
            .where((song) => _selected.contains(song.id))
            .map((song) => song.id)
            .toList()
      });
      if (mounted && _same) Navigator.pop(context, true);
    } catch (e) {
      if (mounted && _same) {
        setState(() {
          _saving = false;
          _error = 'Could not add songs. ${setsErrorMessage(e)}';
        });
      }
    }
  }

  String _containing(int id) {
    final names = _membership
        .where((list) => (list['entries'] as List? ?? [])
            .any((entry) => entry['media_id'] == id))
        .map((list) => list['name']);
    return names.isEmpty ? 'None' : names.join(', ');
  }

  @override
  Widget build(BuildContext context) {
    ref.listen(apiClientProvider, (_, next) {
      if (!identical(_api, next)) {
        setState(() {
          _changed = true;
          _songs = [];
          _loading = false;
          _saving = false;
          _error =
              context.tr('Account changed. Close this dialog and try again.');
        });
      }
    });
    final visible =
        _songs.where((song) => _matchesSong(song, _find.text)).toList();
    return AlertDialog(
        scrollable: true,
        title: Text(context.tr('Add songs')),
        content: SizedBox(
            width: double.maxFinite,
            height: MediaQuery.sizeOf(context).height * .48,
            child: ListView(children: [
              TextField(
                  controller: _find,
                  autofocus: true,
                  onChanged: (_) => setState(() {}),
                  decoration: InputDecoration(
                      labelText: context.tr('Find songs to add'))),
              CheckboxListTile(
                  title: Text(context.tr('Skip repeated songs')),
                  subtitle:
                      Text(context.tr('Skip songs already in each playlist')),
                  value: _skipDuplicates,
                  onChanged: _saving || !_same
                      ? null
                      : (value) =>
                          setState(() => _skipDuplicates = value ?? true)),
              ExpansionTile(
                  title: Text(context.tr('Add to more playlists')),
                  children: [
                    for (final list in _membership.where((list) =>
                        list['can_edit'] == true &&
                        list['source'] == null &&
                        list['rules'] == null &&
                        list['playlist_id'] != widget.playlistId))
                      CheckboxListTile(
                          title: Text(list['name'] as String),
                          value: _targets.contains(list['playlist_id']),
                          onChanged: _saving || !_same
                              ? null
                              : (value) => setState(() {
                                    value == true
                                        ? _targets
                                            .add(list['playlist_id'] as String)
                                        : _targets.remove(list['playlist_id']);
                                  }))
                  ]),
              Text(context.tr('{count} selected', {'count': _selected.length})),
              if (_error != null)
                Text(_error!,
                    style:
                        TextStyle(color: Theme.of(context).colorScheme.error)),
              if (_loading) const CircularProgressIndicator(),
              if (!_loading && visible.isEmpty)
                Text(_songs.isEmpty
                    ? context.tr('No songs available.')
                    : context.tr('No matching songs.')),
              ListView.builder(
                  shrinkWrap: true,
                  physics: const NeverScrollableScrollPhysics(),
                  itemCount: visible.length,
                  itemBuilder: (_, index) {
                    final song = visible[index];
                    return CheckboxListTile(
                        title: Text(song.displayTitle),
                        subtitle: Text(
                            '${song.music?.artist ?? 'Unknown artist'} · ${song.music?.album ?? ''}\nIn playlists: ${_containing(song.id)}'),
                        value: _selected.contains(song.id),
                        onChanged: _saving || !_same
                            ? null
                            : (checked) => setState(() {
                                  checked == true
                                      ? _selected.add(song.id)
                                      : _selected.remove(song.id);
                                }));
                  }),
            ])),
        actions: [
          if (_error != null && _songs.isEmpty && !_changed)
            TextButton(onPressed: _load, child: Text(context.tr('Retry'))),
          TextButton(
              onPressed: () => Navigator.pop(context, false),
              child: Text(context.tr('Cancel'))),
          FilledButton(
              onPressed: _saving || _selected.isEmpty || !_same ? null : _save,
              child: Text(_saving
                  ? context.tr('Saving…')
                  : context.tr('Add selected songs'))),
        ]);
  }
}
