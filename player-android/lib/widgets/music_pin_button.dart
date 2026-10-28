import '../services/download_quality.dart';
import '../services/music_action_intent.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:just_audio/just_audio.dart';

import '../providers/api_client_provider.dart';
import '../providers/auth_state_provider.dart';
import '../providers/audio_handler_provider.dart';
import '../providers/offline_downloads_provider.dart';
import '../providers/playback_session_provider.dart';
import '../services/music_collections.dart';
import '../services/music_queue.dart';
import '../services/offline_collection_pins.dart';

/// Pinning is separate from ZIP export and disposable streaming cache.
class MusicPinButton extends ConsumerWidget {
  const MusicPinButton(
      {super.key,
      required this.collection,
      required this.title,
      this.enabled = true});
  final MusicCollection collection;
  final String title;
  final bool enabled;

  @override
  Widget build(BuildContext context, WidgetRef ref) => OutlinedButton.icon(
      onPressed: enabled
          ? () => showMusicPinDialog(context, collection: collection, title: title)
          : null,
      icon: const Icon(Icons.push_pin_outlined),
      label: const Text('Pin offline…'));
}

/// Opens the pin/manage offline dialog used by the collection More menu.
Future<void> showMusicPinDialog(BuildContext context,
        {required MusicCollection collection, required String title}) =>
    showDialog<void>(
        context: context,
        builder: (_) => _PinDialog(collection: collection, title: title));

class _PinDialog extends ConsumerStatefulWidget {
  const _PinDialog(
      {required this.collection,
      required this.title,
      this.origin,
      this.userId});
  final String? origin;
  final int? userId;
  final MusicCollection collection;
  final String title;
  @override
  ConsumerState<_PinDialog> createState() => _PinDialogState();
}

class _PinDialogState extends ConsumerState<_PinDialog> {
  OfflineCollectionPin? _pin;
  bool _busy = true;
  bool _saving = false;
  String _message = 'Loading saved collection…';
  late final OfflineCollectionPins _pins;
  late final String _origin;
  late final int _user;
  late final int _epoch;

  @override
  void initState() {
    super.initState();
    _pins = OfflineCollectionPins(ref.read(offlineDownloadRepositoryProvider));
    _origin = widget.origin ?? ref.read(playerBaseUrlProvider).origin;
    _user =
        widget.userId ?? ref.read(authStateProvider).valueOrNull?.user?.id ?? 0;
    _epoch = ref.read(credentialMutationQueueProvider).generation;
    _reload();
  }

  bool _current() =>
      mounted &&
      ref.read(credentialMutationQueueProvider).generation == _epoch &&
      (widget.origin != null ||
          ref.read(playerBaseUrlProvider).origin == _origin);

