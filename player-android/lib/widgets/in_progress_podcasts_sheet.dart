import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers/podcast_library_provider.dart';
import '../utils/duration_formatter.dart';

/// Most episodes the in-progress sheet lists, most recently played first.
const inProgressPodcastsLimit = 50;

typedef InProgressPodcasts = ({String scope, List<Map<String, Object?>> rows});

/// Started, unfinished episodes of the current podcast library. Device state
/// only, so the list opens instantly and offline too.
final inProgressPodcastsProvider =
    FutureProvider.autoDispose<InProgressPodcasts>((ref) async {
  final scope = await ref.watch(podcastScopeProvider.future);
  await ref.watch(podcastSubscriptionsProvider.future);
  final rows = await ref
      .watch(podcastRepositoryProvider)
      .browseEpisodes(scope, filter: 'started', limit: inProgressPodcastsLimit);
  return (scope: scope, rows: rows);
});

/// Opens the in-progress list; resolves to the episode the listener picked.
Future<({String scope, Map<String, Object?> episode})?> showInProgressPodcasts(
        BuildContext context) =>
    showModalBottomSheet<({String scope, Map<String, Object?> episode})>(
        context: context,
        isScrollControlled: true,
        showDragHandle: true,
        builder: (_) => const InProgressPodcastsSheet());

class InProgressPodcastsSheet extends ConsumerWidget {
  const InProgressPodcastsSheet({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final list = ref.watch(inProgressPodcastsProvider);
    return SafeArea(
        child: ConstrainedBox(
            constraints: BoxConstraints(
                maxHeight: MediaQuery.sizeOf(context).height * 0.75),
            child: Column(
                key: const Key('in_progress_podcasts_sheet'),
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Padding(
                      padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
                      child: Text('Podcasts in progress',
                          style: Theme.of(context).textTheme.titleMedium)),
                  Flexible(
                      child: list.when(
                    loading: () => const Padding(
                        padding: EdgeInsets.all(24),
                        child: Center(child: CircularProgressIndicator())),
                    error: (_, __) => TextButton.icon(
                        onPressed: () =>
                            ref.invalidate(inProgressPodcastsProvider),
                        icon: const Icon(Icons.refresh),
                        label: const Text('Retry podcasts in progress')),
                    data: (data) => data.rows.isEmpty
                        ? const Padding(
                            padding: EdgeInsets.all(24),
                            child: Text('No podcast episodes in progress.'))
                        : ListView(shrinkWrap: true, children: [
                            for (final episode in data.rows)
                              ListTile(
                                leading: const Icon(Icons.play_circle_outline),
                                title: Text(
                                    episode['title'] as String? ??
                                        'Podcast episode',
                                    maxLines: 2,
                                    overflow: TextOverflow.ellipsis),
                                subtitle: Text(
                                    [
                                      if ((episode['podcast_title']
                                                  as String? ??
                                              '')
                                          .isNotEmpty)
                                        episode['podcast_title'] as String,
                                      'Resume at ${formatDuration((episode['position_seconds'] as num? ?? 0).toDouble())}',
                                    ].join(' · '),
                                    maxLines: 2,
                                    overflow: TextOverflow.ellipsis),
                                onTap: () => Navigator.of(context)
                                    .pop((scope: data.scope, episode: episode)),
                              ),
                          ]),
                  )),
                ])));
  }
}
