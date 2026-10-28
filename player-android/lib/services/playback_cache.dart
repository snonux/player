import '../models/offline_media.dart';
import '../models/music_metadata.dart';
import '../models/lyrics.dart';
import 'lyric_snapshot.dart';
import 'playback_request.dart';
import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'package:crypto/crypto.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:sqflite/sqflite.dart';

/// Disposable, complete copies of played songs and videos. Kept downloads use
/// a separate repository.
class PlaybackCache extends ChangeNotifier {
  PlaybackCache({Directory? directory}) : _directory = directory;
  Directory? _directory;
  SharedPreferences? _preferences;
  final Map<String, Map<String, dynamic>> _rows = {};
  Future<void>? _opening;
  Future<void> _tail = Future.value();
  HttpClient? _client;
  String? _activeKey;
  final Set<String> _protectedKeys = {};
  final Set<String> _invalidated = {};
  int _epoch = 0;
  bool _disposed = false;
  String status = '';
  bool get busy => _activeKey != null;
  static const budgets = [0, 128, 512, 1024, 2048, 4096];
  int get budget => _preferences?.getInt('playback-cache-budget') ?? 128;
  bool get prefetch =>
      _preferences?.getBool('playback-cache-prefetch') ?? false;
  bool get completeBuffer =>
      _preferences?.getBool('playback-cache-buffer') ?? false;
  bool get external =>
      _preferences?.getBool('playback-cache-external') ?? false;
  Iterable<Map<String, dynamic>> get items => _rows.values;
  int get bytes => _rows.values.fold(
      0, (n, r) => n + (r['size'] as int) + (r['lyrics_bytes'] as int? ?? 0));
  void _changed() {
    if (!_disposed) notifyListeners();
  }

  Future<void> initialize() => _opening ??= _open().catchError((Object e) {
        _opening = null;
        throw e;
      });
  Future<void> _open() async {
    _preferences ??= await SharedPreferences.getInstance();
    if (_directory == null) {
      String? path;
      if (external) {
        path =
            await const MethodChannel('zone.foo.player_android/playback_cache')
                .invokeMethod<String>('externalDirectory');
        if (path == null) {
          throw StateError(
              'App external storage is unavailable. Choose internal storage.');
        }
      }
      _directory = Directory(path == null
          ? '${await getDatabasesPath()}/playback-cache'
          : '$path/playback-cache');
    }
    await _directory!.create(recursive: true);
    final index = File('${_directory!.path}/index.json');
    if (await index.exists()) {
      final data = jsonDecode(await index.readAsString());
      if (data is Map) {
        for (final e in data.entries) {
          if (RegExp(r'^[a-f0-9]{64}$').hasMatch(e.key.toString()) &&
              e.value is Map) {
            final row = Map<String, dynamic>.from(e.value);
            if (row['title'] is String &&
                row['used'] is int &&
                row['size'] is int &&
                (row['size'] as int) > 0 &&
                row['origin'] is String &&
                row['user'] is int &&
                await _file(e.key).exists() &&
                await _file(e.key).length() == row['size']) {
              _rows[e.key] = row;
            }
          }
        }
      }
    }
    await for (final entry in _directory!.list(followLinks: false)) {
      final name = entry.uri.pathSegments.last;
      if (entry is File &&
          (name.endsWith('.part') ||
              ((name.endsWith('.audio') || name.endsWith('.lyrics')) &&
                  !_rows.containsKey(name.split('.').first)))) {
        await entry.delete();
      }
    }
    await _prune();
    _changed();
  }

  /// Read-only local browsing adapter; old cache rows retain their saved title.
  Future<List<OfflineMedia>> offlineLibrary() async {
    await initialize();
    final result = <OfflineMedia>[];
    if (budget == 0) return result;
    for (final entry in _rows.entries.toList()) {
      final row = entry.value;
      if (row['type'] == 'video' ||
          _invalidated.contains(entry.key) ||
          !await _file(entry.key).exists() ||
          await _file(entry.key).length() != row['size']) {
        continue;
      }
      result.add(OfflineMedia(
          key: 'cache:${entry.key}',
          origin: row['origin'] as String,
          userId: row['user'] as int,
          mediaId: row['media_id'] as int? ?? 0,
          title: row['title'] as String,
          type: 'audio',
          sizeBytes: row['size'] as int,
          addedAt: DateTime.fromMillisecondsSinceEpoch(row['used'] as int),
          sourceName: row['source_name'] as String? ?? '',
          automationMusicEligible: row['automation_music'] == true,
          music: row['music'] is Map
              ? MusicMetadata.fromJson(
                  Map<String, dynamic>.from(row['music'] as Map))
              : null));
    }
    return result;
  }

