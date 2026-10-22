import 'dart:async';
import 'package:dio/dio.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:just_audio/just_audio.dart';
import 'package:player_android/models/models.dart';
import 'package:player_android/services/audio_handler.dart';
import 'package:player_android/services/music_sequence.dart';
import 'package:player_android/services/music_queue.dart';
import 'package:player_android/services/playback_session_coordinator.dart';
import 'package:player_android/services/playback_request.dart';

class _Player extends AudioPlayer {
  final processing = StreamController<ProcessingState>.broadcast();
  final events = StreamController<PlaybackEvent>.broadcast();
  final positions = StreamController<Duration>.broadcast();
  ProcessingState nativeState = ProcessingState.ready;
  @override
  Stream<Duration> get positionStream => positions.stream;
  Duration elapsed = const Duration(seconds: 7);
  bool running = true;
  int loads = 0;
  @override
  Duration get position => elapsed;
  @override
  Duration get duration => const Duration(seconds: 10);
  @override
  bool get playing => running;
  @override
  Stream<bool> get playingStream => const Stream.empty();
  @override
  Stream<PlaybackEvent> get playbackEventStream => events.stream;
  @override
  Stream<ProcessingState> get processingStateStream => processing.stream;
  @override
  ProcessingState get processingState => nativeState;
  @override
  Future<void> play() async {
    running = true;
  }

  @override
  Future<void> pause() async {
    running = false;
  }

  @override
  Future<void> stop() async {
    running = false;
  }

  @override
  Future<Duration?> setAudioSource(AudioSource source,
      {bool preload = true,
      int? initialIndex,
      Duration? initialPosition}) async {
    loads++;
    return duration;
  }

  @override
  Future<void> seek(Duration? position, {int? index}) async {
    elapsed = position ?? Duration.zero;
  }
}

