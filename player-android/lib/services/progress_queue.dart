import 'dart:async';

import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:dio/dio.dart';
import 'package:sqflite/sqflite.dart';

// SQLite table and column names — kept as constants to avoid typos and make
// schema migrations easy to spot.
const _kTable = 'progress_queue';
const _kColId = 'id';
const _kColMediaId = 'media_id';
const _kColPositionSeconds = 'position_seconds';
const _kColFinished = 'finished';
const _kColQueuedAt = 'queued_at';
const _kColOrigin = 'origin';
const _kColUserId = 'user_id';

// Database schema version. Bump when columns change so onUpgrade fires.
const _kDbVersion = 2;

// Database filename stored in the default sqflite databases path.
const _kDbName = 'progress_queue.db';

// ---------------------------------------------------------------------------
// ProgressSyncClient — narrow interface (ISP)
// ---------------------------------------------------------------------------

/// Narrow interface for the progress API operations that [ProgressQueue] needs.
///
/// Interface Segregation: [ProgressQueue] depends only on
/// [batchUpdateProgress] and [updateProgressStatus], not on the full client.
/// Production code passes a [PlayerApiClient] (which implements this);
/// tests can provide a lightweight stub without subclassing the entire client.
abstract class ProgressSyncClient {
  /// Submits a batch of progress updates to the server.
  ///
  /// Each map must include `media_id`, `position_seconds`, and `observed_at`.
  Future<void> batchUpdateProgress(List<Map<String, dynamic>> updates);

  /// Marks an item finished after all earlier queued positions have synced.
  Future<void> updateProgressStatus({
    required int mediaId,
    required String status,
  });
}

// ---------------------------------------------------------------------------
// ProgressQueueBase — abstract lifecycle interface (LSP + DIP)
// ---------------------------------------------------------------------------

/// Abstract contract for an offline-capable progress queue.
///
/// Callers (provider, player screens) depend on this interface rather than the
/// concrete [ProgressQueue] class (Dependency Inversion).  Alternative
/// implementations (in-memory, no-op) are substitutable without breaking
/// callers (Liskov Substitution).
abstract class ProgressQueueBase {
  /// Opens the backing store for [scope] and subscribes to connectivity.
  ///
  /// A null scope clears pending rows and suspends sync. Must be called before
  /// [enqueue].
  Future<void> init({ProgressScope? scope});

  /// Reactivates a queue already opened at app startup for [scope].
  Future<void> resume(ProgressScope scope);

  /// Persists a playback position and, if online, flushes immediately.
  Future<void> enqueue(int mediaId, double positionSeconds);

  /// Durably queues a separate finished-status command for this media item.
  /// Throws if the command could not be stored under the active account.
  Future<void> enqueueFinished(int mediaId);

  /// Cancels subscriptions and closes the backing store.
  Future<void> dispose();

  /// Discards pending progress and stops automatic sync until [resume].
  Future<void> clearAndSuspend();
}

/// Server and account that own locally recorded progress.
class ProgressScope {
  const ProgressScope({required this.origin, required this.userId});

  final String origin;
  final int userId;

  @override
  bool operator ==(Object other) =>
      other is ProgressScope &&
      origin == other.origin &&
      userId == other.userId;

  @override
  int get hashCode => Object.hash(origin, userId);
}

// ---------------------------------------------------------------------------
// ProgressUpdate value object
// ---------------------------------------------------------------------------

/// Immutable record of a single playback-progress update.
///
/// Used both as a value object passed from the player screens and as an
/// internal DTO deserialised from the SQLite row.  Keeping it in this file
/// avoids leaking a "models" dependency on the queue's persistence layer.
class ProgressUpdate {
  const ProgressUpdate({
    required this.mediaId,
    required this.positionSeconds,
    this.finished = false,
    required this.queuedAt,
    this.rowId,
  });

  final int mediaId;
  final double positionSeconds;
  final bool finished;

  /// Wall-clock time the update was created (ISO-8601 UTC string stored in DB).
  /// Used as the `observed_at` field in the batch request so the server applies
  /// updates in chronological order.
  final String queuedAt;

  /// Non-null after the row has been persisted; null for newly constructed
  /// updates that have not been written to the DB yet.
  final int? rowId;

  /// Converts a position update to the batch shape. Finished rows use the
  /// separate status endpoint and must never be serialized as positions.
  Map<String, dynamic> toBatchMap() {
    if (finished) throw StateError('Finished command is not a batch position');
    return {
      'media_id': mediaId,
      'position_seconds': positionSeconds,
      'observed_at': queuedAt,
    };
  }
}

