import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// Device-wide preferences shared by each app-owned native audio player.
class HeadsetPreferences extends ChangeNotifier {
  bool pauseOnDisconnect = true, resumeOnReconnect = false;
  bool loading = true, saving = false;
  String? error;
  bool _disposed = false;

  Future<void> restore() async {
    try {
      final prefs = await SharedPreferences.getInstance();
      final record = prefs.getString('headset.policy');
      if (record != null) {
        final values = record.split(',');
        if (values.length != 2 ||
            values.any((value) => value != 'true' && value != 'false')) {
          throw const FormatException('Invalid headphone settings');
        }
        pauseOnDisconnect = values[0] == 'true';
        resumeOnReconnect = values[1] == 'true';
      }
    } catch (_) {
      error =
          'Headphone settings could not be loaded. Safe defaults are active.';
    }
    loading = false;
    if (!_disposed) notifyListeners();
  }

  Future<void> save({bool? pauseOnDisconnect, bool? resumeOnReconnect}) async {
    if (loading || saving) return;
    saving = true;
    error = null;
    notifyListeners();
    final pause = pauseOnDisconnect ?? this.pauseOnDisconnect;
    final resume = resumeOnReconnect ?? this.resumeOnReconnect;
    try {
      final prefs = await SharedPreferences.getInstance();
      // One persisted record prevents a partially saved pair of preferences.
      if (!await prefs.setString('headset.policy', '$pause,$resume')) {
        throw StateError('Preference write rejected');
      }
      this.pauseOnDisconnect = pause;
      this.resumeOnReconnect = resume;
    } catch (_) {
      error = 'Headphone settings could not be saved. Try again.';
    } finally {
      saving = false;
      if (!_disposed) notifyListeners();
    }
  }

  @override
  void dispose() {
    _disposed = true;
    super.dispose();
  }
}
