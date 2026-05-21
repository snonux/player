import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

// SharedPreferences key for the server base URL setting.
const _kBaseUrlKey = 'server_base_url';

// Default base URL used when the user has not yet configured one.
// Points to the local Android emulator loopback address so the app is
// runnable out-of-the-box without any manual configuration.
// Private: only referenced within this file; callers read the resolved URL
// through [AppSettings.serverBaseUrl] obtained from [settingsProvider].
const _kDefaultBaseUrl = 'http://10.0.2.2:8080';

/// Immutable snapshot of persisted app settings.
///
/// Keeping settings as a value object means every state change produces a new
/// instance, which plays well with Riverpod's equality-based rebuild suppression
/// and keeps the notifier's contract straightforward.
class AppSettings {
  const AppSettings({required this.serverBaseUrl});

  /// The base URL of the player-server API (e.g. "https://player.example.com").
  final String serverBaseUrl;

  @override
  bool operator ==(Object other) =>
      identical(this, other) ||
      other is AppSettings &&
          runtimeType == other.runtimeType &&
          serverBaseUrl == other.serverBaseUrl;

  @override
  int get hashCode => serverBaseUrl.hashCode;

  @override
  String toString() => 'AppSettings(serverBaseUrl: $serverBaseUrl)';
}

/// Manages persisted app settings via [SharedPreferences].
///
/// Uses [AsyncNotifier] because the initial state load is async (disk read).
/// After initialisation, [setServerBaseUrl] writes to disk and updates state
/// synchronously so the UI reflects changes immediately.
///
/// Design notes (SRP / ISP):
///   - This notifier owns only settings persistence; auth is handled separately
///     by [AuthStateNotifier] to maintain single responsibility.
///   - [SharedPreferences] is created internally rather than injected because
///     it is a platform singleton; tests override the entire provider via
///     [ProviderScope] overrides instead.
class SettingsNotifier extends AsyncNotifier<AppSettings> {
  @override
  Future<AppSettings> build() async {
    // Load persisted settings from disk on first access.  The platform
    // SharedPreferences instance is a singleton; obtaining it here is cheap
    // because subsequent calls return the cached instance.
    final prefs = await SharedPreferences.getInstance();
    final url = prefs.getString(_kBaseUrlKey) ?? _kDefaultBaseUrl;
    return AppSettings(serverBaseUrl: url);
  }

  /// Persists [url] as the new server base URL and updates the in-memory state.
  ///
  /// The UI calls this when the user edits the URL field and submits.  The
  /// async write to [SharedPreferences] is awaited so that a subsequent cold
  /// start will see the new value; the in-memory state is updated first so the
  /// UI is not blocked on the disk write.
  Future<void> setServerBaseUrl(String url) async {
    // Update in-memory state first for immediate UI feedback.
    state = AsyncData(AppSettings(serverBaseUrl: url));

    // Persist to disk so the value survives app restarts.
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_kBaseUrlKey, url);
  }
}

/// The single source of truth for persisted app settings.
///
/// Currently consumed by [SettingsScreen] for displaying and editing settings.
/// Will also be consumed by [apiClientProvider] (for the server base URL) once
/// that provider is wired to read from settings rather than
/// [String.fromEnvironment] — tracked as a future task.
final settingsProvider =
    AsyncNotifierProvider<SettingsNotifier, AppSettings>(
  SettingsNotifier.new,
);
