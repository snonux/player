import 'package:audio_service/audio_service.dart';
import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:just_audio/just_audio.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/services/android_auto_catalog.dart';
import 'package:player_android/services/audio_handler.dart';
import 'package:player_android/services/music_queue.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/services/playback_session_coordinator.dart';

class _Player extends AudioPlayer {
  bool running = false;
  int loads = 0;
  @override
  bool get playing => running;
  @override
  ProcessingState get processingState => ProcessingState.ready;
  @override
  Duration get position => Duration.zero;
  @override
  Duration get duration => const Duration(seconds: 90);
  @override
  Stream<Duration> get positionStream => const Stream.empty();
  @override
  Stream<bool> get playingStream => const Stream.empty();
  @override
  Stream<PlaybackEvent> get playbackEventStream => const Stream.empty();
  @override
  Stream<ProcessingState> get processingStateStream => const Stream.empty();
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
  Future<void> setVolume(double value) async {}
  @override
  Future<void> seek(Duration? position, {int? index}) async {}
  @override
  Future<Duration?> setAudioSource(AudioSource source,
      {bool preload = true,
      int? initialIndex,
      Duration? initialPosition}) async {
    loads++;
    return duration;
  }
}

class _Api extends PlayerApiClient {
  _Api() : super(dio: Dio());
  @override
  Future<List<Map<String, dynamic>>> musicAlbums(
          {String? artist, int? mediaId}) async =>
      [
        {'album_key': 'al_1', 'title': 'Album', 'artist': 'Artist'}
      ];
  @override
  Future<Map<String, dynamic>> musicAlbum(String key) async => {
        'title': 'Album',
        'songs': [7, 3, 7]
            .map((id) => {
                  'id': id,
                  'type': 'audio',
                  'duration': 90,
                  'file_name': '$id.mp3'
                })
            .toList()
      };
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  late _Player player;
  late PlayerAudioHandler handler;
  late AutoAccount? account;
  late AndroidAutoCatalog catalog;

  setUp(() {
    SharedPreferences.setMockInitialValues({});
    player = _Player();
    handler = PlayerAudioHandler(player);
    final coordinator = PlaybackSessionCoordinator();
    account = AutoAccount(_Api(), 'http://server', 1, 1, () => account != null);
    catalog = AndroidAutoCatalog(
        account: () => account,
        queue: () => handler.musicQueue,
        playOccurrence: (id) => handler.jumpQueue(
            handler.musicQueue.entries.indexWhere((entry) => entry.id == id)),
        playSongs: (_, songs, index, label) async {
          await handler.insertQueue(
              entries: songs
                  .map((song) => ServerPlaybackRequest(
                      mediaId: song.id,
                      serverOrigin: 'http://server',
                      userId: 1,
                      sourceUri: Uri.parse(
                          'http://server/api/media/${song.id}/stream'),
                      title: song.displayTitle,
                      startPosition: 0,
                      savePosition: (_) async {},
                      markFinished: () async {}))
                  .toList(),
              action: QueueInsertion.append,
              replace: true,
              sourceLabel: label,
              claim: (request) => coordinator.claim(
                  kind: request.kind,
                  identity: request.identity,
                  stop: handler.stop),
              load: (request, current) async =>
                  current() ? AudioSource.uri(request.sourceUri) : null);
          await handler.jumpQueue(index);
        });
    handler.autoCatalog = catalog;
    addTearDown(handler.stop);
  });

  Future<List<MediaItem>> albumSongs() async {
    final root = await handler.getChildren(AudioService.browsableRootId);
    final albums = await handler.getChildren(root.first.id);
    return handler.getChildren(albums.first.id);
  }

  test(
      'host selection starts the shared ordered occurrence queue without a screen',
      () async {
    final rows = await albumSongs();
    await handler.playFromMediaId(rows[2].id);
    expect(
        handler.musicQueue.entries
            .map((entry) => (entry.request as ServerPlaybackRequest).mediaId),
        [7, 3, 7]);
    expect(handler.musicQueue.index, 1);
    expect(player.running, isTrue);
    expect(player.loads, 1);
    await handler.skipToNext();
    expect(handler.musicQueue.index, 2);
    expect(player.loads, 2);
  });

  test(
      'published queue window has a fresh active index and jumps the exact repeated occurrence',
      () async {
    final rows = await albumSongs();
    await handler.playFromMediaId(rows[2].id);
    handler.publishAutoQueue(
        catalog.queueItems(account!).skip(1).toList(), 'Album');
    expect(handler.playbackState.value.queueIndex, 0);
    await handler.skipToQueueItem(1);
    expect(handler.musicQueue.index, 2);
    handler.publishAutoQueue(
        catalog.queueItems(account!).skip(1).toList(), 'Album');
    expect(handler.playbackState.value.queueIndex, 1);
    await handler.pause();
    handler.publishAutoQueue(
        catalog.queueItems(account!).skip(2).toList(), 'Album');
    expect(handler.playbackState.value.queueIndex, 0);
    expect(handler.playbackState.value.playing, isFalse);
  });

  test(
      'invalid selection reports an action error while existing music remains truthful',
      () async {
    final rows = await albumSongs();
    await handler.playFromMediaId(rows.first.id);
    handler.publishAutoQueue(catalog.queueItems(account!), 'Album');
    account = null;
    await handler.playFromMediaId(rows.first.id);
    expect(player.running, isTrue);
    expect(handler.playbackState.value.playing, isTrue);
    expect(handler.playbackState.value.processingState,
        AudioProcessingState.ready);
    expect(handler.playbackState.value.errorMessage, contains('Open Player'));
  });

  test(
      'queue-only invalidation avoids refetching catalog views and uses two shared streams',
      () async {
    final root = await handler.getChildren('root');
    final library = handler.subscribeToChildren(root[0].id);
    expect(identical(library, handler.subscribeToChildren(root[1].id)), isTrue);
    final queue = handler.subscribeToChildren(root[3].id);
    var libraryEvents = 0, queueEvents = 0;
    final one = library.skip(1).listen((_) => libraryEvents++);
    final two = queue.skip(1).listen((_) => queueEvents++);
    addTearDown(one.cancel);
    addTearDown(two.cancel);
    handler.refreshAutoChildren(queueOnly: true);
    await Future<void>.delayed(Duration.zero);
    expect(libraryEvents, 0);
    expect(queueEvents, 1);
    handler.refreshAutoChildren();
    await Future<void>.delayed(Duration.zero);
    expect(libraryEvents, 1);
    expect(queueEvents, 2);
  });
}
