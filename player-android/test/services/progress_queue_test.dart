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
import 'dart:io';

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
  final List<(int, String)> statusCalls = [];
  final List<String> operationOrder = [];
  int statusFailures = 0;
  final statusRejections = <int, int>{};

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
    operationOrder.add('batch');
  }

  @override
  Future<void> updateProgressStatus({
    required int mediaId,
    required String status,
  }) async {
    final rejected = statusRejections[mediaId];
    if (rejected != null) {
      final request = RequestOptions(path: '/api/v1/progress/status');
      throw DioException(
        requestOptions: request,
        response: Response(
          requestOptions: request,
          statusCode: rejected,
          data: {'error': rejected == 404 ? 'not found' : 'forbidden'},
        ),
        type: DioExceptionType.badResponse,
      );
    }
    if (statusFailures > 0) {
      statusFailures--;
      final request = RequestOptions(path: '/api/v1/progress/status');
      throw DioException(
        requestOptions: request,
        response: Response(requestOptions: request, statusCode: 500),
        type: DioExceptionType.badResponse,
      );
    }
    statusCalls.add((mediaId, status));
    operationOrder.add('status');
  }
}

class _DelayedProgressClient extends _FakeApiClient {
  final firstStarted = Completer<void>();
  final releaseFirst = Completer<void>();
  bool _first = true;

  @override
  Future<void> batchUpdateProgress(List<Map<String, dynamic>> updates) async {
    if (_first) {
      _first = false;
      firstStarted.complete();
      await releaseFirst.future;
      if (shouldThrowOnNextCall) {
        shouldThrowOnNextCall = false;
        throw Exception('old request failed');
      }
      return;
    }
    await super.batchUpdateProgress(updates);
  }
}

class _RejectingProgressClient extends _FakeApiClient {
  final rejected = <int, int>{};
  final attempts = <List<int>>[];
  DioException? overrideError;
  int transientFailures = 0;

  @override
  Future<void> batchUpdateProgress(List<Map<String, dynamic>> updates) async {
    final ids = updates.map((update) => update['media_id'] as int).toList();
    attempts.add(ids);
    final error = overrideError;
    if (error != null) throw error;
    for (var index = 0; index < ids.length; index++) {
      final status = rejected[ids[index]];
      if (status == null) continue;
      final request = RequestOptions(path: '/api/v1/progress/batch');
      throw DioException(
        requestOptions: request,
        response: Response(
          requestOptions: request,
          statusCode: status,
          data: {'error': 'updates[$index]: rejected media'},
        ),
        type: DioExceptionType.badResponse,
      );
    }
    if (transientFailures > 0) {
      transientFailures--;
      final request = RequestOptions(path: '/api/v1/progress/batch');
      throw DioException(
        requestOptions: request,
        response: Response(
          requestOptions: request,
          statusCode: 500,
          data: {'error': 'temporary failure'},
        ),
        type: DioExceptionType.badResponse,
      );
    }
    await super.batchUpdateProgress(updates);
  }
}

class _DelayedInsertDatabase implements Database {
  _DelayedInsertDatabase(this.delegate);

  final Database delegate;
  final insertStarted = Completer<void>();
  final releaseInsert = Completer<void>();
  bool _firstInsert = true;

  @override
  Future<int> insert(String table, Map<String, Object?> values,
      {String? nullColumnHack, ConflictAlgorithm? conflictAlgorithm}) async {
    if (_firstInsert) {
      _firstInsert = false;
      insertStarted.complete();
      await releaseInsert.future;
    }
    return delegate.insert(table, values,
        nullColumnHack: nullColumnHack, conflictAlgorithm: conflictAlgorithm);
  }

  @override
  Future<int> delete(String table, {String? where, List<Object?>? whereArgs}) =>
      delegate.delete(table, where: where, whereArgs: whereArgs);

  @override
  Future<int> rawDelete(String sql, [List<Object?>? arguments]) =>
      delegate.rawDelete(sql, arguments);

  @override
  Future<void> close() => delegate.close();

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

class _DelayedEmptyQueryDatabase implements Database {
  _DelayedEmptyQueryDatabase(this.delegate);

