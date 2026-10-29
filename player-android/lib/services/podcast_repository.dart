import 'dart:convert';
import 'dart:math';
import 'package:sqflite/sqflite.dart';
import 'podcast_identity.dart';
import 'podcast_feed_fetcher.dart';

String podcastAccountScope(String origin, int userId) =>
    'account:${Uri.parse(origin).origin}#$userId';
String podcastEditId() {
  final random = Random.secure();
  return List.generate(
      16, (_) => random.nextInt(256).toRadixString(16).padLeft(2, '0')).join();
}

/// Own database: existing device files and server downloads need no migration.
class PodcastRepository {
  PodcastRepository(
      {this.databasePath = 'podcasts.db', PodcastFeedFetcher? fetcher})
      : fetcher = fetcher ?? PodcastFeedFetcher();
  final String databasePath;
  final PodcastFeedFetcher fetcher;
  Future<Database>? _opening;
  Future<Database> get database => _opening ??=
          openDatabase(databasePath, version: 6, onUpgrade: (db, old, _) async {
        if (old < 2) await createSyncSchema(db);
        if (old < 3) await createDownloadSchema(db);
        if (old < 4) {
          // Reparse cached feeds once after fixing numeric RSS timezones. A
          // conditional 304 response would otherwise leave their dates at zero.
          await db.update('podcast_feeds', {'etag': '', 'last_modified': ''},
              where: 'feed_key IN '
                  '(SELECT feed_key FROM podcast_episodes WHERE published_at=0)');
        }
        if (old < 5) {
          // Existing dates and identities remain intact. Re-read publisher
          // content once so a 304 cannot hide newly supported artwork/notes.
          final columns =
              await db.rawQuery('PRAGMA table_info(podcast_episodes)');
          if (!columns.any((column) => column['name'] == 'image_url')) {
            await db.execute(
                'ALTER TABLE podcast_episodes ADD COLUMN image_url TEXT NOT NULL DEFAULT ""');
          }
          await db.update('podcast_feeds', {'etag': '', 'last_modified': ''});
        }
        if (old < 6) {
          final columns =
              (await db.rawQuery('PRAGMA table_info(podcast_episode_state)'))
                  .map((r) => r['name'])
                  .toSet();
          for (final entry in {
            'deleted': 'INTEGER NOT NULL DEFAULT 0',
            'deletion_ms': 'INTEGER NOT NULL DEFAULT 0',
            'deletion_pending_edit_id': 'TEXT'
          }.entries) {
            if (!columns.contains(entry.key)) {
              await db.execute(
                  'ALTER TABLE podcast_episode_state ADD COLUMN ${entry.key} ${entry.value}');
            }
          }
        }
      }, onCreate: (db, _) async {
        await createSyncSchema(db);
        await createDownloadSchema(db);
        await db.execute(
            'CREATE TABLE podcast_scopes(scope TEXT PRIMARY KEY, epoch TEXT NOT NULL DEFAULT "", cursor INTEGER NOT NULL DEFAULT 0, last_full_success_ms INTEGER, last_attempt_ms INTEGER, last_error_kind TEXT, associated_from_local INTEGER NOT NULL DEFAULT 0)');
        await db.execute(
            'CREATE TABLE podcast_subscriptions(scope TEXT NOT NULL, feed_key TEXT NOT NULL, subscribed INTEGER NOT NULL, subscription_ms INTEGER NOT NULL, title TEXT NOT NULL DEFAULT "", fetch_url TEXT NOT NULL DEFAULT "", urls TEXT NOT NULL DEFAULT "[]", pending_edit_id TEXT, PRIMARY KEY(scope,feed_key))');
        await db.execute(
            'CREATE TABLE podcast_episode_state(scope TEXT NOT NULL, feed_key TEXT NOT NULL, episode_key TEXT NOT NULL, position_seconds REAL NOT NULL DEFAULT 0, position_ms INTEGER NOT NULL DEFAULT 0, position_pending_edit_id TEXT, completed INTEGER NOT NULL DEFAULT 0, completion_source TEXT NOT NULL DEFAULT "", completion_ms INTEGER NOT NULL DEFAULT 0, completion_pending_edit_id TEXT, enclosure_url TEXT NOT NULL DEFAULT "", deleted INTEGER NOT NULL DEFAULT 0, deletion_ms INTEGER NOT NULL DEFAULT 0, deletion_pending_edit_id TEXT, PRIMARY KEY(scope,feed_key,episode_key))');
        await db.execute(
            'CREATE TABLE podcast_feeds(feed_key TEXT PRIMARY KEY, title TEXT NOT NULL DEFAULT "", description TEXT NOT NULL DEFAULT "", image_url TEXT NOT NULL DEFAULT "", fetch_url TEXT NOT NULL, etag TEXT NOT NULL DEFAULT "", last_modified TEXT NOT NULL DEFAULT "", last_checked_ms INTEGER, last_error TEXT, aliases TEXT NOT NULL DEFAULT "[]")');
        await db.execute(
            'CREATE TABLE podcast_episodes(feed_key TEXT NOT NULL, episode_key TEXT NOT NULL, title TEXT NOT NULL, description TEXT NOT NULL, enclosure_url TEXT NOT NULL, enclosure_key TEXT NOT NULL, published_at INTEGER NOT NULL, duration_seconds INTEGER NOT NULL, image_url TEXT NOT NULL DEFAULT "", aliases TEXT NOT NULL DEFAULT "[]", PRIMARY KEY(feed_key,episode_key))');
        await db.execute(
            'CREATE TABLE podcast_sync_rejections(id INTEGER PRIMARY KEY AUTOINCREMENT, scope TEXT NOT NULL, edit_id TEXT NOT NULL, reason TEXT NOT NULL)');
      });
  void Function()? onSyncNeeded;
  static Future<void> createDownloadSchema(DatabaseExecutor db) async {
    // No foreign key to a copy: deleting audio must never cascade into history.
    await db.execute(
        'CREATE TABLE podcast_downloads(id TEXT PRIMARY KEY, feed_key TEXT NOT NULL, episode_key TEXT NOT NULL, state TEXT NOT NULL, size_bytes INTEGER NOT NULL DEFAULT 0, error TEXT)');
    await db.execute(
        'CREATE INDEX podcast_download_identity ON podcast_downloads(feed_key,episode_key)');
  }

