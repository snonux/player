import 'dart:async';
import 'package:go_router/go_router.dart';
import '../app_routes.dart';
import 'podcast_discovery_screen.dart';
import 'media_grid_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/podcast_library_provider.dart';
import '../providers/api_client_provider.dart';
import '../services/podcast_identity.dart';
import '../widgets/podcast_download_controls.dart';
import '../widgets/podcast_artwork.dart';
import '../widgets/podcast_library_switcher.dart';
import '../widgets/podcast_show_notes.dart';
import '../widgets/podcast_publication_date.dart';
import '../services/podcast_metadata.dart';
import '../providers/global_sync_provider.dart';

// Shared by all episode routes so opening a feed keeps the chosen layout.
final podcastEpisodeGridProvider = StateProvider<bool>((ref) => false);

class StandalonePodcastsScreen extends ConsumerStatefulWidget {
  const StandalonePodcastsScreen(
      {super.key, this.viewSwitcher, this.syncMessage});
  final Widget? viewSwitcher;
  final String? syncMessage;
  @override
  ConsumerState<StandalonePodcastsScreen> createState() =>
      _StandalonePodcastsScreenState();
}

class _StandalonePodcastsScreenState
    extends ConsumerState<StandalonePodcastsScreen> {
  bool busy = false;
  String? error;
  Future<void> run(Future<void> Function() action) async {
    setState(() {
      busy = true;
      error = null;
    });
    try {
      await action();
    } catch (_) {
      if (mounted) {
        setState(() => error =
            'Feed unavailable. Your subscription and cached episodes are saved on this device.');
      }
    } finally {
      if (mounted) {
        ref.invalidate(podcastSubscriptionsProvider);
        setState(() => busy = false);
      }
    }
  }

  Future<void> subscribe() async {
    final controller = TextEditingController();
    final url = await showDialog<String>(
        context: context,
        builder: (context) => AlertDialog(
                title: const Text('Subscribe to podcast'),
                content: TextField(
                    controller: controller,
                    keyboardType: TextInputType.url,
                    autocorrect: false,
                    decoration: const InputDecoration(labelText: 'Feed URL')),
                actions: [
                  TextButton(
                      onPressed: () => Navigator.pop(context),
                      child: const Text('Cancel')),
                  FilledButton(
                      onPressed: () => Navigator.pop(context, controller.text),
                      child: const Text('Subscribe'))
                ]));
    // Dispose after the dialog's route transition has released the field.
    Future<void>.delayed(const Duration(seconds: 1), controller.dispose);
    if (url == null || !mounted) return;
    try {
      podcastFeedKey(url);
    } on FormatException {
      setState(() => error =
          'Enter a valid podcast feed URL, such as https://example.com/feed.xml.');
      return;
    }
    await run(() async {
      final repository = ref.read(podcastRepositoryProvider);
      final scope = await ref.read(podcastScopeProvider.future);
      await repository.subscribe(scope, url);
    });
  }

  void _refreshFeeds() => run(() async {
        final repository = ref.read(podcastRepositoryProvider);
        final refreshScope = await ref.read(podcastScopeProvider.future);
        ref.invalidate(podcastServerSubscriptionsProvider);
        await ref.read(podcastServerSubscriptionsProvider.future);
        final currentScope = await ref.read(podcastScopeProvider.future);
        if (!mounted || currentScope != refreshScope) {
          return;
        }
        final subscriptions = await repository.subscriptions(refreshScope);
        var failed = 0;
        for (final feed in subscriptions) {
          try {
            await repository.refreshSubscription(
                refreshScope, feed['feed_key'] as String);
          } catch (_) {
            failed++;
          }
        }
        if (mounted && failed > 0) {
          setState(() => error =
              '$failed feed refreshes failed. Other feeds were refreshed; cached episodes remain available.');
        }
      });

  @override
  Widget build(BuildContext context) {
    ref.watch(podcastOverviewMetadataProvider);
    final feeds = ref.watch(podcastSubscriptionsProvider);
    final scope = ref.watch(podcastScopeProvider).valueOrNull;
    return Scaffold(
        appBar: AppBar(
            bottom: widget.viewSwitcher == null
                ? null
                : PreferredSize(
                    preferredSize: const Size.fromHeight(56),
                    child: widget.viewSwitcher!),
            title:
                Text(scope == 'local' ? 'Podcasts on this device' : 'Podcasts'),
            actions: [
              IconButton(
                  tooltip: 'Explore podcasts',
                  onPressed: busy
                      ? null
                      : () => Navigator.of(context).push(
                          MaterialPageRoute<void>(
                              builder: (_) => const PodcastDiscoveryScreen())),
                  icon: const Icon(Icons.explore_outlined)),
              PopupMenuButton<String>(
                  tooltip: 'Podcast options',
                  icon: const Icon(Icons.menu),
                  style: IconButton.styleFrom(minimumSize: const Size(48, 48)),
                  enabled: !busy,
                  onSelected: (action) {
                    if (action == 'refresh') {
                      _refreshFeeds();
                    } else if (action == 'retention') {
                      showDialog<void>(
                          context: context,
                          builder: (context) => AlertDialog(
                                title: const Text('Completed episode cleanup'),
                                content: const Text(
                                    'Completed episodes leave your lists after seven days. This device also removes their downloads when they are not playing or needed by another library. Listening history and shared server files are kept. Replaying or marking uncompleted keeps an episode. Delayed sync starts a fresh seven-day grace when received.'),
                                actions: [
                                  TextButton(
                                      onPressed: () => Navigator.pop(context),
                                      child: const Text('Close'))
                                ],
                              ));
                    } else {
                      subscribe();
                    }
                  },
                  itemBuilder: (_) => const [
                        PopupMenuItem(
                            value: 'refresh', child: Text('Refresh feeds')),
                        PopupMenuItem(
                            value: 'url', child: Text('Add feed URL')),
                        PopupMenuItem(
                            value: 'retention',
                            child: Text('Completed episode cleanup')),
                      ])
            ]),
        body: Column(children: [
          const PodcastLibrarySwitcher(),
          if (widget.syncMessage != null)
            Padding(
                padding: const EdgeInsets.all(8),
                child: Text(widget.syncMessage!)),
          if (busy) const LinearProgressIndicator(),
          if (error != null)
            Padding(padding: const EdgeInsets.all(16), child: Text(error!)),
          if (widget.viewSwitcher == null)
            ListTile(
                leading: const Icon(Icons.folder_copy_outlined),
                title: const Text('All podcast episodes'),
                onTap: () => context.push(AppRoutes.podcasts)),
          Expanded(
              child: feeds.when(
                  skipLoadingOnReload: true,
                  loading: () =>
                      const Center(child: CircularProgressIndicator()),
                  error: (_, __) => Center(
                      child: TextButton(
                          onPressed: () =>
                              ref.invalidate(podcastSubscriptionsProvider),
                          child: const Text('Retry opening podcasts'))),
                  data: (items) => items
                          .any((item) => item['browse_scope'] != scope)
                      ? const Center(child: CircularProgressIndicator())
                      : items.isEmpty
                          ? Center(
                              child: FilledButton.icon(
                                  onPressed: busy ? null : subscribe,
                                  icon: const Icon(Icons.podcasts),
                                  label: const Text('Subscribe to podcast')))
                          : ListView(children: [
                              for (final feed in items)
                                ListTile(
                                    title: Row(children: [
                                      PodcastArtwork(
                                          imageUrl:
                                              feed['image_url'] as String? ??
                                                  '',
                                          title: feed['title'] as String,
                                          size: 88),
                                      const SizedBox(width: 16),
                                      Expanded(
                                          child: Text(feed['title'] as String)),
                                    ]),
                                    subtitle: Text(
                                        feed['last_error'] as String? ??
                                            feed['fetch_url'] as String),
                                    onTap: () => context.push(Uri(
                                            path: AppRoutes.podcastFeed,
                                            queryParameters: {
                                              'feed':
                                                  feed['feed_key'] as String,
                                              'title': feed['title'] as String
                                            }).toString()),
                                    trailing: PopupMenuButton<String>(
                                        tooltip: 'Manage podcast',
                                        onSelected: (action) => run(() async {
                                              final repository = ref.read(
                                                  podcastRepositoryProvider);
                                              if (action == 'refresh') {
                                                await repository
                                                    .refreshSubscription(
                                                        feed['browse_scope']
                                                            as String,
                                                        feed['feed_key']
                                                            as String);
                                              } else {
                                                await repository.unsubscribe(
                                                    feed['browse_scope']
                                                        as String,
                                                    feed['feed_key'] as String);
                                              }
                                            }),
                                        itemBuilder: (_) => const [
                                              PopupMenuItem(
                                                  value: 'refresh',
                                                  child: Text('Refresh feed')),
                                              PopupMenuItem(
                                                  value: 'unsubscribe',
                                                  child: Text(
                                                      'Unsubscribe (keep history)'))
                                            ]))
                            ])))
        ]));
  }
}

