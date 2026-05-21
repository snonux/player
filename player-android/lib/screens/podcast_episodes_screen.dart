import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/models.dart';
import '../providers/api_client_provider.dart';
import '../utils/duration_formatter.dart';
import '../utils/error_mappers.dart';

// ---------------------------------------------------------------------------
// _buildEpisodeWithCompleted  (file-private helper)
// ---------------------------------------------------------------------------

/// Returns a copy of [episode] with the [isCompleted] flag replaced.
///
/// [PodcastEpisode] is immutable, so we rebuild via [PodcastEpisode.fromJson] /
/// [PodcastEpisode.toJson] to avoid coupling this screen to any `copyWith`
/// generated method.  Extracted as a file-private function so both
/// [_PodcastEpisodesScreenState] and the episode row can share it without
/// adding a public model API (Dependency Inversion, DRY).
PodcastEpisode _buildEpisodeWithCompleted(
  PodcastEpisode episode,
  bool isCompleted,
) {
  final json = episode.toJson()..['is_completed'] = isCompleted;
  return PodcastEpisode.fromJson(json);
}

/// Screen that lists all episodes for a single podcast set.
///
/// Each episode row shows the episode title, publication date, duration, and:
///   - A checkmark icon / toggle button reflecting the [PodcastEpisode.isCompleted]
///     (played/unplayed) state.  Tapping the icon performs an optimistic update
///     via [toggleEpisodeComplete] and reverts on error.
///   - A linear progress bar below the title showing playback position derived
///     from [PodcastEpisode.positionSeconds] and [PodcastEpisode.durationSeconds]
///     (when the episode has been partially played but not completed).
///
/// Design notes:
///   - [ConsumerStatefulWidget] allows local loading/error state, [mounted]
///     guards on async continuations, and pull-to-refresh without lifting
///     state into a global Riverpod notifier.
///   - Error handling is fully delegated to top-level helpers in
///     `error_mappers.dart` — no `dio` import in this file (DIP).
///   - Optimistic updates mirror the pattern in [MediaGridScreen.toggleFavorite]:
///     flip immediately, reconcile/revert after the API call settles.
class PodcastEpisodesScreen extends ConsumerStatefulWidget {
  /// The numeric identifier of the podcast set whose episodes will be listed.
  final int setId;

  /// Optional human-readable name of the podcast feed shown in the app bar.
  ///
  /// Pass this when navigating from [PodcastListScreen] so the app bar title
  /// appears immediately without an extra API call.
  final String? setName;

  const PodcastEpisodesScreen({
    super.key,
    required this.setId,
    this.setName,
  });

  @override
  ConsumerState<PodcastEpisodesScreen> createState() =>
      _PodcastEpisodesScreenState();
}

