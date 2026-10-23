import 'network_policy.dart';
import 'dart:convert';
import 'dart:math';
import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'music_queue_snapshot.dart';

class SavedQueueAccount {
  const SavedQueueAccount(
      {required this.scope,
      required this.current,
      required this.read,
      required this.write});
  final String scope;
  final bool Function() current;
  final Future<Map<String, dynamic>> Function() read;
  final Future<({bool conflict, Map<String, dynamic> queue})> Function(
      Map<String, dynamic>, int) write;
}

Map<String, dynamic> portableQueue(MusicQueueSnapshot q) => {
      'entries':
          q.entries.map((e) => {'id': e.id, 'media_id': e.mediaId}).toList(),
      'current_id': q.index >= 0 ? q.entries[q.index].id : 0,
      'position_seconds': q.position,
      'label': q.label,
      'repeat': q.repeat.name,
      'shuffle': q.shuffle,
      'shuffle_mode': q.shuffleMode,
      'canonical': q.canonical.isEmpty
          ? q.entries.map((e) => e.id).toList()
          : q.canonical,
      'up_next': q.upNext.toList(),
    };

// Natural position advances do not cancel an explicit transfer. Structural
// queue edits and source/account generations do invalidate pending validation.
String portableQueueStructure(MusicQueueSnapshot snapshot) {
  final body = portableQueue(snapshot)
    ..remove('position_seconds')
    ..remove('up_next');
  return jsonEncode(body);
}

MusicQueueSnapshot portableQueueSnapshot(Map<String, dynamic> q) {
  final entries = (q['entries'] as List).cast<Map>();
  return MusicQueueSnapshot.decode(jsonEncode({
    'version': 1,
    'entries':
        entries.map((e) => {'id': e['id'], 'mediaId': e['media_id']}).toList(),
    'index': entries.indexWhere((e) => e['id'] == q['current_id']),
    'position': q['position_seconds'],
    'label': q['label'],
    'repeat': q['repeat'],
    'shuffle': q['shuffle'],
    if (q.containsKey('shuffle_mode')) 'shuffle_mode': q['shuffle_mode'],
    'canonical': q['canonical'],
    'upNext': q['up_next'],
  }));
}

/// Durable pending queue edits are retried by the existing global coordinator.
/// Network replies only change sync storage; applying a queue is an explicit UI action.
typedef PortableQueueRestore = ({MusicQueueSnapshot snapshot, bool unchanged});

class SavedQueueSync extends ChangeNotifier {
  final Map<String, int> _editStamps = {};
  int editStamp(String scope) => _editStamps[scope] ?? 0;
  final Map<String, Map<String, dynamic>> _states = {};
  Future<void> _writes = Future.value();
  String message = '';
  String? scope;
  bool conflict(String key) => _states[key]?['conflict'] == true;
  String _key(String scope) => '$scope.saved-queue';
  Future<Map<String, dynamic>> _load(String key) async {
    if (_states.containsKey(key)) return _states[key]!;
    final prefs = await SharedPreferences.getInstance();
    Map<String, dynamic> state;
    try {
      state = Map<String, dynamic>.from(
          jsonDecode(prefs.getString(_key(key)) ?? '{}') as Map);
    } catch (_) {
      state = {};
    }
    return _states.putIfAbsent(key, () => state);
  }

  Future<void> _save(String key, Map<String, dynamic> state) {
    final raw = jsonEncode(state);
    _writes = _writes.catchError((Object _) {}).then((_) async {
      final prefs = await SharedPreferences.getInstance();
      if (!await prefs.setString(_key(key), raw)) {
        throw StateError('Could not save session');
      }
    });
    return _writes;
  }

  String _mutation() {
    final random = Random.secure();
    return List.generate(
            16, (_) => random.nextInt(256).toRadixString(16).padLeft(2, '0'))
        .join();
  }

  Future<int> pending(SavedQueueAccount account) async =>
      (await _load(account.scope))['pending'] == null ? 0 : 1;

  Future<void> capture(SavedQueueAccount account, MusicQueueSnapshot snapshot,
      {bool baseline = false}) async {
    final incoming = jsonEncode(portableQueue(snapshot));
    if (!baseline &&
        account.current() &&
        _states[account.scope]?['local'] != incoming) {
      _editStamps[account.scope] = editStamp(account.scope) + 1;
    }
    final state = await _load(account.scope);
    if (!account.current()) return;
    final body = portableQueue(snapshot),
        encoded = jsonEncode(portableQueue(snapshot));
    if (state['local'] == encoded) return;
    state['local'] = encoded;
    if (!baseline) {
      state['pending'] = {...body, 'mutation_id': _mutation()};
      if (state['hold'] == true) state['conflict'] = true;
    }
    await _save(account.scope, state);
    if (account.current()) {
      scope = account.scope;
      notifyListeners();
    }
  }

