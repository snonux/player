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

  /// Returns the concrete path for the notes editor of a given [mediaId].
  static String notesPath(String mediaId) => '/notes/$mediaId';
}