  static Future<void> createSyncSchema(DatabaseExecutor db) async {
    await db.execute(
        'CREATE TABLE podcast_account_aliases(scope TEXT NOT NULL, kind TEXT NOT NULL, feed_key TEXT NOT NULL DEFAULT "", source TEXT NOT NULL, target TEXT NOT NULL, PRIMARY KEY(scope,kind,feed_key,source))');
    await db.execute(
        'CREATE TABLE podcast_epoch_recovery(scope TEXT NOT NULL, kind TEXT NOT NULL, feed_key TEXT NOT NULL, episode_key TEXT NOT NULL DEFAULT "", PRIMARY KEY(scope,kind,feed_key,episode_key))');
    await db.execute(
        'CREATE TABLE podcast_recovery_epochs(scope TEXT PRIMARY KEY, epoch TEXT NOT NULL)');
  }

  Future<void> close() async {
    if (_opening != null) await (await _opening!).close();
    _opening = null;
  }

  Future<List<Map<String, Object?>>> subscriptions(String scope) async =>
      (await database).rawQuery(
          'SELECT s.*, f.description,f.image_url,f.last_error,f.last_checked_ms FROM podcast_subscriptions s LEFT JOIN podcast_feeds f USING(feed_key) WHERE s.scope=? AND s.subscribed=1 ORDER BY s.title COLLATE NOCASE',
          [scope]);
  Future<List<Map<String, Object?>>> episodes(String feedKey) async =>
      (await database).query('podcast_episodes',
          where: 'feed_key=?',
          whereArgs: [feedKey],
          orderBy: 'published_at DESC,episode_key');

