import 'dart:async';

import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:sqflite/sqflite.dart';

// SQLite table and column names — kept as constants to avoid typos and make
// schema migrations easy to spot.
const _kTable = 'progress_queue';
const _kColId = 'id';
const _kColMediaId = 'media_id';
const _kColPositionSeconds = 'position_seconds';
const _kColFinished = 'finished';
const _kColQueuedAt = 'queued_at';

// Database schema version. Bump when columns change so onUpgrade fires.
const _kDbVersion = 1;

// Database filename stored in the default sqflite databases path.
const _kDbName = 'progress_queue.db';

// ---------------------------------------------------------------------------
// ProgressSyncClient — narrow interface (ISP)
// ---------------------------------------------------------------------------

/// Narrow interface for the single API operation that [ProgressQueue] needs.
///
/// Interface Segregation: [ProgressQueue] depends only on
/// [batchUpdateProgress], not on the full [PlayerApiClient] surface.
/// Production code passes a [PlayerApiClient] (which implements this);
/// tests can provide a lightweight stub without subclassing the entire client.
abstract class ProgressSyncClient {
  /// Submits a batch of progress updates to the server.
  ///
  /// Each map must include `media_id`, `position_seconds`, and `observed_at`.
  Future<void> batchUpdateProgress(List<Map<String, dynamic>> updates);
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
  /// Opens the backing store and subscribes to connectivity changes.
  ///
  /// Must be called once before [enqueue].
  Future<void> init();

  /// Persists a playback-progress update and, if online, flushes immediately.
  Future<void> enqueue(int mediaId, double positionSeconds,
      {bool finished = false});

  /// Cancels subscriptions and closes the backing store.
  Future<void> dispose();
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

  /// Converts this update to the JSON shape expected by
  /// [ProgressSyncClient.batchUpdateProgress].
  Map<String, dynamic> toBatchMap() => {
        'media_id': mediaId,
        'position_seconds': positionSeconds,
        'observed_at': queuedAt,
      };
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
  ///
  /// [db] is an already-opened [Database]; when non-null it takes precedence
  /// over [databaseFactory] and no additional open call is made.
  ///
  /// [connectivity] is optional; when null the default [Connectivity()] is
  /// used in production.  Pass a fake in tests.
  ProgressQueue({
    required ProgressSyncClient apiClient,
    Future<Database> Function()? databaseFactory,
    Database? db,
    Connectivity? connectivity,
  })  : _apiClient = apiClient,
        _databaseFactory = databaseFactory,
        _db = db,
        _connectivity = connectivity ?? Connectivity();

  final ProgressSyncClient _apiClient;

  // Optional factory for opening the on-disk database; null means use the
  // built-in [_openDatabase] helper which calls sqflite's openDatabase().
  final Future<Database> Function()? _databaseFactory;
  final Connectivity _connectivity;

  // Non-null after [init] has been called.
  Database? _db;

  // Guards against concurrent flush operations.
  bool _isFlushing = false;

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
  Future<void> init() async {
    _db ??= await (_databaseFactory?.call() ?? _openDatabase());
    _subscribeToConnectivity();
  }

  /// Cancels the connectivity subscription and closes the database.
  ///
  /// Awaits any in-flight flush before closing the DB so that a concurrent
  /// flush does not attempt to use the database after it has been closed
  /// (prevents "database_closed" errors during app shutdown or test teardown).
  @override
  Future<void> dispose() async {
    await _connectivitySub?.cancel();
    _connectivitySub = null;
    // Wait for any ongoing flush to finish before closing the database.
    // Ignore errors from the in-flight flush — they are already handled inside
    // [_flush] via try/finally; swallowing here avoids double-reporting.
    await _flushFuture?.catchError((_) {});
    await _db?.close();
    _db = null;
  }

  // ---------------------------------------------------------------------------
  // Public API
  // ---------------------------------------------------------------------------

  /// Persists a progress update locally and, if the device is currently online,
  /// triggers an immediate flush.
  ///
  /// Fire-and-forget in the player screens: any DB write failure is swallowed
  /// so a storage error never interrupts playback.
  @override
  Future<void> enqueue(
    int mediaId,
    double positionSeconds, {
    bool finished = false,
  }) async {
    final db = _db;
    if (db == null) return; // Defensive: init not called.

    final now = DateTime.now().toUtc().toIso8601String();
    await db.insert(_kTable, {
      _kColMediaId: mediaId,
      _kColPositionSeconds: positionSeconds,
      _kColFinished: finished ? 1 : 0,
      _kColQueuedAt: now,
    });

    // Opportunistic online flush: attempt immediately on enqueue so that
    // updates sent while online bypass the DB round-trip latency.
    // Errors are swallowed — the row is already persisted so the next
    // connectivity event will retry.
    final results = await _connectivity.checkConnectivity();
    if (_isOnline(results)) {
      await _flush().catchError((_) {});
    }
  }

  // ---------------------------------------------------------------------------
  // Internal: flush
  // ---------------------------------------------------------------------------

  /// Sends all queued rows to the server via [batchUpdateProgress].
  ///
  /// Rows that are successfully sent are deleted from the DB.  Rows that fail
  /// (e.g., the server returns an error for a specific item) are retained for
  /// the next flush.  The entire batch succeeds or fails atomically from the
  /// client perspective — if the call throws, no rows are deleted.
  ///
  /// [_isFlushing] prevents re-entrant flushes.  The flag is cleared in a
  /// `finally` block so a thrown exception never permanently blocks flushing.
  ///
  /// The future is stored in [_flushFuture] so [dispose] can await it before
  /// closing the database, preventing use-after-close crashes on shutdown.
  Future<void> _flush() {
    if (_isFlushing) return Future.value();
    _isFlushing = true;
    _flushFuture = _flushPendingRows().whenComplete(() {
      _isFlushing = false;
      _flushFuture = null;
    });
    return _flushFuture!;
  }

  /// Loads pending rows, sends them, and removes the ones that succeeded.
  ///
  /// Extracted from [_flush] to keep each method under ~30 lines and make the
  /// "load → send → delete" pipeline independently readable.
  Future<void> _flushPendingRows() async {
    final db = _db;
    if (db == null) return;

    final rows = await db.query(
      _kTable,
      orderBy: '$_kColQueuedAt ASC',
    );
    if (rows.isEmpty) return;

    final updates = rows.map(_rowToUpdate).toList();

    // Build the batch payload for the server.
    final payload = updates.map((u) => u.toBatchMap()).toList();

    // Send — if this throws (network error, server 5xx) we skip deletion and
    // let the next connectivity event retry.
    await _apiClient.batchUpdateProgress(payload);

    // Delete the rows that were just sent successfully.
    final ids = updates.map((u) => u.rowId!).toList();
    await _deleteRows(db, ids);
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
      _kDbName,
      version: _kDbVersion,
      onCreate: (db, version) => _createSchema(db),
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
        $_kColQueuedAt       TEXT    NOT NULL
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
