import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../app_routes.dart';
import '../models/models.dart';
import '../providers/api_client_provider.dart';
import '../utils/duration_formatter.dart';
import '../utils/error_mappers.dart';

// ---------------------------------------------------------------------------
// Data models (file-private)
// ---------------------------------------------------------------------------

/// Represents a subfolder returned by the browseSet API endpoint.
///
/// [name] is the folder's display name and last path component.
/// [hasCover] indicates whether the server has a cover image for this folder.
class _BrowseFolder {
  const _BrowseFolder({required this.name, required this.hasCover});

  final String name;
  final bool hasCover;
}

/// Parsed result from the browseSet API response.
///
/// Holds the canonical current path returned by the server along with lists
/// of subfolders and media items at that path.
class _BrowseResult {
  const _BrowseResult({
    required this.currentPath,
    required this.folders,
    required this.media,
  });

  final String currentPath;
  final List<_BrowseFolder> folders;
  final List<Media> media;
}

// ---------------------------------------------------------------------------
// Parsing helpers (file-private, pure functions — easy to unit-test)
// ---------------------------------------------------------------------------

/// Parses the raw JSON map from [PlayerApiClient.browseSet] into a typed
/// [_BrowseResult].
///
/// Unknown or missing fields are handled gracefully: the result defaults to
/// empty lists rather than throwing, so partial server responses degrade
/// gracefully (resilience principle).
_BrowseResult _parseBrowseResult(Map<String, dynamic> raw) {
  final currentPath = raw['current_path'] as String? ?? '';
  final rawFolders = raw['folders'] as List<dynamic>? ?? [];
  final rawMedia = raw['media'] as List<dynamic>? ?? [];

  final folders = rawFolders
      .whereType<Map<String, dynamic>>()
      .map(
        (f) => _BrowseFolder(
          name: f['name'] as String? ?? '',
          hasCover: f['has_cover'] as bool? ?? false,
        ),
      )
      .where((f) => f.name.isNotEmpty)
      .toList();

  final media = rawMedia
      .whereType<Map<String, dynamic>>()
      .map(Media.fromJson)
      .toList();

  return _BrowseResult(
    currentPath: currentPath,
    folders: folders,
    media: media,
  );
}

/// Splits a slash-delimited [path] into breadcrumb segments.
///
/// Returns a list of `(label, accumulatedPath)` pairs — the first entry is
/// always the root `('Home', '')`, followed by one entry per path component.
/// This pure function makes the breadcrumb logic independently testable.
List<({String label, String path})> _buildBreadcrumbs(String path) {
  final crumbs = <({String label, String path})>[
    (label: 'Home', path: ''),
  ];
  if (path.isEmpty) return crumbs;

  final parts = path.split('/').where((p) => p.isNotEmpty).toList();
  var accumulated = '';
  for (final part in parts) {
    accumulated = accumulated.isEmpty ? part : '$accumulated/$part';
    crumbs.add((label: part, path: accumulated));
  }
  return crumbs;
}

// ---------------------------------------------------------------------------
// FolderBrowserScreen
// ---------------------------------------------------------------------------

/// Displays the contents of a single folder within a [MediaSet].
///
/// Shows subfolders first (with cover thumbnail) followed by media items.
/// A breadcrumb bar at the top lets the user navigate back up the tree.
///
/// Design notes:
///   - Accepts [setId] and optional [path] as constructor parameters so the
///     screen is independently testable and reusable (DIP / ISP).
///   - No `dio` import: errors are mapped by [folderErrorMessage] (DIP).
///   - All async continuations guard on [mounted] to prevent setState after
///     disposal (Flutter best practice).
///   - The generation counter prevents stale responses from overwriting
///     fresher data when navigations happen in quick succession.
class FolderBrowserScreen extends ConsumerStatefulWidget {
  /// The numeric identifier of the set to browse.
  final int setId;

  /// The current subfolder path (empty or null = root of the set).
  final String? path;

  /// Optional human-readable set name shown in the app bar.
  ///
  /// Passed as a route extra by callers so the bar shows the name immediately
  /// without an extra API call.
  final String? setName;

