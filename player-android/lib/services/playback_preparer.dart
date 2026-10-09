import 'dart:async';

import 'package:dio/dio.dart' show CancelToken;
import 'package:flutter/foundation.dart';

/// Answer of one readiness probe against a playback URL.
///
/// Native players cannot interpret the compatibility stream's `503` body, so
/// the app asks first and only hands the URL to a player once it is ready.
@immutable
class PlaybackProbe {
  const PlaybackProbe({required this.statusCode, this.retryAfter});

  final int statusCode;

  /// The server's `Retry-After` hint; null when it sent none.
  final Duration? retryAfter;

  /// The probe is a HEAD request, answered 200 once the rendition exists.
  bool get isReady => statusCode >= 200 && statusCode < 300;

  /// 503 means "transcoding" or "busy": the same GET will succeed later.
  bool get isPreparing => statusCode == 503;
}

/// Probes [uri] once. Implemented by the API client so the request carries
/// the same credentials as every other call to that server.
typedef ProbePlayback = Future<PlaybackProbe> Function(
  Uri uri, {
  CancelToken? cancelToken,
});

/// Why a stream could not be prepared. Each value has its own user message.
enum PlaybackPreparationFailure {
  /// 500: the server could not convert the file (may persist for minutes).
  conversionFailed,

  /// 507: the server has no room for the converted copy.
  outOfStorage,

  /// The server kept answering 503 for longer than the preparer waits.
  timedOut,

  /// No usable answer: connection failure, request timeout, or a 401 that
  /// the API client's interceptor turned into an exception (and a sign-out).
  unreachable,

  /// Any other status (400, 403, 404, 410, 415, ...).
  rejected,
}

class PlaybackPreparationException implements Exception {
  const PlaybackPreparationException(this.failure, {this.statusCode});

  final PlaybackPreparationFailure failure;
  final int? statusCode;

  @override
  String toString() => 'PlaybackPreparationException(${failure.name}'
      '${statusCode == null ? '' : ', HTTP $statusCode'})';
}

/// Waits for a server compatibility stream to become playable.
///
/// The policy (how long to wait in total, how long between probes) lives here
/// so both player screens share it and tests can shorten it.
class PlaybackPreparer {
  const PlaybackPreparer({
    this.maxWait = const Duration(minutes: 10),
    this.defaultRetryInterval = const Duration(seconds: 5),
    this.maxRetryInterval = const Duration(seconds: 30),
    this.now = DateTime.now,
  });

  /// Total time to keep retrying. A first transcode of a long video takes
  /// minutes, but a queue that never drains must not spin forever.
  final Duration maxWait;

  /// Used when the server sends no `Retry-After`.
  final Duration defaultRetryInterval;

  /// Upper bound for a single pause, whatever the server asks for.
  final Duration maxRetryInterval;

  final DateTime Function() now;

  /// Starts probing [uri] with [probe] and returns a handle to await or cancel.
  ///
  /// [stillWanted] is asked before every probe; once it answers false the
  /// wait ends as if cancelled. It covers owners that are replaced without
  /// being told (another item claimed the player).
  PlaybackPreparation prepare(
    Uri uri, {
    required ProbePlayback probe,
    bool Function()? stillWanted,
  }) =>
      PlaybackPreparation._(this, uri, probe, stillWanted);
}

/// One running wait. Cancel it when the user leaves or switches item so no
/// request or timer outlives the screen.
class PlaybackPreparation {
  PlaybackPreparation._(
    this._policy,
    this._uri,
    this._probe,
    this._stillWanted,
  ) {
    ready = _run();
  }

  final PlaybackPreparer _policy;
  final Uri _uri;
  final ProbePlayback _probe;
  final bool Function()? _stillWanted;
  final _cancelToken = CancelToken();
  Timer? _pauseTimer;
  Completer<void>? _pause;

  /// Completes with true when the stream can be played and false when the
  /// wait was cancelled or is no longer wanted; fails with [PlaybackPreparationException] otherwise.
  late final Future<bool> ready;

  bool get isCancelled => _cancelToken.isCancelled;

  /// Aborts the pending probe or pause. [ready] then completes with false.
  void cancel() {
    if (isCancelled) return;
    _cancelToken.cancel();
    _pauseTimer?.cancel();
    final pause = _pause;
    if (pause != null && !pause.isCompleted) pause.complete();
  }

  Future<bool> _run() async {
    final deadline = _policy.now().add(_policy.maxWait);
    while (!isCancelled && _stillWanted?.call() != false) {
      final probe = await _probeOnce();
      if (probe == null) return false;
      if (probe.isReady) return true;
      if (!probe.isPreparing) throw _terminalFailure(probe.statusCode);
      final pause = _pauseFor(probe);
      if (_policy.now().add(pause).isAfter(deadline)) {
        throw const PlaybackPreparationException(
          PlaybackPreparationFailure.timedOut,
        );
      }
      await _wait(pause);
    }
    return false;
  }

  /// Returns null when the wait was cancelled while the request was pending.
  Future<PlaybackProbe?> _probeOnce() async {
    try {
      final probe = await _probe(_uri, cancelToken: _cancelToken);
      return isCancelled ? null : probe;
    } catch (_) {
      if (isCancelled) return null;
      // The detail (socket error, timeout) does not change what the user can
      // do about it, so it is reduced to one failure kind.
      throw const PlaybackPreparationException(
        PlaybackPreparationFailure.unreachable,
      );
    }
  }

  Duration _pauseFor(PlaybackProbe probe) {
    final asked = probe.retryAfter ?? _policy.defaultRetryInterval;
    return asked > _policy.maxRetryInterval ? _policy.maxRetryInterval : asked;
  }

  /// A cancellable delay: [cancel] completes it early and drops the timer.
  Future<void> _wait(Duration duration) {
    final pause = Completer<void>();
    _pause = pause;
    _pauseTimer = Timer(duration, () {
      if (!pause.isCompleted) pause.complete();
    });
    return pause.future;
  }

  static PlaybackPreparationException _terminalFailure(int statusCode) =>
      PlaybackPreparationException(
        switch (statusCode) {
          500 => PlaybackPreparationFailure.conversionFailed,
          507 => PlaybackPreparationFailure.outOfStorage,
          _ => PlaybackPreparationFailure.rejected,
        },
        statusCode: statusCode,
      );
}
