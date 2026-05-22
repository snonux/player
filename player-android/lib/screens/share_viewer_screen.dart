import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../app_routes.dart';
import '../providers/public_api_client_provider.dart';
import '../utils/duration_formatter.dart';
import '../utils/error_mappers.dart';

// ---------------------------------------------------------------------------
// Share page metadata model
// ---------------------------------------------------------------------------

/// Parsed metadata from the GET /s/{token} JSON response.
///
/// Keeps the screen layer free from raw map access: the data is extracted once
/// here and then consumed via typed fields (Separation of Concerns).
class SharePageMetadata {
  const SharePageMetadata({
    required this.fileName,
    required this.type,
    required this.duration,
    required this.hasThumb,
    required this.streamUrl,
    required this.thumbUrl,
    required this.downloadUrl,
  });

  final String fileName;
  final String type;

  /// Duration in seconds; null when the server omits the field.
  final double? duration;

  final bool hasThumb;

  /// Relative path for the stream endpoint (e.g. "/s/abc123/stream").
  final String streamUrl;

  /// Relative path for the thumbnail (e.g. "/s/abc123/thumbnail").
  final String thumbUrl;

  /// Relative path for downloading the original file.
  final String downloadUrl;

  /// Parses a [SharePageMetadata] from the raw JSON string returned by
  /// [PlayerApiClient.getSharedMediaPage].
  ///
  /// Missing fields are replaced with safe defaults so the screen always has
  /// something to display rather than throwing on an unexpected server response.
  factory SharePageMetadata.fromJson(String jsonBody) {
    final map = jsonDecode(jsonBody) as Map<String, dynamic>;
    final media = map['media'] as Map<String, dynamic>? ?? {};

    return SharePageMetadata(
      fileName: (media['file_name'] as String?) ?? 'Unknown file',
      type: (media['type'] as String?) ?? 'video',
      duration: (media['duration'] as num?)?.toDouble(),
      hasThumb: (map['has_thumb'] as bool?) ?? false,
      streamUrl: (map['stream_url'] as String?) ?? '',
      thumbUrl: (map['thumb_url'] as String?) ?? '',
      downloadUrl: (map['download_url'] as String?) ?? '',
    );
  }
}

// ---------------------------------------------------------------------------
// ShareViewerScreen
// ---------------------------------------------------------------------------

/// Public share-viewer screen — no authentication required.
///
/// Accepts a [token] from the go_router path parameter (/share/:token).
/// Calls the unauthenticated [publicApiClientProvider] to fetch share metadata
/// and renders the file name, type, duration, and thumbnail.  A "Play" button
/// navigates to the appropriate video or audio player with the stream URL so
/// the viewer can watch or listen without a user account.
///
/// Design notes:
///   - [ConsumerStatefulWidget] is used so local state (loading, error, data)
///     is managed without extra Riverpod providers for transient UI state, and
///     so [mounted] guards are available on all async continuations.
///   - No [Dio] import: all HTTP calls go through [publicApiClientProvider],
///     keeping the screen layer decoupled from the HTTP transport (DIP).
///   - The screen does not import any authenticated provider, ensuring it cannot
///     accidentally attach a session to a public request.
///   - Progress updates sent by the player screen will fail silently for
///     public shares (the progress endpoint requires auth) — this is acceptable
///     because progress tracking is a per-user authenticated feature.
class ShareViewerScreen extends ConsumerStatefulWidget {
  const ShareViewerScreen({super.key, required this.token});

  /// The opaque share token extracted from the URL path by go_router.
  final String token;

  @override
  ConsumerState<ShareViewerScreen> createState() => _ShareViewerScreenState();
}

class _ShareViewerScreenState extends ConsumerState<ShareViewerScreen> {
  // Null during the initial load; non-null after a successful fetch.
  SharePageMetadata? _page;

  // Non-null when the last fetch attempt failed.
  String? _error;

  // True while the initial or retry load is in flight.
  bool _isLoading = false;

  @override
  void initState() {
    super.initState();
    // Defer first load until after the first frame so any provider overrides
    // applied in tests are in place before [ref] is accessed.
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
  }

  // ---------------------------------------------------------------------------
  // Data loading
  // ---------------------------------------------------------------------------

