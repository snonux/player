import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../app_routes.dart';
import '../models/models.dart';
import '../providers/api_client_provider.dart';
import '../utils/error_mappers.dart';
import 'subscribe_dialog.dart';

/// Podcast list screen: displays only sets where [MediaSet.isPodcast] is true.
///
/// Each list tile shows the feed's cover thumbnail, feed name, and a
/// microphone icon indicating it is a podcast feed.  Tapping a tile
/// navigates to [MediaGridScreen] for that set's episodes.  A FAB opens the
/// [showSubscribeDialog] to add a new podcast feed.
///
/// Design notes:
///   - [ConsumerStatefulWidget] allows local loading/error state, [mounted]
///     guards on async continuations, and pull-to-refresh without lifting
///     state into a global Riverpod notifier.
///   - The screen calls [listSets] and filters client-side (no dedicated
///     podcast-list endpoint exists for MediaSet objects).  This avoids a
///     second network round-trip and keeps the screen free of duplicated
///     loading logic.
///   - Error handling is fully delegated to [podcastListErrorMessage] in
///     `error_mappers.dart` — no `dio` import in this file (DIP).
class PodcastListScreen extends ConsumerStatefulWidget {
  const PodcastListScreen({super.key});

  @override
  ConsumerState<PodcastListScreen> createState() => _PodcastListScreenState();
}

class _PodcastListScreenState extends ConsumerState<PodcastListScreen> {
  // Nullable: null means "not yet loaded" (loading indicator is shown).
  List<MediaSet>? _podcasts;

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

  /// Fetches all sets, filters to podcast sets, and updates local state.
  ///
  /// Called on first mount and on pull-to-refresh.  Filtering is done
  /// client-side immediately after the [listSets] response so the screen never
  /// shows non-podcast sets.  Errors are mapped by the top-level
  /// [podcastListErrorMessage] helper so the widget stays free of Dio.
  Future<void> _load() async {
    if (!mounted) return;
    setState(() {
      _isLoading = true;
      _error = null;
    });

    try {
      final client = ref.read(apiClientProvider);
      final allSets = await client.listSets();
      if (!mounted) return;
      setState(() {
        // Filter to podcast sets only; the home screen shows all sets.
        _podcasts = allSets.where((s) => s.isPodcast).toList();
        _isLoading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = podcastListErrorMessage(e);
        _isLoading = false;
      });
    }
  }

  // ---------------------------------------------------------------------------
  // Subscribe action
  // ---------------------------------------------------------------------------

  /// Opens [showSubscribeDialog] and reloads the list on success.
  ///
  /// Captures [BuildContext]-dependent references before the await so that
  /// post-await accesses are lint-clean (use_build_context_synchronously).
  Future<void> _openSubscribeDialog() async {
    final client = ref.read(apiClientProvider);
    // Capture context-dependent values before the async gap.
    final result = await showSubscribeDialog(context, client: client);

    // Guard: screen may have been disposed while the dialog was open.
    if (!mounted) return;

    // A non-null result means the user successfully subscribed; reload the list
    // so the new podcast feed appears without a manual pull-to-refresh.
    if (result != null) {
      await _load();
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
      floatingActionButton: FloatingActionButton(
        key: const Key('podcast_subscribe_fab'),
        tooltip: 'Subscribe to podcast',
        onPressed: _openSubscribeDialog,
        child: const Icon(Icons.add),
      ),
    );
  }

  /// Builds the app bar with the "Podcasts" title.
  AppBar _buildAppBar() {
    return AppBar(
      title: const Text('Podcasts'),
    );
  }

  /// Delegates to the appropriate state widget:
  ///   - Full-screen spinner (first load, before any data arrives).
  ///   - Error view with a retry button.
  ///   - Empty-state message when no podcast sets exist.
  ///   - Scrollable list of podcast feed tiles once data is available.
  Widget _buildBody(BuildContext context) {
    // Show a full-screen spinner only on the very first load (no data yet).
    if (_isLoading && _podcasts == null) {
      return const Center(
        key: Key('podcasts_loading'),
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
      child: _podcasts == null || _podcasts!.isEmpty
          ? const _EmptyView()
          : _PodcastList(podcasts: _podcasts!),
    );
  }
}

// ---------------------------------------------------------------------------
// Sub-widgets
// ---------------------------------------------------------------------------

/// Scrollable list of podcast feed tiles.
///
/// Extracted into its own stateless widget so [_PodcastListScreenState] stays
/// concise and the list layout is independently testable.
class _PodcastList extends StatelessWidget {
  const _PodcastList({required this.podcasts});

