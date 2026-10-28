import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/podcast_library_provider.dart';
import '../services/podcast_discovery.dart';

final podcastDiscoveryProvider = Provider((ref) => PodcastDiscovery());

class PodcastDiscoveryScreen extends ConsumerStatefulWidget {
  const PodcastDiscoveryScreen({super.key});
  @override
  ConsumerState<PodcastDiscoveryScreen> createState() =>
      _PodcastDiscoveryState();
}

class _PodcastDiscoveryState extends ConsumerState<PodcastDiscoveryScreen> {
  final query = TextEditingController();
  List<DiscoveredPodcast> results = [];
  bool searching = false;
  String country = 'US';
  String message = 'Enter a topic, author or podcast name to explore.';
  String? adding;
  final subscribed = <String>{};
  int revision = 0;

  @override
  void dispose() {
    query.dispose();
    super.dispose();
  }

  Future<void> search({bool popular = false}) async {
    final text = query.text.trim();
    if (!popular && (text.runes.length < 2 || text.runes.length > 100)) {
      setState(() => message = 'Enter 2 to 100 characters to search.');
      return;
    }
    final own = ++revision;
    setState(() {
      searching = true;
      results = [];
      message =
          popular ? 'Loading popular podcasts…' : 'Searching Apple Podcasts…';
    });
    try {
      final directory = ref.read(podcastDiscoveryProvider);
      final rows = popular
          ? await directory.popular(country)
          : await directory.search(text);
      if (!mounted || own != revision) return;
      setState(() {
        results = rows;
        message = rows.isEmpty
            ? 'No podcasts found. Try another topic or add a feed URL.'
            : '${rows.length} podcasts found.';
      });
    } catch (_) {
      if (mounted && own == revision) {
        setState(() => message =
            'Search unavailable. Check your connection and retry, or add a feed URL.');
      }
    } finally {
      if (mounted && own == revision) setState(() => searching = false);
    }
  }

  Future<void> add(String url, String title) async {
    if (adding != null) return;
    setState(() {
      adding = url;
      message = 'Subscribing…';
    });
    try {
      final repository = ref.read(podcastRepositoryProvider);
      final scope = await ref.read(podcastScopeProvider.future);
      await repository.subscribe(scope, url);
      if (mounted) {
        setState(() {
          subscribed.add(url);
          message = 'Subscribed to ${title.isEmpty ? url : title}.';
        });
      }
    } catch (_) {
      if (mounted) {
        setState(() => message =
            'Could not fetch this feed. Retry or check its URL. Your subscriptions remain saved.');
      }
    } finally {
      if (mounted) {
        // subscribe saves the register before fetching the publisher feed.
        // A failed refresh must still reveal that saved subscription.
        ref.invalidate(podcastSubscriptionsProvider);
        setState(() => adding = null);
      }
    }
  }

  Future<void> manual() async {
    final controller = TextEditingController();
    final url = await showDialog<String>(
        context: context,
        builder: (context) => AlertDialog(
              title: const Text('Add feed URL'),
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
                    onPressed: () =>
                        Navigator.pop(context, controller.text.trim()),
                    child: const Text('Subscribe'))
              ],
            ));
    Future<void>.delayed(const Duration(seconds: 1), controller.dispose);
    if (url != null && url.isNotEmpty && mounted) await add(url, '');
  }

  @override
  Widget build(BuildContext context) => Scaffold(
        appBar: AppBar(title: const Text('Explore podcasts'), actions: [
          PopupMenuButton<String>(
              tooltip: 'Podcast options',
              icon: const Icon(Icons.menu),
              style: IconButton.styleFrom(minimumSize: const Size(48, 48)),
              onSelected: (_) => manual(),
              itemBuilder: (_) => [
                    PopupMenuItem(
                        value: 'url',
                        enabled: adding == null,
                        child: const Text('Add feed URL'))
                  ])
        ]),
        body: ListView(padding: const EdgeInsets.all(16), children: [
          const Text(
              'Apple receives searches and the selected chart country. Mobile data may be used. Subscribing contacts the publisher.'),
          const SizedBox(height: 16),
          TextField(
              controller: query,
              maxLength: 100,
              textInputAction: TextInputAction.search,
              onSubmitted: (_) => search(),
              decoration: const InputDecoration(labelText: 'Search podcasts')),
          FilledButton(
              onPressed: searching ? null : search,
              child: const Text('Search')),
          ExpansionTile(
            key: const Key('popular_podcasts_options'),
            title: const Text('Popular podcasts'),
            childrenPadding: const EdgeInsets.only(bottom: 12),
            children: [
              DropdownButtonFormField<String>(
                initialValue: country,
                isExpanded: true,
                decoration: const InputDecoration(labelText: 'Chart country'),
                items: const {
                  'US': 'United States',
                  'GB': 'United Kingdom',
                  'DE': 'Germany',
                  'BG': 'Bulgaria',
                  'FR': 'France',
                  'ES': 'Spain',
                  'IT': 'Italy',
                  'CA': 'Canada',
                  'AU': 'Australia',
                  'IN': 'India',
                  'JP': 'Japan',
                  'BR': 'Brazil',
                  'NL': 'Netherlands'
                }
                    .entries
                    .map((e) =>
                        DropdownMenuItem(value: e.key, child: Text(e.value)))
                    .toList(),
                onChanged: searching
                    ? null
                    : (value) => setState(() => country = value!),
              ),
              OutlinedButton(
                  onPressed: searching ? null : () => search(popular: true),
                  child: const Text('Load popular podcasts')),
            ],
          ),
          const ExpansionTile(
            title: Text('About podcast search'),
            children: [
              Padding(
                padding: EdgeInsets.fromLTRB(16, 0, 16, 16),
                child: Text(
                    'Search Apple Podcasts by title, author or topic. Search terms, selected chart country and your IP address go to Apple. Subscribing contacts the publisher. Searches use your current connection, including mobile data. No artwork is loaded.'),
              ),
            ],
          ),
          if (searching || adding != null) const LinearProgressIndicator(),
          Padding(
              padding: const EdgeInsets.symmetric(vertical: 16),
              child: Semantics(liveRegion: true, child: Text(message))),
          for (final result in results)
            ListTile(
              contentPadding: EdgeInsets.zero,
              title: Text(result.title,
                  maxLines: 2, overflow: TextOverflow.ellipsis),
              subtitle: Text(result.author,
                  maxLines: 1, overflow: TextOverflow.ellipsis),
              trailing: IconButton(
                  style: IconButton.styleFrom(minimumSize: const Size(48, 48)),
                  tooltip: subscribed.contains(result.feedUrl)
                      ? 'Subscribed to ${result.title}'
                      : 'Subscribe to ${result.title}',
                  onPressed:
                      adding != null || subscribed.contains(result.feedUrl)
                          ? null
                          : () => add(result.feedUrl, result.title),
                  icon: Icon(subscribed.contains(result.feedUrl)
                      ? Icons.check
                      : Icons.add)),
            ),
        ]),
      );
}
