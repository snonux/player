import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../app_routes.dart';
import '../models/models.dart';
import '../providers/api_client_provider.dart';
import '../utils/duration_formatter.dart';
import '../utils/error_mappers.dart';
import '../widgets/search_filter_bar.dart';

// ---------------------------------------------------------------------------
// _buildMediaWithFavorite  (file-private helper)
// ---------------------------------------------------------------------------

/// Returns a copy of [media] with the [favorite] flag replaced.
///
/// [Media] is immutable, so we rebuild via [Media.fromJson] / [Media.toJson]
/// to avoid coupling the grid screen to any `copyWith` generated method.
/// Extracted as a file-private function so both [_MediaGridScreenState] and
/// the card overlay can share it without adding a public model API
/// (Dependency Inversion, DRY).
Media _buildMediaWithFavorite(Media media, bool favorite) {
  final json = media.toJson()..['favorite'] = favorite;
  return Media.fromJson(json);
}

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
///   - Infinite-scroll pagination: [ScrollController] detects when the user
///     is within 200px of the bottom and calls [_loadMore] to append the next
///     page.  Pull-to-refresh resets to page 1.
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

// Number of items requested per page.  Matches the server's supported range
// (1–1000); 50 is a comfortable default that avoids very long first loads.
const _kPageSize = 50;

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

  // Pagination state: current offset into the server list.
  int _offset = 0;

  // True when more pages may be available (last page was full).
  // Set to false when a page returns fewer items than [_kPageSize].
  bool _hasMore = true;

  // True while a _loadMore request is in flight, to prevent concurrent loads.
  bool _isLoadingMore = false;

  // Drives automatic page loads when the user scrolls near the list end.
  late final ScrollController _scrollController;

  @override
  void initState() {
    super.initState();
    _scrollController = ScrollController()..addListener(_onScroll);
    // Defer the first load until after the first frame so [ref] is fully bound
    // and any provider overrides in the test environment are applied.
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
  }

  @override
  void dispose() {
    // Cancel the scroll listener and release the controller to prevent
    // callbacks from firing after the widget is removed from the tree.
    _scrollController.dispose();
    super.dispose();
  }

  // ---------------------------------------------------------------------------
  // Scroll listener
  // ---------------------------------------------------------------------------

  /// Triggers [_loadMore] when the scroll position is within 200px of the end.
  ///
  /// Using a pixel threshold (rather than "at max extent") gives the user a
  /// smooth experience: the next page starts loading before they reach the
  /// very last item.
  void _onScroll() {
    final pos = _scrollController.position;
    if (pos.pixels >= pos.maxScrollExtent - 200) {
      _loadMore();
    }
  }

  // ---------------------------------------------------------------------------
  // Data loading
  // ---------------------------------------------------------------------------

  /// Fetches the first page of media for [widget.setId] with the current
  /// [_filter] and resets all pagination state.
  ///
  /// Called on first mount, on pull-to-refresh, and whenever [_filter]
  /// changes.  Resetting [_offset] to 0 and [_hasMore] to true ensures that
  /// subsequent scroll-triggered loads start cleanly from the beginning.
  ///
  /// The [_loadGeneration] counter ensures that a response arriving after a
  /// newer load has started is ignored, preventing stale data from overwriting
  /// fresher results (cancellation-by-generation pattern).
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
      // Reset pagination so the first page is fetched from the start.
      // Also clear _isLoadingMore so a stale _loadMore that was in-flight when
      // _load was triggered (e.g. pull-to-refresh during pagination) does not
      // leave the spinner stuck after the generation-mismatch early return fires.
      _offset = 0;
      _hasMore = true;
      _isLoadingMore = false;
    });

    try {
      final client = ref.read(apiClientProvider);
      final items = await client.listMedia(
        setId: widget.setId,
        search: _filter.query,
        type: _filter.type,
        favorites: _filter.favoritesOnly ? true : null,
        sort: _filter.sortBy,
        limit: _kPageSize,
        offset: 0,
      );

      // Discard the result if a newer load was started while this one was
      // in flight (filter changed, pull-to-refresh, etc.).
      if (!mounted || generation != _loadGeneration) return;

      setState(() {
        _media = items;
        _isLoading = false;
        _offset = items.length;
        // If fewer items than requested were returned, there are no more pages.
        _hasMore = items.length >= _kPageSize;
      });
    } catch (e) {
      if (!mounted || generation != _loadGeneration) return;
      setState(() {
        _error = mediaErrorMessage(e);
        _isLoading = false;
      });
    }
  }

  /// Appends the next page of media items to the existing list.
  ///
  /// Guards against concurrent loads ([_isLoadingMore]) and stops when all
  /// pages have been fetched ([_hasMore] is false).  Uses the current
  /// [_loadGeneration] so a pending [_load] (e.g. filter change or
  /// pull-to-refresh) that starts a new generation will cause this callback
  /// to discard its stale result.
  Future<void> _loadMore() async {
    if (_isLoadingMore || !_hasMore) return;
    if (!mounted) return;

    final generation = _loadGeneration;
    setState(() => _isLoadingMore = true);

    try {
      final client = ref.read(apiClientProvider);
      final items = await client.listMedia(
        setId: widget.setId,
        search: _filter.query,
        type: _filter.type,
        favorites: _filter.favoritesOnly ? true : null,
        sort: _filter.sortBy,
        limit: _kPageSize,
        offset: _offset,
      );

      // Discard if a fresher load (e.g. pull-to-refresh) has started.
      if (!mounted || generation != _loadGeneration) return;

      setState(() {
        _media = [...?_media, ...items];
        _offset += items.length;
        _hasMore = items.length >= _kPageSize;
        _isLoadingMore = false;
      });
    } catch (_) {
      // On error, allow the user to scroll again to retry.
      if (!mounted) return;
      setState(() => _isLoadingMore = false);
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
  // Favourite toggle
  // ---------------------------------------------------------------------------

  /// Optimistically flips the favourite flag on the item at [index], calls
  /// [toggleFavorite] on the server, then reconciles with the confirmed state.
  ///
  /// On error the optimistic update is reverted and a SnackBar is shown.
  ///
  /// Guard: if [_media] is null or [index] is out of range the call is a no-op.
  Future<void> _toggleFavoriteAt(int index) async {
    final items = _media;
    if (items == null || index < 0 || index >= items.length) return;

    final original = items[index];
    final optimistic = _buildMediaWithFavorite(original, !original.favorite);

    // Apply optimistic update immediately so the icon flips without lag.
    setState(() {
      _media = List<Media>.from(items)..[index] = optimistic;
    });

    try {
      final client = ref.read(apiClientProvider);
      final confirmed = await client.toggleFavorite(original.id);
      if (!mounted) return;
      // Reconcile with the value the server actually stored.
      setState(() {
        final current = _media;
        if (current != null && index < current.length) {
          _media = List<Media>.from(current)
            ..[index] = _buildMediaWithFavorite(current[index], confirmed);
        }
      });
    } catch (_) {
      if (!mounted) return;
      // Revert the optimistic update on failure.
      setState(() {
        final current = _media;
        if (current != null && index < current.length) {
          _media = List<Media>.from(current)..[index] = original;
        }
      });
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text('Could not update favourite. Try again.'),
        ),
      );
    }
  }

  /// Toggles the [MediaFilter.favoritesOnly] flag and reloads.
  ///
  /// Called by the app-bar heart icon button as a fast shortcut so the user
  /// can show/hide favourites without opening [SearchFilterBar].  The filter
  /// state is kept in sync with [SearchFilterBar] via [_filter] so both
  /// controls always reflect the same state.
  void _toggleFavoritesFilter() {
    _onFiltersChanged(_filter.copyWith(favoritesOnly: !_filter.favoritesOnly));
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
  ///
  /// Includes a heart icon button as a quick toggle for [MediaFilter.favoritesOnly].
  /// The icon is filled and highlighted when the filter is active so the user
  /// always knows at a glance whether the favourites-only view is on.
  AppBar _buildAppBar() {
    return AppBar(
      title: Text(widget.setName ?? 'Set ${widget.setId}'),
      actions: [
        IconButton(
          key: const Key('media_grid_favorites_filter'),
          tooltip: _filter.favoritesOnly ? 'Show all items' : 'Show favourites only',
          icon: Icon(
            _filter.favoritesOnly ? Icons.favorite : Icons.favorite_border,
            color: _filter.favoritesOnly
                ? Theme.of(context).colorScheme.error
                : null,
          ),
          onPressed: _toggleFavoritesFilter,
        ),
      ],
    );
  }

  /// Delegates to the appropriate state widget:
  ///   - Loading spinner (first load, before any data arrives).
  ///   - Error view with a retry button.
  ///   - Empty-state message when [listMedia] returns an empty list.
  ///   - Grid of media cards once data is available (with bottom loading
  ///     indicator while more pages are being fetched).
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
              onFavoriteToggle: _toggleFavoriteAt,
              scrollController: _scrollController,
              isLoadingMore: _isLoadingMore,
              hasMore: _hasMore,
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

/// Scrollable grid of [Media] cards with infinite-scroll pagination.
///
/// Extracted from [_MediaGridScreenState] so the state class stays concise and
/// the grid layout is independently testable.
///
/// The grid is driven by a [CustomScrollView] with two slivers so an end-of-list
/// indicator (spinner or "No more items" text) can be appended after the last
/// row without breaking the grid layout.
class _MediaGrid extends StatelessWidget {
  const _MediaGrid({
    required this.media,
    required this.thumbnailUrlBuilder,
    required this.onFavoriteToggle,
    required this.scrollController,
    required this.isLoadingMore,
    required this.hasMore,
  });

  final List<Media> media;

  /// Callback that returns the full thumbnail URL for a given media ID.
  ///
  /// Injected rather than computed inline so the widget has no knowledge of
  /// base-URL or API path structure (Dependency Inversion).
  final String Function(int mediaId) thumbnailUrlBuilder;

  /// Called when the user taps the heart icon on a card.
  ///
  /// The argument is the [index] of the item within [media].  Using an index
  /// (rather than the item itself) lets the state class update the correct
  /// position in its list without a linear search.
  final void Function(int index) onFavoriteToggle;

  /// Controls the scroll position and triggers page loads via the listener
  /// attached in [_MediaGridScreenState].
  final ScrollController scrollController;

  /// True while a next-page request is in flight; drives the bottom spinner.
  final bool isLoadingMore;

  /// False once all pages have been loaded; drives the end-of-list text.
  final bool hasMore;

  @override
  Widget build(BuildContext context) {
    return CustomScrollView(
      key: const Key('media_grid'),
      controller: scrollController,
      slivers: [
        _buildGridSliver(),
        _buildFooterSliver(context),
      ],
    );
  }

  /// Builds the main grid sliver containing all loaded media cards.
  SliverPadding _buildGridSliver() {
    return SliverPadding(
      padding: const EdgeInsets.all(12),
      sliver: SliverGrid(
        gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
          crossAxisCount: 2,
          crossAxisSpacing: 12,
          mainAxisSpacing: 12,
          // Slightly taller than square to accommodate the info overlay.
          childAspectRatio: 0.85,
        ),
        delegate: SliverChildBuilderDelegate(
          (context, index) => _MediaCard(
            item: media[index],
            thumbnailUrl: thumbnailUrlBuilder(media[index].id),
            onFavoriteToggle: () => onFavoriteToggle(index),
          ),
          childCount: media.length,
        ),
      ),
    );
  }

  /// Builds the footer sliver: spinner while loading more, or end-of-list text.
  ///
  /// The footer is always present (a single-item list sliver) so the
  /// [CustomScrollView] can always scroll past the last grid row, which
  /// prevents the scroll listener from never triggering on short lists.
  SliverToBoxAdapter _buildFooterSliver(BuildContext context) {
    return SliverToBoxAdapter(
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 16),
        // When more pages are coming but none is in-flight, render an invisible
        // placeholder (SizedBox.shrink) instead of an empty Text so the layout
        // does not reserve unnecessary vertical space.
        child: isLoadingMore
            ? const Center(
                key: Key('media_loading_more'),
                child: CircularProgressIndicator(),
              )
            : hasMore
                ? const SizedBox.shrink()
                : Center(
                    child: Text(
                      'No more items',
                      key: const Key('media_no_more'),
                      style: Theme.of(context).textTheme.bodySmall?.copyWith(
                            color:
                                Theme.of(context).colorScheme.onSurfaceVariant,
                          ),
                    ),
                  ),
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
///   - A heart icon in the bottom-right corner of the thumbnail that reflects
///     the favourite state and fires [onFavoriteToggle] when tapped.
///
/// Tapping the card body navigates to [AppRoutes.mediaDetailPath] for the item.
/// Tapping the heart icon triggers the favourite toggle without navigating.
class _MediaCard extends StatelessWidget {
  const _MediaCard({
    required this.item,
    required this.thumbnailUrl,
    required this.onFavoriteToggle,
  });

  final Media item;
  final String thumbnailUrl;

  /// Called when the heart icon is tapped.  The parent state performs the
  /// optimistic update and API call; this widget is purely presentational
  /// (Single Responsibility, Dependency Inversion).
  final VoidCallback onFavoriteToggle;

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
            // Heart icon anchored to the bottom-right of the card.
            // Positioned inside the info overlay's gradient area so it blends
            // visually. [GestureDetector] is used so taps on the icon do NOT
            // propagate to the [InkWell] above (which would navigate).
            Positioned(
              right: 4,
              bottom: 4,
              child: _FavoriteIconButton(
                isFavorite: item.favorite,
                mediaId: item.id,
                onTap: onFavoriteToggle,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// _FavoriteIconButton
// ---------------------------------------------------------------------------

/// Small heart icon rendered on top of a media card thumbnail.
///
/// Uses a [GestureDetector] with [HitTestBehavior.opaque] to consume the tap
/// before it reaches the parent [InkWell], preventing card-navigation from
/// firing when the user taps the heart.
///
/// Design notes:
///   - Extracted as a separate widget so it is independently testable and
///     keeps [_MediaCard.build] under 30 lines (Single Responsibility).
///   - The icon is styled with a dark shadow so it remains legible over both
///     light and dark thumbnails.
class _FavoriteIconButton extends StatelessWidget {
  const _FavoriteIconButton({
    required this.isFavorite,
    required this.mediaId,
    required this.onTap,
  });

  final bool isFavorite;

  /// Used only for the widget key so tests can find the button by media ID.
  final int mediaId;

  /// Called when the user taps the heart; no navigation occurs.
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      key: Key('media_card_favorite_$mediaId'),
      behavior: HitTestBehavior.opaque,
      onTap: onTap,
      child: Icon(
        isFavorite ? Icons.favorite : Icons.favorite_border,
        size: 20,
        color: isFavorite ? Colors.redAccent : Colors.white70,
        shadows: const [
          Shadow(
            color: Colors.black54,
            blurRadius: 4,
          ),
        ],
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
  /// Delegates to the shared [formatDuration] helper in `duration_formatter.dart`
  /// (DRY) so the formatting logic lives in exactly one place.
  static String _formatDuration(double seconds) => formatDuration(seconds);
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