  /// Account aliases join registers to device metadata without re-keying copies.
  Future<List<Map<String, Object?>>> browseEpisodes(String scope,
      {String? feed,
      String filter = 'uncompleted',
      int offset = 0,
      int limit = 40}) async {
    if (offset < 0 ||
        limit < 1 ||
        limit > 200 ||
        !['uncompleted', 'completed', 'all'].contains(filter)) {
      throw ArgumentError('Invalid podcast page');
    }
    final db = await database;
    return db.transaction((tx) async {
      final aliases = await tx.query('podcast_account_aliases',
          where: 'scope=?', whereArgs: [scope]);
      final aliasTargets = {
        for (final a in aliases)
          (a['kind'], a['feed_key'], a['source']): a['target'] as String
      };
      String resolve(String kind, String feed, String key) {
        final seen = <String>{};
        while (seen.add(key)) {
          final target = aliasTargets[(kind, feed, key)];
          if (target == null) return key;
          key = target;
        }
        throw StateError('Cyclic podcast alias');
      }

      final subscriptions = {
        for (final s in await tx.query('podcast_subscriptions',
            where: 'scope=? AND subscribed=1', whereArgs: [scope]))
          s['feed_key'] as String: s
      };
      final selected = feed == null ? null : resolve('feed', '', feed);
      final states = {
        for (final s in await tx.query('podcast_episode_state',
            where: 'scope=?', whereArgs: [scope]))
          (s['feed_key'], s['episode_key']): s
      };
      final catalog = {
        for (final f in await tx.query('podcast_feeds')) f['feed_key']: f
      };
      final copies = await tx.query('podcast_downloads');
      int rank(Object? state) => switch (state) {
            'available' => 4,
            'pending' || 'downloading' => 3,
            'failed' => 2,
            'missing' => 1,
            _ => 0,
          };
      final copyRanks = <(Object?, Object?), int>{};
      for (final copy in copies) {
        final key = (copy['feed_key'], copy['episode_key']);
        copyRanks[key] = max(copyRanks[key] ?? 0, rank(copy['state']));
      }
      int copyRank(Map<String, Object?> e) =>
          copyRanks[(e['feed_key'], e['episode_key'])] ?? 0;
      final rows = <(String, String), Map<String, Object?>>{};
      for (final e in await tx.query('podcast_episodes',
          orderBy: 'published_at DESC,feed_key,episode_key')) {
        final deviceFeed = e['feed_key'] as String;
        final metadata = catalog[deviceFeed];
        final candidates = <String>[
          deviceFeed,
          ...((jsonDecode(metadata?['aliases'] as String? ?? '[]')) as List)
              .cast<String>()
        ];
        final scopedFeed = candidates
            .map((key) => resolve('feed', '', key))
            .where(subscriptions.containsKey)
            .firstOrNull;
        if (scopedFeed == null || selected != null && selected != scopedFeed) {
          continue;
        }
        final episodeKeys = [
          e['episode_key'] as String,
          ...(jsonDecode(e['aliases'] as String) as List).cast<String>()
        ];
        final mapped = episodeKeys
            .map((key) => resolve('episode', scopedFeed, key))
            .toList();
        final key = mapped
                .where((key) => states.containsKey((scopedFeed, key)))
                .firstOrNull ??
            mapped.where((key) => !episodeKeys.contains(key)).firstOrNull ??
            mapped.first;
        final state = states[(scopedFeed, key)] ?? const <String, Object?>{};
        if (state['deleted'] == 1) continue;
        final completed = state['completed'] == 1;
        if (filter == 'completed' && !completed ||
            filter == 'uncompleted' && completed) {
          continue;
        }
        final previous = rows[(scopedFeed, key)];
        if (previous == null || copyRank(e) > copyRank(previous)) {
          rows[(scopedFeed, key)] = {
            ...e, ...state,
            // Download APIs always receive device keys.
            'feed_key': deviceFeed, 'episode_key': e['episode_key'],
            'scope_feed_key': scopedFeed, 'scope_episode_key': key,
            'podcast_title': subscriptions[scopedFeed]!['title'],
            'feed_image_url': metadata?['image_url'] ?? '',
            'completed': completed ? 1 : 0,
            'position_seconds': state['position_seconds'] ?? 0.0,
            'enclosure_url': e['enclosure_url'],
          };
        }
      }
      final ordered = rows.values.toList()
        ..sort((a, b) {
          final published =
              (b['published_at'] as int).compareTo(a['published_at'] as int);
          if (published != 0) return published;
          final feedOrder = (a['scope_feed_key'] as String)
              .compareTo(b['scope_feed_key'] as String);
          return feedOrder != 0
              ? feedOrder
              : (a['scope_episode_key'] as String)
                  .compareTo(b['scope_episode_key'] as String);
        });
      return ordered.skip(offset).take(limit).toList();
    });
  }

