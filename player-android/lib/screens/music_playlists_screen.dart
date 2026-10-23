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
      '${p['visibility'] == 'shared' ? 'Shared with server' : 'Private'} · Owner: ${p['owner_name'] ?? ''}${p['can_edit'] == false ? ' · Read only' : ''}${p['source'] != null ? ' · Following source' : ''}';
  String _totals(Map<String, dynamic> p) =>
      '${p['song_count']} songs · ${_duration(p['duration'] as num)}';
  String _duration(num value) {
    final seconds = value.round();
    return '${seconds ~/ 60}:${(seconds % 60).toString().padLeft(2, '0')}';
  }

  Widget _cover(Map<String, dynamic> p, double size) {
    final id = p['cover_media_id'] as int?;
    final placeholder = ColoredBox(
        color: Theme.of(context).colorScheme.surfaceContainerHighest,
        child: const Center(
            child:
                Icon(Icons.queue_music, semanticLabel: 'No playlist artwork')));
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
              ? 'Music · Playlists'
              : _playlist?['name'] as String? ?? 'Playlist'),
          actions: [
            const MusicSearchButton(),
            const MusicQueueButton(),
            if (widget.playlistId != null &&
                !_loading &&
                _error == null &&
                _playlist != null)
              PopupMenuButton<String>(
                  tooltip: 'Playlist actions',
                  onSelected: (_) => _export(),
                  itemBuilder: (_) => const [
                        PopupMenuItem(
                            value: 'export', child: Text('Export M3U8…'))
                      ]),
            if (widget.playlistId == null)
              IconButton(
                  tooltip: 'Create playlist',
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
                        Text('Could not load playlists. $_error'),
                        FilledButton(
                            onPressed: _load, child: const Text('Retry'))
                      ])))
              : widget.playlistId == null
                  ? _catalog()
                  : _detail(),
    );
  }

  Widget _catalog() => MusicCollectionCatalog(
      key: ValueKey('playlists-$_generation'),
      items: _list,
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
                label: const Text('Create playlist')),
            OutlinedButton.icon(
                onPressed: _import,
                icon: const Icon(Icons.playlist_add),
                label: const Text('Import M3U…')),
            if (_list.isEmpty)
              const Padding(
                  padding: EdgeInsets.symmetric(vertical: 24),
                  child: Text(
                      'No playlists yet. Create a playlist to save music for later.')),
          ])),
      itemBuilder: (p, checkbox, toggle) => ListTile(
          leading: checkbox ?? _cover(p, 56),
          title: Text(p['name'] as String,
              maxLines: 2, overflow: TextOverflow.ellipsis),
          subtitle: Text('${_totals(p)}\n${_accessLabel(p)}'),
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
        _cover(p, 112),
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
      if (p['source'] is Map) ...[
        Text(
            'Following ${(p['source'] as Map)['path']} · ${(p['source'] as Map)['base'] == 'library' ? 'Library root' : 'Playlist folder'} paths'),
        Text((p['source'] as Map)['status'] == 'ready'
            ? '${(p['source'] as Map)['unavailable']} unavailable · ${(p['source'] as Map)['rejected']} unsafe entries skipped.'
            : 'Source unavailable. Last imported songs retained.'),
        const Text('Stop following to add, remove or move songs.'),
        if (_syncingSource) const Text('Updating source…'),
        Wrap(spacing: 12, children: [
          if (canSyncSource)
            OutlinedButton(
                onPressed: _syncingSource ? null : () => _source(false),
                child: const Text('Sync now')),
          TextButton(
              onPressed: _syncingSource ? null : () => _source(true),
              child: const Text('Stop following…'))
        ]),
      ],
      Wrap(spacing: 12, runSpacing: 8, children: [
        MusicCollectionActions(
            key: ValueKey('playlist-${widget.playlistId}-$_generation'),
            collections: [MusicCollection('playlist', widget.playlistId!)],
            playLabel: 'Play playlist',
            sourceLabel: p['name'] as String,
            enabled: songs.isNotEmpty),
        if (p['can_edit'] != false) ...[
          if (p['source'] == null)
            FilledButton.tonalIcon(
                onPressed: _addMusic,
                icon: const Icon(Icons.add),
                label: const Text('Add music')),
          if (p['source'] == null)
            FilledButton.tonalIcon(
                onPressed: () => _addSongs(),
                icon: const Icon(Icons.playlist_add),
                label: const Text('Add songs')),
          TextButton(
              onPressed: () => _edit(p), child: const Text('Edit playlist')),
          TextButton(onPressed: _delete, child: const Text('Delete playlist')),
        ],
      ]),
      const SizedBox(height: 16),
      TextField(
          controller: _find,
          onChanged: (_) => setState(() {}),
          decoration: InputDecoration(
              labelText: 'Find playlist songs',
              suffixIcon: IconButton(
                  tooltip: 'Clear search',
                  icon: const Icon(Icons.clear),
                  onPressed: () {
                    _find.clear();
                    setState(() {});
                  }))),
      Text(
          'Showing ${songs.where((song) => _matchesSong(song, _find.text)).length} of ${songs.length} songs'),
      if (_songError != null)
        Text(_songError!,
            style: TextStyle(color: Theme.of(context).colorScheme.error)),
      if (songs.isEmpty)
        const Padding(
            padding: EdgeInsets.all(16),
            child: Text('This playlist is empty.')),
      if (songs.isNotEmpty &&
          !songs.any((song) => _matchesSong(song, _find.text)))
        const Padding(
            padding: EdgeInsets.all(16),
            child:
                Text('No matching songs. Clear the search to see all songs.')),
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
              trailing: p['can_edit'] == false || p['source'] != null
                  ? null
                  : PopupMenuButton<String>(
                      tooltip: 'Song actions for ${songs[i].displayTitle}',
                      enabled: !_editingSongs,
                      onSelected: (action) => _changeSong(action, i, songs),
                      itemBuilder: (_) => [
                        PopupMenuItem(
                            value: 'up',
                            enabled: i > 0,
                            child: const Text('Move up')),
                        PopupMenuItem(
                            value: 'down',
                            enabled: i < songs.length - 1,
                            child: const Text('Move down')),
                        const PopupMenuItem(
                            value: 'remove',
                            child: Text('Remove from playlist')),
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
                  title: const Text('Stop following source?'),
                  content:
                      const Text('Current songs remain and can be edited.'),
                  actions: [
                    TextButton(
                        onPressed: () => Navigator.pop(context, false),
                        child: const Text('Cancel')),
                    FilledButton(
                        onPressed: () => Navigator.pop(context, true),
                        child: const Text('Stop following'))
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
              ? 'Source unavailable. Last imported songs retained.'
              : detach
                  ? 'Stopped following source.'
                  : 'Playlist synchronized.')));
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
      content: Text(
          action == 'remove' ? 'Song removed from playlist.' : 'Song moved.'),
      action: action != 'remove'
          ? null
          : SnackBarAction(
              label: 'Undo',
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

  Future<void> _edit([Map<String, dynamic>? p]) async {
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
                  title: Text(p == null ? 'Create playlist' : 'Edit playlist'),
                  content: SingleChildScrollView(
                      child: Form(
                          key: form,
                          child:
                              Column(mainAxisSize: MainAxisSize.min, children: [
                            TextFormField(
                                controller: name,
                                autofocus: true,
                                decoration: const InputDecoration(
                                    labelText: 'Name',
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
                                decoration: const InputDecoration(
                                    labelText: 'Comment',
                                    helperText: 'Up to 2000 characters'),
                                validator: (text) =>
                                    (text ?? '').runes.length > 2000
                                        ? 'Enter at most 2000 characters.'
                                        : null),
                            DropdownButtonFormField<String>(
                              isExpanded: true,
                              initialValue: visibility,
                              decoration: const InputDecoration(
                                  labelText: 'Visibility'),
                              items: const [
                                DropdownMenuItem(
                                    value: 'private', child: Text('Private')),
                                DropdownMenuItem(
                                    value: 'shared',
                                    child: Text('Shared with server'))
                              ],
                              onChanged: saving
                                  ? null
                                  : (value) =>
                                      update(() => visibility = value!),
                            ),
                            const Text(
                                'Shared playlists can be read by signed-in accounts on this server. This does not create a public link or grant access to music.'),
                            if (p?['source'] != null &&
                                p?['can_transfer'] == true)
                              const Text(
                                  'Changing owner stops following the source and keeps the current songs.'),
                            if (p?['can_transfer'] == true)
                              DropdownButtonFormField<int>(
                                isExpanded: true,
                                initialValue: ownerId,
                                decoration:
                                    const InputDecoration(labelText: 'Owner'),
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
                        child: const Text('Cancel')),
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
                                  update(() => error =
                                      'Account changed. Close this dialog and try again.');
                                  return;
                                }
                                if (ownerId != null &&
                                    ownerId != p?['owner_id']) {
                                  final confirmed = await showDialog<bool>(
                                      context: dialogContext,
                                      builder: (confirmationContext) =>
                                          AlertDialog(
                                              title: const Text(
                                                  'Transfer ownership?'),
                                              content: const Text(
                                                  'The previous owner will lose edit access unless they are an administrator.'),
                                              actions: [
                                                TextButton(
                                                    onPressed: () =>
                                                        Navigator.pop(
                                                            confirmationContext,
                                                            false),
                                                    child:
                                                        const Text('Cancel')),
                                                FilledButton(
                                                    onPressed: () =>
                                                        Navigator.pop(
                                                            confirmationContext,
                                                            true),
                                                    child: const Text(
                                                        'Transfer ownership'))
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
                                      error =
                                          'Could not save playlist. ${setsErrorMessage(e)}';
                                    });
                                  }
                                }
                              },
                        child: Text(saving
                            ? 'Saving…'
                            : p == null
                                ? 'Create playlist'
                                : 'Save changes'))
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
      await context.push(
          '${AppRoutes.musicPlaylists}?playlist=${Uri.encodeComponent(created!)}');
      if (mounted) {
        _load();
      }
    } else {
      _load();
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
                    title: const Text('Export playlist'),
                    content: Column(
                        mainAxisSize: MainAxisSize.min,
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(p['name'] as String),
                          Text('UTF-8 M3U8 · $filename'),
                          const SizedBox(height: 12),
                          const Text(
                              'Paths are relative to the server’s media root. Save this file beside the corresponding library folders in the other tool.'),
                          const SizedBox(height: 12),
                          const Text(
                              'Only songs you can access with exportable paths are included; repeated songs keep their order. This file does not grant access to music.'),
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
                          child: const Text('Cancel')),
                      FilledButton(
                          onPressed: saving
                              ? null
                              : () async {
                                  if (!sameAccount()) {
                                    update(() => error =
                                        'Account changed. Close this dialog and try again.');
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
                                          .showSnackBar(const SnackBar(
                                              content:
                                                  Text('Playlist saved.')));
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
                  title: const Text('Delete playlist'),
                  content: Text(error ??
                      'Delete “${p['name']}”? Music files stay available in your library.'),
                  actions: [
                    TextButton(
                        onPressed: () => Navigator.pop(dialogContext),
                        child: const Text('Cancel')),
                    FilledButton(
                        onPressed: saving
                            ? null
                            : () async {
                                if (generation != _generation ||
                                    accountEpoch != _accountEpoch ||
                                    !identical(
                                        api, ref.read(apiClientProvider))) {
                                  update(() => error =
                                      'Account changed. Close this dialog and try again.');
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
                        child: const Text('Delete playlist'))
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
          _error = 'Account changed. Close this dialog and try again.';
        });
      }
    });
    return PopScope(
        canPop: true,
        child: AlertDialog(
          title: const Text('Add music'),
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
                        const Text(
                            'No albums available. Browse Library or ask an administrator to rescan music tags.'),
                      if (_albums.isNotEmpty)
                        const Text(
                            'Choose an album to add its songs in album order.'),
                      for (final album in _albums)
                        ListTile(
                            title: Text(album['title'] as String),
                            subtitle: Text(album['artist'] as String),
                            enabled: !_saving && !_accountChanged,
                            onTap: () => _add(album['album_key'] as String)),
                    ]))),
          actions: [
            if (_error != null && _albums.isEmpty && !_accountChanged)
              TextButton(onPressed: _load, child: const Text('Retry')),
            TextButton(
                onPressed: () => Navigator.pop(context, false),
                child: const Text('Cancel'))
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
      await _api.editPlaylistSongs(widget.playlistId, {
        'action': 'add',
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

  @override
  Widget build(BuildContext context) {
    ref.listen(apiClientProvider, (_, next) {
      if (!identical(_api, next)) {
        setState(() {
          _changed = true;
          _songs = [];
          _loading = false;
          _saving = false;
          _error = 'Account changed. Close this dialog and try again.';
        });
      }
    });
    final visible =
        _songs.where((song) => _matchesSong(song, _find.text)).toList();
    return AlertDialog(
        scrollable: true,
        title: const Text('Add songs'),
        content: SizedBox(
            width: double.maxFinite,
            height: MediaQuery.sizeOf(context).height * .48,
            child: Column(children: [
              TextField(
                  controller: _find,
                  autofocus: true,
                  onChanged: (_) => setState(() {}),
                  decoration:
                      const InputDecoration(labelText: 'Find songs to add')),
              const Text('Repeated songs are added again.'),
              Text('${_selected.length} selected'),
              if (_error != null)
                Text(_error!,
                    style:
                        TextStyle(color: Theme.of(context).colorScheme.error)),
              if (_loading) const CircularProgressIndicator(),
              if (!_loading && visible.isEmpty)
                Text(_songs.isEmpty
                    ? 'No songs available.'
                    : 'No matching songs.'),
              Expanded(
                  child: ListView.builder(
                      itemCount: visible.length,
                      itemBuilder: (_, index) {
                        final song = visible[index];
                        return CheckboxListTile(
                            title: Text(song.displayTitle),
                            subtitle: Text(
                                '${song.music?.artist ?? 'Unknown artist'} · ${song.music?.album ?? ''}'),
                            value: _selected.contains(song.id),
                            onChanged: _saving || !_same
                                ? null
                                : (checked) => setState(() {
                                      checked == true
                                          ? _selected.add(song.id)
                                          : _selected.remove(song.id);
                                    }));
                      })),
            ])),
        actions: [
          if (_error != null && _songs.isEmpty && !_changed)
            TextButton(onPressed: _load, child: const Text('Retry')),
          TextButton(
              onPressed: () => Navigator.pop(context, false),
              child: const Text('Cancel')),
          FilledButton(
              onPressed: _saving || _selected.isEmpty || !_same ? null : _save,
              child: Text(_saving ? 'Saving…' : 'Add selected songs')),
        ]);
  }
}
