import 'dart:async';
import 'package:dio/dio.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:just_audio/just_audio.dart';
import 'package:player_android/models/models.dart';
import 'package:player_android/services/audio_handler.dart';
import 'package:player_android/services/music_sequence.dart';
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
}
