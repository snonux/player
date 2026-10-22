import 'package:flutter/semantics.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/audio_handler_provider.dart';
import '../services/audio_handler.dart';
import '../services/music_queue.dart';
import '../services/playback_request.dart';
import 'package:go_router/go_router.dart';
import '../app_routes.dart';

/// Shared entry point on the player and library screens; no new player route.
class MusicQueueButton extends ConsumerWidget {
  const MusicQueueButton({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) => IconButton(
      tooltip: 'Now Playing queue',
      icon: const Icon(Icons.queue_music),
      onPressed: () => showModalBottomSheet<void>(
          context: context,
          isScrollControlled: true,
          useSafeArea: true,
          builder: (_) => _MusicQueueSheet(ref.read(audioHandlerProvider))));
}

class _MusicQueueSheet extends StatelessWidget {
  const _MusicQueueSheet(this.handler);
  final PlayerAudioHandler handler;
  Future<void> _run(
      BuildContext context, Future<void> Function() action) async {
    try {
      await action();
    } catch (_) {
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(const SnackBar(
            content: Text(
                'Could not play track. Check your connection and try again.')));
      }
    }
  }

  @override
  Widget build(BuildContext context) => SizedBox(
      height: MediaQuery.sizeOf(context).height * .92,
      child: StreamBuilder<void>(
          stream: handler.queueChanges,
          builder: (context, _) {
            final q = handler.musicQueue;
            return Column(children: [
              Padding(
                  padding: const EdgeInsets.all(16),
                  child: Row(children: [
                    Expanded(
                        child: Text('Now Playing',
                            style: Theme.of(context).textTheme.titleLarge)),
                    IconButton(
                        tooltip: 'Close queue',
                        color: Theme.of(context).colorScheme.onSurface,
                        onPressed: () => Navigator.pop(context),
                        icon: const Icon(Icons.close))
                  ])),
              if (q.label.isNotEmpty)
                Padding(
                    padding: const EdgeInsets.symmetric(horizontal: 16),
                    child: Text('Playing from: ${q.label}')),
              Semantics(
                  selected: q.repeat != MusicRepeat.off,
                  child: TextButton.icon(
                      style: TextButton.styleFrom(
                          minimumSize: const Size(48, 48),
                          foregroundColor: q.repeat == MusicRepeat.off
                              ? Theme.of(context).colorScheme.onSurfaceVariant
                              : Theme.of(context).colorScheme.primary),
                      onPressed: q.busy || q.restoreFailed
                          ? null
                          : handler.cycleQueueRepeat,
                      icon: Icon(q.repeat == MusicRepeat.one
                          ? Icons.repeat_one
                          : Icons.repeat),
                      label: Text(
                          'Repeat: ${q.repeat.name[0].toUpperCase()}${q.repeat.name.substring(1)}'))),
              if (q.entries.isNotEmpty || q.status.isNotEmpty)
                Padding(
                    padding: const EdgeInsets.all(12),
                    child: Text(
                        q.busy
                            ? 'Loading track…'
                            : q.status.isNotEmpty
                                ? q.status
                                : q.active
                                    ? 'Current track marked Now'
                                    : 'Queue paused. Choose a track to play.',
                        semanticsLabel: q.status)),
              if (q.restored) ...[
                Text(
                    'Paused at ${Duration(seconds: q.position.floor()).inMinutes}:${(q.position.floor() % 60).toString().padLeft(2, '0')}'),
                FilledButton(
                    onPressed: q.busy
                        ? null
                        : () => _run(context, handler.resumeQueue),
                    child: const Text('Resume queue')),
              ],
              if (q.restoreFailed)
                TextButton(
                    onPressed: q.busy ? null : handler.retryQueueRestore,
                    child: const Text('Retry restore')),
              if (q.index >= 0 &&
                  q.entries[q.index].request is ServerPlaybackRequest)
                TextButton(
                    onPressed: () {
                      final request =
                          q.entries[q.index].request as ServerPlaybackRequest;
                      Navigator.pop(context);
                      context.push(AppRoutes.mediaDetailPath(request.mediaId));
                    },
                    child: const Text('Open current item')),
              Expanded(
                  child: q.entries.isEmpty
                      ? const Center(
                          child: Padding(
                              padding: EdgeInsets.all(24),
                              child: Text(
                                  'Queue is empty. Add songs or play an album to start.')))
                      : ListView.builder(
                          itemCount: q.entries.length,
                          itemBuilder: (context, index) {
                            final entry = q.entries[index],
                                title = entry.request.title;
                            void move(int delta) =>
                                handler.moveQueue(index, delta);
                            void remove() =>
                                _run(context, () => handler.removeQueue(index));
                            return Semantics(
                                key: ValueKey(entry.id),
                                selected: index == q.index,
                                customSemanticsActions: q.busy
                                    ? {}
                                    : {
                                        if (index > 0)
                                          const CustomSemanticsAction(
                                              label: 'Move up'): () => move(-1),
                                        if (index + 1 < q.entries.length)
                                          const CustomSemanticsAction(
                                                  label: 'Move down'):
                                              () => move(1),
                                        const CustomSemanticsAction(
                                            label: 'Remove'): remove,
                                      },
                                child: ListTile(
                                    selected: q.active && index == q.index,
                                    leading: Text('${index + 1}'),
                                    title: Text(title,
                                        maxLines: 3,
                                        overflow: TextOverflow.ellipsis),
                                    subtitle: Text(
                                        index == q.index
                                            ? q.active
                                                ? 'Now'
                                                : 'Selected · paused'
                                            : q.upNextIds.contains(entry.id)
                                                ? 'Up next (you added)'
                                                : entry.request.sourceName,
                                        maxLines: 2,
                                        overflow: TextOverflow.ellipsis),
                                    onTap: q.busy
                                        ? null
                                        : () => _run(context,
                                            () => handler.jumpQueue(index)),
                                    trailing: PopupMenuButton<String>(
                                        tooltip: 'Queue actions for $title',
                                        enabled: !q.busy,
                                        onSelected: (action) {
                                          switch (action) {
                                            case 'play':
                                              _run(
                                                  context,
                                                  () =>
                                                      handler.jumpQueue(index));
                                            case 'up':
                                              move(-1);
                                            case 'down':
                                              move(1);
                                            case 'remove':
                                              remove();
                                          }
                                        },
                                        itemBuilder: (_) => [
                                              const PopupMenuItem(
                                                  value: 'play',
                                                  child: Text('Play now')),
                                              PopupMenuItem(
                                                  value: 'up',
                                                  enabled: index > 0,
                                                  child: const Text('Move up')),
                                              PopupMenuItem(
                                                  value: 'down',
                                                  enabled: index + 1 <
                                                      q.entries.length,
                                                  child:
                                                      const Text('Move down')),
                                              const PopupMenuItem(
                                                  value: 'remove',
                                                  child: Text('Remove')),
                                            ])));
                          })),
              SafeArea(
                  top: false,
                  child: Padding(
                      padding: const EdgeInsets.all(12),
                      child: Wrap(spacing: 12, children: [
                        TextButton(
                            onPressed: q.busy || q.entries.isEmpty
                                ? null
                                : () => _run(context, handler.clearQueue),
                            child: const Text('Clear queue')),
                        TextButton(
                            onPressed:
                                q.busy || !q.canUndo ? null : handler.undoQueue,
                            child: const Text('Undo')),
                      ])))
            ]);
          }));
}