  /// Resolves only bytes already owned by this saved cache account. No API or
  /// credentials participate; evicted/missing files never become streams.
  Future<OfflinePlaybackRequest> offlineRequest(
      String key, String origin, int user,
      {bool protect = false,
      Future<double?> Function()? readPosition,
      Future<void> Function(double)? savePosition,
      Future<void> Function()? markFinished}) async {
    await initialize();
    final row = _rows[key];
    if (row == null ||
        row['type'] == 'video' ||
        row['origin'] != origin ||
        row['user'] != user ||
        _invalidated.contains(key) ||
        !await _file(key).exists() ||
        await _file(key).length() != row['size']) {
      throw StateError('Cached copy unavailable.');
    }
    final uri = await lookup(key, protect: protect);
    if (uri == null || _rows[key] != row) {
      throw StateError('Cached copy unavailable.');
    }
    final music = row['music'] is Map
        ? MusicMetadata.fromJson(Map<String, dynamic>.from(row['music'] as Map))
        : null;
    return OfflinePlaybackRequest(
        downloadKey: 'cache:$key',
        automationMusicEligible: row['automation_music'] == true,
        serverOrigin: origin,
        userId: user,
        sourceUri: uri,
        title: row['title'] as String,
        sourceName: row['source_name'] as String? ?? '',
        music: music,
        artist: music?.artist ?? '',
        album: music?.album ?? '',
        readPosition: readPosition,
        // Device-only browsing has no unlocked server identity. Its caller may
        // supply durable scoped progress after unlocking a saved account.
        savePosition: savePosition ?? (_) async {},
        markFinished: markFinished ?? () async {});
  }

  String key(String origin, int user, int media, String variant) => sha256
      .convert(utf8.encode(jsonEncode([origin, user, media, variant])))
      .toString();
  Future<void> setAutomationMusicEligibility(
      String key, String origin, int user, bool eligible) async {
    await initialize();
    await _write(() async {
      final row = _rows[key];
      if (row == null || row['origin'] != origin || row['user'] != user) {
        throw StateError('Cached account changed.');
      }
      row['automation_music'] = eligible;
      await _save();
    });
  }

  File _file(String key, {bool partial = false}) =>
      File('${_directory!.path}/$key.${partial ? 'part' : 'audio'}');
  Future<T> _write<T>(Future<T> Function() fn) async {
    final before = _tail, done = Completer<void>();
    _tail = done.future;
    await before;
    try {
      return await fn();
    } finally {
      done.complete();
    }
  }

  Future<void> _save() async {
    final f = File('${_directory!.path}/index.tmp');
    await f.writeAsString(jsonEncode(_rows), flush: true);
    await f.rename('${_directory!.path}/index.json');
  }

  Future<void> _prune() async {
    var total = bytes;
    final entries = _rows.entries.toList()
      ..sort(
          (a, b) => (a.value['used'] as int).compareTo(b.value['used'] as int));
    for (final e in entries) {
      if (total <= budget * 1048576) break;
      if (_protectedKeys.contains(e.key)) continue;
      await _file(e.key).delete();
      await _deleteLyrics(e.key);
      total -=
          (e.value['size'] as int) + (e.value['lyrics_bytes'] as int? ?? 0);
      _rows.remove(e.key);
    }
    await _save();
  }

  Future<Uri?> lookup(String key, {bool protect = true}) async {
    await initialize();
    if (budget == 0) return null;
    return _write(() async {
      if (_invalidated.contains(key)) return null;
      final row = _rows[key];
      if (row == null || !await _file(key).exists()) return null;
      row['used'] = DateTime.now().millisecondsSinceEpoch;
      if (protect) _protectedKeys.add(key);
      await _save();
      return _file(key).uri;
    });
  }

  /// Revocation removes reuse permission before any fallible disk operation.
  /// A playing file may retain its bytes; subsequent opens cannot find it.
  Future<void> invalidate(String key) async {
    _invalidated.add(key);
    _rows.remove(key);
    try {
      await _write(() async {
        await _deleteLyrics(key);
        if (!_protectedKeys.contains(key) && await _file(key).exists()) {
          await _file(key).delete();
        }
        await _save();
      });
    } catch (_) {
      // Remove the older permission snapshot if replacing it failed. Playing
      // file handles stay intact; startup discards these unindexed copies.
      try {
        final index = File('${_directory!.path}/index.json');
        if (await index.exists()) await index.delete();
      } catch (_) {/* Read-only storage stays invalidated in memory. */}
      status =
          'A cached copy was invalidated. Storage cleanup will retry after restart.';
    }
    _changed();
  }

