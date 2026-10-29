import 'dart:convert';
import 'dart:math';
import 'package:sqflite/sqflite.dart';
import 'podcast_repository.dart';

/// Durable account registers. Network acknowledgements and cursors commit
/// together; an obsolete credential generation rolls back the entire page.
class PodcastSyncStore {
  PodcastSyncStore(this.repository);
  final PodcastRepository repository;
  Future<Database> get database => repository.database;

  Future<Map<String, Object?>> status(String scope) async {
    final db = await database;
    await db.insert('podcast_scopes', {'scope': scope},
        conflictAlgorithm: ConflictAlgorithm.ignore);
    return (await db
            .query('podcast_scopes', where: 'scope=?', whereArgs: [scope]))
        .single;
  }

  Future<String> resolve(DatabaseExecutor tx, String scope, String kind,
      String feed, String key) async {
    return repository.resolveAccountKey(tx, scope, kind, feed, key);
  }

  Future<void> saveEpisode(String scope, String feed, String episode,
      {double? seconds,
      bool? completed,
      bool? deleted,
      String source = 'manual',
      String enclosureUrl = '',
      int? observedMs}) async {
    if (seconds != null &&
        (!seconds.isFinite || seconds < 0 || seconds > 9e12)) {
      throw ArgumentError('Invalid episode position');
    }
    if (source != 'manual' && source != 'auto') {
      throw ArgumentError('Invalid completion source');
    }
    await (await database).transaction((tx) async {
      feed = await resolve(tx, scope, 'feed', '', feed);
      episode = await resolve(tx, scope, 'episode', feed, episode);
      final old = await _state(tx, scope, feed, episode);
      final now = observedMs ?? DateTime.now().millisecondsSinceEpoch;
      // A paired edit is one event, even when either register was advanced
      // by a future clock or an earlier edit in this millisecond.
      final pairedClock = max(
          now,
          max(old['position_ms'] as int? ?? 0,
                  old['completion_ms'] as int? ?? 0) +
              1);
      await tx.insert('podcast_scopes', {'scope': scope},
          conflictAlgorithm: ConflictAlgorithm.ignore);
      await tx.insert(
          'podcast_episode_state',
          {
            ...old,
            'scope': scope,
            'feed_key': feed,
            'episode_key': episode,
            if (enclosureUrl.isNotEmpty) 'enclosure_url': enclosureUrl,
            if (seconds != null) ...{
              'position_seconds': seconds,
              'position_ms': completed == null
                  ? max(now, (old['position_ms'] as int? ?? 0) + 1)
                  : pairedClock,
              'position_pending_edit_id': podcastEditId()
            },
            if (deleted != null) ...{
              'deleted': deleted ? 1 : 0,
              'deletion_ms': max(now, (old['deletion_ms'] as int? ?? 0) + 1),
              'deletion_pending_edit_id': podcastEditId(),
            },
            if (completed != null) ...{
              'completed': completed ? 1 : 0,
              'completion_source': source,
              'completion_ms': seconds == null
                  ? max(now, (old['completion_ms'] as int? ?? 0) + 1)
                  : pairedClock,
              'completion_pending_edit_id': podcastEditId()
            },
          },
          conflictAlgorithm: ConflictAlgorithm.replace);
    });
    repository.onSyncNeeded?.call();
  }

  Future<Map<String, Object?>> _state(DatabaseExecutor tx, String scope,
          String feed, String episode) async =>
      (await tx.query('podcast_episode_state',
              where: 'scope=? AND feed_key=? AND episode_key=?',
              whereArgs: [scope, feed, episode]))
          .firstOrNull ??
      {};