class _PodcastEpisodesScreenState
    extends ConsumerState<PodcastEpisodesScreen> {
  // Nullable: null means "not yet loaded" (loading indicator is shown).
  List<PodcastEpisode>? _episodes;

  // Non-null when the last load attempt failed.
  String? _error;

  // True while the initial or refresh load is in flight.
  bool _isLoading = false;

  @override
  void initState() {
    super.initState();
    // Defer first load until after the first frame so [ref] is fully bound
    // and any provider overrides in the test environment are applied.
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
  }

  // ---------------------------------------------------------------------------
  // Data loading
  // ---------------------------------------------------------------------------

  /// Fetches episodes for [widget.setId] and updates local state.
  ///
  /// Called on first mount and on pull-to-refresh.  Errors are mapped by
  /// [episodeListErrorMessage] so the widget stays free of Dio.
  Future<void> _load() async {
    if (!mounted) return;
    setState(() {
      _isLoading = true;
      _error = null;
    });

    try {
      final client = ref.read(apiClientProvider);
      final items = await client.listEpisodes(widget.setId);
      if (!mounted) return;
      setState(() {
        _episodes = items;
        _isLoading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = episodeListErrorMessage(e);
        _isLoading = false;
      });
    }
  }

  // ---------------------------------------------------------------------------
  // Played/unplayed toggle
  // ---------------------------------------------------------------------------

  /// Optimistically flips the [isCompleted] flag on the episode at [index],
  /// calls [toggleEpisodeComplete] on the server, then reverts on error.
  ///
  /// Guard: if [_episodes] is null or [index] is out of range the call is a
  /// no-op.  The [mounted] check after the await prevents setState calls on a
  /// disposed widget.
  Future<void> _toggleCompleteAt(int index) async {
    final items = _episodes;
    if (items == null || index < 0 || index >= items.length) return;

    final original = items[index];
    // Flip the played state optimistically so the icon updates without lag.
    final optimistic =
        _buildEpisodeWithCompleted(original, !original.isCompleted);

    setState(() {
      _episodes = List<PodcastEpisode>.from(items)..[index] = optimistic;
    });

    try {
      final client = ref.read(apiClientProvider);
      await client.toggleEpisodeComplete(original.id);
      // toggleEpisodeComplete returns 204 with no body; the optimistic state
      // is already correct — no reconciliation needed.
    } catch (e) {
      if (!mounted) return;
      // Revert the optimistic update so the UI reflects actual server state.
      setState(() {
        final current = _episodes;
        if (current != null && index < current.length) {
          _episodes = List<PodcastEpisode>.from(current)..[index] = original;
        }
      });
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(episodeToggleErrorMessage(e))),
      );
    }
  }

  // ---------------------------------------------------------------------------
  // Build
  // ---------------------------------------------------------------------------

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: _buildAppBar(),
      body: _buildBody(context),
    );
  }

  /// Builds the app bar, showing [widget.setName] when available.
  AppBar _buildAppBar() {
    return AppBar(
      title: Text(widget.setName ?? 'Episodes'),
    );
  }

  /// Delegates to the appropriate state widget:
  ///   - Full-screen spinner (first load, before any data arrives).
  ///   - Error view with a retry button.
  ///   - Empty-state message when [listEpisodes] returns an empty list.
  ///   - Scrollable list of episode rows once data is available.
  Widget _buildBody(BuildContext context) {
    // Show a full-screen spinner only on the very first load (no data yet).
    if (_isLoading && _episodes == null) {
      return const Center(
        key: Key('episodes_loading'),
        child: CircularProgressIndicator(),
      );
    }

    // Show an error view with a retry button if the load failed.
    if (_error != null) {
      return _ErrorView(message: _error!, onRetry: _load);
    }

    // [RefreshIndicator] wraps the scrollable content so pull-to-refresh
    // triggers [_load] on both the list and the empty-state view.
    return RefreshIndicator(
      onRefresh: _load,
      child: _episodes == null || _episodes!.isEmpty
          ? const _EmptyView()
          : _EpisodeList(
              episodes: _episodes!,
              onToggleComplete: _toggleCompleteAt,
            ),
    );
  }
}

// ---------------------------------------------------------------------------
// Sub-widgets
// ---------------------------------------------------------------------------

/// Scrollable list of episode rows.
///
/// Extracted into its own stateless widget so [_PodcastEpisodesScreenState]
/// stays concise and the list layout is independently testable.
class _EpisodeList extends StatelessWidget {
  const _EpisodeList({
    required this.episodes,
    required this.onToggleComplete,
  });

  final List<PodcastEpisode> episodes;

  /// Called with the index of the episode whose played state was tapped.
  ///
  /// Using an index (rather than the episode itself) lets the state class
  /// update the correct position in its list without a linear search.
  final void Function(int index) onToggleComplete;

  @override
  Widget build(BuildContext context) {
    return ListView.separated(
      key: const Key('episodes_list'),
      itemCount: episodes.length,
      separatorBuilder: (_, __) => const Divider(height: 1),
      itemBuilder: (context, index) => _EpisodeRow(
        episode: episodes[index],
        onToggleComplete: () => onToggleComplete(index),
      ),
    );
  }
}

/// List row for a single [PodcastEpisode].
///
/// Shows:
///   - Episode title (dimmed when [PodcastEpisode.isCompleted] is true).
///   - Publication date and formatted duration.
///   - A linear progress bar below the title reflecting playback position
///     (visible only when the episode has been partially played).
///   - A checkmark toggle icon on the trailing edge reflecting [isCompleted].
///
/// Tapping the checkmark fires [onToggleComplete]; tapping the row body is
/// currently a no-op (episode playback will be wired in a future iteration).
class _EpisodeRow extends StatelessWidget {
  const _EpisodeRow({
    required this.episode,
    required this.onToggleComplete,
  });