class _BrowseClient extends PlayerApiClient {
  _BrowseClient(this.data) : super(dio: Dio());
  final Map<String, Map<String, dynamic>> data;
  final calls = <String>[];
  @override
  Future<Map<String, dynamic>> browseSet(int setId, {String? parent}) async {
    calls.add(parent ?? '');
    if (!data.containsKey(parent ?? '')) throw StateError('forbidden');
    return data[parent ?? '']!;
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  for (final mode in MusicRepeat.values) {
    test(
        'repeat ${mode.name} completion and explicit transport use current occurrences',
        () async {
      final player = _Player();
      // A separate handler with the observed player owns the real sequence.
      final h = PlayerAudioHandler(player);
      final coordinator = PlaybackSessionCoordinator();
      var finishes = 0;
      PlaybackRequest song(int id) => ServerPlaybackRequest(
          mediaId: id,
          serverOrigin: 'http://local',
          userId: 1,
          title: 'Song $id',
          sourceUri: Uri.parse('http://local/$id'),
          startPosition: 0,
          savePosition: (_) async {},
          markFinished: () async {
            finishes++;
          });
      await h.insertQueue(
          entries: [song(1), song(2), song(1)],
          action: QueueInsertion.now,
          claim: (r) => coordinator.claim(
              kind: r.kind, identity: r.identity, stop: h.stop),
          load: (r, current) async => AudioSource.uri(r.sourceUri));
      while (h.musicQueue.repeat != mode) {
        h.cycleQueueRepeat();
      }
      await h.jumpQueue(2);
      final occurrence = h.musicQueue.entries[2].id;
      player.elapsed = const Duration(seconds: 10);
      player.processing.add(ProcessingState.completed);
      player.processing.add(ProcessingState.completed);
      await Future<void>.delayed(const Duration(milliseconds: 40));
      expect(finishes, 1);
      expect(h.musicQueue.index, mode == MusicRepeat.all ? 0 : 2);
      if (mode == MusicRepeat.off) {
        expect(player.running, isFalse);
        await h.play();
        player.elapsed = const Duration(seconds: 10);
        player.processing.add(ProcessingState.completed);
        await Future<void>.delayed(const Duration(milliseconds: 40));
        expect(finishes, 2,
            reason: 'Off same-source Replay starts fresh completion run');
      } else {
        expect(player.elapsed, Duration.zero);
      }
      if (mode == MusicRepeat.one) {
        expect(h.musicQueue.entries[h.musicQueue.index].id, occurrence);
        await h.skipToNext();
        expect(h.musicQueue.index, 2);
        await h.skipToPrevious();
        expect(h.musicQueue.index, 1);
        await h.insertQueue(
            entries: [song(3), song(4)],
            action: QueueInsertion.next,
            claim: (r) => coordinator.claim(
                kind: r.kind, identity: r.identity, stop: h.stop),
            load: (r, current) async => AudioSource.uri(r.sourceUri));
        final next = Set<int>.of(h.musicQueue.upNextIds);
        player.processing.add(ProcessingState.completed);
        await Future<void>.delayed(const Duration(milliseconds: 40));
        expect(h.musicQueue.upNextIds, next);
        h.moveQueue(1, 1);
        final selected = h.musicQueue.entries[h.musicQueue.index].id;
        player.processing.add(ProcessingState.completed);
        await Future<void>.delayed(const Duration(milliseconds: 40));
        expect(h.musicQueue.entries[h.musicQueue.index].id, selected);
      }
      await h.clearQueue();
      await h.undoQueue();
      expect(h.musicQueue.repeat, mode);
      h.clearRetainedQueue();
      expect(h.musicQueue.repeat, MusicRepeat.off);
      await h.stop();
    });
  }

  test('deferred completion rejects same-source seek and replacement epoch',
      () async {
    final player = _Player();
    final handler = PlayerAudioHandler(player);
    final gate = Completer<void>();
    var finishes = 0;
    final request = ServerPlaybackRequest(
        mediaId: 1,
        serverOrigin: 'http://local',
        userId: 1,
        title: 'Long',
        sourceUri: Uri.parse('http://local/1'),
        startPosition: 0,
        savePosition: (_) => gate.future,
        markFinished: () async {
          finishes++;
        });
    handler.activateSourceSession(handler.beginSourceSession());
    handler.startProgress(
        savePosition: request.savePosition!,
        markFinished: request.markFinished!);
    handler.startMusicSequence(
        wrap: false,
        entries: [request],
        index: 0,
        expand: () async => [request],
        changed: (_) {},
        load: (r, current) async => AudioSource.uri(r.sourceUri));
    handler.cycleQueueRepeat();
    handler.cycleQueueRepeat();
    player.processing.add(ProcessingState.completed);
    await Future<void>.delayed(const Duration(milliseconds: 10));
    await handler.seekForSession(
        const Duration(seconds: 2), handler.musicState!.sourceGeneration);
    gate.complete();
    await Future<void>.delayed(const Duration(milliseconds: 40));
    expect(player.loads, 0,
        reason: 'seeking invalidates pending natural completion');
    expect(finishes, 1);
    await handler.stop();
  });

  test(
      'repeat One failed reload stays retryable and source takeover cancels pending completion',
      () async {
    final player = _Player();
    final handler = PlayerAudioHandler(player),
        coordinator = PlaybackSessionCoordinator();
    var fail = false;
    final gate = Completer<void>();
    PlaybackRequest song(int id) => ServerPlaybackRequest(
        mediaId: id,
        serverOrigin: 'http://local',
        userId: 1,
        title: 'Song $id',
        sourceUri: Uri.parse('http://local/$id'),
        startPosition: 0,
        savePosition: (_) async {},
        markFinished: () async {});
    await handler.insertQueue(
        entries: [song(1), song(2)],
        action: QueueInsertion.now,
        claim: (r) => coordinator.claim(
            kind: r.kind, identity: r.identity, stop: handler.stop),
        load: (r, current) async {
          if (fail) throw StateError('offline');
          return AudioSource.uri(r.sourceUri);
        });
    handler.cycleQueueRepeat();
    handler.cycleQueueRepeat();
    fail = true;
    player.processing.add(ProcessingState.completed);
    await Future<void>.delayed(const Duration(milliseconds: 40));
    expect(handler.musicState!.error, isNotNull);
    expect(player.running, isFalse);
    expect(handler.musicQueue.index, 0);
    fail = false;
    await handler.retryMusicTrack();
    expect(player.running, isTrue);
    expect(player.elapsed, Duration.zero);
    await handler.endProgress();
    handler.startProgress(
        savePosition: (_) => gate.future, markFinished: () async {});
    player.processing.add(ProcessingState.completed);
    await Future<void>.delayed(const Duration(milliseconds: 10));
    final loads = player.loads;
    handler.beginSourceSession();
    gate.complete();
    await Future<void>.delayed(const Duration(milliseconds: 40));
    expect(player.loads, loads);
    expect(handler.musicState, isNull);
    expect(handler.musicQueue.active, isFalse);
    expect(handler.musicQueue.repeat, MusicRepeat.one);
    await handler.stop();
  });

  test(
      'collection replacement starts first and one Undo restores original occurrences and label',
      () async {
    final handler = PlayerAudioHandler(_Player());
    final coordinator = PlaybackSessionCoordinator();
    PlaybackRequest song(int id) => ServerPlaybackRequest(
        mediaId: id,
        serverOrigin: 'http://local',
        userId: 1,
        title: 'Song $id',
        sourceUri: Uri.parse('http://local/$id'),
        startPosition: 0);
    Future<PlaybackSessionLease?> claim(PlaybackRequest r) => coordinator.claim(
        kind: r.kind, identity: r.identity, stop: handler.stop);
    Future<AudioSource?> load(
            PlaybackRequest r, bool Function() current) async =>
        AudioSource.uri(r.sourceUri);
    await handler.insertQueue(
        entries: [song(1), song(2)],
        action: QueueInsertion.append,
        claim: claim,
        load: load);
    final before = handler.musicQueue.entries.map((e) => e.id).toList();
    expect(handler.musicQueue.active, isFalse);
    await handler.insertQueue(
        entries: [song(3), song(4), song(3)],
        action: QueueInsertion.now,
        replace: true,
        sourceLabel: 'Chosen playlists',
        claim: claim,
        load: load);
    expect(handler.musicQueue.entries.map((e) => e.request.title),
        ['Song 3', 'Song 4', 'Song 3']);
    expect(handler.musicQueue.index, 0);
    expect(handler.musicQueue.label, 'Chosen playlists');
    expect(handler.musicState?.request.title, 'Song 3');
    await handler.undoQueue(expected: handler.queueUndoToken);
    expect(handler.musicQueue.entries.map((e) => e.id), before);
    expect(handler.musicQueue.active, isFalse);
    await handler.stop();
  });
  test('music classification uses source filenames, paths and sets, not titles',
      () {
    Media media(String file,
            {String path = '', int set = 1, double duration = 12}) =>
        Media.fromJson({
          'id': 1,
          'set_id': set,
          'type': 'audio',
          'file_name': file,
          'rel_path': path,
          'duration': duration,
          'music': {'title': 'The Audio Book Song'}
        });
    final sets = [
      MediaSet.fromJson({'id': 1, 'name': 'Music'}),
      MediaSet.fromJson({'id': 2, 'name': 'Books', 'is_podcast': true}),
      MediaSet.fromJson({'id': 3, 'name': 'Audio Books'})
    ];
    expect(isMusicTrack(media('song.flac'), sets), isTrue);
    expect(isMusicTrack(media('symphony.flac', duration: 4000), sets), isTrue);
    expect(isMusicTrack(media('BOOK.M4B'), sets), isFalse);
    expect(
        isMusicTrack(
            media('chapter.mp3', path: 'audio_books/chapter.mp3'), sets),
        isFalse);
    expect(isMusicTrack(media('chapter.mp3', set: 3), sets), isFalse);
    expect(isMusicTrack(media('episode.mp3', set: 2), sets), isFalse);
  });

  for (final complete in [false, true]) {
    test(
        'track transition flushes ${complete ? 'completion' : 'partial progress'} before replacing source, resumes and wraps',
        () async {
      final player = _Player();
      final handler = PlayerAudioHandler(player);
      final events = <String>[];
      PlaybackRequest request(int id) => ServerPlaybackRequest(
          mediaId: id,
          serverOrigin: 'http://local',
          userId: 1,
          sourceUri: Uri.parse('http://local/$id'),
          title: 'Song $id',
          readPosition: () async => id == 2 ? 3 : 0,
          savePosition: (position) async {
            events.add('save$id:$position');
          },
          markFinished: () async {
            events.add('finish$id');
          });
      final entries = [request(1), request(2), request(3)];
      final generation = handler.beginSourceSession();
      handler.activateSourceSession(generation);
      handler.startProgress(
          savePosition: entries[0].savePosition!,
          markFinished: entries[0].markFinished!);
      handler.startMusicSequence(
          entries: entries,
          index: 0,
          expand: () async => entries,
          changed: (r) => events.add('changed${r.title}'),
          load: (r, current) async {
            events.add('load${r.title}');
            return AudioSource.uri(r.sourceUri);
          });
      if (complete) {
        player.elapsed = const Duration(seconds: 10);
        player.processing.add(ProcessingState.completed);
        await Future<void>.delayed(const Duration(milliseconds: 30));
      } else {
        await handler.skipToNext();
      }
      expect(handler.musicState?.request.identity, entries[1].identity);
      expect(events.indexWhere((e) => e.startsWith('save1:')),
          lessThan(events.indexOf('loadSong 2')));
      if (complete) {
        expect(
            events.indexOf('finish1'), lessThan(events.indexOf('loadSong 2')));
      }
      expect(player.elapsed, const Duration(seconds: 3));
      await handler.skipToPrevious();
      expect(handler.musicState?.request.identity, entries[0].identity);
      await handler.skipToPrevious();
      expect(handler.musicState?.request.identity, entries[2].identity);
      await handler.skipToNext();
      expect(handler.musicState?.request.identity, entries[0].identity);
      await handler.stop();
      await player.processing.close();
    });
  }

  test(
      'album boundaries preserve active progress; natural end stops without wrap',
      () async {
    final player = _Player();
    final handler = PlayerAudioHandler(player);
    final saves = <String>[];
    var completions = 0;
    var expansions = 0;
    final entries = [1, 2]
        .map((id) => ServerPlaybackRequest(
              mediaId: id,
              serverOrigin: 'http://local',
              userId: 1,
              title: 'Song $id',
              sourceUri: Uri.parse('http://local/$id'),
              startPosition: 0,
              readPosition: () async => 9,
              savePosition: (position) async {
                saves.add('$id:$position');
              },
              markFinished: () async {
                completions++;
              },
            ))
        .toList();
    handler.activateSourceSession(handler.beginSourceSession());
    handler.startProgress(
        savePosition: entries.first.savePosition!,
        markFinished: entries.first.markFinished!);
    handler.startMusicSequence(
        wrap: false,
        entries: entries,
        index: 0,
        expand: () async {
          expansions++;
          return entries;
        },
        changed: (_) {},
        load: (request, current) async => AudioSource.uri(request.sourceUri));
    await handler.skipToPrevious();
    expect(player.running, isTrue);
    await handler.seek(const Duration(seconds: 4));
    await handler.saveProgress();
    expect(saves, contains('1:4.0'));
    await handler.skipToNext();
    expect(player.position, Duration.zero);
    expect(handler.musicState?.request.identity, entries.last.identity);
    final loads = player.loads;
    await handler.skipToNext();
    expect(player.loads, loads);
    expect(player.running, isTrue);
    await handler.seek(const Duration(seconds: 5));
    await handler.saveProgress();
    expect(saves, contains('2:5.0'));
    player.elapsed = const Duration(seconds: 10);
    player.processing.add(ProcessingState.completed);
    await Future<void>.delayed(const Duration(milliseconds: 30));
    expect(player.running, isFalse);
    expect(expansions, 0);
    expect(player.loads, loads);
    expect(handler.musicState?.request.identity, entries.last.identity);
    expect(completions, 1);
    await handler.play();
    expect(player.position, Duration.zero);
    await handler.seek(const Duration(seconds: 3));
    await handler.saveProgress();
    expect(saves, contains('2:3.0'));
    player.elapsed = const Duration(seconds: 10);
    player.processing.add(ProcessingState.completed);
    await Future<void>.delayed(const Duration(milliseconds: 30));
    expect(completions, 2);
    await handler.seek(const Duration(seconds: 2));
    await handler.play();
    expect(player.position, const Duration(seconds: 2));
    await handler.seek(const Duration(seconds: 4));
    await handler.saveProgress();
    expect(saves, contains('2:4.0'));
    await handler.stop();
    await player.processing.close();
    await player.events.close();
    await player.positions.close();
  });

  test(
      'native Play after a failed album transition cannot start target progress on an old source',
      () async {
    final player = _Player();
    final handler = PlayerAudioHandler(player);
    final saves = <int>[];
    final entries = [1, 2]
        .map((id) => ServerPlaybackRequest(
            mediaId: id,
            serverOrigin: 'http://local',
            userId: 1,
            title: 'Song $id',
            sourceUri: Uri.parse('http://local/$id'),
            savePosition: (_) async {
              saves.add(id);
            },
            markFinished: () async {}))
        .toList();
    handler.activateSourceSession(handler.beginSourceSession());
    handler.startProgress(
        savePosition: entries.first.savePosition!,
        markFinished: entries.first.markFinished!);
    handler.startMusicSequence(
        wrap: false,
        entries: entries,
        index: 0,
        expand: () async => entries,
        changed: (_) {},
        load: (_, __) async {
          throw StateError('denied before source replacement');
        });
    await handler.skipToNext();
    expect(handler.musicState?.request.identity, entries.last.identity);
    expect(handler.musicState?.error, isStateError);
    expect(player.loads, 0);
    await handler.play();
    expect(player.running, isFalse);
    await handler.seek(const Duration(seconds: 4));
    await handler.saveProgress();
    expect(saves, isNot(contains(2)));
    await handler.stop();
    await player.processing.close();
    await player.events.close();
    await player.positions.close();
  });

  test('stop invalidates pending track preparation and rapid skips do not race',
      () async {
    final player = _Player();
    final handler = PlayerAudioHandler(player);
    final gate = Completer<AudioSource?>();
    PlaybackRequest request(int id) => ServerPlaybackRequest(
        mediaId: id,
        serverOrigin: 'http://local',
        userId: 1,
        sourceUri: Uri.parse('http://local/$id'),
        title: 'Song $id',
        savePosition: (_) async {},
        markFinished: () async {});
    final entries = [request(1), request(2)];
    handler.activateSourceSession(handler.beginSourceSession());
    handler.startMusicSequence(
        entries: entries,
        index: 0,
        expand: () async => entries,
        changed: (_) {},
        load: (_, current) => gate.future);
    final moving = handler.skipToNext();
    await Future<void>.delayed(Duration.zero);
    await handler.skipToNext();
    await handler.stop();
    gate.complete(AudioSource.uri(Uri.parse('http://local/2')));
    await moving;
    expect(player.loads, 0);
    expect(player.running, isFalse);
    expect(handler.musicState, isNull);
    await player.processing.close();
  });
  test(
      'folder expansion preserves folders-first order, empty folders and rejects denied children',
      () async {
    Map<String, dynamic> row(int id) =>
        {'id': id, 'set_id': 1, 'type': 'audio'};
    final client = _BrowseClient({
      '': {
        'folders': [
          {'name': 'A'},
          {'name': 'Empty'}
        ],
        'media': [row(4)]
      },
      'A': {
        'current_path': 'A',
        'folders': [
          {'name': 'Disc'}
        ],
        'media': [row(2), row(3)]
      },
      'A/Disc': {
        'media': [row(1)]
      },
      'Empty': {},
    });
    expect((await browseMusicSet(client, 1)).map((m) => m.id), [1, 2, 3, 4]);
    expect(client.calls, ['', 'A', 'A/Disc', 'Empty']);
    expect(await browseMusicSet(_BrowseClient({'': {}}), 1), isEmpty);
    client.data.remove('A/Disc');
    await expectLater(browseMusicSet(client, 1), throwsStateError);
  });

  test('failed track remains visible and Retry reloads that same track',
      () async {
    final player = _Player();
    final handler = PlayerAudioHandler(player);
    final errors = <Object>[];
    final subscription = handler.playbackErrors.listen(errors.add);
    var fail = true;
    PlaybackRequest request(int id) => ServerPlaybackRequest(
        mediaId: id,
        serverOrigin: 'http://local',
        userId: 1,
        sourceUri: Uri.parse('http://local/$id'),
        title: 'Song $id',
        savePosition: (_) async {},
        markFinished: () async {});
    final entries = [request(1), request(2), request(3)];
    handler.activateSourceSession(handler.beginSourceSession());
    handler.startMusicSequence(
        entries: entries,
        index: 0,
        expand: () async => entries,
        changed: (_) {},
        load: (request, current) async {
          if (fail) throw StateError('forbidden');
          return AudioSource.uri(request.sourceUri);
        });
    await handler.skipToNext();
    await Future<void>.delayed(Duration.zero);
    expect(handler.musicState?.request.identity, entries[1].identity);
    expect(handler.musicState?.error, isStateError);
    expect(handler.musicState?.loading, isFalse);
    expect(errors, hasLength(1));
    expect(player.running, isFalse);
    fail = false;
    await handler.retryMusicTrack();
    expect(handler.musicState?.request.identity, entries[1].identity);
    expect(handler.musicState?.error, isNull);
    expect(player.running, isTrue);
    expect(player.loads, 1);
    await handler.stop();
    await subscription.cancel();
    await player.processing.close();
  });
  test(
      'background runtime failure retries current B at its position rather than original A',
      () async {
    final player = _Player();
    final handler = PlayerAudioHandler(player);
    var failThird = false;
    final entries = [1, 2, 3]
        .map((id) => ServerPlaybackRequest(
            mediaId: id,
            serverOrigin: 'http://local',
            userId: 1,
            title: 'Song $id',
            sourceUri: Uri.parse('http://local/$id'),
            readPosition: () async => 1,
            savePosition: (_) async {},
            markFinished: () async {}))
        .toList();
    handler.activateSourceSession(handler.beginSourceSession());
    handler.startMusicSequence(
        entries: entries,
        index: 0,
        expand: () async => entries,
        changed: (_) {},
        load: (request, current) async {
          if (failThird && request.title == 'Song 3') {
            throw StateError('third unavailable');
          }
          return AudioSource.uri(request.sourceUri);
        });
    await handler.skipToNext();
    player.positions.add(const Duration(seconds: 7));
    await Future<void>.delayed(Duration.zero);
    player.nativeState = ProcessingState.idle;
    player.elapsed = Duration.zero;
    player.events.addError(StateError('late source failure'));
    await Future<void>.delayed(Duration.zero);
    expect(handler.musicState?.request.identity, entries[1].identity);
    expect(handler.musicState?.error, isStateError);
    player.elapsed = Duration.zero;
    await handler.retryMusicTrack();
    expect(handler.musicState?.request.identity, entries[1].identity);
    expect(player.position, const Duration(seconds: 7));
    player.nativeState = ProcessingState.ready;
    player.elapsed = const Duration(seconds: 8);
    player.events.addError(StateError('B failed again'));
    await Future<void>.delayed(Duration.zero);
    failThird = true;
    await handler.skipToNext();
    expect(handler.musicState?.request.identity, entries[2].identity);
    expect(handler.musicState?.error, isStateError);
    failThird = false;
    await handler.retryMusicTrack();
    expect(player.position, const Duration(seconds: 1),
        reason: 'C must not inherit failed B position');
    await handler.stop();
    await player.processing.close();
    await player.events.close();
    await player.positions.close();
  });

  test(
      'Retry retains failed folder-edge navigation rather than replaying current',
      () async {
    final player = _Player();
    final handler = PlayerAudioHandler(player);
    final entries = [1, 2]
        .map((id) => ServerPlaybackRequest(
            mediaId: id,
            serverOrigin: 'http://local',
            userId: 1,
            title: 'Song $id',
            sourceUri: Uri.parse('http://local/$id'),
            savePosition: (_) async {},
            markFinished: () async {}))
        .toList();
    var denied = true;
    var expansions = 0;
    handler.activateSourceSession(handler.beginSourceSession());
    handler.startMusicSequence(
        entries: [entries.first],
        index: 0,
        expand: () async {
          expansions++;
          if (denied) throw StateError('forbidden');
          return entries;
        },
        changed: (_) {},
        load: (request, current) async => AudioSource.uri(request.sourceUri));
    await handler.skipToNext();
    expect(handler.musicState?.error, isStateError);
    expect(handler.musicState?.request.identity, entries.first.identity);
    denied = false;
    await handler.retryMusicTrack();
    expect(expansions, 2);
    expect(handler.musicState?.request.identity, entries[1].identity);
    expect(handler.musicState?.error, isNull);
    await handler.stop();
    await player.processing.close();
    await player.events.close();
  });
  for (final mode in ['jump', 'backward', 'natural']) {
    test(
        'Undo cannot resurrect FIFO invalidated by $mode; natural advance retains pending order',
        () async {
      final p = _Player();
      final handler = PlayerAudioHandler(p),
          coordinator = PlaybackSessionCoordinator();
      PlaybackRequest request(int id) => ServerPlaybackRequest(
          mediaId: id,
          serverOrigin: 'http://local',
          userId: 1,
          title: 'Song $id',
          sourceUri: Uri.parse('http://local/$id'),
          startPosition: 0,
          savePosition: (_) async {},
          markFinished: () async {});
      Future<PlaybackSessionLease?> claim(PlaybackRequest r) => coordinator
          .claim(kind: r.kind, identity: r.identity, stop: handler.stop);
      Future<AudioSource?> load(
              PlaybackRequest r, bool Function() current) async =>
          AudioSource.uri(r.sourceUri);
      Future<bool> insert(List<int> ids, QueueInsertion action) =>
          handler.insertQueue(
              entries: ids.map(request).toList(),
              action: action,
              claim: claim,
              load: load);
      await insert([1, 2], QueueInsertion.now);
      await insert([3, 4], QueueInsertion.next);
      if (mode == 'backward') await handler.skipToNext();
      await insert([5], QueueInsertion.append);
      if (mode == 'jump') await handler.jumpQueue(0);
      if (mode == 'backward') await handler.skipToPrevious();
      if (mode == 'natural') await handler.skipToNext();
      await handler.undoQueue();
      await insert([6], QueueInsertion.next);
      expect(
          handler.musicQueue.entries.map((e) => e.request.title).toList(),
          mode == 'natural'
              ? ['Song 1', 'Song 3', 'Song 4', 'Song 6', 'Song 2']
              : ['Song 1', 'Song 6', 'Song 3', 'Song 4', 'Song 2']);
      expect(
          handler.musicQueue.entries
              .where((e) => handler.musicQueue.upNextIds.contains(e.id))
              .map((e) => e.request.title)
              .toList(),
          mode == 'natural' ? ['Song 4', 'Song 6'] : ['Song 6']);
      await handler.stop();
      await p.processing.close();
      await p.events.close();
      await p.positions.close();
    });
  }
  test('superseded Play now cannot confirm or capture a newer scope Undo',
      () async {
    final p = _Player(),
        entered = Completer<void>(),
        release = Completer<void>();
    final h = PlayerAudioHandler(p), coordinator = PlaybackSessionCoordinator();
    PlaybackRequest request(int id) => ServerPlaybackRequest(
        mediaId: id,
        serverOrigin: 'http://local',
        userId: 1,
        title: 'Song $id',
        sourceUri: Uri.parse('http://local/$id'),
        startPosition: 0,
        savePosition: (_) async {},
        markFinished: () async {});
    Future<PlaybackSessionLease?> claim(PlaybackRequest r) =>
        coordinator.claim(kind: r.kind, identity: r.identity, stop: h.stop);
    Future<AudioSource?> load(
        PlaybackRequest r, bool Function() current) async {
      if (r.title == 'Song 1') {
        entered.complete();
        await release.future;
      }
      return AudioSource.uri(r.sourceUri);
    }

    final pending = h.insertQueue(
        entries: [request(1)],
        action: QueueInsertion.now,
        claim: claim,
        load: load);
    await entered.future;
    h.clearRetainedQueue();
    await h.insertQueue(
        entries: [request(2)],
        action: QueueInsertion.next,
        claim: claim,
        load: load);
    final token = h.queueUndoToken;
    release.complete();
    expect(await pending, isFalse);
    expect(h.queueUndoToken, same(token));
    expect(h.musicQueue.entries.single.request.title, 'Song 2');
    await h.stop();
    await p.processing.close();
    await p.events.close();
    await p.positions.close();
  });
  test(
      'explicit insertion preserves position, FIFO and duplicate occurrences; Undo current stops',
      () async {
    final p = _Player(), saves = <String>[];
    final handler = PlayerAudioHandler(p),
        coordinator = PlaybackSessionCoordinator();
    PlaybackRequest request(int id) => ServerPlaybackRequest(
        mediaId: id,
        serverOrigin: 'http://local',
        userId: 1,
        title: 'Song $id',
        sourceUri: Uri.parse('http://local/$id'),
        startPosition: 0,
        savePosition: (seconds) async {
          saves.add('$id:$seconds');
        },
        markFinished: () async {});
    Future<PlaybackSessionLease?> claim(PlaybackRequest r) => coordinator.claim(
        kind: r.kind, identity: r.identity, stop: handler.stop);
    Future<AudioSource?> load(
            PlaybackRequest r, bool Function() current) async =>
        AudioSource.uri(r.sourceUri);
    Future<bool> insert(List<int> ids, QueueInsertion action) =>
        handler.insertQueue(
            entries: ids.map(request).toList(),
            action: action,
            claim: claim,
            load: load);
    List<String> order() =>
        handler.musicQueue.entries.map((e) => e.request.title).toList();
    final initial = [request(1), request(2)],
        lease = (await claim(request(1)))!;
    handler.activateSourceSession(
        handler.beginSourceSession(ownsSource: () => lease.isCurrent));
    handler.startProgress(
        savePosition: initial.first.savePosition!,
        markFinished: initial.first.markFinished!);
    handler.startMusicSequence(
        wrap: false,
        entries: initial,
        index: 0,
        claimQueue: claim,
        changed: lease.updateSource,
        expand: () async => initial,
        load: load);
    final current = handler.musicQueue.entries.first.id;
    await insert([3, 4], QueueInsertion.next);
    await insert([3], QueueInsertion.next);
    await insert([5], QueueInsertion.append);
    expect(
        order(), ['Song 1', 'Song 3', 'Song 4', 'Song 3', 'Song 2', 'Song 5']);
    expect(handler.musicQueue.entries.map((e) => e.id).toSet().length, 6);
    expect(handler.musicQueue.entries.first.id, current);
    expect(p.loads, 0);
    expect(p.elapsed, const Duration(seconds: 7));
    expect(handler.musicQueue.upNextIds.length, 3);
    await handler.skipToNext();
    await insert([6], QueueInsertion.next);
    expect(order(),
        ['Song 1', 'Song 3', 'Song 4', 'Song 3', 'Song 6', 'Song 2', 'Song 5']);
    expect(handler.musicQueue.upNextIds.length, 3);
    await handler.skipToPrevious();
    await insert([7], QueueInsertion.next);
    expect(order().take(3), ['Song 1', 'Song 7', 'Song 3']);
    final before = order();
    await insert([8], QueueInsertion.now);
    expect(handler.musicState?.request.title, 'Song 8');
    p.elapsed = const Duration(seconds: 4);
    await handler.undoQueue();
    expect(order(), before);
    expect(p.running, isFalse);
    expect(saves, contains('8:4.0'));
    final sets = request(99), setsLease = (await claim(request(99)))!;
    handler.activateSourceSession(
        handler.beginSourceSession(ownsSource: () => setsLease.isCurrent));
    handler.startProgress(
        savePosition: sets.savePosition!, markFinished: sets.markFinished!);
    handler.startMusicSequence(
        entries: [sets],
        index: 0,
        changed: setsLease.updateSource,
        expand: () async => [sets],
        load: load);
    p.running = true;
    p.elapsed = const Duration(seconds: 5);
    final loads = p.loads;
    await insert([9], QueueInsertion.append);
    expect(handler.musicState?.request.title, 'Song 99');
    expect(p.elapsed, const Duration(seconds: 5));
    expect(p.loads, loads);
    final token = handler.queueUndoToken;
    handler.moveQueue(0, 1);
    final moved = order();
    await handler.undoQueue(expected: token);
    expect(order(), moved);
    await handler.clearQueue();
    await insert([10], QueueInsertion.next);
    await insert([11], QueueInsertion.next);
    expect(order(), ['Song 10', 'Song 11']);
    expect(handler.musicQueue.active, isFalse);
    expect(p.loads, loads);
    handler.clearRetainedQueue();
    expect(handler.musicQueue.label, isEmpty);
    expect(handler.musicQueue.upNextIds, isEmpty);
    expect(handler.queueUndoToken, isNull);
    await handler.stop();
    await p.processing.close();
    await p.events.close();
    await p.positions.close();
  });
  for (final replace in [false, true]) {
    test(
        'Undo inserted current cannot restore after delayed stop and ${replace ? 'new album' : 'scope clear'}',
        () async {
      final p = _Player(),
          entered = Completer<void>(),
          release = Completer<void>();
      final handler = PlayerAudioHandler(p),
          coordinator = PlaybackSessionCoordinator();
      PlaybackRequest request(int id) => ServerPlaybackRequest(
          mediaId: id,
          serverOrigin: 'http://local',
          userId: 1,
          title: 'Song $id',
          sourceUri: Uri.parse('http://local/$id'),
          startPosition: 0,
          savePosition: (seconds) async {
            if (id == 2) {
              if (!entered.isCompleted) entered.complete();
              await release.future;
            }
          },
          markFinished: () async {});
      Future<PlaybackSessionLease?> claim(PlaybackRequest r) => coordinator
          .claim(kind: r.kind, identity: r.identity, stop: handler.stop);
      Future<AudioSource?> load(
              PlaybackRequest r, bool Function() current) async =>
          AudioSource.uri(r.sourceUri);
      await handler.insertQueue(
          entries: [request(1)],
          action: QueueInsertion.next,
          claim: claim,
          load: load);
      await handler.insertQueue(
          entries: [request(2)],
          action: QueueInsertion.now,
          claim: claim,
          load: load);
      final pending = handler.undoQueue();
      await entered.future;
      if (replace) {
        final fresh = [request(99)];
        handler.activateSourceSession(handler.beginSourceSession());
        handler.startMusicSequence(
            wrap: false,
            entries: fresh,
            index: 0,
            changed: (_) {},
            expand: () async => fresh,
            load: load);
        p.running = true;
      } else {
        handler.clearRetainedQueue();
      }
      release.complete();
      await pending;
      expect(handler.musicQueue.entries.map((e) => e.request.title).toList(),
          replace ? ['Song 99'] : isEmpty);
      expect(handler.musicQueue.canUndo, isFalse);
      if (replace) expect(p.running, isTrue);
      await handler.stop();
      await p.processing.close();
      await p.events.close();
      await p.positions.close();
    });
  }
  test(
      'editable queue keeps occurrence IDs, flushes removed current and Undo stays paused',
      () async {
    final player = _Player();
    // Use the exact injected source for source/position assertions.
    final h = PlayerAudioHandler(player);
    final coordinator = PlaybackSessionCoordinator();
    final events = <String>[];
    PlaybackRequest request(int id) => ServerPlaybackRequest(
        mediaId: id,
        serverOrigin: 'http://local',
        userId: 1,
        sourceUri: Uri.parse('http://local/$id'),
        title: 'Song $id',
        startPosition: 0,
        savePosition: (p) async {
          events.add('save$id:$p');
        },
        markFinished: () async {});
    final entries = [request(1), request(2), request(1)];
    Future<PlaybackSessionLease?> claim(PlaybackRequest r) =>
        coordinator.claim(kind: r.kind, identity: r.identity, stop: h.stop);
    final lease = (await claim(entries.first))!;
    h.activateSourceSession(
        h.beginSourceSession(ownsSource: () => lease.isCurrent));
    h.startProgress(
        savePosition: entries.first.savePosition!,
        markFinished: entries.first.markFinished!);
    h.startMusicSequence(
        wrap: false,
        entries: entries,
        index: 0,
        queueLabel: 'Album',
        claimQueue: claim,
        expand: () async => entries,
        changed: lease.updateSource,
        load: (r, current) async {
          events.add('load${r.title}');
          return AudioSource.uri(r.sourceUri);
        });
    final first = h.musicQueue.entries.first.id;
    h.moveQueue(0, 1);
    expect(h.musicQueue.entries[1].id, first);
    expect(h.musicQueue.index, 1);
    expect(player.loads, 0);
    expect(player.elapsed, const Duration(seconds: 7));
    await h.removeQueue(2);
    expect(h.musicQueue.active, isTrue);
    expect(player.running, isTrue);
    h.undoQueue();
    await h.removeQueue(1);
    expect(player.running, isFalse);
    expect(events, contains('save1:7.0'));
    h.undoQueue();
    expect(h.musicQueue.entries[1].id, first);
    expect(player.running, isFalse);
    await h.jumpQueue(1);
    expect(player.running, isTrue);
    expect(h.musicState?.request.identity, entries.first.identity);
    // Same-account Sets takeover retains contents, but edits cannot touch it.
    final sets = request(99);
    final setsLease = (await claim(sets))!;
    h.activateSourceSession(
        h.beginSourceSession(ownsSource: () => setsLease.isCurrent));
    h.startProgress(
        savePosition: sets.savePosition!, markFinished: sets.markFinished!);
    h.startMusicSequence(
        entries: [sets],
        index: 0,
        expand: () async => [sets],
        changed: setsLease.updateSource,
        load: (r, c) async => AudioSource.uri(r.sourceUri));
    player.running = true;
    player.elapsed = const Duration(seconds: 5);
    final count = events.length, loads = player.loads;
    await h.removeQueue(1);
    h.undoQueue();
    await h.clearQueue();
    h.undoQueue();
    expect(player.running, isTrue);
    expect(player.elapsed, const Duration(seconds: 5));
    expect(player.loads, loads);
    expect(events.length, count);
    expect(h.musicState?.request.identity, sets.identity);
    await h.jumpQueue(0);
    expect(events, contains('save99:5.0'));
    expect(h.musicState?.request.identity, entries[1].identity);
    await h.clearQueue();
    expect(h.musicQueue.entries, isEmpty);
    h.undoQueue();
    expect(h.musicQueue.entries.length, 3);
    expect(player.running, isFalse);
    h.clearRetainedQueue();
    expect(h.musicQueue.entries, isEmpty);
    expect(h.musicQueue.canUndo, isFalse);
    await h.stop();
    await player.processing.close();
    await player.events.close();
    await player.positions.close();
  });
  test(
      'large queue edits retain active entry and loading disallows removal; scope clear cancels late source',
      () async {
    final player = _Player();
    final handler = PlayerAudioHandler(player);
    final entries = List<PlaybackRequest>.generate(
        1001,
        (id) => ServerPlaybackRequest(
            mediaId: id,
            serverOrigin: 'http://local',
            userId: 1,
            title: 'Song $id',
            sourceUri: Uri.parse('http://local/$id'),
            startPosition: 0,
            savePosition: (p) async {},
            markFinished: () async {}));
    handler.activateSourceSession(handler.beginSourceSession());
    final gate = Completer<AudioSource?>();
    handler.startMusicSequence(
        wrap: false,
        entries: entries,
        index: 1000,
        expand: () async => entries,
        changed: (_) {},
        load: (r, c) => gate.future);
    final active = handler.musicQueue.entries.last.id;
    handler.moveQueue(1000, -1);
    expect(handler.musicQueue.entries[999].id, active);
    expect(handler.musicQueue.index, 999);
    final pending = handler.jumpQueue(0);
    await Future<void>.delayed(Duration.zero);
    await handler.removeQueue(0);
    await handler.clearQueue();
    expect(handler.musicQueue.entries.length, 1001);
    handler.clearRetainedQueue();
    gate.complete(AudioSource.uri(entries.first.sourceUri));
    await pending;
    expect(handler.musicQueue.entries, isEmpty);
    expect(player.loads, 0);
    await handler.stop();
    await player.processing.close();
    await player.events.close();
    await player.positions.close();
  });
  for (final remove in [false, true]) {
    for (final replacement in [false, true]) {
      test(
          'pending ${remove ? 'remove current' : 'clear'} cannot reinstate old scope Undo or edit ${replacement ? 'new album' : 'cleared account'}',
          () async {
        final player = _Player();
        final h = PlayerAudioHandler(player);
        final entered = Completer<void>(), release = Completer<void>();
        PlaybackRequest request(int id) => ServerPlaybackRequest(
            mediaId: id,
            serverOrigin: 'http://local',
            userId: 1,
            title: 'Song $id',
            sourceUri: Uri.parse('http://local/$id'),
            startPosition: 0,
            savePosition: (p) async {
              if (!entered.isCompleted) entered.complete();
              await release.future;
            },
            markFinished: () async {});
        final old = [request(1), request(2)];
        h.activateSourceSession(h.beginSourceSession());
        h.startProgress(
            savePosition: old.first.savePosition!,
            markFinished: old.first.markFinished!);
        h.startMusicSequence(
            wrap: false,
            entries: old,
            index: 0,
            expand: () async => old,
            changed: (_) {},
            load: (r, c) async => AudioSource.uri(r.sourceUri));
        final pending = remove ? h.removeQueue(0) : h.clearQueue();
        await entered.future;
        if (replacement) {
          final fresh = [request(99)];
          h.activateSourceSession(h.beginSourceSession());
          h.startMusicSequence(
              wrap: false,
              entries: fresh,
              index: 0,
              expand: () async => fresh,
              changed: (_) {},
              load: (r, c) async => AudioSource.uri(r.sourceUri));
          player.running = true;
        } else {
          h.clearRetainedQueue();
        }
        release.complete();
        await expectLater(pending, completes);
        expect(h.musicQueue.canUndo, isFalse);
        h.undoQueue();
        expect(h.musicQueue.entries.map((e) => e.request.title).toList(),
            replacement ? ['Song 99'] : isEmpty);
        if (replacement) {
          expect(player.running, isTrue,
              reason: 'old delayed stop cannot stop replacement source');
        }
        await h.stop();
        await player.processing.close();
        await player.events.close();
        await player.positions.close();
      });
    }
  }
}