  const FolderBrowserScreen({
    super.key,
    required this.setId,
    this.path,
    this.setName,
  });

  @override
  ConsumerState<FolderBrowserScreen> createState() =>
      _FolderBrowserScreenState();
}

class _FolderBrowserScreenState extends ConsumerState<FolderBrowserScreen> {
  // Nullable: null means "not yet loaded" (loading indicator is shown).
  _BrowseResult? _result;

  // Non-null when the last load attempt failed.
  String? _error;

  // True while the initial or refresh load is in flight.
  bool _isLoading = false;

  // Generation counter used to discard stale async responses (cancellation-
  // by-generation pattern, consistent with MediaGridScreen).
  int _loadGeneration = 0;

  @override
  void initState() {
    super.initState();
    // Defer the first load until after the first frame so [ref] is fully
    // bound and any test-environment provider overrides are applied.
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
  }

  // ---------------------------------------------------------------------------
  // Data loading
  // ---------------------------------------------------------------------------

  /// Fetches subfolder and media content for [widget.setId] at [widget.path]
  /// and updates local state.
  ///
  /// The generation counter ensures responses from superseded loads are
  /// silently discarded, preventing race conditions on rapid navigations.
  Future<void> _load() async {
    if (!mounted) return;

    final generation = ++_loadGeneration;

    setState(() {
      _isLoading = true;
      _error = null;
    });

    try {
      final client = ref.read(apiClientProvider);
      final raw = await client.browseSet(
        widget.setId,
        parent: widget.path,
      );

      if (!mounted || generation != _loadGeneration) return;

      setState(() {
        _result = _parseBrowseResult(raw);
        _isLoading = false;
      });
    } catch (e) {
      if (!mounted || generation != _loadGeneration) return;
      setState(() {
        _error = folderErrorMessage(e);
        _isLoading = false;
      });
    }
  }

  // ---------------------------------------------------------------------------
  // Navigation helpers
  // ---------------------------------------------------------------------------

  /// Navigates into [folderName] by pushing a new [FolderBrowserScreen] route.
  ///
  /// The new path is the current path joined with [folderName]. Using
  /// `context.push` (rather than `go`) keeps the back-stack intact so the
  /// user can navigate back up to the parent folder.
  void _openFolder(String folderName) {
    final currentPath = widget.path ?? '';
    final newPath =
        currentPath.isEmpty ? folderName : '$currentPath/$folderName';
    context.push(
      AppRoutes.folderBrowserPath(widget.setId, path: newPath),
      extra: widget.setName,
    );
  }

  /// Navigates to a breadcrumb [crumbPath].
  ///
  /// For the root ('') we pop until the route matching the root path; for
  /// intermediate paths we push the new screen. This keeps the navigator
  /// stack correct in all navigation directions.
  void _navigateToBreadcrumb(String crumbPath) {
    if (crumbPath == (widget.path ?? '')) return; // already here
    context.push(
      AppRoutes.folderBrowserPath(widget.setId, path: crumbPath),
      extra: widget.setName,
    );
  }

  /// Navigates to the [MediaDetailScreen] for [mediaId].
  void _openMedia(int mediaId) =>
      context.push(AppRoutes.mediaDetailPath(mediaId));

  /// Returns the URL for the cover image of a folder at [folderPath] within
  /// [widget.setId].
  ///
  /// Delegates URL construction to [PlayerApiClient.setFolderCoverUrl] so this
  /// screen never builds API paths or accesses Dio internals directly
  /// (Dependency Inversion Principle, consistent with [thumbnailUrl] pattern).
  String _coverUrl(String folderPath) {
    final client = ref.read(apiClientProvider);
    return client.setFolderCoverUrl(widget.setId, folder: folderPath);
  }