  /// A pulled subscription may have no publisher metadata on this device yet.
  Future<void> refreshSubscription(String scope, String key) async {
    final db = await database;
    var deviceKey = key;
    for (final f in await db.query('podcast_feeds')) {
      final keys = [
        f['feed_key'] as String,
        ...(jsonDecode(f['aliases'] as String) as List).cast<String>()
      ];
      for (final candidate in keys) {
        if (await resolveAccountKey(db, scope, 'feed', '', candidate) == key) {
          deviceKey = f['feed_key'] as String;
          await refresh(deviceKey);
          return;
        }
      }
    }
    final subscription = (await db.query('podcast_subscriptions',
            where: 'scope=? AND feed_key=? AND subscribed=1',
            whereArgs: [scope, key]))
        .firstOrNull;
    if (subscription == null) return;
    final url = subscription['fetch_url'] as String;
    podcastFeedKey(url);
    await db.insert('podcast_feeds', {'feed_key': deviceKey, 'fetch_url': url},
        conflictAlgorithm: ConflictAlgorithm.ignore);
    await refresh(deviceKey);
  }

  /// Import metadata only. Device identities, personal registers, tombstones,
  /// copy keys and publisher conditional-request validators remain intact.
  Future<void> importServerEpisodes(
      String scope, String feed, List<Map<String, dynamic>> rows,
      {required bool Function() current}) async {
    if (!current()) throw StateError('Account changed');
    await (await database).transaction((tx) async {
      if (!current()) throw StateError('Account changed');
      final canonical = await resolveAccountKey(tx, scope, 'feed', '', feed);
      final subscription = (await tx.query('podcast_subscriptions',
              where: 'scope=? AND feed_key=? AND subscribed=1',
              whereArgs: [scope, canonical]))
          .firstOrNull;
      if (subscription == null) throw StateError('Subscription changed');
      var deviceFeed = canonical;
      for (final catalog in await tx.query('podcast_feeds')) {
        final keys = [
          catalog['feed_key'] as String,
          ...(jsonDecode(catalog['aliases'] as String) as List).cast<String>()
        ];
        var found = false;
        for (final key in keys) {
          if (await resolveAccountKey(tx, scope, 'feed', '', key) ==
              canonical) {
            found = true;
            break;
          }
        }
        if (found) {
          deviceFeed = catalog['feed_key'] as String;
          break;
        }
      }
      await tx.insert(
          'podcast_feeds',
          {
            'feed_key': deviceFeed,
            'fetch_url': subscription['fetch_url'],
            'title': subscription['title']
          },
          conflictAlgorithm: ConflictAlgorithm.ignore);
      final existing = await tx.query('podcast_episodes',
          where: 'feed_key=?', whereArgs: [deviceFeed]);
      final known = existing
          .map((e) => PodcastKnownEpisode(
              e['episode_key'] as String,
              e['enclosure_key'] as String,
              (jsonDecode(e['aliases'] as String) as List).cast<String>()))
          .toList();
      final items = <PodcastIdentityItem>[];
      for (final row in rows) {
        if (await resolveAccountKey(
                tx, scope, 'feed', '', row['feed_key'] as String) !=
            canonical) {
          throw StateError('Unexpected feed metadata');
        }
        final key = row['episode_key'] as String;
        items.add(PodcastIdentityItem(
            key.startsWith('guid:') ? key.substring(5) : '',
            row['episode_url'] as String));
      }
      final matches = podcastMatchEpisodes(known, items);
      for (var i = 0; i < rows.length; i++) {
        final row = rows[i], match = matches[i];
        if (match.key.isEmpty) continue;
        final previous = known.where((e) => e.key == match.key).firstOrNull;
        final aliases = <String>{
          ...?previous?.aliases,
          if (match.alias.isNotEmpty) match.alias,
          if (row['episode_key'] != match.key) row['episode_key'] as String
        };
        final published =
            DateTime.tryParse(row['published_at'] as String? ?? '');
        final enclosure = row['episode_url'] as String;
        await tx.insert(
            'podcast_episodes',
            {
              'feed_key': deviceFeed,
              'episode_key': match.key,
              'title': row['title'] as String? ?? '',
              'description': row['description'] as String? ?? '',
              'image_url': row['image_url'] as String? ?? '',
              'enclosure_url': enclosure,
              'enclosure_key': podcastEnclosureKey(enclosure),
              'published_at': published?.millisecondsSinceEpoch ?? 0,
              'duration_seconds':
                  (row['duration_seconds'] as num?)?.round() ?? 0,
              'aliases': jsonEncode(aliases.toList())
            },
            conflictAlgorithm: ConflictAlgorithm.replace);
      }
      await tx.update(
          'podcast_feeds',
          {
            // Keep the publisher error: this account-filtered catalogue page
            // cannot establish that another account has a complete feed cache.
            'last_checked_ms': DateTime.now().millisecondsSinceEpoch,
            if (rows.isNotEmpty)
              'image_url': rows.first['feed_image_url'] as String? ?? ''
          },
          where: 'feed_key=?',
          whereArgs: [deviceFeed]);
      if (!current()) throw StateError('Account changed');
    });
  }

