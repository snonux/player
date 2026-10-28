import 'dart:async';

import '../l10n/music_localizations.dart';
import 'queue_playlist_dialog.dart';
import 'package:flutter/semantics.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/audio_handler_provider.dart';
import '../providers/cast_provider.dart';
import '../services/audio_handler.dart';
import '../services/cast/cast_controller.dart';
import '../services/music_queue.dart';
import '../services/playback_request.dart';
import 'package:go_router/go_router.dart';
import '../app_routes.dart';
import 'music_autoplay_control.dart';
import 'overflow_menu.dart';
import 'saved_queue_controls.dart';

/// Shared entry point on the player and library screens; no new player route.
///
/// While a DLNA renderer holds a cast queue, opens that same sheet idiom
/// backed by [CastController.rendererQueue] instead of a separate cast UI.
class MusicQueueButton extends ConsumerWidget {
  const MusicQueueButton({super.key, this.offlineOnly = false});
  final bool offlineOnly;
  @override
  Widget build(BuildContext context, WidgetRef ref) => IconButton(
      key: const Key('music_queue_button'),
      tooltip: context.tr('Now Playing queue'),
      icon: const Icon(Icons.queue_music),
      onPressed: () {
        final cast = ref.read(castControllerProvider);
        if (cast.active?.device.isRenderer == true &&
            cast.rendererQueue.isNotEmpty) {
          showRendererQueueSheet(context, cast);
          return;
        }
        showModalBottomSheet<void>(
            context: context,
            isScrollControlled: true,
            useSafeArea: true,
            builder: (_) => _MusicQueueSheet(ref.read(audioHandlerProvider),
                offlineOnly: offlineOnly));
      });
}

/// Same Now Playing sheet chrome as the local queue, for a renderer cast.
void showRendererQueueSheet(BuildContext context, CastController controller) {
  showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    useSafeArea: true,
    builder: (_) => _RendererQueueSheet(controller),
  );
}

class _RendererQueueSheet extends StatelessWidget {
  const _RendererQueueSheet(this.controller);

  final CastController controller;

  @override
  Widget build(BuildContext context) => SizedBox(
      height: MediaQuery.sizeOf(context).height * .92,
      child: ListenableBuilder(
          listenable: controller,
          builder: (context, _) {
            final queue = controller.rendererQueue;
            final index = controller.rendererIndex;
            return CustomScrollView(slivers: [
              SliverToBoxAdapter(
                  child: Padding(
                      padding: const EdgeInsets.all(16),
                      child: Row(children: [
                        Expanded(
                            child: Text(
                                queue.isEmpty
                                    ? context.tr('Now Playing')
                                    : '${context.tr('Now Playing')} · ${index + 1} of ${queue.length}',
                                key: const Key('renderer_queue_title'),
                                style:
                                    Theme.of(context).textTheme.titleLarge)),
                        IconButton(
                            tooltip: context.tr('Close queue'),
                            color: Theme.of(context).colorScheme.onSurface,
                            onPressed: () => Navigator.pop(context),
                            icon: const Icon(Icons.close))
                      ]))),
              if (queue.isEmpty)
                SliverToBoxAdapter(
                    child: Center(
                        child: Padding(
                            padding: const EdgeInsets.all(24),
                            child: Text(context.tr(
                                'Queue is empty. Add songs or play an album to start.')))))
              else
                SliverList.builder(
                    itemCount: queue.length,
                    itemBuilder: (context, i) {
                      final request = queue[i];
                      return ListTile(
                          key: Key('renderer_queue_item_$i'),
                          selected: i == index,
                          leading: Text('${i + 1}'),
                          title: Text(request.title,
                              maxLines: 3, overflow: TextOverflow.ellipsis),
                          subtitle: Text(
                              i == index ? 'Now' : request.subtitle,
                              maxLines: 2,
                              overflow: TextOverflow.ellipsis),
                          onTap: () {
                            Navigator.pop(context);
                            unawaited(
                                controller.moveRendererQueue(i - index));
                          });
                    }),
            ]);
          }));
}