  Future<void> sync(SavedQueueAccount account) async {
    await MusicNetworkPolicy.initialize();
    if (!account.current()) return;
    if (!MusicNetworkPolicy.syncAllowed) {
      message =
          'Saved music session sync waits for Wi-Fi. Changes stay on this device.';
      notifyListeners();
      return;
    }
    final state = await _load(account.scope);
    if (!account.current()) return;
    try {
      if (state['revision'] == null) {
        if (!MusicNetworkPolicy.syncAllowed) return;
        final remote = await account.read();
        if (!account.current()) return;
        state['remote'] = remote;
        state['revision'] = remote['revision'];
        if (remote['revision'] > 0) {
          if (state['pending'] != null &&
              (state['pending'] as Map)['mutation_id'] ==
                  remote['mutation_id']) {
            state['pending'] = null;
            state['hold'] = false;
          } else {
            state['hold'] = true;
            state['conflict'] = state['pending'] != null;
          }
        }
      }
      final pending = state['pending'];
      if (pending != null && state['conflict'] != true) {
        if (!MusicNetworkPolicy.syncAllowed) return;
        final result = await account.write(
            Map<String, dynamic>.from(pending as Map),
            state['revision'] as int);
        if (!account.current()) return;
        state['remote'] = result.queue;
        state['revision'] = result.queue['revision'];
        if (result.conflict) {
          state['conflict'] = true;
        } else if (state['pending'] != null &&
            (state['pending'] as Map)['mutation_id'] ==
                pending['mutation_id']) {
          state['pending'] = null;
        }
      }
      if (!account.current()) return;
      message = state['conflict'] == true
          ? 'Another client saved a session. Choose which session to keep.'
          : 'Session saved.';
      await _save(account.scope, state);
      if (state['conflict'] == true) throw StateError('Session conflict');
    } catch (_) {
      if (account.current() && state['conflict'] != true) {
        message = 'Session sync unavailable. Changes stay on this device.';
      }
      rethrow;
    } finally {
      if (account.current()) {
        scope = account.scope;
        notifyListeners();
      }
    }
  }

  Future<Map<String, dynamic>?> refresh(SavedQueueAccount account) async {
    await MusicNetworkPolicy.initialize();
    if (!account.current()) return null;
    if (!MusicNetworkPolicy.syncAllowed) {
      message =
          'Continue session waits for Wi-Fi. Change Music network settings or retry on Wi-Fi.';
      notifyListeners();
      return null;
    }
    final remote = await account.read();
    if (!account.current()) return null;
    final state = await _load(account.scope);
    if (!account.current()) return null;
    state['remote'] = remote;
    if (state['pending'] != null &&
        state['revision'] != null &&
        state['revision'] != remote['revision']) {
      state['conflict'] = true;
    }
    await _save(account.scope, state);
    return account.current() ? remote : null;
  }

  Future<void> replace(
      SavedQueueAccount account, MusicQueueSnapshot snapshot) async {
    final state = await _load(account.scope);
    if (!account.current()) return;
    final remote = await refresh(account);
    if (remote == null || !account.current()) return;
    state['revision'] = remote['revision'];
    state['conflict'] = false;
    state['hold'] = false;
    state['local'] = jsonEncode(portableQueue(snapshot));
    state['pending'] = {...portableQueue(snapshot), 'mutation_id': _mutation()};
    await _save(account.scope, state);
  }

  Future<void> keepOther(SavedQueueAccount account) async {
    final state = await _load(account.scope);
    if (!account.current()) return;
    state['pending'] = null;
    state['conflict'] = false;
    state['hold'] = true;
    await _save(account.scope, state);
    if (account.current()) {
      message = 'Other session kept. This queue stays on this device.';
      notifyListeners();
    }
  }

  Future<void> adopt(SavedQueueAccount account, Map<String, dynamic> remote,
      MusicQueueSnapshot snapshot,
      {required int expectedEditStamp, required bool unchanged}) async {
    final state = await _load(account.scope);
    if (!account.current()) return;
    final mayClear = unchanged && editStamp(account.scope) == expectedEditStamp;
    if (mayClear) {
      state['local'] = jsonEncode(portableQueue(snapshot));
      state['pending'] = null;
    }
    state['remote'] = remote;
    state['revision'] = remote['revision'];
    state['conflict'] = false;
    state['hold'] = false;
    await _save(account.scope, state);
    if (account.current()) {
      message = 'Session continued · paused. Resume when you’re ready.';
      notifyListeners();
    }
  }
}
