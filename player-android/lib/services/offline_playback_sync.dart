import 'dart:async';

import 'package:connectivity_plus/connectivity_plus.dart';

import 'offline_download_repository.dart';

/// Credentials stay outside the durable store. A captured account generation
/// guards both request dispatch and applying a response after an account switch.
class OfflineSyncAccount {
  OfflineSyncAccount(
      {required this.origin,
      required this.userId,
      required this.current,
      required this.send});
  final String origin;
  final int userId;
  final bool Function() current;
  final Future<Map<String, dynamic>> Function(int, Map<String, dynamic>) send;
}

class OfflinePlaybackSync {
  OfflinePlaybackSync(this.repository, this.account,
      {Duration interval = const Duration(seconds: 15),
      Connectivity? connectivity}) {
    repository.onSyncNeeded = trigger;
    repository.beforePlayback = syncKey;
    _timer = Timer.periodic(interval, (_) => trigger());
    _connectivity = (connectivity ?? Connectivity())
        .onConnectivityChanged
        .listen((_) => trigger());
    trigger();
  }
  final OfflineDownloadRepository repository;
  final OfflineSyncAccount? Function() account;
  Timer? _timer;
  StreamSubscription<List<ConnectivityResult>>? _connectivity;
  Future<void>? _running;
  bool _disposed = false;
  bool _again = false;

  void trigger() {
    if (_disposed) return;
    if (_running != null) {
      _again = true;
      return;
    }
    unawaited(sync());
  }

  Future<void> sync() {
    if (_disposed) return Future.value();
    if (_running != null) return _running!;
    _running = _sync().catchError((Object _) {
      /* Retain edits for next retry. */
    }).whenComplete(() {
      _running = null;
      if (_again && !_disposed) {
        _again = false;
        trigger();
      }
    });
    return _running!;
  }

  Future<void> syncKey(String key) async {
    // A foreground open gets its own request even if an unrelated background
    // request is slow. Local playback still works while offline or locked.
    final active = account();
    if (active == null || !active.current() || _disposed) return;
    final items = await repository.syncItems(active.origin, active.userId);
    final matching = items.where((item) => item['key'] == key);
    if (matching.isEmpty) return;
    await _syncItem(active, matching.single)
        .timeout(const Duration(seconds: 5), onTimeout: () {});
  }

  Future<void> _syncItem(
      OfflineSyncAccount active, Map<String, Object?> item) async {
    if (_disposed || !active.current()) return;
    final key = item['key'] as String;
    final payload = await repository.syncPayload(key);
    if (_disposed || !active.current()) return;
    try {
      final result = await active.send(item['media_id'] as int, payload);
      if (_disposed || !active.current()) return;
      await repository.mergeSync(
          key, result, () => !_disposed && active.current());
    } catch (_) {/* Durable rows remain available for retry. */}
  }

  Future<void> _sync() async {
    final active = account();
    if (active == null || !active.current()) return;
    final items = await repository.syncItems(active.origin, active.userId);
    for (final item in items) {
      if (_disposed || !active.current()) return;
      await _syncItem(active, item);
    }
  }

  Future<void> dispose() async {
    _disposed = true;
    _timer?.cancel();
    await _connectivity?.cancel();
    repository.onSyncNeeded = null;
    repository.beforePlayback = null;
    await _running;
  }
}