class _MusicQueueSheet extends ConsumerWidget {
  const _MusicQueueSheet(this.handler, {this.offlineOnly = false});
  final bool offlineOnly;
  final PlayerAudioHandler handler;
  Future<void> _run(
      BuildContext context, Future<void> Function() action) async {
    try {
      await action();
    } catch (_) {
      if (context.mounted) {
        ScaffoldMessenger.of(context).showSnackBar(SnackBar(
            content: Text(context.tr(
                'Could not play track. Check your connection and try again.'))));
      }
    }
  }

  bool _shuffleBusy(MusicQueueState q) =>
      q.busy || q.restoreFailed || (!q.shuffle && q.shuffleEligible < 2);

  List<Widget> _queueMoreItems(
      BuildContext context, WidgetRef ref, MusicQueueState q) {
    final shuffleLocked =
        q.busy || q.restoreFailed || q.shuffleEligible < 2;
    return [
      if (!offlineOnly) ...savedQueueMenuItems(context, ref),
      if (!offlineOnly)
        saveQueuePlaylistMenuItem(context, ref, q,
            label: context.tr('Save as playlist')),
      if (q.shuffle)
        OverflowMenuItem(
            icon: Icons.shuffle,
            label: context.tr('Reshuffle remaining'),
            onSelected: shuffleLocked
                ? null
                : () => handler.shuffleQueue(reshuffle: true)),
      if (q.index >= 0 &&
          q.entries[q.index].request is ServerPlaybackRequest)
        OverflowMenuItem(
            icon: Icons.open_in_new,
            label: context.tr('Open current item'),
            onSelected: () {
              final request =
                  q.entries[q.index].request as ServerPlaybackRequest;
              Navigator.pop(context);
              context.push(AppRoutes.mediaDetailPath(request.mediaId));
            }),
      OverflowMenuItem(
          icon: Icons.clear_all,
          label: context.tr('Clear queue'),
          onSelected: q.busy || q.entries.isEmpty
              ? null
              : () => _run(context, handler.clearQueue)),
      OverflowMenuItem(
          icon: Icons.undo,
          label: context.tr('Undo'),
          onSelected: q.busy || !q.canUndo ? null : handler.undoQueue),
    ];
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) => SizedBox(
      height: MediaQuery.sizeOf(context).height * .92,
      child: StreamBuilder<void>(
          stream: handler.queueChanges,
          builder: (context, _) {
            final q = handler.musicQueue;
            final shuffleDisabled = _shuffleBusy(q);
            return CustomScrollView(slivers: [
              SliverToBoxAdapter(
                  child: Column(children: [
                Padding(
                    padding: const EdgeInsets.all(16),
                    child: Row(children: [
                      Expanded(
                          child: Text(context.tr('Now Playing'),
                              style: Theme.of(context).textTheme.titleLarge)),
                      IconButton(
                          tooltip: context.tr('Close queue'),
                          color: Theme.of(context).colorScheme.onSurface,
                          onPressed: () => Navigator.pop(context),
                          icon: const Icon(Icons.close))
                    ])),
                if (!offlineOnly) const SavedQueueControls(),
                if (q.label.isNotEmpty)
                  Padding(
                      padding: const EdgeInsets.symmetric(horizontal: 16),
                      child: Text(context
                          .tr('Playing from: {name}', {'name': q.label}))),
                Wrap(
                    alignment: WrapAlignment.center,
                    crossAxisAlignment: WrapCrossAlignment.center,
                    children: [
                      PopupMenuButton<String>(
                          tooltip: context.tr('Shuffle options'),
                          enabled: !q.busy &&
                              !q.restoreFailed &&
                              (q.shuffle || q.shuffleEligible >= 2),
                          onSelected: (action) {
                            if (action == 'toggle') {
                              handler.shuffleQueue();
                            } else if (action == 'reshuffle') {
                              handler.shuffleQueue(reshuffle: true);
                            } else {
                              handler.shuffleQueue(mode: action);
                            }
                          },
                          itemBuilder: (_) => [
                                PopupMenuItem(
                                    value: 'toggle',
                                    enabled: !shuffleDisabled || q.shuffle,
                                    child: Text(
                                        '${context.tr('Shuffle')}: ${context.tr(q.shuffle ? (q.shuffleMode == 'albums' ? 'Albums' : 'Tracks') : 'Off')} · Toggle')),
                                PopupMenuItem(
                                    value: 'tracks',
                                    enabled: !shuffleDisabled,
                                    child:
                                        Text(context.tr('Shuffle tracks'))),
                                if (!offlineOnly)
                                  PopupMenuItem(
                                      value: 'albums',
                                      enabled: !shuffleDisabled,
                                      child: Text(
                                          context.tr('Shuffle albums'))),
                                if (q.shuffle)
                                  PopupMenuItem(
                                      value: 'reshuffle',
                                      enabled: !shuffleDisabled,
                                      child: Text(
                                          context.tr('Reshuffle remaining'))),
                              ],
                          child: Semantics(
                              selected: q.shuffle,
                              child: Padding(
                                  padding: const EdgeInsets.symmetric(
                                      horizontal: 8, vertical: 12),
                                  child: FittedBox(
                                      fit: BoxFit.scaleDown,
                                      child: Row(
                                          mainAxisSize: MainAxisSize.min,
                                          children: [
                                            Icon(Icons.shuffle,
                                                color: q.shuffle
                                                    ? Theme.of(context)
                                                        .colorScheme
                                                        .primary
                                                    : Theme.of(context)
                                                        .colorScheme
                                                        .onSurfaceVariant),
                                            const SizedBox(width: 8),
                                            Text(
                                                context.tr('Shuffle: {mode}', {
                                                  'mode': context.tr(q.shuffle
                                                      ? (q.shuffleMode ==
                                                              'albums'
                                                          ? context
                                                              .tr('Albums')
                                                          : context
                                                              .tr('Tracks'))
                                                      : context.tr('Off'))
                                                }),
                                                style: TextStyle(
                                                    color: q.shuffle
                                                        ? Theme.of(context)
                                                            .colorScheme
                                                            .primary
                                                        : Theme.of(context)
                                                            .colorScheme
                                                            .onSurfaceVariant)),
                                          ]))))),
                      Semantics(
                          selected: q.repeat != MusicRepeat.off,
                          child: TextButton.icon(
                              style: TextButton.styleFrom(
                                  minimumSize: const Size(48, 48),
                                  foregroundColor: q.repeat == MusicRepeat.off
                                      ? Theme.of(context)
                                          .colorScheme
                                          .onSurfaceVariant
                                      : Theme.of(context).colorScheme.primary),
                              onPressed: q.busy || q.restoreFailed
                                  ? null
                                  : handler.cycleQueueRepeat,
                              icon: Icon(q.repeat == MusicRepeat.one
                                  ? Icons.repeat_one
                                  : Icons.repeat),
                              label: Text(context.tr('Repeat: {mode}', {
                                'mode': context.tr(
                                    q.repeat.name[0].toUpperCase() +
                                        q.repeat.name.substring(1))
                              })))),
                      OverflowMenuButton(
                          key: const Key('queue_more_button'),
                          items: (_) => _queueMoreItems(context, ref, q)),
                    ]),
                MusicAutoplayControl(
                    enabled: q.autoplayEnabled,
                    available: !offlineOnly && q.autoplayAvailable,
                    status: q.autoplayStatus,
                    onChanged: handler.setQueueAutoplay),
                Padding(
                    padding: const EdgeInsets.symmetric(horizontal: 16),
                    child: Text(
                        'Upcoming songs only; Play next stays first. Albums keep disc/track order.${q.shuffleEligible < 2 ? ' At least two upcoming songs outside Play next are needed to shuffle.' : ''}')),
                if (q.entries.isNotEmpty || q.status.isNotEmpty)
                  Padding(
                      padding: const EdgeInsets.all(12),
                      child: Text(
                          q.busy
                              ? context.tr('Loading track…')
                              : q.status.isNotEmpty
                                  ? q.status
                                  : q.active
                                      ? context.tr('Current track marked Now')
                                      : context.tr(
                                          'Queue paused. Choose a track to play.'),
                          semanticsLabel: q.status)),
                if (q.restored) ...[
                  Text(
                      'Paused at ${Duration(seconds: q.position.floor()).inMinutes}:${(q.position.floor() % 60).toString().padLeft(2, '0')}'),
                  FilledButton(
                      onPressed: q.busy
                          ? null
                          : () => _run(context, handler.resumeQueue),
                      child: Text(context.tr('Resume queue'))),
                ],
                if (q.restoreFailed)
                  TextButton(
                      onPressed: q.busy ? null : handler.retryQueueRestore,
                      child: Text(context.tr('Retry restore'))),
              ])),
              if (q.entries.isEmpty)
                SliverToBoxAdapter(
                    child: Center(
                        child: Padding(
                            padding: const EdgeInsets.all(24),
                            child: Text(context.tr(
                                'Queue is empty. Add songs or play an album to start.')))))
              else
                SliverList.builder(
                    itemCount: q.entries.length,
                    itemBuilder: (context, index) {
                      final entry = q.entries[index],
                          title = entry.request.title;
                      void move(int delta) => handler.moveQueue(index, delta);
                      void remove() =>
                          _run(context, () => handler.removeQueue(index));
                      return Semantics(
                          key: ValueKey(entry.id),
                          selected: index == q.index,
                          customSemanticsActions: q.busy
                              ? {}
                              : {
                                  if (index > 0)
                                    CustomSemanticsAction(
                                            label: context.tr('Move up')):
                                        () => move(-1),
                                  if (index + 1 < q.entries.length)
                                    CustomSemanticsAction(
                                            label: context.tr('Move down')):
                                        () => move(1),
                                  CustomSemanticsAction(
                                      label: context.tr('Remove')): remove,
                                },
                          child: ListTile(
                              selected: q.active && index == q.index,
                              leading: Text('${index + 1}'),
                              title: Text(title,
                                  maxLines: 3, overflow: TextOverflow.ellipsis),
                              subtitle: Text(
                                  index == q.index
                                      ? q.active
                                          ? 'Now'
                                          : 'Selected · paused'
                                      : entry.autoplayRelated
                                          ? context
                                              .tr('Autoplay · related music')
                                          : q.upNextIds.contains(entry.id)
                                              ? context
                                                  .tr('Up next (you added)')
                                              : entry.request.sourceName,
                                  maxLines: 2,
                                  overflow: TextOverflow.ellipsis),
                              onTap: q.busy
                                  ? null
                                  : () => _run(
                                      context, () => handler.jumpQueue(index)),
                              trailing: PopupMenuButton<String>(
                                  tooltip: 'Queue actions for $title',
                                  enabled: !q.busy,
                                  onSelected: (action) {
                                    switch (action) {
                                      case 'play':
                                        _run(context,
                                            () => handler.jumpQueue(index));
                                      case 'up':
                                        move(-1);
                                      case 'down':
                                        move(1);
                                      case 'remove':
                                        remove();
                                    }
                                  },
                                  itemBuilder: (_) => [
                                        PopupMenuItem(
                                            value: 'play',
                                            child:
                                                Text(context.tr('Play now'))),
                                        PopupMenuItem(
                                            value: 'up',
                                            enabled: index > 0,
                                            child: Text(context.tr('Move up'))),
                                        PopupMenuItem(
                                            value: 'down',
                                            enabled:
                                                index + 1 < q.entries.length,
                                            child:
                                                Text(context.tr('Move down'))),
                                        PopupMenuItem(
                                            value: 'remove',
                                            child: Text(context.tr('Remove'))),
                                      ])));
                    }),
            ]);
          }));
}
