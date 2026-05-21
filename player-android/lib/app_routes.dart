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

  /// Returns the concrete path for a media-detail page given a numeric [id].
  static String mediaDetailPath(int id) => '/media/$id';
}