// ---------------------------------------------------------------------------
// ProgressQueue
// ---------------------------------------------------------------------------

/// Offline-capable progress queue backed by SQLite.
///
/// Responsibilities (Single Responsibility: one per bullet):
///   - Persist [enqueue] calls to a local SQLite table so updates survive
///     process restarts while the device is offline.
///   - Watch network connectivity via [Connectivity] and trigger a flush
///     automatically when the device goes from offline to online.
///   - Flush pending rows by calling [ProgressSyncClient.batchUpdateProgress];
///     remove successfully sent rows and retain any that fail (for retry).
///
/// Design notes:
///   - No Flutter imports — this is a pure-Dart service (can be unit tested
///     without a widget tree).
///   - [ProgressSyncClient] is injected (Interface Segregation + Dependency
///     Inversion); [ProgressQueue] only depends on the one method it uses.
///   - [databaseFactory] is injected so tests can supply an in-memory opener
///     without touching the filesystem (Dependency Inversion).
///   - [Database] may also be injected directly via [db] for tests that have
///     already opened a connection.
///   - Rows from another scope remain on disk but are never submitted under
///     the active credentials. Logout explicitly discards all pending rows.
///   - Concurrent flush is prevented with [_isFlushing]; a second connectivity
///     event while a flush is in progress is silently ignored — the flush will
///     drain all rows anyway.
class ProgressQueue implements ProgressQueueBase {
  /// Creates the queue.
  ///
  /// [apiClient] must implement [ProgressSyncClient]; in production this is
  /// a [PlayerApiClient].  Tests can pass a lightweight stub.
  ///
  /// [databaseFactory] is an optional factory for opening the SQLite database.
  /// When null, [init] calls [openDatabase] with the default on-disk path.
  /// Inject a custom factory in tests to get an in-memory database without
  /// touching the filesystem (Dependency Inversion).
  /// [databasePath] overrides that path for persistent database tests.
  ///
  /// [db] is an already-opened [Database]; when non-null it takes precedence
  /// over [databaseFactory] and no additional open call is made.
  ///
  /// [connectivity] is optional; when null the default [Connectivity()] is
  /// used in production.  Pass a fake in tests.
  ProgressQueue({
    required ProgressSyncClient apiClient,
    Future<Database> Function()? databaseFactory,
    String? databasePath,
    Database? db,
    Connectivity? connectivity,
    Duration retryDelay = const Duration(seconds: 5),
  })  : _apiClient = apiClient,
        _databaseFactory = databaseFactory,
        _databasePath = databasePath,
        _db = db,
        _connectivity = connectivity ?? Connectivity(),
        _retryDelay = retryDelay;

  final ProgressSyncClient _apiClient;

  // Optional factory for opening the on-disk database; null means use the
  // built-in [_openDatabase] helper which calls sqflite's openDatabase().
  final Future<Database> Function()? _databaseFactory;
  final String? _databasePath;
  final Connectivity _connectivity;
  final Duration _retryDelay;

  // Non-null after [init] has been called.
  Database? _db;

  // Guards against concurrent flush operations.
  bool _isFlushing = false;
  int? _retryEpoch;
  Timer? _retryTimer;
  int _retryAttempts = 0;
  bool _disposed = false;
  bool _suspended = false;
  ProgressScope? _scope;
  int _accountEpoch = 0;
  Future<void> _dbMutationTail = Future.value();

  Future<T> _runDbMutation<T>(Future<T> Function() action) async {
    final previous = _dbMutationTail;
    final release = Completer<void>();
    _dbMutationTail = release.future;
    await previous;
    try {
      return await action();
    } finally {
      release.complete();
    }
  }

  // Holds the in-flight flush future so [dispose] can await it before closing
  // the database, preventing "database_closed" errors on shutdown.
  Future<void>? _flushFuture;

  // Subscription to connectivity changes; cancelled in [dispose].
  StreamSubscription<List<ConnectivityResult>>? _connectivitySub;

  // ---------------------------------------------------------------------------
  // Lifecycle
  // ---------------------------------------------------------------------------

