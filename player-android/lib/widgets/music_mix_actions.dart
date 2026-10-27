import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../models/media.dart';
import '../providers/api_client_provider.dart';
import '../providers/playback_session_provider.dart';
import '../services/music_action_intent.dart';
import '../services/music_queue.dart';
import 'server_song_actions.dart';

/// A finite local-library mix; internet stations have separate navigation.
class MusicMixActions extends ConsumerStatefulWidget {
  const MusicMixActions(
      {super.key, required this.kind, required this.entityKey});
  final String kind, entityKey;
  @override
  ConsumerState<MusicMixActions> createState() => _MusicMixActionsState();
}

class _MusicMixActionsState extends ConsumerState<MusicMixActions> {
  int _minimum = 0, _departure = 0, _operation = 0;
  bool _busy = false, _routeCurrent = true;
  String? _message;
  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final current = ModalRoute.of(context)?.isCurrent ?? true;
    if (_routeCurrent && !current) _departure++;
    _routeCurrent = current;
  }

  Future<void> _start() async {
    if (_busy) return;
    final operation = ++_operation;
    final api = ref.read(apiClientProvider);
    final mutations = ref.read(credentialMutationQueueProvider);
    final epoch = mutations.generation;
    final coordinator = ref.read(playbackSessionCoordinatorProvider);
    final source = coordinator.generation;
    final intent = beginMusicActionIntent(), departure = _departure;
    final kind = widget.kind, key = widget.entityKey;
    bool ownerCurrent() =>
        mounted &&
        operation == _operation &&
        isMusicActionIntent(intent) &&
        departure == _departure &&
        kind == widget.kind &&
        key == widget.entityKey &&
        epoch == mutations.generation &&
        ref.read(apiClientProvider) == api &&
        (ModalRoute.of(context)?.isCurrent ?? true);
    bool current() => ownerCurrent() && source == coordinator.generation;
    setState(() {
      _busy = true;
      _message = 'Finding mix songs…';
    });
    try {
      final result = await api.musicMix(kind, key, minRating: _minimum);
      if (!mounted || !current()) return;
      final songs = (result['songs'] as List)
          .cast<Map<String, dynamic>>()
          .map(Media.fromJson)
          .toList();
      final title = result['title'] as String;
      if (songs.isEmpty) {
        setState(() => _message =
            'No matching songs for $title. Try a lower rating or add music tags.');
        return;
      }
      final label = '${kind == 'artist' ? 'Artist radio' : 'Mix'} · $title';
      setState(() => _message = 'Starting mix…');
      final insertion = await insertServerSongs(
          context, ref, songs, QueueInsertion.now,
          replace: true, sourceLabel: label, actionIntent: intent);
      if (!ownerCurrent()) return;
      if (insertion.playbackGeneration != null &&
          insertion.playbackGeneration != coordinator.generation) {
        return;
      }
      setState(() => _message = switch (insertion.outcome) {
            ServerSongInsertionOutcome.accepted =>
              '$label · ${songs.length} ${songs.length == 1 ? 'song' : 'songs'}',
            ServerSongInsertionOutcome.busy =>
              'Loading track… Please wait, then try again.',
            ServerSongInsertionOutcome.signedOut =>
              'Sign in to start this mix.',
            ServerSongInsertionOutcome.failed =>
              'Could not start mix. Check your connection and try again.',
            ServerSongInsertionOutcome.cancelled => null,
          });
    } catch (_) {
      if (current()) {
        setState(() => _message =
            'Could not start mix. Check your connection and library access, then try again.');
      }
    } finally {
      if (mounted && operation == _operation) {
        setState(() {
          _busy = false;
          if (_message == 'Finding mix songs…' || _message == 'Starting mix…') {
            _message = null;
          }
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    ref.listen(apiClientProvider, (previous, next) {
      if (previous != next) {
        setState(() {
          _operation++;
          _busy = false;
          _message = null;
        });
      }
    });
    return Padding(
        padding: const EdgeInsets.symmetric(vertical: 16),
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Semantics(
              header: true,
              child: Text(
                  widget.kind == 'artist' ? 'Artist radio' : 'Instant Mix',
                  style: Theme.of(context).textTheme.titleLarge)),
          Text('From this ${widget.kind} · related songs in your library'),
          const Text(
              'Shared artist credits and genres · up to 50 distinct songs'),
          DropdownButtonFormField<int>(
              initialValue: _minimum,
              decoration:
                  const InputDecoration(labelText: 'Minimum song rating'),
              items: [
                for (var n = 0; n <= 5; n++)
                  DropdownMenuItem(
                      value: n,
                      child: Text(n == 0 ? 'Any rating' : '$n+ stars'))
              ],
              onChanged: _busy
                  ? null
                  : (value) => setState(() => _minimum = value ?? 0)),
          const SizedBox(height: 8),
          FilledButton.icon(
              onPressed: _busy ? null : _start,
              icon: const Icon(Icons.auto_awesome),
              label:
                  Text(widget.kind == 'artist' ? 'Artist radio' : 'Start mix')),
          if (_message != null)
            Semantics(liveRegion: true, child: Text(_message!)),
        ]));
  }
}