  Future<List<String>> uncachedSubscriptions(String scope) async {
    final db = await database;
    final cached = <String>{};
    for (final row in await db.rawQuery(
        'SELECT DISTINCT f.feed_key,f.aliases FROM podcast_feeds f JOIN podcast_episodes e USING(feed_key) WHERE f.last_error IS NULL')) {
      for (final key in [
        row['feed_key'] as String,
        ...(jsonDecode(row['aliases'] as String) as List).cast<String>()
      ]) {
        cached.add(await resolveAccountKey(db, scope, 'feed', '', key));
      }
    }
    return (await subscriptions(scope))
        .map((s) => s['feed_key'] as String)
        .where((key) => !cached.contains(key))
        .toList();
  }

  Future<String> resolveFeed(DatabaseExecutor db, String key) async {
    final feeds = await db.query('podcast_feeds');
    for (final f in feeds) {
      if (f['feed_key'] == key ||
          (jsonDecode(f['aliases'] as String) as List).contains(key)) {
        return f['feed_key'] as String;
      }
    }
    return key;
  }

  Future<String> resolveAccountKey(DatabaseExecutor tx, String scope,
      String kind, String feed, String key) async {
    final visited = <String>{};
    while (visited.add(key)) {
      final aliases = await tx.query('podcast_account_aliases',
          where: 'scope=? AND kind=? AND feed_key=? AND source=?',
          whereArgs: [scope, kind, kind == 'feed' ? '' : feed, key]);
      if (aliases.isEmpty) return key;
      key = aliases.single['target'] as String;
    }
    throw StateError('Cyclic podcast alias');
  }

  Future<String> subscribe(String scope, String url) async {
    final minted = podcastFeedKey(url);
    final db = await database;
    final key = await db.transaction((tx) async {
      final key = await resolveAccountKey(
          tx, scope, 'feed', '', await resolveFeed(tx, minted));
      await tx.insert(
          'podcast_feeds', {'feed_key': key, 'fetch_url': _fetchUrl(url)},
          conflictAlgorithm: ConflictAlgorithm.ignore);
      await _subscription(tx, scope, key, true, url: _fetchUrl(url));
      return key;
    });
    onSyncNeeded?.call();
    await refresh(key);
    return key;
  }

  String _fetchUrl(String raw) {
    final trimmed = podcastTrim(raw);
    if (RegExp(r'^https?://', caseSensitive: false).hasMatch(trimmed)) {
      return trimmed;
    }
    var inner = trimmed.substring(trimmed.indexOf(':') + 1);
    if (inner.startsWith('//')) inner = inner.substring(2);
    return RegExp(r'^https?://', caseSensitive: false).hasMatch(inner)
        ? inner
        : 'http://$inner';
  }

  Future<void> unsubscribe(String scope, String key) async {
    await (await database)
        .transaction((tx) => _subscription(tx, scope, key, false));
    onSyncNeeded?.call();
  }

  Future<void> _subscription(
      DatabaseExecutor tx, String scope, String key, bool subscribed,
      {String? url}) async {
    key = await resolveAccountKey(tx, scope, 'feed', '', key);
    await tx.insert('podcast_scopes', {'scope': scope},
        conflictAlgorithm: ConflictAlgorithm.ignore);
    final old = await tx.query('podcast_subscriptions',
        where: 'scope=? AND feed_key=?', whereArgs: [scope, key]);
    final previous = old.firstOrNull;
    await tx.insert(
        'podcast_subscriptions',
        {
          'scope': scope,
          'feed_key': key,
          'subscribed': subscribed ? 1 : 0,
          'subscription_ms': max(DateTime.now().millisecondsSinceEpoch,
              (previous?['subscription_ms'] as int? ?? 0) + 1),
          'title': previous?['title'] ?? url ?? key,
          'fetch_url': url ?? previous?['fetch_url'] ?? '',
          'urls': previous?['urls'] ?? jsonEncode([if (url != null) url]),
          'pending_edit_id': podcastEditId()
        },
        conflictAlgorithm: ConflictAlgorithm.replace);
  }

