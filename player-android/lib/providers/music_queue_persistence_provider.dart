import 'playback_cache_provider.dart';
import 'dart:async';
import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';
import '../models/media.dart';
import '../services/music_queue.dart';
import '../services/music_queue_snapshot.dart';
import '../services/saved_queue_sync.dart';
import '../services/playback_request.dart';
import 'api_client_provider.dart';
import 'global_sync_provider.dart';
import 'saved_queue_provider.dart';
import 'audio_handler_provider.dart';
import 'playback_session_provider.dart';
import 'progress_queue_provider.dart';

final musicQueuePersistenceProvider =
    Provider((ref) => MusicQueuePersistence(ref));

/// Rebuilds sources only after explicit unlock; no credentials or media URLs
/// are written to disk. Retained functions capture the account credential epoch.
class MusicQueuePersistence {
  MusicQueuePersistence(this._ref, {this.writeSnapshot});
  final Future<bool> Function(SharedPreferences, String, String)? writeSnapshot;
  final Ref _ref;
  Future<void> _writes = Future.value();
  String? _boundKey;
  int? _boundScope, _boundGeneration;
  Future<PortableQueueRestore?> Function(MusicQueueSnapshot)? _portableRestore;
  Future<PortableQueueRestore?> restorePortable(
          MusicQueueSnapshot snapshot) async =>
      await _portableRestore?.call(snapshot);