  /// Opens the SQLite database (if not already provided) and subscribes to
  /// connectivity changes.
  ///
  /// Must be called once before any other method.  Safe to call multiple times
  /// (subsequent calls are no-ops if the DB is already open).
  ///
  /// The database is obtained from the injected [_databaseFactory] when
  /// supplied, falling back to [_openDatabase] which calls sqflite's
  /// [openDatabase] with the default on-disk path.
  @override
  Future<void> init({ProgressScope? scope}) async {
    if (scope != null && (scope.origin.isEmpty || scope.userId <= 0)) {
      throw ArgumentError('Progress scope requires an origin and user ID');
    }
    final changed = scope != _scope;
    if (changed) {
      _accountEpoch++;
      _clearRetry();
    }
    _scope = scope;
    _suspended = scope == null;
    _db ??= await (_databaseFactory?.call() ?? _openDatabase());
    if (_suspended) {
      await _connectivitySub?.cancel();
      _connectivitySub = null;
      await _runDbMutation(() => _db!.delete(_kTable));
    } else {
      _subscribeToConnectivity();
      unawaited(_flushIfOnline());
    }
  }

  @override
  Future<void> resume(ProgressScope scope) async {
    if (_db != null) await init(scope: scope);
  }

  @override
  Future<void> clearAndSuspend() async {
    _suspended = true;
    _scope = null;
    _accountEpoch++;
    _retryEpoch = null;
    _clearRetry();
    await _connectivitySub?.cancel();
    _connectivitySub = null;
    await _runDbMutation(() async {
      await _db?.delete(_kTable);
    });
  }

  /// Cancels the connectivity subscription and closes the database.
  ///
  /// Awaits any in-flight flush before closing the DB so that a concurrent
  /// flush does not attempt to use the database after it has been closed
  /// (prevents "database_closed" errors during app shutdown or test teardown).
  @override
  Future<void> dispose() async {
    _disposed = true;
    _clearRetry();
    await _connectivitySub?.cancel();
    _connectivitySub = null;
    // Wait for any ongoing flush to finish before closing the database.
    // Ignore errors from the in-flight flush — they are already handled inside
    // [_flush] via try/finally; swallowing here avoids double-reporting.
    await _flushFuture?.catchError((_) {});
    await _dbMutationTail;
    await _db?.close();
    _db = null;
  }

  // ---------------------------------------------------------------------------
  // Public API
  // ---------------------------------------------------------------------------

  /// Persists a position locally and, if online, triggers an immediate flush.
  ///
  /// Fire-and-forget in the player screens: any DB write failure is swallowed
  /// so a storage error never interrupts playback.
  @override
  Future<void> enqueue(int mediaId, double positionSeconds) =>
      _enqueue(mediaId, positionSeconds, finished: false);

  @override
  Future<void> enqueueFinished(int mediaId) =>
      _enqueue(mediaId, 0, finished: true);

  Future<void> _enqueue(int mediaId, double positionSeconds,
      {required bool finished}) async {
    if (_suspended) {
      if (finished) throw StateError('Progress queue is suspended');
      return;
    }
    final db = _db;
    if (db == null) {
      if (finished) throw StateError('Progress queue is not initialized');
      return;
    }

    final now = DateTime.now().toUtc().toIso8601String();
    final epoch = _accountEpoch;
    final scope = _scope;
    if (scope == null) {
      if (finished) throw StateError('Progress queue has no account');
      return;
    }
    var stored = false;
    await _runDbMutation(() async {
      if (_suspended || epoch != _accountEpoch || scope != _scope) return;
      await db.insert(_kTable, {
        _kColMediaId: mediaId,
        _kColPositionSeconds: positionSeconds,
        _kColFinished: finished ? 1 : 0,
        _kColQueuedAt: now,
        _kColOrigin: scope.origin,
        _kColUserId: scope.userId,
      });
      stored = true;
    });
    if (!stored || _suspended || epoch != _accountEpoch || scope != _scope) {
      if (finished) throw StateError('Progress account changed before save');
      return;
    }

    // Opportunistic online flush: attempt immediately on enqueue so that
    // updates sent while online bypass the DB round-trip latency.
    // Once inserted, success means durable storage. Connectivity and HTTP
    // failures must never make the caller enqueue a duplicate completion.
    await _flushIfOnline();
  }

  Future<void> _flushIfOnline() async {
    if (_disposed || _suspended) return;
    try {
      if (_isOnline(await _connectivity.checkConnectivity())) {
        await _flush();
      }
    } catch (_) {
      // A persisted row remains available for the next retry/connectivity event.
      _scheduleRetry();
    }
  }

