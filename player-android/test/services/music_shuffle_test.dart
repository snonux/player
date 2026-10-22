import 'dart:math';
import 'package:flutter_test/flutter_test.dart';
import 'package:just_audio/just_audio.dart';
import 'package:player_android/models/media.dart';
import 'package:player_android/services/audio_handler.dart';
import 'package:player_android/services/music_queue.dart';
import 'package:player_android/services/music_queue_snapshot.dart';
import 'package:player_android/services/music_shuffle.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/services/playback_session_coordinator.dart';

class _Player extends AudioPlayer {
  int loads = 0;
  Duration elapsed = const Duration(seconds: 4);
  @override
  Stream<Duration> get positionStream => const Stream.empty();
  @override
  Stream<bool> get playingStream => const Stream.empty();
  @override
  Stream<PlaybackEvent> get playbackEventStream => const Stream.empty();
  @override
  Stream<ProcessingState> get processingStateStream => const Stream.empty();
  @override
  Duration get position => elapsed;
  @override
  Duration get duration => const Duration(seconds: 12);
  @override
  ProcessingState get processingState => ProcessingState.ready;
  @override
  bool get playing => false;
  @override
  Future<void> pause() async {}
  @override
  Future<void> play() async {}
  @override
  Future<void> stop() async {}
  @override
  Future<void> seek(Duration? value, {int? index}) async {
    elapsed = value ?? Duration.zero;
  }

  @override
  Future<Duration?> setAudioSource(AudioSource source,
      {bool preload = true,
      int? initialIndex,
      Duration? initialPosition}) async {
    loads++;
    return duration;
  }
}

PlaybackRequest request(int id) => ServerPlaybackRequest(
    mediaId: id,
    serverOrigin: 'http://one',
    userId: 1,
    title: 'Song $id',
    sourceUri: Uri.parse('http://one/$id'),
    startPosition: 0,
    savePosition: (_) async {},
    markFinished: () async {});
