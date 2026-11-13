import 'dart:async';
import 'dart:io';
import 'package:dio/dio.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// Names one signed-in account on one server. Saved queues key their
/// durable state by it, so the format must stay stable.
String accountSyncScope(String origin, int userId) =>
    'account:${Uri.parse(origin).origin}#$userId';

/// How long playback waits for a pending sync before starting anyway.
const syncBeforePlaybackTimeout = Duration(seconds: 3);

/// A sync step that chose not to run now (policy, conflict, cancel).
class SyncDeferred implements Exception {
  const SyncDeferred(this.kind);
  final String kind;
}

/// Groups a sync failure into the kinds the sync status shows.
String syncErrorKind(Object error) {
  if (error is SyncDeferred) return error.kind;
  if (error is DioException) {
    if (error.response?.statusCode == 401) return 'auth';
    if ([404, 405, 501].contains(error.response?.statusCode)) {
      return 'unsupported';
    }
    if (error.response != null) return 'server';
    return 'offline';
  }
  if (error is TimeoutException ||
      error is SocketException ||
      error is HandshakeException) {
    return 'offline';
  }
  return 'server';
}

/// The last sync outcome of an account, kept across app restarts.
class GlobalSyncOutcome {
  const GlobalSyncOutcome({this.lastFullSuccessMs, this.errorKind});
  final int? lastFullSuccessMs;
  final String? errorKind;
}

/// Durable per-account sync status.
class GlobalSyncStatusStore {
  String _key(String scope) => '$scope.global-sync';

  Future<GlobalSyncOutcome> read(String scope) async {
    final prefs = await SharedPreferences.getInstance();
    final success = prefs.getInt('${_key(scope)}.success-ms');
    final error = prefs.getString('${_key(scope)}.error');
    return GlobalSyncOutcome(lastFullSuccessMs: success, errorKind: error);
  }

  Future<void> record(String scope,
      {required int attemptMs, String? error}) async {
    final prefs = await SharedPreferences.getInstance();
    if (error == null) {
      await prefs.setInt('${_key(scope)}.success-ms', attemptMs);
      await prefs.remove('${_key(scope)}.error');
    } else {
      await prefs.setString('${_key(scope)}.error', error);
    }
  }
}