  Future<Map<String, dynamic>> payload(String scope,
      {int limit = 500,
      String? feed,
      String? episode,
      bool includeDeletion = true}) async {
    final db = await database;
    final meta = await status(scope);
    if (feed != null) {
      feed = await resolve(db, scope, 'feed', '', feed);
      episode = await resolve(db, scope, 'episode', feed, episode!);
    }
    final subs = <Map<String, dynamic>>[];
    final episodes = <Map<String, dynamic>>[];
    var remaining = limit;
    if (feed == null) {
      for (final row in await db.query('podcast_subscriptions',
          where: 'scope=? AND pending_edit_id IS NOT NULL',
          whereArgs: [scope],
          limit: remaining)) {
        subs.add({
          'edit_id': row['pending_edit_id'],
          'feed_key': row['feed_key'],
          'subscribed': row['subscribed'] == 1,
          'observed_ms': row['subscription_ms'],
          'title': row['title'],
          'fetch_url': row['fetch_url'],
          'urls': jsonDecode(row['urls'] as String)
        });
        remaining--;
      }
    }
    for (final row in await db.query('podcast_episode_state',
        where:
            'scope=? AND (position_pending_edit_id IS NOT NULL OR completion_pending_edit_id IS NOT NULL${includeDeletion ? ' OR deletion_pending_edit_id IS NOT NULL' : ''})${feed == null ? '' : ' AND feed_key=? AND episode_key=?'}',
        whereArgs: [
          scope,
          if (feed != null) ...[feed, episode]
        ])) {
      if (remaining == 0) break;
      final entry = <String, dynamic>{
        'feed_key': row['feed_key'],
        'episode_key': row['episode_key'],
        'enclosure_url': row['enclosure_url']
      };
      for (final kind in ['position', 'completion', 'deletion']) {
        if (remaining == 0 ||
            row['${kind}_pending_edit_id'] == null ||
            kind == 'deletion' && !includeDeletion) {
          continue;
        }
        entry[kind] = {
          'edit_id': row['${kind}_pending_edit_id'],
          'observed_ms': row['${kind}_ms'],
          if (kind == 'position')
            'seconds': row['position_seconds']
          else if (kind == 'deletion')
            'deleted': row['deleted'] == 1
          else ...{
            'completed': row['completed'] == 1,
            'source': row['completion_source']
          }
        };
        remaining--;
      }
      episodes.add(entry);
    }
    final recovery = await db
        .query('podcast_recovery_epochs', where: 'scope=?', whereArgs: [scope]);
    return {
      'epoch': recovery.firstOrNull?['epoch'] ?? meta['epoch'],
      if (feed == null) ...{'since': meta['cursor'], 'limit': 500} else
        'scope': {'feed_key': feed, 'episode_key': episode},
      'subscriptions': subs,
      'episodes': episodes
    };
  }

  static Set<String> editIds(Map<String, dynamic> payload) => {
        for (final row in payload['subscriptions'] as List)
          row['edit_id'] as String,
        for (final row in payload['episodes'] as List)
          for (final kind in ['position', 'completion', 'deletion'])
            if (row[kind] != null) row[kind]['edit_id'] as String,
      };
  Future<Set<String>> pending(String scope,
      {bool includeDeletion = true}) async {
    final db = await database;
    return {
      for (final row in await db.query('podcast_subscriptions',
          where: 'scope=? AND pending_edit_id IS NOT NULL', whereArgs: [scope]))
        row['pending_edit_id'] as String,
      for (final row in await db
          .query('podcast_episode_state', where: 'scope=?', whereArgs: [scope]))
        for (final kind in ['position', 'completion', 'deletion'])
          if ((kind != 'deletion' || includeDeletion) &&
              row['${kind}_pending_edit_id'] != null)
            row['${kind}_pending_edit_id'] as String,
    };
  }

