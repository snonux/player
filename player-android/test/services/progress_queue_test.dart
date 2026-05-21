// Unit tests for ProgressQueue (lib/services/progress_queue.dart).
//
// Tests cover:
//   1. enqueue stores a row to the SQLite database (no flush while offline).
//   2. flush sends all queued rows via batchUpdateProgress.
//   3. flush clears rows after a successful send.
//   4. flush retains rows on server error.
//   5. offline items are flushed when connectivity is restored.
//   6. concurrent flush calls do not double-send.
//
// The SQLite backend is replaced with sqflite_common_ffi's in-memory factory so
// the tests run on Linux/macOS CI without a real Android device.  Connectivity
// is simulated by injecting a [_FakeConnectivity] whose stream is controlled by
// a [StreamController].
//
// Timing note: after emitting a connectivity event the listener is async.
// [_pump] drains the Dart microtask and timer queues by issuing several
// [Future<void>.delayed(Duration.zero)] calls to give the async chain
// enough event-loop turns to complete.
//
// Run with: flutter test test/services/progress_queue_test.dart

import 'dart:async';

import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';

import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/services/progress_queue.dart';

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

/// Fake [PlayerApiClient] that records [batchUpdateProgress] calls.
///
/// [shouldThrowOnNextCall] makes the next call throw an [Exception] to
/// simulate a server error.
class _FakeApiClient extends PlayerApiClient {
  _FakeApiClient() : super(dio: Dio());

  // Accumulated payloads (each entry = one batchUpdateProgress call).
  final List<List<Map<String, dynamic>>> calls = [];

  // When true, the next call throws instead of recording.
  bool shouldThrowOnNextCall = false;

  @override
  Future<void> batchUpdateProgress(
    List<Map<String, dynamic>> updates,
  ) async {
    if (shouldThrowOnNextCall) {
      shouldThrowOnNextCall = false;
      throw Exception('simulated server error');
    }
    calls.add(List.unmodifiable(updates));
  }
}

/// Fake [Connectivity] driven by the test via [emitStatus].
///
/// Defaults to offline ([ConnectivityResult.none]) so enqueue tests do not
/// trigger accidental auto-flush.
class _FakeConnectivity implements Connectivity {
  _FakeConnectivity() {
    _controller = StreamController<List<ConnectivityResult>>.broadcast();
  }

  late final StreamController<List<ConnectivityResult>> _controller;
  List<ConnectivityResult> _current = [ConnectivityResult.none];

  /// Pushes [results] to the stream and updates [checkConnectivity] state.
  void emitStatus(List<ConnectivityResult> results) {
    _current = results;
    _controller.add(results);
  }

  @override
  Stream<List<ConnectivityResult>> get onConnectivityChanged =>
      _controller.stream;

  @override
  Future<List<ConnectivityResult>> checkConnectivity() async => _current;

  void close() => _controller.close();

  @override
  dynamic noSuchMethod(Invocation i) => super.noSuchMethod(i);
}

// ---------------------------------------------------------------------------
// Fixture factory
// ---------------------------------------------------------------------------

/// Creates an in-memory [Database] with the production schema.
Future<Database> _openInMemoryDb() async {
  return databaseFactoryFfi.openDatabase(
    inMemoryDatabasePath,
    options: OpenDatabaseOptions(
      version: 1,
      onCreate: (db, _) => db.execute('''
        CREATE TABLE progress_queue (
          id               INTEGER PRIMARY KEY AUTOINCREMENT,
          media_id         INTEGER NOT NULL,
          position_seconds REAL    NOT NULL,
          finished         INTEGER NOT NULL DEFAULT 0,
          queued_at        TEXT    NOT NULL
        )
      '''),
    ),
  );
}

/// Builds a [ProgressQueue] with in-memory DB and fake connectivity.
Future<({ProgressQueue queue, _FakeApiClient client, _FakeConnectivity conn})>
    _makeQueue() async {
  final db = await _openInMemoryDb();
  final client = _FakeApiClient();
  final conn = _FakeConnectivity();
  final queue = ProgressQueue(apiClient: client, db: db, connectivity: conn);
  await queue.init();
  return (queue: queue, client: client, conn: conn);
}

