import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'app_routes.dart';
import 'navigation_key.dart';
import 'providers/auth_state_provider.dart';
import 'providers/first_run_provider.dart';
import 'screens/audio_player_screen.dart';
import 'screens/bootstrap_screen.dart';
import 'screens/continue_watching_screen.dart';
import 'screens/home_screen.dart';
import 'screens/login_screen.dart';
import 'screens/media_detail_screen.dart';
import 'screens/media_grid_screen.dart';
import 'screens/podcast_episodes_screen.dart';
import 'screens/podcast_list_screen.dart';
import 'screens/settings_screen.dart';
import 'screens/share_screen.dart';
import 'screens/my_shares_screen.dart';
import 'screens/notes_editor_screen.dart';
import 'screens/folder_browser_screen.dart';
import 'screens/video_player_screen.dart';

// Re-export AppRoutes so existing callers that import router.dart for routes
// do not need to change their import path.
export 'app_routes.dart' show AppRoutes;

// ---------------------------------------------------------------------------
// Router provider
// ---------------------------------------------------------------------------

/// Builds the [GoRouter] instance as a Riverpod [Provider] so that:
///   1. The navigator key is shared with [DioClient] (enabling 401 redirects).
///   2. The [refreshListenable] is driven by [authStateProvider] changes,
///      which triggers redirect re-evaluation on every auth state transition.
///   3. The provider is created lazily and disposed with [ProviderScope].
final routerProvider = Provider<GoRouter>((ref) {
  // Watch auth state so the router is rebuilt when it changes.
  // Using a ChangeNotifier bridge because GoRouter's refreshListenable expects
  // a Listenable, while Riverpod exposes streams/notifiers.
  final notifier = _RouterRefreshNotifier(ref);

  return GoRouter(
    // Share the navigator key with DioClient so imperative 401 redirects
    // work through go_router rather than the raw Navigator.
    navigatorKey: navigatorKey,

    // Trigger redirect re-evaluation whenever auth state changes.
    refreshListenable: notifier,

    // Default entry point before redirect logic resolves.
    initialLocation: AppRoutes.home,

    redirect: (context, state) {
      final authAsync = ref.read(authStateProvider);

      // While the initial token check is in-flight, hold the current path.
      // The router will re-evaluate once refreshListenable fires.
      if (authAsync.isLoading || authAsync.hasError) return null;

      final auth = authAsync.requireValue;
      final location = state.matchedLocation;
      final isLoginRoute = location == AppRoutes.login;
      // Bootstrap is a public route (user is unauthenticated by definition).
      final isBootstrapRoute = location == AppRoutes.bootstrap;

      if (auth.isAuthenticated && (isLoginRoute || isBootstrapRoute)) {
        // Prevent already-authenticated users from viewing auth/setup screens.
        return AppRoutes.home;
      }

      if (auth.isUnauthenticated && !isLoginRoute && !isBootstrapRoute) {
        // Unauthenticated: any route other than /login and /bootstrap is
        // protected.  This covers /home, /media/:id, /share, /settings, and
        // any future authenticated routes added to the route table.
        //
        // Determine whether this is first-run (no users exist yet) or a normal
        // returning-user scenario.  firstRunProvider returns true when the
        // server reports count == 0.
        //
        // While the check is loading we stay put; the router re-evaluates when
        // firstRunProvider's AsyncValue settles (via refreshListenable).
        final firstRunAsync = ref.read(firstRunProvider);
        if (firstRunAsync.isLoading) return null;

        // On first-run redirect to /bootstrap so the admin account can be set
        // up; otherwise send to /login for normal credential entry.
        final isFirstRun = firstRunAsync.valueOrNull ?? false;
        return isFirstRun ? AppRoutes.bootstrap : AppRoutes.login;
      }

      // No redirect needed.
      return null;
    },

    routes: [
      GoRoute(
        path: AppRoutes.bootstrap,
        builder: (context, state) => const BootstrapScreen(),
      ),
      GoRoute(
        path: AppRoutes.login,
        builder: (context, state) => const LoginScreen(),
      ),
      GoRoute(
        path: AppRoutes.home,
        // HomeScreen now hosts SetsListScreen — the real media-library view.
        builder: (context, state) => const SetsListScreen(),
      ),
      GoRoute(
        // Continue-watching screen — lists all in-progress media items.
        path: AppRoutes.continueWatching,
        builder: (context, state) => const ContinueWatchingScreen(),
      ),
      GoRoute(
        path: AppRoutes.mediaGrid,
        builder: (context, state) {
          // The ':setId' path parameter is guaranteed by the route pattern.
          final raw = state.pathParameters['setId']!;
          final setId = int.tryParse(raw) ?? 0;
          // The set name is optionally passed as a route extra (String) by the
          // calling screen (e.g. SetsListScreen) so the app bar can show it
          // immediately without an extra API call.
          final setName = state.extra is String ? state.extra as String : null;
          return MediaGridScreen(setId: setId, setName: setName);
        },
      ),
      GoRoute(
        path: AppRoutes.mediaDetail,
        builder: (context, state) {
          // The ':id' path parameter is guaranteed by the route pattern.
          final id = state.pathParameters['id']!;
          return MediaDetailScreen(mediaId: id);
        },
      ),
      GoRoute(
        path: AppRoutes.share,
        builder: (context, state) => const ShareScreen(),
      ),
      GoRoute(
        path: AppRoutes.settings,
        builder: (context, state) => const SettingsScreen(),
      ),
      GoRoute(
        // Podcast list screen — shows all sets with isPodcast == true.
        // A FAB inside the screen opens the SubscribeDialog.
        path: AppRoutes.podcasts,
        builder: (context, state) => const PodcastListScreen(),
      ),
      GoRoute(
        // Podcast episodes screen — shows all episodes for a podcast set.
        // The ':setId' path segment is the numeric podcast set identifier.
        // The optional 'name' query parameter provides the feed title for the
        // app bar without an extra API round-trip.
        path: AppRoutes.podcastEpisodes,
        builder: (context, state) {
          final raw = state.pathParameters['setId']!;
          final setId = int.tryParse(raw) ?? 0;
          final setName = state.uri.queryParameters['name'];
          return PodcastEpisodesScreen(setId: setId, setName: setName);
        },
      ),
      GoRoute(
        path: AppRoutes.videoPlayer,
        builder: (context, state) {
          // ':mediaId' is guaranteed present by the route pattern.
          final mediaId = state.pathParameters['mediaId']!;
          final (mediaUrl, startPosition) = _parsePlayerExtra(state.extra);
          return VideoPlayerScreen(
            mediaId: mediaId,
            mediaUrl: mediaUrl,
            startPosition: startPosition,
          );
        },
      ),
      GoRoute(
        path: AppRoutes.audioPlayer,
        builder: (context, state) {
          // ':mediaId' is guaranteed present by the route pattern.
          final mediaId = state.pathParameters['mediaId']!;
          final (mediaUrl, startPosition) = _parsePlayerExtra(state.extra);
          return AudioPlayerScreen(
            mediaId: mediaId,
            mediaUrl: mediaUrl,
            startPosition: startPosition,
          );
        },
      ),
      GoRoute(
        // Notes editor — shows and edits the user's personal note for a media
        // item.  The ':mediaId' segment is the numeric media item identifier.
        path: AppRoutes.notes,
        builder: (context, state) {
          final mediaId = state.pathParameters['mediaId']!;
          return NotesEditorScreen(mediaId: mediaId);
        },
      ),
      GoRoute(
        // My Shares — lists all share links created by the authenticated user.
        // Reachable from the Settings screen.
        path: AppRoutes.shares,
        builder: (context, state) => const MySharesScreen(),
      ),
      GoRoute(
        // Folder browser — shows subfolders and media at the current path
        // within a set.  The ':setId' path segment identifies the set;
        // the optional 'path' query parameter identifies the current subfolder
        // (absent or empty means root).
        path: AppRoutes.folderBrowser,
        builder: (context, state) {
          final raw = state.pathParameters['setId']!;
          final setId = int.tryParse(raw) ?? 0;
          final path = state.uri.queryParameters['path'];
          // setName is optionally passed as a String extra so the screen can
          // show the set name in the app bar without an extra API call.
          final setName =
              state.extra is String ? state.extra as String : null;
          return FolderBrowserScreen(
            setId: setId,
            path: path,
            setName: setName,
          );
        },
      ),
    ],
  );
});

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