  final PodcastEpisode episode;

  /// Called when the user taps the played/unplayed icon.
  ///
  /// The parent state performs the optimistic update and API call; this
  /// widget is purely presentational (Single Responsibility / DIP).
  final VoidCallback onToggleComplete;

  @override
  Widget build(BuildContext context) {
    return Padding(
      key: Key('episode_row_${episode.id}'),
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // Episode info fills the available width.
          Expanded(child: _EpisodeInfo(episode: episode)),
          const SizedBox(width: 8),
          // Checkmark toggle anchored to the trailing edge.
          _PlayedToggle(
            episodeId: episode.id,
            isCompleted: episode.isCompleted,
            onTap: onToggleComplete,
          ),
        ],
      ),
    );
  }
}

/// Displays the text content of an episode row: title, meta line, and
/// optional progress bar.
///
/// Extracted from [_EpisodeRow] to keep each widget under ~30 lines and to
/// isolate the progress-bar logic (Single Responsibility).
class _EpisodeInfo extends StatelessWidget {
  const _EpisodeInfo({required this.episode});

  final PodcastEpisode episode;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    // Dim the title when the episode has been fully played so unplayed
    // episodes stand out visually.
    final titleColor = episode.isCompleted
        ? theme.colorScheme.onSurface.withAlpha(128)
        : theme.colorScheme.onSurface;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        // Episode title, dimmed when completed.
        Text(
          episode.title,
          key: Key('episode_title_${episode.id}'),
          style: theme.textTheme.bodyMedium?.copyWith(color: titleColor),
          maxLines: 2,
          overflow: TextOverflow.ellipsis,
        ),
        const SizedBox(height: 4),
        // Meta line: formatted duration and optional publish date.
        _MetaLine(
          durationSeconds: episode.durationSeconds,
          publishedAt: episode.publishedAt,
        ),
        // Progress bar shown only when partially played (position > 0 and not
        // fully completed) so it does not clutter completed or never-started
        // episodes.
        if (_shouldShowProgress) ...[
          const SizedBox(height: 6),
          _PlaybackProgressBar(
            positionSeconds: episode.positionSeconds,
            durationSeconds: episode.durationSeconds ?? 0,
          ),
        ],
      ],
    );
  }

  /// True when the episode has a saved playback position but has not been
  /// marked as fully completed — i.e. the user is partway through.
  bool get _shouldShowProgress =>
      !episode.isCompleted &&
      episode.positionSeconds > 0 &&
      (episode.durationSeconds ?? 0) > 0;
}

/// Displays formatted duration and optional publish date for an episode.
///
/// Accepts only the two primitive values it actually uses ([durationSeconds]
/// and [publishedAt]) rather than the full [PodcastEpisode].  This mirrors the
/// same pattern used by [_PlaybackProgressBar] and avoids the ISP violation of
/// depending on a wide interface for two fields (Single Responsibility, ISP).
class _MetaLine extends StatelessWidget {
  const _MetaLine({required this.durationSeconds, required this.publishedAt});

  final double? durationSeconds;
  final DateTime? publishedAt;

  @override
  Widget build(BuildContext context) {
    final textStyle = Theme.of(context)
        .textTheme
        .bodySmall
        ?.copyWith(color: Theme.of(context).colorScheme.onSurfaceVariant);

    final parts = <String>[];
    if (durationSeconds != null && durationSeconds! > 0) {
      parts.add(formatDuration(durationSeconds!));
    }
    if (publishedAt != null) {
      parts.add(_formatDate(publishedAt!));
    }

    return Text(
      parts.join(' · '),
      style: textStyle,
      maxLines: 1,
      overflow: TextOverflow.ellipsis,
    );
  }

  /// Formats [date] as `MMM d, yyyy` (e.g. "Jan 5, 2024").
  ///
  /// Uses pure Dart arithmetic so there is no dependency on the `intl` package.
  static String _formatDate(DateTime date) {
    const months = [
      'Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun',
      'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec',
    ];
    return '${months[date.month - 1]} ${date.day}, ${date.year}';
  }
}

