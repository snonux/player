import 'dart:async';
import 'dart:convert';
import 'package:flutter/foundation.dart';
import 'package:sqflite/sqflite.dart';
import '../models/media.dart';
import '../models/offline_media.dart';
import 'offline_collection_pins.dart';
import 'podcast_sync.dart';
import 'offline_download_repository.dart';

const automaticMusicCopyLimit = 200;

class AutomaticMusicPolicy {
  const AutomaticMusicPolicy(
      {this.enabled = false,
      this.playlists = false,
      this.starred = false,
      this.recent = false,
      this.wifiOnly = true,
      this.hours = 6});
  final bool enabled, playlists, starred, recent, wifiOnly;
  final int hours;
  bool get selected => playlists || starred || recent;
  factory AutomaticMusicPolicy.fromJson(Map value) => AutomaticMusicPolicy(
      enabled: value['enabled'] == true,
      playlists: value['playlists'] == true,
      starred: value['starred'] == true,
      recent: value['recent'] == true,
      wifiOnly: value['wifiOnly'] != false,
      hours: [1, 6, 24].contains(value['hours']) ? value['hours'] as int : 6);
  Map<String, dynamic> toJson() => {
        'enabled': enabled,
        'playlists': playlists,
        'starred': starred,
        'recent': recent,
        'wifiOnly': wifiOnly,
        'hours': hours
      };
}

class AutomaticMusicSnapshot {
  AutomaticMusicSnapshot(this.key, this.title, this.songs, {this.previousIds});
  final String key, title;
  final List<Media> songs;
  final String? previousIds;
}

class AutomaticMusicTargets {
  AutomaticMusicTargets(this.snapshots, this.songs);
  final List<AutomaticMusicSnapshot> snapshots;
  final List<Media> songs;
}

/// Resolve the entire authoritative selection before changing any saved membership.
Future<AutomaticMusicTargets> resolveAutomaticMusic(
    AutomaticMusicPolicy policy,
    List<Map<String, Object?>> pins,
    Future<dynamic> Function(String) read,
    bool Function() current) async {
  void check() {
    if (!current()) throw StateError('Automatic music canceled.');
  }

  List<Media> songs(dynamic value) {
    if (value is! List) throw StateError('Incomplete music response.');
    return value
        .map((row) => Media.fromJson(Map<String, dynamic>.from(row as Map)))
        .toList();
  }

  final snapshots = <AutomaticMusicSnapshot>[];
  if (policy.playlists) {
    for (final pin in pins.where((p) =>
        p['kind'] == 'playlist' &&
        p['pinned'] == 1 &&
        !(p['collection_key'] as String).startsWith('@automatic-'))) {
      final detail = await read(
          '/api/v1/playlists/${Uri.encodeComponent(pin['collection_key'] as String)}');
      check();
      snapshots.add(AutomaticMusicSnapshot(
          pin['collection_key'] as String,
          detail['name'] as String? ?? pin['title'] as String,
          songs(detail['songs']),
          previousIds: pin['media_ids'] as String));
    }
  }
  if (policy.starred || policy.recent) {
    final albums = await read('/api/v1/music/albums');
    check();
    if (albums is! List) throw StateError('Incomplete music catalog.');
    final catalog = <int, Media>{};
    for (final album in albums) {
      final detail = await read(
          '/api/v1/music/albums/${Uri.encodeComponent(album['album_key'] as String)}');
      check();
      for (final song in songs(detail['songs'])) {
        catalog[song.id] = song;
      }
    }
    if (policy.starred) {
      final favorites = <int>{};
      var offset = 0;
      for (;;) {
        final page = songs(await read(
            '/api/v1/media?favorites=true&type=audio&limit=200&offset=$offset'));
        check();
        favorites.addAll(page.map((s) => s.id));
        if (page.length < 200) break;
        offset += 200;
        if (offset > 100000) {
          throw StateError('Favorites pagination is too large.');
        }
      }
      snapshots.add(AutomaticMusicSnapshot(
          '@automatic-starred',
          'Automatic music · starred songs',
          catalog.values.where((s) => favorites.contains(s.id)).toList()));
    }
    if (policy.recent) {
      final recent = catalog.values.toList()
        ..sort((a, b) {
          final date = (b.createdAt?.millisecondsSinceEpoch ?? 0)
              .compareTo(a.createdAt?.millisecondsSinceEpoch ?? 0);
          return date == 0 ? b.id.compareTo(a.id) : date;
        });
      snapshots.add(AutomaticMusicSnapshot(
          '@automatic-recent',
          'Automatic music · newest 50 songs (recently added)',
          recent.take(50).toList()));
    }
  }
  final unique = <int, Media>{};
  for (final snapshot in snapshots) {
    for (final song in snapshot.songs) {
      if (song.id <= 0 || song.type != 'audio') {
        throw StateError('Invalid music response.');
      }
      unique[song.id] = song;
    }
  }
  if (unique.length > automaticMusicCopyLimit) {
    throw StateError(
        'More than 200 selected songs. Choose fewer playlists or categories.');
  }
  return AutomaticMusicTargets(snapshots, unique.values.toList());
}

