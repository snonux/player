import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../app_routes.dart';
import '../providers/podcast_library_provider.dart';
import '../utils/duration_formatter.dart';

/// The most recently listened, unfinished podcast episode in the current
/// podcast library, or null when nothing is in progress.
typedef ResumablePodcast = ({String scope, Map<String, Object?> episode});

/// Reads device podcast state only: no feed refresh and no server request, so
/// the shortcut is ready right after app start, offline included. A podcast
/// sync or feed refresh invalidates the subscriptions and so this row too.
final resumePodcastProvider =
    FutureProvider.autoDispose<ResumablePodcast?>((ref) async {
  final scope = await ref.watch(podcastScopeProvider.future);
  await ref.watch(podcastSubscriptionsProvider.future);
  final rows = await ref
      .watch(podcastRepositoryProvider)
      .browseEpisodes(scope, filter: 'started', limit: 1);
  return rows.isEmpty ? null : (scope: scope, episode: rows.first);
});

/// Shown in the mini-player slot while nothing is playing (for example right
/// after app start): one tap resumes the last podcast episode where it left
/// off. Optional chrome: any failure to read the library simply hides it.
class ResumePodcastBar extends ConsumerStatefulWidget {
  const ResumePodcastBar({super.key});

  @override
  ConsumerState<ResumePodcastBar> createState() => _ResumePodcastBarState();
}

class _ResumePodcastBarState extends ConsumerState<ResumePodcastBar> {
  bool _opening = false;

  @override
  Widget build(BuildContext context) {
    final resumable = ref.watch(resumePodcastProvider).valueOrNull;
    if (resumable == null) return const SizedBox.shrink();
    final episode = resumable.episode;
    final title = episode['title'] as String? ?? 'Podcast episode';
    final podcast = episode['podcast_title'] as String? ?? '';
    final position =
        formatDuration((episode['position_seconds'] as num? ?? 0).toDouble());
    final subtitle = [
      if (podcast.isNotEmpty) podcast,
      'Resume at $position',
    ].join(' · ');
    return Material(
        key: const Key('podcast_resume_bar'),
        color: Theme.of(context).colorScheme.surfaceContainer,
        child: SafeArea(
            top: false,
            child: Row(children: [
              Expanded(
                  child: InkWell(
                      key: const Key('podcast_resume_open'),
                      onTap: _opening ? null : () => _resume(resumable),
                      child: Semantics(
                          button: true,
                          label: 'Resume podcast: $title, $subtitle',
                          excludeSemantics: true,
                          child: Padding(
                              padding: const EdgeInsets.all(8),
                              child: Row(children: [
                                const SizedBox(
                                    width: 48,
                                    height: 48,
                                    child: Icon(Icons.podcasts)),
                                const SizedBox(width: 8),
                                Expanded(
                                    child: Column(
                                        crossAxisAlignment:
                                            CrossAxisAlignment.start,
                                        mainAxisSize: MainAxisSize.min,
                                        children: [
                                      Text(title,
                                          maxLines: 1,
                                          overflow: TextOverflow.ellipsis),
                                      Text(subtitle,
                                          maxLines: 1,
                                          overflow: TextOverflow.ellipsis,
                                          style: Theme.of(context)
                                              .textTheme
                                              .bodySmall),
                                    ])),
                              ]))))),
              IconButton(
                  key: const Key('podcast_resume_play'),
                  tooltip: 'Resume podcast',
                  style: IconButton.styleFrom(minimumSize: const Size(48, 48)),
                  onPressed: _opening ? null : () => _resume(resumable),
                  icon: const Icon(Icons.play_arrow)),
            ])));
  }

  Future<void> _resume(ResumablePodcast resumable) async {
    setState(() => _opening = true);
    try {
      final episode = resumable.episode;
      final request = await ref.read(podcastDownloadsProvider).playbackRequest(
          resumable.scope, episode['feed_key'] as String, episode,
          allowStreaming: true);
      // An account or library switch while the request was prepared must not
      // start the previous library's episode.
      final scope = await ref.read(podcastScopeProvider.future);
      if (!mounted || scope != resumable.scope) return;
      setState(() => _opening = false);
      await context.push<void>(AppRoutes.podcastPlayer, extra: request);
    } catch (_) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(const SnackBar(
            content: Text(
                'Could not resume this episode. Check the connection, then retry.')));
      }
    } finally {
      if (mounted) {
        if (_opening) setState(() => _opening = false);
        ref.invalidate(resumePodcastProvider);
      }
    }
  }
}
