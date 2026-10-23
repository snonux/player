import 'package:shared_preferences/shared_preferences.dart';
import 'network_policy.dart';

/// Device preferences keyed by canonical server origin, independent of endpoints.
class StreamQuality {
  static SharedPreferences? _preferences;
  static Future<void> initialize() async {
    _preferences = await SharedPreferences.getInstance();
    await MusicNetworkPolicy.initialize();
  }

  static String _key(String server) => 'stream-quality:$server';
  static String selected(String server) => forNetwork(
      server, MusicNetworkPolicy.network == 'wifi' ? 'wifi' : 'mobile');
  static String forNetwork(String server, String network) =>
      _preferences?.getString('${_key(server)}:$network') ??
      _preferences?.getString(_key(server)) ??
      'auto';
  static Future<void> save(String server, String profile,
      {String? network}) async {
    if (_preferences == null) await initialize();
    if (!await _preferences!.setString(
        network == null ? _key(server) : '${_key(server)}:$network', profile)) {
      throw StateError('Could not save stream quality');
    }
    MusicNetworkPolicy.changes.value++;
  }
}
