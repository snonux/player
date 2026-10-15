import 'dart:async';
import 'dart:io';

import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';
import 'package:player_android/services/progress_queue.dart';

class _Connectivity implements Connectivity {
  final events = StreamController<List<ConnectivityResult>>.broadcast();
  @override
  Future<List<ConnectivityResult>> checkConnectivity() async =>
      [ConnectivityResult.none];
  @override
  Stream<List<ConnectivityResult>> get onConnectivityChanged => events.stream;
}

class _Client implements ProgressSyncClient, TimestampedProgressSyncClient {
  final attempts = <String>[];
  final first = Completer<void>(), second = Completer<void>();
  final positions = <Map<String, dynamic>>[];
  @override
  Future<void> batchUpdateProgress(List<Map<String, dynamic>> updates) async {
    positions.addAll(updates);
  }

  @override
  Future<void> updateProgressStatus(
          {required int mediaId, required String status}) =>
      throw StateError('Timestamp was omitted');
  @override
  Future<void> updateProgressStatusAt(
      {required int mediaId,
      required String status,
      required String observedAt}) async {
    expect(mediaId, 7);
    expect(status, 'finished');
    attempts.add(observedAt);
    if (attempts.length == 1) {
      first.complete();
      throw const SocketException('offline');
    }
    second.complete();
  }
}

void main() {
  setUpAll(() {
    sqfliteFfiInit();
    databaseFactory = databaseFactoryFfi;
  });
  test(
      'production timestamp capability preserves completion edit time across delayed retries',
      () async {
    final dir = await Directory.systemTemp.createTemp('completion-time-');
    final connectivity = _Connectivity(), client = _Client();
    final queue = ProgressQueue(
        apiClient: client,
        connectivity: connectivity,
        databasePath: '${dir.path}/queue.db',
        retryDelay: const Duration(days: 1));
    try {
      await queue.init(
          scope: const ProgressScope(origin: 'https://one.test', userId: 1));
      final before = DateTime.fromMillisecondsSinceEpoch(
          DateTime.now().millisecondsSinceEpoch,
          isUtc: true);
      await queue.enqueueFinished(7);
      final after = DateTime.now().toUtc();
      connectivity.events.add([ConnectivityResult.wifi]);
      await client.first.future.timeout(const Duration(seconds: 2));
      final observed = DateTime.parse(client.attempts.single);
      expect(observed.isBefore(before), isFalse);
      expect(observed.isAfter(after), isFalse);
      await Future<void>.delayed(const Duration(milliseconds: 10));
      connectivity.events.add([ConnectivityResult.wifi]);
      await client.second.future.timeout(const Duration(seconds: 2));
      expect(client.attempts,
          [observed.toIso8601String(), observed.toIso8601String()]);
    } finally {
      await queue.dispose();
      await connectivity.events.close();
      await dir.delete(recursive: true);
    }
  });
  test(
      'same-millisecond position, completion and replay stay ordered across reopen',
      () async {
    final dir = await Directory.systemTemp.createTemp('completion-order-');
    final path = '${dir.path}/queue.db';
    final fixed = DateTime.utc(2026, 10, 15, 12, 0, 0, 123);
    final client = _Client(), conn = _Connectivity();
    var queue = ProgressQueue(
        apiClient: client,
        connectivity: conn,
        databasePath: path,
        now: () => fixed,
        retryDelay: const Duration(days: 1));
    try {
      await queue.init(
          scope: const ProgressScope(origin: 'https://one.test', userId: 1));
      await queue.enqueue(7, 42);
      await queue.enqueueFinished(7);
      await queue.dispose();
      queue = ProgressQueue(
          apiClient: client,
          connectivity: conn,
          databasePath: path,
          now: () => fixed,
          retryDelay: const Duration(days: 1));
      await queue.init(
          scope: const ProgressScope(origin: 'https://one.test', userId: 1));
      await queue.enqueue(7, 5);
      conn.events.add([ConnectivityResult.wifi]);
      await client.first.future.timeout(const Duration(seconds: 2));
      await Future<void>.delayed(const Duration(milliseconds: 10));
      conn.events.add([ConnectivityResult.wifi]);
      await client.second.future.timeout(const Duration(seconds: 2));
      // Queue dispatch drains its later replay after the completion response.
      while (client.positions.length < 2) {
        await Future<void>.delayed(Duration.zero);
      }
      expect(
          DateTime.parse(client.positions.first['observed_at'] as String)
              .millisecondsSinceEpoch,
          fixed.millisecondsSinceEpoch);
      expect(DateTime.parse(client.attempts.first).millisecondsSinceEpoch,
          fixed.millisecondsSinceEpoch + 1);
      expect(
          DateTime.parse(client.positions.last['observed_at'] as String)
              .millisecondsSinceEpoch,
          fixed.millisecondsSinceEpoch + 2);
    } finally {
      await queue.dispose();
      await conn.events.close();
      await dir.delete(recursive: true);
    }
  });
}
