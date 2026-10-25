import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/models/media.dart';
import 'package:player_android/models/offline_media.dart';
import 'package:player_android/services/automatic_music.dart';
import 'package:player_android/services/offline_collection_pins.dart';
import 'package:player_android/services/offline_download_repository.dart';
import 'package:player_android/services/music_collections.dart';
import 'package:player_android/services/podcast_sync.dart';
import 'package:player_android/services/podcast_repository.dart';
import 'package:player_android/services/podcast_sync_store.dart';
import 'package:player_android/services/global_sync_coordinator.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';

void main() {
  setUpAll(() {
    sqfliteFfiInit();
    databaseFactory = databaseFactoryFfi;
  });
  late Directory dir;
  late OfflineDownloadRepository repository;
  late AutomaticMusicLibrary library;
  setUp(() async {
    dir = await Directory.systemTemp.createTemp('automatic-music-');
    repository = OfflineDownloadRepository(directory: dir);
    library = AutomaticMusicLibrary(repository);
  });
  tearDown(() async {
    await repository.close();
    await dir.delete(recursive: true);
  });
  const origin = 'http://player.test';
  Media song(int id) => Media.fromJson({
        'id': id,
        'type': 'audio',
        'file_name': '$id.mp3',
        'created_at': '2026-10-${id.toString().padLeft(2, '0')}T00:00:00Z'
      });
  Future<void> save(Media media, {bool automatic = true}) async {
    await repository.database;
    final key = offlineMediaKey(origin, 1, media.id);
    await repository.file(key).writeAsBytes([1, 2, 3]);
    await repository.add(
        OfflineMedia(
            key: key,
            origin: origin,
            userId: 1,
            mediaId: media.id,
            title: media.displayTitle,
            type: 'audio',
            sizeBytes: 3,
            addedAt: DateTime.now()),
        automaticMusic: automatic);
  }

  test(
      'all fetches complete before replacement, valid empty reconciles and retired copies survive reopen',
      () async {
    await save(song(1), automatic: false);
    await save(song(2));
    final pins = OfflineCollectionPins(repository);
    const collection = MusicCollection('playlist', 'p');
    await pins.remember(origin, 1, collection, 'old', [song(1)], () => true);
    final disk = <String, String>{};
    final service = AutomaticMusic(library,
        load: (k) async => disk[k],
        persist: (k, v) async {
          disk[k] = v;
        },
        network: () => 'wifi');
    addTearDown(service.dispose);
    var fail = true;
    var members = [song(2), song(3)];
    final account = AutomaticMusicAccount(
        origin: origin,
        userId: 1,
        current: () => true,
        read: (path) async {
          if (fail) throw StateError('offline');
          return {
            'name': 'Updated',
            'songs': members
                .map((s) =>
                    {'id': s.id, 'type': 'audio', 'file_name': s.fileName})
                .toList()
          };
        },
        save: (media, current, abort) async {
          expect(current(), true);
          if (!await library.available(origin, 1, media.id)) await save(media);
        });
    await service.bind(account.key);
    await service
        .savePolicy(const AutomaticMusicPolicy(enabled: true, playlists: true));
    await expectLater(service.sync(account, explicit: true), throwsStateError);
    expect((await pins.status(origin, 1, collection))!.mediaIds, [1]);
    expect(service.status['lastSuccess'], null);
    fail = false;
    await service.sync(account, explicit: true);
    expect((await pins.status(origin, 1, collection))!.mediaIds, [2, 3]);
    expect(await library.owned(origin, 1), containsAll([2, 3]));
    expect(await library.owned(origin, 1), isNot(contains(1)));
    expect(service.status['retired'], 0);
    members = [];
    await service.sync(account, explicit: true);
    expect((await pins.status(origin, 1, collection))!.mediaIds, isEmpty);
    expect(service.status['retired'], 2);
    expect(await repository.file(offlineMediaKey(origin, 1, 2)).readAsBytes(),
        [1, 2, 3]);
    await repository.close();
    repository = OfflineDownloadRepository(directory: dir);
    expect(await AutomaticMusicLibrary(repository).owned(origin, 1),
        containsAll([2, 3]));
    expect(
        (await OfflineCollectionPins(repository).status(origin, 1, collection))!
            .mediaIds,
        isEmpty);
    expect(await library.owned(origin, 2), isEmpty);
  });
  test(
      'network cancellation while loading does not strand settings in loading state',
      () async {
    final loaded = Completer<String?>();
    final service = AutomaticMusic(library,
        load: (k) => loaded.future,
        persist: (k, v) async {},
        network: () => 'wifi');
    addTearDown(service.dispose);
    final pending = service.bind('account');
    expect(service.loading, true);
    service.cancel();
    loaded.complete(jsonEncode({
      'policy': {'enabled': true, 'recent': true}
    }));
    await pending;
    expect(service.loading, false);
    expect(service.policy.enabled, true);
    await service.bind('account');
    expect(service.loading, false);
  });
  test(
      'cancel delayed download invalidates membership and records durable canceled status and next due',
      () async {
    final gate = Completer<void>();
    final entered = Completer<void>();
    final disk = <String, String>{};
    var alive = true;
    final service = AutomaticMusic(library,
        load: (k) async => disk[k],
        persist: (k, v) async {
          disk[k] = v;
        },
        network: () => 'wifi',
        now: () => DateTime.fromMillisecondsSinceEpoch(1000));
    addTearDown(service.dispose);
    final account = AutomaticMusicAccount(
        origin: origin,
        userId: 1,
        current: () => alive,
        read: (p) async => p.endsWith('/albums')
            ? [
                {'album_key': 'a'}
              ]
            : {
                'songs': [
                  {'id': 1, 'type': 'audio'}
                ]
              },
        save: (song, current, abort) async {
          entered.complete();
          await gate.future;
          expect(current(), false);
        });
    await service.bind(account.key);
    await service
        .savePolicy(const AutomaticMusicPolicy(enabled: true, recent: true));
    final pending = service.sync(account, explicit: true);
    await entered.future;
    service.cancel();
    gate.complete();
    await expectLater(pending, throwsStateError);
    await Future<void>.delayed(Duration.zero);
    expect(service.active, false);
    expect(service.status['message'], contains('Canceled'));
    expect(service.status['nextDue'], 21601000);
    expect((jsonDecode(disk[account.key]!) as Map)['error'], 'canceled');
    expect(await library.pins(origin, 1), isEmpty);
    alive = false;
    await service.sync(account, explicit: true);
    expect(service.active, false);
  });
  test(
      'unknown Wi-Fi refuses data, interval respects due and unsupported interval defaults',
      () async {
    var reads = 0;
    final service = AutomaticMusic(library,
        load: (k) async => null,
        persist: (k, v) async {},
        network: () => 'unknown');
    addTearDown(service.dispose);
    final account = AutomaticMusicAccount(
        origin: origin,
        userId: 1,
        current: () => true,
        read: (p) async {
          reads++;
          return [];
        },
        save: (s, c, a) async {});
    await service.bind(account.key);
    await service
        .savePolicy(const AutomaticMusicPolicy(enabled: true, recent: true));
    await expectLater(service.sync(account, explicit: true),
        throwsA(isA<AutomaticMusicPolicyWait>()));
    expect(reads, 0);
    expect(service.status['error'], 'policy');
    await expectLater(service.sync(account),
        throwsA(isA<SyncDeferred>().having((e) => e.kind, 'kind', 'policy')));
    expect(reads, 0);
    expect(AutomaticMusicPolicy.fromJson({'hours': 0}).hours, 6);
  });
  test(
      'changed/unpinned snapshot and missing saved bytes prevent atomic publication',
      () async {
    final pins = OfflineCollectionPins(repository);
    const collection = MusicCollection('playlist', 'p');
    await save(song(1));
    await pins.remember(origin, 1, collection, 'Old', [song(1)], () => true);
    final previous =
        (await library.pins(origin, 1)).single['media_ids'] as String;
    final snapshots = [
      AutomaticMusicSnapshot('p', 'New', [], previousIds: previous),
      AutomaticMusicSnapshot('@automatic-recent', 'Recent', [song(1)])
    ];
    await pins.unpin(origin, 1, collection);
    await expectLater(
        library.commit(origin, 1, snapshots, () => true), throwsStateError);
    expect((await pins.status(origin, 1, collection))!.mediaIds, [1]);
    expect((await library.pins(origin, 1)).length, 1);
    await repository.file(offlineMediaKey(origin, 1, 1)).delete();
    await expectLater(
        library.commit(
            origin,
            1,
            [
              AutomaticMusicSnapshot('@automatic-recent', 'Recent', [song(1)])
            ],
            () => true),
        throwsStateError);
  });
  test(
      'starred pagination intersects music catalog, recent is newest50, invalid responses fail closed',
      () async {
    final calls = <String>[];
    final targets = await resolveAutomaticMusic(
        const AutomaticMusicPolicy(starred: true, recent: true), [], (p) async {
      calls.add(p);
      if (p.endsWith('/albums')) {
        return [
          {'album_key': 'a'}
        ];
      }
      if (p.contains('/albums/')) {
        return {
          'songs': List.generate(
              55,
              (i) => {
                    'id': i + 1,
                    'type': 'audio',
                    'created_at': DateTime.utc(2026, 1, 1)
                        .add(Duration(days: i))
                        .toIso8601String()
                  })
        };
      }
      if (p.endsWith('offset=0')) {
        return List.generate(200, (i) => {'id': i + 1, 'type': 'audio'});
      }
      return [
        {'id': 900, 'type': 'audio'}
      ];
    }, () => true);
    expect(calls.last, contains('offset=200'));
    expect(targets.snapshots.first.songs.length, 55);
    expect(targets.snapshots.last.songs.first.id, 55);
    expect(targets.snapshots.last.songs.last.id, 6);
    expect(targets.songs.length, 55);
    await expectLater(
        resolveAutomaticMusic(const AutomaticMusicPolicy(recent: true), [],
            (p) async => {}, () => true),
        throwsStateError);
  });
  test(
      'failed music stays a global failure on not-due passes until successful retry or deliberate pause',
      () async {
    var now = 1000, reads = 0;
    var fail = false;
    final music = AutomaticMusic(library,
        load: (k) async => null,
        persist: (k, v) async {},
        network: () => 'wifi',
        now: () => DateTime.fromMillisecondsSinceEpoch(now));
    final account = AutomaticMusicAccount(
        origin: origin,
        userId: 1,
        current: () => true,
        read: (p) async {
          reads++;
          if (fail) throw StateError('network failed');
          return [];
        },
        save: (s, c, a) async {});
    await music.bind(account.key);
    await music
        .savePolicy(const AutomaticMusicPolicy(enabled: true, recent: true));
    final podcastRepo =
        PodcastRepository(databasePath: '${dir.path}/podcasts.db');
    final sync = GlobalSyncCoordinator(
        PodcastSync(PodcastSyncStore(podcastRepo)),
        () => GlobalSyncAccount(
            podcast: PodcastSyncAccount(
                scope: 'account:test',
                current: () => true,
                supported: () async => false,
                send: (_) async => {}),
            syncMedia: (_) async {},
            flushProgress: () async {},
            pendingOther: () async => 0,
            syncMusic: (explicit) => music.sync(account, explicit: explicit)),
        now: () => DateTime.fromMillisecondsSinceEpoch(now));
    addTearDown(() async {
      sync.dispose();
      music.dispose();
      await podcastRepo.close();
    });
    await sync.syncNow();
    expect(sync.state.lastFullSuccessMs, 1000);
    now = 2000;
    fail = true;
    await sync.syncNow();
    expect(sync.state.lastFullSuccessMs, 1000);
    expect(sync.state.errorKind, 'server');
    final attempted = reads;
    now = 3000;
    await sync.syncNow(explicit: false);
    expect(reads, attempted);
    expect(sync.state.lastFullSuccessMs, 1000);
    expect(sync.state.errorKind, 'server');
    fail = false;
    now = 4000;
    await sync.syncNow();
    expect(sync.state.lastFullSuccessMs, 4000);
    expect(sync.state.errorKind, null);
    await music.savePolicy(const AutomaticMusicPolicy());
    now = 5000;
    await sync.syncNow(explicit: false);
    expect(sync.state.lastFullSuccessMs, 5000);
  });
  test(
      'retained-copy capacity excludes borrowed manual downloads and explicit removal frees capacity',
      () async {
    for (var id = 1; id <= 200; id++) {
      await save(song(id));
    }
    await save(song(999), automatic: false);
    var selected = 999, saves = 0;
    final service = AutomaticMusic(library,
        load: (k) async => null,
        persist: (k, v) async {},
        network: () => 'wifi');
    addTearDown(service.dispose);
    final account = AutomaticMusicAccount(
        origin: origin,
        userId: 1,
        current: () => true,
        read: (path) async => path.endsWith('/albums')
            ? [
                {'album_key': 'a'}
              ]
            : {
                'songs': [
                  {'id': selected, 'type': 'audio'}
                ]
              },
        save: (media, current, abort) async {
          if (!await library.available(origin, 1, media.id)) {
            saves++;
            await save(media);
          }
        });
    await service.bind(account.key);
    await service
        .savePolicy(const AutomaticMusicPolicy(enabled: true, recent: true));
    await service.sync(account, explicit: true);
    expect(saves, 0);
    expect(service.status['retained'], 200);
    selected = 1000;
    await expectLater(service.sync(account, explicit: true), throwsStateError);
    expect(saves, 0);
    expect((await library.pins(origin, 1)).single['media_ids'], '[999]');
    await repository.file(offlineMediaKey(origin, 1, 1)).delete();
    await repository.remove(offlineMediaKey(origin, 1, 1));
    await service.sync(account, explicit: true);
    expect(saves, 1);
    expect((await library.owned(origin, 1)).length, 200);
    expect(await library.owned(origin, 1), isNot(contains(999)));
  });
  test(
      'restart after real unfinished checkpoint stays a global failure until retry or policy reset',
      () async {
    final disk = <String, String>{},
        entered = Completer<void>(),
        gate = Completer<void>();
    var oldAlive = true, newReads = 0;
    final oldMusic = AutomaticMusic(library,
        load: (k) async => disk[k],
        persist: (k, v) async {
          disk[k] = v;
        },
        network: () => 'wifi',
        now: () => DateTime.fromMillisecondsSinceEpoch(1000));
    final oldAccount = AutomaticMusicAccount(
        origin: origin,
        userId: 1,
        current: () => oldAlive,
        read: (p) async {
          entered.complete();
          await gate.future;
          return [];
        },
        save: (s, c, a) async {});
    await oldMusic.bind(oldAccount.key);
    await oldMusic
        .savePolicy(const AutomaticMusicPolicy(enabled: true, recent: true));
    final abandoned = oldMusic.sync(oldAccount, explicit: true);
    await entered.future;
    final checkpoint = disk[oldAccount.key]!;
    expect((jsonDecode(checkpoint) as Map)['unfinishedAttempt'], true);
    final music = AutomaticMusic(library,
        load: (k) async => disk[k],
        persist: (k, v) async {
          disk[k] = v;
        },
        network: () => 'wifi',
        now: () => DateTime.fromMillisecondsSinceEpoch(2000));
    final account = AutomaticMusicAccount(
        origin: origin,
        userId: 1,
        current: () => true,
        read: (p) async {
          newReads++;
          return [];
        },
        save: (s, c, a) async {});
    await music.bind(account.key);
    expect(music.due, false);
    expect(music.status['error'], 'interrupted');
    expect(music.status['message'], contains('interrupted'));
    final podcastRepo =
        PodcastRepository(databasePath: '${dir.path}/interrupted-podcasts.db');
    final store = PodcastSyncStore(podcastRepo);
    await store.status('account:test');
    await (await store.database).update(
        'podcast_scopes', {'last_full_success_ms': 17},
        where: 'scope=?', whereArgs: ['account:test']);
    final sync = GlobalSyncCoordinator(
        PodcastSync(store),
        () => GlobalSyncAccount(
            podcast: PodcastSyncAccount(
                scope: 'account:test',
                current: () => true,
                supported: () async => false,
                send: (_) async => {}),
            syncMedia: (_) async {},
            flushProgress: () async {},
            pendingOther: () async => 0,
            syncMusic: (explicit) => music.sync(account, explicit: explicit)),
        now: () => DateTime.fromMillisecondsSinceEpoch(2000));
    addTearDown(() async {
      sync.dispose();
      music.dispose();
      oldMusic.dispose();
      await podcastRepo.close();
    });
    await sync.syncNow(explicit: false);
    expect(newReads, 0);
    expect(sync.state.errorKind, 'interrupted');
    expect(sync.state.lastFullSuccessMs, 17);
    oldAlive = false;
    gate.complete();
    await expectLater(abandoned, throwsStateError);
    expect(disk[account.key], checkpoint,
        reason: 'obsolete old run cannot clear interrupted checkpoint');
    await sync.syncNow();
    expect(newReads, 1);
    expect(sync.state.lastFullSuccessMs, 2000);
    expect(sync.state.errorKind, null);
    expect(music.status['unfinishedAttempt'], false);
    final paused = AutomaticMusic(library,
        load: (k) async => checkpoint,
        persist: (k, v) async {},
        network: () => 'wifi');
    addTearDown(paused.dispose);
    await paused.bind(account.key);
    expect(paused.status['error'], 'interrupted');
    await paused.savePolicy(const AutomaticMusicPolicy());
    expect(paused.status['unfinishedAttempt'], false);
    expect(paused.status['error'], null);
    await paused.sync(account);
    expect(newReads, 1);
  });
}
