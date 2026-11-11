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
import '../widgets/music_mini_player.dart';
import '../widgets/podcast_library_switcher.dart';
import '../services/podcast_metadata.dart';
import '../providers/global_sync_provider.dart';
import '../widgets/podcast_episode_delete.dart';

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
        bottomNavigationBar: const NowPlayingBar(),
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
                  onPressed: busy ? null : _explore,
                  icon: const Icon(Icons.explore_outlined)),
              PopupMenuButton<String>(
                  tooltip: 'Podcast options',
                  icon: const Icon(Icons.menu),
                  style: IconButton.styleFrom(minimumSize: const Size(48, 48)),
                  enabled: !busy,
                  onSelected: (action) {
                    if (action == 'refresh') {
                      _refreshFeeds();
                    } else if (action == 'layout') {
                      ref.read(podcastEpisodeGridProvider.notifier).state =
                          !ref.read(podcastEpisodeGridProvider);
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
                  itemBuilder: (_) => [
                        const PopupMenuItem(
                            value: 'refresh',
                            child: ListTile(
                                leading: Icon(Icons.refresh),
                                title: Text('Refresh feeds'))),
                        const PopupMenuItem(
                            value: 'url',
                            child: ListTile(
                                leading: Icon(Icons.add_link),
                                title: Text('Add feed URL'))),
                        PopupMenuItem(
                            value: 'layout',
                            child: ListTile(
                                leading: Icon(
                                    ref.read(podcastEpisodeGridProvider)
                                        ? Icons.view_list
                                        : Icons.grid_view),
                                title: Text(ref.read(podcastEpisodeGridProvider)
                                    ? 'Show as list'
                                    : 'Show as grid'))),
                        const PopupMenuItem(
                            value: 'retention',
                            child: ListTile(
                                leading: Icon(Icons.auto_delete_outlined),
                                title: Text('Completed episode cleanup'))),
                      ])
            ]),
        body: Column(children: [
          const PodcastLibrarySwitcher(),
          if (widget.syncMessage != null) PodcastNotice(widget.syncMessage!),
          if (busy) const LinearProgressIndicator(),
          if (error != null) PodcastNotice(error!, isError: true),
          if (widget.viewSwitcher == null)
            ListTile(
                leading: const Icon(Icons.folder_copy_outlined),
                title: const Text('All podcast episodes'),
                trailing: const Icon(Icons.chevron_right),
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
                  data: (items) =>
                      items.any((item) => item['browse_scope'] != scope)
                          ? const Center(child: CircularProgressIndicator())
                          : items.isEmpty
                              ? _EmptyFeeds(
                                  onExplore: busy ? null : _explore,
                                  onSubscribe: busy ? null : subscribe)
                              : ref.watch(podcastEpisodeGridProvider)
                                  ? _feedGrid(items)
                                  : ListView(children: [
                                      for (final feed in items) _feedTile(feed)
                                    ])))
        ]));
  }

  void _explore() => Navigator.of(context).push(
      MaterialPageRoute<void>(builder: (_) => const PodcastDiscoveryScreen()));

  void _openFeed(Map<String, Object?> feed) =>
      context.push(Uri(path: AppRoutes.podcastFeed, queryParameters: {
        'feed': feed['feed_key'] as String,
        'title': feed['title'] as String
      }).toString());

  /// Short source line: the feed's error, else its host name.
  String _feedSubtitle(Map<String, Object?> feed) {
    final failure = feed['last_error'] as String?;
    if (failure != null && failure.isNotEmpty) return failure;
    final url = feed['fetch_url'] as String;
    final host = Uri.tryParse(url)?.host ?? '';
    return host.isEmpty ? url : host;
  }

  Widget _manageButton(Map<String, Object?> feed) => PopupMenuButton<String>(
      tooltip: 'Manage podcast',
      onSelected: (action) => run(() async {
            final repository = ref.read(podcastRepositoryProvider);
            if (action == 'refresh') {
              await repository.refreshSubscription(
                  feed['browse_scope'] as String, feed['feed_key'] as String);
            } else {
              await repository.unsubscribe(
                  feed['browse_scope'] as String, feed['feed_key'] as String);
            }
          }),
      itemBuilder: (_) => const [
            PopupMenuItem(value: 'refresh', child: Text('Refresh feed')),
            PopupMenuItem(
                value: 'unsubscribe', child: Text('Unsubscribe (keep history)'))
          ]);

  Widget _feedTile(Map<String, Object?> feed) {
    final failed = (feed['last_error'] as String?)?.isNotEmpty == true;
    return ListTile(
        contentPadding: const EdgeInsets.fromLTRB(16, 4, 4, 4),
        leading: PodcastArtwork(
            imageUrl: feed['image_url'] as String? ?? '',
            title: feed['title'] as String,
            size: 56),
        title: Text(feed['title'] as String,
            maxLines: 2, overflow: TextOverflow.ellipsis),
        subtitle: Text(_feedSubtitle(feed),
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
            style: failed
                ? TextStyle(color: Theme.of(context).colorScheme.error)
                : null),
        onTap: () => _openFeed(feed),
        trailing: _manageButton(feed));
  }

  /// Cover grid with at least three podcasts per row on a phone.
  Widget _feedGrid(List<Map<String, Object?>> items) =>
      LayoutBuilder(builder: (context, constraints) {
        final columns = ((constraints.maxWidth - 16) / 150).floor().clamp(3, 8);
        final width = (constraints.maxWidth - 16 - (columns - 1) * 8) / columns;
        // Flowing cards grow with enlarged text instead of clipping titles.
        return SingleChildScrollView(
            padding: const EdgeInsets.all(8),
            child: Wrap(spacing: 8, runSpacing: 8, children: [
              for (final feed in items)
                SizedBox(
                    width: width,
                    child: Builder(builder: (context) {
                      final surface = Theme.of(context).colorScheme.surface;
                      return Card(
                          clipBehavior: Clip.antiAlias,
                          margin: EdgeInsets.zero,
                          child: InkWell(
                              onTap: () => _openFeed(feed),
                              child: Column(
                                  crossAxisAlignment:
                                      CrossAxisAlignment.stretch,
                                  children: [
                                    Stack(children: [
                                      AspectRatio(
                                          aspectRatio: 1,
                                          child: PodcastArtwork(
                                              imageUrl: feed['image_url']
                                                      as String? ??
                                                  '',
                                              title: feed['title'] as String,
                                              size: null)),
                                      Positioned(
                                          right: 0,
                                          top: 0,
                                          child: DecoratedBox(
                                              decoration: BoxDecoration(
                                                  color: surface.withValues(
                                                      alpha: 0.85),
                                                  shape: BoxShape.circle),
                                              child: _manageButton(feed))),
                                    ]),
                                    Padding(
                                        padding: const EdgeInsets.all(8),
                                        child: Text(feed['title'] as String,
                                            maxLines: 2,
                                            overflow: TextOverflow.ellipsis,
                                            style: Theme.of(context)
                                                .textTheme
                                                .titleSmall)),
                                  ])));
                    })),
            ]));
      });
}

