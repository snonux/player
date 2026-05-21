import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../api/player_api_client.dart';
import '../app_routes.dart';
import '../models/models.dart';
import '../providers/api_client_provider.dart';
import '../utils/error_mappers.dart';

/// Continue Watching screen: lists all media items the authenticated user has
/// started but not finished, with a thumbnail, title, type icon, and duration.
///
/// Design notes:
///   - [ConsumerStatefulWidget] is used so we can manage local loading and
///     error state, guard async continuations on [mounted], and call
///     [setState] to trigger rebuilds (mirrors [SetsListScreen] patterns).
///   - [listInProgress] is called from [initState] and again on
///     pull-to-refresh.  The result is stored locally rather than in a
///     Riverpod notifier because this screen owns its full lifecycle.
///   - Error handling uses the top-level [continueWatchingErrorMessage] helper
///     from error_mappers.dart (DIP — no Dio import in this file).
///   - Tapping a card routes to `/video/:mediaId` or `/audio/:mediaId` with a
///     [Map] extra carrying `{mediaUrl, position}` so the player can seek to
///     the saved position without an extra API round-trip.
class ContinueWatchingScreen extends ConsumerStatefulWidget {
  const ContinueWatchingScreen({super.key});

  @override
  ConsumerState<ContinueWatchingScreen> createState() =>
      _ContinueWatchingScreenState();
}

