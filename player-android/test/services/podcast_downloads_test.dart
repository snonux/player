import 'dart:async';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';
import 'package:player_android/services/podcast_downloads.dart';
import 'package:player_android/services/podcast_repository.dart';
import 'package:player_android/services/podcast_sync_store.dart';
import 'package:player_android/services/podcast_sync.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/services/playback_session_coordinator.dart';

void main() {
  sqfliteFfiInit();
  databaseFactory = databaseFactoryFfi;
  late Directory directory;
  late HttpServer publisher;
  late StreamSubscription<HttpRequest> requests;
  late PodcastRepository repository;
  late PodcastDownloads downloads;
  late String origin, feed;
  late Map<String, Object?> episode;
  var mode = 'ok';
  var feedMoved = false;
  final paths = <String>[];
  Completer<void>? streaming;
  Completer<void>? release;

  setUp(() async {
    mode = 'ok';
    feedMoved = false;
    paths.clear();
    directory = await Directory.systemTemp.createTemp('podcast-downloads-');
    publisher = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    origin = 'http://127.0.0.1:${publisher.port}';
    requests = publisher.listen((request) async {
      paths.add(request.uri.path);
      expect(request.headers.value('authorization'), isNull);
      expect(request.headers.value('cookie'), isNull);
      expect(request.headers.value('referer'), isNull);
      final response = request.response;
      if (request.uri.path == '/feed' && feedMoved) {
        response.statusCode = 301;
        response.headers.set('location', '/target');
      } else if (request.uri.path == '/feed' || request.uri.path == '/target') {
        response.write(
            '<rss><channel><title>Podcast</title><item><title>Episode</title>'
            '<guid>one</guid><enclosure url="$origin/redirect?paid=secret"/>'
            '<duration>120</duration></item></channel></rss>');
      } else if (request.uri.path == '/redirect') {
        response.statusCode = 302;
        response.headers.set('location', '/audio?paid=secret');
        response.cookies.add(Cookie('publisher', 'must-not-replay'));
      } else if (mode == 'html') {
        response.headers.contentType = ContentType.html;
        response.write('sign in to publisher');
      } else if (mode == 'error') {
        response.statusCode = 403;
      } else {
        response.headers.contentType = ContentType('audio', 'mpeg');
        if (mode == 'truncated') {
          final socket = await response.detachSocket(writeHeaders: false);
          socket.write(
              'HTTP/1.1 200 OK\r\nContent-Type: audio/mpeg\r\nContent-Length: 100\r\nConnection: close\r\n\r\nabc');
          await socket.flush();
          await socket.close();
          return;
        }
        if (mode == 'slow') {
          response.add([1, 2, 3]);
          await response.flush();
          streaming!.complete();
          await release!.future;
        } else {
          response.contentLength = 4;
          response.add([1, 2, 3, 4]);
        }
      }
      try {
        await response.close();
      } catch (_) {}
    });
    repository =
        PodcastRepository(databasePath: '${directory.path}/podcasts.db');
    feed = await repository.subscribe('local', '$origin/feed');
    episode = (await repository.episodes(feed)).single;
    downloads = PodcastDownloads(repository, PlaybackSessionCoordinator(),
        directory: Directory('${directory.path}/audio'));
  });
  tearDown(() async {
    downloads.dispose();
    await requests.cancel();
    await publisher.close(force: true);
    await repository.close();
    await directory.delete(recursive: true);
  });

  test(
      'direct publisher copy restarts without server/login; removal retains scoped history',
      () async {
    final key = episode['episode_key'] as String;
    final store = PodcastSyncStore(repository);
    await store.saveEpisode('local', feed, key, seconds: 23, completed: true);
    await store.saveEpisode('account:https://unreachable.invalid#2', feed, key,
        seconds: 45, completed: false);
    final before =
        await (await repository.database).query('podcast_episode_state');
    await downloads.download(feed, key);
    expect(paths, ['/feed', '/redirect', '/audio']);
    final row = await downloads.copy(feed, key);
    expect(row!['state'], 'available');
    expect(row['size_bytes'], 4);
    expect(row['id'], matches(RegExp(r'^[a-f0-9]{32}$')));
    downloads.dispose();
    await repository.close();
    downloads = PodcastDownloads(repository, PlaybackSessionCoordinator(),
        directory: Directory('${directory.path}/audio'));
    final request = await downloads.playbackRequest('local', feed, episode);
    expect(request, isA<PodcastPlaybackRequest>());
    expect(request.kind, PlaybackSourceKind.local);
    expect(await File.fromUri(request.sourceUri).readAsBytes(), [1, 2, 3, 4]);
    expect(await request.readPosition!(), 23); // completion is independent
    final other = await downloads.playbackRequest(
        'account:https://unreachable.invalid#2', feed, episode);
    expect(await other.readPosition!(), 45);
    final beginning = await downloads.playbackRequest(
        'account:https://unreachable.invalid#2', feed, episode,
        fromBeginning: true);
    expect(await beginning.readPosition!(), 0);
    expect(await (await repository.database).query('podcast_episode_state'),
        before);
    await downloads.remove(feed, key);
    expect(await File.fromUri(request.sourceUri).exists(), false);
    expect((await downloads.copy(feed, key))!['state'], 'missing');
    expect(await (await repository.database).query('podcast_episode_state'),
        before);
    expect(await repository.subscriptions('local'), hasLength(1));
    expect(await repository.episodes(feed), hasLength(1));
    await downloads.download(feed, key);
    expect((await downloads.copy(feed, key))!['state'], 'available');
    expect(paths.where((path) => path.startsWith('/api')), isEmpty);
  });

  test(
      'native duration overrides feed; manual status and start-over retain clocks',
      () async {
    final key = episode['episode_key'] as String;
    await downloads.download(feed, key);
    final store = PodcastSyncStore(repository);
    await store.saveEpisode('local', feed, key, seconds: 118, completed: true);
    final request = await downloads.playbackRequest('local', feed, episode);
    expect(await request.readPosition!(), 0);
    expect(await request.readPositionForDuration!(const Duration(seconds: 300)),
        118);
    await store.saveEpisode('local', feed, key, completed: false);
    final before =
        await (await repository.database).query('podcast_episode_state');
    expect(await request.readPositionForDuration!(const Duration(seconds: 120)),
        0);
    expect(await (await repository.database).query('podcast_episode_state'),
        before);
    await request.finishAtPosition!(120);
    final ended =
        (await (await repository.database).query('podcast_episode_state'))
            .single;
    expect(ended['position_seconds'], 120);
    expect(ended['completed'], 1);
    expect(ended['completion_source'], 'auto');
    expect(ended['position_ms'], ended['completion_ms']);
    await repository.close();
    final restarted = await downloads.playbackRequest('local', feed, episode);
    expect(await restarted.readPosition!(), 0);
    final pending = (await store.payload('local'))['episodes'] as List;
    expect(pending.single['position'], isNotNull);
    expect(pending.single['completion'], isNotNull);
  });

  test(
      'bounded startup late reply preserves newer local edit and account switch',
      () async {
    final key = episode['episode_key'] as String;
    await downloads.download(feed, key);
    final store = PodcastSyncStore(repository);
    const scope = 'account:https://example.com#1';
    await store.saveEpisode(scope, feed, key, seconds: 20, observedMs: 100);
    var current = true;
    var calls = 0;
    final sent = Completer<Map<String, dynamic>>();
    final response = Completer<Map<String, dynamic>>();
    downloads.dispose();
    downloads = PodcastDownloads(repository, PlaybackSessionCoordinator(),
        directory: Directory('${directory.path}/audio'),
        captureSyncAccount: () => PodcastSyncAccount(
            scope: scope,
            current: () => current,
            supported: () async => true,
            send: (payload) {
              calls++;
              if (!sent.isCompleted) sent.complete(payload);
              return response.future;
            }));
    final request = await downloads.playbackRequest(scope, feed, episode);
    final starting = request.readPosition!();
    final payload = await sent.future;
    expect(await starting.timeout(const Duration(seconds: 5)), 20);
    await request.savePosition!(35);
    response.complete({
      'acked': PodcastSyncStore.editIds(payload).toList(),
      'episodes': [
        {
          'feed_key': feed,
          'episode_key': key,
          'position': {'seconds': 20, 'observed_ms': 100},
          'completion': {
            'completed': false,
            'source': 'manual',
            'observed_ms': 500
          }
        }
      ]
    });
    // Observe an independent server register to prove the late page applied.
    var applied = false;
    for (var attempt = 0; attempt < 100; attempt++) {
      final row =
          (await (await repository.database).query('podcast_episode_state'))
              .single;
      if (row['completion_ms'] == 500) {
        applied = true;
        break;
      }
      await Future<void>.delayed(const Duration(milliseconds: 10));
    }
    expect(applied, true);
    expect(
        (await (await repository.database).query('podcast_episode_state'))
            .single['position_seconds'],
        35);
    current = false;
    await request.synchronize!();
    final standalone = await downloads.playbackRequest('local', feed, episode);
    await standalone.readPosition!();
    expect(calls, 1);
    await request.finishAtPosition!(120);
    expect(calls, 1);
    final row =
        (await (await repository.database).query('podcast_episode_state'))
            .single;
    expect(row['scope'], scope);
    expect(row['completed'], 1);
    expect(row['completion_ms'], row['position_ms']);
  });

  test(
      'end commits before detached sync; blocked network cannot hold local audio work',
      () async {
    final key = episode['episode_key'] as String;
    await downloads.download(feed, key);
    const scope = 'account:https://example.com#1';
    final sent = Completer<void>();
    final response = Completer<Map<String, dynamic>>();
    downloads.dispose();
    downloads = PodcastDownloads(repository, PlaybackSessionCoordinator(),
        directory: Directory('${directory.path}/audio'),
        captureSyncAccount: () => PodcastSyncAccount(
            scope: scope,
            current: () => true,
            supported: () async => true,
            send: (payload) async {
              final row = (await (await repository.database)
                      .query('podcast_episode_state'))
                  .single;
              expect(row['completed'], 1);
              expect(row['position_seconds'], 120);
              expect(row['position_ms'], row['completion_ms']);
              sent.complete();
              return response.future;
            }));
    final request = await downloads.playbackRequest(scope, feed, episode);
    await request.finishAtPosition!(120).timeout(const Duration(seconds: 1));
    await sent.future;
    expect(response.isCompleted, false);
    await request.savePosition!(12).timeout(const Duration(seconds: 1));
    final row =
        (await (await repository.database).query('podcast_episode_state'))
            .single;
    expect(row['position_seconds'], 12);
    expect(row['completed'], 1);
    response.completeError(const SocketException('offline'));
    await Future<void>.delayed(Duration.zero);
  });

  test('preplay waits for its account and keeps the first valid lease',
      () async {
    final key = episode['episode_key'] as String;
    await downloads.download(feed, key);
    const scope = 'account:https://example.com#1';
    String? selectedScope;
    var current = true;
    var sent = 0;
    downloads.dispose();
    downloads = PodcastDownloads(repository, PlaybackSessionCoordinator(),
        directory: Directory('${directory.path}/audio'),
        captureSyncAccount: () => selectedScope == null
            ? null
            : PodcastSyncAccount(
                scope: selectedScope,
                current: () => current,
                supported: () async => true,
                send: (payload) async {
                  sent++;
                  expect(
                      payload['scope'], {'feed_key': feed, 'episode_key': key});
                  return {'acked': <String>[], 'episodes': <Object>[]};
                }));
    final request = await downloads.playbackRequest(scope, feed, episode);
    await request.readPositionForDuration!(const Duration(seconds: 120));
    expect(sent, 0);
    selectedScope = 'account:https://example.com#2';
    await request.readPositionForDuration!(const Duration(seconds: 120));
    expect(sent, 0, reason: 'another account must not sync this request');
    selectedScope = scope;
    await request.readPositionForDuration!(const Duration(seconds: 120));
    expect(sent, 1);
    current = false;
    await request.synchronize!();
    expect(sent, 1, reason: 'an obsolete captured lease cannot be replaced');
  });

  for (final failure in ['html', 'error', 'truncated']) {
    test('$failure never marks partial audio available; retry starts cleanly',
        () async {
      final key = episode['episode_key'] as String;
      mode = failure;
      await expectLater(downloads.download(feed, key), throwsStateError);
      expect((await downloads.copy(feed, key))!['state'], 'failed');
      expect((await downloads.copy(feed, key))!['error'],
          isNot(contains('secret')));
      expect(
          await Directory('${directory.path}/audio').list().toList(), isEmpty);
      mode = 'ok';
      await downloads.download(feed, key);
      expect((await downloads.copy(feed, key))!['state'], 'available');
    });
  }

  test('cancel cleans partial bytes and permits retry', () async {
    final key = episode['episode_key'] as String;
    mode = 'slow';
    streaming = Completer<void>();
    release = Completer<void>();
    final pending = downloads.download(feed, key);
    await streaming!.future;
    await expectLater(downloads.remove(feed, key), throwsStateError);
    downloads.cancel();
    release!.complete();
    await pending;
    expect((await downloads.copy(feed, key))!['state'], 'missing');
    expect(await Directory('${directory.path}/audio').list().toList(), isEmpty);
    mode = 'ok';
    await downloads.download(feed, key);
    expect((await downloads.copy(feed, key))!['state'], 'available');
  });

  test(
      'catalog merge during transfer retains copy and old playback callbacks resolve aliases',
      () async {
    final target = await repository.subscribe('local', '$origin/target');
    final key = episode['episode_key'] as String;
    mode = 'slow';
    streaming = Completer<void>();
    release = Completer<void>();
    final pending = downloads.download(feed, key);
    await streaming!.future;
    feedMoved = true;
    await repository.refresh(feed);
    release!.complete();
    await pending;
    final old = await downloads.copy(feed, key);
    final merged = await downloads.copy(target, key);
    expect(old!['id'], merged!['id']);
    expect(merged['feed_key'], target);
    expect(merged['state'], 'available');
    final request = await downloads.playbackRequest('local', feed, episode);
    await request.savePosition!(19);
    final states =
        await (await repository.database).query('podcast_episode_state');
    expect(states.single['feed_key'], target);
    expect(states.single['scope'], 'local');
    expect(states.single['position_seconds'], 19);
    await downloads.remove(feed, key);
    expect((await downloads.copy(target, key))!['state'], 'missing');
  });

  test(
      'removal stops the actual source before removing bytes and retains final save',
      () async {
    final playback = PlaybackSessionCoordinator();
    downloads.dispose();
    downloads = PodcastDownloads(repository, playback,
        directory: Directory('${directory.path}/audio'));
    final key = episode['episode_key'] as String;
    await downloads.download(feed, key);
    final request = await downloads.playbackRequest('local', feed, episode);
    var stopped = false;
    await playback.claim(
        kind: request.kind,
        identity: request.identity,
        sourceUri: request.sourceUri.toString(),
        stop: () async {
          expect(await File.fromUri(request.sourceUri).exists(), true);
          await request.savePosition!(31);
          stopped = true;
        });
    await downloads.remove(feed, key);
    expect(stopped, true);
    expect(playback.activeIdentity, isNull);
    final state =
        (await (await repository.database).query('podcast_episode_state'))
            .single;
    expect(state['position_seconds'], 31);
    expect(state['position_pending_edit_id'], isNotNull);
    expect(await File.fromUri(request.sourceUri).exists(), false);
  });

  test('restart marks unfinished durable copy failed and repairs missing bytes',
      () async {
    final key = episode['episode_key'] as String;
    await downloads.ready;
    const id = '0123456789abcdef0123456789abcdef';
    await (await repository.database).insert('podcast_downloads', {
      'id': id,
      'feed_key': feed,
      'episode_key': key,
      'state': 'downloading'
    });
    await File('${directory.path}/audio/$id.part').writeAsBytes([1, 2]);
    downloads.dispose();
    downloads = PodcastDownloads(repository, PlaybackSessionCoordinator(),
        directory: Directory('${directory.path}/audio'));
    expect((await downloads.copy(feed, key))!['state'], 'failed');
    expect(await Directory('${directory.path}/audio').list().toList(), isEmpty);
    await downloads.download(feed, key);
    final request = await downloads.playbackRequest('local', feed, episode);
    await File.fromUri(request.sourceUri).writeAsBytes([1]);
    expect((await downloads.copy(feed, key))!['state'], 'failed');
    await expectLater(
        downloads.playbackRequest('local', feed, episode), throwsStateError);
    await downloads.download(feed, key);
    expect((await downloads.copy(feed, key))!['state'], 'available');
  });
}