/// Parses the route [extra] passed to video and audio player routes.
///
/// Accepts two shapes forwarded by different call-sites:
///   - `Map<String, dynamic>`: `{mediaUrl: String, position: double}` from
///     [ContinueWatchingScreen] so the player can seek immediately without an
///     extra [getMediaProgress] round-trip.
///   - `String`: plain stream URL forwarded by [MediaDetailScreen].
///
/// Returns a record `(mediaUrl, startPosition)` with null for absent values.
/// Extracted to avoid duplicating this logic across the video and audio routes.
(String?, double?) _parsePlayerExtra(Object? extra) {
  if (extra is Map<String, dynamic>) {
    final mediaUrl = extra['mediaUrl'] as String?;
    final startPosition = (extra['position'] as num?)?.toDouble();
    return (mediaUrl, startPosition);
  }
  return (extra is String ? extra : null, null);
}

/// Bridges Riverpod's auth and first-run providers to [GoRouter.refreshListenable].
///
/// GoRouter expects a [ChangeNotifier] (or any [Listenable]) for its refresh
/// mechanism.  This notifier listens to both [authStateProvider] and
/// [firstRunProvider], calling [notifyListeners] on every change so the router
/// re-runs its redirect callback whenever auth state or first-run status settles.
class _RouterRefreshNotifier extends ChangeNotifier {
  _RouterRefreshNotifier(Ref ref) {
    // Listen to auth state changes (login, logout, token expiry).
    _authSubscription = ref.listen<AsyncValue<AuthState>>(
      authStateProvider,
      (_, __) => notifyListeners(),
    );
    // Listen to first-run state so the router re-evaluates after the initial
    // user-count check resolves from loading to a concrete true/false value.
    _firstRunSubscription = ref.listen<AsyncValue<bool>>(
      firstRunProvider,
      (_, __) => notifyListeners(),
    );
  }

  late final ProviderSubscription<AsyncValue<AuthState>> _authSubscription;
  late final ProviderSubscription<AsyncValue<bool>> _firstRunSubscription;

  @override
  void dispose() {
    _authSubscription.close();
    _firstRunSubscription.close();
    super.dispose();
  }
}
