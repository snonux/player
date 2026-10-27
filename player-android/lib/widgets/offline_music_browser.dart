import '../providers/playback_cache_provider.dart';
import 'music_queue_sheet.dart';
import 'music_mini_player.dart';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:just_audio/just_audio.dart';

import '../l10n/music_localizations.dart';
import '../models/offline_media.dart';
import '../providers/audio_handler_provider.dart';
import '../providers/offline_downloads_provider.dart';
import '../providers/playback_session_provider.dart';
import '../services/music_action_intent.dart';
import '../services/music_queue.dart';
import '../services/offline_music_library.dart';
import '../services/playback_request.dart';

final offlineMusicItemsProvider =
    FutureProvider<List<OfflineMedia>>((ref) async {
  final downloads = ref.watch(offlineMediaListProvider.future);
  final cache = ref.watch(playbackCacheProvider);
  final saved = await downloads;
  final cached = await cache.offlineLibrary();
  final identities = saved
      .map((item) => jsonEncode([item.origin, item.userId, item.mediaId]))
      .toSet();
  return [
    ...saved,
    ...cached.where((item) =>
        item.mediaId == 0 ||
        !identities
            .contains(jsonEncode([item.origin, item.userId, item.mediaId])))
  ];
});

/// A device-local view: selecting a saved account never unlocks online APIs.
class OfflineMusicBrowser extends ConsumerStatefulWidget {
  const OfflineMusicBrowser({super.key});
  @override
  ConsumerState<OfflineMusicBrowser> createState() =>
      _OfflineMusicBrowserState();
}

class _OfflineMusicBrowserState extends ConsumerState<OfflineMusicBrowser> {
  String? _account;
  String _kind = 'Songs', _query = '', _message = '';
  bool _busy = false;
  int _revision = 0;
  Future<List<OfflineMusicGroup>>? _groups;
  List<OfflineMedia>? _items;

  void _reload(List<OfflineMedia> items) {
    _groups = _account == null
        ? Future.value([])
        : offlineMusicGroups(ref.read(offlineDownloadRepositoryProvider), items,
            _account!, _kind);
  }

