import 'dart:convert';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:go_router/go_router.dart';
import '../app_routes.dart';
import '../providers/api_client_provider.dart';
import '../providers/auth_state_provider.dart';
import '../services/music_home.dart';
import 'package:flutter/material.dart';
import '../services/music_discovery.dart';
import '../services/music_collections.dart';
import 'music_collection_actions.dart';

/// Compact discovery sections share the catalog's queue actions and identities.
class MusicDiscoveryPanel extends ConsumerStatefulWidget {
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
  ConsumerState<MusicDiscoveryPanel> createState() =>
      _MusicDiscoveryPanelState();
}

class _MusicDiscoveryPanelState extends ConsumerState<MusicDiscoveryPanel> {
  MusicHome? _home;
  String? _scope;
  List<Map<String, dynamic>>? _playlists;
  bool _playlistFailed = false;
  int _playlistRequest = 0;
  Future<void> _writes = Future.value();
  String label(String key) =>
      key == 'pinned' ? 'Pinned playlists' : albumCollections[key]![0];

  Future<void> _load(String scope) async {
    MusicHome home;
    try {
      final prefs = await SharedPreferences.getInstance();
      final raw = prefs.getString(scope);
      home = MusicHome(raw == null
          ? null
          : Map<String, dynamic>.from(jsonDecode(raw) as Map));
    } catch (_) {
      home = MusicHome();
    }
    if (!mounted || _scope != scope) return;
    setState(() => _home = home);
    await _loadPlaylists(scope);
  }

  Future<void> _loadPlaylists(String scope) async {
    final client = ref.read(apiClientProvider);
    final request = ++_playlistRequest;
    if (mounted && _scope == scope) {
      setState(() {
        _playlists = null;
        _playlistFailed = false;
      });
    }
    try {
      final lists = await client.playlists();
      if (mounted &&
          _scope == scope &&
          request == _playlistRequest &&
          ref.read(apiClientProvider) == client) {
        setState(() => _playlists = lists);
      }
    } catch (_) {
      if (mounted &&
          _scope == scope &&
          request == _playlistRequest &&
          ref.read(apiClientProvider) == client) {
        setState(() => _playlistFailed = true);
      }
    }
  }

  void _update(VoidCallback change) {
    setState(change);
    final scope = _scope!, raw = jsonEncode(_home!.toJson());
    _writes = _writes.then((_) async {
      try {
        final prefs = await SharedPreferences.getInstance();
        if (!await prefs.setString(scope, raw)) {
          throw StateError('Storage unavailable');
        }
      } catch (_) {
        if (mounted && _scope == scope) {
          ScaffoldMessenger.of(context).showSnackBar(
              const SnackBar(content: Text('Could not remember home.')));
        }
      }
    });
  }

  Future<void> _customize() async {
    final scope = _scope;
    if (_playlists == null) await _loadPlaylists(scope!);
    if (!mounted || scope != _scope) return;
    await showDialog<void>(
        context: context,
        builder: (dialogContext) => Consumer(builder: (context, ref, _) {
              final account =
                  ref.watch(authStateProvider).valueOrNull?.user?.id;
              final origin = ref.watch(playerBaseUrlProvider).origin;
              return StatefulBuilder(builder: (context, updateDialog) {
                // An account change immediately removes previous-account titles from an open dialog.
                final sameAccount = mounted &&
                    _scope == scope &&
                    scope == 'player.music-home.v1:$origin:$account';
                void change(VoidCallback action) {
                  if (sameAccount) {
                    _update(action);
                    updateDialog(() {});
                  }
                }

                final home = _home ?? MusicHome();
                return AlertDialog(
                    title: const Text('Customize home'),
                    content: SizedBox(
                        width: 480,
                        child: SingleChildScrollView(
                            child: Column(
                                mainAxisSize: MainAxisSize.min,
                                children: [
                              const Text(
                                  'Saved on this device for this server and account. Changes apply immediately.'),
                              if (!sameAccount)
                                const Text(
                                    'Account changed. Close and reopen Customize home.')
                              else ...[
                                for (var i = 0; i < home.order.length; i++)
                                  Column(children: [
                                    SwitchListTile(
                                        title: Text(label(home.order[i])),
                                        value: !home.hidden
                                            .contains(home.order[i]),
                                        onChanged: (show) => change(() {
                                              if (show) {
                                                home.hidden
                                                    .remove(home.order[i]);
                                              } else {
                                                home.hidden.add(home.order[i]);
                                              }
                                            })),
                                    Row(
                                        mainAxisAlignment:
                                            MainAxisAlignment.end,
                                        children: [
                                          IconButton(
                                              tooltip:
                                                  'Move ${label(home.order[i])} up',
                                              icon: const Icon(
                                                  Icons.arrow_upward),
                                              onPressed: i == 0
                                                  ? null
                                                  : () => change(() {
                                                        final key = home.order
                                                            .removeAt(i);
                                                        home.order
                                                            .insert(i - 1, key);
                                                      })),
                                          IconButton(
                                              tooltip:
                                                  'Move ${label(home.order[i])} down',
                                              icon: const Icon(
                                                  Icons.arrow_downward),
                                              onPressed: i ==
                                                      home.order.length - 1
                                                  ? null
                                                  : () => change(() {
                                                        final key = home.order
                                                            .removeAt(i);
                                                        home.order
                                                            .insert(i + 1, key);
                                                      })),
                                        ]),
                                  ]),
                                TextButton(
                                    onPressed: () =>
                                        change(() => _home = MusicHome()),
                                    child: const Text('Restore defaults')),
                                Text('Pin playlists',
                                    style: Theme.of(context)
                                        .textTheme
                                        .titleMedium),
                                if (_playlists == null)
                                  Text(_playlistFailed
                                      ? 'Playlists unavailable. Check your connection and retry.'
                                      : 'Loading playlists. Reopen Customize home when ready.'),
                                if (_playlistFailed)
                                  TextButton(
                                      onPressed: () async {
                                        await _loadPlaylists(scope!);
                                        if (context.mounted) {
                                          updateDialog(() {});
                                        }
                                      },
                                      child: const Text('Retry playlists')),
                                if (_playlists?.isEmpty == true)
                                  const Text(
                                      'No readable playlists. Create one in Music · Playlists.'),
                                for (final playlist
                                    in _playlists ?? <Map<String, dynamic>>[])
                                  CheckboxListTile(
                                      title: Text('${playlist['name']}'),
                                      value: home.pins
                                          .contains(playlist['playlist_id']),
                                      onChanged: (pin) => change(() {
                                            final id = playlist['playlist_id']
                                                as String;
                                            if (pin == true) {
                                              home.pins.add(id);
                                            } else {
                                              home.pins.remove(id);
                                            }
                                          })),
                              ],
                            ]))),
                    actions: [
                      TextButton(
                          onPressed: () => Navigator.pop(dialogContext),
                          child: const Text('Done'))
                    ]);
              });
            }));
  }

