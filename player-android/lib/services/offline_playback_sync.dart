import 'dart:async';

import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:dio/dio.dart';

import 'offline_download_repository.dart';
import 'podcast_sync.dart' show syncBeforePlaybackTimeout;

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
      Connectivity? connectivity,
      bool automatic = true}) {
    repository.onSyncNeeded = trigger;
    repository.beforePlayback = syncKey;
    if (!automatic) return;
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
    final matching =
        items.where((item) => item['key'] == key && item['gone_ms'] == null);
    if (matching.isEmpty) return;
    await _syncItem(active, matching.single)
        .timeout(syncBeforePlaybackTimeout, onTimeout: () {})
        .catchError((_) {});
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
    } on DioException catch (error) {
      if (active.current() && [403, 404].contains(error.response?.statusCode)) {
        await repository.markSyncGone(
            key, () => !_disposed && active.current());
        return;
      }
      rethrow;
    }
    await repository.clearSyncGone(key, () => !_disposed && active.current());
  }

  Future<void> _sync() async {
    final active = account();
    if (active == null || !active.current()) return;
    final items = await repository.syncItems(active.origin, active.userId);
    for (final item in items) {
      if (_disposed || !active.current()) return;
      if (item['gone_ms'] == null) await _syncItem(active, item);
    }
  }

  /// Coordinator path propagates failures instead of mistaking a quiet
  /// background retry for a successful run. Gone copies count as completed.
  Future<void> syncAll(OfflineSyncAccount active,
      {bool explicit = false}) async {
    Object? failure;
    final items = await repository.syncItems(active.origin, active.userId);
    for (final item in items) {
      if (_disposed || !active.current()) throw StateError('Account changed');
      if (!explicit && item['gone_ms'] != null) continue;
      try {
        final key = item['key'] as String;
        Set<String> identities(Map<String, dynamic> payload) => {
              if (payload['progress'] != null)
                'p:${payload['progress']['observed_ms']}',
              for (final b in payload['bookmarks'] as List)
                'b:${b['sync_key']}:${b['observed_ms']}',
            };
        final initial = identities(await repository.syncPayload(key));
        var previous = initial.length + 1;
        do {
          await _syncItem(active, item);
          final currentItems =
              await repository.syncItems(active.origin, active.userId);
          if (currentItems
              .any((row) => row['key'] == key && row['gone_ms'] != null)) {
            break;
          }
          final remaining = initial
              .intersection(identities(await repository.syncPayload(key)));
          if (remaining.isEmpty) break;
          if (remaining.length >= previous) {
            throw StateError('Media edits remain pending');
          }
          previous = remaining.length;
        } while (active.current() && !_disposed);
      } catch (error) {
        failure ??= error;
      }
    }
    if (_disposed || !active.current()) throw StateError('Account changed');
    if (failure != null) throw failure;
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