  // ---------------------------------------------------------------------------
  // Build
  // ---------------------------------------------------------------------------

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: _buildAppBar(),
      body: _buildBody(),
    );
  }

  /// Builds the app bar with the set name (or a default label).
  AppBar _buildAppBar() {
    return AppBar(
      title: Text(widget.setName ?? 'Set ${widget.setId}'),
    );
  }

  /// Dispatches to the appropriate body widget based on the current state.
  Widget _buildBody() {
    // Full-screen spinner only on the very first load (no data yet).
    if (_isLoading && _result == null) {
      return const Center(
        key: Key('folder_loading'),
        child: CircularProgressIndicator(),
      );
    }

    // Error view with a retry button.
    if (_error != null) {
      return _ErrorView(message: _error!, onRetry: _load);
    }

    return RefreshIndicator(
      onRefresh: _load,
      child: _buildContent(),
    );
  }

  /// Builds the scrollable content: breadcrumb bar + folder list + media list.
  Widget _buildContent() {
    final result = _result;
    final path = widget.path ?? '';
    final crumbs = _buildBreadcrumbs(path);

    return CustomScrollView(
      key: const Key('folder_browser_scroll'),
      physics: const AlwaysScrollableScrollPhysics(),
      slivers: [
        // Breadcrumb navigation bar.
        SliverToBoxAdapter(
          child: _BreadcrumbBar(
            crumbs: crumbs,
            onTap: _navigateToBreadcrumb,
          ),
        ),

        // Content: empty state, or folders + media list.
        if (result == null || (result.folders.isEmpty && result.media.isEmpty))
          const SliverFillRemaining(
            hasScrollBody: false,
            child: _EmptyView(),
          )
        else ...[
          // Subfolder tiles shown before media items.
          // Item 0 is the section header; items 1..n are the folder tiles.
          if (result.folders.isNotEmpty)
            SliverList.builder(
              itemCount: result.folders.length + 1,
              itemBuilder: (context, index) {
                if (index == 0) {
                  return const _SectionHeader(
                    key: Key('folder_section_header'),
                    label: 'Folders',
                  );
                }
                final folder = result.folders[index - 1];
                final folderPath = path.isEmpty
                    ? folder.name
                    : '$path/${folder.name}';
                return _FolderTile(
                  key: Key('folder_tile_${folder.name}'),
                  folder: folder,
                  coverUrl: folder.hasCover ? _coverUrl(folderPath) : '',
                  onTap: () => _openFolder(folder.name),
                );
              },
            ),

          // Media items shown below subfolders.
          // Item 0 is the section header; items 1..n are the media tiles.
          if (result.media.isNotEmpty)
            SliverList.builder(
              itemCount: result.media.length + 1,
              itemBuilder: (context, index) {
                if (index == 0) {
                  return const _SectionHeader(
                    key: Key('media_section_header'),
                    label: 'Media',
                  );
                }
                final item = result.media[index - 1];
                return _MediaTile(
                  key: Key('media_tile_${item.id}'),
                  item: item,
                  thumbnailUrl: _thumbnailUrl(item.id),
                  onTap: () => _openMedia(item.id),
                );
              },
            ),
        ],
      ],
    );
  }

  /// Returns the thumbnail URL for a media item.
  ///
  /// Delegates to [PlayerApiClient.thumbnailUrl] so no API path is
  /// hard-coded in the screen layer (DIP).
  String _thumbnailUrl(int mediaId) {
    final client = ref.read(apiClientProvider);
    return client.thumbnailUrl(mediaId);
  }
}

// ---------------------------------------------------------------------------
// Sub-widgets
// ---------------------------------------------------------------------------

/// Horizontal scrollable breadcrumb bar showing the current folder hierarchy.
///
/// Each crumb is a tappable [TextButton]; the last crumb (current folder) is
/// shown in a highlighted style and is not interactive.
class _BreadcrumbBar extends StatelessWidget {
  const _BreadcrumbBar({required this.crumbs, required this.onTap});

  /// Ordered list of `(label, path)` pairs, root-first.
  final List<({String label, String path})> crumbs;

  /// Called when any non-last crumb is tapped; receives the target path.
  final void Function(String path) onTap;

