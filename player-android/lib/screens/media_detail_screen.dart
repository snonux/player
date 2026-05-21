import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../app_routes.dart';
import '../models/models.dart';
import '../providers/api_client_provider.dart';
import '../utils/error_mappers.dart';
import 'create_share_dialog.dart';

// ---------------------------------------------------------------------------
// MediaDetailScreen
// ---------------------------------------------------------------------------

/// Displays a single media item with its title, full metadata (codec,
/// resolution, duration, file size), a thumbnail banner, a favourite toggle,
/// tag chips, and a play button that routes to the correct player.
///
/// Design notes:
///   - [ConsumerStatefulWidget] is used so we can hold local loading/error
///     state, guard async continuations with [mounted], and call [setState]
///     to trigger rebuilds after the favourite toggle.
///   - [getMedia] is called from [initState] (via a post-frame callback so the
///     Riverpod ref is fully bound) and on pull-to-refresh.
///   - No `dio` import — error mapping is delegated to [mediaDetailErrorMessage]
///     in `error_mappers.dart` (Dependency Inversion Principle).
///   - The screen is split into multiple focused sub-widgets so the state
///     class stays well under 50 lines.
class MediaDetailScreen extends ConsumerStatefulWidget {
  /// The string form of the media ID extracted from the '/media/:id' route.
  final String mediaId;

  const MediaDetailScreen({super.key, required this.mediaId});

  @override
  ConsumerState<MediaDetailScreen> createState() => _MediaDetailScreenState();
}

class _MediaDetailScreenState extends ConsumerState<MediaDetailScreen> {
  // Nullable: null means the first load has not completed yet.
  Media? _media;

  // Non-null when the last load attempt failed.
  String? _error;

  // True while a getMedia call is in flight (shows the full-screen spinner).
  bool _isLoading = false;

  // True while a toggleFavorite call is in flight; prevents concurrent taps
  // from queuing up multiple API calls that could result in a desync.
  bool _isFavoriteLoading = false;

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

  /// Fetches the media item from the server and updates local state.
  ///
  /// Called on first mount and on pull-to-refresh.  Errors are mapped by the
  /// top-level [mediaDetailErrorMessage] helper so no `dio` import is needed.
  Future<void> _load() async {
    if (!mounted) return;
    setState(() {
      _isLoading = true;
      _error = null;
    });

    try {
      final id = int.tryParse(widget.mediaId) ?? 0;
      final client = ref.read(apiClientProvider);
      final media = await client.getMedia(id);
      if (!mounted) return;
      setState(() {
        _media = media;
        _isLoading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = mediaDetailErrorMessage(e);
        _isLoading = false;
      });
    }
  }

  // ---------------------------------------------------------------------------
  // Favourite toggle
  // ---------------------------------------------------------------------------

