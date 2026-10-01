import 'dart:async';

import '../models/local_media.dart';
import 'local_document_picker.dart';
import 'local_library_repository.dart';

/// A playback owner can hold a URI grant while a source is still active.
abstract interface class LocalDocumentUsage {
  bool isInUse(String uri);
}

class NoActiveLocalDocumentUsage implements LocalDocumentUsage {
  const NoActiveLocalDocumentUsage();

  @override
  bool isInUse(String uri) => false;
}

class LocalRelinkResult {
  const LocalRelinkResult({
    required this.media,
    this.oldGrantReleaseFailed = false,
    this.previousFileWasUnavailable = false,
  });

  final LocalMedia media;
  final bool oldGrantReleaseFailed;
  final bool previousFileWasUnavailable;
}

class LocalRemovalResult {
  const LocalRemovalResult({
    required this.removed,
    required this.grantReleased,
    this.releaseFailed = false,
  });

  final bool removed;
  final bool grantReleased;
  final bool releaseFailed;
}

/// Coordinates SAF grants with durable database cleanup records.
class LocalLibraryManager {
  LocalLibraryManager({
    required LocalDocumentPicker picker,
    required LocalMediaRepository media,
    required LocalGrantCleanupRepository cleanup,
    required LocalDocumentUsage usage,
  })  : _picker = picker,
        _media = media,
        _cleanup = cleanup,
        _usage = usage;

  final LocalDocumentPicker _picker;
  final LocalMediaRepository _media;
  final LocalGrantCleanupRepository _cleanup;
  final LocalDocumentUsage _usage;
  Future<void> _operationTail = Future<void>.value();

  Future<LocalMedia?> addDocument(LocalDocumentKind kind) =>
      _serialize(() async {
        final picked = await _picker.pick(kind);
        if (picked == null) return null;
        try {
          return await _media.importDocument(
            uri: picked.uri,
            title: _titleFor(picked),
            mimeType: _mimeTypeFor(picked),
            sizeBytes: picked.sizeBytes,
            ownsPersistedReadGrant: picked.newPersistedReadGrant,
          );
        } catch (_) {
          await _scheduleAndRetry(picked);
          rethrow;
        }
      });

  Future<LocalRelinkResult?> relink({
    required int id,
    required Future<bool?> Function(
      LocalMedia current,
      PickedLocalDocument replacement,
    ) confirmRetainProgress,
  }) =>
      _serialize(() async {
        final current = await _media.getById(id);
        if (current == null) return null;
        final previousFileWasUnavailable =
            !await _picker.isReadable(current.uri);
        final picked = await _picker.pick(_kindFor(current.mimeType));
        if (picked == null) return null;
        var committed = false;
        try {
          var retainProgress = true;
          if (picked.uri != current.uri) {
            final choice = await confirmRetainProgress(current, picked);
            if (choice == null) return null;
            retainProgress = choice;
          }

          final relinked = await _media.relink(
            id: id,
            uri: picked.uri,
            title: _titleFor(picked),
            mimeType: _mimeTypeFor(picked),
            sizeBytes: picked.sizeBytes,
            retainProgress: retainProgress,
            ownsPersistedReadGrant: picked.newPersistedReadGrant ||
                (picked.uri == current.uri && current.ownsPersistedReadGrant),
          );
          committed = true;
          var releaseFailed = false;
          if (current.uri != relinked.uri && current.ownsPersistedReadGrant) {
            if (!(await _cleanup.pendingUris()).contains(current.uri)) {
              // SQLite queues this in the relink transaction. Keep alternate
              // repository implementations safe as well.
              await _cleanup.scheduleRelease(current.uri);
            }
            releaseFailed = !await _retryUri(current.uri);
          }
          return LocalRelinkResult(
            media: relinked,
            oldGrantReleaseFailed: releaseFailed,
            previousFileWasUnavailable: previousFileWasUnavailable,
          );
        } finally {
          if (!committed) await _scheduleAndRetry(picked);
        }
      });

  Future<LocalRemovalResult> remove(int id) => _serialize(() async {
        final current = await _media.getById(id);
        if (current == null) {
          return const LocalRemovalResult(removed: false, grantReleased: false);
        }
        final removed = await _media.remove(id);
        if (!removed) {
          return const LocalRemovalResult(removed: false, grantReleased: false);
        }
        if (!current.ownsPersistedReadGrant) {
          return const LocalRemovalResult(removed: true, grantReleased: false);
        }
        if (!(await _cleanup.pendingUris()).contains(current.uri)) {
          // SQLite implementations enqueue this atomically with deletion. Keep
          // alternate repository implementations safe as well.
          await _cleanup.scheduleRelease(current.uri);
        }
        final released = await _retryUri(current.uri);
        return LocalRemovalResult(
          removed: true,
          grantReleased: released,
          releaseFailed: !released && !_usage.isInUse(current.uri),
        );
      });

  /// Retry queued releases at startup or when playback relinquishes a URI.
  Future<bool> releaseIfUnused(String uri) => _serialize(() async {
        if (!(await _cleanup.pendingUris()).contains(uri)) return false;
        return _retryUri(uri);
      });

  Future<void> retryPendingGrantReleases() => _serialize(() async {
        for (final uri in await _cleanup.pendingUris()) {
          await _retryUri(uri);
        }
      });

  Future<T> _serialize<T>(Future<T> Function() operation) {
    final previous = _operationTail;
    final release = Completer<void>();
    _operationTail = release.future;
    return _runSerialized(previous, release, operation);
  }

  Future<T> _runSerialized<T>(
    Future<void> previous,
    Completer<void> release,
    Future<T> Function() operation,
  ) async {
    await previous;
    try {
      return await operation();
    } finally {
      release.complete();
    }
  }

  Future<void> _scheduleAndRetry(PickedLocalDocument picked) async {
    if (!picked.newPersistedReadGrant) return;
    if (await _media.getByUri(picked.uri) != null) return;
    await _cleanup.scheduleRelease(picked.uri);
    await _retryUri(picked.uri);
  }

  Future<bool> _retryUri(String uri) async {
    if (await _media.getByUri(uri) != null) {
      await _cleanup.forget(uri);
      return false;
    }
    if (_usage.isInUse(uri)) return false;
    if (!(await _cleanup.pendingUris()).contains(uri)) return false;
    try {
      await _picker.releasePersistedReadGrant(uri);
      await _cleanup.forget(uri);
      return true;
    } catch (_) {
      // Keep the durable queue entry so a later startup can retry it.
      return false;
    }
  }

  String _titleFor(PickedLocalDocument picked) {
    final title = picked.name?.trim();
    if (title != null && title.isNotEmpty) return title;
    return 'Untitled ${picked.kind.name} file';
  }

  String _mimeTypeFor(PickedLocalDocument picked) {
    final mimeType = picked.mimeType?.trim();
    if (mimeType != null && mimeType.isNotEmpty) return mimeType;
    return picked.kind == LocalDocumentKind.audio ? 'audio/*' : 'video/*';
  }

  LocalDocumentKind _kindFor(String mimeType) =>
      mimeType.toLowerCase().startsWith('video/')
          ? LocalDocumentKind.video
          : LocalDocumentKind.audio;
}
