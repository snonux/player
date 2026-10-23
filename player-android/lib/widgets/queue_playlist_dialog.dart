import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../app_routes.dart';
import '../api/player_api_client.dart';
import '../providers/api_client_provider.dart';
import '../providers/auth_state_provider.dart';
import '../services/music_queue.dart';
import '../services/playback_request.dart';
import '../utils/error_mappers.dart';

/// Saves a snapshot independently of playback or later queue edits.
class SaveQueuePlaylistButton extends ConsumerWidget {
  const SaveQueuePlaylistButton({super.key, required this.queue});
  final MusicQueueState queue;

  @override
  Widget build(BuildContext context, WidgetRef ref) => TextButton.icon(
      style: TextButton.styleFrom(minimumSize: const Size(48, 48)),
      icon: const Icon(Icons.playlist_add),
      label: const Text('Save as playlist'),
      onPressed: queue.busy || queue.restoreFailed || queue.entries.isEmpty
          ? null
          : () async {
              final auth = ref.read(authStateProvider).valueOrNull;
              final mutations = ref.read(credentialMutationQueueProvider);
              final origin = ref.read(playerBaseUrlProvider).origin;
              if (auth?.isAuthenticated != true ||
                  !mutations.credentialsEnabled) {
                return;
              }
              final user = auth!.user!.id, epoch = mutations.generation;
              final api = ref.read(apiClientProvider);
              final requests =
                  queue.entries.map((entry) => entry.request).toList();
              if (requests.any((request) =>
                  request is! ServerPlaybackRequest ||
                  request.userId != user ||
                  request.serverOrigin != origin)) {
                ScaffoldMessenger.of(context).showSnackBar(const SnackBar(
                    content: Text(
                        'This queue belongs to a different account. Open music for this account first.')));
                return;
              }
              bool current() =>
                  mutations.credentialsEnabled &&
                  mutations.generation == epoch &&
                  mutations.credentialOrigin == origin &&
                  identical(api, ref.read(apiClientProvider)) &&
                  ref.read(authStateProvider).valueOrNull?.user?.id == user;
              final ids = requests
                  .cast<ServerPlaybackRequest>()
                  .map((r) => r.mediaId)
                  .toList();
              final id = await showDialog<String>(
                  context: context,
                  builder: (_) => QueuePlaylistDialog(
                      api: api, mediaIds: ids, epoch: epoch, current: current));
              if (!context.mounted || id == null || !current()) return;
              final router = GoRouter.of(context);
              Navigator.of(context).pop();
              router.push(
                  '${AppRoutes.musicPlaylists}?playlist=${Uri.encodeComponent(id)}');
            });
}

class QueuePlaylistDialog extends StatefulWidget {
  const QueuePlaylistDialog(
      {super.key,
      required this.api,
      required this.mediaIds,
      required this.epoch,
      required this.current});
  final PlayerApiClient api;
  final List<int> mediaIds;
  final int epoch;
  final bool Function() current;
  @override
  State<QueuePlaylistDialog> createState() => _QueuePlaylistDialogState();
}

class _QueuePlaylistDialogState extends State<QueuePlaylistDialog> {
  final _name = TextEditingController();
  bool _busy = false;
  String? _error, _saved;
  @override
  void dispose() {
    _name.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    if (_busy) return;
    if (!widget.current()) {
      Navigator.pop(context);
      return;
    }
    final name = _name.text.trim();
    if (name.runes.isEmpty || name.runes.length > 200) {
      setState(() => _error = 'Name must be 1–200 characters.');
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final saved = await widget.api
          .createPlaylistFromQueue(name, widget.mediaIds, widget.epoch);
      if (!mounted) return;
      if (!widget.current()) {
        Navigator.pop(context);
        return;
      }
      setState(() => _saved = saved['playlist_id'] as String);
    } catch (e) {
      if (mounted) {
        if (!widget.current()) {
          Navigator.pop(context);
          return;
        }
        setState(
            () => _error = 'Could not save playlist. ${setsErrorMessage(e)}');
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
          title: const Text('Save queue as playlist'),
          content: SizedBox(
              width: 440,
              child: SingleChildScrollView(
                  child: Column(
                      mainAxisSize: MainAxisSize.min,
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: _saved != null
                          ? [
                              const Text(
                                  'Private playlist saved. Your queue keeps playing.')
                            ]
                          : [
                              TextField(
                                  controller: _name,
                                  autofocus: true,
                                  enabled: !_busy,
                                  textInputAction: TextInputAction.done,
                                  onSubmitted: (_) => _save(),
                                  decoration:
                                      const InputDecoration(labelText: 'Name')),
                              const SizedBox(height: 16),
                              Text(
                                  '${widget.mediaIds.length} songs · Private playlist. Keeps this order and repeated songs.'),
                              const Text(
                                  'Your queue keeps playing. If a request loses its connection, check Playlists before retrying to avoid a second copy.'),
                              if (_busy) const Text('Saving playlist…'),
                              if (_error != null)
                                Semantics(
                                    liveRegion: true,
                                    child: Text(_error!,
                                        style: TextStyle(
                                            color: Theme.of(context)
                                                .colorScheme
                                                .error))),
                            ]))),
          actions: [
            TextButton(
                onPressed: () => Navigator.pop(context),
                child: Text(_saved == null ? 'Cancel' : 'Close')),
            if (_saved != null)
              FilledButton(
                  onPressed: () {
                    if (widget.current()) {
                      Navigator.pop(context, _saved);
                    } else {
                      Navigator.pop(context);
                    }
                  },
                  child: const Text('Open playlist'))
            else
              FilledButton(
                  onPressed: _busy ? null : _save,
                  child: const Text('Save playlist')),
          ]);
}