class AutomaticMusicLibrary {
  AutomaticMusicLibrary(this.repository);
  final OfflineDownloadRepository repository;
  Future<bool> available(String origin, int user, int id) async {
    final item = await repository.get(offlineMediaKey(origin, user, id));
    if (item == null) return false;
    final file = repository.file(item.key);
    return await file.exists() && await file.length() == item.sizeBytes;
  }

  Future<List<int>> owned(String origin, int user) async {
    final db = await repository.database;
    await db.execute(
        'CREATE TABLE IF NOT EXISTS automatic_music_copies (download_key TEXT PRIMARY KEY REFERENCES downloads(key) ON DELETE CASCADE)');
    final rows = await db.rawQuery(
        'SELECT d.media_id FROM automatic_music_copies a JOIN downloads d ON d.key=a.download_key WHERE d.origin=? AND d.user_id=?',
        [origin, user]);
    final ids = <int>[];
    for (final row in rows) {
      final id = row['media_id'] as int;
      if (await available(origin, user, id)) ids.add(id);
    }
    return ids;
  }

  Future<List<Map<String, Object?>>> pins(String origin, int user) async =>
      (await OfflineCollectionPins(repository).collections())
          .where((p) => p['origin'] == origin && p['user_id'] == user)
          .toList();
  Future<void> commit(String origin, int user,
      List<AutomaticMusicSnapshot> snapshots, bool Function() current) async {
    void check() {
      if (!current()) throw StateError('Automatic music canceled.');
    }

    for (final snapshot in snapshots) {
      for (final song in snapshot.songs) {
        if (!await available(origin, user, song.id)) {
          throw StateError('A saved song is missing. Retry sync.');
        }
        check();
      }
    }
    final db = await repository.database;
    await db.transaction((tx) async {
      check();
      for (final snapshot in snapshots) {
        final prior = await tx.query('music_offline_pins',
            where: 'origin=? AND user_id=? AND kind=? AND collection_key=?',
            whereArgs: [origin, user, 'playlist', snapshot.key]);
        check();
        if (snapshot.previousIds != null &&
            (prior.isEmpty ||
                prior.single['pinned'] != 1 ||
                prior.single['media_ids'] != snapshot.previousIds)) {
          throw StateError('Pinned playlists changed. Retry sync.');
        }
        for (final song in snapshot.songs) {
          final row = await tx.query('downloads',
              columns: ['key'],
              where: 'key=?',
              whereArgs: [offlineMediaKey(origin, user, song.id)]);
          check();
          if (row.isEmpty) {
            throw StateError('A saved song was removed. Retry sync.');
          }
        }
      }
      final keys = snapshots.map((s) => s.key).toSet();
      for (final key in ['@automatic-starred', '@automatic-recent']) {
        if (!keys.contains(key)) {
          await tx.update(
              'music_offline_pins', {'pinned': 0, 'media_ids': '[]'},
              where: 'origin=? AND user_id=? AND kind=? AND collection_key=?',
              whereArgs: [origin, user, 'playlist', key]);
          check();
        }
      }
      for (final snapshot in snapshots) {
        await tx.insert(
            'music_offline_pins',
            {
              'origin': origin,
              'user_id': user,
              'kind': 'playlist',
              'collection_key': snapshot.key,
              'title': snapshot.title,
              'media_ids':
                  jsonEncode(snapshot.songs.map((s) => s.id).toSet().toList()),
              'pinned': 1
            },
            conflictAlgorithm: ConflictAlgorithm.replace);
        check();
      }
    });
  }
}

