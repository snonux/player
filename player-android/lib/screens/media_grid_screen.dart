import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../app_routes.dart';
import '../models/models.dart';
import '../providers/api_client_provider.dart';
import '../utils/error_mappers.dart';
import '../widgets/search_filter_bar.dart';

/// Displays the media items inside a single [MediaSet] as a scrollable grid.
///
/// Each card shows the item's thumbnail, title (file name), media-type icon
/// (video / audio / image), and formatted duration.  Tapping a card navigates
/// to [MediaDetailScreen] via `/media/:id`.
///
/// Design notes:
///   - [ConsumerStatefulWidget] allows local loading/error state, [mounted]
///     guards on async continuations, and pull-to-refresh without lifting
///     state into a global Riverpod notifier.
///   - [setId] is a constructor parameter (not route global state) so the
///     screen is independently testable and reusable for any set.
///   - [setName] is an optional display label passed as a route extra; the
///     app bar falls back to "Set $setId" when it is absent.
///   - Error handling is fully delegated to top-level helpers in
///     `error_mappers.dart` — no `dio` import in this file (DIP).
///   - [SearchFilterBar] is shown below the AppBar; filter changes cancel
///     any in-flight load and start a new one with the updated parameters.
class MediaGridScreen extends ConsumerStatefulWidget {
  /// The numeric identifier of the set whose media items will be displayed.
  final int setId;

  /// Optional human-readable name of the set shown in the app bar.
  ///
  /// Pass this as a route extra from the calling screen so the app bar shows
  /// the set name immediately without a separate API call.
  final String? setName;

  const MediaGridScreen({super.key, required this.setId, this.setName});

  @override
  ConsumerState<MediaGridScreen> createState() => _MediaGridScreenState();
}

class _MediaGridScreenState extends ConsumerState<MediaGridScreen> {
  // Nullable: null means "not yet loaded" (loading indicator is shown).
  List<Media>? _media;

  // Non-null when the last load attempt failed.
  String? _error;

  // True while the initial or refresh load is in flight.
  bool _isLoading = false;

  // Current filter state; starts with no filters applied.
  MediaFilter _filter = const MediaFilter();

  // Generation counter — incremented whenever a new load is started.
  // The async callback checks this value before updating state so that a
  // stale response from a cancelled logical request is silently discarded,
  // preventing race conditions when the user changes filters rapidly.
  int _loadGeneration = 0;

  @override
  void initState() {
    super.initState();
    // Defer the first load until after the first frame so [ref] is fully bound
    // and any provider overrides in the test environment are applied.
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
  }

  // ---------------------------------------------------------------------------
  // Data loading
  // ---------------------------------------------------------------------------

  /// Fetches media items for [widget.setId] with the current [_filter] and
  /// updates local state.
  ///
  /// Called on first mount, on pull-to-refresh, and whenever [_filter]
  /// changes.  The [_loadGeneration] counter ensures that a response arriving
  /// after a newer load has started is ignored, preventing stale data from
  /// overwriting fresher results (cancellation-by-generation pattern).
  ///
  /// Errors are mapped by the top-level [mediaErrorMessage] helper so the
  /// widget stays free of Dio.
  Future<void> _load() async {
    if (!mounted) return;

    // Bump the generation before the async gap so that any pending callback
    // from the previous load detects the change and drops its result.
    final generation = ++_loadGeneration;

    setState(() {
      _isLoading = true;
      _error = null;
    });

    try {
      final client = ref.read(apiClientProvider);
      final items = await client.listMedia(
        setId: widget.setId,
        search: _filter.query,
        type: _filter.type,
        favorites: _filter.favoritesOnly ? true : null,
        sort: _filter.sortBy,
      );

      // Discard the result if a newer load was started while this one was
      // in flight (filter changed, pull-to-refresh, etc.).
      if (!mounted || generation != _loadGeneration) return;

      setState(() {
        _media = items;
        _isLoading = false;
      });
    } catch (e) {
      if (!mounted || generation != _loadGeneration) return;
      setState(() {
        _error = mediaErrorMessage(e);
        _isLoading = false;
      });
    }
  }

  /// Called by [SearchFilterBar] when any filter dimension changes.
  ///
  /// Stores the new filter and immediately starts a new load.  The generation
  /// counter in [_load] ensures the previous in-flight request is logically
  /// cancelled even though the underlying Future cannot be cancelled.
  void _onFiltersChanged(MediaFilter filter) {
    setState(() => _filter = filter);
    _load();
  }