  Future<void> initialize(int userId) async {
    final handler = _ref.read(audioHandlerProvider);
    final origin = _ref.read(playerBaseUrlProvider);
    final mutations = _ref.read(credentialMutationQueueProvider);
    final generation = mutations.generation, scope = handler.queueScope;
    final key = MusicQueueSnapshot.key(origin.origin, userId);
    if (_boundKey == key &&
        _boundScope == scope &&
        _boundGeneration == generation) {
      return;
    }
    _boundKey = key;
    _boundScope = scope;
    _boundGeneration = generation;
    final client = _ref.read(apiClientProvider);
    final progress = _ref.read(progressQueueProvider);
    final cachedLoad = _ref.read(cachedAudioLoaderProvider);
    final coordinator = _ref.read(playbackSessionCoordinatorProvider);
    bool current() =>
        mutations.generation == generation && handler.queueScope == scope;
    handler.loadQueueAlbums = (ids) async {
      if (!current()) throw StateError('Account changed');
      final albums = await client.musicQueueAlbums(ids);
      if (!current()) throw StateError('Account changed');
      return albums;
    };
    final prefs = await SharedPreferences.getInstance();
    if (!current()) return;
    var validating = true;
    final recovery = handler.queueRecoveryGeneration;
    var restoring = false;
    var lastQueueSyncMs = 0;
    void persist() {
      final baseline = restoring;
      final snapshot = handler.queueSnapshot;
      if (!current() ||
          handler.musicQueue.restoreFailed ||
          (validating &&
              (handler.queueRecoveryGeneration == recovery ||
                  handler.musicQueue.entries.isEmpty))) {
        return;
      }
      _writes = _writes.then((_) async {
        try {
          if (!current()) return;
          if (!await (writeSnapshot?.call(prefs, key, snapshot.encode()) ??
              prefs.setString(key, snapshot.encode()))) {
            if (current()) handler.queueStorageFailed();
          }
          final account = captureSavedQueueAccount(_ref);
          if (account != null && current()) {
            await _ref
                .read(savedQueueSyncProvider)
                .capture(account, snapshot, baseline: baseline);
            final now = DateTime.now().millisecondsSinceEpoch;
            if (current() &&
                !baseline &&
                (!handler.player.playing || now - lastQueueSyncMs >= 15000)) {
              lastQueueSyncMs = now;
              _ref.read(globalSyncCoordinatorProvider).trigger();
            }
          }
        } catch (_) {
          if (current()) handler.queueStorageFailed();
        }
      });
    }

    handler.persistMusicQueue = persist;
    Future<PortableQueueRestore?> restore(
        [MusicQueueSnapshot? portable]) async {
      if (!current()) return null;
      if (portable == null) validating = true;
      final attempt = handler.queueRecoveryGeneration;
      var intentGeneration = coordinator.generation;
      final originalQueue = portableQueueStructure(handler.queueSnapshot);
      bool wanted() =>
          current() &&
          handler.queueRecoveryGeneration == attempt &&
          coordinator.generation == intentGeneration &&
          (portable == null ||
              portableQueueStructure(handler.queueSnapshot) == originalQueue);
      if (portable == null) {
        handler.queueRestoreStatus('Restoring queue…', busy: true);
      }
      MusicQueueSnapshot? snapshot = portable;
      try {
        await _writes;
        if (portable == null) {
          final raw = prefs.getString(key);
          if (raw != null) snapshot = MusicQueueSnapshot.decode(raw);
        }
      } catch (_) {
        if (!wanted()) return null;
        try {
          await prefs.remove(key);
        } catch (_) {}
        validating = false;
        handler.queueRestoreStatus(
            'Saved queue was unreadable. Add songs to start again.');
        handler.persistMusicQueue = persist;
        return null;
      }
      if (!wanted()) return null;
      if (snapshot == null) {
        validating = false;
        handler.queueRestoreStatus('');
        handler.persistMusicQueue = persist;
        return null;
      }
      try {
        final media = <int, Media>{};
        for (final id in snapshot.entries.map((e) => e.mediaId).toSet()) {
          try {
            final item = await client.getMedia(id);
            if (item.type == 'audio' && item.deletedAt == null) {
              media[id] = item;
            }
          } on DioException catch (error) {
            if (![403, 404, 410].contains(error.response?.statusCode)) rethrow;
          }
          if (!wanted()) return null;
        }
        final entries = snapshot.entries
            .where((e) => media.containsKey(e.mediaId))
            .map((entry) {
          final song = media[entry.mediaId]!;
          return MusicQueueEntry(
              entry.id,
              ServerPlaybackRequest(
                  mediaId: song.id,
                  serverOrigin: origin.origin,
                  userId: userId,
                  sourceUri: Uri.parse(client.playbackUrl(song)),
                  title: song.displayTitle,
                  artist: song.music?.artist ?? '',
                  album: song.music?.album ?? '',
                  music: song.music,
                  artworkUri: song.thumbnailPath.isNotEmpty
                      ? Uri.parse(client.thumbnailUrl(song.id))
                      : null,
                  sourceName: song.relPath,
                  startPosition: song.duration <= 1200 ? 0 : null,
                  readPosition: () => client.getMediaProgress(song.id),
                  savePosition: (seconds) => progress.enqueue(song.id, seconds),
                  markFinished: () => progress.enqueueFinished(song.id)));
        }).toList();
        final selected =
            snapshot.index >= 0 ? snapshot.entries[snapshot.index].id : null;
        var index = entries.indexWhere((entry) => entry.id == selected);
        var position = index >= 0 ? snapshot.position : 0.0;
        if (index < 0 && entries.isNotEmpty) {
          index = snapshot.index.clamp(0, entries.length - 1);
        }
        if (index >= 0) {
          final duration =
              media[(entries[index].request as ServerPlaybackRequest).mediaId]!
                  .duration;
          if (duration > 0) position = position.clamp(0, duration);
        }
        if (!wanted()) return null;
        if (portable != null) {
          restoring = true;
          intentGeneration =
              coordinator.generation + (coordinator.activeKind == null ? 0 : 1);
          await coordinator.stopCurrent();
          if (!wanted()) {
            restoring = false;
            return null;
          }
        }
        handler.persistMusicQueue = persist;
        validating = false;
        restoring = true;
        handler.restoreMusicQueue(
            entries: entries,
            index: index,
            label: snapshot.label,
            repeat: snapshot.repeat,
            shuffle: snapshot.shuffle,
            shuffleMode: snapshot.shuffleMode,
            canonical: snapshot.canonical
                .where((id) => entries.any((e) => e.id == id))
                .toList(),
            position: position,
            upNext:
                snapshot.upNext.intersection(entries.map((e) => e.id).toSet()),
            unavailable: snapshot.entries.length - entries.length,
            claim: (request) async {
              if (!current()) return null;
              return coordinator.claim(
                  kind: request.kind,
                  identity: request.identity,
                  sourceUri: request.sourceUri.toString(),
                  stop: handler.stop);
            },
            load: (request, wanted) =>
                cachedLoad(request, () => current() && wanted()));
        final applied = handler.queueSnapshot;
        restoring = false;
        await _writes;
        if (!current() ||
            coordinator.generation != intentGeneration ||
            handler.queueRecoveryGeneration != attempt) {
          return null;
        }
        return (
          snapshot: applied,
          unchanged: handler.queueSnapshot.encode() == applied.encode()
        );
      } catch (_) {
        restoring = false;
        if (wanted() && portable == null) {
          handler.queueRestoreStatus(
              'Can’t restore queue. Check your connection and retry.',
              failed: true);
        }
      }
      return null;
    }

    _portableRestore = (snapshot) => restore(snapshot);
    handler.retryQueueRestore = () async {
      await restore();
    };
    unawaited(restore());
  }
}