  void _clearRetry() {
    _retryTimer?.cancel();
    _retryTimer = null;
    _retryAttempts = 0;
  }

  void _scheduleRetry() {
    if (_disposed || _suspended || _retryTimer != null) return;
    final epoch = _accountEpoch;
    final multiplier = 1 << _retryAttempts.clamp(0, 6);
    final delay = _retryDelay * multiplier;
    _retryAttempts++;
    _retryTimer = Timer(delay, () {
      _retryTimer = null;
      if (!_disposed && !_suspended && epoch == _accountEpoch) {
        unawaited(_flushIfOnline());
      }
    });
  }

  // ---------------------------------------------------------------------------
  // Internal: flush
  // ---------------------------------------------------------------------------

  /// Sends all queued rows to the server via [batchUpdateProgress].
  ///
  /// Successfully sent rows are deleted. An indexed 403/404 drops only the
  /// rejected row and retries the remaining atomic batch. Network, server,
  /// and unclassified errors leave their rows queued for a later flush.
  ///
  /// [_isFlushing] prevents re-entrant flushes.  The flag is cleared in a
  /// `finally` block so a thrown exception never permanently blocks flushing.
  ///
  /// The future is stored in [_flushFuture] so [dispose] can await it before
  /// closing the database, preventing use-after-close crashes on shutdown.
  Future<void> _flush() {
    if (_disposed || _suspended) return Future.value();
    if (_isFlushing) {
      _retryEpoch = _accountEpoch;
      return Future.value();
    }
    _isFlushing = true;
    _flushFuture = _flushPendingRows().whenComplete(() {
      _isFlushing = false;
      _flushFuture = null;
      if (!_suspended && _retryEpoch == _accountEpoch) {
        _retryEpoch = null;
        unawaited(_flush().catchError((_) {}));
      }
    });
    return _flushFuture!;
  }

  /// Loads pending rows, sends them, and removes the ones that succeeded or
  /// were permanently rejected for a specific media item.
  ///
  /// Extracted from [_flush] to keep each method under ~30 lines and make the
  /// "load → send → delete" pipeline independently readable.
  Future<void> _flushPendingRows() async {
    final epoch = _accountEpoch;
    final scope = _scope;
    final db = _db;
    if (db == null || scope == null) return;
    bool current() =>
        !_disposed && !_suspended && epoch == _accountEpoch && scope == _scope;

    final rows = await db.query(
      _kTable,
      where: '$_kColOrigin = ? AND $_kColUserId = ?',
      whereArgs: [scope.origin, scope.userId],
      orderBy: '$_kColQueuedAt ASC, $_kColId ASC',
    );
    if (rows.isEmpty) {
      if (current()) _clearRetry();
      return;
    }
    if (!current()) return;

    final pending = rows.map(_rowToUpdate).toList();
    while (pending.isNotEmpty && current()) {
      if (pending.first.finished) {
        final command = pending.first;
        try {
          await _apiClient.updateProgressStatus(
              mediaId: command.mediaId, status: 'finished');
        } catch (error) {
          if (!current()) return;
          if (!_permanentlyRejectedStatus(error)) {
            _scheduleRetry();
            return; // Transient status failure retains ordering for retry.
          }
          // The server confirmed this item no longer exists or is forbidden.
          // Drop only that command so later valid media can still sync.
        }
        if (!current()) return;
        await _runDbMutation(() async {
          if (current()) await _deleteRows(db, [command.rowId!]);
        });
        pending.removeAt(0);
        continue;
      }

      final positions = pending.takeWhile((row) => !row.finished).toList();
      try {
        await _apiClient.batchUpdateProgress(
            positions.map((row) => row.toBatchMap()).toList());
      } catch (error) {
        if (!current()) return;
        final index = _permanentlyRejectedIndex(error, positions.length);
        if (index == null) {
          _scheduleRetry();
          return; // Network/5xx/unknown errors stay queued.
        }
        final rejected = positions[index];
        pending.remove(rejected);
        await _runDbMutation(() async {
          if (current()) await _deleteRows(db, [rejected.rowId!]);
        });
        continue;
      }
      if (!current()) return;
      await _runDbMutation(() async {
        if (current()) {
          await _deleteRows(db, positions.map((row) => row.rowId!).toList());
        }
      });
      pending.removeRange(0, positions.length);
    }
    if (current() && pending.isEmpty) _clearRetry();
  }

