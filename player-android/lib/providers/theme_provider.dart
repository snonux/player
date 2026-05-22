import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

// SharedPreferences key for the persisted theme mode.
const _kThemeModeKey = 'theme_mode';

// Mapping between the persisted string value and [ThemeMode] enum.
// Using explicit strings (not enum index) so the stored values are stable
// across code refactors that might change enum ordering.
const _kLight = 'light';
const _kDark = 'dark';
const _kSystem = 'system';

/// Returns the [ThemeMode] that corresponds to a persisted string.
///
/// Falls back to [ThemeMode.system] for any unknown or null value so that a
/// fresh install (or a corrupt preference) always behaves sensibly.
ThemeMode _themeModeFromString(String? value) => switch (value) {
      _kLight => ThemeMode.light,
      _kDark => ThemeMode.dark,
      _ => ThemeMode.system,
    };

/// Returns the string that is persisted for a given [ThemeMode].
String _themeModeToString(ThemeMode mode) => switch (mode) {
      ThemeMode.light => _kLight,
      ThemeMode.dark => _kDark,
      ThemeMode.system => _kSystem,
    };

// ---------------------------------------------------------------------------
// Color schemes derived from player-server/web/css/theme.css
//
// Dark palette mirrors the CSS :root block; light palette mirrors
// [data-theme="light"].  Material 3 ColorScheme is built from the key tokens:
//   primary               ← --accent
//   onPrimary             ← --text-inverse / white
//   surface               ← --bg-surface
//   scaffoldBackgroundColor ← --bg-body
//   error                 ← --danger
// ---------------------------------------------------------------------------

/// Material 3 dark [ColorScheme] matching the server's default dark palette.
const darkColorScheme = ColorScheme(
  brightness: Brightness.dark,
  // Accent #5e9eff — the interactive highlight colour.
  primary: Color(0xFF5E9EFF),
  onPrimary: Color(0xFF0B0D12), // --text-inverse: dark text on accent buttons.
  primaryContainer: Color(0xFF1E222C), // --bg-elevated
  onPrimaryContainer: Color(0xFFE6E8EF), // --text-primary
  secondary: Color(0xFF3DDC84), // --success: used for "playing" states.
  onSecondary: Color(0xFF0B0D12),
  secondaryContainer: Color(0xFF161920), // --bg-surface
  onSecondaryContainer: Color(0xFFA3A8B8), // --text-secondary
  tertiary: Color(0xFFFFB300), // --warn
  onTertiary: Color(0xFF0B0D12),
  tertiaryContainer: Color(0xFF1B1F27), // --bg-surface-hover
  onTertiaryContainer: Color(0xFFE6E8EF),
  error: Color(0xFFF25C5C), // --danger
  onError: Color(0xFFFFFFFF),
  errorContainer: Color(0xFF1E222C),
  onErrorContainer: Color(0xFFF25C5C),
  surface: Color(0xFF161920), // --bg-surface
  onSurface: Color(0xFFE6E8EF), // --text-primary
  onSurfaceVariant: Color(0xFFA3A8B8), // --text-secondary
  outline: Color(0xFF252A36), // --border
  outlineVariant: Color(0xFF2E3546), // --border-strong
  shadow: Color(0xFF000000),
  scrim: Color(0xFF000000),
  inverseSurface: Color(0xFFE6E8EF),
  onInverseSurface: Color(0xFF0F1117),
  inversePrimary: Color(0xFF2B6CB0), // light accent for chip labels on dark bg
);

