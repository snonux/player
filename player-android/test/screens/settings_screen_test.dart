// Widget tests for SettingsScreen.
//
// Tests cover:
//   1. Displays username: the token stored in TokenStorage is shown as the
//      signed-in username.
//   2. Saves base URL: entering a URL and tapping Save persists it via the
//      settings provider.
//   3. Logout flow: tapping Log Out calls AuthStateNotifier.logout and clears
//      the stored token.
//   4. Admin section: "Manage Users" tile shown only for admin users;
//      hidden for non-admin users.
//
// Riverpod providers are overridden with in-memory fakes so tests run without
// a real server, OS keychain, or SharedPreferences disk I/O.
//
// Run with: flutter test test/screens/settings_screen_test.dart

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/models/models.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/current_user_provider.dart';
import 'package:player_android/providers/settings_provider.dart';
import 'package:player_android/providers/theme_provider.dart';
import 'package:player_android/screens/settings_screen.dart';

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

/// In-memory [TokenStorage] that avoids the platform OS keychain in tests.
class _FakeTokenStorage implements TokenStorage {
  String? _token;

  @override
  Future<String?> readToken() async => _token;

  @override
  Future<void> writeToken(String token) async => _token = token;

  @override
  Future<void> deleteToken() async => _token = null;
}

/// In-memory [SettingsNotifier] whose state is set directly in tests.
///
/// Uses [AsyncNotifier] the same way the production notifier does, but
/// bypasses [SharedPreferences] so tests have no disk I/O.
class _FakeSettingsNotifier extends SettingsNotifier {
  _FakeSettingsNotifier(this._initialUrl);

  final String _initialUrl;

  // Captures the last URL passed to setServerBaseUrl for test assertions.
  String? savedUrl;

  @override
  Future<AppSettings> build() async =>
      AppSettings(serverBaseUrl: _initialUrl);

  @override
  Future<void> setServerBaseUrl(String url) async {
    savedUrl = url;
    // Mirror the production implementation: update in-memory state immediately.
    state = AsyncData(AppSettings(serverBaseUrl: url));
  }
}

/// In-memory [ThemeNotifier] that bypasses [SharedPreferences] in tests.
///
/// Starts at [ThemeMode.system] (the default) and records the last mode passed
/// to [setThemeMode] so tests can assert on the captured value.
class _FakeThemeNotifier extends ThemeNotifier {
  // Tracks the last mode applied via setThemeMode for test assertions.
  ThemeMode? capturedMode;

  @override
  Future<ThemeMode> build() async => ThemeMode.system;

  @override
  Future<void> setThemeMode(ThemeMode mode) async {
    capturedMode = mode;
    // Mirror the production behaviour: update in-memory state immediately.
    state = AsyncData(mode);
  }
}

// ---------------------------------------------------------------------------
// Helper: pump SettingsScreen inside a minimal ProviderScope.
// ---------------------------------------------------------------------------

/// Pumps [SettingsScreen] inside a [ProviderScope] that overrides:
///   - [tokenStorageProvider] with an in-memory fake (avoids OS keychain)
///   - [settingsProvider] with an in-memory fake (avoids SharedPreferences)
///   - [themeProvider] with [themeNotifier] when provided (avoids SharedPreferences)
///   - [currentUserProvider] with [currentUser] when provided (drives admin gating)
///
/// Uses [MaterialApp.router] with a minimal [GoRouter] so that [context.go]
/// calls inside [SettingsScreen._logout] and the admin tile do not throw
/// "No GoRouter in context".
///
/// Returns a record containing:
///   - [storage]: the fake token storage for post-test assertions.
///   - [settings]: the fake settings notifier for post-test assertions.
Future<({_FakeTokenStorage storage, _FakeSettingsNotifier settings})>
    _pumpSettingsScreen(
  WidgetTester tester, {
  String initialToken = 'alice',
  String initialUrl = 'http://10.0.2.2:8080',
  _FakeThemeNotifier? themeNotifier,
  User? currentUser,
}) async {
  final fakeStorage = _FakeTokenStorage().._token = initialToken;
  final fakeSettings = _FakeSettingsNotifier(initialUrl);

  // A minimal GoRouter that renders SettingsScreen at '/'.  The /login and
  // /admin/users routes are included so that context.go calls inside the
  // screen do not trigger "route not found" errors.
  final router = GoRouter(
    initialLocation: '/',
    routes: [
      GoRoute(
        path: '/',
        builder: (_, __) => const SettingsScreen(),
      ),
      GoRoute(
        path: '/login',
        builder: (_, __) => const Scaffold(body: Text('Login')),
      ),
      GoRoute(
        path: '/admin/users',
        builder: (_, __) => const Scaffold(body: Text('Admin Users')),
      ),
    ],
  );

  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        // Avoid OS keychain / SharedPreferences in tests.
        tokenStorageProvider.overrideWithValue(fakeStorage),
        settingsProvider.overrideWith(() => fakeSettings),
        // Override theme provider when the caller supplies a fake notifier.
        if (themeNotifier != null)
          themeProvider.overrideWith(() => themeNotifier),
        // Override currentUserProvider to control admin-section visibility
        // without a real listUsers round-trip.
        if (currentUser != null)
          currentUserProvider.overrideWith((ref) async => currentUser),
      ],
      child: MaterialApp.router(routerConfig: router),
    ),
  );

  // Allow async providers (_currentUsernameProvider, settingsProvider,
  // currentUserProvider) to resolve their futures before we inspect the tree.
  await tester.pumpAndSettle();

  return (storage: fakeStorage, settings: fakeSettings);
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