  /// Accept only item-indexed access failures from the progress batch API.
  /// A generic 403/404 (proxy, route or account issue) is not evidence that a
  /// queued media row is permanently invalid and must remain retryable.
  int? _permanentlyRejectedIndex(Object error, int count) {
    if (error is! DioException) return null;
    final response = error.response;
    if (response?.statusCode != 403 && response?.statusCode != 404) {
      return null;
    }
    final data = response?.data;
    if (data is! Map || data['error'] is! String) return null;
    final match =
        RegExp(r'^updates\[(\d+)\]:').firstMatch(data['error'] as String);
    if (match == null) return null;
    final index = int.tryParse(match.group(1)!);
    return index != null && index >= 0 && index < count ? index : null;
  }

  bool _permanentlyRejectedStatus(Object error) {
    if (error is! DioException) return false;
    final response = error.response;
    final data = response?.data;
    if (data is! Map) return false;
    return (response?.statusCode == 404 && data['error'] == 'not found') ||
        (response?.statusCode == 403 && data['error'] == 'forbidden');
  }

  // ---------------------------------------------------------------------------
  // Internal: connectivity
  // ---------------------------------------------------------------------------

  /// Subscribes to connectivity changes and flushes when online is detected.
  ///
  /// The subscription is only set up once; subsequent [init] calls are no-ops
  /// because [_connectivitySub] is already non-null.
  ///
  /// Errors from [_flush] are swallowed inside the listener — the flush
  /// already handles its own error recovery (rows retained on failure) and
  /// an unhandled stream error would tear down the subscription.
  void _subscribeToConnectivity() {
    _connectivitySub ??= _connectivity.onConnectivityChanged.listen(
      (results) async {
        if (_isOnline(results)) {
          await _flush().catchError((_) {});
        }
      },
    );
  }

  // ---------------------------------------------------------------------------
  // Internal: helpers
  // ---------------------------------------------------------------------------

  /// Opens (or creates) the on-disk SQLite database and runs migrations.
  Future<Database> _openDatabase() {
    return openDatabase(
      _databasePath ?? _kDbName,
      version: _kDbVersion,
      onCreate: (db, version) => _createSchema(db),
      onUpgrade: (db, oldVersion, newVersion) async {
        // v1 rows have no owner; assigning them to the currently signed-in
        // account could disclose another account's playback history.
        if (oldVersion < 2) {
          await db.execute('DROP TABLE $_kTable');
          await _createSchema(db);
        }
      },
    );
  }

  /// Creates the progress_queue table on first run.
  Future<void> _createSchema(Database db) {
    return db.execute('''
      CREATE TABLE $_kTable (
        $_kColId             INTEGER PRIMARY KEY AUTOINCREMENT,
        $_kColMediaId        INTEGER NOT NULL,
        $_kColPositionSeconds REAL    NOT NULL,
        $_kColFinished       INTEGER NOT NULL DEFAULT 0,
        $_kColQueuedAt       TEXT    NOT NULL,
        $_kColOrigin         TEXT    NOT NULL,
        $_kColUserId         INTEGER NOT NULL
      )
    ''');
  }

  /// Converts a raw SQLite row map into a [ProgressUpdate].
  ProgressUpdate _rowToUpdate(Map<String, dynamic> row) {
    return ProgressUpdate(
      rowId: row[_kColId] as int,
      mediaId: row[_kColMediaId] as int,
      positionSeconds: (row[_kColPositionSeconds] as num).toDouble(),
      finished: (row[_kColFinished] as int) != 0,
      queuedAt: row[_kColQueuedAt] as String,
    );
  }

  /// Deletes rows with the given [ids] from the queue table.
  ///
  /// Uses a single DELETE … WHERE id IN (…) statement for efficiency.
  Future<void> _deleteRows(Database db, List<int> ids) async {
    if (ids.isEmpty) return;
    final placeholders = List.filled(ids.length, '?').join(', ');
    await db.rawDelete(
      'DELETE FROM $_kTable WHERE $_kColId IN ($placeholders)',
      ids,
    );
  }

  /// Returns `true` when at least one connectivity result indicates an active
  /// network interface (WiFi, mobile, ethernet, or VPN).
  ///
  /// [ConnectivityResult.none] is the only value treated as offline.
  bool _isOnline(List<ConnectivityResult> results) {
    return results.any((r) => r != ConnectivityResult.none);
  }
}