  /// Fetches share metadata from the server using the unauthenticated client.
  ///
  /// Uses [publicApiClientProvider] so no bearer token is attached.
  /// Errors are mapped to human-readable strings by [shareViewerErrorMessage].
  Future<void> _load() async {
    if (!mounted) return;
    setState(() {
      _isLoading = true;
      _error = null;
    });

    try {
      final client = ref.read(publicApiClientProvider);
      final json = await client.getSharedMediaPage(widget.token);
      final page = SharePageMetadata.fromJson(json);
      if (!mounted) return;
      setState(() {
        _page = page;
        _isLoading = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = shareViewerErrorMessage(e);
        _isLoading = false;
      });
    }
  }

  // ---------------------------------------------------------------------------
  // Play action
  // ---------------------------------------------------------------------------

  /// Navigates to the appropriate player screen with the absolute stream URL.
  ///
  /// Builds the absolute URL from [PlayerApiClient.baseUrl] and the relative
  /// [SharePageMetadata.streamUrl] path so the player screen receives a fully
  /// qualified URL it can pass directly to ExoPlayer / just_audio.
  ///
  /// The player screen's [mediaId] is set to '0' as a placeholder because
  /// progress tracking (which requires auth) is not available for public shares;
  /// failed progress-update calls in the player are already fire-and-forget and
  /// do not affect playback.
  void _play() {
    if (_page == null) return;

    final client = ref.read(publicApiClientProvider);
    final absoluteStreamUrl = '${client.baseUrl}${_page!.streamUrl}';

    // Navigate to the audio or video player based on the media type.
    // The stream URL is passed as a route extra so the player uses it directly
    // without deriving it from a media ID (Dependency Inversion).
    final playerPath = AppRoutes.playerPathForType(_page!.type, '0');
    context.go(playerPath, extra: absoluteStreamUrl);
  }

  // ---------------------------------------------------------------------------
  // Build
  // ---------------------------------------------------------------------------

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Shared Media')),
      body: _buildBody(context),
    );
  }

  /// Returns the appropriate body widget for the current state:
  ///   - Full-screen spinner while the first load is in flight.
  ///   - Error view with retry button when the fetch failed.
  ///   - Metadata card with Play button when data is ready.
  Widget _buildBody(BuildContext context) {
    if (_isLoading && _page == null) {
      return const Center(
        key: Key('share_viewer_loading'),
        child: CircularProgressIndicator(),
      );
    }

    if (_error != null) {
      return _ErrorView(message: _error!, onRetry: _load);
    }

    if (_page == null) {
      // Defensive guard: should not be reachable under normal flow.
      return const SizedBox.shrink();
    }

    return _MetadataView(page: _page!, onPlay: _play);
  }
}

// ---------------------------------------------------------------------------
// Sub-widgets
// ---------------------------------------------------------------------------

/// Displays the share metadata and the Play button.
///
/// Extracted into its own stateless widget so the parent state class stays
/// focused on data-loading concerns and the UI is independently testable
/// (Single Responsibility Principle).
class _MetadataView extends StatelessWidget {
  const _MetadataView({required this.page, required this.onPlay});

  final SharePageMetadata page;
  final VoidCallback onPlay;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return SingleChildScrollView(
      padding: const EdgeInsets.all(16),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          // Thumbnail or media-type icon placeholder.
          _ThumbnailWidget(page: page),
          const SizedBox(height: 16),

          // File name — primary text element of the card.
          Text(
            page.fileName,
            key: const Key('share_viewer_filename'),
            style: theme.textTheme.titleLarge,
            textAlign: TextAlign.center,
          ),
          const SizedBox(height: 8),

          // Type and duration shown as a secondary metadata row.
          _MetadataRow(page: page),
          const SizedBox(height: 24),

          // Play button — navigates to the appropriate player screen.
          FilledButton.icon(
            key: const Key('share_viewer_play_button'),
            onPressed: onPlay,
            icon: const Icon(Icons.play_arrow),
            label: const Text('Play'),
            style: FilledButton.styleFrom(
              padding: const EdgeInsets.symmetric(vertical: 14),
            ),
          ),
        ],
      ),
    );
  }
}