  @override
  Widget build(BuildContext context) {
    return Container(
      key: const Key('breadcrumb_bar'),
      color: Theme.of(context).colorScheme.surfaceContainerLow,
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      child: SingleChildScrollView(
        scrollDirection: Axis.horizontal,
        child: Row(
          children: _buildCrumbs(context),
        ),
      ),
    );
  }

  /// Builds the crumb widgets interleaved with separator chevrons.
  List<Widget> _buildCrumbs(BuildContext context) {
    final widgets = <Widget>[];
    for (var i = 0; i < crumbs.length; i++) {
      final crumb = crumbs[i];
      final isLast = i == crumbs.length - 1;

      if (i > 0) {
        // Separator between crumbs.
        widgets.add(
          Icon(
            Icons.chevron_right,
            size: 16,
            color: Theme.of(context).colorScheme.onSurfaceVariant,
          ),
        );
      }

      widgets.add(
        isLast
            // Current folder: styled as body text, non-tappable.
            ? Text(
                crumb.label,
                key: Key('breadcrumb_current_${crumb.label}'),
                style: Theme.of(context).textTheme.bodyMedium?.copyWith(
                      fontWeight: FontWeight.w600,
                    ),
              )
            // Ancestor folder: tappable TextButton.
            : TextButton(
                key: Key('breadcrumb_${crumb.label}'),
                style: TextButton.styleFrom(
                  padding: const EdgeInsets.symmetric(horizontal: 4),
                  minimumSize: Size.zero,
                  tapTargetSize: MaterialTapTargetSize.shrinkWrap,
                ),
                onPressed: () => onTap(crumb.path),
                child: Text(crumb.label),
              ),
      );
    }
    return widgets;
  }
}

/// Section header label (e.g. "Folders", "Media").
///
/// Separates the folder list from the media list with a lightweight divider
/// and label so the user understands the two sections at a glance.
class _SectionHeader extends StatelessWidget {
  const _SectionHeader({super.key, required this.label});

  final String label;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 12, 16, 4),
      child: Text(
        label,
        style: Theme.of(context).textTheme.labelLarge?.copyWith(
              color: Theme.of(context).colorScheme.primary,
            ),
      ),
    );
  }
}

/// List tile for a single subfolder entry.
///
/// Shows a folder cover image (if available) or a folder icon placeholder,
/// and the folder name.  Tapping fires [onTap] to navigate into the folder.
class _FolderTile extends StatelessWidget {
  const _FolderTile({
    super.key,
    required this.folder,
    required this.coverUrl,
    required this.onTap,
  });

  final _BrowseFolder folder;

  /// Full URL of the folder cover image; empty string means no cover.
  final String coverUrl;

  /// Called when the tile is tapped.
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return ListTile(
      leading: _FolderCoverImage(
        key: Key('folder_cover_${folder.name}'),
        coverUrl: coverUrl,
      ),
      title: Text(
        folder.name,
        key: Key('folder_name_${folder.name}'),
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
      ),
      trailing: const Icon(Icons.chevron_right),
      onTap: onTap,
    );
  }
}

/// Square cover thumbnail for a folder.
///
/// Falls back to a folder icon placeholder when no cover URL is provided or
/// when the network request fails. Consistent with the [_ThumbnailImage]
/// pattern in [MediaGridScreen].
class _FolderCoverImage extends StatelessWidget {
  const _FolderCoverImage({super.key, required this.coverUrl});

  final String coverUrl;

  @override
  Widget build(BuildContext context) {
    const size = 48.0;

    if (coverUrl.isEmpty) return _placeholder(context, size);

    return SizedBox(
      width: size,
      height: size,
      child: CachedNetworkImage(
        imageUrl: coverUrl,
        fit: BoxFit.cover,
        placeholder: (_, __) =>
            const Center(child: CircularProgressIndicator()),
        errorWidget: (_, __, ___) => _placeholder(context, size),
      ),
    );
  }

  static Widget _placeholder(BuildContext context, double size) => SizedBox(
        width: size,
        height: size,
        child: ColoredBox(
          color: Theme.of(context).colorScheme.surfaceContainerHighest,
          child: Icon(
            Icons.folder_outlined,
            size: size * 0.6,
            color: Theme.of(context).colorScheme.onSurfaceVariant,
          ),
        ),
      );
}