  Future<void> refresh(String key) async {
    final db = await database;
    final rows =
        await db.query('podcast_feeds', where: 'feed_key=?', whereArgs: [key]);
    if (rows.isEmpty) return;
    final old = rows.single;
    try {
      final feed = await fetcher.fetch(old['fetch_url'] as String,
          etag: old['etag'] as String,
          lastModified: old['last_modified'] as String);
      await db.transaction((tx) async {
        for (final url in feed.aliasUrls) {
          final target = await resolveFeed(tx, podcastFeedKey(url));
          if (target != key &&
              (await tx.query('podcast_feeds',
                      where: 'feed_key=?', whereArgs: [target]))
                  .isNotEmpty) {
            await _mergeFeeds(tx, key, target);
            key = target;
          }
        }
        final currentFeed = (await tx
                .query('podcast_feeds', where: 'feed_key=?', whereArgs: [key]))
            .single;
        final aliasKeys = (jsonDecode(currentFeed['aliases'] as String) as List)
            .cast<String>()
            .toSet();
        for (final url in feed.aliasUrls) {
          final alias = podcastFeedKey(url);
          if (alias != key && aliasKeys.length < 16) aliasKeys.add(alias);
        }
        await tx.update(
            'podcast_feeds',
            {
              'fetch_url': feed.fetchUrl,
              'aliases': jsonEncode(aliasKeys.toList()),
              'last_checked_ms': DateTime.now().millisecondsSinceEpoch,
              'last_error': null,
              if (!feed.notModified) ...{
                'title': feed.title,
                'description': feed.description,
                'image_url': feed.imageUrl,
                'etag': feed.etag,
                'last_modified': feed.lastModified
              }
            },
            where: 'feed_key=?',
            whereArgs: [key]);
        if (feed.notModified) return;
        await tx.update('podcast_subscriptions',
            {'title': feed.title, 'fetch_url': feed.fetchUrl},
            where: 'feed_key=?', whereArgs: [key]);
        final existing = await tx
            .query('podcast_episodes', where: 'feed_key=?', whereArgs: [key]);
        final known = existing
            .map((e) => PodcastKnownEpisode(
                e['episode_key'] as String,
                e['enclosure_key'] as String,
                (jsonDecode(e['aliases'] as String) as List).cast<String>()))
            .toList();
        final matches = podcastMatchEpisodes(
            known, feed.episodes.map((e) => e.identity).toList());
        for (var i = 0; i < matches.length; i++) {
          final match = matches[i];
          if (match.key.isEmpty) continue;
          final e = feed.episodes[i];
          final previous = known.where((k) => k.key == match.key).firstOrNull;
          await tx.insert(
              'podcast_episodes',
              {
                'feed_key': key,
                'episode_key': match.key,
                'title': e.title,
                'description': e.description,
                'image_url': e.imageUrl,
                'enclosure_url': e.identity.enclosureUrl,
                'enclosure_key': podcastEnclosureKey(e.identity.enclosureUrl),
                'published_at': e.publishedAt,
                'duration_seconds': e.durationSeconds,
                'aliases': jsonEncode([
                  ...?previous?.aliases,
                  if (match.alias.isNotEmpty) match.alias
                ])
              },
              conflictAlgorithm: ConflictAlgorithm.replace);
        }
      });
    } catch (error) {
      await db.update(
          'podcast_feeds',
          {
            'last_error':
                'Feed refresh unavailable. Cached episodes remain available.',
            'last_checked_ms': DateTime.now().millisecondsSinceEpoch
          },
          where: 'feed_key=?',
          whereArgs: [key]);
      rethrow;
    }
  }

