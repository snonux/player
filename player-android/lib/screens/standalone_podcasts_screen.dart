import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/podcast_library_provider.dart';
import '../services/podcast_identity.dart';
import '../widgets/podcast_download_controls.dart';

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

  @override
  Widget build(BuildContext context) {
    final feeds = ref.watch(podcastSubscriptionsProvider);
    return Scaffold(
        appBar: AppBar(title: const Text('Podcasts on this device'), actions: [
          IconButton(
              tooltip: 'Subscribe to podcast',
              onPressed: busy ? null : subscribe,
              icon: const Icon(Icons.add)),
          IconButton(
              tooltip: 'Refresh feeds',
              onPressed: busy
                  ? null
                  : () => run(() async {
                        final repository = ref.read(podcastRepositoryProvider);
                        final subscriptions =
                            await ref.read(podcastSubscriptionsProvider.future);
                        var failed = 0;
                        for (final feed in subscriptions) {
                          try {
                            await repository
                                .refresh(feed['feed_key'] as String);
                          } catch (_) {
                            failed++;
                          }
                        }
                        if (mounted && failed > 0) {
                          setState(() => error =
                              '$failed feed refreshes failed. Other feeds were refreshed; cached episodes remain available.');
                        }
                      }),
              icon: const Icon(Icons.refresh))
        ]),
        body: Column(children: [
          if (busy) const LinearProgressIndicator(),
          if (error != null)
            Padding(padding: const EdgeInsets.all(16), child: Text(error!)),
          Expanded(
              child: feeds.when(
                  loading: () =>
                      const Center(child: CircularProgressIndicator()),
                  error: (_, __) => Center(
                      child: TextButton(
                          onPressed: () =>
                              ref.invalidate(podcastSubscriptionsProvider),
                          child: const Text('Retry opening podcasts'))),
                  data: (items) => items.isEmpty
                      ? Center(
                          child: FilledButton.icon(
                              onPressed: busy ? null : subscribe,
                              icon: const Icon(Icons.podcasts),
                              label: const Text('Subscribe to podcast')))
                      : ListView(children: [
                          for (final feed in items)
                            ListTile(
                                leading: const Icon(Icons.podcasts),
                                title: Text(feed['title'] as String),
                                subtitle: Text(feed['last_error'] as String? ??
                                    feed['fetch_url'] as String),
                                onTap: () => Navigator.of(context).push(
                                    MaterialPageRoute<void>(
                                        builder: (_) => _CachedEpisodesScreen(
                                            feedKey: feed['feed_key'] as String,
                                            title: feed['title'] as String))),
                                trailing: PopupMenuButton<String>(
                                    tooltip: 'Manage podcast',
                                    onSelected: (action) => run(() async {
                                          final repository = ref
                                              .read(podcastRepositoryProvider);
                                          if (action == 'refresh') {
                                            await repository.refresh(
                                                feed['feed_key'] as String);
                                          } else {
                                            await repository.unsubscribe(
                                                await ref.read(
                                                    podcastScopeProvider
                                                        .future),
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

class _CachedEpisodesScreen extends ConsumerStatefulWidget {
  const _CachedEpisodesScreen({required this.feedKey, required this.title});
  final String feedKey, title;
  @override
  ConsumerState<_CachedEpisodesScreen> createState() =>
      _CachedEpisodesScreenState();
}

class _CachedEpisodesScreenState extends ConsumerState<_CachedEpisodesScreen> {
  late Future<List<Map<String, Object?>>> episodes;
  @override
  void initState() {
    super.initState();
    episodes = ref.read(podcastRepositoryProvider).episodes(widget.feedKey);
  }

  @override
  Widget build(BuildContext context) => Scaffold(
      appBar: AppBar(title: Text(widget.title)),
      body: FutureBuilder<List<Map<String, Object?>>>(
          future: episodes,
          builder: (context, snapshot) {
            if (snapshot.hasError) {
              return const Center(
                  child: Text('Could not open cached episodes.'));
            }
            if (!snapshot.hasData) {
              return const Center(child: CircularProgressIndicator());
            }
            if (snapshot.data!.isEmpty) {
              return const Center(
                  child: Text(
                      'No cached episodes yet. Refresh the feed when online.'));
            }
            return ListView(children: [
              for (final episode in snapshot.data!)
                ListTile(
                    leading: const Icon(Icons.headphones),
                    title: Text(episode['title'] as String),
                    subtitle: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(episode['description'] as String,
                              maxLines: 3, overflow: TextOverflow.ellipsis),
                          PodcastDownloadControls(
                              feed: widget.feedKey, episode: episode),
                        ]))
            ]);
          }));
}