  final Database delegate;
  final queryStarted = Completer<void>();
  final releaseQuery = Completer<void>();
  bool _delayFirst = true;

  @override
  Future<List<Map<String, Object?>>> query(String table,
      {bool? distinct,
      List<String>? columns,
      String? where,
      List<Object?>? whereArgs,
      String? groupBy,
      String? having,
      String? orderBy,
      int? limit,
      int? offset}) async {
    final rows = await delegate.query(table,
        distinct: distinct,
        columns: columns,
        where: where,
        whereArgs: whereArgs,
        groupBy: groupBy,
        having: having,
        orderBy: orderBy,
        limit: limit,
        offset: offset);
    if (_delayFirst) {
      _delayFirst = false;
      queryStarted.complete();
      await releaseQuery.future;
    }
    return rows;
  }

  @override
  Future<int> insert(String table, Map<String, Object?> values,
          {String? nullColumnHack, ConflictAlgorithm? conflictAlgorithm}) =>
      delegate.insert(table, values,
          nullColumnHack: nullColumnHack, conflictAlgorithm: conflictAlgorithm);

  @override
  Future<int> delete(String table, {String? where, List<Object?>? whereArgs}) =>
      delegate.delete(table, where: where, whereArgs: whereArgs);

  @override
  Future<int> rawDelete(String sql, [List<Object?>? arguments]) =>
      delegate.rawDelete(sql, arguments);

  @override
  Future<void> close() => delegate.close();

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
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
  bool throwOnNextCheck = false;
  int failedChecks = 0;

  /// Pushes [results] to the stream and updates [checkConnectivity] state.
  void emitStatus(List<ConnectivityResult> results) {
    _current = results;
    _controller.add(results);
  }

  void setStatusSilently(List<ConnectivityResult> results) {
    _current = results;
  }

  @override
  Stream<List<ConnectivityResult>> get onConnectivityChanged =>
      _controller.stream;

  @override
  Future<List<ConnectivityResult>> checkConnectivity() async {
    if (throwOnNextCheck) {
      throwOnNextCheck = false;
      failedChecks++;
      throw StateError('connectivity plugin unavailable');
    }
    return _current;
  }

  void close() => _controller.close();