  Future<void> _mergeFeeds(
      DatabaseExecutor tx, String source, String target) async {
    final sourceFeed = (await tx
            .query('podcast_feeds', where: 'feed_key=?', whereArgs: [source]))
        .single;
    final targetFeed = (await tx
            .query('podcast_feeds', where: 'feed_key=?', whereArgs: [target]))
        .single;
    final aliases = <String>{
      ...(jsonDecode(targetFeed['aliases'] as String) as List).cast<String>(),
      source,
      ...(jsonDecode(sourceFeed['aliases'] as String) as List).cast<String>()
    }.toList();
    await tx.update('podcast_feeds', {'aliases': jsonEncode(aliases)},
        where: 'feed_key=?', whereArgs: [target]);
    for (final row in await tx.query('podcast_subscriptions',
        where: 'feed_key=?', whereArgs: [source])) {
      final old = await tx.query('podcast_subscriptions',
          where: 'scope=? AND feed_key=?', whereArgs: [row['scope'], target]);
      final winner = old.isEmpty ||
              (row['subscription_ms'] as int) >
                  (old.single['subscription_ms'] as int)
          ? row
          : old.single;
      await tx.insert('podcast_subscriptions',
          {...winner, 'feed_key': target, 'pending_edit_id': podcastEditId()},
          conflictAlgorithm: ConflictAlgorithm.replace);
    }
    final targetEpisodes = await tx
        .query('podcast_episodes', where: 'feed_key=?', whereArgs: [target]);
    final known = targetEpisodes
        .map((e) => PodcastKnownEpisode(
            e['episode_key'] as String,
            e['enclosure_key'] as String,
            (jsonDecode(e['aliases'] as String) as List).cast<String>()))
        .toList();
    for (final row in await tx
        .query('podcast_episodes', where: 'feed_key=?', whereArgs: [source])) {
      final episodeKey = podcastResolveEpisode(known,
              row['episode_key'] as String, row['enclosure_url'] as String) ??
          row['episode_key'] as String;
      await tx.update(
          'podcast_downloads', {'feed_key': target, 'episode_key': episodeKey},
          where: 'feed_key=? AND episode_key=?',
          whereArgs: [source, row['episode_key']]);
      final existing = await tx.query('podcast_episodes',
          where: 'feed_key=? AND episode_key=?',
          whereArgs: [target, episodeKey]);
      if (existing.isEmpty) {
        await tx.insert('podcast_episodes',
            {...row, 'feed_key': target, 'episode_key': episodeKey});
      } else {
        // Acquisition is capped during ingest; merging established identities
        // must retain every alias or old URLs/GUIDs can split history again.
        final episodeAliases = <String>{
          ...(jsonDecode(existing.single['aliases'] as String) as List)
              .cast<String>(),
          ...(jsonDecode(row['aliases'] as String) as List).cast<String>(),
          if (row['episode_key'] != episodeKey) row['episode_key'] as String,
        }..remove(episodeKey);
        await tx.update('podcast_episodes',
            {'aliases': jsonEncode(episodeAliases.toList())},
            where: 'feed_key=? AND episode_key=?',
            whereArgs: [target, episodeKey]);
      }
    }
    final mergedEpisodes = await tx
        .query('podcast_episodes', where: 'feed_key=?', whereArgs: [target]);
    final mergedKnown = mergedEpisodes
        .map((e) => PodcastKnownEpisode(
            e['episode_key'] as String,
            e['enclosure_key'] as String,
            (jsonDecode(e['aliases'] as String) as List).cast<String>()))
        .toList();
    for (final row in await tx.query('podcast_episode_state',
        where: 'feed_key=?', whereArgs: [source])) {
      final episodeKey = podcastResolveEpisode(mergedKnown,
              row['episode_key'] as String, row['enclosure_url'] as String) ??
          row['episode_key'] as String;
      await _mergeState(tx, row, row['scope'] as String, target, episodeKey,
          repushWinner: true);
    }
    await tx.delete('podcast_subscriptions',
        where: 'feed_key=?', whereArgs: [source]);
    await tx.delete('podcast_episode_state',
        where: 'feed_key=?', whereArgs: [source]);
    await tx
        .delete('podcast_episodes', where: 'feed_key=?', whereArgs: [source]);
    await tx.delete('podcast_feeds', where: 'feed_key=?', whereArgs: [source]);
  }

