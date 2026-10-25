import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:sqflite/sqflite.dart';

import 'offline_downloads.dart' show OfflineDownloadCancelled;
import 'playback_request.dart';
import 'playback_session_coordinator.dart';
import 'podcast_identity.dart';
import 'podcast_repository.dart';
import 'podcast_sync_store.dart';

/// Device copies only. No API client, account transport, or music hooks.
class PodcastDownloads extends ChangeNotifier {
  PodcastDownloads(this.repository, this.playback, {Directory? directory})
      : _directory = directory;
  final PodcastRepository repository;
  final PlaybackSessionCoordinator playback;
  Directory? _directory;
  Future<void>? _initializing;
  String? _active;
  HttpClient? _client;
  bool _cancelled = false, _disposed = false;

  void _changed() {
    if (!_disposed) notifyListeners();
  }

  Future<void> get ready => _initializing ??=
          _initialize().catchError((Object error, StackTrace stack) {
        _initializing = null;
        Error.throwWithStackTrace(error, stack);
      });
  File _file(String id, {bool partial = false}) {
    if (!RegExp(r'^[0-9a-f]{32}$').hasMatch(id)) {
      throw StateError('Invalid device copy identity');
    }
    return File('${_directory!.path}/$id${partial ? '.part' : '.audio'}');
  }

  Future<void> _initialize() async {
    final db = await repository.database;
    _directory ??= Directory('${await getDatabasesPath()}/podcast-downloads');
    await _directory!.create(recursive: true);
    for (final row in await db.query('podcast_downloads')) {
      final id = row['id'] as String;
      final partial = _file(id, partial: true);
      if (await partial.exists()) await partial.delete();
      if (row['state'] != 'available') {
        final file = _file(id);
        if (await file.exists()) await file.delete();
        if (row['state'] == 'pending' || row['state'] == 'downloading') {
          await db.update(
              'podcast_downloads',
              {
                'state': 'failed',
                'error': 'Download interrupted. Retry download.'
              },
              where: 'id=?',
              whereArgs: [id]);
        }
      }
    }
  }

  Future<(String, String)> _identity(String feed, String episode,
      {DatabaseExecutor? executor}) async {
    final db = executor ?? await repository.database;
    feed = await repository.resolveFeed(db, feed);
    final known = (await db
            .query('podcast_episodes', where: 'feed_key=?', whereArgs: [feed]))
        .map((e) => PodcastKnownEpisode(
            e['episode_key'] as String,
            e['enclosure_key'] as String,
            (jsonDecode(e['aliases'] as String) as List).cast<String>()))
        .toList();
    return (feed, podcastResolveEpisode(known, episode, '') ?? episode);
  }

  Future<List<Map<String, Object?>>> _copies(
      String feed, String episode) async {
    final identity = await _identity(feed, episode);
    return (await repository.database).query('podcast_downloads',
        where: 'feed_key=? AND episode_key=?',
        whereArgs: [identity.$1, identity.$2],
        orderBy:
            "CASE state WHEN 'available' THEN 0 WHEN 'downloading' THEN 1 WHEN 'pending' THEN 2 ELSE 3 END");
  }

  Future<Map<String, Object?>?> copy(String feed, String episode) async {
    await ready;
    for (var row in await _copies(feed, episode)) {
      if (row['state'] == 'available') {
        final file = _file(row['id'] as String);
        if (!await file.exists() || await file.length() != row['size_bytes']) {
          await (await repository.database).update(
              'podcast_downloads',
              {
                'state': 'failed',
                'error': 'Device copy is missing or incomplete. Retry download.'
              },
              where: 'id=?',
              whereArgs: [row['id']]);
          continue;
        }
      }
      return row;
    }
    return (await _copies(feed, episode)).firstOrNull;
  }

  void cancel() {
    _cancelled = true;
    _client?.close(force: true);
  }

  void _check() {
    if (_cancelled || _disposed) throw OfflineDownloadCancelled();
  }

  static Uri _url(String value) {
    final uri = Uri.parse(value);
    if (!{'http', 'https'}.contains(uri.scheme) ||
        uri.host.isEmpty ||
        uri.userInfo.isNotEmpty) {
      throw const FormatException('Invalid publisher audio URL');
    }
    return uri.replace(fragment: '');
  }

