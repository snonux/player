import 'dart:async';
import 'package:flutter_test/flutter_test.dart';
import 'package:just_audio/just_audio.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:player_android/services/audio_handler.dart';
import 'package:player_android/services/playback_request.dart';

class _Player extends AudioPlayer {
  final indices = StreamController<int?>.broadcast();
  AudioSource? loaded;
  Duration elapsed = Duration.zero;
  bool running = false;
  int loads = 0, pauses = 0;
  @override
  Stream<int?> get currentIndexStream => indices.stream;
  @override
  Stream<PlaybackEvent> get playbackEventStream => const Stream.empty();
  @override
  Stream<bool> get playingStream => const Stream.empty();
  @override
  Stream<ProcessingState> get processingStateStream => const Stream.empty();
  @override
  Stream<Duration> get positionStream => const Stream.empty();
  @override
  bool get playing => running;
  @override
  Duration get position => elapsed;
  @override
  Duration get duration => const Duration(seconds: 10);
  @override
  Future<Duration?> setAudioSource(AudioSource source,
      {bool preload = true,
      int? initialIndex,
      Duration? initialPosition}) async {
    loaded = source;
    loads++;
    return duration;
  }

  @override
  Future<void> pause() async {
    running = false;
    pauses++;
  }

  @override
  Future<void> play() async {
    running = true;
  }

  @override
  Future<void> stop() async {
    running = false;
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(() => SharedPreferences.setMockInitialValues({}));

  Future<(PlayerAudioHandler, _Player, List<String>)> start(
      {Future<AudioSource?> Function(PlaybackRequest, bool Function())? prepare,
      bool Function()? owns}) async {
    final player = _Player(), writes = <String>[];
    final handler = PlayerAudioHandler(player);
    await handler.setGaplessEnabled(true);
    final generation = handler.beginSourceSession(ownsSource: owns);
    PlaybackRequest request(int id) => ServerPlaybackRequest(
        mediaId: id,
        serverOrigin: 'http://fixture',
        userId: 1,
        sourceUri: Uri.parse('http://fixture/$id'),
        title: 'Song $id',
        startPosition: 0,
        savePosition: (seconds) async => writes.add('$id:$seconds'),
        markFinished: () async => writes.add('$id:finished'));
    final entries = [request(1), request(2), request(3)];
    handler.prepareGaplessSource = prepare ??
        (request, current) async =>
            current() ? AudioSource.uri(request.sourceUri) : null;
    handler.setMediaRequest(entries[0]);
    await handler.loadSourceForSession(
        AudioSource.uri(entries[0].sourceUri), generation);
    handler.startProgress(
        savePosition: entries[0].savePosition!,
        markFinished: entries[0].markFinished!);
    handler.startMusicSequence(
        wrap: false,
        entries: entries,
        index: 0,
        load: (request, _) async => AudioSource.uri(request.sourceUri),
        changed: (request) => writes.add('identity:${request.title}'),
        expand: () async => entries);
    handler.activateSourceSession(generation);
    await handler.play();
    await Future<void>.delayed(const Duration(milliseconds: 10));
    return (handler, player, writes);
  }

  test(
      'native decoder boundary updates identity and snapshots outgoing progress without reload',
      () async {
    final (handler, player, writes) = await start();
    final playlist = player.loaded as ConcatenatingAudioSource;
    expect(playlist.useLazyPreparation, false);
    expect(playlist.children, hasLength(2));
    final generation = handler.musicState!.sourceGeneration;
    player.elapsed = const Duration(milliseconds: 125);
    player.indices.add(1);
    await Future<void>.delayed(const Duration(milliseconds: 10));
    expect(player.loads, 1,
        reason: 'No setAudioSource or pause at natural boundary');
    expect(player.pauses, 0);
    expect(handler.currentMetadataRequest!.title, 'Song 2');
    expect(handler.musicQueue.index, 1);
    expect(handler.musicState!.sourceGeneration, greaterThan(generation));
    expect(writes, containsAllInOrder(['1:10.0', '1:finished']));
    expect(writes, isNot(contains('1:0.125')));
    expect(playlist.children, hasLength(2),
        reason: 'Completed child pruned; next decoded child retained');
    await handler.saveProgress();
    expect(writes, contains('2:0.125'));
    await handler.stop();
  });

  test(
      'queue reorder discards delayed preparation and prepares correct occurrence',
      () async {
    final pending = Completer<AudioSource?>();
    final requested = <String>[];
    final (handler, player, _) = await start(prepare: (request, current) {
      requested.add(request.title);
      if (request.title == 'Song 2') return pending.future;
      return Future.value(AudioSource.uri(request.sourceUri));
    });
    handler.moveQueue(2, -1);
    await Future<void>.delayed(const Duration(milliseconds: 10));
    pending.complete(AudioSource.uri(Uri.parse('http://fixture/stale')));
    await Future<void>.delayed(const Duration(milliseconds: 10));
    final playlist = player.loaded as ConcatenatingAudioSource;
    expect(requested, ['Song 2', 'Song 3']);
    expect(playlist.children, hasLength(2));
    expect((playlist.children.last as UriAudioSource).uri.path, '/3');
    expect(player.elapsed, Duration.zero);
    await handler.stop();
  });

  test('account/source ownership rejects pending decoder preparation',
      () async {
    var owned = true;
    final pending = Completer<AudioSource?>();
    final (handler, player, _) =
        await start(owns: () => owned, prepare: (_, current) => pending.future);
    owned = false;
    pending.complete(AudioSource.uri(Uri.parse('http://fixture/stale')));
    await Future<void>.delayed(const Duration(milliseconds: 10));
    expect((player.loaded as ConcatenatingAudioSource).children, hasLength(1));
    await handler.stop();
  });

  test(
      'turning gapless off removes buffered successor without replacing current source',
      () async {
    final (handler, player, _) = await start();
    player.elapsed = const Duration(seconds: 4);
    await handler.setGaplessEnabled(false);
    await Future<void>.delayed(const Duration(milliseconds: 10));
    expect((player.loaded as ConcatenatingAudioSource).children, hasLength(1));
    expect(player.loads, 1);
    expect(player.elapsed.inSeconds, 4);
    expect(handler.gaplessEnabled, false);
    await handler.stop();
  });
}