/// Shows the thumbnail image when available, or a media-type icon placeholder.
///
/// Delegates to [_ThumbnailImage] when [SharePageMetadata.hasThumb] is true,
/// or to [_FallbackThumbnail] when there is no thumbnail (Open-Closed: adding
/// a new media type only requires extending [_FallbackThumbnail]).
class _ThumbnailWidget extends ConsumerWidget {
  const _ThumbnailWidget({required this.page});

  final SharePageMetadata page;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    if (!page.hasThumb || page.thumbUrl.isEmpty) {
      return _FallbackThumbnail(type: page.type);
    }

    final client = ref.read(publicApiClientProvider);
    final absoluteThumbUrl = '${client.baseUrl}${page.thumbUrl}';

    return ClipRRect(
      borderRadius: BorderRadius.circular(8),
      child: AspectRatio(
        aspectRatio: 16 / 9,
        child: Image.network(
          absoluteThumbUrl,
          key: const Key('share_viewer_thumbnail'),
          fit: BoxFit.cover,
          // Fall back to the type icon if the image fails to load (e.g. network
          // error) so the viewer always sees something meaningful.
          errorBuilder: (_, __, ___) => _FallbackThumbnail(type: page.type),
        ),
      ),
    );
  }
}

/// Icon placeholder shown when no thumbnail is available or fails to load.
///
/// The icon is chosen by [type] so audio shares show a music note while video
/// shares show a movie icon, giving the viewer a visual hint about the content.
///
/// The icon lookup uses a map rather than a binary conditional so that new
/// media types can be added by extending [_typeIcons] alone (Open-Closed
/// Principle) — no if/else chain to update.
class _FallbackThumbnail extends StatelessWidget {
  const _FallbackThumbnail({required this.type});

  final String type;

  /// Maps media type strings to their representative Material icons.
  ///
  /// Unknown types fall back to [Icons.movie] via the null-coalescing lookup
  /// in [build], so new server-side types degrade gracefully without crashes.
  static const _typeIcons = {
    'audio': Icons.audio_file,
    'video': Icons.movie,
  };

  @override
  Widget build(BuildContext context) {
    final icon = _typeIcons[type] ?? Icons.movie;
    return AspectRatio(
      aspectRatio: 16 / 9,
      child: Container(
        key: const Key('share_viewer_thumbnail_placeholder'),
        decoration: BoxDecoration(
          color: Theme.of(context).colorScheme.surfaceContainerHighest,
          borderRadius: BorderRadius.circular(8),
        ),
        child: Icon(
          icon,
          size: 72,
          color: Theme.of(context).colorScheme.onSurfaceVariant,
        ),
      ),
    );
  }
}

/// One-line row showing the media type (capitalised) and formatted duration.
///
/// Placed below the filename so the viewer can see at a glance what kind of
/// media the link points to and how long it is before tapping Play.
class _MetadataRow extends StatelessWidget {
  const _MetadataRow({required this.page});

  final SharePageMetadata page;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final typeLabel = _capitalise(page.type);
    final durationLabel =
        page.duration != null ? formatDuration(page.duration!) : null;

    final parts = [typeLabel, if (durationLabel != null) durationLabel];

    return Text(
      parts.join(' · '),
      key: const Key('share_viewer_metadata'),
      style: theme.textTheme.bodyMedium?.copyWith(
        color: theme.colorScheme.onSurfaceVariant,
      ),
      textAlign: TextAlign.center,
    );
  }

  /// Returns [s] with its first character uppercased.
  static String _capitalise(String s) =>
      s.isEmpty ? s : '${s[0].toUpperCase()}${s.substring(1)}';
}

/// Full-screen error view with a retry button.
///
/// Shown when [getSharedMediaPage] throws — e.g. 404 (invalid token),
/// 410 (expired link), or a network failure.  The [message] comes from
/// [shareViewerErrorMessage], which maps exceptions to human-readable strings.
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
              Icons.link_off,
              size: 56,
              color: Theme.of(context).colorScheme.error,
            ),
            const SizedBox(height: 16),
            Text(
              message,
              key: const Key('share_viewer_error'),
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.bodyLarge,
            ),
            const SizedBox(height: 24),
            ElevatedButton.icon(
              key: const Key('share_viewer_retry'),
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