  Future<void> remove(String key) async {
    await initialize();
    await _write(() async {
      if (_protectedKeys.contains(key)) return;
      if (await _file(key).exists()) await _file(key).delete();
      await _deleteLyrics(key);
      _rows.remove(key);
      await _save();
    });
    _changed();
  }

  void cancel() {
    ++_epoch;
    _client?.close(force: true);
    status = 'Transfer cancelled.';
    _changed();
  }

  void playingUri(Uri? uri) {
    _protectedKeys
        .removeWhere((key) => _directory == null || _file(key).uri != uri);
  }

  void release() {
    _protectedKeys.clear();
    cancel();
  }

  Future<void> configure(
      {int? megabytes, bool? next, bool? buffer, bool? useExternal}) async {
    try {
      await initialize();
    } catch (_) {
      if (useExternal != false || _protectedKeys.isNotEmpty || busy) rethrow;
      _preferences ??= await SharedPreferences.getInstance();
      await _preferences!.setBool('playback-cache-external', false);
      _directory = null;
      _opening = null;
      _rows.clear();
      await initialize();
    }
    cancel();
    if (megabytes != null) {
      if (!budgets.contains(megabytes)) {
        throw ArgumentError('Invalid budget');
      }
      await _preferences!.setInt('playback-cache-budget', megabytes);
    }
    if (next != null) {
      await _preferences!.setBool('playback-cache-prefetch', next);
    }
    if (buffer != null) {
      await _preferences!.setBool('playback-cache-buffer', buffer);
    }
    if (useExternal != null && useExternal != external) {
      if (_protectedKeys.isNotEmpty || busy) {
        throw StateError(
            'Stop playback and transfers before changing storage.');
      }
      if (useExternal) {
        final path =
            await const MethodChannel('zone.foo.player_android/playback_cache')
                .invokeMethod<String>('externalDirectory');
        if (path == null) {
          throw StateError('App external storage is unavailable.');
        }
      }
      await clear();
      await _preferences!.setBool('playback-cache-external', useExternal);
      _directory = null;
      _opening = null;
      _rows.clear();
      await initialize();
    }
    await _write(_prune);
    status = 'Cache settings saved.';
    _changed();
  }

  Future<void> clear() async {
    await initialize();
    cancel();
    await _write(() async {
      for (final key in _rows.keys.toList()) {
        if (_protectedKeys.contains(key)) continue;
        if (await _file(key).exists()) await _file(key).delete();
        await _deleteLyrics(key);
        _rows.remove(key);
      }
      await _save();
    });
    status = _protectedKeys.isEmpty
        ? 'Cache cleared. Kept downloads remain available.'
        : 'Cache cleared except the playing song. Kept downloads remain available.';
    _changed();
  }

  /// Removes the given share of cached items that were played longest ago.
  /// The playing item is kept; returns how many items were removed.
  Future<int> clearLeastRecent(int percent) async {
    if (percent <= 0 || percent > 100) throw ArgumentError('Invalid share');
    await initialize();
    cancel();
    var removed = 0, freed = 0;
    await _write(() async {
      final oldest = _rows.entries.toList()
        ..sort((a, b) =>
            (a.value['used'] as int).compareTo(b.value['used'] as int));
      final count = (oldest.length * percent / 100).ceil();
      for (final entry in oldest.take(count)) {
        if (_protectedKeys.contains(entry.key)) continue;
        if (await _file(entry.key).exists()) await _file(entry.key).delete();
        await _deleteLyrics(entry.key);
        freed += (entry.value['size'] as int) +
            (entry.value['lyrics_bytes'] as int? ?? 0);
        _rows.remove(entry.key);
        removed++;
      }
      await _save();
    });
    status = removed == 0
        ? 'Nothing to remove.'
        : 'Removed $removed least recently played '
            '${removed == 1 ? 'item' : 'items'} '
            '(${(freed / 1048576).toStringAsFixed(1)} MB).';
    _changed();
    return removed;
  }