/// Material 3 light [ColorScheme] matching the server's [data-theme="light"] palette.
const lightColorScheme = ColorScheme(
  brightness: Brightness.light,
  // Accent #2b6cb0 — the interactive highlight colour in light mode.
  primary: Color(0xFF2B6CB0),
  onPrimary: Color(0xFFFFFFFF), // --text-inverse: white text on accent buttons.
  primaryContainer: Color(0xFFFFFFFF), // --bg-elevated
  onPrimaryContainer: Color(0xFF12131A), // --text-primary
  secondary: Color(0xFF258855), // --success
  onSecondary: Color(0xFFFFFFFF),
  secondaryContainer: Color(0xFFFFFFFF), // --bg-surface
  onSecondaryContainer: Color(0xFF4A4F5E), // --text-secondary
  tertiary: Color(0xFFFFB300), // --warn (unchanged in light mode)
  onTertiary: Color(0xFF12131A),
  tertiaryContainer: Color(0xFFF0F2F7), // --bg-surface-hover
  onTertiaryContainer: Color(0xFF12131A),
  error: Color(0xFFC53030), // --danger (light variant)
  onError: Color(0xFFFFFFFF),
  errorContainer: Color(0xFFF4F5F8),
  onErrorContainer: Color(0xFFC53030),
  surface: Color(0xFFFFFFFF), // --bg-surface
  onSurface: Color(0xFF12131A), // --text-primary
  onSurfaceVariant: Color(0xFF4A4F5E), // --text-secondary
  outline: Color(0xFFD6DAE4), // --border
  outlineVariant: Color(0xFFC3C9D6), // --border-strong
  shadow: Color(0xFF000000),
  scrim: Color(0xFF000000),
  inverseSurface: Color(0xFF12131A),
  onInverseSurface: Color(0xFFF4F5F8),
  inversePrimary: Color(0xFF5E9EFF), // dark accent for chip labels on light bg
);

// ---------------------------------------------------------------------------
// ThemeData factories
// ---------------------------------------------------------------------------

/// Builds a Material 3 [ThemeData] for dark mode.
///
/// [useMaterial3] must be true so that the ColorScheme tokens above are
/// interpreted correctly by all M3 components (NavigationBar, Card, etc.).
ThemeData buildDarkTheme() => ThemeData(
      useMaterial3: true,
      colorScheme: darkColorScheme,
      scaffoldBackgroundColor: const Color(0xFF0F1117), // --bg-body dark
    );

/// Builds a Material 3 [ThemeData] for light mode.
ThemeData buildLightTheme() => ThemeData(
      useMaterial3: true,
      colorScheme: lightColorScheme,
      scaffoldBackgroundColor: const Color(0xFFF4F5F8), // --bg-body light
    );

// ---------------------------------------------------------------------------
// ThemeNotifier
// ---------------------------------------------------------------------------

/// Manages the user's preferred [ThemeMode] and persists it via [SharedPreferences].
///
/// Uses [AsyncNotifier] because the initial load requires an async disk read.
/// After the first load, [setThemeMode] updates the in-memory state immediately
/// and then persists to disk so the UI is never blocked on I/O.
///
/// Design notes (SRP):
///   - Theme persistence is isolated here; color definitions live as constants
///     above.  [SettingsNotifier] handles other persisted settings (server URL)
///     and is kept separate to avoid growing a god-class.
class ThemeNotifier extends AsyncNotifier<ThemeMode> {
  @override
  Future<ThemeMode> build() async {
    // Read the persisted theme preference on first access.  SharedPreferences
    // returns a cached singleton on subsequent calls so this is cheap.
    final prefs = await SharedPreferences.getInstance();
    return _themeModeFromString(prefs.getString(_kThemeModeKey));
  }

  /// Updates the active [ThemeMode] and persists the choice to disk.
  ///
  /// In-memory state is updated first so [MaterialApp.themeMode] changes
  /// immediately without blocking on I/O.  If the disk write fails, the
  /// previous state is restored so in-memory and disk stay in sync.
  Future<void> setThemeMode(ThemeMode mode) async {
    final previous = state;
    state = AsyncData(mode);
    try {
      final prefs = await SharedPreferences.getInstance();
      await prefs.setString(_kThemeModeKey, _themeModeToString(mode));
    } catch (_) {
      // Roll back so in-memory and disk stay in sync.
      state = previous;
      rethrow;
    }
  }
}

/// The single source of truth for the active [ThemeMode].
///
/// Consumed by [PlayerAndroidApp] (via [themeProvider]) to set
/// [MaterialApp.themeMode], and by [SettingsScreen] to render the toggle.
final themeProvider = AsyncNotifierProvider<ThemeNotifier, ThemeMode>(
  ThemeNotifier.new,
);
