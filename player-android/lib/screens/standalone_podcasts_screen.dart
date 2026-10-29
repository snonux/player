import 'dart:async';
import 'podcast_discovery_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/podcast_library_provider.dart';
import '../providers/api_client_provider.dart';
import '../services/podcast_identity.dart';
import '../widgets/podcast_download_controls.dart';
import '../widgets/podcast_artwork.dart';
import '../widgets/podcast_show_notes.dart';
import '../widgets/podcast_publication_date.dart';
import '../services/podcast_metadata.dart';
import '../providers/global_sync_provider.dart';

class StandalonePodcastsScreen extends ConsumerStatefulWidget {
  const StandalonePodcastsScreen({super.key});
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
    final feeds = ref.watch(podcastSubscriptionsProvider);
    final scope = ref.watch(podcastScopeProvider).valueOrNull;
    return Scaffold(
        appBar: AppBar(title: const Text('Podcasts on this device'), actions: [
          IconButton(
              tooltip: 'Explore podcasts',
              onPressed: busy
                  ? null
                  : () => Navigator.of(context).push(MaterialPageRoute<void>(
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
                } else {
                  subscribe();
                }
              },
              itemBuilder: (_) => const [
                    PopupMenuItem(
                        value: 'refresh', child: Text('Refresh feeds')),
                    PopupMenuItem(value: 'url', child: Text('Add feed URL')),
                  ])
        ]),
        body: Column(children: [
          if (busy) const LinearProgressIndicator(),
          if (error != null)
            Padding(padding: const EdgeInsets.all(16), child: Text(error!)),
          ListTile(
              leading: const Icon(Icons.folder_copy_outlined),
              title: const Text('All podcast episodes'),
              onTap: () => Navigator.of(context).push(MaterialPageRoute<void>(
                  builder: (_) => const LocalPodcastEpisodesScreen()))),
          Expanded(
              child: feeds.when(
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
                                    leading: PodcastArtwork(
                                        imageUrl: feed['image_url'] as String? ??
                                            '',
                                        title: feed['title'] as String),
                                    title: Text(feed['title'] as String),
                                    subtitle: Text(
                                        feed['last_error'] as String? ??
                                            feed['fetch_url'] as String),
                                    onTap: () => Navigator.of(context).push(
                                        MaterialPageRoute<void>(
                                            builder: (_) =>
                                                LocalPodcastEpisodesScreen(
                                                    feedKey: feed['feed_key']
                                                        as String,
                                                    title: feed['title']
                                                        as String))),
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
      {super.key, this.feedKey, this.title = 'All podcast episodes'});
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
  bool refreshing = false;
  int refreshDone = 0, refreshTotal = 0, refreshGeneration = 0;

  @override
  void initState() {
    super.initState();
    if (widget.feedKey == null) {
      filter = 'all';
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) unawaited(refreshFeeds(missingOnly: true));
      });
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
    final generation = ++refreshGeneration;
    try {
      final repository = ref.read(podcastRepositoryProvider);
      final scope = await ref.read(podcastScopeProvider.future);
      if (!mounted || generation != refreshGeneration) return;
      final keys = widget.feedKey != null
          ? [widget.feedKey!]
          : missingOnly
              ? await repository.uncachedSubscriptions(scope)
              : (await repository.subscriptions(scope))
                  .map((s) => s['feed_key'] as String)
                  .toList();
      if (!mounted || generation != refreshGeneration) return;
      setState(() {
        refreshing = keys.isNotEmpty;
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
    setState(() => offset = next);
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

  @override
  Widget build(BuildContext context) {
    final scope = ref.watch(podcastScopeProvider).valueOrNull;
    ref.listen(podcastScopeProvider, (previous, next) {
      if (previous?.valueOrNull != next.valueOrNull) {
        setState(() {
          offset = 0;
          error = null;
        });
        if (scroll.hasClients) scroll.jumpTo(0);
        if (widget.feedKey == null && previous?.valueOrNull != null) {
          unawaited(refreshFeeds(missingOnly: true));
        }
      }
    });
    final episodes =
        ref.watch(podcastEpisodePageProvider((widget.feedKey, filter, offset)));
    return Scaffold(
      appBar: AppBar(title: Text(widget.title), actions: [
        IconButton(
            tooltip: 'Reload episodes',
            onPressed: refreshing ? null : () => refreshFeeds(),
            icon: const Icon(Icons.refresh))
      ]),
      body: Column(children: [
        if (refreshing) const LinearProgressIndicator(),
        if (refreshing) Text('Refreshing feeds $refreshDone of $refreshTotal'),
        if (widget.feedKey == null)
          const Padding(
              padding: EdgeInsets.all(8),
              child: Text('Newest first · Includes listened episodes')),
        if (widget.feedKey != null)
          Padding(
              padding: const EdgeInsets.all(8),
              child: Wrap(spacing: 8, children: [
                for (final option in ['uncompleted', 'completed', 'all'])
                  ChoiceChip(
                      label: Text(
                          '${option[0].toUpperCase()}${option.substring(1)}'),
                      selected: filter == option,
                      onSelected: (_) {
                        setState(() => filter = option);
                        page(0);
                      })
              ])),
        if (error != null) Text(error!),
        Expanded(
            child: episodes.when(
          skipLoadingOnReload: true,
          loading: () => const Center(child: CircularProgressIndicator()),
          error: (_, __) => Center(
              child: TextButton(
                  onPressed: reload,
                  child: const Text('Retry opening episodes'))),
          data: (rows) => rows.any((row) => row['browse_scope'] != scope)
              ? const Center(child: CircularProgressIndicator())
              : ListView(controller: scroll, children: [
                  if (rows.isEmpty)
                    const Padding(
                        padding: EdgeInsets.all(24),
                        child: Text(
                            'No episodes in this filter. Refresh the feed to fetch episodes.')),
                  for (final e in rows.take(40))
                    ListTile(
                      key: ValueKey((e['feed_key'], e['episode_key'])),
                      leading: ColorFiltered(
                          colorFilter: ColorFilter.mode(
                              e['completed'] == 1
                                  ? Colors.grey
                                  : Colors.transparent,
                              e['completed'] == 1
                                  ? BlendMode.saturation
                                  : BlendMode.dst),
                          child: Opacity(
                              opacity: e['completed'] == 1 ? 0.5 : 1,
                              child: PodcastArtwork(
                                  imageUrl: e['image_url'] as String? ?? '',
                                  fallbackUrl:
                                      e['feed_image_url'] as String? ?? '',
                                  title: e['title'] as String))),
                      title: Text(e['title'] as String,
                          style: e['completed'] == 1
                              ? TextStyle(
                                  color: Theme.of(context)
                                      .colorScheme
                                      .onSurfaceVariant)
                              : null),
                      subtitle: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            if (widget.feedKey == null)
                              Text(e['podcast_title'] as String),
                            PodcastPublicationDate(
                                date:
                                    podcastPublicationDate(e['published_at'])),
                            Text(_progressSummary(e)),
                            if ((e['position_seconds'] as num) > 0 &&
                                (e['duration_seconds'] as num) > 0)
                              LinearProgressIndicator(
                                  value: ((e['position_seconds'] as num) /
                                          (e['duration_seconds'] as num))
                                      .clamp(0.0, 1.0)),
                            PodcastShowNotes(
                                description: e['description'] as String? ?? ''),
                            PodcastDownloadControls(
                                feed: e['feed_key'] as String,
                                scope: e['browse_scope'] as String,
                                episode: e,
                                onPlaybackReturn: reload,
                                onDeleteEpisode: () => deleteEpisode(
                                    e, e['browse_scope'] as String),
                                completeLabel: e['completed'] == 1
                                    ? 'Mark uncompleted'
                                    : 'Mark completed',
                                onToggleComplete: scope == null ||
                                        pending.contains(
                                            '${e['scope_feed_key']}:${e['scope_episode_key']}')
                                    ? null
                                    : () => complete(e, scope)),
                          ]),
                    ),
                  Wrap(
                      alignment: WrapAlignment.center,
                      crossAxisAlignment: WrapCrossAlignment.center,
                      spacing: 8,
                      children: [
                        TextButton(
                            onPressed:
                                offset == 0 ? null : () => page(offset - 40),
                            child: const Text('Previous page')),
                        Text('Page ${offset ~/ 40 + 1}'),
                        TextButton(
                            onPressed: rows.length <= 40
                                ? null
                                : () => page(offset + 40),
                            child: const Text('Next page')),
                      ]),
                ]),
        )),
      ]),
    );
  }
}
