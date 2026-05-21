import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../app_routes.dart';
import '../models/models.dart';
import '../providers/api_client_provider.dart';
import '../utils/error_mappers.dart';

/// Home screen: displays all media sets as a scrollable grid.
///
/// Each card shows the set's cover thumbnail, name, and media type badge
/// (podcast sets get a microphone icon overlay).  Tapping a card navigates
/// to [MediaGridScreen] for that set.
///
/// Design notes:
///   - [ConsumerStatefulWidget] is used so we can manage local loading and
///     error state, guard async continuations on [mounted], and call
///     [setState] to trigger rebuilds.
///   - [listSets] is called from [initState] and again on pull-to-refresh.
///     The result is stored locally rather than in a Riverpod provider because
///     this screen owns the full lifecycle (loading → data → refresh).
///   - Error handling distinguishes network/connectivity errors from server
///     errors using top-level helper functions (not instance methods), keeping
///     the error-mapping logic testable and decoupled from the widget.
class SetsListScreen extends ConsumerStatefulWidget {
  const SetsListScreen({super.key});

  @override
  ConsumerState<SetsListScreen> createState() => _SetsListScreenState();
}

class _SetsListScreenState extends ConsumerState<SetsListScreen> {
  // Nullable: null means "not yet loaded" (loading indicator is shown).
  List<MediaSet>? _sets;

  // Non-null when the last load attempt failed.
  String? _error;

  // True while the initial or refresh load is in flight.
  bool _isLoading = false;

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

  /// Fetches all sets from the server and updates local state.
  ///
  /// Called on first mount and on pull-to-refresh.  Errors are mapped by the
  /// top-level [setsErrorMessage] helper so the widget itself stays simple.
  Future<void> _load() async {
    if (!mounted) return;
    setState(() {
      _isLoading = true;
      _error = null;
    });

    try {
      final client = ref.read(apiClientProvider);
      final sets = await client.listSets();
      if (!mounted) return;
      setState(() {
        _sets = sets;
        _isLoading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = setsErrorMessage(e);
        _isLoading = false;
      });
    }
  }

  // ---------------------------------------------------------------------------
  // Build
  // ---------------------------------------------------------------------------

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: _buildAppBar(context),
      body: _buildBody(context),
    );
  }

  /// Builds the app bar with a Settings navigation icon.
  AppBar _buildAppBar(BuildContext context) {
    return AppBar(
      title: const Text('Library'),
      actions: [
        IconButton(
          key: const Key('home_settings_button'),
          icon: const Icon(Icons.settings_outlined),
          tooltip: 'Settings',
          // Navigate to the settings screen when the icon is tapped.
          onPressed: () => context.go(AppRoutes.settings),
        ),
      ],
    );
  }

  /// Builds the main body, delegating to the appropriate state widget:
  ///   - Loading spinner (first load, before any data arrives).
  ///   - Error view with a retry button.
  ///   - Empty-state message when the server returns an empty list.
  ///   - Grid of set cards once data is available.
  Widget _buildBody(BuildContext context) {
    // Show a full-screen spinner only on the very first load (no data yet).
    if (_isLoading && _sets == null) {
      return const Center(
        key: Key('sets_loading'),
        child: CircularProgressIndicator(),
      );
    }

    // Show an error view with a retry button if the load failed.
    if (_error != null) {
      return _ErrorView(
        message: _error!,
        onRetry: _load,
      );
    }

    // [RefreshIndicator] wraps the scrollable content so pull-to-refresh
    // triggers [_load] on both the grid and the empty-state view.
    return RefreshIndicator(
      onRefresh: _load,
      child: _sets == null || _sets!.isEmpty
          ? const _EmptyView()
          : _SetsGrid(sets: _sets!),
    );
  }
}

// ---------------------------------------------------------------------------
// Sub-widgets
// ---------------------------------------------------------------------------

/// Scrollable grid of [MediaSet] cards.
///
/// Extracted into its own stateless widget so [_SetsListScreenState] stays
/// below 50 lines and the grid layout is independently testable.
class _SetsGrid extends StatelessWidget {
  const _SetsGrid({required this.sets});

  final List<MediaSet> sets;

  @override
  Widget build(BuildContext context) {
    return GridView.builder(
      key: const Key('sets_grid'),
      padding: const EdgeInsets.all(12),
      // Two columns on phones; the cross-axis count could be made adaptive
      // for larger screens in a future iteration.
      gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
        crossAxisCount: 2,
        crossAxisSpacing: 12,
        mainAxisSpacing: 12,
        // Slightly taller than square to accommodate the title row below.
        childAspectRatio: 0.85,
      ),
      itemCount: sets.length,
      itemBuilder: (context, index) => _SetCard(mediaSet: sets[index]),
    );
  }
}

