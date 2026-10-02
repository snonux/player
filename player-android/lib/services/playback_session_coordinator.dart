import 'dart:async';

import 'playback_request.dart';

typedef StopPlaybackSession = Future<void> Function();

/// Serializes ownership when audio and video screens use separate controllers.
/// Every claim invalidates earlier async work before it asks the old source to
/// flush and stop.
class PlaybackSessionCoordinator {
  int _generation = 0;
  _ActivePlayback? _active;
  PlaybackSourceKind? _pendingKind;
  String? _pendingIdentity;
  String? _pendingSourceUri;
  Future<void>? _stopping;
  final Set<String> _stoppingLocalUris = {};

  int get generation => _generation;
  PlaybackSourceKind? get activeKind => _active?.kind ?? _pendingKind;
  String? get activeIdentity => _active?.identity ?? _pendingIdentity;

  Future<PlaybackSessionLease?> claim({
    required PlaybackSourceKind kind,
    required String identity,
    required StopPlaybackSession stop,
    String? sourceUri,
  }) async {
    final generation = ++_generation;
    final previous = _active;
    _active = null;
    _pendingKind = kind;
    _pendingIdentity = identity;
    _pendingSourceUri = sourceUri;
    try {
      await _stopPrevious(previous);
    } catch (_) {
      if (_generation == generation) {
        _pendingKind = null;
        _pendingIdentity = null;
        _pendingSourceUri = null;
      }
      rethrow;
    }
    if (_generation != generation) return null;
    _pendingKind = null;
    _pendingIdentity = null;
    _pendingSourceUri = null;
    final lease = PlaybackSessionLease._(this, generation, kind, identity);
    _active = _ActivePlayback(kind, identity, stop, lease, sourceUri);
    return lease;
  }

  bool isCurrent(PlaybackSessionLease lease) =>
      _generation == lease.generation && identical(_active?.lease, lease);

  Future<void> stopServerSession() =>
      _stopIf((kind) => kind == PlaybackSourceKind.server);

  Future<void> stopCurrent() async {
    await _stopIf((_) => true);
    await _stopping;
  }

  Future<void> _stopIf(bool Function(PlaybackSourceKind) matches) async {
    final active = _active;
    final kind = activeKind;
    if (kind == null || !matches(kind)) return;
    ++_generation;
    _active = null;
    _pendingKind = null;
    _pendingIdentity = null;
    _pendingSourceUri = null;
    await _stopPrevious(active);
  }

  Future<void> _stopPrevious(_ActivePlayback? previous) async {
    final pending = _stopping;
    if (previous == null) {
      await pending;
      return;
    }
    final uri =
        previous.kind == PlaybackSourceKind.local ? previous.sourceUri : null;
    if (uri != null) _stoppingLocalUris.add(uri);
    final completion = Completer<void>();
    final stopping = completion.future;
    _stopping = stopping;
    try {
      if (pending != null) await pending;
      await previous.stop();
    } finally {
      completion.complete();
      if (identical(_stopping, stopping)) _stopping = null;
      if (uri != null) _stoppingLocalUris.remove(uri);
    }
  }

  Future<void> release(PlaybackSessionLease lease) async {
    if (!isCurrent(lease)) return;
    ++_generation;
    _active = null;
  }

  Future<void> stop(PlaybackSessionLease lease) async {
    if (!isCurrent(lease)) return;
    await stopCurrent();
  }

  bool isLocalSourceInUse(String uri) =>
      (_active?.kind == PlaybackSourceKind.local &&
          _active?.sourceUri == uri) ||
      (_pendingKind == PlaybackSourceKind.local && _pendingSourceUri == uri) ||
      _stoppingLocalUris.contains(uri);

  Future<void> stopLocalSource(String uri) async {
    if (activeKind == PlaybackSourceKind.local &&
        (_active?.sourceUri ?? _pendingSourceUri) == uri) {
      await stopCurrent();
    } else if (_stoppingLocalUris.contains(uri)) {
      await _stopping;
    }
  }
}

class PlaybackSessionLease {
  const PlaybackSessionLease._(
    this._owner,
    this.generation,
    this.kind,
    this.identity,
  );

  final PlaybackSessionCoordinator _owner;
  final int generation;
  final PlaybackSourceKind kind;
  final String identity;

  bool get isCurrent => _owner.isCurrent(this);
  Future<void> release() => _owner.release(this);
  Future<void> stop() => _owner.stop(this);
}

class _ActivePlayback {
  const _ActivePlayback(
    this.kind,
    this.identity,
    this.stop,
    this.lease,
    this.sourceUri,
  );

  final PlaybackSourceKind kind;
  final String identity;
  final StopPlaybackSession stop;
  final PlaybackSessionLease lease;
  final String? sourceUri;
}
