import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/audio_handler_provider.dart';
import '../providers/global_sync_provider.dart';
import '../providers/music_queue_persistence_provider.dart';
import '../providers/playback_session_provider.dart';
import '../providers/saved_queue_provider.dart';
import '../services/saved_queue_sync.dart';
import 'overflow_menu.dart';

/// Status line for the queue sheet; session actions live in [savedQueueMenuItems].
class SavedQueueControls extends ConsumerWidget {
  const SavedQueueControls({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final sync = ref.watch(savedQueueSyncProvider);
    final account = ref.watch(savedQueueAccountProvider);
    if (account == null) return const SizedBox.shrink();
    return Padding(
        padding: const EdgeInsets.symmetric(horizontal: 16),
        child: Text(sync.message, semanticsLabel: sync.message));
  }
}

/// Session Overflow rows. [host] must be the queue sheet context so dialogs
/// survive after the More sheet pops.
List<Widget> savedQueueMenuItems(BuildContext host, WidgetRef ref) {
  final sync = ref.read(savedQueueSyncProvider);
  final account = ref.read(savedQueueAccountProvider);
  if (account == null) return const [];

  Future<bool> confirm(String title, String detail) async =>
      await showDialog<bool>(
          context: host,
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

  Future<void> act(Future<void> Function() action) async {
    try {
      await action();
    } catch (_) {
      if (host.mounted) {
        ScaffoldMessenger.of(host).showSnackBar(const SnackBar(
            content: Text(
                'Session unavailable. Check your connection and retry.')));
      }
    }
  }

  return [
    OverflowMenuItem(
        icon: Icons.cloud_download_outlined,
        label: 'Continue session',
        onSelected: () => act(() async {
              final handler = ref.read(audioHandlerProvider);
              if (handler.musicQueue.entries.isNotEmpty &&
                  !await confirm('Continue saved session?',
                      'Replace this queue with the saved session. Playback stays paused.')) {
                return;
              }
              if (!account.current()) return;
              final coordinator = ref.read(playbackSessionCoordinatorProvider);
              final originalQueue =
                  portableQueueStructure(handler.queueSnapshot);
              final intent = coordinator.generation,
                  recovery = handler.queueRecoveryGeneration;
              final remote = await sync.refresh(account);
              if (!host.mounted ||
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
            })),
    OverflowMenuItem(
        icon: Icons.cloud_upload_outlined,
        label: 'Replace saved session',
        onSelected: () => act(() async {
              if (!await confirm('Replace saved session?',
                  'Save this queue for your other clients. Their current playback keeps playing.')) {
                return;
              }
              if (!account.current()) return;
              await sync.replace(
                  account, ref.read(audioHandlerProvider).queueSnapshot);
              if (account.current()) {
                await ref.read(globalSyncCoordinatorProvider).syncNow();
              }
            })),
    if (sync.conflict(account.scope))
      OverflowMenuItem(
          icon: Icons.sync_alt,
          label: 'Keep other session',
          onSelected: () => act(() => sync.keepOther(account))),
  ];
}
