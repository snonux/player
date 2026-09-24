import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

// SharedPreferences key for the server base URL setting.
const _kBaseUrlKey = 'server_base_url';

// Default base URL used when the user has not yet configured one.
// Points to the local Android emulator loopback address so the app is
// runnable out-of-the-box without any manual configuration.
// Private: only referenced within this file; callers read the resolved URL
// through [AppSettings.serverBaseUrl] obtained from [settingsProvider].
const kPlayerBaseUrl = String.fromEnvironment(
  'PLAYER_BASE_URL',
  defaultValue: 'http://10.0.2.2:8080',
);

/// Accept only a server origin. API paths are rooted at /api/v1 and a URL
/// containing credentials, a path, or a query would be misleading or unsafe.
Uri parseServerBaseUrl(String value) {
  final uri = Uri.tryParse(value.trim());
  if (uri == null ||
      (uri.scheme != 'http' && uri.scheme != 'https') ||
      uri.host.isEmpty ||
      uri.userInfo.isNotEmpty ||
      (uri.path.isNotEmpty && uri.path != '/') ||
      uri.hasQuery ||
      uri.hasFragment) {
    throw const FormatException(
        'Enter an HTTP or HTTPS server address without a path.');
  }
  return Uri.parse(uri.origin);
}

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
/// After initialisation, [setServerBaseUrl] validates and persists the new
/// origin before publishing it to API consumers.
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
    final saved = prefs.getString(_kBaseUrlKey);
    try {
      return AppSettings(
          serverBaseUrl:
              parseServerBaseUrl(saved ?? kPlayerBaseUrl).toString());
    } on FormatException {
      return AppSettings(
          serverBaseUrl: parseServerBaseUrl(kPlayerBaseUrl).toString());
    }
  }

  /// Persists [url] as the new server base URL and updates the in-memory state.
  ///
  /// The UI calls this when the user edits the URL field and submits.  The
  /// async write to [SharedPreferences] is awaited so that a subsequent cold
  /// start and current API consumers agree on the same origin.
  Future<void> setServerBaseUrl(String url) async {
    final normalized = parseServerBaseUrl(url).toString();
    final prefs = await SharedPreferences.getInstance();
    if (!await prefs.setString(_kBaseUrlKey, normalized)) {
      throw StateError('Could not save server address');
    }
    state = AsyncData(AppSettings(serverBaseUrl: normalized));
  }
}

/// The single source of truth for persisted app settings.
///
/// The same setting feeds authenticated, public, and media clients.
final settingsProvider = AsyncNotifierProvider<SettingsNotifier, AppSettings>(
  SettingsNotifier.new,
);