  Future<void> download(String feed, String episode) async {
    // Reserve before the first await, including initialization.
    if (_active != null) {
      throw StateError('Wait for the current podcast download.');
    }
    _active = '';
    _cancelled = false;
    String? id;
    RandomAccessFile? output;
    var committed = false;
    try {
      await ready;
      _check();
      final identity = await _identity(feed, episode);
      feed = identity.$1;
      episode = identity.$2;
      final existing = await copy(feed, episode);
      if (existing?['state'] == 'available') return;
      id = existing?['id'] as String? ?? podcastEditId();
      _active = id;
      final db = await repository.database;
      // Insert under the catalog merge transaction boundary. A later merge
      // may re-key this row while HTTP streams; completion updates by id.
      var uri = await db.transaction((tx) async {
        final current = await _identity(feed, episode, executor: tx);
        feed = current.$1;
        episode = current.$2;
        final rows = await tx.query('podcast_episodes',
            where: 'feed_key=? AND episode_key=?', whereArgs: [feed, episode]);
        if (rows.isEmpty) throw StateError('Refresh this podcast first.');
        final uri = _url(rows.single['enclosure_url'] as String);
        await tx.insert(
            'podcast_downloads',
            {
              'id': id,
              'feed_key': feed,
              'episode_key': episode,
              'state': 'pending',
              'size_bytes': 0,
              'error': null
            },
            conflictAlgorithm: ConflictAlgorithm.replace);
        return uri;
      });
      _changed();
      // Stop any stale/truncated local source before replacing its bytes.
      await playback.stopLocalSource(_file(id).uri.toString());
      _check();
      _client = HttpClient()
        ..autoUncompress = false
        ..connectionTimeout = const Duration(seconds: 30);
      HttpClientResponse? response;
      for (var hops = 0; hops <= 10; hops++) {
        _check();
        final request =
            await _client!.getUrl(uri).timeout(const Duration(seconds: 30));
        request.followRedirects = false;
        // No Authorization, Cookie, Referer or caller-provided headers.
        request.headers.set(HttpHeaders.acceptEncodingHeader, 'identity');
        response = await request.close().timeout(const Duration(seconds: 30));
        _check();
        if (!{301, 302, 303, 307, 308}.contains(response.statusCode)) break;
        final location = response.headers.value(HttpHeaders.locationHeader);
        if (location == null || hops == 10) {
          throw const HttpException('Redirect failed');
        }
        uri = _url(uri.resolve(location).toString());
        // Do not retain cookies, including those set by a redirect response.
        await response.drain<void>().timeout(const Duration(seconds: 30));
      }
      final audio = response!;
      if (audio.statusCode != HttpStatus.ok) {
        throw const HttpException('Publisher refused audio');
      }
      final mime = audio.headers.contentType?.mimeType;
      if (mime != null &&
          !mime.startsWith('audio/') &&
          mime != 'application/octet-stream' &&
          mime != 'application/ogg' &&
          mime != 'video/mp4') {
        throw const HttpException('Publisher did not return audio');
      }
      await db.update('podcast_downloads', {'state': 'downloading'},
          where: 'id=?', whereArgs: [id]);
      _changed();
      output = await _file(id, partial: true).open(mode: FileMode.write);
      var received = 0;
      await for (final chunk in audio.timeout(const Duration(seconds: 30))) {
        _check();
        received += chunk.length;
        if (audio.contentLength >= 0 && received > audio.contentLength) {
          throw const HttpException('Audio exceeds declared length');
        }
        await output.writeFrom(chunk);
      }
      if (received == 0 ||
          (audio.contentLength >= 0 && received != audio.contentLength)) {
        throw const HttpException('Incomplete audio');
      }
      await output.flush();
      await output.close();
      output = null;
      _check();
      await _file(id, partial: true).rename(_file(id).path);
      _check();
      await db.update('podcast_downloads',
          {'state': 'available', 'size_bytes': received, 'error': null},
          where: 'id=?', whereArgs: [id]);
      _check();
      committed = true;
    } catch (_) {
      if (id != null) {
        await (await repository.database).update(
            'podcast_downloads',
            {
              'state': _cancelled ? 'missing' : 'failed',
              'error': _cancelled
                  ? null
                  : 'Download failed. Check publisher access, connection and device storage, then retry.'
            },
            where: 'id=?',
            whereArgs: [id]);
      }
      if (!_cancelled) {
        throw StateError('Podcast download failed. Retry download.');
      }
    } finally {
      _client?.close(force: true);
      _client = null;
      try {
        await output?.close();
        if (id != null) {
          for (final file in [
            _file(id, partial: true),
            if (!committed) _file(id)
          ]) {
            if (await file.exists()) await file.delete();
          }
        }
      } finally {
        _active = null;
        _changed();
      }
    }
  }

  Future<void> remove(String feed, String episode) async {
    await ready;
    for (final row in await _copies(feed, episode)) {
      final id = row['id'] as String;
      if (_active == id) throw StateError('Cancel this download first.');
      final file = _file(id);
      await playback.stopLocalSource(file.uri.toString());
      if (await file.exists()) await file.delete();
      await (await repository.database).update('podcast_downloads',
          {'state': 'missing', 'size_bytes': 0, 'error': null},
          where: 'id=?', whereArgs: [id]);
    }
    _changed();
  }

  Future<PodcastPlaybackRequest> playbackRequest(
      String scope, String feed, Map<String, Object?> episode) async {
    final key = episode['episode_key'] as String;
    final row = await copy(feed, key);
    if (row?['state'] != 'available') {
      throw StateError('Download this episode first.');
    }
    final store = PodcastSyncStore(repository);
    final duration = episode['duration_seconds'] as int;
    Future<void> save({double? seconds, bool? completed}) async {
      final current = await _identity(feed, key);
      await store.saveEpisode(scope, current.$1, current.$2,
          seconds: seconds,
          completed: completed,
          source: 'auto',
          enclosureUrl: episode['enclosure_url'] as String);
    }

    return PodcastPlaybackRequest(
      identity: 'podcast:$scope:$feed:$key',
      title: episode['title'] as String,
      sourceName: 'Podcast on this device',
      sourceUri: _file(row!['id'] as String).uri,
      readPosition: () async {
        final db = await repository.database;
        final current = await _identity(feed, key);
        final resolvedFeed =
            await store.resolve(db, scope, 'feed', '', current.$1);
        final resolvedEpisode =
            await store.resolve(db, scope, 'episode', resolvedFeed, current.$2);
        final state = (await db.query('podcast_episode_state',
                where: 'scope=? AND feed_key=? AND episode_key=?',
                whereArgs: [scope, resolvedFeed, resolvedEpisode]))
            .firstOrNull;
        final seconds = (state?['position_seconds'] as num?)?.toDouble() ?? 0;
        if ((state?['completed'] == 1 &&
                (state!['completion_ms'] as int) >=
                    (state['position_ms'] as int)) ||
            (duration > 0 && seconds >= duration - 5)) {
          return 0;
        }
        return seconds;
      },
      savePosition: (seconds) => save(seconds: seconds),
      markFinished: () => save(completed: true),
    );
  }

  @override
  void dispose() {
    _disposed = true;
    cancel();
    super.dispose();
  }
}