  Future<void> _queue(List<OfflineMedia> songs, QueueInsertion action) async {
    if (_busy || songs.isEmpty) return;
    final account = _account, revision = _revision;
    final sourceLabel = context.tr('Offline music');
    final intent = beginMusicActionIntent();
    final handler = ref.read(audioHandlerProvider);
    final playbackCache = ref.read(playbackCacheProvider);
    final coordinator = ref.read(playbackSessionCoordinatorProvider);
    final generation = coordinator.generation;
    bool current() =>
        mounted &&
        _account == account &&
        revision == _revision &&
        isMusicActionIntent(intent);
    setState(() {
      _busy = true;
      _message = '';
    });
    try {
      final repository = ref.read(offlineDownloadRepositoryProvider);
      final requests = <OfflinePlaybackRequest>[];
      for (final song in songs) {
        requests.add(song.key.startsWith('cache:')
            ? await ref
                .read(playbackCacheProvider)
                .offlineRequest(song.key.substring(6), song.origin, song.userId)
            : await repository.playbackRequest(song.key));
        if (!current()) return;
      }
      if (coordinator.generation != generation) return;
      final queue = handler.musicQueue;
      final sameAccount = queue.entries.every((entry) {
        final request = entry.request;
        return request is OfflinePlaybackRequest &&
            jsonEncode([request.serverOrigin, request.userId]) == account;
      });
      if (action != QueueInsertion.now && !sameAccount) {
        throw StateError('Choose Play now to replace another account’s queue.');
      }
      if (queue.entries.isEmpty ||
          (action == QueueInsertion.now && !sameAccount)) {
        await handler.stop();
        if (!current()) return;
        handler.clearRetainedQueue();
      }
      final added = await handler.insertQueue(
          entries: requests,
          action: action,
          replace: action == QueueInsertion.now,
          sourceLabel: sourceLabel,
          claim: (request) => coordinator.claim(
              kind: request.kind,
              identity: request.identity,
              sourceUri: request.sourceUri.toString(),
              stop: handler.stop),
          load: (request, valid) async {
            if (!valid()) return null;
            final cache = playbackCache;
            cache.playingUri(null);
            if (request is OfflinePlaybackRequest &&
                request.downloadKey.startsWith('cache:')) {
              final local = await cache.offlineRequest(
                  request.downloadKey.substring(6),
                  request.serverOrigin!,
                  request.userId!,
                  protect: true);
              return valid() ? AudioSource.uri(local.sourceUri) : null;
            }
            return valid() ? AudioSource.uri(request.sourceUri) : null;
          });
      if (current()) {
        setState(() => _message = added
            ? context.tr('Added to queue')
            : context.tr('Playback is busy. Retry.'));
      }
    } catch (error) {
      if (current()) {
        setState(() => _message = error is StateError &&
                error.message.toString().startsWith('Choose Play now')
            ? context.tr('Choose Play now to replace another account’s queue.')
            : context.tr(
                'Some downloads are missing. Reconnect and download them again.'));
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  void _open(OfflineMusicGroup group) {
    showModalBottomSheet<void>(
        context: context,
        isScrollControlled: true,
        builder: (sheet) => SafeArea(
            child: FractionallySizedBox(
                heightFactor: .8,
                child: Column(children: [
                  ListTile(
                      title: Text(group.title),
                      subtitle: Text(context.tr(
                          '{saved} of {total} songs available', {
                        'saved': group.songs.length,
                        'total': group.total
                      }))),
                  if (group.songs.length < group.total)
                    Padding(
                        padding: const EdgeInsets.symmetric(horizontal: 16),
                        child: Text(context.tr(
                            'Some downloads are missing. Reconnect and download them again.'))),
                  Wrap(spacing: 8, children: [
                    for (final action in QueueInsertion.values)
                      TextButton(
                          onPressed: _busy || group.songs.isEmpty
                              ? null
                              : () {
                                  Navigator.pop(sheet);
                                  _queue(group.songs, action);
                                },
                          child: Text(context.tr(switch (action) {
                            QueueInsertion.now => 'Play now',
                            QueueInsertion.next => 'Play next',
                            QueueInsertion.append => 'Add to queue'
                          }))),
                  ]),
                  Expanded(
                      child: ListView(children: [
                    for (final song in group.songs)
                      ListTile(
                          title: Text(song.title),
                          subtitle: Text([
                            song.music?.artist ?? '',
                            context.tr(song.key.startsWith('cache:')
                                ? 'Cached (disposable)'
                                : 'Downloaded')
                          ].where((text) => text.isNotEmpty).join(' · ')),
                          trailing: const Icon(Icons.play_arrow),
                          onTap: _busy
                              ? null
                              : () {
                                  Navigator.pop(sheet);
                                  _queue([song], QueueInsertion.now);
                                })
                  ])),
                ]))));
  }

  @override
  Widget build(BuildContext context) {
    final items = ref.watch(offlineMusicItemsProvider);
    return Scaffold(
        appBar: AppBar(
            title: Text(context.tr('Offline music')),
            actions: const [MusicQueueButton(offlineOnly: true)]),
        bottomNavigationBar: const MusicMiniPlayer(),
        body: items.when(
            loading: () => const Center(child: CircularProgressIndicator()),
            error: (_, __) => Center(
                child: TextButton(
                    onPressed: () {
                      ref.invalidate(offlineMediaListProvider);
                      ref.invalidate(offlineMusicItemsProvider);
                    },
                    child:
                        Text(context.tr('Could not load downloads. Retry')))),
            data: (items) {
              final accounts = items
                  .where((s) => s.type == 'audio')
                  .map(offlineMusicAccount)
                  .toSet()
                  .toList();
              if (accounts.isEmpty) {
                return Center(
                    child: Text(context.tr('No downloaded music yet.')));
              }
              if (!accounts.contains(_account)) {
                _account = accounts.first;
                _groups = null;
              }
              if (!identical(_items, items) || _groups == null) {
                _items = items;
                _reload(items);
              }
              return Column(children: [
                Padding(
                    padding: const EdgeInsets.all(16),
                    child: Column(children: [
                      Text(context.tr(
                          'Saved on this device. Online actions need a connection and sign-in.')),
                      Text(context.tr(
                          'Cached copies are disposable and may be evicted. Downloads stay separate.')),
                      DropdownButtonFormField<String>(
                          key: ValueKey(_account),
                          initialValue: _account,
                          decoration: InputDecoration(
                              labelText:
                                  context.tr('Saved server and account')),
                          isExpanded: true,
                          items: [
                            for (final account in accounts)
                              DropdownMenuItem(
                                  value: account,
                                  child: Text(
                                      '${(jsonDecode(account) as List)[0]} · Account ${(jsonDecode(account) as List)[1]}',
                                      overflow: TextOverflow.ellipsis))
                          ],
                          onChanged: _busy
                              ? null
                              : (value) async {
                                  setState(() {
                                    _busy = true;
                                    _revision++;
                                  });
                                  final handler =
                                      ref.read(audioHandlerProvider);
                                  await handler.stop();
                                  handler.clearRetainedQueue();
                                  if (!mounted) return;
                                  setState(() {
                                    _account = value;
                                    _busy = false;
                                    _message = '';
                                    _reload(items);
                                  });
                                }),
                      DropdownButtonFormField<String>(
                          initialValue: _kind,
                          decoration:
                              InputDecoration(labelText: context.tr('Browse')),
                          items: [
                            for (final kind in [
                              'Songs',
                              'Albums',
                              'Artists',
                              'Playlists'
                            ])
                              DropdownMenuItem(
                                  value: kind, child: Text(context.tr(kind)))
                          ],
                          onChanged: (value) => setState(() {
                                _kind = value!;
                                _reload(items);
                              })),
                      TextField(
                          decoration: InputDecoration(
                              labelText: context.tr('Search downloaded music'),
                              prefixIcon: const Icon(Icons.search)),
                          onChanged: (value) => setState(() => _query = value)),
                      if (_message.isNotEmpty)
                        Semantics(liveRegion: true, child: Text(_message)),
                      if (_busy) const LinearProgressIndicator(),
                    ])),
                Expanded(
                    child: FutureBuilder<List<OfflineMusicGroup>>(
                        future: _groups,
                        builder: (context, snapshot) {
                          if (snapshot.hasError) {
                            return Center(
                                child: TextButton(
                                    onPressed: () =>
                                        setState(() => _reload(items)),
                                    child: Text(context.tr(
                                        'Could not load downloads. Retry'))));
                          }
                          if (snapshot.connectionState !=
                              ConnectionState.done) {
                            return const Center(
                                child: CircularProgressIndicator());
                          }
                          final rows = (snapshot.data ?? [])
                              .where((row) => offlineMusicMatches(row, _query))
                              .toList();
                          if (rows.isEmpty) {
                            return Center(
                                child: Text(context
                                    .tr('No downloaded music matches.')));
                          }
                          return ListView(children: [
                            for (final row in rows)
                              ListTile(
                                  leading: Icon(_kind == 'Artists'
                                      ? Icons.person_outline
                                      : _kind == 'Playlists'
                                          ? Icons.queue_music
                                          : Icons.library_music),
                                  title: Text(row.title,
                                      maxLines: 2,
                                      overflow: TextOverflow.ellipsis),
                                  subtitle: Text(context.tr(
                                      '{saved} of {total} songs available', {
                                    'saved': row.songs.length,
                                    'total': row.total
                                  })),
                                  trailing: const Icon(Icons.chevron_right),
                                  onTap: () => _open(row))
                          ]);
                        })),
              ]);
            }));
  }
}
