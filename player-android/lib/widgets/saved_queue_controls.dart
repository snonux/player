import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/audio_handler_provider.dart';
import '../providers/global_sync_provider.dart';
import '../providers/music_queue_persistence_provider.dart';
import '../providers/playback_session_provider.dart';
import '../providers/saved_queue_provider.dart';
import '../services/saved_queue_sync.dart';

/// Native actions in the existing queue sheet; responses never control playback.
class SavedQueueControls extends ConsumerStatefulWidget {
  const SavedQueueControls({super.key});
  @override
  ConsumerState<SavedQueueControls> createState() => _SavedQueueControlsState();
}

class _SavedQueueControlsState extends ConsumerState<SavedQueueControls> {
  bool _busy = false;
  String? _error;
  Future<bool> _confirm(String title, String detail) async =>
      await showDialog<bool>(
          context: context,
          builder: (context) =>
              AlertDialog(title: Text(title), content: Text(detail), actions: [
                TextButton(
                    onPressed: () => Navigator.pop(context, false),
                    child: const Text('Keep this queue')),
                FilledButton(
                    onPressed: () => Navigator.pop(context, true),
                    child: const Text('Replace')),
              ])) ??
      false;
  Future<void> _act(Future<void> Function() action) async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await action();
    } catch (_) {
      if (mounted) {
        setState(() =>
            _error = 'Session unavailable. Check your connection and retry.');
      }
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final sync = ref.watch(savedQueueSyncProvider);
    final account = ref.watch(savedQueueAccountProvider);
    if (account == null) return const SizedBox.shrink();
    return Padding(
        padding: const EdgeInsets.symmetric(horizontal: 16),
        child: Column(children: [
          Wrap(alignment: WrapAlignment.center, children: [
            TextButton(
                onPressed: _busy
                    ? null
                    : () => _act(() async {
                          final handler = ref.read(audioHandlerProvider);
                          if (handler.musicQueue.entries.isNotEmpty &&
                              !await _confirm('Continue saved session?',
                                  'Replace this queue with the saved session. Playback stays paused.')) {
                            return;
                          }
                          if (!account.current()) return;
                          final coordinator =
                              ref.read(playbackSessionCoordinatorProvider);
                          final originalQueue =
                              portableQueueStructure(handler.queueSnapshot);
                          final intent = coordinator.generation,
                              recovery = handler.queueRecoveryGeneration;
                          final remote = await sync.refresh(account);
                          if (!mounted ||
                              !account.current() ||
                              remote == null ||
                              intent != coordinator.generation ||
                              recovery != handler.queueRecoveryGeneration ||
                              portableQueueStructure(handler.queueSnapshot) !=
                                  originalQueue) {
                            return;
                          }
                          final snapshot = portableQueueSnapshot(remote);
                          final editStamp = sync.editStamp(account.scope);
                          final restored = await ref
                              .read(musicQueuePersistenceProvider)
                              .restorePortable(snapshot);
                          if (restored != null && account.current()) {
                            await sync.adopt(account, remote, restored.snapshot,
                                expectedEditStamp: editStamp,
                                unchanged: restored.unchanged);
                          }
                        }),
                child: const Text('Continue session')),
            TextButton(
                onPressed: _busy
                    ? null
                    : () => _act(() async {
                          if (!await _confirm('Replace saved session?',
                              'Save this queue for your other clients. Their current playback keeps playing.')) {
                            return;
                          }
                          if (!account.current()) return;
                          await sync.replace(account,
                              ref.read(audioHandlerProvider).queueSnapshot);
                          if (account.current()) {
                            await ref
                                .read(globalSyncCoordinatorProvider)
                                .syncNow();
                          }
                        }),
                child: const Text('Replace saved session')),
            if (sync.conflict(account.scope))
              TextButton(
                  onPressed:
                      _busy ? null : () => _act(() => sync.keepOther(account)),
                  child: const Text('Keep other session')),
          ]),
          if (_busy) const LinearProgressIndicator(),
          if (_error != null || sync.scope == account.scope)
            Text(_error ?? sync.message,
                semanticsLabel: _error ?? sync.message),
        ]));
  }
}