  Future<bool> apply(
      String scope, Map<String, dynamic> response, bool Function() current,
      {bool scoped = false}) async {
    return (await database).transaction((tx) async {
      if (!current()) throw StateError('Account changed');
      if (!scoped) {
        await _beginEpoch(tx, scope, response['epoch'] as String,
            nextCursor: response['next_since'] as int);
      }
      for (final mapping in [
        ...(response['resolved'] as List? ?? []),
        ...(response['aliases'] as List? ?? [])
      ]) {
        await _alias(tx, scope, Map<String, dynamic>.from(mapping as Map));
      }
      final acked = (response['acked'] as List? ?? []).cast<String>().toSet();
      final rejected = response['rejected'] as List? ?? [];
      for (final entry in rejected) {
        await tx.insert('podcast_sync_rejections', {
          'scope': scope,
          'edit_id': entry['edit_id'],
          'reason': entry['reason']
        });
      }
      final removed = {
        ...acked,
        for (final r in rejected) r['edit_id'] as String
      };
      // Capture IDs before clearing them: only this exact acknowledged local edit
      // may adopt a clamped/superseded server register irrespective of clock.
      final accepted = <String>{...acked};
      for (final row in response['subscriptions'] as List? ?? []) {
        final key =
            await resolve(tx, scope, 'feed', '', row['feed_key'] as String);
        final old = (await tx.query('podcast_subscriptions',
                where: 'scope=? AND feed_key=?', whereArgs: [scope, key]))
            .firstOrNull;
        final pending = old?['pending_edit_id'];
        final force = pending != null && accepted.contains(pending);
        if (force ||
            (pending == null &&
                (row['subscription_ms'] as int) >=
                    (old?['subscription_ms'] as int? ?? 0))) {
          await tx.insert(
              'podcast_subscriptions',
              {
                'scope': scope,
                'feed_key': key,
                'subscribed': row['subscribed'] == true ? 1 : 0,
                'subscription_ms': row['subscription_ms'],
                'title': row['title'] ?? '',
                'fetch_url': row['fetch_url'] ?? '',
                'urls': jsonEncode(row['urls'] ?? []),
                'pending_edit_id': null
              },
              conflictAlgorithm: ConflictAlgorithm.replace);
          await tx.delete('podcast_epoch_recovery',
              where: 'scope=? AND kind=? AND feed_key=?',
              whereArgs: [scope, 'subscription', key]);
        }
        // Publisher refresh is deliberately separate from synchronization.
        if ((row['fetch_url'] as String? ?? '').isNotEmpty) {
          await tx.insert(
              'podcast_feeds',
              {
                'feed_key': key,
                'fetch_url': row['fetch_url'],
                'title': row['title'] ?? ''
              },
              conflictAlgorithm: ConflictAlgorithm.ignore);
        }
      }
      for (final row in response['episodes'] as List? ?? []) {
        final feed =
            await resolve(tx, scope, 'feed', '', row['feed_key'] as String);
        final episode = await resolve(
            tx, scope, 'episode', feed, row['episode_key'] as String);
        final old = await _state(tx, scope, feed, episode);
        final merged = <String, Object?>{
          ...old,
          'scope': scope,
          'feed_key': feed,
          'episode_key': episode
        };
        for (final kind in ['position', 'completion', 'deletion']) {
          final value = row[kind] as Map?;
          if (value == null) continue;
          final pending = old['${kind}_pending_edit_id'];
          final incomingClock = value['observed_ms'] as int;
          final oldClock = old['${kind}_ms'] as int? ?? 0;
          final losesDeletionTie = kind == 'deletion' &&
              incomingClock > 0 &&
              incomingClock == oldClock &&
              old['deleted'] == 1 &&
              value['deleted'] != true;
          if ((pending != null && accepted.contains(pending)) ||
              (pending == null &&
                  incomingClock >= oldClock &&
                  !losesDeletionTie)) {
            merged.addAll({
              '${kind}_ms': value['observed_ms'],
              '${kind}_pending_edit_id': null,
              if (kind == 'position')
                'position_seconds': value['seconds']
              else if (kind == 'deletion')
                'deleted': value['deleted'] == true ? 1 : 0
              else ...{
                'completed': value['completed'] == true ? 1 : 0,
                'completion_source': value['source']
              }
            });
            await tx.delete('podcast_epoch_recovery',
                where: 'scope=? AND kind=? AND feed_key=? AND episode_key=?',
                whereArgs: [scope, kind, feed, episode]);
          }
        }
        await tx.insert('podcast_episode_state', merged,
            conflictAlgorithm: ConflictAlgorithm.replace);
      }
      for (final id in removed) {
        await tx.update('podcast_subscriptions', {'pending_edit_id': null},
            where: 'scope=? AND pending_edit_id=?', whereArgs: [scope, id]);
        for (final kind in ['position', 'completion', 'deletion']) {
          // A hidden episode has no row action to retry a rejected deletion.
          // Retain that exact durable edit until the server acknowledges it.
          if (kind == 'deletion' && !acked.contains(id)) continue;
          await tx.update(
              'podcast_episode_state', {'${kind}_pending_edit_id': null},
              where: 'scope=? AND ${kind}_pending_edit_id=?',
              whereArgs: [scope, id]);
        }
      }
      if (!scoped) {
        await tx.update('podcast_scopes', {'cursor': response['next_since']},
            where: 'scope=?', whereArgs: [scope]);
        if (response['has_more'] == false) {
          await _finishEpoch(tx, scope, response['epoch'] as String);
        }
      }
      if (!current()) throw StateError('Account changed');
      return rejected.isEmpty;
    });
  }