/// List tile for a single media item.
///
/// Shows a thumbnail (or icon placeholder), the file name, type icon, and
/// formatted duration.  Tapping fires [onTap] to navigate to the detail screen.
class _MediaTile extends StatelessWidget {
  const _MediaTile({
    super.key,
    required this.item,
    required this.thumbnailUrl,
    required this.onTap,
  });

  final Media item;

  /// Full URL of the media thumbnail; empty string means no thumbnail.
  final String thumbnailUrl;

  /// Called when the tile is tapped.
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return ListTile(
      leading: _MediaThumbnail(
        key: Key('media_thumb_${item.id}'),
        thumbnailUrl: thumbnailUrl,
        type: item.type,
      ),
      title: Text(
        item.fileName,
        key: Key('media_tile_name_${item.id}'),
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
      ),
      subtitle: Row(
        children: [
          Icon(_typeIcon(item.type), size: 12),
          const SizedBox(width: 4),
          Text(
            _formatDuration(item.duration),
            key: Key('media_tile_duration_${item.id}'),
            style: Theme.of(context).textTheme.bodySmall,
          ),
        ],
      ),
      onTap: onTap,
    );
  }

  /// Returns an appropriate icon for the given media [type].
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
  /// Delegates to the shared [formatDuration] helper (DRY) so the formatting
  /// logic lives in exactly one place across all screen widgets.
  static String _formatDuration(double seconds) => formatDuration(seconds);
}

/// Small square thumbnail for a media tile.
///
/// Mirrors the [_ThumbnailImage] approach from [MediaGridScreen], using
/// [CachedNetworkImage] with placeholder and error fallback.
class _MediaThumbnail extends StatelessWidget {
  const _MediaThumbnail({
    super.key,
    required this.thumbnailUrl,
    required this.type,
  });

  final String thumbnailUrl;

  /// Media type string used to pick the placeholder icon.
  final String type;

  @override
  Widget build(BuildContext context) {
    const size = 48.0;

    if (thumbnailUrl.isEmpty) return _placeholder(context, size);

    return SizedBox(
      width: size,
      height: size,
      child: CachedNetworkImage(
        imageUrl: thumbnailUrl,
        fit: BoxFit.cover,
        placeholder: (_, __) =>
            const Center(child: CircularProgressIndicator()),
        errorWidget: (_, __, ___) => _placeholder(context, size),
      ),
    );
  }

  static Widget _placeholder(BuildContext context, double size) => SizedBox(
        width: size,
        height: size,
        child: ColoredBox(
          color: Theme.of(context).colorScheme.surfaceContainerHighest,
          child: Icon(
            Icons.image_outlined,
            size: size * 0.6,
            color: Theme.of(context).colorScheme.onSurfaceVariant,
          ),
        ),
      );
}

/// Full-screen empty-state view shown when the current folder is empty.
///
/// Wrapped in a layout that fills the sliver so pull-to-refresh gestures are
/// still captured by [RefreshIndicator].
class _EmptyView extends StatelessWidget {
  const _EmptyView();

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Icon(
            Icons.folder_open_outlined,
            size: 72,
            color: Theme.of(context).colorScheme.onSurfaceVariant,
          ),
          const SizedBox(height: 16),
          Text(
            'This folder is empty',
            key: const Key('folder_empty'),
            style: Theme.of(context).textTheme.titleMedium,
          ),
          const SizedBox(height: 8),
          Text(
            'Pull down to refresh.',
            style: Theme.of(context).textTheme.bodySmall,
          ),
        ],
      ),
    );
  }
}

/// Full-screen error view with a retry button.
///
/// Shown when [browseSet] throws (network error, server error, etc.).
/// The [message] comes from [folderErrorMessage], which maps exceptions to
/// human-readable strings without exposing Dio to the screen layer.
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
              key: const Key('folder_error'),
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.bodyLarge,
            ),
            const SizedBox(height: 24),
            ElevatedButton.icon(
              key: const Key('folder_retry'),
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
