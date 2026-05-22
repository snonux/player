import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../app_routes.dart';
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
///   - Infinite-scroll pagination: [ScrollController] detects when the user
///     is within 200px of the bottom and calls [_loadMore] to append the next
///     page.  Pull-to-refresh resets to page 1.
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

// Number of episodes requested per page.  The server default is 50 (see
// player-server/docs/api.md §GET /api/podcasts/{id}/episodes).
const _kEpisodePageSize = 50;

class _PodcastEpisodesScreenState
    extends ConsumerState<PodcastEpisodesScreen> {
  // Nullable: null means "not yet loaded" (loading indicator is shown).
  List<PodcastEpisode>? _episodes;

  // Non-null when the last load attempt failed.
  String? _error;

  // True while the initial or refresh load is in flight.
  bool _isLoading = false;

  // Tracks episodes whose download is in-flight to prevent double-tap
  // from firing concurrent API calls for the same episode.
  final Set<int> _pendingDownloads = {};

  // Generation counter — incremented on each fresh load (refresh/first-mount).
  // Checked after every async gap so stale responses from cancelled loads are
  // silently discarded (cancellation-by-generation pattern).
  int _loadGeneration = 0;

  // Pagination state: current offset into the server list.
  int _offset = 0;

  // True when more pages may be available (last page was full).
  bool _hasMore = true;

  // True while a _loadMore request is in flight to prevent concurrent loads.
  bool _isLoadingMore = false;

  @override
  void initState() {
    super.initState();
    // Defer first load until after the first frame so [ref] is fully bound
    // and any provider overrides in the test environment are applied.
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
  }

  // ---------------------------------------------------------------------------
  // Scroll detection via notification
  // ---------------------------------------------------------------------------

  /// Called by [NotificationListener] in [_buildBody] on every scroll update.
  ///
  /// Using [ScrollNotification] (rather than [ScrollController.addListener])
  /// avoids attaching a controller to the [ListView], which would otherwise
  /// interfere with [RefreshIndicator]'s overscroll detection in test and
  /// production environments.  The notification still bubbles up to
  /// [RefreshIndicator] because [_onScrollNotification] returns false.
  bool _onScrollNotification(ScrollNotification notification) {
    if (notification is ScrollUpdateNotification) {
      final metrics = notification.metrics;
      if (metrics.pixels >= metrics.maxScrollExtent - 200) {
        _loadMore();
      }
    }
    // Return false so the notification continues to bubble (e.g. to RefreshIndicator).
    return false;
  }

  // ---------------------------------------------------------------------------
  // Data loading
  // ---------------------------------------------------------------------------

  /// Fetches the first page of episodes for [widget.setId] and resets all
  /// pagination state.
  ///
  /// Called on first mount and on pull-to-refresh.  Resetting [_offset] to 0
  /// and [_hasMore] to true ensures subsequent scroll-triggered loads start
  /// cleanly from the beginning.  Errors are mapped by [episodeListErrorMessage]
  /// so the widget stays free of Dio.
  Future<void> _load() async {
    if (!mounted) return;

    // Bump the generation before the async gap so stale callbacks from the
    // previous load detect the change and drop their result.
    final generation = ++_loadGeneration;

    setState(() {
      _isLoading = true;
      _error = null;
      // Reset pagination so page 1 is fetched from scratch.
      // Also clear _isLoadingMore so a stale _loadMore that was in-flight when
      // _load was triggered (e.g. pull-to-refresh during pagination) does not
      // leave the spinner stuck after the generation-mismatch early return fires.
      _offset = 0;
      _hasMore = true;
      _isLoadingMore = false;
    });

    try {
      final client = ref.read(apiClientProvider);
      final items = await client.listEpisodes(
        widget.setId,
        limit: _kEpisodePageSize,
        offset: 0,
      );

      if (!mounted || generation != _loadGeneration) return;

      setState(() {
        _episodes = items;
        _isLoading = false;
        _offset = items.length;
        _hasMore = items.length >= _kEpisodePageSize;
      });
    } catch (e) {
      if (!mounted || generation != _loadGeneration) return;
      setState(() {
        _error = episodeListErrorMessage(e);
        _isLoading = false;
      });
    }
  }

  /// Appends the next page of episodes to the existing list.
  ///
  /// Guards against concurrent loads and stops when all pages have been
  /// fetched ([_hasMore] is false).  Checks [_loadGeneration] so a pending
  /// refresh discards this stale response.
  Future<void> _loadMore() async {
    if (_isLoadingMore || !_hasMore) return;
    if (!mounted) return;

    final generation = _loadGeneration;
    setState(() => _isLoadingMore = true);

    try {
      final client = ref.read(apiClientProvider);
      final items = await client.listEpisodes(
        widget.setId,
        limit: _kEpisodePageSize,
        offset: _offset,
      );

      if (!mounted || generation != _loadGeneration) return;

      setState(() {
        _episodes = [...?_episodes, ...items];
        _offset += items.length;
        _hasMore = items.length >= _kEpisodePageSize;
        _isLoadingMore = false;
      });
    } catch (_) {
      // On error, allow the user to scroll again to retry.
      if (!mounted) return;
      setState(() => _isLoadingMore = false);
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
  // Download
  // ---------------------------------------------------------------------------

  /// Triggers a server-side download of the episode at [index] and updates
  /// the row with the returned [Media.id] so the play button becomes active.
  ///
  /// The download is initiated on the server; the returned [Media] object
  /// carries the newly created [mediaId].  On success the episode row is
  /// updated in-place so the play button appears without a full reload.
  /// On failure the original row is preserved and a SnackBar is shown.
  ///
  /// Double-tap protection: [_pendingDownloads] prevents concurrent API calls
  /// for the same episode if the user taps the download button multiple times
  /// before the first await returns.
  Future<void> _downloadEpisodeAt(int index) async {
    final items = _episodes;
    if (items == null || index < 0 || index >= items.length) return;

    final original = items[index];
    // Guard: do not re-download an episode that already has a media file.
    if (original.mediaId != null) return;

    // Guard: ignore duplicate taps while a download is already in-flight.
    if (_pendingDownloads.contains(original.id)) return;
    setState(() => _pendingDownloads.add(original.id));

    try {
      final client = ref.read(apiClientProvider);
      final media = await client.downloadEpisode(original.id);
      if (!mounted) return;

      // Update the episode row with the server-assigned mediaId so the play
      // button appears without waiting for a full page reload.
      // Read _episodes fresh inside setState: _load() may have completed during
      // the await, and using the pre-await snapshot would silently overwrite
      // fresher data.  Also guard that the index is still valid and that the
      // row still lacks a mediaId (i.e. it hasn't been updated by _load()).
      final updated = PodcastEpisode.fromJson(
        original.toJson()
          ..['media_id'] = media.id
          ..['is_downloaded'] = true,
      );
      setState(() {
        final current = _episodes;
        if (current != null &&
            index < current.length &&
            current[index].mediaId == null) {
          _episodes = List<PodcastEpisode>.from(current)..[index] = updated;
        }
      });
    } catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(episodeDownloadErrorMessage(e))),
      );
    } finally {
      // Always clear the pending flag so the button is re-enabled regardless
      // of success or failure.
      if (mounted) setState(() => _pendingDownloads.remove(original.id));
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
  ///   - Scrollable list of episode rows once data is available (with bottom
  ///     loading indicator while more pages are being fetched).
  ///
  /// [NotificationListener] wraps the [RefreshIndicator] and intercepts
  /// [ScrollUpdateNotification] to trigger [_loadMore] near the list end.
  /// Returning false from [_onScrollNotification] ensures the notification
  /// continues to bubble so [RefreshIndicator]'s overscroll detection still
  /// works correctly.
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

    // [NotificationListener] sits outside [RefreshIndicator] and listens for
    // scroll updates from the inner [ListView] to trigger infinite-scroll
    // page loads.  The [RefreshIndicator] receives notifications too because
    // [_onScrollNotification] returns false (non-consuming).
    return NotificationListener<ScrollNotification>(
      onNotification: _onScrollNotification,
      child: RefreshIndicator(
        onRefresh: _load,
        child: _episodes == null || _episodes!.isEmpty
            ? const _EmptyView()
            : _EpisodeList(
                episodes: _episodes!,
                pendingDownloads: _pendingDownloads,
                onToggleComplete: _toggleCompleteAt,
                onDownload: _downloadEpisodeAt,
                // mediaId is non-null: _EpisodeRow only invokes onPlay when episode.mediaId is set.
                onPlay: (mediaId) => context.go(
                  AppRoutes.audioPlayerPath(mediaId.toString()),
                ),
                isLoadingMore: _isLoadingMore,
                hasMore: _hasMore,
              ),
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// Sub-widgets
// ---------------------------------------------------------------------------

/// Scrollable list of episode rows with infinite-scroll pagination.
///
/// Extracted into its own stateless widget so [_PodcastEpisodesScreenState]
/// stays concise and the list layout is independently testable.
///
/// A footer item is appended after the last episode row: a spinner while more
/// pages are loading, or an end-of-list message once all pages are fetched.
///
/// Scroll detection is handled externally via a [NotificationListener] in
/// the parent state (rather than a [ScrollController] attached to this
/// [ListView]) so that [RefreshIndicator]'s overscroll detection is not
/// interfered with.
class _EpisodeList extends StatelessWidget {
  const _EpisodeList({
    required this.episodes,
    required this.pendingDownloads,
    required this.onToggleComplete,
    required this.onDownload,
    required this.onPlay,
    required this.isLoadingMore,
    required this.hasMore,
  });

  final List<PodcastEpisode> episodes;

  /// Set of episode IDs whose download is currently in-flight.
  ///
  /// Passed down from the state class so each [_EpisodeRow] can visually
  /// disable its download button while the request is pending.
  final Set<int> pendingDownloads;

  /// Called with the index of the episode whose played state was tapped.
  ///
  /// Using an index (rather than the episode itself) lets the state class
  /// update the correct position in its list without a linear search.
  final void Function(int index) onToggleComplete;

  /// Called with the index of the episode to be downloaded from the server.
  final void Function(int index) onDownload;

  /// Called with the [mediaId] of the episode to play.
  ///
  /// Only invoked when the episode already has a [PodcastEpisode.mediaId]
  /// (i.e. it has been downloaded and a Media row exists on the server).
  final void Function(int mediaId) onPlay;

  /// True while a next-page request is in flight; drives the bottom spinner.
  final bool isLoadingMore;

  /// False once all pages have been loaded; drives the end-of-list text.
  final bool hasMore;

  @override
  Widget build(BuildContext context) {
    // Total item count includes one footer slot after the last episode row.
    final totalCount = episodes.length + 1;

    return ListView.separated(
      key: const Key('episodes_list'),
      // +1 for the footer (loading indicator or end-of-list message).
      itemCount: totalCount,
      separatorBuilder: (_, index) =>
          // Do not draw a divider above the footer item.
          index < episodes.length - 1
              ? const Divider(height: 1)
              : const SizedBox.shrink(),
      itemBuilder: (context, index) {
        // Last slot is the footer.
        if (index == episodes.length) {
          return _buildFooter(context);
        }
        return _EpisodeRow(
          episode: episodes[index],
          isDownloadPending: pendingDownloads.contains(episodes[index].id),
          onToggleComplete: () => onToggleComplete(index),
          onDownload: () => onDownload(index),
          onPlay: onPlay,
        );
      },
    );
  }

  /// Builds the footer widget appended after the last episode row.
  ///
  /// Shows a spinner while more pages are loading, or an "All episodes loaded"
  /// text once [hasMore] is false.
  Widget _buildFooter(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 16),
      child: isLoadingMore
          ? const Center(
              key: Key('episodes_loading_more'),
              child: CircularProgressIndicator(),
            )
          : Center(
              child: Text(
                hasMore ? '' : 'All episodes loaded',
                key: const Key('episodes_no_more'),
                style: Theme.of(context).textTheme.bodySmall?.copyWith(
                      color: Theme.of(context).colorScheme.onSurfaceVariant,
                    ),
              ),
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
///   - Action buttons on the trailing edge:
///       * Play button (when [PodcastEpisode.mediaId] is non-null).
///       * Download button (when [PodcastEpisode.mediaId] is null, i.e. not yet
///         downloaded from the remote feed to the server's media library).
///       * Checkmark toggle reflecting [PodcastEpisode.isCompleted].
class _EpisodeRow extends StatelessWidget {
  const _EpisodeRow({
    required this.episode,
    required this.isDownloadPending,
    required this.onToggleComplete,
    required this.onDownload,
    required this.onPlay,
  });

  final PodcastEpisode episode;

  /// True while this episode's download request is in-flight.
  ///
  /// Forwarded to [_DownloadButton] to visually disable it and prevent
  /// additional taps from firing concurrent API calls.
  final bool isDownloadPending;

  /// Called when the user taps the played/unplayed icon.
  ///
  /// The parent state performs the optimistic update and API call; this
  /// widget is purely presentational (Single Responsibility / DIP).
  final VoidCallback onToggleComplete;

  /// Called when the user taps the download icon.
  ///
  /// Only shown when [episode.mediaId] is null (episode not yet downloaded).
  final VoidCallback onDownload;

  /// Called with the episode's [mediaId] when the user taps the play icon.
  ///
  /// Only shown when [episode.mediaId] is non-null (episode is downloaded).
  final void Function(int mediaId) onPlay;

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
          const SizedBox(width: 4),
          // Show play or download depending on whether the media file exists.
          if (episode.mediaId != null)
            _PlayButton(
              episodeId: episode.id,
              onTap: () => onPlay(episode.mediaId!),
            )
          else
            _DownloadButton(
              episodeId: episode.id,
              isLoading: isDownloadPending,
              onTap: onDownload,
            ),
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

/// Shared icon-button primitive used by [_PlayButton] and [_DownloadButton].
///
/// Both buttons are structurally identical — a [GestureDetector] wrapping a
/// padded [Icon] — and differ only in key string, icon data, and color.
/// Extracting this base widget eliminates the duplication (DRY) while keeping
/// each caller widget as a thin, readable wrapper (Single Responsibility).
///
/// When [isLoading] is true the tap is suppressed, providing a visual and
/// interactive disabled state without needing a separate StatefulWidget.
class _EpisodeActionButton extends StatelessWidget {
  const _EpisodeActionButton({
    required this.widgetKey,
    required this.icon,
    required this.color,
    required this.onTap,
    this.isLoading = false,
  });

  /// Widget key forwarded directly to the [GestureDetector] so callers can
  /// assign test-discoverable keys (e.g. `Key('episode_play_button_42')`).
  final Key widgetKey;

  final IconData icon;
  final Color color;
  final VoidCallback onTap;

  /// When true, taps are ignored and the icon is dimmed to signal that an
  /// operation is already in-flight (prevents duplicate API calls).
  final bool isLoading;

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      key: widgetKey,
      behavior: HitTestBehavior.opaque,
      // Suppress taps while loading to act as a lightweight disabled state.
      onTap: isLoading ? null : onTap,
      child: Padding(
        padding: const EdgeInsets.all(4),
        child: Icon(
          icon,
          size: 24,
          // Dim the icon when loading so the user has visual feedback that
          // the button is temporarily inactive.
          color: isLoading ? color.withAlpha(100) : color,
        ),
      ),
    );
  }
}

/// Icon button that opens the [AudioPlayerScreen] for a downloaded episode.
///
/// Shown in [_EpisodeRow] only when [PodcastEpisode.mediaId] is non-null,
/// meaning the episode has been downloaded and a [Media] row exists.
/// Delegates rendering to [_EpisodeActionButton] (DRY).
class _PlayButton extends StatelessWidget {
  const _PlayButton({
    required this.episodeId,
    required this.onTap,
  });

  final int episodeId;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return _EpisodeActionButton(
      widgetKey: Key('episode_play_button_$episodeId'),
      icon: Icons.play_circle_outline,
      color: Theme.of(context).colorScheme.primary,
      onTap: onTap,
    );
  }
}

/// Icon button that triggers a server-side download of an episode.
///
/// Shown in [_EpisodeRow] only when [PodcastEpisode.mediaId] is null —
/// meaning the episode has not yet been fetched from the remote feed URL
/// into the server's media library.  Once downloaded, the server creates a
/// [Media] row and [PodcastEpisode.mediaId] becomes non-null, replacing this
/// button with [_PlayButton] in the next render cycle.
/// Delegates rendering to [_EpisodeActionButton] (DRY).
class _DownloadButton extends StatelessWidget {
  const _DownloadButton({
    required this.episodeId,
    required this.isLoading,
    required this.onTap,
  });

  final int episodeId;

  /// True while the download request is in-flight; passed to
  /// [_EpisodeActionButton] to visually disable the button.
  final bool isLoading;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return _EpisodeActionButton(
      widgetKey: Key('episode_download_button_$episodeId'),
      icon: Icons.download_outlined,
      color: Theme.of(context).colorScheme.onSurfaceVariant,
      isLoading: isLoading,
      onTap: onTap,
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