  @override
  dynamic noSuchMethod(Invocation i) => super.noSuchMethod(i);
}

// ---------------------------------------------------------------------------
// Fixture factory
// ---------------------------------------------------------------------------

const _scopeA = ProgressScope(origin: 'https://a.example', userId: 1);
const _scopeB = ProgressScope(origin: 'https://a.example', userId: 2);
const _scopeOtherServer = ProgressScope(origin: 'https://b.example', userId: 1);

/// Creates an in-memory [Database] with the production schema.
Future<Database> _openTestDb(String path) async {
  return databaseFactoryFfi.openDatabase(
    path,
    options: OpenDatabaseOptions(
      version: 2,
      onCreate: (db, _) => db.execute('''
        CREATE TABLE progress_queue (
          id               INTEGER PRIMARY KEY AUTOINCREMENT,
          media_id         INTEGER NOT NULL,
          position_seconds REAL    NOT NULL,
          finished         INTEGER NOT NULL DEFAULT 0,
          queued_at        TEXT    NOT NULL,
          origin           TEXT    NOT NULL,
          user_id          INTEGER NOT NULL
        )
      '''),
    ),
  );
}

Future<Database> _openInMemoryDb() => _openTestDb(inMemoryDatabasePath);

/// Builds a [ProgressQueue] with in-memory DB and fake connectivity.
Future<({ProgressQueue queue, _FakeApiClient client, _FakeConnectivity conn})>
    _makeQueue() async {
  final db = await _openInMemoryDb();
  final client = _FakeApiClient();
  final conn = _FakeConnectivity();
  final queue = ProgressQueue(apiClient: client, db: db, connectivity: conn);
  await queue.init(scope: _scopeA);
  return (queue: queue, client: client, conn: conn);
}

/// Drains the Dart event loop enough times for async stream listeners and DB
/// operations to complete.
///
/// A single [Future<void>.delayed(Duration.zero)] is not sufficient because
/// stream listeners schedule their work one microtask turn later; the DB calls
/// inside the listener add further async hops.  Twenty round-trips covers the
/// full async chain (stream delivery → listener body → DB query → DB delete).
Future<void> _pump() async {
  await Future<void>.delayed(const Duration(milliseconds: 20));
  for (var i = 0; i < 20; i++) {
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

  test('unauthenticated startup clears pending rows before subscribing',
      () async {
    final db = await _openInMemoryDb();
    await db.insert('progress_queue', {
      'media_id': 42,
      'position_seconds': 12.5,
      'finished': 0,
      'queued_at': DateTime.now().toUtc().toIso8601String(),
      'origin': _scopeA.origin,
      'user_id': _scopeA.userId,
    });
    final client = _FakeApiClient();
    final conn = _FakeConnectivity();
    conn.emitStatus([ConnectivityResult.wifi]);
    final queue = ProgressQueue(apiClient: client, db: db, connectivity: conn);

    await queue.init();
    await _pump();
    expect(client.calls, isEmpty);
    expect(await db.query('progress_queue'), isEmpty);
    await queue.init(scope: _scopeA);
    conn.emitStatus([ConnectivityResult.wifi]);
    await _pump();
    expect(client.calls, isEmpty);
    await queue.dispose();
    conn.close();
  });

  test('account and server switches flush only matching pending rows',
      () async {
    final db = await _openInMemoryDb();
    final client = _FakeApiClient();
    final conn = _FakeConnectivity();
    final queue = ProgressQueue(apiClient: client, db: db, connectivity: conn);
    await queue.init(scope: _scopeA);
    await queue.enqueue(42, 10);
    await queue.init(scope: _scopeB);
    await queue.enqueue(42, 20);
    await queue.init(scope: _scopeOtherServer);
    await queue.enqueue(42, 30);

    conn.emitStatus([ConnectivityResult.wifi]);
    await _pump();
    expect(client.calls, hasLength(1));
    expect(client.calls.last.single['position_seconds'], 30);
    expect(await db.query('progress_queue'), hasLength(2));

    await queue.init(scope: _scopeB);
    conn.emitStatus([ConnectivityResult.wifi]);
    await _pump();
    expect(client.calls.last.single['position_seconds'], 20);
    await queue.init(scope: _scopeA);
    conn.emitStatus([ConnectivityResult.wifi]);
    await _pump();
    expect(client.calls.last.single['position_seconds'], 10);
    expect(await db.query('progress_queue'), isEmpty);
    await queue.dispose();
    conn.close();
  });

  test('matching authenticated scope survives database reopen', () async {
    final dir = await Directory.systemTemp.createTemp('progress-queue-');
    final path = '${dir.path}/progress.db';
    try {
      final connA = _FakeConnectivity();
      final first = ProgressQueue(
        apiClient: _FakeApiClient(),
        databasePath: path,
        connectivity: connA,
      );
      await first.init(scope: _scopeA);
      await first.enqueue(42, 12);
      await first.dispose();
      connA.close();

      final client = _FakeApiClient();
      final connB = _FakeConnectivity();
      final second = ProgressQueue(
        apiClient: client,
        databasePath: path,
        connectivity: connB,
      );
      await second.init(scope: _scopeB);
      connB.emitStatus([ConnectivityResult.wifi]);
      await _pump();
      expect(client.calls, isEmpty);
      await second.init(scope: _scopeA);
      connB.emitStatus([ConnectivityResult.wifi]);
      await _pump();
      expect(client.calls.single.single['position_seconds'], 12);
      await second.dispose();
      connB.close();
    } finally {
      await dir.delete(recursive: true);
    }
  });

  test('v1 rows without ownership are dropped during migration', () async {
    final dir = await Directory.systemTemp.createTemp('progress-v1-');
    final path = '${dir.path}/progress.db';
    try {
      final oldDb = await databaseFactoryFfi.openDatabase(path,
          options: OpenDatabaseOptions(
            version: 1,
            onCreate: (db, _) => db.execute('''
              CREATE TABLE progress_queue (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                media_id INTEGER NOT NULL,
                position_seconds REAL NOT NULL,
                finished INTEGER NOT NULL DEFAULT 0,
                queued_at TEXT NOT NULL
              )
            '''),
          ));
      await oldDb.insert('progress_queue', {
        'media_id': 42,
        'position_seconds': 12.0,
        'queued_at': DateTime.now().toUtc().toIso8601String(),
      });
      await oldDb.close();
      final conn = _FakeConnectivity();
      final client = _FakeApiClient();
      final queue = ProgressQueue(
        apiClient: client,
        databasePath: path,
        connectivity: conn,
      );
      await queue.init(scope: _scopeA);
      conn.emitStatus([ConnectivityResult.wifi]);
      await _pump();
      expect(client.calls, isEmpty);
      await queue.enqueue(43, 4);
      await queue.dispose();
      conn.close();
    } finally {
      await dir.delete(recursive: true);
    }
  });

  test('logout discards old progress and suspends sync until next login',
      () async {
    final (:queue, :client, :conn) = await _makeQueue();
    await queue.enqueue(42, 12.5);
    await queue.clearAndSuspend();
    await queue.enqueue(43, 4.0);
    conn.emitStatus([ConnectivityResult.wifi]);
    await _pump();
    expect(client.calls, isEmpty);

    await queue.init(scope: _scopeA);
    conn.emitStatus([ConnectivityResult.wifi]);
    await _pump();
    expect(client.calls, isEmpty);
    await queue.enqueue(44, 8.0);
    expect(client.calls, hasLength(1));
    expect(client.calls.single.single['media_id'], 44);
    await queue.dispose();
  });

  test('in-flight enqueue is removed before a new account resumes', () async {
    final delegate = await _openInMemoryDb();
    final db = _DelayedInsertDatabase(delegate);
    final conn = _FakeConnectivity();
    final queue =
        ProgressQueue(apiClient: _FakeApiClient(), db: db, connectivity: conn);
    await queue.init(scope: _scopeA);
    final enqueueFuture = queue.enqueue(1, 1.0);
    await db.insertStarted.future;
    final clearFuture = queue.clearAndSuspend();
    db.releaseInsert.complete();
    await Future.wait([enqueueFuture, clearFuture]);
    expect(await delegate.query('progress_queue'), isEmpty);

    await queue.resume(_scopeB);
    await queue.enqueue(2, 2.0);
    expect((await delegate.query('progress_queue')).single['media_id'], 2);
    await queue.dispose();
    conn.close();
  });

  for (final oldRequestSucceeds in [true, false]) {
    test(
        'old in-flight flush cannot affect next account '
        '(${oldRequestSucceeds ? 'success' : 'failure'})', () async {
      final db = await _openInMemoryDb();
      final client = _DelayedProgressClient();
      final conn = _FakeConnectivity();
      final queue =
          ProgressQueue(apiClient: client, db: db, connectivity: conn);
      await queue.init(scope: _scopeA);
      await queue.enqueue(1, 1.0);
      conn.emitStatus([ConnectivityResult.wifi]);
      await client.firstStarted.future;

      await queue.clearAndSuspend();
      conn.emitStatus([ConnectivityResult.none]);
      await queue.init(scope: _scopeA);
      await queue.enqueue(2, 2.0);
      client.shouldThrowOnNextCall = !oldRequestSucceeds;
      client.releaseFirst.complete();
      await _pump();
      expect((await db.query('progress_queue')).single['media_id'], 2);

      conn.emitStatus([ConnectivityResult.wifi]);
      await _pump();
      expect(client.calls, hasLength(1));
      expect(client.calls.single.single['media_id'], 2);
      await queue.dispose();
      conn.close();
    });
  }

  test('new account flush retries after an old request releases the guard',
      () async {
    final db = await _openInMemoryDb();
    final client = _DelayedProgressClient();
    final conn = _FakeConnectivity();
    final queue = ProgressQueue(apiClient: client, db: db, connectivity: conn);
    await queue.init(scope: _scopeA);
    await queue.enqueue(1, 1.0);
    conn.emitStatus([ConnectivityResult.wifi]);
    await client.firstStarted.future;

    await queue.clearAndSuspend();
    await queue.resume(_scopeB);
    await queue.enqueue(2, 2.0);
    // This online enqueue asks to flush B while A's request still owns the
    // guard. No second connectivity event is sent after A completes.
    client.releaseFirst.complete();
    await _pump();
    expect(client.calls, hasLength(1));
    expect(client.calls.single.single['media_id'], 2);
    await queue.dispose();
    conn.close();
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

    test('stores multiple items while offline without calling the API',
        () async {
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

    test('payload contains media_id, position_seconds, and observed_at',
        () async {
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
    test('indexed 404 and 403 rows do not block valid or future updates',
        () async {
      final db = await _openInMemoryDb();
      final client = _RejectingProgressClient()
        ..rejected.addAll({2: 404, 4: 403});
      final conn = _FakeConnectivity();
      final queue =
          ProgressQueue(apiClient: client, db: db, connectivity: conn);
      await queue.init(scope: _scopeA);
      for (final id in [1, 2, 3, 4, 5]) {
        await queue.enqueue(id, id.toDouble());
      }

      conn.emitStatus([ConnectivityResult.wifi]);
      await _pump();
      expect(client.attempts, [
        [1, 2, 3, 4, 5],
        [1, 3, 4, 5],
        [1, 3, 5],
      ]);
      expect(client.calls.single.map((u) => u['media_id']).toList(), [1, 3, 5]);
      expect(await db.query('progress_queue'), isEmpty);

      await queue.enqueue(6, 6.0);
      expect(client.calls.last.single['media_id'], 6);
      await queue.dispose();
      conn.close();
    });

    test('generic 403 and server 500 retain all rows for retry', () async {
      for (final status in [403, 500]) {
        final db = await _openInMemoryDb();
        final client = _RejectingProgressClient();
        final conn = _FakeConnectivity();
        final queue =
            ProgressQueue(apiClient: client, db: db, connectivity: conn);
        await queue.init(scope: _scopeA);
        await queue.enqueue(1, 1.0);
        await queue.enqueue(2, 2.0);
        final request = RequestOptions(path: '/api/v1/progress/batch');
        client.overrideError = DioException(
          requestOptions: request,
          response: Response(
            requestOptions: request,
            statusCode: status,
            data: {'error': 'generic failure'},
          ),
          type: DioExceptionType.badResponse,
        );
        conn.emitStatus([ConnectivityResult.wifi]);
        await _pump();
        expect(await db.query('progress_queue'), hasLength(2));
        expect(client.calls, isEmpty);

        client.overrideError = null;
        conn.emitStatus([ConnectivityResult.wifi]);
        await _pump();
        expect(client.calls.single, hasLength(2));
        expect(await db.query('progress_queue'), isEmpty);
        await queue.dispose();
        conn.close();
      }
    });

    test('transient failure after isolating a bad row retains valid rows',
        () async {
      final db = await _openInMemoryDb();
      final client = _RejectingProgressClient()
        ..rejected[2] = 404
        ..transientFailures = 1;
      final conn = _FakeConnectivity();
      final queue =
          ProgressQueue(apiClient: client, db: db, connectivity: conn);
      await queue.init(scope: _scopeA);
      await queue.enqueue(1, 1.0);
      await queue.enqueue(2, 2.0);
      await queue.enqueue(3, 3.0);

      conn.emitStatus([ConnectivityResult.wifi]);
      await _pump();
      expect(
          (await db.query('progress_queue'))
              .map((row) => row['media_id'])
              .toList(),
          [1, 3]);
      expect(client.calls, isEmpty);

      conn.emitStatus([ConnectivityResult.wifi]);
      await _pump();
      expect(client.calls.single.map((u) => u['media_id']).toList(), [1, 3]);
      expect(await db.query('progress_queue'), isEmpty);
      await queue.dispose();
      conn.close();
    });

    test('rows survive a failed flush and are sent on the next attempt',
        () async {
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
    test('two rapid connectivity events result in at most one API call',
        () async {
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

  group('durable finished commands', () {
    test('old empty query cannot cancel new account completion retry',
        () async {
      final delegate = await _openInMemoryDb();
      final db = _DelayedEmptyQueryDatabase(delegate);
      final client = _FakeApiClient();
      final conn = _FakeConnectivity();
      final queue = ProgressQueue(
        apiClient: client,
        db: db,
        connectivity: conn,
        retryDelay: const Duration(milliseconds: 20),
      );
      await queue.init(scope: _scopeA);
      conn.emitStatus([ConnectivityResult.wifi]);
      await db.queryStarted.future;

      conn.emitStatus([ConnectivityResult.none]);
      await queue.init(scope: _scopeB);
      await _pump();
      conn.throwOnNextCheck = true;
      await queue.enqueueFinished(43);
      expect(conn.failedChecks, 1);
      conn.setStatusSilently([ConnectivityResult.wifi]);
      db.releaseQuery.complete(); // A's empty result arrives after B's retry.
      await _pump();
      await Future<void>.delayed(const Duration(milliseconds: 60));
      await _pump();

      expect(client.statusCalls, [(43, 'finished')]);
      expect(await delegate.query('progress_queue'), isEmpty);
      await queue.dispose();
      conn.close();
    });

    test('500 completion retries while connectivity remains online', () async {
      final db = await _openInMemoryDb();
      final client = _FakeApiClient()..statusFailures = 1;
      final conn = _FakeConnectivity();
      final queue = ProgressQueue(
        apiClient: client,
        db: db,
        connectivity: conn,
        retryDelay: const Duration(milliseconds: 10),
      );
      await queue.init(scope: _scopeA);
      conn.emitStatus([ConnectivityResult.wifi]);
      await queue.enqueueFinished(42);
      expect(await db.query('progress_queue'), hasLength(1));
      await Future<void>.delayed(const Duration(milliseconds: 40));
      await _pump();
      expect(client.statusCalls, [(42, 'finished')]);
      expect(await db.query('progress_queue'), isEmpty);
      await queue.dispose();
      conn.close();
    });

    test(
        'connectivity check failure after insert does not duplicate completion',
        () async {
      final db = await _openInMemoryDb();
      final client = _FakeApiClient();
      final conn = _FakeConnectivity();
      final queue = ProgressQueue(
        apiClient: client,
        db: db,
        connectivity: conn,
      );
      await queue.init(scope: _scopeA);
      await _pump();
      conn.throwOnNextCheck = true;
      await queue.enqueueFinished(42);
      expect(conn.failedChecks, 1);
      final rows = await db.query('progress_queue');
      expect(rows, hasLength(1));
      expect(rows.single['finished'], 1);
      conn.emitStatus([ConnectivityResult.wifi]);
      await _pump();
      expect(client.statusCalls, [(42, 'finished')]);
      expect(await db.query('progress_queue'), isEmpty);
      await queue.dispose();
      conn.close();
    });

    test('offline completion syncs after earlier positions, before later ones',
        () async {
      final db = await _openInMemoryDb();
      final client = _FakeApiClient();
      final conn = _FakeConnectivity();
      final queue =
          ProgressQueue(apiClient: client, db: db, connectivity: conn);
      await queue.init(scope: _scopeA);
      await queue.enqueue(42, 94);
      await queue.enqueueFinished(42);
      await queue.enqueue(43, 4);
      final rows = await db.query('progress_queue', orderBy: 'id ASC');
      expect(rows.map((row) => row['finished']).toList(), [0, 1, 0]);
      expect(client.calls, isEmpty);

      conn.emitStatus([ConnectivityResult.wifi]);
      await _pump();
      expect(client.operationOrder, ['batch', 'status', 'batch']);
      expect(client.calls[0].single['media_id'], 42);
      expect(client.calls[1].single['media_id'], 43);
      expect(client.statusCalls, [(42, 'finished')]);
      expect(await db.query('progress_queue'), isEmpty);
      await queue.dispose();
      conn.close();
    });

    test('500 on status keeps only completion for reconnect retry', () async {
      final db = await _openInMemoryDb();
      final client = _FakeApiClient()..statusFailures = 1;
      final conn = _FakeConnectivity();
      final queue =
          ProgressQueue(apiClient: client, db: db, connectivity: conn);
      await queue.init(scope: _scopeA);
      await queue.enqueue(42, 96);
      await queue.enqueueFinished(42);
      conn.emitStatus([ConnectivityResult.wifi]);
      await _pump();
      expect(client.calls, hasLength(1));
      expect(client.statusCalls, isEmpty);
      final remaining = await db.query('progress_queue');
      expect(remaining, hasLength(1));
      expect(remaining.single['finished'], 1);

      conn.emitStatus([ConnectivityResult.wifi]);
      await _pump();
      expect(client.calls, hasLength(1));
      expect(client.statusCalls, [(42, 'finished')]);
      expect(await db.query('progress_queue'), isEmpty);
      await queue.dispose();
      conn.close();
    });

    test('server-confirmed deleted completion does not block later media',
        () async {
      final db = await _openInMemoryDb();
      final client = _FakeApiClient()..statusRejections[42] = 404;
      final conn = _FakeConnectivity();
      final queue =
          ProgressQueue(apiClient: client, db: db, connectivity: conn);
      await queue.init(scope: _scopeA);
      await queue.enqueueFinished(42);
      await queue.enqueue(43, 8);
      conn.emitStatus([ConnectivityResult.wifi]);
      await _pump();
      expect(client.statusCalls, isEmpty);
      expect(client.calls.single.single['media_id'], 43);
      expect(await db.query('progress_queue'), isEmpty);
      await queue.dispose();
      conn.close();
    });

    test('completion survives database reopen without becoming a position',
        () async {
      final dir = await Directory.systemTemp.createTemp('progress-finished-');
      final path = '${dir.path}/progress.db';
      try {
        final firstConn = _FakeConnectivity();
        final first = ProgressQueue(
          apiClient: _FakeApiClient(),
          databasePath: path,
          connectivity: firstConn,
        );
        await first.init(scope: _scopeA);
        await first.enqueue(42, 95);
        await first.enqueueFinished(42);
        await first.dispose();
        firstConn.close();

        final client = _FakeApiClient();
        final conn = _FakeConnectivity();
        conn.emitStatus([ConnectivityResult.wifi]);
        final reopened = ProgressQueue(
          apiClient: client,
          databasePath: path,
          connectivity: conn,
        );
        await reopened.init(scope: _scopeA);
        await _pump();
        expect(client.operationOrder, ['batch', 'status']);
        expect(client.statusCalls, [(42, 'finished')]);
        await reopened.dispose();
        conn.close();
      } finally {
        await dir.delete(recursive: true);
      }
    });

    test('completion remains scoped to its server and account', () async {
      final db = await _openInMemoryDb();
      final client = _FakeApiClient();
      final conn = _FakeConnectivity();
      final queue =
          ProgressQueue(apiClient: client, db: db, connectivity: conn);
      await queue.init(scope: _scopeA);
      await queue.enqueueFinished(42);
      await queue.init(scope: _scopeB);
      await queue.enqueueFinished(43);
      await queue.init(scope: _scopeOtherServer);
      await queue.enqueueFinished(44);

      conn.emitStatus([ConnectivityResult.wifi]);
      await _pump();
      expect(client.statusCalls, [(44, 'finished')]);
      await queue.init(scope: _scopeB);
      conn.emitStatus([ConnectivityResult.wifi]);
      await _pump();
      expect(client.statusCalls.last, (43, 'finished'));
      await queue.init(scope: _scopeA);
      conn.emitStatus([ConnectivityResult.wifi]);
      await _pump();
      expect(client.statusCalls.last, (42, 'finished'));
      expect(await db.query('progress_queue'), isEmpty);
      await queue.dispose();
      conn.close();
    });

    test('completion is not acknowledged when the queue is suspended',
        () async {
      final fixture = await _makeQueue();
      final queue = fixture.queue;
      final conn = fixture.conn;
      await queue.clearAndSuspend();
      await expectLater(queue.enqueueFinished(42), throwsStateError);
      await queue.dispose();
      conn.close();
    });
  });
}
