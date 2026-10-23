import 'dart:async';
import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

class MusicNetworkRestricted implements Exception {
  const MusicNetworkRestricted();
  static const message =
      'Library audio streaming waits for Wi-Fi. Change Music network settings to allow mobile streaming, then retry.';
}

/// Device policy for music transfers. Unknown bearers use Mobile settings.
class MusicNetworkPolicy {
  static SharedPreferences? _preferences;
  static Future<void>? _opening;
  static String detected = 'unknown';
  static final changes = ValueNotifier<int>(0);
  static Future<void> initialize() => _opening ??= _open();
  static Future<void> _open() async {
    _preferences = await SharedPreferences.getInstance();
    try {
      _update(await Connectivity().checkConnectivity());
      Connectivity().onConnectivityChanged.listen(_update);
    } catch (_) {/* Manual selection remains available. */}
  }

  static void _update(List<ConnectivityResult> values) {
    // Mobile takes precedence when simultaneous bearers are reported.
    detected = values.contains(ConnectivityResult.mobile)
        ? 'mobile'
        : values.contains(ConnectivityResult.wifi) ||
                values.contains(ConnectivityResult.ethernet)
            ? 'wifi'
            : 'unknown';
    changes.value++;
  }

  static String get mode {
    final value = _preferences?.getString('music-network-mode');
    return value == 'wifi' || value == 'mobile' ? value! : 'auto';
  }

  static String get network => mode == 'auto' ? detected : mode;
  static bool flag(String name, {bool fallback = false}) =>
      _preferences?.getBool('music-network-$name') ?? fallback;
  static bool get streamBlocked => flag('stream-wifi') && network != 'wifi';
  static bool get syncAllowed => !flag('sync-wifi') || network == 'wifi';
  static bool get prefetchAllowed =>
      !streamBlocked &&
      (network == 'wifi'
          ? flag('wifi-prefetch', fallback: true)
          : flag('mobile-prefetch'));
  static Future<void> save(String name, Object value) async {
    await initialize();
    final success = value is bool
        ? await _preferences!.setBool('music-network-$name', value)
        : await _preferences!.setString('music-network-$name', value as String);
    if (!success) throw StateError('Could not save music network settings');
    changes.value++;
  }

  static void assertStreamingAllowed() {
    if (streamBlocked) throw const MusicNetworkRestricted();
  }
}