  Widget _pinned() {
    final home = _home!, lists = _playlists;
    final accessible = lists == null
        ? <Map<String, dynamic>>[]
        : [
            for (final id in home.pins)
              ...lists.where((p) => p['playlist_id'] == id)
          ];
    return Card(
        child: Padding(
            padding: const EdgeInsets.all(16),
            child:
                Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text('Pinned playlists',
                  style: Theme.of(context).textTheme.titleMedium),
              if (home.pins.isEmpty)
                const Text('Choose playlists in Customize home.'),
              if (lists == null) ...[
                Text(_playlistFailed
                    ? 'Could not load playlists. Check your connection.'
                    : 'Loading playlists…'),
                if (_playlistFailed)
                  TextButton(
                      onPressed: () => _loadPlaylists(_scope!),
                      child: const Text('Retry playlists')),
              ],
              for (final playlist in accessible) ...[
                ListTile(
                    contentPadding: EdgeInsets.zero,
                    title: Text('${playlist['name']}'),
                    trailing: const Icon(Icons.chevron_right),
                    onTap: () => context.push(
                        '${AppRoutes.musicPlaylists}?playlist=${Uri.encodeComponent(playlist['playlist_id'] as String)}')),
                MusicCollectionActions(
                    key: ValueKey('pin-${playlist['playlist_id']}'),
                    collections: [
                      MusicCollection(
                          'playlist', playlist['playlist_id'] as String)
                    ],
                    playLabel: 'Play playlist',
                    sourceLabel: playlist['name'] as String,
                    enabled: (playlist['song_count'] as num? ?? 0) > 0),
                TextButton(
                    onPressed: () => _update(
                        () => home.pins.remove(playlist['playlist_id'])),
                    child: Text('Unpin ${playlist['name']}')),
              ],
              if (lists != null && accessible.length < home.pins.length)
                TextButton(
                    onPressed: () => _update(() => home.pins = accessible
                        .map((p) => p['playlist_id'] as String)
                        .toList()),
                    child: Text(
                        'Remove ${home.pins.length - accessible.length} unavailable pins')),
            ])));
  }

  @override
  Widget build(BuildContext context) {
    final user = ref.watch(authStateProvider).valueOrNull?.user;
    final origin = ref.watch(playerBaseUrlProvider).origin;
    ref.watch(apiClientProvider);
    final scope = 'player.music-home.v1:$origin:${user?.id}';
    if (_scope != scope) {
      _scope = scope;
      _home = null;
      _playlists = null;
      _playlistFailed = false;
      Future.microtask(() => _load(scope));
    }
    // Watching transport also disposes obsolete results after a credential switch.
    if (_home == null) {
      return const Center(
          child:
              CircularProgressIndicator(semanticsLabel: 'Loading music home'));
    }
    return RefreshIndicator(
        onRefresh: () async {
          await _loadPlaylists(scope);
          await widget.onRefresh();
        },
        child: ListView(padding: const EdgeInsets.all(16), children: [
          Text('Music home', style: Theme.of(context).textTheme.titleLarge),
          if (widget.filtered)
            const Text(
                'From albums matching your current filters. Playlist pins use all readable playlists.'),
          Wrap(children: [
            TextButton(
                onPressed: widget.onBrowse,
                child: const Text('View all albums')),
            TextButton(
                onPressed: _customize, child: const Text('Customize home'))
          ]),
          if (_home!.order.every(_home!.hidden.contains))
            const Text(
                'All home collections are hidden. Use Customize home to show them or restore defaults.'),
          for (final key in _home!.order)
            if (!_home!.hidden.contains(key))
              key == 'pinned'
                  ? _pinned()
                  : _section(context, key, albumCollections[key]!),
        ]));
  }

  Widget _section(BuildContext context, String key, List<String> copy) {
    final items = widget.collections[key] ?? [];
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
                    onTap: () =>
                        widget.onOpenAlbum(album['album_key'] as String)),
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
                  onPressed: () => widget.onViewAll(key, items),
                  child: Text('View all ${copy[0].toLowerCase()}')),
            ])));
  }
}