  Future<void> _beginEpoch(DatabaseExecutor tx, String scope, String epoch,
      {required int nextCursor}) async {
    final meta =
        (await tx.query('podcast_scopes', where: 'scope=?', whereArgs: [scope]))
            .single;
    final recovering = await tx
        .query('podcast_recovery_epochs', where: 'scope=?', whereArgs: [scope]);
    if ((recovering.firstOrNull?['epoch'] ?? meta['epoch']) == epoch &&
        nextCursor >= (meta['cursor'] as int)) {
      return;
    }
    await tx
        .delete('podcast_epoch_recovery', where: 'scope=?', whereArgs: [scope]);
    await tx.insert('podcast_recovery_epochs', {'scope': scope, 'epoch': epoch},
        conflictAlgorithm: ConflictAlgorithm.replace);
    await tx.update('podcast_scopes', {'cursor': 0},
        where: 'scope=?', whereArgs: [scope]);
    await tx.rawInsert(
        "INSERT INTO podcast_epoch_recovery(scope,kind,feed_key) SELECT scope,'subscription',feed_key FROM podcast_subscriptions WHERE scope=? AND pending_edit_id IS NULL",
        [scope]);
    for (final kind in ['position', 'completion', 'deletion']) {
      await tx.rawInsert(
          "INSERT INTO podcast_epoch_recovery(scope,kind,feed_key,episode_key) SELECT scope,?,feed_key,episode_key FROM podcast_episode_state WHERE scope=? AND ${kind}_ms>0 AND ${kind}_pending_edit_id IS NULL",
          [kind, scope]);
    }
  }

  Future<void> _finishEpoch(
      DatabaseExecutor tx, String scope, String epoch) async {
    for (final row in await tx.query('podcast_epoch_recovery',
        where: 'scope=?', whereArgs: [scope])) {
      final kind = row['kind'] as String;
      final sub = kind == 'subscription';
      await tx.update(
          sub ? 'podcast_subscriptions' : 'podcast_episode_state',
          {
            sub ? 'pending_edit_id' : '${kind}_pending_edit_id': podcastEditId()
          },
          where:
              'scope=? AND feed_key=?${sub ? '' : ' AND episode_key=?'} AND ${sub ? 'pending_edit_id' : '${kind}_pending_edit_id'} IS NULL',
          whereArgs: [scope, row['feed_key'], if (!sub) row['episode_key']]);
    }
    await tx
        .delete('podcast_epoch_recovery', where: 'scope=?', whereArgs: [scope]);
    await tx.delete('podcast_recovery_epochs',
        where: 'scope=?', whereArgs: [scope]);
    await tx.update('podcast_scopes', {'epoch': epoch},
        where: 'scope=?', whereArgs: [scope]);
  }