  Future<void> _mergeState(DatabaseExecutor tx, Map<String, Object?> row,
      String scope, String feedKey, String episodeKey,
      {bool repushWinner = false}) async {
    final old = await tx.query('podcast_episode_state',
        where: 'scope=? AND feed_key=? AND episode_key=?',
        whereArgs: [scope, feedKey, episodeKey]);
    final merged = <String, Object?>{
      ...row,
      if (old.isNotEmpty) ...old.single,
      'scope': scope,
      'feed_key': feedKey,
      'episode_key': episodeKey
    };
    for (final register in ['position', 'completion', 'deletion']) {
      final incomingClock = row['${register}_ms'] as int;
      final oldClock = old.firstOrNull?['${register}_ms'] as int? ?? 0;
      final winsDeletionTie = register == 'deletion' &&
          incomingClock > 0 &&
          incomingClock == oldClock &&
          row['deleted'] == 1;
      if (incomingClock > oldClock || winsDeletionTie) {
        for (final field in register == 'position'
            ? ['position_seconds', 'position_ms']
            : register == 'completion'
                ? ['completed', 'completion_source', 'completion_ms']
                : ['deleted', 'deletion_ms']) {
          merged[field] = row[field];
        }
        merged['${register}_pending_edit_id'] = podcastEditId();
      } else if (repushWinner &&
          row['${register}_pending_edit_id'] != null &&
          merged['${register}_pending_edit_id'] == null) {
        merged['${register}_pending_edit_id'] = podcastEditId();
      }
    }
    await tx.insert('podcast_episode_state', merged,
        conflictAlgorithm: ConflictAlgorithm.replace);
  }

  /// Whether the separate local library has registers not yet associated here.
  Future<bool> canAssociateLocal(String accountScope) async {
    final db = await database;
    return db.transaction((tx) async {
      final accounts = await tx.query('podcast_scopes',
          columns: ['associated_from_local'],
          where: 'scope=?',
          whereArgs: [accountScope]);
      if (accounts.isNotEmpty &&
          accounts.single['associated_from_local'] == 1) {
        return false;
      }
      for (final table in ['podcast_subscriptions', 'podcast_episode_state']) {
        if ((await tx.query(table,
                columns: ['scope'],
                where: 'scope=?',
                whereArgs: ['local'],
                limit: 1))
            .isNotEmpty) {
          return true;
        }
      }
      return false;
    });
  }

  /// Explicit consent only. Original clocks survive; account ties win.
  Future<void> associateLocal(String accountScope,
      {bool Function()? current}) async {
    if (!accountScope.startsWith('account:')) {
      throw ArgumentError('An account scope is required');
    }
    await (await database).transaction((tx) async {
      void checkAccount() {
        if (current != null && !current()) throw StateError('Account changed');
      }

      checkAccount();
      await tx.insert('podcast_scopes', {'scope': accountScope},
          conflictAlgorithm: ConflictAlgorithm.ignore);
      for (final row in await tx.query('podcast_subscriptions',
          where: 'scope=?', whereArgs: ['local'])) {
        final key = await resolveAccountKey(tx, accountScope, 'feed', '',
            await resolveFeed(tx, row['feed_key'] as String));
        final old = await tx.query('podcast_subscriptions',
            where: 'scope=? AND feed_key=?', whereArgs: [accountScope, key]);
        if (old.isEmpty ||
            (row['subscription_ms'] as int) >
                (old.single['subscription_ms'] as int)) {
          await tx.insert(
              'podcast_subscriptions',
              {
                ...row,
                'scope': accountScope,
                'feed_key': key,
                'pending_edit_id': podcastEditId()
              },
              conflictAlgorithm: ConflictAlgorithm.replace);
        }
      }
      for (final row in await tx.query('podcast_episode_state',
          where: 'scope=?', whereArgs: ['local'])) {
        final key = await resolveAccountKey(tx, accountScope, 'feed', '',
            await resolveFeed(tx, row['feed_key'] as String));
        final episodes = await tx
            .query('podcast_episodes', where: 'feed_key=?', whereArgs: [key]);
        final known = episodes
            .map((e) => PodcastKnownEpisode(
                e['episode_key'] as String,
                e['enclosure_key'] as String,
                (jsonDecode(e['aliases'] as String) as List).cast<String>()))
            .toList();
        final localEpisodeKey = podcastResolveEpisode(known,
                row['episode_key'] as String, row['enclosure_url'] as String) ??
            row['episode_key'] as String;
        final episodeKey = await resolveAccountKey(
            tx, accountScope, 'episode', key, localEpisodeKey);
        await _mergeState(tx, row, accountScope, key, episodeKey);
      }
      await tx.update('podcast_scopes', {'associated_from_local': 1},
          where: 'scope=?', whereArgs: [accountScope]);
      checkAccount();
    });
    onSyncNeeded?.call();
  }
}
