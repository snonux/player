import 'package:shared_preferences/shared_preferences.dart';

/// Device preferences keyed by canonical server origin, independent of endpoints.
class StreamQuality {
  static SharedPreferences? _preferences;
  static Future<void> initialize() async {
    _preferences = await SharedPreferences.getInstance();
  }

  static String _key(String server) => 'stream-quality:$server';
  static String selected(String server) =>
      _preferences?.getString(_key(server)) ?? 'auto';
  static Future<void> save(String server, String profile) async {
    if (_preferences == null) await initialize();
    if (!await _preferences!.setString(_key(server), profile)) {
      throw StateError('Could not save stream quality');
    }
  }
}