  Future<void> download(
      {required String key,
      required Uri uri,
      required Map<String, String> headers,
      required String title,
      int? mediaId,
      MusicMetadata? music,
      bool automationMusicEligible = false,
      String sourceName = '',
      String kind = 'audio',
      required String origin,
      required int userId,
      required bool Function() authorized}) async {
    await initialize();
    if (budget == 0 || !authorized() || _rows.containsKey(key)) return;
    if (busy) return;
    final epoch = ++_epoch;
    final noun = kind == 'video' ? 'Video' : 'Song';
    _activeKey = key;
    status = 'Caching $title…';
    _changed();
    RandomAccessFile? output;
    HttpClient? client;
    bool wanted() => epoch == _epoch && authorized() && !_disposed;
    try {
      client = HttpClient()
        ..autoUncompress = false
        ..connectionTimeout = const Duration(seconds: 15);
      _client = client;
      final request = await client.getUrl(uri);
      request.followRedirects = false;
      headers.forEach(request.headers.set);
      final response =
          await request.close().timeout(const Duration(seconds: 30));
      if (response.statusCode != 200 ||
          !(response.headers.contentType?.mimeType.startsWith('$kind/') ??
              false)) {
        throw HttpException('Server did not return complete $kind.');
      }
      final declared = response.contentLength;
      if (declared > budget * 1048576) {
        throw StateError('$noun exceeds the cache budget.');
      }
      output = await _file(key, partial: true).open(mode: FileMode.write);
      var received = 0;
      await for (final chunk in response.timeout(const Duration(seconds: 30))) {
        if (!wanted()) throw StateError('Transfer cancelled.');
        received += chunk.length;
        if (received > budget * 1048576) {
          throw StateError('$noun exceeds the cache budget.');
        }
        await output.writeFrom(chunk);
      }
      if (!wanted() ||
          received == 0 ||
          (declared >= 0 && received != declared)) {
        throw StateError('Incomplete cache copy.');
      }
      await output.flush();
      await output.close();
      output = null;
      final lyrics = mediaId == null || kind != 'audio'
          ? null
          : await fetchLyricSnapshot(
              audioUri: uri,
              mediaId: mediaId,
              headers: headers,
              etag: response.headers.value('etag'),
              authorized: wanted);
      await _write(() async {
        if (!wanted()) return;
        await _file(key, partial: true).rename(_file(key).path);
        _invalidated.remove(key);
        var lyricBytes = 0;
        if (lyrics != null) {
          try {
            final body = jsonEncode(lyrics);
            await _lyricsFile(key).writeAsString(body, flush: true);
            lyricBytes = utf8.encode(body).length;
          } catch (_) {
            try {
              await _deleteLyrics(key);
            } catch (_) {/* Orphan cleanup retries at restart. */}
          }
        }
        _rows[key] = {
          'title': title,
          if (kind != 'audio') 'type': kind,
          if (lyricBytes > 0) 'lyrics_bytes': lyricBytes,
          if (mediaId != null) 'media_id': mediaId,
          if (music != null) 'music': music.toJson(),
          'source_name': sourceName,
          'automation_music': automationMusicEligible,
          'size': received,
          'used': DateTime.now().millisecondsSinceEpoch,
          'origin': origin,
          'user': userId,
          'etag': response.headers.value('etag') ?? ''
        };
        await _prune();
      });
      status = '$noun cached.';
    } catch (_) {
      if (wanted()) {
        status = 'Could not cache ${noun.toLowerCase()}. '
            'Streaming and kept downloads remain available.';
      }
    } finally {
      client?.close(force: true);
      if (identical(_client, client)) _client = null;
      await output?.close();
      final part = _file(key, partial: true);
      if (await part.exists()) await part.delete();
      _activeKey = null;
      _changed();
    }
  }

  String? etag(String key) => _rows[key]?['etag'] as String?;

  /// Uses the actual loaded local source, never the current quality preference
  /// or another same-ID copy. Unproven/network sources have no saved snapshot.
  Future<Lyrics> lyricsForSource(Uri source, String origin, int user) async {
    await initialize();
    if (source.scheme != 'file') return const Lyrics();
    for (final entry in _rows.entries) {
      if (entry.value['origin'] == origin &&
          entry.value['user'] == user &&
          _file(entry.key).uri == source) {
        return lyrics(entry.key, origin, user);
      }
    }
    return const Lyrics();
  }

  Future<Lyrics> lyrics(String key, String origin, int user) async {
    await initialize();
    final row = _rows[key];
    if (row == null ||
        row['origin'] != origin ||
        row['user'] != user ||
        _invalidated.contains(key) ||
        !await _file(key).exists() ||
        await _file(key).length() != row['size']) {
      return const Lyrics();
    }
    try {
      final file = _lyricsFile(key);
      if (!await file.exists() || await file.length() > 1048576) {
        return const Lyrics();
      }
      final data = jsonDecode(await file.readAsString());
      return Lyrics.fromJson(data is Map
          ? {...data, 'provider_enabled': false, 'saved': true}
          : null);
    } catch (_) {
      return const Lyrics();
    }
  }

  File _lyricsFile(String key) => File('${_directory!.path}/$key.lyrics');
  Future<void> _deleteLyrics(String key) async {
    final file = _lyricsFile(key);
    if (await file.exists()) await file.delete();
  }

  @override
  void dispose() {
    _disposed = true;
    cancel();
    super.dispose();
  }
}
