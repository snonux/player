import 'package:cookie_jar/cookie_jar.dart';
import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:just_audio/just_audio.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/models/media.dart';
import 'package:player_android/models/user.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/audio_handler_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/playback_cache_provider.dart';
import 'package:player_android/providers/progress_queue_provider.dart';
import 'package:player_android/services/audio_handler.dart';
import 'package:player_android/services/music_automation.dart';
import 'package:player_android/services/music_queue.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/services/progress_queue.dart';
import 'package:player_android/widgets/server_song_actions.dart';

class _Client extends PlayerApiClient {
  _Client() : super(dio: Dio());
  @override
  Future<List<Map<String, dynamic>>> musicAlbums(
          {String? artist, int? mediaId, List<int>? libraryIds}) async =>
      mediaId == 1
          ? [
              {'album_key': 'music'}
            ]
          : [];
  @override
  Future<double?> getMediaProgress(int id) async => 0;
  @override
  String playbackUrl(Media media) => 'https://one/${media.id}';
}

class _Auth extends AuthStateNotifier {
  @override
  Future<AuthState> build() async => const AuthState.authenticated(
      user: User(id: 1, username: 'test', isAdmin: false));
}

class _Progress extends ProgressQueueBase {
  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
  @override
  Future<void> enqueue(int id, double seconds) async {}
  @override
  Future<void> enqueueFinished(int id) async {}
}

class _Player extends AudioPlayer {
  bool running = false;
  @override
  Stream<PlaybackEvent> get playbackEventStream => const Stream.empty();
  @override
  Stream<bool> get playingStream => const Stream.empty();
  @override
  Stream<ProcessingState> get processingStateStream => const Stream.empty();
  @override
  Duration get position => Duration.zero;
  @override
  Duration get duration => const Duration(seconds: 200);
  @override
  ProcessingState get processingState => ProcessingState.ready;
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
  Future<void> seek(Duration? position, {int? index}) async {}
  @override
  Future<Duration?> setAudioSource(AudioSource source,
          {bool preload = true,
          int? initialIndex,
          Duration? initialPosition}) async =>
      duration;
}

void main() {
  testWidgets(
      'tagged podcast through media-detail Play now path stays manually playable but automation refuses; legacy snapshots use catalog',
      (tester) async {
    SharedPreferences.setMockInitialValues({});
    final api = _Client(), player = _Player();
    final handler = PlayerAudioHandler(player);
    addTearDown(() => tester.runAsync(handler.stop));
    late BuildContext context;
    late WidgetRef ref;
    await tester.pumpWidget(ProviderScope(
        overrides: [
          apiClientProvider.overrideWithValue(api),
          audioHandlerProvider.overrideWithValue(handler),
          playerBaseUrlProvider.overrideWithValue(Uri.parse('https://one')),
          authStateProvider.overrideWith(_Auth.new),
          cookieJarProvider.overrideWithValue(CookieJar()),
          progressQueueProvider.overrideWithValue(_Progress()),
          cachedAudioLoaderProvider.overrideWith((ref, generation) =>
              (r, wanted, {onPreparing}) async =>
                  wanted() ? AudioSource.uri(r.sourceUri) : null),
        ],
        child: MaterialApp(home: Scaffold(body: Consumer(builder: (c, r, _) {
          context = c;
          ref = r;
          r.watch(authStateProvider);
          return const SizedBox();
        })))));
    await tester.pumpAndSettle();
    final episode = Media.fromJson({
      'id': 2,
      'set_id': 42,
      'file_name': 'episode.mp3',
      'type': 'audio',
      'music': {'title': 'Tagged episode', 'artist': 'Host', 'album': 'Podcast'}
    });
    // This is the exact production action called by media-detail's Play now.
    final result = await tester.runAsync(
        () => insertServerSongs(context, ref, [episode], QueueInsertion.now));
    expect(result!.outcome, ServerSongInsertionOutcome.accepted);
    expect(player.running, isTrue);
    await tester.runAsync(handler.pause);
    final queued = handler.musicQueue.entries.single.request;
    expect(queued.music, isNotNull);
    Future<bool> catalog(int id) async =>
        (await api.musicAlbums(mediaId: id)).isNotEmpty;
    await expectLater(
        validateAutomationQueueMusic(
            [queued], 'https://one', 1, catalog, () => true),
        throwsStateError);
    final legacy = ServerPlaybackRequest(
        mediaId: 2,
        serverOrigin: 'https://one',
        userId: 1,
        sourceUri: Uri.parse('https://one/2'),
        title: 'Restored episode',
        music: queued.music);
    await expectLater(
        validateAutomationQueueMusic(
            [legacy], 'https://one', 1, catalog, () => true),
        throwsStateError);
    final music = ServerPlaybackRequest(
        mediaId: 1,
        serverOrigin: 'https://one',
        userId: 1,
        sourceUri: Uri.parse('https://one/1'),
        title: 'Untagged music');
    await validateAutomationQueueMusic(
        [music], 'https://one', 1, catalog, () => true);
    await tester.runAsync(handler.stop);
    await tester.pumpWidget(const SizedBox());
  });
}