class _EmptyFeeds extends StatelessWidget {
  const _EmptyFeeds({required this.onExplore, required this.onSubscribe});
  final VoidCallback? onExplore, onSubscribe;
  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Center(
        child: SingleChildScrollView(
            padding: const EdgeInsets.all(24),
            child: Column(mainAxisSize: MainAxisSize.min, children: [
              Icon(Icons.podcasts,
                  size: 64, color: theme.colorScheme.onSurfaceVariant),
              const SizedBox(height: 16),
              Text('No podcasts yet', style: theme.textTheme.titleLarge),
              const SizedBox(height: 8),
              Text('Find a show in the podcast directory or add its feed URL.',
                  textAlign: TextAlign.center,
                  style: theme.textTheme.bodyMedium),
              const SizedBox(height: 24),
              Wrap(
                  alignment: WrapAlignment.center,
                  spacing: 8,
                  runSpacing: 8,
                  children: [
                    FilledButton.icon(
                        onPressed: onExplore,
                        icon: const Icon(Icons.explore_outlined),
                        label: const Text('Explore podcasts')),
                    OutlinedButton.icon(
                        onPressed: onSubscribe,
                        icon: const Icon(Icons.add_link),
                        label: const Text('Subscribe to podcast')),
                  ]),
            ])));
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
    if (!await confirmEpisodeDelete(context) || !current()) return;
    try {
      await ref.read(podcastDownloadsProvider).deleteEpisode(
          ref.read(podcastSyncStoreProvider), scope, episode,
          current: current);
      if (current()) reload();
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

  /// Listening state in a few words: listened, time left, or length.
  String _progressSummary(Map<String, Object?> e) {
    if (e['completed'] == 1) return 'Listened';
    final position = ((e['position_seconds'] as num?) ?? 0).toDouble();
    final duration = ((e['duration_seconds'] as num?) ?? 0).toDouble();
    if (position > 0 && duration > position) {
      return 'In progress · ${_clock(duration - position)} left';
    }
    if (position > 0) return 'In progress · ${_clock(position)}';
    if (duration > 0) return 'Unplayed · ${_clock(duration)}';
    return 'Unplayed';
  }

  static String _clock(double seconds) {
    final total = seconds.round().clamp(0, 24 * 3600);
    final hours = total ~/ 3600;
    final minutes = (total % 3600) ~/ 60;
    if (hours > 0) return '$hours h ${minutes.toString().padLeft(2, '0')} min';
    if (minutes > 0) return '$minutes min';
    return '${total % 60} s';
  }

  Widget episodeTile(Map<String, Object?> e, String? scope, bool grid) {
    final completed = e['completed'] == 1;
    final theme = Theme.of(context);
    final colors = theme.colorScheme;
    final isSelected = selecting && selected.contains(episodeIdentity(e));
    final position = (e['position_seconds'] as num?) ?? 0;
    final duration = (e['duration_seconds'] as num?) ?? 0;
    final date = podcastPublicationDate(e['published_at']);
    final source = [
      if (widget.feedKey == null) e['podcast_title'] as String,
      if (date != null)
        MaterialLocalizations.of(context).formatShortDate(date.toLocal()),
    ].join(' · ');
    final artwork = ColorFiltered(
      colorFilter: ColorFilter.mode(
        completed ? Colors.grey : Colors.transparent,
        completed ? BlendMode.saturation : BlendMode.dst,
      ),
      child: Opacity(
        opacity: completed ? 0.3 : 1,
        child: PodcastArtwork(
          imageUrl: e['image_url'] as String? ?? '',
          fallbackUrl: e['feed_image_url'] as String? ?? '',
          title: e['title'] as String,
          size: grid ? null : 56,
        ),
      ),
    );
    final details = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [
        Text(
          e['title'] as String,
          maxLines: 2,
          overflow: TextOverflow.ellipsis,
          style: theme.textTheme.titleSmall
              ?.copyWith(color: completed ? colors.onSurfaceVariant : null),
        ),
        const SizedBox(height: 2),
        if (source.isNotEmpty)
          Text(source,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: theme.textTheme.bodySmall
                  ?.copyWith(color: colors.onSurfaceVariant)),
        Row(children: [
          if (completed) ...[
            Icon(Icons.check_circle, size: 16, color: colors.onSurfaceVariant),
            const SizedBox(width: 4),
          ],
          Flexible(
            child: Text(_progressSummary(e),
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: theme.textTheme.bodySmall?.copyWith(
                    color: colors.onSurfaceVariant,
                    fontWeight: completed ? FontWeight.bold : null)),
          ),
        ]),
        if (position > 0 && duration > 0 && !completed)
          Padding(
            padding: const EdgeInsets.only(top: 6),
            child: LinearProgressIndicator(
              value: (position / duration).clamp(0.0, 1.0),
              borderRadius: BorderRadius.circular(2),
            ),
          ),
      ],
    );
    final body = grid
        ? Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            AspectRatio(aspectRatio: 1, child: artwork),
            const SizedBox(height: 8),
            details
          ])
        : Row(children: [
            artwork,
            const SizedBox(width: 12),
            Expanded(child: details),
          ]);
    final Widget tile;
    if (selecting) {
      final checkbox = Checkbox(
        value: isSelected,
        semanticLabel: 'Select ${e['title']}',
        onChanged: bulkBusy ? null : (_) => selectEpisode(e),
      );
      tile = InkWell(
        key: ValueKey((e['feed_key'], e['episode_key'])),
        onTap: bulkBusy ? null : () => selectEpisode(e),
        onLongPress: bulkBusy ? null : () => selectEpisode(e),
        child: Ink(
          color: isSelected ? colors.secondaryContainer : null,
          child: grid
              ? Stack(children: [
                  Padding(
                      padding: const EdgeInsets.only(bottom: 10), child: body),
                  Positioned(
                    left: 4,
                    top: 4,
                    child: DecoratedBox(
                      decoration: BoxDecoration(
                          color: colors.surface.withValues(alpha: 0.85),
                          shape: BoxShape.circle),
                      child: checkbox,
                    ),
                  ),
                ])
              : Padding(
                  padding: const EdgeInsets.fromLTRB(4, 8, 16, 8),
                  child: Row(children: [checkbox, Expanded(child: body)]),
                ),
        ),
      );
    } else {
      tile = PodcastDownloadControls(
        key: ValueKey((e['feed_key'], e['episode_key'])),
        feed: e['feed_key'] as String,
        scope: e['browse_scope'] as String,
        episode: e,
        body: grid ? details : body,
        cover: grid ? artwork : null,
        vertical: grid,
        onLongPress: bulkBusy ? null : () => selectEpisode(e),
        onPlaybackReturn: reload,
        onDeleteEpisode: () => deleteEpisode(e, e['browse_scope'] as String),
        completeLabel: completed ? 'Mark uncompleted' : 'Mark completed',
        onToggleComplete: scope == null ||
                pending.contains(
                  '${e['scope_feed_key']}:${e['scope_episode_key']}',
                )
            ? null
            : () => complete(e, scope),
      );
    }
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
            clipBehavior: Clip.antiAlias,
            color: completed ? colors.surfaceContainerHighest : null,
            child: styledTile,
          )
        : selecting || bulkBusy
            ? styledTile
            : EpisodeSwipeToDelete(
                episode: (e['feed_key'], e['episode_key']),
                onDelete: () => deleteEpisode(e, e['browse_scope'] as String),
                child: styledTile);
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
          bottomNavigationBar: selecting ? null : const NowPlayingBar(),
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
                    style:
                        IconButton.styleFrom(minimumSize: const Size(48, 48)),
                    onSelected: (view) {
                      if (view == 'select') {
                        selectEpisode(null);
                        return;
                      }
                      if (view == 'layout') {
                        ref.read(podcastEpisodeGridProvider.notifier).state =
                            !grid;
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
                          value: 'select',
                          child: ListTile(
                              leading: Icon(Icons.checklist),
                              title: Text('Select episodes'))),
                      PopupMenuItem(
                          value: 'layout',
                          child: ListTile(
                              leading: Icon(
                                  grid ? Icons.view_list : Icons.grid_view),
                              title: Text(
                                  grid ? 'Show as list' : 'Show as grid'))),
                      if (widget.feedKey == null && widget.viewSwitcher == null)
                        const PopupMenuItem(
                            value: 'feeds',
                            child: ListTile(
                                leading: Icon(Icons.podcasts),
                                title: Text('Subscriptions and feeds'))),
                      if (widget.feedKey == null && widget.serverSetId != null)
                        const PopupMenuItem(
                            value: 'files',
                            child: ListTile(
                                leading: Icon(Icons.folder_outlined),
                                title: Text('Downloaded server files'))),
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
            if (widget.syncMessage != null) PodcastNotice(widget.syncMessage!),
            if (refreshing)
              LinearProgressIndicator(
                  semanticsLabel: refreshTotal == 0
                      ? 'Updating podcasts…'
                      : 'Refreshing feeds $refreshDone of $refreshTotal'),
            SingleChildScrollView(
                scrollDirection: Axis.horizontal,
                padding: const EdgeInsets.fromLTRB(16, 8, 16, 4),
                child: Row(children: [
                  for (final (option, label) in const [
                    ('uncompleted', 'Uncompleted'),
                    ('started', 'In progress'),
                    ('completed', 'Completed'),
                    ('all', 'All'),
                  ])
                    Padding(
                        padding: const EdgeInsets.only(right: 8),
                        child: ChoiceChip(
                            label: Text(label),
                            selected: filter == option,
                            onSelected: bulkBusy
                                ? null
                                : (_) {
                                    setState(() => filter = option);
                                    page(0);
                                  })),
                  if (refreshing)
                    Text(
                        refreshTotal == 0
                            ? 'Updating podcasts…'
                            : 'Refreshing feeds $refreshDone of $refreshTotal',
                        style: Theme.of(context).textTheme.bodySmall)
                  else if (widget.feedKey == null)
                    Text(
                        filter == 'started'
                            ? 'Recently played first'
                            : 'Newest first',
                        style: Theme.of(context).textTheme.bodySmall),
                ])),
            if (error != null) PodcastNotice(error!, isError: true),
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
                        if (rows.isEmpty) const _EmptyEpisodes(),
                        if (grid)
                          LayoutBuilder(builder: (context, constraints) {
                            // Flowing cards grow with text and expanded notes.
                            final available = constraints.maxWidth - 16;
                            // At least three cards per row on a phone.
                            final columns =
                                (available / 150).floor().clamp(3, 8);
                            final width =
                                (available - (columns - 1) * 8) / columns;
                            return Padding(
                                padding: const EdgeInsets.all(8),
                                child:
                                    Wrap(spacing: 8, runSpacing: 8, children: [
                                  for (final e in rows.take(40))
                                    SizedBox(
                                        width: width,
                                        child: episodeTile(e, scope, true)),
                                ]));
                          })
                        else
                          for (final e in rows.take(40))
                            episodeTile(e, scope, false),
                        if (offset > 0 || loadedRows.length > 40)
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

/// A short, readable status or error line above a podcast list.
class PodcastNotice extends StatelessWidget {
  const PodcastNotice(this.message, {super.key, this.isError = false});
  final String message;
  final bool isError;
  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    return Container(
      width: double.infinity,
      margin: const EdgeInsets.fromLTRB(16, 8, 16, 0),
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      decoration: BoxDecoration(
        color: isError ? colors.errorContainer : colors.secondaryContainer,
        borderRadius: BorderRadius.circular(8),
      ),
      child: Row(children: [
        Icon(isError ? Icons.error_outline : Icons.info_outline,
            size: 20,
            color: isError
                ? colors.onErrorContainer
                : colors.onSecondaryContainer),
        const SizedBox(width: 8),
        Expanded(
            child: Text(message,
                style: TextStyle(
                    color: isError
                        ? colors.onErrorContainer
                        : colors.onSecondaryContainer))),
      ]),
    );
  }
}

class _EmptyEpisodes extends StatelessWidget {
  const _EmptyEpisodes();
  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.fromLTRB(24, 48, 24, 24),
      child: Column(children: [
        Icon(Icons.podcasts,
            size: 56, color: theme.colorScheme.onSurfaceVariant),
        const SizedBox(height: 16),
        Text('No episodes in this filter. Refresh the feed to fetch episodes.',
            textAlign: TextAlign.center, style: theme.textTheme.bodyLarge),
      ]),
    );
  }
}