class AutomaticMusicAccount {
  AutomaticMusicAccount(
      {required this.origin,
      required this.userId,
      required this.current,
      required this.read,
      required this.save,
      this.cancel});
  final String origin;
  final void Function()? cancel;
  final int userId;
  final bool Function() current;
  final Future<dynamic> Function(String) read;
  final Future<void> Function(
      Media, bool Function(), void Function(void Function())) save;
  String get key => jsonEncode([origin, userId]);
}

/// Owns only automatic work. Cancel never stops a manual download or playback.
class AutomaticMusic extends ChangeNotifier {
  AutomaticMusic(this.library,
      {required this.load,
      required this.persist,
      required this.network,
      DateTime Function()? now})
      : now = now ?? DateTime.now;
  final AutomaticMusicLibrary library;
  final Future<String?> Function(String) load;
  final Future<void> Function(String, String) persist;
  final String Function() network;
  final DateTime Function() now;
  String? key;
  Map<String, dynamic> status = {};
  AutomaticMusicPolicy get policy =>
      AutomaticMusicPolicy.fromJson(status['policy'] as Map? ?? {});
  bool loading = false, active = false, disposed = false;
  int _epoch = 0;
  int _bindGeneration = 0;
  void Function()? _abort;
  Future<void> _ready = Future.value(), _writes = Future.value();
  Future<void>? _running;
  String? _invocation;
  bool get due =>
      policy.enabled &&
      policy.selected &&
      (status['nextDue'] as int? ?? 0) <= now().millisecondsSinceEpoch;
  Future<void> bind(String? next) {
    if (next == key) return _ready;
    cancel();
    key = next;
    status = {};
    loading = next != null;
    final generation = ++_bindGeneration;
    notifyListeners();
    return _ready = () async {
      try {
        final saved = next == null ? null : await load(next);
        if (generation != _bindGeneration || disposed) return;
        status = saved == null
            ? {}
            : Map<String, dynamic>.from(jsonDecode(saved) as Map);
        if (status['unfinishedAttempt'] == true) {
          status = {
            ...status,
            'error': 'interrupted',
            'message':
                'Previous automatic sync was interrupted. Retry to finish keeping music current.'
          };
        }
      } catch (_) {
        if (generation == _bindGeneration) {
          status = {
            'message':
                'Automatic music settings unavailable. Retry after checking storage.'
          };
        }
      } finally {
        if (generation == _bindGeneration && !disposed) {
          loading = false;
          notifyListeners();
        }
      }
    }();
  }

  Future<void> _store(
      String scope, Map<String, dynamic> value, bool Function() current) {
    final result = _writes.catchError((Object _) {}).then((_) async {
      if (!current()) return;
      await persist(scope, jsonEncode(value));
      if (current()) {
        status = value;
        notifyListeners();
      }
    });
    _writes = result;
    return result;
  }

  Future<void> savePolicy(AutomaticMusicPolicy value) async {
    final scope = key;
    if (scope == null || loading) {
      throw StateError('Sign in before configuring music.');
    }
    cancel();
    final epoch = _epoch;
    await _store(
        scope,
        {
          ...status,
          'policy': value.toJson(),
          'nextDue': 0,
          'error': null,
          'unfinishedAttempt': false,
          'message': ''
        },
        () => !disposed && key == scope && _epoch == epoch);
  }