class _ContinueWatchingScreenState
    extends ConsumerState<ContinueWatchingScreen> {
  // Nullable: null means "not yet loaded" (loading indicator is shown).
  List<Media>? _items;

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

  /// Fetches all in-progress media items and updates local state.
  ///
  /// Called on first mount and on pull-to-refresh.  Errors are mapped by the
  /// top-level [continueWatchingErrorMessage] helper so this method stays simple.
  Future<void> _load() async {
    if (!mounted) return;
    setState(() {
      _isLoading = true;
      _error = null;
    });

    try {
      final client = ref.read(apiClientProvider);
      final items = await client.listInProgress();
      if (!mounted) return;
      setState(() {
        _items = items;
        _isLoading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = continueWatchingErrorMessage(e);
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
      appBar: _buildAppBar(),
      body: _buildBody(context),
    );
  }

  /// Builds the app bar with title.
  AppBar _buildAppBar() {
    return AppBar(
      title: const Text('Continue Watching'),
    );
  }

  /// Builds the main body, delegating to the appropriate state widget:
  ///   - Loading spinner (first load, before any data arrives).
  ///   - Error view with a retry button.
  ///   - Empty-state message when the server returns an empty list.
  ///   - List of resume cards once data is available.
  Widget _buildBody(BuildContext context) {
    // Show a full-screen spinner only on the very first load (no data yet).
    if (_isLoading && _items == null) {
      return const Center(
        key: Key('continue_watching_loading'),
        child: CircularProgressIndicator(),
      );
    }

    // Show an error view with a retry button if the load failed.
    if (_error != null) {
      return _ErrorView(message: _error!, onRetry: _load);
    }

    // [RefreshIndicator] wraps the scrollable content so pull-to-refresh
    // triggers [_load] on both the list and the empty-state view.
    // Read client once here so sub-widgets can build authenticated URLs
    // (e.g. thumbnailUrl) without re-reading the provider on every rebuild.
    final client = ref.read(apiClientProvider);
    return RefreshIndicator(
      onRefresh: _load,
      child: _items == null || _items!.isEmpty
          ? const _EmptyView()
          : _ResumeList(items: _items!, client: client, onTap: _onCardTap),
    );
  }

  // ---------------------------------------------------------------------------
  // Navigation
  // ---------------------------------------------------------------------------

  /// Navigates to the appropriate player screen for [item].
  ///
  /// Passes a [Map] extra with `mediaUrl` and `position` so the player can
  /// seek to the saved position without a separate [getMediaProgress] call
  /// (avoids an extra API round-trip per resume action).
  ///
  /// [getMediaProgress] is called here to obtain the saved position.  A null
  /// result (no progress row) starts the player from the beginning.
  Future<void> _onCardTap(Media item) async {
    final client = ref.read(apiClientProvider);
    final mediaId = item.id;
    final mediaUrl = client.streamUrl(mediaId);

    // Fetch saved position best-effort; null means "start from beginning".
    double? position;
    try {
      position = await client.getMediaProgress(mediaId);
    } catch (_) {
      // Position fetch failure is non-fatal; the player starts from the start.
    }

    if (!mounted) return;

    // Pass both the stream URL and the saved position so the player can seek
    // immediately without a second round-trip to the server.
    final extra = <String, dynamic>{
      'mediaUrl': mediaUrl,
      if (position != null) 'position': position,
    };

    final mediaIdStr = mediaId.toString();
    // Delegate audio-vs-video path selection to the centralised helper so this
    // call-site does not duplicate the routing logic (OCP).
    context.go(AppRoutes.playerPathForType(item.type, mediaIdStr), extra: extra);
  }
}

// ---------------------------------------------------------------------------
// Sub-widgets
// ---------------------------------------------------------------------------

/// Scrollable list of resume cards.
///
/// Extracted into its own stateless widget so [_ContinueWatchingScreenState]
/// stays small and the list layout is independently testable.
class _ResumeList extends StatelessWidget {
  const _ResumeList({
    required this.items,
    required this.client,
    required this.onTap,
  });

  final List<Media> items;

  /// API client forwarded to each card so thumbnails use authenticated URLs.
  final PlayerApiClient client;

  /// Called with the tapped [Media] item; the parent handles navigation.
  final void Function(Media) onTap;

  @override
  Widget build(BuildContext context) {
    return ListView.builder(
      key: const Key('continue_watching_list'),
      padding: const EdgeInsets.symmetric(vertical: 8),
      itemCount: items.length,
      itemBuilder: (context, index) => _ResumeCard(
        item: items[index],
        client: client,
        onTap: onTap,
      ),
    );
  }
}

/// A single resume card showing thumbnail, title, type icon, and duration.
///
/// Tapping the card delegates navigation back to [_ContinueWatchingScreenState]
/// via [onTap] (Single Responsibility — this widget is purely presentational).
class _ResumeCard extends StatelessWidget {
  const _ResumeCard({
    required this.item,
    required this.client,
    required this.onTap,
  });

  final Media item;

  /// API client used to build the authenticated thumbnail URL.
  final PlayerApiClient client;

  final void Function(Media) onTap;

  @override
  Widget build(BuildContext context) {
    return Card(
      key: Key('resume_card_${item.id}'),
      margin: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: () => onTap(item),
        child: Row(
          children: [
            // Thumbnail on the left: fixed 100×75 px, using the authenticated
            // API endpoint rather than the raw relative server path.
            _Thumbnail(item: item, client: client),
            // Title, type icon and duration on the right.
            Expanded(child: _CardDetails(item: item)),
          ],
        ),
      ),
    );
  }
}

/// Left-side thumbnail for a resume card.
///
/// Displays the media thumbnail via [CachedNetworkImage] with a placeholder
/// and error fallback, keeping re-download count low across rebuilds.
///
/// Uses [client.thumbnailUrl] (the authenticated API endpoint) rather than
/// [Media.thumbnailPath] (a relative server path that is not a valid URL).
class _Thumbnail extends StatelessWidget {
  const _Thumbnail({required this.item, required this.client});

  final Media item;

  /// API client used to build the authenticated thumbnail URL.
  final PlayerApiClient client;

  @override
  Widget build(BuildContext context) {
    // Use the authenticated thumbnail endpoint instead of the raw relative path.
    final url = client.thumbnailUrl(item.id);

    // Skip the network request entirely when the URL is empty (server has not
    // generated a thumbnail yet) and fall back to the placeholder icon.
    if (url.isEmpty) return _placeholder(context);

    return SizedBox(
      width: 100,
      height: 75,
      child: CachedNetworkImage(
        imageUrl: url,
        fit: BoxFit.cover,
        width: 100,
        height: 75,
        placeholder: (_, __) => _loadingWidget(),
        errorWidget: (_, __, ___) => _placeholder(context),
      ),
    );
  }

  static Widget _loadingWidget() => const SizedBox(
        width: 100,
        height: 75,
        child: Center(child: CircularProgressIndicator(strokeWidth: 2)),
      );

  static Widget _placeholder(BuildContext context) => SizedBox(
        width: 100,
        height: 75,
        child: ColoredBox(
          color: Theme.of(context).colorScheme.surfaceContainerHighest,
          child: Icon(
            Icons.movie_outlined,
            size: 36,
            color: Theme.of(context).colorScheme.onSurfaceVariant,
          ),
        ),
      );
}

/// Right-side details: title, type icon badge, and total duration.
///
/// Shows a video/audio icon badge next to the title so the user can identify
/// the media type at a glance without relying on colour alone.
class _CardDetails extends StatelessWidget {
  const _CardDetails({required this.item});

  final Media item;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          // Title row with type icon badge.
          Row(
            children: [
              _TypeIcon(type: item.type),
              const SizedBox(width: 6),
              Expanded(
                child: Text(
                  item.fileName,
                  key: Key('resume_card_title_${item.id}'),
                  style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                        fontWeight: FontWeight.w600,
                      ),
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
            ],
          ),
          const SizedBox(height: 4),
          // Total duration: provides a rough "how much is left" sense even
          // without the saved position in the list response.
          Text(
            _formatDuration(item.duration),
            key: Key('resume_card_duration_${item.id}'),
            style: Theme.of(context).textTheme.bodySmall,
          ),
        ],
      ),
    );
  }
}