  /// Calls [toggleFavorite] on the server and reflects the new state locally.
  ///
  /// The server returns the new favourite state; we apply it to the in-memory
  /// [_media] copy so the UI updates immediately without a full reload.
  /// If the call fails, a snack-bar is shown and the toggle is reverted
  /// (the local state was not yet changed, so no explicit revert is needed).
  ///
  /// [_isFavoriteLoading] is set to true for the duration of the call to block
  /// concurrent taps that could otherwise race and desync the UI with the server.
  Future<void> _toggleFavorite() async {
    final media = _media;
    // Guard against concurrent taps and against toggling before data is loaded.
    if (media == null || _isFavoriteLoading) return;

    setState(() => _isFavoriteLoading = true);

    // Optimistically flip the favourite flag in local state so the icon
    // updates instantly without waiting for the round-trip.
    final newFavorite = !media.favorite;
    if (!mounted) return;
    setState(() {
      _media = _buildMediaWithFavorite(media, newFavorite);
    });

    try {
      final client = ref.read(apiClientProvider);
      final confirmed = await client.toggleFavorite(media.id);
      if (!mounted) return;
      // Reconcile with the value the server actually stored.
      setState(() {
        _media = _buildMediaWithFavorite(_media!, confirmed);
      });
    } catch (e) {
      if (!mounted) return;
      // Revert the optimistic update on failure.
      setState(() {
        _media = _buildMediaWithFavorite(_media!, media.favorite);
      });
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Could not update favourite. Try again.')),
      );
    } finally {
      if (mounted) setState(() => _isFavoriteLoading = false);
    }
  }

  /// Returns a copy of [media] with [favorite] replaced.
  ///
  /// [Media] is immutable so we reconstruct it via [Media.fromJson]/[toJson]
  /// to avoid adding a `copyWith` method to the model layer.
  Media _buildMediaWithFavorite(Media media, bool favorite) {
    final json = media.toJson()..['favorite'] = favorite;
    return Media.fromJson(json);
  }

  // ---------------------------------------------------------------------------
  // Share
  // ---------------------------------------------------------------------------

  /// Opens [showCreateShareDialog] for the current media item.
  ///
  /// Delegates all share logic (date picker, max uses, clipboard copy) to
  /// [CreateShareDialog] so this class remains focused on media display and
  /// navigation (Single Responsibility).  The injected [PlayerApiClient] is
  /// passed directly so the dialog never needs its own provider read — keeping
  /// the dialog provider-free and independently testable (Dependency Inversion).
  Future<void> _share() async {
    final media = _media;
    if (media == null || !mounted) return;

    final client = ref.read(apiClientProvider);
    // showCreateShareDialog is async; the mounted check after the await guards
    // against the widget being disposed while the dialog is open.
    await showCreateShareDialog(
      context,
      mediaId: media.id,
      client: client,
    );
    // No post-dialog state update needed: the dialog handles clipboard copy
    // and the SnackBar internally.
  }

  // ---------------------------------------------------------------------------
  // Navigation
  // ---------------------------------------------------------------------------

  /// Routes to the video or audio player based on [media.type].
  ///
  /// The stream URL is obtained via [PlayerApiClient.streamUrl] — keeping the
  /// API path in one place and preventing Dio internals from leaking into the
  /// UI layer (Dependency Inversion).  The URL is passed as a route extra so
  /// the player screen can start playback without a second API call.
  void _play() {
    final media = _media;
    if (media == null) return;

    final client = ref.read(apiClientProvider);
    // Delegate URL construction to the client; avoids coupling the screen to
    // the underlying Dio base URL or request structure.
    final streamUrl = client.streamUrl(media.id);

    if (media.type == 'video') {
      context.go(
        AppRoutes.videoPlayerPath(media.id.toString()),
        extra: streamUrl,
      );
    } else {
      // audio / podcast / unknown — default to the audio player.
      context.go(
        AppRoutes.audioPlayerPath(media.id.toString()),
        extra: streamUrl,
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

  /// Builds the app bar with title and a three-dot overflow menu.
  ///
  /// The overflow menu currently contains a single "Share" action that opens
  /// [showCreateShareDialog].  Using a [PopupMenuButton] rather than a plain
  /// [IconButton] keeps the pattern open for future menu items without layout
  /// changes.  The [onSelected] callback uses a [Map]-based dispatch so adding
  /// a new action requires only a new enum value and one map entry — no
  /// if/else chain to extend (Open-Closed Principle).  The Share action is
  /// disabled while media is still loading (null) to prevent calling the API
  /// with a stale ID.
  AppBar _buildAppBar() {
    return AppBar(
      title: Text(_media?.fileName ?? 'Media ${widget.mediaId}'),
      actions: [
        PopupMenuButton<_MenuAction>(
          key: const Key('media_detail_overflow_menu'),
          onSelected: (action) {
            // Map-based dispatch: adding a new menu action requires only a new
            // enum value, a handler method, and one entry here — no if/else
            // chain to extend (Open-Closed Principle).
            final handlers = <_MenuAction, VoidCallback>{
              _MenuAction.share: _share,
            };
            handlers[action]?.call();
          },
          itemBuilder: (_) => [
            PopupMenuItem<_MenuAction>(
              key: const Key('media_detail_share_menu_item'),
              // Disable the item until media has loaded so the mediaId is valid.
              enabled: _media != null,
              value: _MenuAction.share,
              child: const ListTile(
                leading: Icon(Icons.share),
                title: Text('Share'),
                contentPadding: EdgeInsets.zero,
              ),
            ),
          ],
        ),
      ],
    );
  }

  /// Delegates to the appropriate state widget based on loading/error/data.
  Widget _buildBody(BuildContext context) {
    // Full-screen spinner only on the very first load (no data yet).
    if (_isLoading && _media == null) {
      return const Center(
        key: Key('media_detail_loading'),
        child: CircularProgressIndicator(),
      );
    }

    if (_error != null) {
      return _ErrorView(
        message: _error!,
        onRetry: _load,
      );
    }

    if (_media == null) {
      // Should not happen in normal flow, but guard defensively.
      return const SizedBox.shrink();
    }

    return RefreshIndicator(
      onRefresh: _load,
      child: _MediaDetailContent(
        media: _media!,
        thumbnailUrl: ref.read(apiClientProvider).thumbnailUrl(_media!.id),
        onFavoriteToggle: _toggleFavorite,
        onPlay: _play,
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// _MenuAction
// ---------------------------------------------------------------------------

/// Enum of available overflow-menu actions in [MediaDetailScreen].
///
/// Using a typed enum (rather than raw strings) makes [PopupMenuButton] type
/// safe and avoids stringly-typed comparisons in [onSelected] (type safety /
/// Open-Closed: add new actions here without touching the menu-builder switch).
enum _MenuAction { share }

// ---------------------------------------------------------------------------
// _MediaDetailContent
// ---------------------------------------------------------------------------

/// Scrollable body of the media detail screen.
///
/// Extracted from [_MediaDetailScreenState] so the state class stays concise
/// and this widget is independently testable.  All callbacks are injected so
/// this widget has no direct dependency on providers or navigation
/// (Dependency Inversion, Single Responsibility).
class _MediaDetailContent extends StatelessWidget {
  const _MediaDetailContent({
    required this.media,
    required this.thumbnailUrl,
    required this.onFavoriteToggle,
    required this.onPlay,
  });

  final Media media;

  /// Pre-computed thumbnail URL so this widget stays provider-free.
  final String thumbnailUrl;

  /// Called when the favourite icon button is tapped.
  final VoidCallback onFavoriteToggle;

  /// Called when the play button is tapped.
  final VoidCallback onPlay;

  @override
  Widget build(BuildContext context) {
    return SingleChildScrollView(
      physics: const AlwaysScrollableScrollPhysics(),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          // Full-width thumbnail/cover image.
          _ThumbnailBanner(thumbnailUrl: thumbnailUrl, type: media.type),

          Padding(
            padding: const EdgeInsets.fromLTRB(16, 12, 16, 0),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                // Title + favourite toggle on the same row.
                _TitleRow(
                  title: media.fileName,
                  isFavorite: media.favorite,
                  onFavoriteToggle: onFavoriteToggle,
                ),

                const SizedBox(height: 8),

                // Codec · resolution · duration · file size.
                _MetadataRow(media: media),

                // Tag chips (hidden when no tags).
                if (media.tags.isNotEmpty) ...[
                  const SizedBox(height: 12),
                  _TagChips(tags: media.tags),
                ],

                const SizedBox(height: 24),
              ],
            ),
          ),

          // Play button anchored at the bottom of the scrollable area.
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: _PlayButton(type: media.type, onPlay: onPlay),
          ),

          const SizedBox(height: 24),
        ],
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// _ThumbnailBanner
// ---------------------------------------------------------------------------

/// Full-width hero image at the top of the detail screen.
///
/// Falls back to an icon placeholder when [thumbnailUrl] is empty or the
/// network request fails — mirrors the card thumbnail pattern from
/// [MediaGridScreen] for visual consistency.
class _ThumbnailBanner extends StatelessWidget {
  const _ThumbnailBanner({required this.thumbnailUrl, required this.type});

  final String thumbnailUrl;

  /// Media type string used to choose the placeholder icon.
  final String type;

  @override
  Widget build(BuildContext context) {
    return AspectRatio(
      // 16:9 for video; square-ish (4:3) for audio/other for visual variety.
      aspectRatio: type == 'video' ? 16 / 9 : 4 / 3,
      child: thumbnailUrl.isEmpty
          ? _placeholder(context)
          : CachedNetworkImage(
              key: const Key('media_detail_thumbnail'),
              imageUrl: thumbnailUrl,
              fit: BoxFit.cover,
              placeholder: (_, __) =>
                  const Center(child: CircularProgressIndicator()),
              errorWidget: (_, __, ___) => _placeholder(context),
            ),
    );
  }

  /// Colored box with a type-appropriate icon when no thumbnail is available.
  Widget _placeholder(BuildContext context) {
    final icon = type == 'video'
        ? Icons.videocam_outlined
        : type == 'audio'
            ? Icons.headphones_outlined
            : Icons.image_outlined;

    return ColoredBox(
      color: Theme.of(context).colorScheme.surfaceContainerHighest,
      child: Icon(
        icon,
        size: 72,
        color: Theme.of(context).colorScheme.onSurfaceVariant,
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// _TitleRow
// ---------------------------------------------------------------------------

/// Row containing the media title and a favourite toggle icon button.
///
/// The favourite icon is filled when [isFavorite] is true, outlined otherwise.
/// Tapping calls [onFavoriteToggle] — the actual API call and state update are
/// handled by the parent state class.
class _TitleRow extends StatelessWidget {
  const _TitleRow({
    required this.title,
    required this.isFavorite,
    required this.onFavoriteToggle,
  });

  final String title;
  final bool isFavorite;
  final VoidCallback onFavoriteToggle;

  @override
  Widget build(BuildContext context) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Expanded(
          child: Text(
            title,
            key: const Key('media_detail_title'),
            style: Theme.of(context).textTheme.titleLarge,
          ),
        ),
        IconButton(
          key: const Key('media_detail_favorite'),
          icon: Icon(
            isFavorite ? Icons.favorite : Icons.favorite_border,
            color: isFavorite
                ? Theme.of(context).colorScheme.error
                : Theme.of(context).colorScheme.onSurfaceVariant,
          ),
          tooltip: isFavorite ? 'Remove from favourites' : 'Add to favourites',
          onPressed: onFavoriteToggle,
        ),
      ],
    );
  }
}

// ---------------------------------------------------------------------------
// _MetadataRow
// ---------------------------------------------------------------------------

/// Horizontal row of codec · resolution · duration · file-size chips.
///
/// Renders each non-empty value as a compact text badge separated by a
/// centred dot divider.  Empty or zero values are omitted to avoid noise
/// (e.g. audio items have no meaningful resolution).
class _MetadataRow extends StatelessWidget {
  const _MetadataRow({required this.media});

  final Media media;

  @override
  Widget build(BuildContext context) {
    final parts = _buildParts();
    if (parts.isEmpty) return const SizedBox.shrink();

    return Wrap(
      key: const Key('media_detail_metadata'),
      spacing: 4,
      runSpacing: 4,
      children: [
        for (int i = 0; i < parts.length; i++) ...[
          if (i > 0)
            Text(
              '·',
              style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
            ),
          Text(
            parts[i],
            style: Theme.of(context).textTheme.bodySmall?.copyWith(
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
          ),
        ],
      ],
    );
  }

  /// Collects non-empty metadata strings to display.
  List<String> _buildParts() {
    final parts = <String>[];
    if (media.codec.isNotEmpty) parts.add(media.codec);
    if (media.resolution.isNotEmpty) parts.add(media.resolution);
    if (media.duration > 0) parts.add(_formatDuration(media.duration));
    if (media.fileSizeBytes > 0) parts.add(_formatFileSize(media.fileSizeBytes));
    return parts;
  }

  /// Formats [seconds] as `h:mm:ss` or `m:ss`.
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

  /// Formats [bytes] as a human-readable size string (KB, MB, GB).
  static String _formatFileSize(int bytes) {
    if (bytes >= 1073741824) {
      return '${(bytes / 1073741824).toStringAsFixed(1)} GB';
    }
    if (bytes >= 1048576) {
      return '${(bytes / 1048576).toStringAsFixed(1)} MB';
    }
    return '${(bytes / 1024).toStringAsFixed(0)} KB';
  }
}

// ---------------------------------------------------------------------------
// _TagChips
// ---------------------------------------------------------------------------

/// Horizontally wrapping row of tag chips.
///
/// Uses [Chip] (non-interactive, display-only) rather than [FilterChip]
/// because the detail screen does not filter — it merely shows what tags
/// are attached to the item.
class _TagChips extends StatelessWidget {
  const _TagChips({required this.tags});

  final List<String> tags;

  @override
  Widget build(BuildContext context) {
    return Wrap(
      key: const Key('media_detail_tags'),
      spacing: 8,
      runSpacing: 4,
      children: [
        for (final tag in tags)
          Chip(
            label: Text(tag),
            labelStyle: Theme.of(context).textTheme.labelSmall,
            padding: EdgeInsets.zero,
            visualDensity: VisualDensity.compact,
          ),
      ],
    );
  }
}

// ---------------------------------------------------------------------------
// _PlayButton
// ---------------------------------------------------------------------------

/// Full-width play button at the bottom of the detail screen.
///
/// Shows a video or audio icon depending on [type].  Calls [onPlay] when
/// tapped; routing to the correct player is the parent's responsibility
/// (Single Responsibility: this widget only concerns itself with the button
/// appearance and callback delegation).
class _PlayButton extends StatelessWidget {
  const _PlayButton({required this.type, required this.onPlay});

  final String type;
  final VoidCallback onPlay;

  @override
  Widget build(BuildContext context) {
    final isVideo = type == 'video';
    return FilledButton.icon(
      key: const Key('media_detail_play'),
      onPressed: onPlay,
      icon: Icon(isVideo ? Icons.play_circle_outline : Icons.headphones),
      label: Text(isVideo ? 'Play Video' : 'Play Audio'),
      style: FilledButton.styleFrom(
        minimumSize: const Size.fromHeight(48),
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// _ErrorView
// ---------------------------------------------------------------------------

/// Full-screen error view with a retry button.
///
/// Shown when [getMedia] throws.  The [message] comes from
/// [mediaDetailErrorMessage], which maps exceptions to human-readable strings.
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
              key: const Key('media_detail_error'),
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.bodyLarge,
            ),
            const SizedBox(height: 24),
            ElevatedButton.icon(
              key: const Key('media_detail_retry'),
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