void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
      'optional filters count occurrences and canonical sample retains original order',
      () {
    Media song(int id, int year, List<String> genres) => Media.fromJson({
          'id': id,
          'type': 'audio',
          'file_name': '$id.mp3',
          'music': {'year': year, 'genres': genres}
        });
    final songs = [
      song(1, 2021, ['Jazz']),
      song(2, 2022, [' Jazz ']),
      song(1, 2021, ['Jazz']),
      song(3, 2020, ['Rock'])
    ];
    final all =
        MusicShuffleOptions.parse(genre: ' JAZZ ', from: '2021', to: '2022');
    expect(all.select(songs).map((s) => s.id), [1, 2, 1]);
    final sample = MusicShuffleOptions.parse(genre: 'jazz', maximum: '2')
        .select(songs, Random(4));
    expect(sample.length, 2);
    expect(sample.every((s) => songs.contains(s)), isTrue);
    expect(songs.indexOf(sample.first), lessThan(songs.indexOf(sample.last)));
    expect(MusicShuffleOptions.parse(from: '2023').select(songs), isEmpty);
    for (final invalid in ['0', '-1', '1.2', '1e2', '10001', 'abc']) {
      expect(() => MusicShuffleOptions.parse(maximum: invalid),
          throwsFormatException);
    }
    expect(() => MusicShuffleOptions.parse(from: '2022', to: '2021'),
        throwsFormatException);
    expect(
        () => MusicShuffleOptions.parse(from: '10000'), throwsFormatException);
  });
  test('pure remainder preserves played prefix and pinned FIFO', () {
    final entries =
        List.generate(7, (i) => MusicQueueEntry(i + 1, request(i % 3)));
    final canonical = entries.map((e) => e.id).toList();
    final shuffled =
        orderMusicRemainder(entries, 1, canonical, {3, 4}, true, Random(2));
    expect(shuffled.take(4), entries.take(4));
    expect(shuffled.map((e) => e.id).toSet(), canonical.toSet());
    expect(orderMusicRemainder(shuffled, 1, canonical, {3, 4}, false), entries);
    expect(orderMusicRemainder(shuffled, 4, canonical, {3, 4}, false).take(5),
        shuffled.take(5));
  });
  test(
      'handler shuffle source stability FIFO append Undo persistence and scope',
      () async {
    final player = _Player(), coordinator = PlaybackSessionCoordinator();
    final h = PlayerAudioHandler(player);
    Future<bool> insert(List<int> ids, QueueInsertion action,
            {bool replace = false, bool shuffled = false}) =>
        h.insertQueue(
            entries: ids.map(request).toList(),
            action: action,
            replace: replace,
            shuffled: shuffled,
            claim: (r) => coordinator.claim(
                kind: r.kind, identity: r.identity, stop: h.stop),
            load: (r, c) async => AudioSource.uri(r.sourceUri));
    await insert([1, 2, 1, 3, 4, 5], QueueInsertion.now, replace: true);
    await h.jumpQueue(1);
    final original = h.musicQueue.entries.map((e) => e.id).toList();
    final loads = player.loads;
    player.elapsed = const Duration(seconds: 4);
    h.shuffleQueue();
    expect(h.musicQueue.shuffle, isTrue);
    expect(h.musicQueue.entries.take(2).map((e) => e.id), original.take(2));
    expect(player.loads, loads);
    expect(player.elapsed, const Duration(seconds: 4));
    final shuffled = h.musicQueue.entries.map((e) => e.id).toList();
    h.shuffleQueue(reshuffle: true);
    await h.undoQueue();
    expect(h.musicQueue.entries.map((e) => e.id), shuffled);
    h.shuffleQueue();
    expect(h.musicQueue.entries.map((e) => e.id), original);
    h.shuffleQueue();
    await insert([6, 7], QueueInsertion.next);
    final pinned =
        h.musicQueue.entries.skip(2).take(2).map((e) => e.id).toList();
    h.shuffleQueue(reshuffle: true);
    expect(h.musicQueue.entries.skip(2).take(2).map((e) => e.id), pinned);
    await insert([8], QueueInsertion.append);
    h.shuffleQueue();
    expect((h.musicQueue.entries.last.request as ServerPlaybackRequest).mediaId,
        8);
    expect(h.musicQueue.entries.skip(2).take(2).map((e) => e.id), pinned);
    await h.skipToNext();
    final prefix = h.musicQueue.entries
        .take(h.musicQueue.index + 1)
        .map((e) => e.id)
        .toList();
    h.shuffleQueue();
    h.shuffleQueue();
    expect(h.musicQueue.entries.take(prefix.length).map((e) => e.id), prefix);
    h.shuffleQueue();
    final saved = MusicQueueSnapshot.decode(h.queueSnapshot.encode());
    expect(saved.shuffle, isTrue);
    expect(saved.canonical.toSet(), saved.entries.map((e) => e.id).toSet());
    await h.removeQueue(h.musicQueue.entries.length - 1);
    await h.undoQueue();
    expect(h.queueSnapshot.canonical, saved.canonical);
    await h.clearQueue();
    await h.undoQueue();
    expect(h.queueSnapshot.shuffle, isTrue);
    await insert([9, 9, 10], QueueInsertion.now, replace: true, shuffled: true);
    expect(h.musicQueue.shuffle, isTrue);
    expect(h.musicQueue.index, 0);
    expect(
        h.musicQueue.entries
            .map((e) => (e.request as ServerPlaybackRequest).mediaId)
            .toList()
          ..sort(),
        [9, 9, 10]);
    final canonical = h.queueSnapshot.canonical;
    expect(canonical, orderedEquals(canonical.toList()..sort()));
    h.moveQueue(1, 1);
    final moved = h.musicQueue.entries.map((e) => e.id).toList();
    h.shuffleQueue();
    expect(h.musicQueue.entries.map((e) => e.id), moved);
    h.clearRetainedQueue();
    expect(h.musicQueue.shuffle, isFalse);
    expect(h.queueSnapshot.canonical, isEmpty);
    await h.stop();
  });
}