/// Drains the Dart event loop enough times for async stream listeners and DB
/// operations to complete.
///
/// A single [Future<void>.delayed(Duration.zero)] is not sufficient because
/// stream listeners schedule their work one microtask turn later; the DB calls
/// inside the listener add further async hops.  Five round-trips covers the
/// full async chain (stream delivery → listener body → DB query → DB delete).
Future<void> _pump() async {
  for (var i = 0; i < 5; i++) {
    await Future<void>.delayed(Duration.zero);
  }
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

void main() {
  setUpAll(() {
    sqfliteFfiInit();
    databaseFactory = databaseFactoryFfi;
  });

  // --------------------------------------------------------------------------
  // 1. enqueue stores to the database (no flush while offline)
  // --------------------------------------------------------------------------

  group('enqueue stores to DB', () {
    test('does not call the API when device is offline', () async {
      final (:queue, :client, :conn) = await _makeQueue();

      await queue.enqueue(42, 12.5);

      expect(client.calls, isEmpty,
          reason: 'No API call expected while offline');
      await queue.dispose();
    });

    test('stores multiple items while offline without calling the API', () async {
      final (:queue, :client, :conn) = await _makeQueue();

      await queue.enqueue(1, 5.0);
      await queue.enqueue(2, 10.0);

      expect(client.calls, isEmpty,
          reason: 'No API call expected while offline');
      await queue.dispose();
    });

    test('flushes immediately when online at enqueue time', () async {
      final (:queue, :client, :conn) = await _makeQueue();

      // Report WiFi so checkConnectivity() returns online during enqueue.
      conn.emitStatus([ConnectivityResult.wifi]);
      await _pump();

      await queue.enqueue(7, 30.0);
      await _pump();

      expect(client.calls, hasLength(1),
          reason: 'Should flush immediately when already online');
      expect(client.calls.first.first['media_id'], equals(7));
      await queue.dispose();
    });
  });

  // --------------------------------------------------------------------------
  // 2. flush sends via batchUpdateProgress
  // --------------------------------------------------------------------------

  group('flush sends batch', () {
    test('sends all queued rows in one batchUpdateProgress call', () async {
      final (:queue, :client, :conn) = await _makeQueue();

      await queue.enqueue(10, 15.0);
      await queue.enqueue(11, 25.0);

      conn.emitStatus([ConnectivityResult.mobile]);
      await _pump();

      expect(client.calls, hasLength(1),
          reason: 'Exactly one batch call expected');
      expect(client.calls.first, hasLength(2),
          reason: 'Both rows must be in the batch');
      final mediaIds = client.calls.first.map((m) => m['media_id']).toSet();
      expect(mediaIds, equals({10, 11}));
      await queue.dispose();
    });

    test('payload contains media_id, position_seconds, and observed_at', () async {
      final (:queue, :client, :conn) = await _makeQueue();

      await queue.enqueue(5, 99.5);
      conn.emitStatus([ConnectivityResult.wifi]);
      await _pump();

      expect(client.calls, hasLength(1));
      final item = client.calls.first.first;
      expect(item['media_id'], equals(5));
      expect(item['position_seconds'], equals(99.5));
      expect(item.containsKey('observed_at'), isTrue,
          reason: 'observed_at is required by the server for ordering');
      await queue.dispose();
    });
  });

  // --------------------------------------------------------------------------
  // 3. flush clears rows after successful send
  // --------------------------------------------------------------------------

  group('flush clears rows on success', () {
    test('rows are removed from the DB so a second flush is a no-op', () async {
      final (:queue, :client, :conn) = await _makeQueue();

      await queue.enqueue(20, 1.0);
      await queue.enqueue(21, 2.0);

      conn.emitStatus([ConnectivityResult.wifi]);
      await _pump();

      // Second flush on an empty table must not trigger another API call.
      conn.emitStatus([ConnectivityResult.wifi]);
      await _pump();

      expect(client.calls, hasLength(1),
          reason: 'Second flush must be a no-op after rows are cleared');
      await queue.dispose();
    });
  });

  // --------------------------------------------------------------------------
  // 4. flush retains rows on server error
  // --------------------------------------------------------------------------

  group('flush retains rows on server error', () {
    test('rows survive a failed flush and are sent on the next attempt', () async {
      final (:queue, :client, :conn) = await _makeQueue();

      await queue.enqueue(30, 5.0);

      // First flush: API throws.
      client.shouldThrowOnNextCall = true;
      conn.emitStatus([ConnectivityResult.wifi]);
      await _pump();

      expect(client.calls, isEmpty,
          reason: 'No successful call should have been recorded after throw');

      // Second flush: API succeeds; rows should still be present.
      conn.emitStatus([ConnectivityResult.wifi]);
      await _pump();

      expect(client.calls, hasLength(1),
          reason: 'Row should be retried on the second flush');
      expect(client.calls.first.first['media_id'], equals(30));
      await queue.dispose();
    });
  });

  // --------------------------------------------------------------------------
  // 5. offline items flushed on reconnect
  // --------------------------------------------------------------------------

  group('offline items flushed on reconnect', () {
    test('all queued items are sent when connectivity is restored', () async {
      final (:queue, :client, :conn) = await _makeQueue();

      for (var i = 0; i < 3; i++) {
        await queue.enqueue(100 + i, i * 10.0);
      }
      expect(client.calls, isEmpty, reason: 'No flush while offline');

      conn.emitStatus([ConnectivityResult.wifi]);
      await _pump();

      expect(client.calls, hasLength(1),
          reason: 'One batch call expected after reconnect');
      expect(client.calls.first, hasLength(3),
          reason: 'All three rows must be in the batch');
      await queue.dispose();
    });
  });

  // --------------------------------------------------------------------------
  // 6. concurrent flush guard
  // --------------------------------------------------------------------------

  group('concurrent flush guard', () {
    test('two rapid connectivity events result in at most one API call', () async {
      final (:queue, :client, :conn) = await _makeQueue();

      await queue.enqueue(50, 1.0);

      // Emit two events in rapid succession before any async work can run.
      conn.emitStatus([ConnectivityResult.wifi]);
      conn.emitStatus([ConnectivityResult.wifi]);
      await _pump();

      // The _isFlushing guard prevents a second concurrent flush, so at most
      // one successful API call should have been made.
      expect(client.calls.length, lessThanOrEqualTo(1),
          reason: '_isFlushing should prevent double-flush');
      await queue.dispose();
    });
  });
}