class LocalPodcastEpisodesScreen extends ConsumerStatefulWidget {
  const LocalPodcastEpisodesScreen(
      {super.key,
      this.feedKey,
      this.title = 'All podcast episodes',
      this.serverSetId,
      this.serverSetName,
      this.viewSwitcher,
      this.syncMessage});
  final int? serverSetId;
  final String? serverSetName;
  final Widget? viewSwitcher;
  final String? syncMessage;
  final String? feedKey;
  final String title;
  @override
  ConsumerState<LocalPodcastEpisodesScreen> createState() =>
      _LocalPodcastEpisodesScreenState();
}

class _LocalPodcastEpisodesScreenState
    extends ConsumerState<LocalPodcastEpisodesScreen> {
  String filter = 'uncompleted';
  int offset = 0;
  String? error;
  final scroll = ScrollController();
  final pending = <String>{};
  bool selecting = false, bulkBusy = false;
  int selectionEpoch = 0;
  final selected = <(String, String, String)>{};
  List<Map<String, Object?>> visibleRows = [], selectionRows = [];
  (String, String, String) episodeIdentity(Map<String, Object?> e) => (
        e['browse_scope'] as String,
        e['scope_feed_key'] as String,
        e['scope_episode_key'] as String
      );

  void cancelSelection() {
    if (bulkBusy) return;
    setState(() {
      selectionEpoch++;
      selecting = false;
      selected.clear();
      selectionRows = [];
    });
    reload();
  }

  void selectEpisode(Map<String, Object?>? e) {
    if (bulkBusy) return;
    setState(() {
      if (!selecting) {
        selectionRows =
            visibleRows.map((e) => Map<String, Object?>.of(e)).toList();
      }
      selecting = true;
      if (e != null) {
        final key = episodeIdentity(e);
        if (!selected.add(key)) selected.remove(key);
      }
    });
  }

  Future<void> bulkAction(String action) async {
    if (bulkBusy || selected.isEmpty) return;
    final scope = ref.read(podcastScopeProvider).valueOrNull;
    if (scope == null) return;
    final credentials = ref.read(credentialMutationQueueProvider);
    final generation = credentials.generation;
    final epoch = selectionEpoch;
    bool current() =>
        mounted &&
        selectionEpoch == epoch &&
        credentials.generation == generation &&
        ref.read(podcastScopeProvider).valueOrNull == scope;
    final batch = selectionRows
        .where((e) => selected.contains(episodeIdentity(e)))
        .toList();
    setState(() {
      bulkBusy = true;
      error = null;
    });
    if (action == 'delete' || action == 'remove-download') {
      final confirmed = await showDialog<bool>(
          context: context,
          builder: (dialog) => AlertDialog(
                title: Text(action == 'delete'
                    ? 'Delete ${batch.length} episodes?'
                    : 'Delete ${batch.length} downloaded files?'),
                content: Text(action == 'delete'
                    ? 'Remove the selected episodes from your podcast lists and delete their downloads on this device. Listening history is kept.'
                    : 'Delete the selected downloads from this device. Episodes and listening history stay in your library.'),
                actions: [
                  TextButton(
                      onPressed: () => Navigator.pop(dialog, false),
                      child: const Text('Cancel')),
                  TextButton(
                      onPressed: () => Navigator.pop(dialog, true),
                      child: const Text('Delete'))
                ],
              ));
      if (!current()) {
        if (mounted && selectionEpoch == epoch) {
          setState(() {
            bulkBusy = false;
            selecting = false;
            selected.clear();
            selectionRows = [];
          });
        }
        return;
      }
      if (confirmed != true) {
        setState(() => bulkBusy = false);
        return;
      }
    }
    var failed = 0;
    for (final e in batch) {
      if (!current()) break;
      try {
        if (action == 'delete' || action == 'remove-download') {
          await ref.read(podcastDownloadsProvider).removeEpisodeCopy(
              e['feed_key'] as String, e['episode_key'] as String,
              current: current);
        }
        if (!current()) break;
        if (action != 'remove-download') {
          await ref.read(podcastSyncStoreProvider).saveEpisode(scope,
              e['scope_feed_key'] as String, e['scope_episode_key'] as String,
              completed: action == 'delete' ? null : action == 'completed',
              deleted: action == 'delete' ? true : null,
              enclosureUrl: e['enclosure_url'] as String);
        }
        if (!current()) break;
        selected.remove(episodeIdentity(e));
        if (action == 'delete') {
          selectionRows.remove(e);
        } else if (action != 'remove-download') {
          e['completed'] = action == 'completed' ? 1 : 0;
        }
      } catch (_) {
        failed++;
      }
    }
    if (!current()) {
      if (mounted && selectionEpoch == epoch) {
        setState(() {
          bulkBusy = false;
          selecting = false;
          selected.clear();
          selectionRows = [];
        });
      }
      return;
    }
    setState(() {
      bulkBusy = false;
      error = failed == 0
          ? null
          : '$failed episodes could not be updated. Still selected; retry the action.';
      if (selected.isEmpty) {
        selecting = false;
        selectionRows = [];
      }
    });
    reload();
  }

  bool refreshing = false;
  bool checkingMissing = false;
  int refreshDone = 0, refreshTotal = 0, refreshGeneration = 0;

  @override
  void initState() {
    super.initState();
    if (widget.feedKey == null) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) unawaited(refreshFeeds(missingOnly: true));
      });
    }
  }

  @override
  void didUpdateWidget(covariant LocalPodcastEpisodesScreen oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.feedKey != widget.feedKey) {
      selectionEpoch++;
      selecting = false;
      bulkBusy = false;
      selected.clear();
      selectionRows = [];
      offset = 0;
      error = null;
    }
  }

  @override
  void dispose() {
    refreshGeneration++;
    scroll.dispose();
    super.dispose();
  }

  void reload() => ref.invalidate(podcastEpisodePageProvider);

  Future<void> refreshFeeds({bool missingOnly = false}) async {
    if (missingOnly && (checkingMissing || refreshing)) return;
    if (missingOnly) checkingMissing = true;
    if (!missingOnly) {
      setState(() {
        refreshing = true;
        refreshDone = 0;
        refreshTotal = 0;
        error = null;
      });
    }
    final generation = ++refreshGeneration;
    try {
      final repository = ref.read(podcastRepositoryProvider);
      final scope = await ref.read(podcastScopeProvider.future);
      if (!mounted || generation != refreshGeneration) return;
      if (!missingOnly) {
        ref.invalidate(podcastServerSubscriptionsProvider);
        unawaited(ref
            .read(podcastServerSubscriptionsProvider.future)
            .then<void>((_) {}, onError: (Object _, StackTrace __) {}));
      }
      final currentScope = await ref.read(podcastScopeProvider.future);
      if (!mounted ||
          generation != refreshGeneration ||
          currentScope != scope) {
        return;
      }
      final keys = widget.feedKey != null
          ? [widget.feedKey!]
          : missingOnly
              ? await repository.missingSubscriptions(scope)
              : (await repository.subscriptions(scope))
                  .map((s) => s['feed_key'] as String)
                  .toList();
      if (!mounted || generation != refreshGeneration || keys.isEmpty) return;
      setState(() {
        refreshing = true;
        refreshDone = 0;
        refreshTotal = keys.length;
        error = null;
      });
      final metadata = ref.read(podcastServerMetadataProvider);
      var failures = 0;
      for (final key in keys) {
        if (!mounted || generation != refreshGeneration) return;
        var fallbackAttempted = false;
        final canFallback = scope.startsWith('account:') &&
            metadata != null &&
            metadata.scope == scope;
        Future<void> fallback() async {
          if (!canFallback) throw StateError('Server metadata unavailable');
          fallbackAttempted = true;
          await metadata.refresh(key, onPage: () {
            if (mounted && generation == refreshGeneration) {
              ref.invalidate(podcastSubscriptionsProvider);
              reload();
            }
          });
        }

        try {
          await repository.refreshSubscription(scope, key);
          // A valid publisher response can still omit dates or all episodes.
          // Enrich only incomplete metadata, without importing personal state.
          if (canFallback &&
              (await repository.uncachedSubscriptions(scope)).contains(key)) {
            await fallback();
          }
        } catch (_) {
          try {
            if (fallbackAttempted) rethrow;
            await fallback();
          } catch (_) {
            failures++;
          }
        }
        if (!mounted || generation != refreshGeneration) return;
        ref.invalidate(podcastSubscriptionsProvider);
        reload();
        setState(() => refreshDone++);
      }
      if (mounted && generation == refreshGeneration) {
        setState(() {
          refreshing = false;
          if (failures > 0) {
            error =
                '$failures feeds unavailable. Cached episodes remain available. Retry refresh.';
          }
        });
      }
    } catch (_) {
      if (mounted && generation == refreshGeneration) {
        setState(() {
          refreshing = false;
          error =
              'Could not refresh feeds. Cached episodes remain available. Retry refresh.';
        });
      }
    } finally {
      if (missingOnly) checkingMissing = false;
      if (mounted && generation == refreshGeneration && refreshing) {
        setState(() => refreshing = false);
      }
    }
  }

  Future<void> deleteEpisode(Map<String, Object?> episode, String scope) async {
    final credentials = ref.read(credentialMutationQueueProvider);
    final generation = credentials.generation;
    bool current() =>
        mounted &&
        credentials.generation == generation &&
        ref.read(podcastScopeProvider).valueOrNull == scope;
    final confirmed = await showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
              title: const Text('Delete episode?'),
              content: const Text(
                  'Remove this episode from your podcast lists and delete its download on this device. Listening history is kept.'),
              actions: [
                TextButton(
                    onPressed: () => Navigator.pop(context, false),
                    child: const Text('Cancel')),
                TextButton(
                    onPressed: () => Navigator.pop(context, true),
                    child: const Text('Delete episode')),
              ],
            ));
    if (confirmed != true || !current()) return;
    try {
      await ref.read(podcastDownloadsProvider).removeEpisodeCopy(
          episode['feed_key'] as String, episode['episode_key'] as String,
          current: current);
      if (!current()) return;
      await ref.read(podcastSyncStoreProvider).saveEpisode(
          scope,
          episode['scope_feed_key'] as String,
          episode['scope_episode_key'] as String,
          deleted: true,
          enclosureUrl: episode['enclosure_url'] as String);
      reload();
    } catch (_) {
      if (mounted) setState(() => error = 'Could not delete episode. Retry.');
    }
  }

  Future<void> complete(Map<String, Object?> episode, String scope) async {
    final key = '${episode['scope_feed_key']}:${episode['scope_episode_key']}';
    setState(() {
      pending.add(key);
      error = null;
    });
    try {
      await ref.read(podcastSyncStoreProvider).saveEpisode(
          scope,
          episode['scope_feed_key'] as String,
          episode['scope_episode_key'] as String,
          completed: episode['completed'] != 1,
          enclosureUrl: episode['enclosure_url'] as String);
      reload();
    } catch (_) {
      if (mounted) setState(() => error = 'Could not save completion. Retry.');
    } finally {
      if (mounted) setState(() => pending.remove(key));
    }
  }

  void page(int next) {
    if (bulkBusy) return;
    setState(() {
      offset = next;
      selectionEpoch++;
      selecting = false;
      selected.clear();
      selectionRows = [];
    });
    if (scroll.hasClients) scroll.jumpTo(0);
  }

  String _progressSummary(Map<String, Object?> e) {
    if (e['completed'] == 1) return 'Listened';
    final position = (e['position_seconds'] as num?) ?? 0;
    if (position <= 0) return 'Unplayed';
    final total = position.round().clamp(0, 24 * 3600);
    final hours = total ~/ 3600;
    final minutes = (total % 3600) ~/ 60;
    final seconds = (total % 60).toString().padLeft(2, '0');
    final clock = hours > 0
        ? '$hours:${minutes.toString().padLeft(2, '0')}:$seconds'
        : '$minutes:$seconds';
    return 'In progress · $clock';
  }

  Widget episodeTile(Map<String, Object?> e, String? scope, bool grid) {
    final completed = e['completed'] == 1;
    final colors = Theme.of(context).colorScheme;
    final tile = ListTile(
      key: ValueKey((e['feed_key'], e['episode_key'])),
      selected: selecting && selected.contains(episodeIdentity(e)),
      onLongPress: bulkBusy ? null : () => selectEpisode(e),
      onTap: selecting && !bulkBusy ? () => selectEpisode(e) : null,
      leading: selecting
          ? Checkbox(
              value: selected.contains(episodeIdentity(e)),
              semanticLabel: 'Select ${e['title']}',
              onChanged: bulkBusy ? null : (_) => selectEpisode(e),
            )
          : null,
      title: grid
          ? Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                ColorFiltered(
                  colorFilter: ColorFilter.mode(
                    e['completed'] == 1 ? Colors.grey : Colors.transparent,
                    e['completed'] == 1 ? BlendMode.saturation : BlendMode.dst,
                  ),
                  child: Opacity(
                    opacity: completed ? 0.3 : 1,
                    child: PodcastArtwork(
                      imageUrl: e['image_url'] as String? ?? '',
                      fallbackUrl: e['feed_image_url'] as String? ?? '',
                      title: e['title'] as String,
                      size: 112,
                    ),
                  ),
                ),
                const SizedBox(height: 12),
                Text(
                  e['title'] as String,
                  style: e['completed'] == 1
                      ? TextStyle(
                          color: Theme.of(context).colorScheme.onSurfaceVariant,
                        )
                      : null,
                ),
              ],
            )
          : Row(
              children: [
                ColorFiltered(
                  colorFilter: ColorFilter.mode(
                    e['completed'] == 1 ? Colors.grey : Colors.transparent,
                    e['completed'] == 1 ? BlendMode.saturation : BlendMode.dst,
                  ),
                  child: Opacity(
                    opacity: completed ? 0.3 : 1,
                    child: PodcastArtwork(
                      imageUrl: e['image_url'] as String? ?? '',
                      fallbackUrl: e['feed_image_url'] as String? ?? '',
                      title: e['title'] as String,
                      size: 88,
                    ),
                  ),
                ),
                const SizedBox(width: 16),
                Expanded(
                  child: Text(
                    e['title'] as String,
                    style: e['completed'] == 1
                        ? TextStyle(
                            color:
                                Theme.of(context).colorScheme.onSurfaceVariant,
                          )
                        : null,
                  ),
                ),
              ],
            ),
      subtitle: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (widget.feedKey == null) Text(e['podcast_title'] as String),
          PodcastPublicationDate(
            date: podcastPublicationDate(e['published_at']),
          ),
          if (completed)
            Row(
              children: [
                Icon(Icons.check_circle,
                    size: 18, color: colors.onSurfaceVariant),
                const SizedBox(width: 6),
                Text('Listened',
                    style: TextStyle(
                        color: colors.onSurfaceVariant,
                        fontWeight: FontWeight.bold)),
              ],
            )
          else
            Text(_progressSummary(e)),
          if ((e['position_seconds'] as num) > 0 &&
              (e['duration_seconds'] as num) > 0)
            LinearProgressIndicator(
              value: ((e['position_seconds'] as num) /
                      (e['duration_seconds'] as num))
                  .clamp(0.0, 1.0),
            ),
          if (!selecting)
            PodcastShowNotes(description: e['description'] as String? ?? ''),
          if (!selecting)
            PodcastDownloadControls(
              feed: e['feed_key'] as String,
              scope: e['browse_scope'] as String,
              episode: e,
              onPlaybackReturn: reload,
              onDeleteEpisode: () =>
                  deleteEpisode(e, e['browse_scope'] as String),
              completeLabel:
                  e['completed'] == 1 ? 'Mark uncompleted' : 'Mark completed',
              onToggleComplete: scope == null ||
                      pending.contains(
                        '${e['scope_feed_key']}:${e['scope_episode_key']}',
                      )
                  ? null
                  : () => complete(e, scope),
            ),
        ],
      ),
    );
    final styledTile = completed
        ? Material(
            color: colors.surfaceContainerHighest,
            child: Ink(
              decoration: BoxDecoration(
                border:
                    Border(left: BorderSide(color: colors.outline, width: 4)),
              ),
              child: tile,
            ),
          )
        : tile;
    return grid
        ? Card(
            color: completed ? colors.surfaceContainerHighest : null,
            child: Padding(
              padding: const EdgeInsets.symmetric(vertical: 8),
              child: styledTile,
            ),
          )
        : styledTile;
  }

  @override
  Widget build(BuildContext context) {
    final scope = ref.watch(podcastScopeProvider).valueOrNull;
    ref.listen(podcastScopeProvider, (previous, next) {
      if (previous?.valueOrNull != next.valueOrNull) {
        setState(() {
          offset = 0;
          error = null;
          selectionEpoch++;
          selecting = false;
          bulkBusy = false;
          selected.clear();
          selectionRows = [];
        });
        if (scroll.hasClients) scroll.jumpTo(0);
        if (widget.feedKey == null && previous?.valueOrNull != null) {
          unawaited(refreshFeeds(missingOnly: true));
        }
      }
    });
    ref.listen(podcastSubscriptionsProvider, (_, next) {
      if (widget.feedKey == null && next.hasValue && !next.isLoading) {
        unawaited(refreshFeeds(missingOnly: true));
      }
    });
    final grid = ref.watch(podcastEpisodeGridProvider);
    final episodes =
        ref.watch(podcastEpisodePageProvider((widget.feedKey, filter, offset)));
    return PopScope(
        canPop: !selecting && !bulkBusy,
        onPopInvokedWithResult: (didPop, _) {
          if (!didPop && !bulkBusy) cancelSelection();
        },
        child: Scaffold(
          appBar: AppBar(
              leading: selecting
                  ? IconButton(
                      tooltip: 'Cancel selection',
                      icon: const Icon(Icons.close),
                      onPressed: bulkBusy ? null : cancelSelection)
                  : null,
              bottom: selecting || widget.viewSwitcher == null
                  ? null
                  : PreferredSize(
                      preferredSize: const Size.fromHeight(56),
                      child: widget.viewSwitcher!),
              title: Text(
                  selecting ? '${selected.length} selected' : widget.title),
              actions: [
                if (selecting) ...[
                  IconButton(
                      tooltip: 'Select all visible episodes',
                      icon: const Icon(Icons.select_all),
                      onPressed: bulkBusy
                          ? null
                          : () => setState(() {
                                selected
                                    .addAll(selectionRows.map(episodeIdentity));
                              })),
                  PopupMenuButton<String>(
                      tooltip: 'Selected episode actions',
                      enabled: !bulkBusy && selected.isNotEmpty,
                      onSelected: bulkAction,
                      itemBuilder: (_) => const [
                            PopupMenuItem(
                                value: 'completed',
                                child: Text('Mark completed')),
                            PopupMenuItem(
                                value: 'uncompleted',
                                child: Text('Mark uncompleted')),
                            PopupMenuItem(
                                value: 'remove-download',
                                child: Text('Delete downloaded files')),
                            PopupMenuItem(
                                value: 'delete',
                                child: Text('Delete episodes')),
                          ]),
                ] else ...[
                  PopupMenuButton<String>(
                    tooltip: 'Podcast views',
                    icon: const Icon(Icons.menu),
                    onSelected: (view) {
                      if (view == 'select') {
                        selectEpisode(null);
                        return;
                      }
                      Navigator.of(context).push(MaterialPageRoute<void>(
                        builder: (_) => view == 'files'
                            ? MediaGridScreen(
                                setId: widget.serverSetId!,
                                setName: widget.serverSetName)
                            : const StandalonePodcastsScreen(),
                      ));
                    },
                    itemBuilder: (_) => [
                      const PopupMenuItem(
                          value: 'select', child: Text('Select episodes')),
                      if (widget.feedKey == null && widget.viewSwitcher == null)
                        const PopupMenuItem(
                            value: 'feeds',
                            child: Text('Subscriptions and feeds')),
                      if (widget.feedKey == null && widget.serverSetId != null)
                        const PopupMenuItem(
                            value: 'files',
                            child: Text('Downloaded server files')),
                    ],
                  ),
                  IconButton(
                      tooltip: 'Reload episodes',
                      onPressed: refreshing ? null : () => refreshFeeds(),
                      icon: const Icon(Icons.refresh))
                ]
              ]),
          body: Column(children: [
            if (bulkBusy)
              const LinearProgressIndicator(
                  semanticsLabel: 'Updating selected episodes'),
            if (!bulkBusy) const PodcastLibrarySwitcher(),
            if (widget.syncMessage != null)
              Padding(
                  padding: const EdgeInsets.all(8),
                  child: Text(widget.syncMessage!)),
            if (refreshing) const LinearProgressIndicator(),
            if (refreshing)
              Text(refreshTotal == 0
                  ? 'Updating podcasts…'
                  : 'Refreshing feeds $refreshDone of $refreshTotal'),
            if (widget.feedKey == null)
              const Padding(
                  padding: EdgeInsets.all(8), child: Text('Newest first')),
            Padding(
                padding: const EdgeInsets.all(8),
                child: Wrap(spacing: 8, children: [
                  for (final option in ['uncompleted', 'completed', 'all'])
                    ChoiceChip(
                        label: Text(
                            '${option[0].toUpperCase()}${option.substring(1)}'),
                        selected: filter == option,
                        onSelected: bulkBusy
                            ? null
                            : (_) {
                                setState(() => filter = option);
                                page(0);
                              })
                ])),
            Padding(
              padding: const EdgeInsets.all(8),
              child: SegmentedButton<bool>(
                segments: const [
                  ButtonSegment(
                      value: false,
                      icon: Icon(Icons.view_list),
                      label: Text('List')),
                  ButtonSegment(
                      value: true,
                      icon: Icon(Icons.grid_view),
                      label: Text('Grid')),
                ],
                selected: {grid},
                onSelectionChanged: (values) => ref
                    .read(podcastEpisodeGridProvider.notifier)
                    .state = values.single,
              ),
            ),
            if (error != null) Text(error!),
            Expanded(
                child: episodes.when(
              skipLoadingOnReload: true,
              loading: () => const Center(child: CircularProgressIndicator()),
              error: (_, __) => Center(
                  child: TextButton(
                      onPressed: reload,
                      child: const Text('Retry opening episodes'))),
              data: (loadedRows) {
                visibleRows = loadedRows.take(40).toList();
                final rows = selecting ? selectionRows : loadedRows;
                return rows.any((row) => row['browse_scope'] != scope)
                    ? const Center(child: CircularProgressIndicator())
                    : ListView(controller: scroll, children: [
                        if (rows.isEmpty)
                          const Padding(
                              padding: EdgeInsets.all(24),
                              child: Text(
                                  'No episodes in this filter. Refresh the feed to fetch episodes.')),
                        if (grid)
                          LayoutBuilder(builder: (context, constraints) {
                            // Flowing cards grow with text and expanded notes.
                            final columns = (constraints.maxWidth / 300)
                                .floor()
                                .clamp(1, 6);
                            final width =
                                (constraints.maxWidth - (columns - 1) * 8) /
                                    columns;
                            return Wrap(spacing: 8, runSpacing: 8, children: [
                              for (final e in rows.take(40))
                                SizedBox(
                                    width: width,
                                    child: episodeTile(e, scope, true)),
                            ]);
                          })
                        else
                          for (final e in rows.take(40))
                            episodeTile(e, scope, false),
                        Wrap(
                            alignment: WrapAlignment.center,
                            crossAxisAlignment: WrapCrossAlignment.center,
                            spacing: 8,
                            children: [
                              TextButton(
                                  onPressed: offset == 0
                                      ? null
                                      : () => page(offset - 40),
                                  child: const Text('Previous page')),
                              Text('Page ${offset ~/ 40 + 1}'),
                              TextButton(
                                  onPressed: loadedRows.length <= 40
                                      ? null
                                      : () => page(offset + 40),
                                  child: const Text('Next page')),
                            ]),
                      ]);
              },
            )),
          ]),
        ));
  }
}