  Future<void> _alias(
      DatabaseExecutor tx, String scope, Map<String, dynamic> alias) async {
    final kind = alias['kind'] as String;
    final source = alias['from'] as String;
    var feed = alias['feed_key'] as String? ?? '';
    if (kind == 'episode') feed = await resolve(tx, scope, 'feed', '', feed);
    final target = await resolve(tx, scope, kind, feed, alias['to'] as String);
    if (source == target) return;
    await tx.insert(
        'podcast_account_aliases',
        {
          'scope': scope,
          'kind': kind,
          'feed_key': feed,
          'source': source,
          'target': target
        },
        conflictAlgorithm: ConflictAlgorithm.replace);
    if (kind == 'feed') {
      final rows = await tx.query('podcast_subscriptions',
          where: 'scope=? AND feed_key=?', whereArgs: [scope, source]);
      if (rows.isNotEmpty) {
        await _moveSubscription(tx, scope, rows.single, target);
      }
      for (final row in await tx.query('podcast_episode_state',
          where: 'scope=? AND feed_key=?', whereArgs: [scope, source])) {
        final episode = await resolve(
            tx, scope, 'episode', target, row['episode_key'] as String);
        await _moveState(tx, scope, row, target, episode);
      }
      await tx.update('podcast_account_aliases', {'feed_key': target},
          where: 'scope=? AND kind=? AND feed_key=?',
          whereArgs: [scope, 'episode', source],
          conflictAlgorithm: ConflictAlgorithm.ignore);
      // Recovery markers follow register identities, including collision cases.
      await tx.rawInsert(
          'INSERT OR IGNORE INTO podcast_epoch_recovery SELECT scope,kind,?,episode_key FROM podcast_epoch_recovery WHERE scope=? AND feed_key=?',
          [target, scope, source]);
      await tx.delete('podcast_epoch_recovery',
          where: 'scope=? AND feed_key=?', whereArgs: [scope, source]);
    } else {
      final row = await _state(tx, scope, feed, source);
      if (row.isNotEmpty) await _moveState(tx, scope, row, feed, target);
      await tx.rawInsert(
          'INSERT OR IGNORE INTO podcast_epoch_recovery SELECT scope,kind,feed_key,? FROM podcast_epoch_recovery WHERE scope=? AND feed_key=? AND episode_key=?',
          [target, scope, feed, source]);
      await tx.delete('podcast_epoch_recovery',
          where: 'scope=? AND feed_key=? AND episode_key=?',
          whereArgs: [scope, feed, source]);
    }
  }

  Future<void> _moveSubscription(DatabaseExecutor tx, String scope,
      Map<String, Object?> row, String target) async {
    final old = (await tx.query('podcast_subscriptions',
            where: 'scope=? AND feed_key=?', whereArgs: [scope, target]))
        .firstOrNull;
    final winner = old == null ||
            (row['subscription_ms'] as int) > (old['subscription_ms'] as int)
        ? row
        : old;
    await tx.insert(
        'podcast_subscriptions',
        {
          ...winner,
          'feed_key': target,
          'pending_edit_id': winner['pending_edit_id'] ??
              (row['pending_edit_id'] != null || old?['pending_edit_id'] != null
                  ? podcastEditId()
                  : null)
        },
        conflictAlgorithm: ConflictAlgorithm.replace);
    await tx.delete('podcast_subscriptions',
        where: 'scope=? AND feed_key=?', whereArgs: [scope, row['feed_key']]);
  }

  Future<void> _moveState(DatabaseExecutor tx, String scope,
      Map<String, Object?> row, String feed, String episode) async {
    final old = await _state(tx, scope, feed, episode);
    final merged = <String, Object?>{
      ...row,
      ...old,
      'scope': scope,
      'feed_key': feed,
      'episode_key': episode
    };
    for (final kind in ['position', 'completion', 'deletion']) {
      final incomingClock = row['${kind}_ms'] as int;
      final oldClock = old['${kind}_ms'] as int? ?? 0;
      final winsDeletionTie = kind == 'deletion' &&
          incomingClock > 0 &&
          incomingClock == oldClock &&
          row['deleted'] == 1;
      final winner = incomingClock > oldClock || winsDeletionTie ? row : old;
      if (winner.isEmpty) continue;
      for (final field in kind == 'position'
          ? ['position_ms', 'position_seconds']
          : kind == 'completion'
              ? ['completion_ms', 'completed', 'completion_source']
              : ['deletion_ms', 'deleted']) {
        merged[field] = winner[field];
      }
      merged['${kind}_pending_edit_id'] = winner['${kind}_pending_edit_id'] ??
          (row['${kind}_pending_edit_id'] != null ||
                  old['${kind}_pending_edit_id'] != null
              ? podcastEditId()
              : null);
    }
    await tx.insert('podcast_episode_state', merged,
        conflictAlgorithm: ConflictAlgorithm.replace);
    await tx.delete('podcast_episode_state',
        where: 'scope=? AND feed_key=? AND episode_key=?',
        whereArgs: [scope, row['feed_key'], row['episode_key']]);
  }
}