  // ---------------------------------------------------------------------------
  // Build
  // ---------------------------------------------------------------------------

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: _buildAppBar(),
      body: Column(
        children: [
          // Search/filter bar sits directly below the AppBar.
          SearchFilterBar(
            initialFilter: _filter,
            onFiltersChanged: _onFiltersChanged,
          ),
          // The remaining space is occupied by the data body.
          Expanded(child: _buildBody(context)),
        ],
      ),
    );
  }

  /// Builds the app bar, showing [widget.setName] when available.
  AppBar _buildAppBar() {
    return AppBar(
      title: Text(widget.setName ?? 'Set ${widget.setId}'),
    );
  }

  /// Delegates to the appropriate state widget:
  ///   - Loading spinner (first load, before any data arrives).
  ///   - Error view with a retry button.
  ///   - Empty-state message when [listMedia] returns an empty list.
  ///   - Grid of media cards once data is available.
  Widget _buildBody(BuildContext context) {
    // Show a full-screen spinner only on the very first load (no data yet).
    if (_isLoading && _media == null) {
      return const Center(
        key: Key('media_loading'),
        child: CircularProgressIndicator(),
      );
    }

    // Show an error view with a retry button if the load failed.
    if (_error != null) {
      return _ErrorView(message: _error!, onRetry: _load);
    }

    // [RefreshIndicator] wraps the scrollable content so pull-to-refresh
    // triggers [_load] on both the grid and the empty-state view.
    return RefreshIndicator(
      onRefresh: _load,
      child: _media == null || _media!.isEmpty
          ? const _EmptyView()
          : _MediaGrid(
              media: _media!,
              thumbnailUrlBuilder: _thumbnailUrl,
            ),
    );
  }

  /// Delegates thumbnail URL construction to [PlayerApiClient] so this screen
  /// stays free of Dio / URL-building logic (Single Responsibility).
  String _thumbnailUrl(int mediaId) {
    final client = ref.read(apiClientProvider);
    return client.thumbnailUrl(mediaId);
  }
}

// ---------------------------------------------------------------------------
// Sub-widgets
// ---------------------------------------------------------------------------

/// Scrollable grid of [Media] cards.
///
/// Extracted from [_MediaGridScreenState] so the state class stays concise and
/// the grid layout is independently testable.
class _MediaGrid extends StatelessWidget {
  const _MediaGrid({
    required this.media,
    required this.thumbnailUrlBuilder,
  });

  final List<Media> media;

  /// Callback that returns the full thumbnail URL for a given media ID.
  ///
  /// Injected rather than computed inline so the widget has no knowledge of
  /// base-URL or API path structure (Dependency Inversion).
  final String Function(int mediaId) thumbnailUrlBuilder;

  @override
  Widget build(BuildContext context) {
    return GridView.builder(
      key: const Key('media_grid'),
      padding: const EdgeInsets.all(12),
      // Two columns on phones; adaptive count could be added for tablets later.
      gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
        crossAxisCount: 2,
        crossAxisSpacing: 12,
        mainAxisSpacing: 12,
        // Slightly taller than square to accommodate the info overlay.
        childAspectRatio: 0.85,
      ),
      itemCount: media.length,
      itemBuilder: (context, index) => _MediaCard(
        item: media[index],
        thumbnailUrl: thumbnailUrlBuilder(media[index].id),
      ),
    );
  }
}

/// Material 3 card for a single [Media] item.
///
/// Shows:
///   - Thumbnail image with placeholder and error fallback.
///   - Semi-transparent overlay at the bottom with title, type icon, and
///     duration.
///
/// Tapping navigates to [AppRoutes.mediaDetailPath] for the item.
class _MediaCard extends StatelessWidget {
  const _MediaCard({required this.item, required this.thumbnailUrl});

  final Media item;
  final String thumbnailUrl;

  @override
  Widget build(BuildContext context) {
    return Card(
      key: Key('media_card_${item.id}'),
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: () => context.go(AppRoutes.mediaDetailPath(item.id)),
        child: Stack(
          fit: StackFit.expand,
          children: [
            // Thumbnail fills the full card area.
            _ThumbnailImage(thumbnailUrl: thumbnailUrl),
            // Info overlay anchored to the bottom of the card.
            Positioned(
              left: 0,
              right: 0,
              bottom: 0,
              child: _InfoOverlay(item: item),
            ),
          ],
        ),
      ),
    );
  }
}

/// Thumbnail image for a media card, loaded via [CachedNetworkImage].
///
/// Provides a grey placeholder while loading or when [thumbnailUrl] is empty,
/// and a broken-image icon on network error. Checking for empty URL before
/// attempting a network request mirrors the pattern used in [_CoverImage]
/// (home_screen.dart) and avoids unnecessary HTTP traffic when no thumbnail
/// is available.
class _ThumbnailImage extends StatelessWidget {
  const _ThumbnailImage({required this.thumbnailUrl});

