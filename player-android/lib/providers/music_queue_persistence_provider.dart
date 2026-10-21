import 'dart:async';
import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:just_audio/just_audio.dart';
import 'package:shared_preferences/shared_preferences.dart';
import '../api/dio_client.dart';
import '../api/player_api_client.dart';
import '../models/media.dart';
import '../services/music_queue.dart';
import '../services/music_queue_snapshot.dart';
import '../services/playback_request.dart';
import 'api_client_provider.dart';
import 'audio_handler_provider.dart';
import 'playback_preparer_provider.dart';
import 'playback_session_provider.dart';
import 'progress_queue_provider.dart';

final musicQueuePersistenceProvider =
    Provider((ref) => MusicQueuePersistence(ref));

/// Rebuilds sources only after explicit unlock; no credentials or media URLs
/// are written to disk. Retained functions capture the account credential epoch.
class MusicQueuePersistence {
  MusicQueuePersistence(this._ref);
  final Ref _ref;
  Future<void> _writes = Future.value();
  String? _boundKey;
  int? _boundScope, _boundGeneration;

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
    final storage = _ref.read(tokenStorageProvider);
    final jar = _ref.read(cookieJarProvider);
    final preparer = _ref.read(playbackPreparerProvider);
    final coordinator = _ref.read(playbackSessionCoordinatorProvider);
    bool current() =>
        mutations.generation == generation && handler.queueScope == scope;
    final prefs = await SharedPreferences.getInstance();
    if (!current()) return;
    var validating = true;
    final recovery = handler.queueRecoveryGeneration;
    void persist() {
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
          if (!await prefs.setString(key, snapshot.encode())) {
            if (current()) handler.queueStorageFailed();
          }
        } catch (_) {
          if (current()) handler.queueStorageFailed();
        }
      });
    }

    handler.persistMusicQueue = persist;
    Future<void> restore() async {
      if (!current()) return;
      validating = true;
      final attempt = handler.queueRecoveryGeneration;
      bool wanted() => current() && handler.queueRecoveryGeneration == attempt;
      handler.queueRestoreStatus('Restoring queue…', busy: true);
      MusicQueueSnapshot? snapshot;
      try {
        await _writes;
        final raw = prefs.getString(key);
        if (raw != null) snapshot = MusicQueueSnapshot.decode(raw);
      } catch (_) {
        if (!wanted()) return;
        try {
          await prefs.remove(key);
        } catch (_) {}
        validating = false;
        handler.queueRestoreStatus(
            'Saved queue was unreadable. Add songs to start again.');
        handler.persistMusicQueue = persist;
        return;
      }
      if (!wanted()) return;
      if (snapshot == null) {
        validating = false;
        handler.queueRestoreStatus('');
        handler.persistMusicQueue = persist;
        return;
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
          if (!wanted()) return;
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
        if (!wanted()) return;
        handler.persistMusicQueue = persist;
        validating = false;
        handler.restoreMusicQueue(
            entries: entries,
            index: index,
            label: snapshot.label,
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
            load: (request, wanted) async {
              bool valid() => current() && wanted();
              if (!valid()) return null;
              if (isCompatStreamUrl(request.sourceUri)) {
                final ready = await preparer
                    .prepare(request.sourceUri,
                        probe: client.probePlayback, stillWanted: valid)
                    .ready;
                if (!valid()) return null;
                if (!ready) throw StateError('Playback preparation stopped');
              }
              final headers = await accountRequestHeaders(
                  uri: request.sourceUri,
                  baseUrl: origin,
                  storage: storage,
                  cookieJar: jar,
                  mutations: mutations);
              if (!valid()) return null;
              return AudioSource.uri(request.sourceUri,
                  headers: headers.isEmpty ? null : headers);
            });
      } catch (_) {
        if (wanted()) {
          handler.queueRestoreStatus(
              'Can’t restore queue. Check your connection and retry.',
              failed: true);
        }
      }
    }

    handler.retryQueueRestore = restore;
    unawaited(restore());
  }
}