void main() {
  // --------------------------------------------------------------------------
  // Username display
  // --------------------------------------------------------------------------

  group('username display', () {
    testWidgets('shows the token stored in TokenStorage as the username',
        (tester) async {
      await _pumpSettingsScreen(tester, initialToken: 'alice');

      // The settings_username widget should show the stored token value.
      expect(find.byKey(const Key('settings_username')), findsOneWidget);
      expect(find.text('alice'), findsOneWidget);
    });

    testWidgets('shows placeholder when no token is stored', (tester) async {
      // Pump with an empty token — simulates a freshly logged-out state
      // where the screen is still mounted transiently before redirect.
      final fakeStorage = _FakeTokenStorage(); // _token is null
      final fakeSettings = _FakeSettingsNotifier('http://10.0.2.2:8080');

      final router = GoRouter(
        initialLocation: '/',
        routes: [
          GoRoute(
            path: '/',
            builder: (_, __) => const SettingsScreen(),
          ),
          GoRoute(
            path: '/login',
            builder: (_, __) => const Scaffold(body: Text('Login')),
          ),
        ],
      );

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            tokenStorageProvider.overrideWithValue(fakeStorage),
            settingsProvider.overrideWith(() => fakeSettings),
          ],
          child: MaterialApp.router(routerConfig: router),
        ),
      );
      await tester.pumpAndSettle();

      // When the token is null, the username row shows the fallback '—'.
      expect(find.text('—'), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Base URL editing
  // --------------------------------------------------------------------------

  group('base URL', () {
    testWidgets('pre-fills the URL field from the settings provider',
        (tester) async {
      await _pumpSettingsScreen(
        tester,
        initialUrl: 'https://player.example.com',
      );

      // The URL field should be seeded with the persisted value.
      final field = tester.widget<TextField>(
        find.byKey(const Key('settings_base_url')),
      );
      expect(field.controller?.text, equals('https://player.example.com'));
    });

    testWidgets('saves URL when Save URL button is tapped', (tester) async {
      final result = await _pumpSettingsScreen(
        tester,
        initialUrl: 'http://10.0.2.2:8080',
      );

      // Edit the URL field.
      await tester.tap(find.byKey(const Key('settings_base_url')));
      await tester.pump();
      await tester.enterText(
        find.byKey(const Key('settings_base_url')),
        'https://new-server.example.com',
      );

      // Tap Save URL.
      await tester.tap(find.byKey(const Key('settings_save_url')));
      await tester.pumpAndSettle();

      // The fake notifier should have captured the new URL.
      expect(
        result.settings.savedUrl,
        equals('https://new-server.example.com'),
      );
    });

    testWidgets('saves URL when keyboard Done action is triggered',
        (tester) async {
      final result = await _pumpSettingsScreen(tester);

      await tester.tap(find.byKey(const Key('settings_base_url')));
      await tester.pump();
      await tester.enterText(
        find.byKey(const Key('settings_base_url')),
        'http://192.168.1.100:8080',
      );

      // Simulate the "Done" keyboard action.
      await tester.testTextInput.receiveAction(TextInputAction.done);
      await tester.pumpAndSettle();

      expect(
        result.settings.savedUrl,
        equals('http://192.168.1.100:8080'),
      );
    });

    testWidgets('does not save when URL field is empty', (tester) async {
      final result = await _pumpSettingsScreen(
        tester,
        initialUrl: 'http://10.0.2.2:8080',
      );

      // Clear the field and tap Save.
      await tester.enterText(find.byKey(const Key('settings_base_url')), '');
      await tester.tap(find.byKey(const Key('settings_save_url')));
      await tester.pumpAndSettle();

      // Nothing should have been saved since the field was blank.
      expect(result.settings.savedUrl, isNull);
    });
  });

  // --------------------------------------------------------------------------
  // Theme toggle
  // --------------------------------------------------------------------------

  group('theme toggle', () {
    testWidgets('renders with system segment selected by default',
        (tester) async {
      final fakeTheme = _FakeThemeNotifier();
      await _pumpSettingsScreen(tester, themeNotifier: fakeTheme);

      // The segmented button should be present with the system segment selected.
      final button = tester.widget<SegmentedButton<ThemeMode>>(
        find.byKey(const Key('settings_theme_toggle')),
      );
      expect(button.selected, equals({ThemeMode.system}));
    });

    testWidgets('tapping a segment calls setThemeMode with the right ThemeMode',
        (tester) async {
      final fakeTheme = _FakeThemeNotifier();
      await _pumpSettingsScreen(tester, themeNotifier: fakeTheme);

      // Scroll the segmented button into view before tapping — the settings
      // screen content may exceed the test viewport height.
      await tester.ensureVisible(find.byKey(const Key('settings_theme_toggle')));
      await tester.pumpAndSettle();

      // Tap the "Dark" segment label.
      await tester.tap(find.text('Dark'));
      await tester.pumpAndSettle();

      // The fake notifier should have received ThemeMode.dark.
      expect(fakeTheme.capturedMode, equals(ThemeMode.dark));

      // The widget should also reflect the new theme state visually.
      final button = tester.widget<SegmentedButton<ThemeMode>>(
        find.byKey(const Key('settings_theme_toggle')),
      );
      expect(button.selected, equals({ThemeMode.dark}));
    });
  });

  // --------------------------------------------------------------------------
  // Logout flow
  // --------------------------------------------------------------------------

  group('logout flow', () {
    testWidgets('logout button is visible and enabled initially',
        (tester) async {
      await _pumpSettingsScreen(tester);

      expect(find.byKey(const Key('settings_logout')), findsOneWidget);
      expect(find.byType(CircularProgressIndicator), findsNothing);
    });

    testWidgets('tapping logout clears the token from TokenStorage',
        (tester) async {
      final result = await _pumpSettingsScreen(
        tester,
        initialToken: 'alice',
      );

      // Verify the token is set before logout.
      expect(result.storage._token, equals('alice'));

      // Tap the logout button.
      await tester.tap(find.byKey(const Key('settings_logout')));
      await tester.pumpAndSettle();

      // AuthStateNotifier.logout() should have deleted the token.
      expect(result.storage._token, isNull);
    });

    testWidgets('tapping logout updates auth state to unauthenticated',
        (tester) async {
      // Capture the auth state notifier to inspect state after logout.
      AuthState? capturedState;
      final fakeStorage = _FakeTokenStorage().._token = 'bob';
      final fakeSettings = _FakeSettingsNotifier('http://10.0.2.2:8080');

      // Minimal GoRouter: '/' renders SettingsScreen, '/login' is the redirect
      // target so context.go('/login') in the logout handler does not throw.
      final router = GoRouter(
        initialLocation: '/',
        routes: [
          GoRoute(
            path: '/',
            builder: (_, __) => Consumer(
              builder: (context, ref, _) {
                // Watch and capture auth state for post-logout assertion.
                final authAsync = ref.watch(authStateProvider);
                authAsync.whenData((s) => capturedState = s);
                return const SettingsScreen();
              },
            ),
          ),
          GoRoute(
            path: '/login',
            builder: (_, __) => const Scaffold(body: Text('Login')),
          ),
        ],
      );

      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            tokenStorageProvider.overrideWithValue(fakeStorage),
            settingsProvider.overrideWith(() => fakeSettings),
          ],
          child: MaterialApp.router(routerConfig: router),
        ),
      );
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('settings_logout')));
      await tester.pumpAndSettle();

      // After logout the auth state should be unauthenticated.
      expect(capturedState?.isUnauthenticated, isTrue);
    });
  });

  // --------------------------------------------------------------------------
  // Admin section visibility
  // --------------------------------------------------------------------------

  group('admin section visibility', () {
    testWidgets('shows Manage Users tile when current user is admin',
        (tester) async {
      const adminUser = User(id: 1, username: 'alice', isAdmin: true);
      await _pumpSettingsScreen(
        tester,
        initialToken: 'alice',
        currentUser: adminUser,
      );

      // Scroll to ensure the admin section is rendered in the viewport.
      await tester.ensureVisible(
        find.byKey(const Key('settings_manage_users')),
      );
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('settings_manage_users')), findsOneWidget);
      expect(find.text('Manage Users'), findsOneWidget);
    });

    testWidgets('hides Manage Users tile when current user is not admin',
        (tester) async {
      const regularUser = User(id: 2, username: 'bob', isAdmin: false);
      await _pumpSettingsScreen(
        tester,
        initialToken: 'bob',
        currentUser: regularUser,
      );

      // The admin tile must not be present for a non-admin user.
      expect(find.byKey(const Key('settings_manage_users')), findsNothing);
      expect(find.text('Administration'), findsNothing);
    });

    testWidgets('hides admin section when currentUserProvider returns null',
        (tester) async {
      // No currentUser override → currentUserProvider returns null (loading
      // or unauthenticated) → admin section stays hidden.
      await _pumpSettingsScreen(tester, initialToken: 'alice');

      expect(find.byKey(const Key('settings_manage_users')), findsNothing);
    });
  });
}