  final List<MediaSet> podcasts;

  @override
  Widget build(BuildContext context) {
    return ListView.builder(
      key: const Key('podcasts_list'),
      itemCount: podcasts.length,
      itemBuilder: (context, index) => _PodcastTile(podcast: podcasts[index]),
    );
  }
}

/// List tile for a single podcast [MediaSet].
///
/// Shows:
///   - Cover thumbnail (or a placeholder when empty).
///   - Feed name.
///   - Microphone icon indicating it is a podcast feed.
///
/// Tapping navigates to [MediaGridScreen] for the set's episodes via
/// [AppRoutes.mediaGridPath].  The set name is forwarded as a route extra so
/// the media-grid app bar shows it immediately without a second API call.
class _PodcastTile extends StatelessWidget {
  const _PodcastTile({required this.podcast});

  final MediaSet podcast;

  @override
  Widget build(BuildContext context) {
    return ListTile(
      key: Key('podcast_tile_${podcast.id}'),
      leading: _PodcastCover(podcast: podcast),
      title: Text(
        podcast.name,
        key: Key('podcast_name_${podcast.id}'),
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
      ),
      // Microphone icon indicates podcast feed type.
      trailing: Icon(
        Icons.mic_outlined,
        color: Theme.of(context).colorScheme.primary,
      ),
      // Navigate to the media-grid screen for this podcast's episodes.
      onTap: () => context.go(
        AppRoutes.mediaGridPath(podcast.id),
        extra: podcast.name,
      ),
    );
  }
}

/// Square cover thumbnail for a podcast feed tile.
///
/// Uses [CachedNetworkImage] to avoid re-downloading on rebuilds and to
/// provide placeholder/error fallback states.  Falls back to a grey
/// container with a microphone icon when [MediaSet.coverThumbnailPath] is
/// empty or when the network request fails.
class _PodcastCover extends StatelessWidget {
  const _PodcastCover({required this.podcast});

  final MediaSet podcast;

  @override
  Widget build(BuildContext context) {
    const size = 56.0;

    if (podcast.coverThumbnailPath.isEmpty) {
      return _placeholderWidget(context, size);
    }

    return ClipRRect(
      borderRadius: BorderRadius.circular(6),
      child: CachedNetworkImage(
        imageUrl: podcast.coverThumbnailPath,
        width: size,
        height: size,
        fit: BoxFit.cover,
        placeholder: (_, __) => _loadingWidget(size),
        errorWidget: (_, __, ___) => _placeholderWidget(context, size),
      ),
    );
  }

  // Static helpers: neither uses [this], so they are class-scoped utilities.
  static Widget _loadingWidget(double size) => SizedBox(
        width: size,
        height: size,
        child: const Center(child: CircularProgressIndicator(strokeWidth: 2)),
      );

  static Widget _placeholderWidget(BuildContext context, double size) =>
      Container(
        width: size,
        height: size,
        decoration: BoxDecoration(
          color: Theme.of(context).colorScheme.surfaceContainerHighest,
          borderRadius: BorderRadius.circular(6),
        ),
        child: Icon(
          Icons.mic_outlined,
          size: 28,
          color: Theme.of(context).colorScheme.onSurfaceVariant,
        ),
      );
}

/// Full-screen empty-state view, shown when no podcast sets are found.
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
                'No podcasts yet',
                key: const Key('podcasts_empty'),
                style: Theme.of(context).textTheme.titleMedium,
              ),
              const SizedBox(height: 8),
              Text(
                'Tap + to subscribe to a feed.',
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
/// The [message] comes from [podcastListErrorMessage], which maps exceptions
/// to human-readable strings.
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
              key: const Key('podcasts_error'),
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.bodyLarge,
            ),
            const SizedBox(height: 24),
            ElevatedButton.icon(
              key: const Key('podcasts_retry'),
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
// [podcastListErrorMessage] is defined in ../utils/error_mappers.dart,
// keeping the screen layer free of the package:dio/dio.dart dependency (DIP fix).
