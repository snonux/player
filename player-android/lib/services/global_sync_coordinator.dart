import 'dart:async';
import 'dart:math';
import 'package:flutter/foundation.dart';
import 'podcast_sync.dart';

class GlobalSyncAccount {
  GlobalSyncAccount(
      {required this.podcast,
      required this.syncMedia,
      required this.flushProgress,
      required this.pendingOther});
  final PodcastSyncAccount podcast;
  final Future<void> Function(bool explicit) syncMedia;
  final Future<void> Function() flushProgress;
  final Future<int> Function() pendingOther;
}

class GlobalSyncState {
  const GlobalSyncState(
      {this.scope,
      this.active = false,
      this.pending = 0,
      this.lastFullSuccessMs,
      this.errorKind});
  final String? scope;
  final bool active;
  final int pending;
  final int? lastFullSuccessMs;
  final String? errorKind;
  bool get configured => scope != null && scope != 'local';
  bool get neverSynced => configured && lastFullSuccessMs == null;
  bool staleAt(int nowMs) =>
      configured &&
      lastFullSuccessMs != null &&
      nowMs - lastFullSuccessMs! >= const Duration(hours: 48).inMilliseconds;
}

/// One quiet retry loop for supported domains; no UI, credentials or player.
class GlobalSyncCoordinator extends ChangeNotifier {
  GlobalSyncCoordinator(this.podcasts, this.account, {DateTime Function()? now})
      : _now = now ?? DateTime.now;
  final PodcastSync podcasts;
  final GlobalSyncAccount? Function() account;
  final DateTime Function() _now;
  GlobalSyncState state = const GlobalSyncState();
  Timer? _timer;
  Future<void>? _running;
  bool _disposed = false;
  bool _again = false;
  bool _explicitAgain = false;
  String? _authBlockedScope;
  int _failures = 0;

  void trigger(
      {bool explicit = false, bool reset = true, bool signedIn = false}) {
    if (_disposed) return;
    if (signedIn) _authBlockedScope = null;
    final active = account();
    if (active == null) {
      _timer?.cancel();
      state = const GlobalSyncState();
      notifyListeners();
      return;
    }
    if (_authBlockedScope == active.podcast.scope && !signedIn) return;
    if (reset) {
      _failures = 0;
      _timer?.cancel();
    }
    if (_running != null) {
      _again = true;
      _explicitAgain = _explicitAgain || explicit;
      return;
    }
    unawaited(syncNow(explicit: explicit));
  }

  Future<void> syncNow({bool explicit = true}) {
    if (_disposed || _authBlockedScope == account()?.podcast.scope) {
      return Future.value();
    }
    if (_running != null) {
      _again = true;
      _explicitAgain = _explicitAgain || explicit;
      return _running!;
    }
    _running = _run(explicit).catchError((Object error) {
      if (_disposed) return;
      final active = account();
      if (active == null || !active.podcast.current()) return;
      state = GlobalSyncState(
          scope: active.podcast.scope,
          pending: state.pending,
          lastFullSuccessMs: state.lastFullSuccessMs,
          errorKind: syncErrorKind(error));
      notifyListeners();
      _timer?.cancel();
      _timer = Timer(const Duration(seconds: 15), () => trigger(reset: false));
    }).whenComplete(() {
      _running = null;
      if (_again && !_disposed) {
        _again = false;
        final explicit = _explicitAgain;
        _explicitAgain = false;
        trigger(explicit: explicit);
      }
    });
    return _running!;
  }

  Future<void> _run(bool explicit) async {
    final active = account();
    if (active == null) return;
    final lease = active.podcast;
    bool current() => !_disposed && lease.current();
    if (!current()) return;
    final store = podcasts.store;
    final db = await store.database;
    final meta = await store.status(lease.scope);
    final pending =
        (await store.pending(lease.scope)).length + await active.pendingOther();
    if (!current()) return;
    state = GlobalSyncState(
        scope: lease.scope,
        active: true,
        pending: pending,
        lastFullSuccessMs: meta['last_full_success_ms'] as int?,
        errorKind: meta['last_error_kind'] as String?);
    notifyListeners();
    String? error;
    final result = await podcasts.sync(lease);
    if (!result.full) error = result.errorKind ?? 'server';
    for (final step in [
      () => active.syncMedia(explicit),
      active.flushProgress
    ]) {
      if (!current()) return;
      try {
        await step();
      } catch (failure) {
        final kind = syncErrorKind(failure);
        if (error == null || kind == 'auth') error = kind;
      }
    }
    if (!current()) return;
    final now = _now().millisecondsSinceEpoch;
    final remaining =
        (await store.pending(lease.scope)).length + await active.pendingOther();
    if (!current()) return;
    await db.transaction((tx) async {
      if (!current()) throw StateError('Account changed');
      await tx.update(
          'podcast_scopes',
          {
            'last_attempt_ms': now,
            'last_error_kind': error,
            if (error == null) 'last_full_success_ms': now
          },
          where: 'scope=?',
          whereArgs: [lease.scope]);
      if (!current()) throw StateError('Account changed');
    });
    if (!current()) return;
    state = GlobalSyncState(
        scope: lease.scope,
        pending: remaining,
        lastFullSuccessMs:
            error == null ? now : meta['last_full_success_ms'] as int?,
        errorKind: error);
    notifyListeners();
    if (error == 'auth') {
      _authBlockedScope = lease.scope;
      return;
    }
    _failures = error == null ? 0 : _failures + 1;
    final seconds =
        error == null ? 60 : min(900, 15 * (1 << min(6, _failures - 1)));
    _timer?.cancel();
    _timer = Timer(Duration(seconds: seconds), () => trigger(reset: false));
  }

  @override
  void dispose() {
    _disposed = true;
    _timer?.cancel();
    super.dispose();
  }
}