  final String thumbnailUrl;

  @override
  Widget build(BuildContext context) {
    // When the URL is empty, skip the network request and show the placeholder
    // immediately — consistent with the set-cover image pattern.
    if (thumbnailUrl.isEmpty) return _placeholder(context);

    return CachedNetworkImage(
      imageUrl: thumbnailUrl,
      fit: BoxFit.cover,
      placeholder: (_, __) => _loading(),
      errorWidget: (_, __, ___) => _error(context),
    );
  }

  static Widget _loading() =>
      const Center(child: CircularProgressIndicator());

  static Widget _placeholder(BuildContext context) => ColoredBox(
        color: Theme.of(context).colorScheme.surfaceContainerHighest,
        child: Icon(
          Icons.image_outlined,
          size: 48,
          color: Theme.of(context).colorScheme.onSurfaceVariant,
        ),
      );

  static Widget _error(BuildContext context) => ColoredBox(
        color: Theme.of(context).colorScheme.surfaceContainerHighest,
        child: Icon(
          Icons.broken_image_outlined,
          size: 48,
          color: Theme.of(context).colorScheme.onSurfaceVariant,
        ),
      );
}

/// Semi-transparent overlay at the bottom of a media card.
///
/// Displays:
///   - Type icon (video camera / headphones / image).
///   - Title truncated to one line.
///   - Formatted duration.
///
/// The gradient background ensures text readability over any thumbnail.
class _InfoOverlay extends StatelessWidget {
  const _InfoOverlay({required this.item});

  final Media item;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
      decoration: BoxDecoration(
        gradient: LinearGradient(
          begin: Alignment.topCenter,
          end: Alignment.bottomCenter,
          colors: [
            Colors.transparent,
            Colors.black.withAlpha(200),
          ],
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisSize: MainAxisSize.min,
        children: [
          // Type icon + truncated title on the same row.
          Row(
            children: [
              Icon(
                _typeIcon(item.type),
                size: 14,
                color: Colors.white70,
              ),
              const SizedBox(width: 4),
              Expanded(
                child: Text(
                  item.fileName,
                  key: Key('media_title_${item.id}'),
                  style: const TextStyle(
                    color: Colors.white,
                    fontSize: 12,
                    fontWeight: FontWeight.w600,
                  ),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
            ],
          ),
          const SizedBox(height: 2),
          // Duration formatted as mm:ss or hh:mm:ss.
          Text(
            _formatDuration(item.duration),
            key: Key('media_duration_${item.id}'),
            style: const TextStyle(color: Colors.white70, fontSize: 11),
          ),
        ],
      ),
    );
  }

  /// Returns an appropriate icon for the given media [type].
  ///
  /// Falls back to [Icons.insert_drive_file_outlined] for unknown types.
  static IconData _typeIcon(String type) {
    switch (type) {
      case 'video':
        return Icons.videocam_outlined;
      case 'audio':
        return Icons.headphones_outlined;
      case 'image':
        return Icons.image_outlined;
      default:
        return Icons.insert_drive_file_outlined;
    }
  }

  /// Formats [seconds] as `h:mm:ss` or `m:ss`, omitting leading zeros.
  ///
  /// Uses integer arithmetic only — no Duration formatting dependency — to
  /// keep this helper lightweight and independently testable.
  static String _formatDuration(double seconds) {
    final total = seconds.truncate();
    final h = total ~/ 3600;
    final m = (total % 3600) ~/ 60;
    final s = total % 60;
    if (h > 0) {
      return '$h:${m.toString().padLeft(2, '0')}:${s.toString().padLeft(2, '0')}';
    }
    return '$m:${s.toString().padLeft(2, '0')}';
  }
}

/// Full-screen empty-state view, shown when [listMedia] returns an empty list.
///
/// Wrapped in a [ListView] with [AlwaysScrollableScrollPhysics] so the
/// [RefreshIndicator] parent can still trigger a pull-to-refresh gesture.
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
                Icons.video_library_outlined,
                size: 72,
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
              const SizedBox(height: 16),
              Text(
                'No media found',
                key: const Key('media_empty'),
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
/// Shown when [listMedia] throws (network error, server error, etc.).
/// The [message] comes from [mediaErrorMessage], which maps exceptions to
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
              key: const Key('media_error'),
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.bodyLarge,
            ),
            const SizedBox(height: 24),
            ElevatedButton.icon(
              key: const Key('media_retry'),
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