/// Thin linear progress bar showing playback position relative to duration.
///
/// The bar is only rendered when the caller has already verified that both
/// [positionSeconds] and [durationSeconds] are positive, so the fraction is
/// always in [0, 1].
///
/// Extracted from [_EpisodeInfo] so it is independently testable and keeps the
/// parent under ~30 lines (Single Responsibility).
class _PlaybackProgressBar extends StatelessWidget {
  const _PlaybackProgressBar({
    required this.positionSeconds,
    required this.durationSeconds,
  });

  final double positionSeconds;
  final double durationSeconds;

  @override
  Widget build(BuildContext context) {
    // Clamp to [0, 1] to guard against server-side inconsistencies (e.g.
    // position slightly beyond duration due to encoding length mismatch).
    final fraction = (positionSeconds / durationSeconds).clamp(0.0, 1.0);

    return LinearProgressIndicator(
      key: const Key('episode_progress_bar'),
      value: fraction,
      minHeight: 3,
      backgroundColor:
          Theme.of(context).colorScheme.surfaceContainerHighest,
    );
  }
}

/// Icon button that reflects the played/unplayed state of an episode.
///
/// Renders a filled check-circle icon when [isCompleted] is true and an
/// outlined one otherwise.  Uses a [GestureDetector] with
/// [HitTestBehavior.opaque] to consume taps without propagating to parent
/// [InkWell] widgets (mirrors [_FavoriteIconButton] in media_grid_screen.dart).
///
/// Extracted as a separate widget so it is independently testable and to keep
/// [_EpisodeRow.build] under 30 lines (Single Responsibility).
class _PlayedToggle extends StatelessWidget {
  const _PlayedToggle({
    required this.episodeId,
    required this.isCompleted,
    required this.onTap,
  });

  final int episodeId;
  final bool isCompleted;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      key: Key('episode_played_toggle_$episodeId'),
      behavior: HitTestBehavior.opaque,
      onTap: onTap,
      child: Padding(
        padding: const EdgeInsets.all(4),
        child: Icon(
          isCompleted ? Icons.check_circle : Icons.check_circle_outline,
          size: 24,
          color: isCompleted
              ? Theme.of(context).colorScheme.primary
              : Theme.of(context).colorScheme.onSurfaceVariant,
        ),
      ),
    );
  }
}

/// Full-screen empty-state view, shown when [listEpisodes] returns an empty
/// list.
///
/// Wrapped in a [ListView] with [AlwaysScrollableScrollPhysics] so the
/// [RefreshIndicator] parent can still trigger a pull-to-refresh gesture even
/// when there is no scrollable content.
class _EmptyView extends StatelessWidget {
  const _EmptyView();

  @override
  Widget build(BuildContext context) {
    return ListView(
      physics: const AlwaysScrollableScrollPhysics(),
      children: [
        SizedBox(
          height: MediaQuery.of(context).size.height * 0.6,
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              Icon(
                Icons.podcasts_outlined,
                size: 72,
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
              const SizedBox(height: 16),
              Text(
                'No episodes yet',
                key: const Key('episodes_empty'),
                style: Theme.of(context).textTheme.titleMedium,
              ),
              const SizedBox(height: 8),
              Text(
                'Pull down to refresh.',
                style: Theme.of(context).textTheme.bodySmall,
              ),
            ],
          ),
        ),
      ],
    );
  }
}

/// Full-screen error view with a retry button.
///
/// Shown when [listEpisodes] throws (network error, server error, etc.).
/// The [message] comes from [episodeListErrorMessage], which maps exceptions to
/// human-readable strings.
class _ErrorView extends StatelessWidget {
  const _ErrorView({required this.message, required this.onRetry});

  final String message;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Icon(
              Icons.error_outline,
              size: 56,
              color: Theme.of(context).colorScheme.error,
            ),
            const SizedBox(height: 16),
            Text(
              message,
              key: const Key('episodes_error'),
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.bodyLarge,
            ),
            const SizedBox(height: 24),
            ElevatedButton.icon(
              key: const Key('episodes_retry'),
              onPressed: onRetry,
              icon: const Icon(Icons.refresh),
              label: const Text('Retry'),
            ),
          ],
        ),
      ),
    );
  }
}