  Future<void> _reload() async {
    try {
      final pin = await _pins.status(_origin, _user, widget.collection);
      if (_current()) {
        setState(() {
          _pin = pin;
          _message = '';
        });
      }
    } catch (_) {
      if (mounted) {
        setState(() => _message =
            'Could not load saved downloads. Check device storage and retry.');
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _save() async {
    setState(() {
      _busy = true;
      _message = 'Loading songs…';
    });
    final downloads = ref.read(offlineDownloadsProvider);
    final server = ref.read(apiClientProvider).rawDio.options.baseUrl;
    try {
      final profile = await DownloadQuality.selected(server);
      if (!_current()) return;
      if (downloads.active != null || downloads.batch?.running == true) {
        throw StateError('Another download is running.');
      }
      final songs = await resolveMusicCollections(
          ref.read(apiClientProvider), [widget.collection], _current);
      if (!_current()) return;
      await _pins.remember(
          _origin, _user, widget.collection, widget.title, songs, _current);
      if (!_current()) return;
      setState(() => _message = 'Saving collection… Keep Player open.');
      final save =
          mediaOfflineSaver(ref, batchMember: true, downloadProfile: profile);
      setState(() => _saving = true);
      await downloads.downloadBatch(songs, save, _current);
      await _reload();
    } catch (_) {
      if (mounted) {
        setState(() {
          _busy = false;
          _message =
              'Could not finish pinning. Check connection, access and storage, then retry. Completed downloads remain available.';
        });
      }
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  Future<void> _play() async {
    setState(() {
      _busy = true;
      _message = 'Opening downloaded music…';
    });
    try {
      final handler = ref.read(audioHandlerProvider);
      final coordinator = ref.read(playbackSessionCoordinatorProvider);
      final operation = beginMusicActionIntent();
      final sourceGeneration = coordinator.generation;
      final requests = await _pins.playback(_origin, _user, widget.collection);
      if (!mounted ||
          !_current() ||
          !isMusicActionIntent(operation) ||
          coordinator.generation != sourceGeneration ||
          !(ModalRoute.of(context)?.isCurrent ?? true)) {
        return;
      }
      int? claimedGeneration;
      final added = await handler.insertQueue(
          entries: requests,
          action: QueueInsertion.now,
          replace: true,
          sourceLabel: 'Downloaded ${widget.title}',
          claim: (request) async {
            final lease = await coordinator.claim(
                kind: request.kind,
                identity: request.identity,
                sourceUri: request.sourceUri.toString(),
                stop: handler.stop);
            claimedGeneration = lease?.generation;
            return lease;
          },
          load: (request, current) async =>
              current() ? AudioSource.uri(request.sourceUri) : null);
      if (!_current() ||
          !isMusicActionIntent(operation) ||
          (claimedGeneration != null &&
              coordinator.generation != claimedGeneration)) {
        return;
      }
      if (!added || handler.musicState?.error != null) {
        throw StateError('Could not play the downloaded track.');
      }
      if (mounted) Navigator.pop(context);
    } catch (_) {
      if (mounted) {
        setState(() => _message =
            'Could not play downloads. Check that saved files are complete and retry.');
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final downloads = ref.watch(offlineDownloadsProvider);
    final batch = _saving ? downloads.batch : null;
    final canSave = _user > 0 &&
        ref.watch(authStateProvider).valueOrNull?.user?.id == _user &&
        ref.watch(playerBaseUrlProvider).origin == _origin &&
        ref.read(credentialMutationQueueProvider).credentialsEnabled;
    return AlertDialog(
      title: Text(widget.title, maxLines: 3, overflow: TextOverflow.ellipsis),
      content: SizedBox(
          width: 420,
          child: SingleChildScrollView(
              child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                Text(_pin == null
                    ? 'Not pinned'
                    : '${_pin!.pinned ? 'Pinned' : 'Unpinned'} · ${_pin!.saved.length} of ${_pin!.mediaIds.length} songs downloaded'),
                const SizedBox(height: 12),
                const Text(
                    'New copies use Download quality in Settings, independently of streaming quality. Existing downloads keep their saved quality.'),
                if (_pin?.saved.isNotEmpty == true)
                  Text(
                      'Saved quality: ${_pin!.saved.map((item) => '${item.downloadQualityLabel}${item.contentType.isEmpty ? '' : ' (${item.contentType})'}').toSet().join(', ')}'),
                const Text(
                    'Pinned downloads are separate from disposable playback cache. Unpin keeps saved files. Remove download in Downloads removes a local copy; server music stays available.'),
                if (!canSave)
                  const Text(
                      'Sign in to this collection’s server account to pin or refresh. Saved complete downloads can still play.'),
                if (_pin != null && !_pin!.complete)
                  const Text(
                      'Some songs are missing. Retry pinning to finish the collection.'),
                if (_busy && batch?.running == true)
                  Text(
                      '${batch!.completed} of ${batch.songs.length} saved · ${batch.failed.length} failed'),
                if (_message.isNotEmpty)
                  Semantics(liveRegion: true, child: Text(_message)),
              ]))),
      actions: [
        TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('Close')),
        if (_busy && batch?.running == true)
          TextButton(
              onPressed: downloads.cancel,
              child: const Text('Cancel downloads')),
        if (!_busy && _pin?.pinned == true)
          TextButton(
              onPressed: () async {
                setState(() => _busy = true);
                try {
                  await _pins.unpin(_origin, _user, widget.collection);
                  await _reload();
                } catch (_) {
                  if (mounted) {
                    setState(() {
                      _busy = false;
                      _message = 'Could not unpin. Retry.';
                    });
                  }
                }
              },
              child: const Text('Unpin')),
        if (!_busy && _pin?.complete == true)
          TextButton(onPressed: _play, child: const Text('Play downloaded')),
        FilledButton(
            onPressed: _busy || !_current() || !canSave ? null : _save,
            child: Text(_pin == null || !_pin!.pinned
                ? 'Pin offline'
                : _pin!.complete
                    ? 'Refresh pin'
                    : 'Retry pinning')),
      ],
    );
  }
}

/// Saved snapshots remain reachable from Downloads without a network session.
class OfflinePinnedCollections extends ConsumerStatefulWidget {
  const OfflinePinnedCollections({super.key});
  @override
  ConsumerState<OfflinePinnedCollections> createState() =>
      _PinnedCollectionsState();
}

class _PinnedCollectionsState extends ConsumerState<OfflinePinnedCollections> {
  int _revision = 0;
  @override
  Widget build(BuildContext context) {
    ref.watch(offlineDownloadsProvider.select((d) => d.libraryRevision));
    final service =
        OfflineCollectionPins(ref.read(offlineDownloadRepositoryProvider));
    return FutureBuilder<List<Map<String, Object?>>>(
      key: ValueKey(_revision),
      future: service.collections(),
      builder: (context, snapshot) {
        if (snapshot.hasError) {
          return const ListTile(
              title: Text(
                  'Saved collections unavailable. Reopen Downloads to retry.'));
        }
        final rows = snapshot.data ?? const [];
        if (rows.isEmpty) return const SizedBox.shrink();
        return ExpansionTile(
            title: const Text('Saved music collections'),
            children: [
              SizedBox(
                  height: 240,
                  child: ListView(children: [
                    for (final row in rows)
                      ListTile(
                        leading: Icon(row['pinned'] == 1
                            ? Icons.push_pin
                            : Icons.download_done),
                        title: Text(row['title'] as String,
                            maxLines: 2, overflow: TextOverflow.ellipsis),
                        subtitle: Text(
                            '${row['pinned'] == 1 ? 'Pinned' : 'Unpinned'} · ${row['origin']} · Account ${row['user_id']}'),
                        trailing: const Icon(Icons.chevron_right),
                        onTap: () async {
                          await showDialog<void>(
                              context: context,
                              builder: (_) => _PinDialog(
                                  collection: MusicCollection(
                                      row['kind'] as String,
                                      row['collection_key'] as String),
                                  title: row['title'] as String,
                                  origin: row['origin'] as String,
                                  userId: row['user_id'] as int));
                          if (mounted) setState(() => _revision++);
                        },
                      ),
                  ]))
            ]);
      },
    );
  }
}
