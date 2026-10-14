import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

const _kSavedServersKey = 'saved_servers';
const _kBaseUrlKey = 'server_base_url';
const _kLibraryDestinationKey = 'library_destination';
const _kLegacyAuthOriginKey = 'auth_origin';
const kPlayerBaseUrl = String.fromEnvironment(
  'PLAYER_BASE_URL',
  defaultValue: 'http://10.0.2.2:8080',
);

Uri parseServerBaseUrl(String value) {
  final uri = Uri.tryParse(value.trim());
  if (uri == null ||
      (uri.scheme != 'http' && uri.scheme != 'https') ||
      uri.host.isEmpty ||
      uri.userInfo.isNotEmpty ||
      (uri.path.isNotEmpty && uri.path != '/') ||
      uri.hasQuery ||
      uri.hasFragment ||
      uri.port < 1 ||
      uri.port > 65535) {
    throw const FormatException(
        'Enter an HTTP or HTTPS server address without a path.');
  }
  return Uri.parse(uri.origin);
}

enum LibraryDestination { local, server }

class AppSettings {
  const AppSettings({
    required this.serverBaseUrl,
    this.destination = LibraryDestination.server,
    this.configurationError,
    this.savedServers = const [],
    this.selectionRequired = false,
  });

  final String? serverBaseUrl;
  final List<String> savedServers;
  final bool selectionRequired;
  final LibraryDestination destination;
  final String? configurationError;

  @override
  bool operator ==(Object other) =>
      identical(this, other) ||
      other is AppSettings &&
          runtimeType == other.runtimeType &&
          serverBaseUrl == other.serverBaseUrl &&
          selectionRequired == other.selectionRequired &&
          savedServers.join("\n") == other.savedServers.join("\n") &&
          destination == other.destination &&
          configurationError == other.configurationError;

  @override
  int get hashCode => Object.hash(serverBaseUrl, destination,
      configurationError, selectionRequired, Object.hashAll(savedServers));

  @override
  String toString() =>
      'AppSettings(serverBaseUrl: $serverBaseUrl, destination: $destination)';
}

class SettingsNotifier extends AsyncNotifier<AppSettings> {
  @override
  Future<AppSettings> build() async {
    final prefs = await SharedPreferences.getInstance();
    final saved = prefs.getString(_kBaseUrlKey);
    final selected = prefs.getString(_kLibraryDestinationKey);
    String? origin;
    String? error;
    var hasConfiguredOrigin = false;

    if (saved != null) {
      try {
        origin = parseServerBaseUrl(saved).toString();
        hasConfiguredOrigin = true;
      } on FormatException catch (e) {
        error = e.message;
      }
    } else {
      // Older installs stored the origin only alongside auth state. Preserve
      // it even when credentials expired so the user can sign in again.
      final legacyOrigin = prefs.getString(_kLegacyAuthOriginKey);
      if (legacyOrigin != null) {
        try {
          origin = parseServerBaseUrl(legacyOrigin).toString();
          hasConfiguredOrigin = true;
          await prefs.setString(_kBaseUrlKey, origin);
        } on FormatException {
          error = 'Saved server address is invalid.';
        }
      }
    }

    final servers = <String>{};
    for (final savedServer
        in prefs.getStringList(_kSavedServersKey) ?? <String>[]) {
      try {
        servers.add(parseServerBaseUrl(savedServer).toString());
      } on FormatException {
        error ??= 'A saved server address is invalid.';
      }
    }
    if (origin != null) servers.add(origin);
    await prefs.setStringList(_kSavedServersKey, servers.toList());

    final destination = switch (selected) {
      'local' => LibraryDestination.local,
      'server' => LibraryDestination.server,
      _ => hasConfiguredOrigin
          ? LibraryDestination.server
          : LibraryDestination.local,
    };
    // Repeating this write after an interrupted migration is harmless.
    await prefs.setString(_kLibraryDestinationKey,
        destination == LibraryDestination.server ? 'server' : 'local');
    return AppSettings(
      serverBaseUrl: origin,
      destination: error == null ? destination : LibraryDestination.local,
      configurationError: error,
      savedServers: List.unmodifiable(servers),
      selectionRequired: servers.isNotEmpty,
    );
  }

  Future<void> setServerBaseUrl(String url) async {
    final normalized = parseServerBaseUrl(url).toString();
    final prefs = await SharedPreferences.getInstance();
    final current = await future;
    final servers = {...current.savedServers, normalized}.toList();
    if (!await prefs.setStringList(_kSavedServersKey, servers) ||
        !await prefs.setString(_kBaseUrlKey, normalized) ||
        !await prefs.setString(_kLibraryDestinationKey, 'server')) {
      throw StateError('Could not save server address');
    }
    state = AsyncData(AppSettings(
      serverBaseUrl: normalized,
      savedServers: List.unmodifiable(servers),
      destination: LibraryDestination.server,
    ));
  }

  Future<void> selectDestination(LibraryDestination destination) async {
    final prefs = await SharedPreferences.getInstance();
    if (!await prefs.setString(_kLibraryDestinationKey,
        destination == LibraryDestination.server ? 'server' : 'local')) {
      throw StateError('Could not save library selection');
    }
    final current = await future;
    state = AsyncData(AppSettings(
      serverBaseUrl: current.serverBaseUrl,
      destination: destination,
      configurationError: current.configurationError,
      savedServers: current.savedServers,
    ));
  }
}

final settingsProvider = AsyncNotifierProvider<SettingsNotifier, AppSettings>(
  SettingsNotifier.new,
);