  void cancel({String? invocation}) {
    if (invocation != null && invocation != _invocation) return;
    _epoch++;
    _abort?.call();
    _abort = null;
    final scope = key, epoch = _epoch;
    if (active && scope != null && !disposed) {
      unawaited(_store(
              scope,
              {
                ...status,
                'message': 'Canceled. Completed downloads remain available.',
                'error': 'canceled',
                'unfinishedAttempt': false,
                'nextDue': now().millisecondsSinceEpoch +
                    Duration(hours: policy.hours).inMilliseconds
              },
              () => !disposed && key == scope && epoch == _epoch)
          .catchError((Object _) {}));
    }
  }

  Future<void> sync(AutomaticMusicAccount account,
      {bool explicit = false, String? invocation}) async {
    if (!account.current()) return;
    if (key != account.key) await bind(account.key);
    await _ready;
    if (disposed ||
        !account.current() ||
        key != account.key ||
        !policy.enabled ||
        !policy.selected) {
      return;
    }
    if (!explicit && !due) {
      if (status['error'] is String) {
        throw SyncDeferred(status['error'] as String);
      }
      return;
    }
    if (_running != null) return _running!;
    final scope = account.key, epoch = _epoch, p = policy;
    bool current() =>
        !disposed &&
        epoch == _epoch &&
        key == scope &&
        account.current() &&
        (!p.wifiOnly || network() == 'wifi');
    bool same() =>
        !disposed && epoch == _epoch && key == scope && account.current();
    void check() {
      if (!current()) {
        throw StateError('Automatic music canceled or network policy changed.');
      }
    }

    _invocation = invocation;
    _abort = account.cancel;
    active = true;
    notifyListeners();
    _running = () async {
      final started = now().millisecondsSinceEpoch;
      await _store(
          scope,
          {
            ...status,
            'unfinishedAttempt': true,
            'lastAttempt': started,
            'nextDue': started + Duration(hours: p.hours).inMilliseconds,
            'error': null
          },
          same);
      try {
        if (p.wifiOnly && network() != 'wifi') {
          throw const AutomaticMusicPolicyWait();
        }
        final pins = await library.pins(account.origin, account.userId);
        check();
        final targets =
            await resolveAutomaticMusic(p, pins, account.read, current);
        check();
        final owned = await library.owned(account.origin, account.userId);
        check();
        var required = 0;
        for (final song in targets.songs) {
          if (!await library.available(
              account.origin, account.userId, song.id)) {
            required++;
          }
          check();
        }
        if (owned.length + required > automaticMusicCopyLimit) {
          throw StateError(
              'Automatic music reached 200 retained copies. Remove retired Downloads and retry.');
        }
        var done = 0;
        for (final song in targets.songs) {
          check();
          await account.save(song, current, (abort) {
            _abort = () {
              account.cancel?.call();
              abort();
            };
          });
          _abort = account.cancel;
          check();
          done++;
          await _store(
              scope,
              {
                ...status,
                'message': 'Saving $done of ${targets.songs.length} songs…'
              },
              same);
        }
        await library.commit(
            account.origin, account.userId, targets.snapshots, current);
        check();
        final retained = await library.owned(account.origin, account.userId);
        check();
        final desired = targets.songs.map((s) => s.id).toSet();
        await _store(
            scope,
            {
              ...status,
              'unfinishedAttempt': false,
              'lastSuccess': now().millisecondsSinceEpoch,
              'members': desired.length,
              'retained': retained.length,
              'retired': retained.where((id) => !desired.contains(id)).length,
              'message': 'Offline music is current.',
              'error': null
            },
            same);
      } catch (error) {
        await _store(
            scope,
            {
              ...status,
              'unfinishedAttempt': false,
              'error': syncErrorKind(error),
              'message': error.toString()
            },
            same);
        rethrow;
      }
    }()
        .whenComplete(() {
      _running = null;
      _abort = null;
      _invocation = null;
      active = false;
      if (!disposed) notifyListeners();
    });
    return _running!;
  }

  void schedulerChanged() {
    if (!disposed) notifyListeners();
  }

  @override
  void dispose() {
    cancel();
    disposed = true;
    super.dispose();
  }
}

class AutomaticMusicPolicyWait extends SyncDeferred {
  const AutomaticMusicPolicyWait() : super('policy');
  @override
  String toString() =>
      'Automatic music waits for Wi-Fi. Check Music network settings.';
}