/// Formats [durationSeconds] as `mm:ss` or `h:mm:ss`.
///
/// Top-level function (consistent with the audio player screen pattern) so it
/// can be reused without an instance and is easy to unit-test in isolation.
String _formatDuration(double durationSeconds) {
  final d = Duration(milliseconds: (durationSeconds * 1000).round());
  final h = d.inHours;
  final m = d.inMinutes.remainder(60).toString().padLeft(2, '0');
  final s = d.inSeconds.remainder(60).toString().padLeft(2, '0');
  return h > 0 ? '$h:$m:$s' : '$m:$s';
}

/// Small type-icon badge for video or audio items.
///
/// Uses [Icons.videocam_outlined] for video and [Icons.headphones] for audio,
/// falling back to [Icons.play_circle_outline] for unknown types.
class _TypeIcon extends StatelessWidget {
  const _TypeIcon({required this.type});

  final String type;

  @override
  Widget build(BuildContext context) {
    final icon = switch (type) {
      'audio' => Icons.headphones,
      'video' => Icons.videocam_outlined,
      _ => Icons.play_circle_outline,
    };
    return Icon(
      icon,
      size: 18,
      color: Theme.of(context).colorScheme.primary,
    );
  }
}

/// Full-screen empty-state view shown when [listInProgress] returns an empty list.
///
/// Wrapped in a [ListView] with [AlwaysScrollableScrollPhysics] so the
/// [RefreshIndicator] can still trigger pull-to-refresh with no content.
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
                Icons.play_circle_outline,
                size: 72,
                color: Theme.of(context).colorScheme.onSurfaceVariant,
              ),
              const SizedBox(height: 16),
              Text(
                'Nothing in progress',
                key: const Key('continue_watching_empty'),
                style: Theme.of(context).textTheme.titleMedium,
              ),
              const SizedBox(height: 8),
              Text(
                'Start playing something and it will appear here.',
                style: Theme.of(context).textTheme.bodySmall,
                textAlign: TextAlign.center,
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
/// Shown when [listInProgress] throws (network error, server error, etc.).
/// The [message] comes from [continueWatchingErrorMessage].
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
              key: const Key('continue_watching_error'),
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.bodyLarge,
            ),
            const SizedBox(height: 24),
            ElevatedButton.icon(
              key: const Key('continue_watching_retry'),
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
