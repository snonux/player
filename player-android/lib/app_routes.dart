/// Centralised route path constants for go_router.
///
/// Keeping these in a standalone file lets screen widgets reference route
/// paths without importing router.dart, which would create circular imports
/// (router.dart imports screen files; screens should not import the router).
abstract final class AppRoutes {
  static const login = '/login';
  static const home = '/home';
  static const mediaDetail = '/media/:id';
  static const share = '/share';

  /// Public share-viewer route — no authentication required.
  ///
  /// The ':token' segment is the opaque share token issued by the server.
  /// This route is intentionally outside the authenticated route set so
  /// anyone with a share link can view the shared media without logging in.
  static const shareViewer = '/share/:token';

  static const settings = '/settings';

  /// First-run setup route shown when no admin account exists yet.
  static const bootstrap = '/bootstrap';

  /// Route that lists media items inside a specific set.
  /// The ':setId' segment is a numeric set identifier.
  static const mediaGrid = '/sets/:setId';

  /// Route for the video player screen.
  /// The ':mediaId' segment identifies the media item to play.
  static const videoPlayer = '/video/:mediaId';

  /// Route for the audio player screen.
  /// The ':mediaId' segment identifies the media item to play.
  static const audioPlayer = '/audio/:mediaId';

  /// Route that lists all podcast feeds (sets where isPodcast is true).
  /// Opens [PodcastListScreen] and supports the SubscribeDialog FAB.
  static const podcasts = '/podcasts';

  /// Route that shows the Continue Watching screen (in-progress media items).
  static const continueWatching = '/continue';

  /// Route for the notes editor screen for a specific media item.
  /// The ':mediaId' segment identifies the media item whose note is edited.
  static const notes = '/notes/:mediaId';

  /// URL prefix used in the router redirect guard to identify share-viewer URLs.
  ///
  /// The go_router pattern [shareViewer] is a template string with a `:token`
  /// placeholder and cannot be used directly as a prefix check.  This constant
  /// encapsulates the literal prefix so [router.dart]'s redirect logic can
  /// reference a named constant rather than embedding a raw string literal
  /// (Dependency Inversion — high-level routing policy depends on this abstraction,
  /// not on a hardcoded '/share/' string).
  static const shareViewerPrefix = '/share/';

  /// Returns the concrete path for the share-viewer page of a given [token].
  static String shareViewerPath(String token) => '/share/$token';

  /// Returns the concrete path for a media-detail page given a numeric [id].
  static String mediaDetailPath(int id) => '/media/$id';

  /// Returns the concrete path for the media-grid page of a given [setId].
  static String mediaGridPath(int setId) => '/sets/$setId';

  /// Returns the concrete path for the video player of a given [mediaId].
  static String videoPlayerPath(String mediaId) => '/video/$mediaId';

  /// Returns the concrete path for the audio player of a given [mediaId].
  static String audioPlayerPath(String mediaId) => '/audio/$mediaId';

  /// Returns the appropriate player path for [type] and [mediaId].
  ///
  /// Centralises the audio-vs-video routing decision so call-sites do not need
  /// to repeat the same if/else.  Audio maps to [audioPlayerPath]; every other
  /// type (including 'video' and unknown) maps to [videoPlayerPath].
  static String playerPathForType(String type, String mediaId) =>
      type == 'audio' ? audioPlayerPath(mediaId) : videoPlayerPath(mediaId);

  /// Route that lists all share links created by the authenticated user.
  /// Opens [MySharesScreen] from Settings.
  static const shares = '/shares';

  /// Route for browsing folders within a set.
  ///
  /// The ':setId' path segment identifies the set; the optional 'path' query
  /// parameter specifies the current subfolder (empty or absent = root).
  static const folderBrowser = '/browse/:setId';

  /// Returns the concrete path for the folder browser of a given [setId].
  ///
  /// [path] is the optional subfolder path; omit or pass null for the root.
  static String folderBrowserPath(int setId, {String? path}) {
    final base = '/browse/$setId';
    if (path == null || path.isEmpty) return base;
    return '$base?path=${Uri.encodeComponent(path)}';
  }

  /// Returns the concrete path for the notes editor of a given [mediaId].
  static String notesPath(String mediaId) => '/notes/$mediaId';

  /// Route that lists episodes for a single podcast set.
  ///
  /// The ':setId' path segment is the numeric podcast set identifier.
  /// Opens [PodcastEpisodesScreen] where the user can see episode titles,
  /// played state, and playback progress.
  static const podcastEpisodes = '/podcasts/:setId/episodes';

  /// Returns the concrete path for the podcast-episodes screen of [setId].
  ///
  /// [setName] is optionally passed as a URL query parameter so the app bar
  /// can show it without an extra API call.
  static String podcastEpisodesPath(int setId, {String? setName}) {
    final base = '/podcasts/$setId/episodes';
    if (setName == null || setName.isEmpty) return base;
    return '$base?name=${Uri.encodeComponent(setName)}';
  }

  /// Route for the admin user management screen.
  ///
  /// Only accessible when the authenticated user has admin privileges.
  /// Shows a list of all registered users and allows creating/deleting accounts.
  static const adminUsers = '/admin/users';

  /// Route for the admin permission matrix screen.
  ///
  /// Displays a cross-table of users vs. sets with checkboxes for granting or
  /// revoking per-user access to each set.  Admin-only.
  static const adminPermissions = '/admin/permissions';

  /// Route for the admin rescan screen.
  ///
  /// Allows an admin to trigger a library rescan and monitor live progress.
  static const adminRescan = '/admin/rescan';

  /// Route for the admin trash screen.
  ///
  /// Lists soft-deleted media items and allows restore or hard-delete.
  static const adminTrash = '/admin/trash';
}