/// Material 3 card representing a single [MediaSet].
///
/// Shows: cover thumbnail (with placeholder and error fallback), set name,
/// and a podcast badge (microphone icon) for podcast sets.
///
/// Tapping navigates to [AppRoutes.mediaGridPath] for the set.
class _SetCard extends StatelessWidget {
  const _SetCard({required this.mediaSet});

  final MediaSet mediaSet;

  @override
  Widget build(BuildContext context) {
    return Card(
      key: Key('set_card_${mediaSet.id}'),
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        // Pass the set name as a route extra so MediaGridScreen can show it in
        // the app bar immediately, without making a second API call.
        onTap: () => context.go(
          AppRoutes.mediaGridPath(mediaSet.id),
          extra: mediaSet.name,
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            // Cover thumbnail: takes up ~70 % of the card height.
            Expanded(child: _CoverImage(mediaSet: mediaSet)),
            // Name row with podcast badge.
            _NameRow(mediaSet: mediaSet),
          ],
        ),
      ),
    );
  }
}

/// Displays the set's cover image via [CachedNetworkImage].
///
/// Falls back to a grey container with a folder icon when:
///   - [MediaSet.coverThumbnailPath] is empty.
///   - The network request fails.
///   - The image is still loading (shows a [CircularProgressIndicator]).
///
/// The podcast badge (microphone icon) is overlaid in the top-right corner
/// for sets where [MediaSet.isPodcast] is true.
class _CoverImage extends StatelessWidget {
  const _CoverImage({required this.mediaSet});

  final MediaSet mediaSet;

  @override
  Widget build(BuildContext context) {
    return Stack(
      fit: StackFit.expand,
      children: [
        // Cover thumbnail — use CachedNetworkImage to avoid re-downloading
        // on every rebuild and to provide placeholder/error states.
        if (mediaSet.coverThumbnailPath.isEmpty)
          _placeholderWidget(context)
        else
          CachedNetworkImage(
            imageUrl: mediaSet.coverThumbnailPath,
            fit: BoxFit.cover,
            placeholder: (_, __) => _loadingWidget(),
            errorWidget: (_, __, ___) => _placeholderWidget(context),
          ),

        // Podcast badge: microphone icon in the top-right corner.
        if (mediaSet.isPodcast)
          const Positioned(
            top: 6,
            right: 6,
            child: _PodcastBadge(),
          ),
      ],
    );
  }

  // Static helpers: neither uses [this], so they are class-scoped utilities
  // rather than instance methods.
  static Widget _loadingWidget() =>
      const Center(child: CircularProgressIndicator());

  static Widget _placeholderWidget(BuildContext context) => ColoredBox(
        color: Theme.of(context).colorScheme.surfaceContainerHighest,
        child: Icon(
          Icons.folder_outlined,
          size: 48,
          color: Theme.of(context).colorScheme.onSurfaceVariant,
        ),
      );
}

/// Podcast badge displayed on the cover thumbnail corner.
///
/// A small Material 3 filled badge containing a microphone icon, indicating
/// that the set is a podcast feed rather than a plain media collection.
class _PodcastBadge extends StatelessWidget {
  const _PodcastBadge();

  @override
  Widget build(BuildContext context) {
    return Container(
      key: const Key('podcast_badge'),
      padding: const EdgeInsets.all(4),
      decoration: BoxDecoration(
        color: Theme.of(context).colorScheme.primary,
        borderRadius: BorderRadius.circular(6),
      ),
      child: Icon(
        Icons.mic,
        size: 16,
        color: Theme.of(context).colorScheme.onPrimary,
      ),
    );
  }
}

/// Name row below the cover thumbnail.
///
/// Shows the set name in a single line (ellipsis on overflow).
class _NameRow extends StatelessWidget {
  const _NameRow({required this.mediaSet});

  final MediaSet mediaSet;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
      child: Text(
        mediaSet.name,
        style: Theme.of(context).textTheme.bodyMedium?.copyWith(
              fontWeight: FontWeight.w600,
            ),
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
      ),
    );
  }
}

/// Full-screen empty-state view, shown when [listSets] returns an empty list.
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
                Icons.video_library_outlined,
                size: 72,
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
              const SizedBox(height: 16),
              Text(
                'No sets found',
                key: const Key('sets_empty'),
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
/// Shown when [listSets] throws (network error, server error, etc.).
/// The [message] comes from [setsErrorMessage], which maps exceptions to
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
              key: const Key('sets_error'),
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.bodyLarge,
            ),
            const SizedBox(height: 24),
            ElevatedButton.icon(
              key: const Key('sets_retry'),
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

// ---------------------------------------------------------------------------
// Top-level error-mapping helpers
// ---------------------------------------------------------------------------
//
// [setsErrorMessage] is now defined in ../utils/error_mappers.dart and
// re-exported via the import above, keeping the screen layer free of the
// package:dio/dio.dart dependency (DIP fix).
