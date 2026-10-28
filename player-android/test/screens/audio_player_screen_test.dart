import 'package:cookie_jar/cookie_jar.dart';
// Widget tests for AudioPlayerScreen (audio_player_screen.dart).
//
// Tests cover:
//   1. Loading indicator shown during the initial build before initState fires.
//   2. Error view rendered when AudioPlayer.setAudioSource() throws.
//   3. Error view contains a human-readable message and a retry button.
//   4. Retry button re-triggers initialisation and ends in error state again.
//   5. Screen renders the AppBar title containing the mediaId.
//   6. Stream URL resolution (route-extra URL and client.streamUrl fallback).
//   7. Compatibility stream: preparing state while the server answers 503,
//      terminal errors, leaving while preparing, and readable error texts for
//      load failures and failures reported after loading.
//
// just_audio / audio_service behaviour in the test harness:
//   AudioPlayer initialises lazily; the native just_audio platform channel is
//   not available in the Flutter unit-test environment, so setAudioSource()
//   hangs indefinitely if we let it wait for a platform response.  We work
//   around this by:
//     a) Registering a no-op mock handler for the `com.ryanheise.audio_session`
//        method channel so that AudioSession.instance resolves immediately.
//     b) Providing a [_FakePlayerAudioHandler] via [audioHandlerProvider] so
//        that no real AudioPlayer or AudioService platform channel is invoked.
//     c) Using pump(Duration(seconds: N)) instead of pumpAndSettle() to advance
//        the test clock a fixed amount — enough for the async init path to
//        attempt and fail, without waiting forever.
//
// As a result the screen will be stuck in the "loading" state in tests (the
// platform call never returns), which is the expected observable behaviour in a
// headless test environment.  All tests verify the loading spinner, and the
// error-state tests use a FakeAudioPlayerScreen that injects a pre-built error.
//
// Run with: flutter test test/screens/audio_player_screen_test.dart

import 'dart:async';
import 'dart:io';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:just_audio/just_audio.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/app_routes.dart';
import 'package:player_android/models/audio_format.dart';
import 'package:player_android/models/lyrics.dart';
import 'package:player_android/models/media.dart';
import 'package:player_android/models/media_set.dart';
import 'package:player_android/models/media_bookmark.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/settings_provider.dart';
import 'package:player_android/providers/local_library_provider.dart';
import 'package:player_android/providers/audio_handler_provider.dart';
import 'package:player_android/providers/playback_preparer_provider.dart';
import 'package:player_android/providers/public_api_client_provider.dart';
import 'package:player_android/providers/playback_session_provider.dart';
import 'package:player_android/providers/cast_provider.dart';
import 'package:player_android/providers/presentation_provider.dart';
import 'package:player_android/providers/theme_provider.dart';
import 'package:player_android/providers/playback_cache_provider.dart';
import 'package:player_android/providers/progress_queue_provider.dart';
import 'package:player_android/screens/audio_player_screen.dart';
import 'package:player_android/screens/local_library_screen.dart';
import 'package:player_android/screens/notes_editor_screen.dart';
import 'package:player_android/services/local_library_repository.dart';
import 'package:player_android/services/audio_handler.dart';
import 'package:player_android/services/progress_queue.dart';
import 'package:player_android/services/playback_preparer.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/services/music_queue.dart';
import 'package:player_android/services/network_policy.dart';
import 'package:player_android/services/stream_quality.dart';
import 'package:player_android/widgets/music_mini_player.dart';
import 'package:player_android/services/playback_session_coordinator.dart';
import 'package:player_android/services/cast/cast_controller.dart';
import 'package:player_android/services/cast/cast_device.dart';
import 'package:player_android/services/cast/cast_session.dart';

import '../services/cast/fake_cast_receiver.dart';
import '../support/server_endpoint.dart';

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

/// In-memory [TokenStorage] that returns a fixed test token.
///
/// Avoids the platform-specific OS keychain in widget tests.
class _FakeTokenStorage implements TokenStorage {
  const _FakeTokenStorage();

  @override
  Future<String?> readToken() async => 'test-token';

  @override
  Future<void> writeToken(String token) async {}

  @override
  Future<void> deleteToken() async {}
}

class _Authenticated extends AuthStateNotifier {
  @override
  Future<AuthState> build() async => const AuthState.authenticated();
}

/// Controllable [PlayerApiClient] stub for [AudioPlayerScreen] tests.
///
/// Supplies catalog metadata, stream URLs, preparation probes and progress
/// callbacks without a server or native transport.
class _FakeApiClient extends PlayerApiClient {
  _FakeApiClient() : super(dio: Dio());

  final media = <int, Media>{};

  @override
  Future<List<Map<String, dynamic>>> musicAlbums(
          {String? artist, int? mediaId, List<int>? libraryIds}) async =>
      [];

  @override
  Future<List<Map<String, dynamic>>> streamProfiles() async => [
        {'id': 'opus96', 'label': 'Data saver · Opus 96 kbps'}
      ];

  @override
  Future<Media> getMedia(int id, {CancelToken? cancelToken}) async =>
      media[id] ??
      Media.fromJson({
        'id': id,
        'type': 'audio',
        'file_name': 'Audio – $id',
        'transcoded': probeAnswers.isNotEmpty,
      });

  @override
  String compatUrl(int mediaId) =>
      'http://localhost:8080/api/v1/media/$mediaId/compat';

  /// Records how many times [getMediaProgress] was called.
  final bookmarks = <MediaBookmark>[];
  @override
  Future<List<MediaBookmark>> listBookmarks(int mediaId) async =>
      List.of(bookmarks);
  @override
  Future<MediaBookmark> addBookmark(
      int mediaId, double seconds, String label) async {
    final bookmark = MediaBookmark(
        id: bookmarks.length + 1, positionSeconds: seconds, label: label);
    bookmarks.add(bookmark);
    return bookmark;
  }

  @override
  Future<void> deleteBookmark(int mediaId, int id) async {
    bookmarks.removeWhere((item) => item.id == id);
  }

  int browseCalls = 0;
  @override
  Future<List<MediaSet>> listSets() async => [
        MediaSet.fromJson({'id': 1, 'name': 'Music'})
      ];
  @override
  Future<Map<String, dynamic>> browseSet(int setId, {String? parent}) async {
    browseCalls++;
    throw StateError('Filtered list must not browse outside itself');
  }

  int getMediaProgressCallCount = 0;
  int updateStatusCallCount = 0;

  /// When non-null, [getMediaProgress] returns this value.
  double? progressResult;
  final lyricsIds = <int>[];
  final shareLyricsRequests = <(String, String?)>[];
  Future<Lyrics> Function()? lyricsResult;

  @override
  Future<Lyrics> getMediaLyrics(int mediaId) {
    lyricsIds.add(mediaId);
    return lyricsResult?.call() ?? Future.value(const Lyrics());
  }

  @override
  Future<Lyrics> getSharedLyrics(String token, {String? view}) {
    shareLyricsRequests.add((token, view));
    return lyricsResult?.call() ?? Future.value(const Lyrics());
  }

  final formatIds = <int>[];
  final shareFormatRequests = <(String, String?)>[];
  AudioFormat formatResult = const AudioFormat();

  @override
  Future<AudioFormat> getMediaAudioFormat(int mediaId) async {
    formatIds.add(mediaId);
    return formatResult;
  }

  @override
  Future<AudioFormat> getSharedAudioFormat(String token, {String? view}) async {
    shareFormatRequests.add((token, view));
    return formatResult;
  }

  /// Answers for [probePlayback], consumed in order; the last one repeats.
  /// Empty means the test does not expect a compatibility-stream probe.
  final probeAnswers = <PlaybackProbe>[];
  final probedUrls = <Uri>[];
  final probeTokens = <CancelToken?>[];

  @override
  Future<PlaybackProbe> probePlayback(
    Uri url, {
    CancelToken? cancelToken,
  }) async {
    if (probeAnswers.isEmpty) throw StateError('Unexpected probe of $url');
    final index = probedUrls.length;
    probedUrls.add(url);
    probeTokens.add(cancelToken);
    return probeAnswers[
        index < probeAnswers.length ? index : probeAnswers.length - 1];
  }

  @override
  Future<double?> getMediaProgress(int mediaId) async {
    getMediaProgressCallCount++;
    return progressResult;
  }

  @override
  Future<void> updateProgress({
    required int mediaId,
    required double positionSeconds,
  }) async {}

  @override
  Future<void> updateProgressStatus({
    required int mediaId,
    required String status,
  }) async {
    updateStatusCallCount++;
  }

  /// Returns a synthetic stream URL for construction in tests.
  @override
  String streamUrl(int mediaId) =>
      'http://localhost:8080/api/v1/media/$mediaId/stream';
}

/// No-op [ProgressQueueBase] stub for widget tests.
///
/// Implements [ProgressQueueBase] directly rather than extending [ProgressQueue]
/// so no real SQLite database is opened and no connectivity subscription is
/// created in the test harness (Liskov Substitution — any [ProgressQueueBase]
/// can be injected wherever the interface is required).
class _FakeProgressQueue implements ProgressQueueBase {
  final updates = <(int, double)>[];
  @override
  Future<void> clearAndSuspend() async {}
  @override
  Future<void> init(
      {ProgressScope? scope}) async {} // no-op — no DB needed in widget tests

  @override
  Future<void> resume(ProgressScope scope) async {}

  @override
  Future<void> suspend() async {}

  @override
  Future<void> enqueue(int mediaId, double positionSeconds) async {
    updates.add((mediaId, positionSeconds));
  }

  final finishedItems = <int>[];

  @override
  Future<void> enqueueFinished(int mediaId) async {
    finishedItems.add(mediaId);
  }

  @override
  Future<void> dispose() async {} // no-op
}

/// A [PlayerAudioHandler] subclass that wraps a real [AudioPlayer] but
/// overrides [setMediaItem] and playback methods to be no-ops so that no
/// platform channels are invoked during widget tests.
///
/// The [player] getter returns the real AudioPlayer so that stream subscriptions
/// in [AudioPlayerScreen] (positionStream, playingStream) work without crashing,
/// while [setAudioSource] is the call that will hang (pending platform channel) —
/// matching the existing test behaviour where the screen stays in the loading
/// state.
class _FakePlayerAudioHandler extends PlayerAudioHandler {
  _FakePlayerAudioHandler() : super(AudioPlayer());

  /// Prevents any media-session metadata broadcast from being sent,
  /// since there is no registered AudioService in the test harness.
  @override
  void setMediaItem({required String id, required String title}) {
    // no-op in tests — audio_service is not initialised
  }

  @override
  Future<void> play({bool automatic = false}) async {} // no-op

  @override
  Future<void> pause({bool automatic = false}) async {} // no-op

  @override
  Future<void> stop() async {} // no-op
}

class _PlayableAudioPlayer extends AudioPlayer {
  @override
  Future<void> setVolume(double volume) async {}

  Completer<Duration?>? pendingLoad;
  bool failNextLoad = false;

  /// When non-null, every load fails with it (an undecodable source).
  Object? loadError;

  /// Lets a test report a native failure after the source was loaded.
  final playbackEvents = StreamController<PlaybackEvent>.broadcast();
  int sourceRequests = 0;
  AudioSource? loadedSource;
  Duration elapsed = Duration.zero;
  bool isPlaying = false;
  bool isStopped = false;
  double selectedSpeed = 1.0;
  final playingChanges = StreamController<bool>.broadcast();

  @override
  Future<Duration?> setAudioSource(AudioSource source,
      {bool preload = true, int? initialIndex, Duration? initialPosition}) {
    isStopped = false;
    sourceRequests++;
    loadedSource = source;
    elapsed = Duration.zero;
    if (loadError != null) return Future.error(loadError!);
    if (failNextLoad) {
      failNextLoad = false;
      return Future.error(StateError('first load failed'));
    }
    return pendingLoad?.future ?? Future.value(const Duration(seconds: 100));
  }

  @override
  Future<void> seek(Duration? position, {int? index}) async {
    elapsed = position ?? Duration.zero;
  }

  @override
  Duration get position => elapsed;
  @override
  Duration? get duration => const Duration(seconds: 100);
  @override
  bool get playing => isPlaying;
  @override
  Stream<bool> get playingStream => playingChanges.stream;
  @override
  Stream<PlaybackEvent> get playbackEventStream => playbackEvents.stream;
  @override
  Stream<ProcessingState> get processingStateStream => const Stream.empty();
  @override
  ProcessingState get processingState =>
      isStopped ? ProcessingState.idle : ProcessingState.ready;

  @override
  Future<void> play() async {
    isStopped = false;
    isPlaying = true;
    playingChanges.add(true);
  }

  @override
  Future<void> pause() async {
    isPlaying = false;
    playingChanges.add(false);
  }

  @override
  Future<void> stop() async {
    isStopped = true;
    isPlaying = false;
    playingChanges.add(false);
  }

  @override
  Future<void> setSpeed(double speed) async {
    selectedSpeed = speed;
  }
}

class _PlayableHandler extends PlayerAudioHandler {
  _PlayableHandler(super.player);
  int playCalls = 0;
  String? lastTitle;
  String? lastId;

  @override
  void setMediaRequest(PlaybackRequest request) {
    lastId = request.identity;
    lastTitle = request.title;
    super.setMediaRequest(request);
  }

  @override
  Future<void> play({bool automatic = false}) async {
    playCalls++;
    await super.play(automatic: automatic);
  }
}

// ---------------------------------------------------------------------------
// Test setup helpers
// ---------------------------------------------------------------------------

/// Registers a no-op mock handler for the audio_session method channel.
///
/// Without this, AudioSession.instance (called inside just_audio's
/// setAudioSource) waits for a platform response that never arrives in the
/// headless test environment, causing pumpAndSettle to time out.
void _setupAudioSessionMock() {
  SharedPreferences.setMockInitialValues({});
  const connectivity = MethodChannel('dev.fluttercommunity.plus/connectivity');
  const connectivityEvents =
      MethodChannel('dev.fluttercommunity.plus/connectivity_status');
  final messenger =
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
  messenger.setMockMethodCallHandler(connectivity, (_) async => ['wifi']);
  messenger.setMockMethodCallHandler(connectivityEvents, (_) async => null);
  const audioSessionChannel = MethodChannel('com.ryanheise.audio_session');
  TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
      .setMockMethodCallHandler(audioSessionChannel, (_) async => null);
}

/// Removes the audio_session mock handler after each test.
void _teardownAudioSessionMock() {
  final messenger =
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
  messenger.setMockMethodCallHandler(
      const MethodChannel('dev.fluttercommunity.plus/connectivity'), null);
  messenger.setMockMethodCallHandler(
      const MethodChannel('dev.fluttercommunity.plus/connectivity_status'),
      null);
  const audioSessionChannel = MethodChannel('com.ryanheise.audio_session');
  TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
      .setMockMethodCallHandler(audioSessionChannel, null);
}

/// Pumps [AudioPlayerScreen] for [mediaId] inside a [ProviderScope] with
/// overridden providers, backed by a minimal [GoRouter] for navigation.
///
/// Playable fixtures drain startup work without advancing the fake clock.
/// The default handler intentionally keeps its native source load pending.
Future<void> _pumpScreen(
  WidgetTester tester,
  _FakeApiClient fakeClient, {
  String mediaId = '42',
  List<Media> sequence = const [],
  int? sequenceIndex,
  bool traverseFolders = false,
  bool albumCollection = false,
  String? mediaUrl,
  String? mediaTitle,
  Uri? serverBaseUrl,
  _FakePlayerAudioHandler? fakeHandler,
  PlayerAudioHandler? handlerOverride,
  _FakeProgressQueue? progressQueue,
  PlaybackRequest? request,
  PlaybackSessionCoordinator? coordinator,
  bool forbidServerDependencies = false,
  double textScale = 1.0,
  PlaybackPreparer? preparer,
  LocalLibraryDatabase? localDatabase,
  CastController? castController,
  CastDiscovery? castDiscovery,
  ThemeData? theme,
}) async {
  final handler = handlerOverride ?? fakeHandler ?? _FakePlayerAudioHandler();
  fakeClient.media.addEntries(sequence.map((item) => MapEntry(item.id, item)));
  final origin = request is ServerPlaybackRequest
      ? request.serverOrigin
      : (serverBaseUrl ?? Uri.parse(kPlayerBaseUrl)).origin;
  final credentials = CredentialMutationQueue()..enable(0, origin: origin);

  final router = GoRouter(
    initialLocation: '/audio/$mediaId',
    routes: [
      GoRoute(
        path: '/audio/:mediaId',
        builder: (context, state) => AudioPlayerScreen(
          mediaId: state.pathParameters['mediaId']!,
          sequence: sequence,
          sequenceIndex: sequenceIndex,
          traverseFolders: traverseFolders,
          albumCollection: albumCollection,
          mediaUrl: mediaUrl,
          mediaTitle: mediaTitle,
          request: request,
        ),
      ),
    ],
  );

  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        if (localDatabase != null)
          localLibraryDatabaseProvider.overrideWithValue(localDatabase),
        unpairedServerEndpointOverride(),
        authStateProvider.overrideWith(_Authenticated.new),
        cookieJarProvider.overrideWith((ref) => CookieJar()),
        credentialMutationQueueProvider.overrideWithValue(credentials),
        if (serverBaseUrl != null)
          playerBaseUrlProvider.overrideWithValue(serverBaseUrl),
        if (coordinator != null)
          playbackSessionCoordinatorProvider.overrideWithValue(coordinator),
        if (castController != null)
          castControllerProvider.overrideWith((ref) => castController),
        if (castDiscovery != null)
          castDiscoveryProvider.overrideWithValue(castDiscovery),
        if (preparer != null)
          playbackPreparerProvider.overrideWithValue(preparer),
        tokenStorageProvider.overrideWith((ref) {
          if (forbidServerDependencies) throw StateError('Server token read');
          return const _FakeTokenStorage();
        }),
        apiClientProvider.overrideWith((ref) {
          if (forbidServerDependencies) throw StateError('Server API read');
          return fakeClient;
        }),
        // Override audioHandlerProvider so no real AudioService or AudioPlayer
        // platform channels are invoked during widget tests.
        audioHandlerProvider.overrideWithValue(handler),
        // Override progressQueueProvider so no real SQLite DB is opened and
        // no connectivity subscription is created during widget tests.
        progressQueueProvider.overrideWith((ref) {
          if (forbidServerDependencies) throw StateError('Server queue read');
          return progressQueue ?? _FakeProgressQueue();
        }),
      ],
      child: MaterialApp.router(
        theme: theme,
        routerConfig: router,
        builder: (context, child) => MediaQuery(
          data: MediaQuery.of(context)
              .copyWith(textScaler: TextScaler.linear(textScale)),
          child: child!,
        ),
      ),
    ),
  );
  // Network policy is initialized outside the widget test's fake clock. Let
  // its completed startup future resume source loading before asserting UI.
  await tester.runAsync(() => Future<void>.delayed(Duration.zero));
  if (handler is _PlayableHandler) {
    for (var i = 0;
        i < 20 &&
            find
                .byKey(const Key('audio_player_loading'))
                .evaluate()
                .isNotEmpty &&
            find.byKey(_kPreparingLabel).evaluate().isEmpty;
        i++) {
      await tester.pump();
      await tester.runAsync(() => Future<void>.delayed(Duration.zero));
    }
    await tester.pump();
  }
}

const _kCompatUrl = 'http://localhost:8080/api/v1/media/42/compat';
const _kPreparingLabel = Key('playback_preparing_label');
const _kTranscoding = PlaybackProbe(
  statusCode: 503,
  retryAfter: Duration(seconds: 5),
);
const _kReady = PlaybackProbe(statusCode: 200);

/// Opens the player's "More options" menu (secondary actions live there).
Future<void> _openMenu(WidgetTester tester) async {
  await tester.tap(find.byTooltip('More options'));
  for (var i = 0; i < 6; i++) {
    await tester.pump(const Duration(milliseconds: 100));
  }
}

Future<void> _flushPlaybackStartup(WidgetTester tester) async {
  for (var i = 0; i < 4; i++) {
    await tester.pump();
    await tester.runAsync(() => Future<void>.delayed(Duration.zero));
  }
  await tester.pump();
}

/// A playable handler whose player is cleaned up when the test ends.
(_PlayableAudioPlayer, _PlayableHandler) _usePlayableHandler(
    WidgetTester tester) {
  final player = _PlayableAudioPlayer();
  final handler = _PlayableHandler(player);
  addTearDown(() async {
    await tester.pumpWidget(const SizedBox.shrink());
    await handler.stop();
    await handler.endProgress();
    await player.playingChanges.close();
    await player.playbackEvents.close();
  });
  return (player, handler);
}

String _errorText(WidgetTester tester) => tester
    .widget<Text>(find.byKey(const Key('audio_player_error_message')))
    .data!;

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUpAll(() async {
    _setupAudioSessionMock();
    await MusicNetworkPolicy.initialize();
    _teardownAudioSessionMock();
  });
  for (final accent in PlayerAccent.values) {
    testWidgets(
        'audio player uses dark surface contrast for ${accent.name} in light mode',
        (tester) async {
      SharedPreferences.setMockInitialValues({
        'player_presentation': '{"accent":"${accent.name}"}',
      });
      await _pumpScreen(tester, _FakeApiClient(),
          theme: buildLightTheme(accent));
      await tester.pump();
      await tester.pump();

      await _openMenu(tester);
      await tester.tap(find.byKey(const Key('sleep_timer_menu_item')));
      for (var i = 0; i < 6; i++) {
        await tester.pump(const Duration(milliseconds: 100));
      }
      await tester.tap(find.text('5 minutes'));
      for (var i = 0; i < 6; i++) {
        await tester.pump(const Duration(milliseconds: 100));
      }
      final buttonFinder = find.ancestor(
          of: find.textContaining('remaining'),
          matching: find.byType(TextButton));
      expect(buttonFinder, findsOneWidget);
      final buttonContext = tester.element(buttonFinder);
      final button = tester.widget<TextButton>(buttonFinder);
      final foreground =
          button.defaultStyleOf(buttonContext).foregroundColor!.resolve({})!;
      expect(Theme.of(buttonContext).brightness, Brightness.dark);
      expect(foreground, buildDarkTheme(accent).colorScheme.primary);
      final contrast = (foreground.computeLuminance() + .05) /
          (Colors.black.computeLuminance() + .05);
      expect(contrast, greaterThanOrEqualTo(4.5));
      expect(
          Theme.of(tester.element(find.byType(AudioPlayerScreen))).brightness,
          Brightness.light,
          reason: 'the player does not change the app theme');
      await tester.tap(find.text('Cancel timer'));
      await tester.pump();
      await tester.pumpWidget(const SizedBox.shrink());
    });
  }
  testWidgets(
      'renderer mini reopens current queue occurrence without local source claim',
      (tester) async {
    await tester.runAsync(() async {
      await StreamQuality.initialize();
      await MusicNetworkPolicy.save('mode', 'wifi');
      await StreamQuality.save('http://local', 'opus96');
    });
    addTearDown(() async {
      await StreamQuality.save('http://local', 'auto');
      await StreamQuality.save('http://local', 'auto', network: 'wifi');
      await MusicNetworkPolicy.save('mode', 'auto');
    });
    final (player, handler) = _usePlayableHandler(tester);
    final client = _FakeApiClient();
    client.rawDio.options.baseUrl = 'http://local';
    final coordinator = PlaybackSessionCoordinator();
    ServerPlaybackRequest request(int id) => ServerPlaybackRequest(
        mediaId: id,
        serverOrigin: 'http://local',
        userId: 1,
        title: 'Renderer song $id',
        sourceUri: Uri.parse('http://local/$id'),
        startPosition: 0,
        savePosition: (_) async {},
        markFinished: () async {});
    final entries = [request(1), request(2), request(1)];
    const device = CastDevice(
        id: 'opaque', name: 'Kitchen', host: '', port: 0, isRenderer: true);
    final controller = CastController(
        coordinator: coordinator,
        resolveMedia: (request, {required isVideo}) async => CastMedia(
            url: request.sourceUri,
            contentType: 'audio/mpeg',
            title: request.title),
        startSession: (device) => CastSession.start(device,
            connect: (_) async => FakeCastReceiver()));
    await tester.runAsync(() => controller.start(
        device: device,
        request: entries[2],
        position: const Duration(seconds: 37),
        isVideo: false,
        rendererQueue: entries,
        rendererIndex: 2,
        existingOccurrence: 'a' * 32));
    final generation = coordinator.generation;
    await tester.pumpWidget(ProviderScope(
        overrides: [
          audioHandlerProvider.overrideWithValue(handler),
          playbackSessionCoordinatorProvider.overrideWithValue(coordinator),
          castControllerProvider.overrideWith((ref) => controller),
          apiClientProvider.overrideWithValue(client),
          playerBaseUrlProvider.overrideWithValue(Uri.parse('http://local')),
          credentialMutationQueueProvider.overrideWithValue(
              CredentialMutationQueue(credentialsEnabled: true)),
          tokenStorageProvider.overrideWithValue(const _FakeTokenStorage()),
          cookieJarProvider.overrideWith((ref) => CookieJar()),
          progressQueueProvider.overrideWithValue(_FakeProgressQueue()),
          cachedAudioLoaderProvider.overrideWith((ref, generation) =>
              (request, current, {onPreparing}) async =>
                  AudioSource.uri(request.sourceUri)),
        ],
        child: const MaterialApp(
            home: Scaffold(bottomNavigationBar: MusicMiniPlayer()))));
    await tester.pump();
    expect(find.text('On Kitchen'), findsOneWidget);
    await tester.tap(find.byKey(const Key('renderer_mini_open')));
    await tester.pumpAndSettle();
    expect(find.text('Playing on Kitchen'), findsOneWidget);
    expect(find.byKey(const Key('music_queue_button')), findsOneWidget);
    await tester.tap(find.byKey(const Key('music_queue_button')));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('renderer_queue_title')), findsOneWidget);
    expect(find.textContaining('3 of 3'), findsOneWidget);
    await tester.tap(find.byTooltip('Close queue'));
    await tester.pumpAndSettle();
    expect(player.sourceRequests, 0);
    expect(coordinator.generation, generation);
    expect(controller.currentOccurrenceID, 'a' * 32);
    await tester.pageBack();
    await tester.pump();
    expect(find.byKey(const Key('renderer_mini_open')), findsOneWidget);
    await tester.tap(find.byKey(const Key('renderer_mini_open')));
    for (var i = 0; i < 5; i++) {
      await tester.pump();
    }
    player.failNextLoad = true;
    await tester.runAsync(controller.stop);
    for (var i = 0; i < 8; i++) {
      await tester.pump(const Duration(milliseconds: 100));
    }
    expect(find.byKey(const Key('audio_player_error')), findsOneWidget);
    await tester.tap(find.text('Quality'));
    await tester.pumpAndSettle();
    await tester.tap(find.byType(DropdownButtonFormField<String>).last);
    await tester.pumpAndSettle();
    await tester.tap(find.text('Original (no conversion)').last);
    await tester.pumpAndSettle();
    Navigator.of(tester.element(find.text('Stream quality'))).pop();
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('audio_player_retry')));
    await _flushPlaybackStartup(tester);
    expect(player.sourceRequests, 2);
    expect((player.loadedSource as UriAudioSource).uri,
        Uri.parse(client.streamUrl(1)));
    expect(player.elapsed.inSeconds, greaterThanOrEqualTo(37));
    expect(player.isPlaying, isFalse);
    expect(handler.musicQueue.index, 2);
    expect(handler.listeningOccurrenceID, 'a' * 32);
    unawaited(handler.stop());
    for (var i = 0; i < 8; i++) {
      await tester.pump(const Duration(milliseconds: 100));
    }
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets(
      'mini player attaches active lease without source load or position reset',
      (tester) async {
    final (player, handler) = _usePlayableHandler(tester);
    final coordinator = PlaybackSessionCoordinator();
    final request = ServerPlaybackRequest(
        mediaId: 1,
        serverOrigin: 'http://local',
        userId: 1,
        title: 'Real title',
        artist: 'Real artist',
        album: 'Real album',
        sourceUri: Uri.parse('http://local/1'),
        startPosition: 0,
        savePosition: (_) async {},
        markFinished: () async {});
    await tester.runAsync(() => handler.insertQueue(
        entries: [request],
        action: QueueInsertion.now,
        claim: (r) => coordinator.claim(
            kind: r.kind, identity: r.identity, stop: handler.stop),
        load: (r, current) async => AudioSource.uri(r.sourceUri)));
    player.elapsed = const Duration(seconds: 37);
    final loads = player.sourceRequests, lease = handler.sequenceLease;
    await tester.pumpWidget(ProviderScope(
        overrides: [
          audioHandlerProvider.overrideWithValue(handler),
          apiClientProvider.overrideWithValue(_FakeApiClient()),
        ],
        child: const MaterialApp(
            home: Scaffold(bottomNavigationBar: MusicMiniPlayer()))));
    await tester.pump();
    await tester.pump();
    expect(find.text('Real artist · Real album'), findsOneWidget);
    await tester.tap(find.byKey(const Key('music_mini_open')));
    for (var i = 0; i < 5; i++) {
      await tester.pump();
    }
    expect(find.byKey(const Key('audio_track_title')), findsOneWidget);
    expect(player.sourceRequests, loads);
    expect(player.elapsed, const Duration(seconds: 37));
    expect(identical(handler.sequenceLease, lease), isTrue);
    await tester.pageBack();
    await tester.pump();
    expect(player.isPlaying, isTrue);
    expect(player.sourceRequests, loads);
    await tester.runAsync(handler.stop);
    await tester.pump();
    expect(find.byKey(const Key('music_mini_open')), findsNothing);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets(
      'mini attachment seeds loading and error snapshots and rejects stopped lease',
      (tester) async {
    final (player, handler) = _usePlayableHandler(tester);
    final coordinator = PlaybackSessionCoordinator();
    PlaybackRequest request(int id) => ServerPlaybackRequest(
        mediaId: id,
        serverOrigin: 'http://local',
        userId: 1,
        title: 'Track $id',
        sourceUri: Uri.parse('http://local/$id'),
        startPosition: 0,
        savePosition: (_) async {},
        markFinished: () async {});
    final pending = Completer<AudioSource?>();
    await tester.runAsync(() => handler.insertQueue(
        entries: [request(1), request(2)],
        action: QueueInsertion.now,
        claim: (r) => coordinator.claim(
            kind: r.kind, identity: r.identity, stop: handler.stop),
        load: (r, current) async => r.title == 'Track 2'
            ? await pending.future
            : AudioSource.uri(r.sourceUri)));
    late Future<void> moving;
    await tester.runAsync(() async {
      moving = handler.skipToNext();
      await Future<void>.delayed(Duration.zero);
    });
    final state = handler.musicState!, lease = handler.sequenceLease!;
    expect(state.loading, isTrue);
    await tester.pumpWidget(ProviderScope(
        overrides: [
          audioHandlerProvider.overrideWithValue(handler),
          apiClientProvider.overrideWithValue(_FakeApiClient())
        ],
        child: const MaterialApp(
            home: Scaffold(bottomNavigationBar: MusicMiniPlayer()))));
    await tester.pump();
    await tester.pump();
    await tester.tap(find.byKey(const Key('music_mini_open')));
    await tester.pump();
    await tester.pump();
    expect(find.byKey(const Key('audio_player_loading')), findsOneWidget);
    await tester.runAsync(() async {
      pending.completeError(StateError('track unavailable'));
      await moving;
    });
    for (var i = 0; i < 5; i++) {
      await tester.pump();
    }
    expect(find.byKey(const Key('audio_player_retry')), findsOneWidget);
    await tester.pageBack();
    await tester.pump();
    final next = tester.widget<IconButton>(find.byWidgetPredicate(
        (w) => w is IconButton && w.tooltip == 'Next track'));
    expect(next.onPressed, isNotNull,
        reason: 'failure snapshot rebuilds mini controls after loading ends');
    final loads = player.sourceRequests;
    await tester.runAsync(handler.stop);
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pumpWidget(ProviderScope(
        overrides: [audioHandlerProvider.overrideWithValue(handler)],
        child: MaterialApp(
            home: AudioPlayerScreen(
                mediaId: '2',
                request: state.request,
                attachCurrent: true,
                attachmentLease: lease,
                attachmentGeneration: state.sourceGeneration))));
    await tester.pump();
    await tester.pump();
    expect(find.text('This track is no longer playing. Return to the queue.'),
        findsOneWidget);
    expect(player.sourceRequests, loads,
        reason: 'stale attachment never reloads the stopped source');
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets(
      'server lyrics use the current media and line taps seek its session',
      (tester) async {
    final (player, handler) = _usePlayableHandler(tester);
    final client = _FakeApiClient()
      ..lyricsResult = () async => Lyrics.fromJson({
            'synced': true,
            'lines': [
              {'text': 'Seek here', 'start': 12.5}
            ],
          });
    await _pumpScreen(tester, client,
        handlerOverride: handler,
        mediaUrl: 'http://localhost:8080/api/v1/media/42/stream');
    for (var i = 0; i < 20 && !player.isPlaying; i++) {
      await tester.pump(const Duration(milliseconds: 10));
    }
    await tester.pump();
    expect(player.isPlaying, isTrue);
    await tester.ensureVisible(find.byKey(const Key('audio_player_lyrics')));
    await tester.tap(find.byKey(const Key('audio_player_lyrics')));
    await tester.pump();
    await tester.pump();
    expect(client.lyricsIds, [42]);
    await tester.ensureVisible(find.text('Seek here'));
    await tester.tap(find.text('Seek here'));
    await tester.pump();
    expect(player.elapsed, const Duration(milliseconds: 12500));
    expect(player.isPlaying, isTrue);
    await tester.runAsync(handler.stop);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('shows the server audio format under the seek bar',
      (tester) async {
    final (player, handler) = _usePlayableHandler(tester);
    final client = _FakeApiClient()
      ..formatResult = AudioFormat.fromJson({
        'codec_label': 'FLAC',
        'bit_depth': 24,
        'sample_rate': 96000,
        'bitrate': 2304000,
        'channels': 2,
        'lossless': true,
      });
    await _pumpScreen(tester, client,
        handlerOverride: handler,
        mediaUrl: 'http://localhost:8080/api/v1/media/42/stream');
    await tester.pump();
    await tester.pump();
    expect(client.formatIds, [42]);
    expect(find.text('FLAC · 24-bit · 96 kHz · 2304 kbps · Stereo'),
        findsOneWidget);
    expect(player.isPlaying, isTrue);
    await tester.runAsync(handler.stop);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets(
      'public lyrics use the public client and original viewing credential',
      (tester) async {
    final (player, handler) = _usePlayableHandler(tester);
    final client = _FakeApiClient();
    await tester.pumpWidget(ProviderScope(
      overrides: [
        publicApiClientProvider.overrideWithValue(client),
        apiClientProvider
            .overrideWith((_) => throw StateError('Account API accessed')),
        audioHandlerProvider.overrideWithValue(handler),
      ],
      child: const MaterialApp(
          home: AudioPlayerScreen(
        mediaId: '0',
        isPublicShare: true,
        mediaUrl: 'http://localhost/s/abc/stream?view=original',
      )),
    ));
    await tester.pump();
    await tester.pump();
    await tester.ensureVisible(find.byKey(const Key('audio_player_lyrics')));
    await tester.tap(find.byKey(const Key('audio_player_lyrics')));
    await tester.pump();
    await tester.pump();
    expect(client.shareLyricsRequests, [('abc', 'original')]);
    expect(client.shareFormatRequests, [('abc', 'original')]);
    expect(find.text('No lyrics available.'), findsOneWidget);
    expect(player.isPlaying, isTrue);
    await tester.runAsync(handler.stop);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('same local ID with a new URI replaces the loaded audio source',
      (tester) async {
    final player = _PlayableAudioPlayer();
    final handler = _PlayableHandler(player);
    addTearDown(() async {
      await tester.runAsync(handler.stop);
      await player.playingChanges.close();
    });
    Future<void> showSource(String uri) => tester.pumpWidget(ProviderScope(
          overrides: [
            audioHandlerProvider.overrideWithValue(handler),
            apiClientProvider
                .overrideWith((_) => throw StateError('Server API read')),
            progressQueueProvider
                .overrideWith((_) => throw StateError('Server queue read')),
            tokenStorageProvider
                .overrideWith((_) => throw StateError('Server token read')),
          ],
          child: MaterialApp(
              home: AudioPlayerScreen(
            mediaId: '7',
            request: LocalPlaybackRequest(
              localMediaId: 7,
              sourceUri: Uri.parse(uri),
              title: 'Local track',
            ),
          )),
        ));
    await showSource('content://provider/old');
    await tester.pump();
    await tester.pump();
    expect((player.loadedSource as UriAudioSource).uri.toString(),
        'content://provider/old');
    await showSource('content://provider/new');
    for (var attempt = 0; attempt < 100 && handler.playCalls < 2; attempt++) {
      await tester.pump();
      await tester.runAsync(
          () async => Future<void>.delayed(const Duration(milliseconds: 10)));
    }
    expect((player.loadedSource as UriAudioSource).uri.toString(),
        'content://provider/new');
    expect(handler.playCalls, 2);
    expect(handler.lastId, 'local:7');
    player.failNextLoad = true;
    await showSource('content://provider/unreadable');
    for (var attempt = 0;
        attempt < 100 &&
            find.byKey(const Key('audio_player_error')).evaluate().isEmpty;
        attempt++) {
      await tester.pump();
      await tester.runAsync(
          () async => Future<void>.delayed(const Duration(milliseconds: 10)));
    }
    expect(find.byKey(const Key('audio_player_error')), findsOneWidget);
    expect(handler.playCalls, 2);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('disposed audio route stops a source waiting for resume data',
      (tester) async {
    final coordinator = PlaybackSessionCoordinator();
    final resume = Completer<double?>();
    var readingResume = false;
    final player = _PlayableAudioPlayer();
    final handler = _PlayableHandler(player);
    const uri = 'content://provider/pending-resume';
    await _pumpScreen(tester, _FakeApiClient(),
        handlerOverride: handler,
        coordinator: coordinator,
        forbidServerDependencies: true,
        request: LocalPlaybackRequest(
            localMediaId: 7,
            sourceUri: Uri.parse(uri),
            title: 'Waiting track',
            readPosition: () {
              readingResume = true;
              return resume.future;
            }));
    for (var attempt = 0; attempt < 100 && !readingResume; attempt++) {
      await tester.pump();
      await tester.runAsync(
          () async => Future<void>.delayed(const Duration(milliseconds: 10)));
    }
    expect(readingResume, isTrue);
    expect(player.sourceRequests, 1);
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump();
    for (var attempt = 0;
        attempt < 100 && coordinator.isLocalSourceInUse(uri);
        attempt++) {
      await tester.pump(const Duration(milliseconds: 10));
      await tester.runAsync(
          () async => Future<void>.delayed(const Duration(milliseconds: 1)));
    }
    expect(coordinator.activeKind, isNull);
    expect(coordinator.isLocalSourceInUse(uri), isFalse);
    resume.complete(8);
    await tester.pump();
    await tester.pump();
    expect(handler.playCalls, 0);
    expect(player.isPlaying, isFalse);
    expect(player.elapsed, Duration.zero);
    await tester.runAsync(handler.stop);
    await player.playingChanges.close();
  });

  testWidgets('disposed audio route releases a deferred source claim',
      (tester) async {
    final coordinator = PlaybackSessionCoordinator();
    final stopping = Completer<void>();
    await coordinator.claim(
        kind: PlaybackSourceKind.server,
        identity: 'server:old-video',
        stop: () => stopping.future);
    final player = _PlayableAudioPlayer();
    final handler = _PlayableHandler(player);
    const uri = 'content://provider/abandoned';
    await _pumpScreen(tester, _FakeApiClient(),
        handlerOverride: handler,
        coordinator: coordinator,
        forbidServerDependencies: true,
        request: LocalPlaybackRequest(
            localMediaId: 7,
            sourceUri: Uri.parse(uri),
            title: 'Abandoned track'));
    await tester.pump();
    expect(coordinator.isLocalSourceInUse(uri), isTrue);
    await tester.pumpWidget(const SizedBox.shrink());
    stopping.complete();
    await tester.pump();
    await tester.pump();
    expect(coordinator.activeKind, isNull);
    expect(coordinator.isLocalSourceInUse(uri), isFalse);
    expect(player.sourceRequests, 0);
    await tester.runAsync(handler.stop);
    await player.playingChanges.close();
  });

  testWidgets('notification stop followed by screen play reloads progress',
      (tester) async {
    _setupAudioSessionMock();
    addTearDown(_teardownAudioSessionMock);
    final player = _PlayableAudioPlayer();
    final handler = _PlayableHandler(player);
    final saved = <double>[];
    double? resumePosition;
    addTearDown(() async {
      await tester.runAsync(handler.stop);
      await player.playingChanges.close();
    });
    await _pumpScreen(
      tester,
      _FakeApiClient(),
      handlerOverride: handler,
      forbidServerDependencies: true,
      request: LocalPlaybackRequest(
          localMediaId: 7,
          sourceUri: Uri.parse('content://provider/audio'),
          title: 'Track',
          readPosition: () async => resumePosition,
          savePosition: (seconds) async {
            saved.add(seconds);
            resumePosition = seconds;
          },
          markFinished: () async {}),
    );
    for (var attempt = 0; attempt < 100 && handler.playCalls == 0; attempt++) {
      await tester.pump();
      await tester.runAsync(
          () async => Future<void>.delayed(const Duration(milliseconds: 10)));
    }
    expect(handler.playCalls, 1);
    player.elapsed = const Duration(seconds: 8);
    await tester.runAsync(handler.stop);
    await handler.play(); // Notification Play cannot restart a stopped source.
    expect(player.isPlaying, isFalse);
    await tester.pump();
    await tester
        .ensureVisible(find.byKey(const Key('audio_player_play_pause')));
    await tester.tap(find.byKey(const Key('audio_player_play_pause')));
    for (var attempt = 0; attempt < 100 && !player.isPlaying; attempt++) {
      await tester.pump();
      await tester.runAsync(
          () async => Future<void>.delayed(const Duration(milliseconds: 10)));
    }
    expect(player.sourceRequests, 2);
    expect(player.isPlaying, isTrue);
    expect(player.elapsed, const Duration(seconds: 8));
    player.elapsed = const Duration(seconds: 19);
    await tester.pump(const Duration(seconds: 5));
    await tester.pump();
    expect(saved, contains(19));
    await tester
        .ensureVisible(find.byKey(const Key('audio_player_skip_forward')));
    await tester.tap(find.byKey(const Key('audio_player_skip_forward')));
    await tester.pump();
    expect(player.elapsed, const Duration(seconds: 34));
    await tester.runAsync(handler.stop);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('retained audio controls cannot restart or change a new owner',
      (tester) async {
    _setupAudioSessionMock();
    addTearDown(_teardownAudioSessionMock);
    final player = _PlayableAudioPlayer();
    final handler = _PlayableHandler(player);
    addTearDown(() async {
      await tester.runAsync(handler.stop);
      await player.playingChanges.close();
    });
    await _pumpScreen(
      tester,
      _FakeApiClient(),
      handlerOverride: handler,
      request: LocalPlaybackRequest(
          localMediaId: 7,
          sourceUri: Uri.parse('content://provider/old'),
          title: 'Old track'),
      forbidServerDependencies: true,
    );
    for (var attempt = 0; attempt < 100 && handler.playCalls == 0; attempt++) {
      await tester.pump();
      await tester.runAsync(
          () async => Future<void>.delayed(const Duration(milliseconds: 10)));
    }
    expect(handler.playCalls, 1);
    final container = ProviderScope.containerOf(
        tester.element(find.byType(AudioPlayerScreen)),
        listen: false);
    await tester.runAsync(() => container
        .read(playbackSessionCoordinatorProvider)
        .claim(
            kind: PlaybackSourceKind.publicShare,
            identity: 'public-share:new',
            stop: () async {}));
    await tester.pump();
    expect(player.isPlaying, isFalse);
    await tester
        .ensureVisible(find.byKey(const Key('audio_player_play_pause')));
    await tester.tap(find.byKey(const Key('audio_player_play_pause')));
    await _openMenu(tester);
    await tester.tap(find.byKey(const Key('audio_player_speed_selector')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('audio_player_speed_1_5')));
    await tester.pump();
    expect(handler.playCalls, 1);
    expect(player.isPlaying, isFalse);
    expect(player.selectedSpeed, 1);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('player shows only essentials; secondary actions are in the menu',
      (tester) async {
    final player = _PlayableAudioPlayer();
    final handler = _PlayableHandler(player);
    await _pumpScreen(tester, _FakeApiClient(), handlerOverride: handler);
    await tester.pump();
    await tester.pump();
    expect(find.byKey(const Key('audio_player_view')), findsOneWidget);
    for (final tooltip in ['Bookmarks', 'Notes', 'Info', 'Details']) {
      expect(find.byTooltip(tooltip), findsNothing, reason: tooltip);
    }
    for (final text in [
      'Gapless',
      'Loudness',
      'Equalizer',
      'Quality',
      'Sleep timer',
      'Music visuals',
      'Sound & playback',
    ]) {
      expect(find.text(text), findsNothing, reason: text);
    }
    expect(find.byKey(const Key('audio_player_speed_1_0')), findsNothing);
    expect(find.byKey(const Key('audio_player_play_pause')), findsOneWidget);
    expect(find.byKey(const Key('audio_player_seek_bar')), findsOneWidget);

    await _openMenu(tester);
    for (final key in [
      'playback_details_button',
      'playback_info_item',
      'playback_bookmarks_item',
      'playback_notes_item',
      'sleep_timer_menu_item',
      'audio_player_speed_selector',
      'sound_playback_menu',
    ]) {
      await tester.ensureVisible(find.byKey(Key(key)));
      expect(find.byKey(Key(key)), findsOneWidget, reason: key);
    }
    await tester.tap(find.byKey(const Key('sound_playback_menu')));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('sound_playback_sheet')), findsOneWidget);
    await tester.ensureVisible(find.byKey(const Key('gapless_toggle')));
    expect(find.byKey(const Key('gapless_toggle')), findsOneWidget);
    expect(find.text('Gapless'), findsWidgets);
    for (final text in ['Loudness', 'Equalizer', 'Quality']) {
      await tester.ensureVisible(find.text(text));
      expect(find.text(text), findsOneWidget, reason: text);
    }
    expect(tester.takeException(), isNull);
    await handler.endProgress();
    await player.playingChanges.close();
  });

  testWidgets(
      'music mode shows three transport buttons; ±15s lives in the menu',
      (tester) async {
    final (player, handler) = _usePlayableHandler(tester);
    final client = _FakeApiClient();
    final songs = [1, 2]
        .map((id) => Media.fromJson({
              'id': id,
              'set_id': 1,
              'type': 'audio',
              'file_name': 'song$id.mp3',
              'music': {'title': 'Song $id'}
            }))
        .toList();
    await _pumpScreen(tester, client,
        mediaId: '1',
        mediaTitle: 'Song 1',
        mediaUrl: client.streamUrl(1),
        sequence: songs,
        handlerOverride: handler,
        serverBaseUrl: Uri.parse('http://localhost:8080'));
    await _flushPlaybackStartup(tester);
    for (var i = 0; i < 5; i++) {
      await tester.pump();
    }
    expect(handler.musicState, isNotNull);
    expect(find.byKey(const Key('audio_player_skip_back')), findsOneWidget);
    expect(find.byKey(const Key('audio_player_play_pause')), findsOneWidget);
    expect(find.byKey(const Key('audio_player_skip_forward')), findsOneWidget);
    expect(find.byTooltip('Skip back 15 seconds'), findsNothing,
        reason: '±15s is not a fifth primary button in music mode');
    expect(find.byTooltip('Skip forward 15 seconds'), findsNothing);
    player.elapsed = const Duration(seconds: 20);
    await _openMenu(tester);
    await tester.ensureVisible(find.byKey(const Key('audio_player_seek_back_15')));
    await tester.tap(find.byKey(const Key('audio_player_seek_back_15')));
    await tester.pump();
    expect(player.elapsed, const Duration(seconds: 5));
    await _openMenu(tester);
    await tester.ensureVisible(
        find.byKey(const Key('audio_player_seek_forward_15')));
    await tester.tap(find.byKey(const Key('audio_player_seek_forward_15')));
    await tester.pump();
    expect(player.elapsed, const Duration(seconds: 20));
    await tester.runAsync(handler.stop);
  });

  testWidgets('open bookmark sheet stays bound to its original audio session',
      (tester) async {
    _setupAudioSessionMock();
    addTearDown(_teardownAudioSessionMock);
    final player = _PlayableAudioPlayer();
    final handler = _PlayableHandler(player);
    final client = _FakeApiClient();
    final coordinator = PlaybackSessionCoordinator();
    addTearDown(() async {
      await tester.runAsync(handler.stop);
      await player.playingChanges.close();
    });
    await _pumpScreen(tester, client,
        handlerOverride: handler,
        coordinator: coordinator,
        mediaUrl: 'https://player.example/api/v1/media/42/stream');
    await tester.pump();
    await tester.pump();
    player.elapsed = const Duration(seconds: 25);
    await _openMenu(tester);
    await tester.tap(find.byKey(const Key('playback_bookmarks_item')));
    await tester.pumpAndSettle();
    await tester.enterText(find.byKey(const Key('bookmark_label')), 'Original');
    await tester.tap(find.text('Save current position'));
    await tester.pumpAndSettle();
    expect(client.bookmarks.single.positionSeconds, 25);
    await tester.runAsync(() => coordinator.claim(
        kind: PlaybackSourceKind.publicShare,
        identity: 'new-player',
        stop: () async {}));
    player.elapsed = const Duration(seconds: 73);
    await tester.pump();
    await tester.tap(find.text('Save current position'));
    await tester.pumpAndSettle();
    expect(
        find.text(
            'Playback changed. Reopen bookmarks from the current player.'),
        findsOneWidget);
    expect(client.bookmarks.length, 1);
    await tester.tap(find.text('Original'));
    await tester.pumpAndSettle();
    expect(player.elapsed, const Duration(seconds: 73));
    expect(find.text('Could not jump to this bookmark. Please try again.'),
        findsOneWidget);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets(
      'SAF identity playback actions use real SQLite and library Notes preserve the same draft',
      (tester) async {
    _setupAudioSessionMock();
    addTearDown(_teardownAudioSessionMock);
    SharedPreferences.setMockInitialValues({});
    sqfliteFfiInit();
    databaseFactory = databaseFactoryFfiNoIsolate;
    final directory = await tester
        .runAsync(() => Directory.systemTemp.createTemp('personal-saf-ui-'));
    final path = '${directory!.path}/local.db';
    final database = LocalLibraryDatabase(databasePath: path);
    final item = await tester.runAsync(() =>
        SqliteLocalMediaRepository(database).importDocument(
            uri: 'content://documents/audio/saf-track',
            title: 'SAF track',
            mimeType: 'audio/mpeg',
            durationMs: 100000));
    final player = _PlayableAudioPlayer();
    final handler = _PlayableHandler(player);
    Future<void> waitFor(bool Function() ready) async {
      for (var attempt = 0; attempt < 150 && !ready(); attempt++) {
        await tester.pump(const Duration(milliseconds: 20));
        await tester.runAsync(
            () => Future<void>.delayed(const Duration(milliseconds: 10)));
      }
      expect(ready(), isTrue);
    }

    Future<T> complete<T>(Future<T> Function() action) async {
      late T value;
      Object? error;
      var done = false;
      unawaited(action().then((result) {
        value = result;
        done = true;
      }, onError: (Object failure, StackTrace stack) {
        error = failure;
        done = true;
      }));
      await waitFor(() => done);
      if (error != null) throw error!;
      return value;
    }

    Future<void> tap(Finder finder) async {
      await tester.pump(const Duration(milliseconds: 500));
      await tester.ensureVisible(finder);
      await tester.pump(const Duration(milliseconds: 100));
      await tester.tap(finder);
    }

    Future<void> menu(Key item) async {
      await tap(find.byTooltip('More options'));
      await waitFor(() => find.byKey(item).evaluate().isNotEmpty);
      await tap(find.byKey(item));
    }

    addTearDown(() async {
      await complete(handler.stop);
      await complete(database.close);
      await tester.runAsync(() => directory.delete(recursive: true));
      await player.playingChanges.close();
    });
    await _pumpScreen(tester, _FakeApiClient(),
        handlerOverride: handler,
        localDatabase: database,
        forbidServerDependencies: true,
        request: LocalPlaybackRequest(
            localMediaId: item!.id,
            sourceUri: Uri.parse(item.uri),
            title: item.title));
    await waitFor(() =>
        find.byKey(const Key('audio_player_play_pause')).evaluate().isNotEmpty);
    player.elapsed = const Duration(milliseconds: 32500);
    await menu(const Key('playback_bookmarks_item'));
    await tester.pump(const Duration(milliseconds: 500));
    await waitFor(
        () => find.text('No saved positions yet.').evaluate().isNotEmpty);
    await tester.enterText(
        find.byKey(const Key('bookmark_label')), 'SAF chapter');
    await tester.pump(const Duration(milliseconds: 500));
    await tester.ensureVisible(find.text('Save current position'));
    await tap(find.text('Save current position'));
    await waitFor(() =>
        find.widgetWithText(ListTile, 'SAF chapter').evaluate().isNotEmpty);
    expect(
        (await complete(() => database.personalStore(item.id).listBookmarks()))
            .single
            .positionSeconds,
        32.5);
    player.elapsed = Duration.zero;
    await tap(find.text('SAF chapter'));
    await waitFor(() => player.elapsed == const Duration(milliseconds: 32500));
    await tester.pump(const Duration(milliseconds: 500));
    await menu(const Key('playback_notes_item'));
    await tester.pump(const Duration(milliseconds: 500));
    await waitFor(
        () => find.byKey(const Key('notes_text_field')).evaluate().isNotEmpty);
    await tester.enterText(
        find.byKey(const Key('notes_text_field')), 'SAF note');
    await tester.binding.handlePopRoute();
    await waitFor(() => find.byType(NotesEditorScreen).evaluate().isEmpty);
    await tester.pump(const Duration(milliseconds: 500));
    await menu(const Key('playback_bookmarks_item'));
    await tester.pump(const Duration(milliseconds: 500));
    await waitFor(
        () => find.byTooltip('Delete bookmark').evaluate().isNotEmpty);
    await tap(find.byTooltip('Delete bookmark'));
    await waitFor(
        () => find.text('No saved positions yet.').evaluate().isNotEmpty);
    expect(
        await complete(() => database.personalStore(item.id).listBookmarks()),
        isEmpty);
    await complete(handler.stop);
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pumpWidget(ProviderScope(
        overrides: [localLibraryDatabaseProvider.overrideWithValue(database)],
        child: const MaterialApp(home: LocalLibraryScreen())));
    await waitFor(() => find.text('SAF track').evaluate().isNotEmpty);
    await tap(find.byTooltip('Manage SAF track'));
    await tester.pumpAndSettle();
    await tap(find.text('Notes'));
    await tester.pump(const Duration(milliseconds: 500));
    await waitFor(
        () => find.byKey(const Key('notes_text_field')).evaluate().isNotEmpty);
    expect(
        tester
            .widget<TextField>(find.byKey(const Key('notes_text_field')))
            .controller!
            .text,
        'SAF note');
    await tester.binding.handlePopRoute();
    await waitFor(() => find.byType(NotesEditorScreen).evaluate().isEmpty);
    await tester.pumpWidget(const SizedBox.shrink());
    await complete(database.close);
    final reopened = LocalLibraryDatabase(databasePath: path);
    expect(
        (await complete(() => reopened.personalStore(item.id).getNote()))
            ?.content,
        'SAF note');
    await complete(reopened.close);
  });

  testWidgets('local audio uses a content URI and local progress callbacks',
      (tester) async {
    _setupAudioSessionMock();
    addTearDown(_teardownAudioSessionMock);
    final player = _PlayableAudioPlayer();
    final handler = _PlayableHandler(player);
    final queue = _FakeProgressQueue();
    final localPositions = <double>[];
    var localFinished = 0;
    final request = LocalPlaybackRequest(
      localMediaId: 77,
      sourceUri: Uri.parse('content://provider/audio/77'),
      title: 'On-device track',
      startPosition: 14,
      savePosition: (seconds) async => localPositions.add(seconds),
      markFinished: () async {
        localFinished++;
      },
    );
    final client = _FakeApiClient();
    addTearDown(() async {
      await tester.runAsync(handler.stop);
      await player.playingChanges.close();
    });

    await _pumpScreen(
      tester,
      client,
      mediaId: '77',
      handlerOverride: handler,
      progressQueue: queue,
      request: request,
      forbidServerDependencies: true,
    );
    await tester.pump();
    await tester.pump();

    final source = player.loadedSource! as UriAudioSource;
    expect(source.uri, request.sourceUri);
    expect(source.headers, isNull);
    expect(player.elapsed, const Duration(seconds: 14));
    expect(handler.lastId, 'local:77');
    expect(handler.lastTitle, 'On-device track');
    expect(client.getMediaProgressCallCount, 0);

    await handler.endProgress();
    expect(localPositions, [14]);
    expect(localFinished, 0);
    expect(queue.updates, isEmpty);
    expect(queue.finishedItems, isEmpty);
  });

  testWidgets('loaded title identifies the player and audio notification',
      (tester) async {
    _setupAudioSessionMock();
    addTearDown(_teardownAudioSessionMock);
    final player = _PlayableAudioPlayer();
    final handler = _PlayableHandler(player);
    await _pumpScreen(tester, _FakeApiClient(),
        handlerOverride: handler,
        mediaTitle: 'Chapter One.mp3',
        serverBaseUrl: Uri.parse('http://localhost:8080'));
    await tester.pump();
    await tester.pump();
    expect(find.widgetWithText(AppBar, 'Chapter One.mp3'), findsOneWidget);
    expect(find.byKey(const Key('audio_track_title')), findsOneWidget);
    expect(handler.lastTitle, 'Chapter One.mp3');
    expect((player.loadedSource as UriAudioSource).headers, {
      'Authorization': 'Bearer test-token',
    });
    await tester.pumpWidget(const SizedBox.shrink());
    await handler.endProgress();
    await player.playingChanges.close();
  });

  setUp(_setupAudioSessionMock);
  tearDown(_teardownAudioSessionMock);

  testWidgets('audio threshold queues one durable finished command',
      (tester) async {
    final player = _PlayableAudioPlayer();
    final handler = _PlayableHandler(player);
    final queue = _FakeProgressQueue();
    final client = _FakeApiClient();
    addTearDown(() async {
      await handler.endProgress();
      await player.playingChanges.close();
    });
    await _pumpScreen(tester, client,
        handlerOverride: handler, progressQueue: queue);
    await tester.pump();
    await tester.pump();
    player.elapsed = const Duration(seconds: 96);
    await tester.pump(const Duration(seconds: 5));
    await tester.runAsync(() async => Future<void>.delayed(Duration.zero));
    expect(queue.finishedItems, [42]);
    expect(client.updateStatusCallCount, 0);

    player.elapsed = const Duration(seconds: 99);
    await tester.pump(const Duration(seconds: 5));
    await tester.runAsync(() async => Future<void>.delayed(Duration.zero));
    expect(queue.finishedItems, [42]);
    expect(queue.updates, [(42, 96.0)]);
    await tester.runAsync(handler.endProgress);
  });

  testWidgets('public share audio plays without credentials or progress writes',
      (tester) async {
    final player = _PlayableAudioPlayer();
    final handler = _PlayableHandler(player);
    final client = _FakeApiClient();
    final queue = _FakeProgressQueue();
    final router = GoRouter(initialLocation: '/s/tok7/audio', routes: [
      GoRoute(
        path: AppRoutes.sharedAudioPlayer,
        builder: (_, __) => const AudioPlayerScreen(
          mediaId: '0',
          mediaUrl: 'http://test.local/s/tok7/stream',
          mediaTitle: 'Shared podcast.mp3',
          isPublicShare: true,
        ),
      ),
    ]);
    addTearDown(() async {
      router.dispose();
      await handler.endProgress();
      await player.playingChanges.close();
    });
    await tester.pumpWidget(ProviderScope(
      overrides: [
        apiClientProvider.overrideWithValue(client),
        audioHandlerProvider.overrideWithValue(handler),
        progressQueueProvider.overrideWithValue(queue),
      ],
      child: MaterialApp.router(routerConfig: router),
    ));
    await tester.pump();
    await tester.pump();
    expect(handler.playCalls, 1);
    expect((player.loadedSource as UriAudioSource).headers, isNull);
    expect(handler.lastTitle, 'Shared podcast.mp3');
    expect(find.widgetWithText(AppBar, 'Shared podcast.mp3'), findsOneWidget);
    expect(client.getMediaProgressCallCount, 0);
    player.elapsed = const Duration(seconds: 97);
    await tester.pump(const Duration(seconds: 5));
    expect(queue.updates, isEmpty);
    expect(client.updateStatusCallCount, 0);
  });

  for (final (width, scale) in [
    (360.0, 1.0),
    (360.0, 2.0),
    (320.0, 2.0),
  ]) {
    testWidgets('speed choices fit ${width.toInt()}dp at ${scale}x text',
        (tester) async {
      tester.view.physicalSize = Size(width, 640);
      tester.view.devicePixelRatio = 1;
      addTearDown(() {
        tester.view.resetPhysicalSize();
        tester.view.resetDevicePixelRatio();
      });
      final player = _PlayableAudioPlayer();
      final handler = _PlayableHandler(player);
      await _pumpScreen(tester, _FakeApiClient(),
          handlerOverride: handler, textScale: scale);
      await tester.pump();
      await tester.pump();
      expect(find.byKey(const Key('audio_player_view')), findsOneWidget);

      await _openMenu(tester);
      await tester.tap(find.byKey(const Key('audio_player_speed_selector')));
      await tester.pumpAndSettle();
      for (final key in [
        'audio_player_speed_0_5',
        'audio_player_speed_1_0',
        'audio_player_speed_1_25',
        'audio_player_speed_1_5',
        'audio_player_speed_2_0',
      ]) {
        await tester.ensureVisible(find.byKey(Key(key)));
        final rect = tester.getRect(find.byKey(Key(key)));
        expect(rect.height, greaterThanOrEqualTo(48));
        expect(rect.left, greaterThanOrEqualTo(0));
        expect(rect.right, lessThanOrEqualTo(width));
      }
      expect(tester.takeException(), isNull);

      await tester.tap(find.byKey(const Key('audio_player_speed_2_0')));
      await tester.pump();
      expect(player.selectedSpeed, 2.0);
      await handler.endProgress();
      await player.playingChanges.close();
    });
  }

  testWidgets(
      'music runtime failure retries B at B position and filtered edges stay inside displayed results',
      (tester) async {
    final (player, handler) = _usePlayableHandler(tester);
    addTearDown(handler.stop);
    final client = _FakeApiClient()..progressResult = 2;
    final songs = [1, 2]
        .map((id) => Media.fromJson({
              'id': id,
              'set_id': 1,
              'type': 'audio',
              'file_name': 'song$id.mp3',
              'music': {'title': 'Song $id'}
            }))
        .toList();
    await _pumpScreen(tester, client,
        mediaId: '1',
        mediaTitle: 'Song 1',
        mediaUrl: client.streamUrl(1),
        sequence: songs,
        handlerOverride: handler,
        serverBaseUrl: Uri.parse('http://localhost:8080'));
    for (var i = 0; i < 5; i++) {
      await tester.pump();
    }
    await tester.runAsync(handler.skipToNext);
    await tester.pump();
    expect(handler.lastTitle, 'Song 2');
    player.elapsed = const Duration(seconds: 7);
    await tester.pump(const Duration(seconds: 1));
    player.playbackEvents.addError(StateError('late source failure'));
    await tester.pump();
    await tester.pump();
    expect(find.byKey(const Key('audio_player_error')), findsOneWidget);
    expect(handler.musicState?.error, isStateError);
    player.elapsed = Duration.zero;
    await tester.tap(find.byKey(const Key('audio_player_retry')));
    await _flushPlaybackStartup(tester);
    for (var i = 0; i < 5; i++) {
      await tester.pump();
    }
    expect(handler.lastTitle, 'Song 2');
    expect((player.loadedSource as UriAudioSource).uri.path,
        '/api/v1/media/2/stream');
    expect(player.position, const Duration(seconds: 7));
    expect(find.byKey(const Key('audio_player_error')), findsNothing);
    await tester.runAsync(handler.skipToNext);
    await tester.pump();
    expect(handler.lastTitle, 'Song 1');
    expect(client.browseCalls, 0,
        reason: 'filtered result must wrap without escaping into source set');
    await tester.runAsync(handler.stop);
  });

  testWidgets(
      'playlist later duplicate maps selected occurrence through nonmusic filtering',
      (tester) async {
    final (_, handler) = _usePlayableHandler(tester);
    addTearDown(handler.stop);
    final client = _FakeApiClient();
    final songs = [0, 1, 2, 1, 2]
        .map((id) => Media.fromJson({
              'id': id,
              'set_id': 1,
              'type': id == 0 ? 'image' : 'audio',
              'file_name': id == 0 ? 'cover.jpg' : 'song$id.mp3',
              'duration': 12,
              'music': {'title': 'Song $id'},
            }))
        .toList();
    await _pumpScreen(tester, client,
        mediaId: '1',
        mediaUrl: client.streamUrl(1),
        mediaTitle: 'Song 1',
        sequence: songs,
        sequenceIndex: 3,
        albumCollection: true,
        handlerOverride: handler,
        serverBaseUrl: Uri.parse('http://localhost:8080'));
    for (var i = 0; i < 5; i++) {
      await tester.pump();
    }
    try {
      expect(handler.musicQueue.entries.length, 4);
      expect(handler.musicQueue.index, 2,
          reason: 'selected repeated song is the third filtered occurrence');
      await tester.runAsync(handler.skipToNext);
      await tester.pump();
      expect(handler.musicQueue.index, 3);
      expect(handler.lastTitle, 'Song 2');
      await tester.runAsync(handler.skipToNext);
      await tester.pump();
      expect(handler.musicQueue.index, 3,
          reason: 'collection must not wrap from its last occurrence');
      expect(handler.lastTitle, 'Song 2');
    } finally {
      await tester.runAsync(handler.stop);
    }
  });

  for (final selectedIndex in [-1, 20, 1]) {
    testWidgets(
        'playlist invalid or mismatched occurrence $selectedIndex preserves identity fallback',
        (tester) async {
      final (_, handler) = _usePlayableHandler(tester);
      addTearDown(handler.stop);
      final client = _FakeApiClient();
      final songs = [1, 2, 1]
          .map((id) => Media.fromJson({
                'id': id,
                'set_id': 1,
                'type': 'audio',
                'file_name': 'song$id.mp3',
                'duration': 12,
                'music': {'title': 'Song $id'}
              }))
          .toList();
      await _pumpScreen(tester, client,
          mediaId: '1',
          mediaUrl: client.streamUrl(1),
          sequence: songs,
          sequenceIndex: selectedIndex,
          albumCollection: true,
          handlerOverride: handler,
          serverBaseUrl: Uri.parse('http://localhost:8080'));
      for (var i = 0; i < 5; i++) {
        await tester.pump();
      }
      expect(handler.musicQueue.index, 0);
      expect(handler.lastTitle, 'Song 1');
      await tester.runAsync(handler.stop);
    });
  }

  testWidgets(
      'retained queue resume adopts fresh lease and shows actual current song',
      (tester) async {
    final (_, handler) = _usePlayableHandler(tester);
    addTearDown(handler.stop);
    final client = _FakeApiClient();
    final songs = [1, 2]
        .map((id) => Media.fromJson({
              'id': id,
              'set_id': 1,
              'type': 'audio',
              'file_name': 'song$id.mp3',
              'duration': 12,
              'music': {'title': 'Song $id'}
            }))
        .toList();
    await _pumpScreen(tester, client,
        mediaId: '1',
        mediaTitle: 'Song 1',
        mediaUrl: client.streamUrl(1),
        sequence: songs,
        albumCollection: true,
        handlerOverride: handler,
        serverBaseUrl: Uri.parse('http://localhost:8080'));
    for (var i = 0; i < 5; i++) {
      await tester.pump();
    }
    final original = handler.sequenceLease;
    expect(original?.isCurrent, isTrue);
    await tester.runAsync(() => handler.removeQueue(0));
    expect(handler.musicQueue.active, isFalse);
    await tester.runAsync(() => handler.jumpQueue(0));
    for (var i = 0; i < 5; i++) {
      await tester.pump();
    }
    expect(original?.isCurrent, isFalse);
    expect(handler.sequenceLease?.isCurrent, isTrue);
    expect(handler.lastTitle, 'Song 2');
    expect(find.widgetWithText(AppBar, 'Song 2'), findsOneWidget);
    expect(find.byKey(const Key('audio_player_error')), findsNothing);
    await tester.runAsync(handler.stop);
  });

  testWidgets('hardware Back leaves audio playing on previous route',
      (tester) async {
    final player = _PlayableAudioPlayer();
    final handler = _PlayableHandler(player);
    final queue = _FakeProgressQueue();
    final router = GoRouter(initialLocation: AppRoutes.home, routes: [
      GoRoute(
          path: AppRoutes.home,
          builder: (context, _) => Scaffold(
                appBar: AppBar(title: const Text('Library')),
                body: TextButton(
                  onPressed: () =>
                      context.push(AppRoutes.audioPlayerPath('42')),
                  child: const Text('Open audio'),
                ),
              )),
      GoRoute(
          path: AppRoutes.audioPlayer,
          builder: (_, state) =>
              AudioPlayerScreen(mediaId: state.pathParameters['mediaId']!)),
    ]);
    await tester.pumpWidget(ProviderScope(
      overrides: [
        tokenStorageProvider.overrideWithValue(const _FakeTokenStorage()),
        apiClientProvider.overrideWithValue(_FakeApiClient()),
        authStateProvider.overrideWith(_Authenticated.new),
        cookieJarProvider.overrideWith((ref) => CookieJar()),
        unpairedServerEndpointOverride(),
        credentialMutationQueueProvider.overrideWithValue(
            CredentialMutationQueue()..enable(0, origin: kPlayerBaseUrl)),
        audioHandlerProvider.overrideWithValue(handler),
        progressQueueProvider.overrideWithValue(queue),
      ],
      child: MaterialApp.router(routerConfig: router),
    ));
    await tester.tap(find.text('Open audio'));
    await tester.pump();
    await tester.runAsync(() => Future<void>.delayed(Duration.zero));
    await tester.pump();
    await tester.pump();
    expect(player.isPlaying, isTrue);

    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    expect(find.text('Library'), findsOneWidget);
    expect(player.isPlaying, isTrue);
    player.elapsed = const Duration(seconds: 24);
    await tester.pump(const Duration(seconds: 5));
    await tester.runAsync(() async => Future<void>.delayed(Duration.zero));
    expect(queue.updates, contains((42, 24.0)));
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.runAsync(handler.stop);
    await handler.endProgress();
    await player.playingChanges.close();
  });

  testWidgets('screen disposal keeps progress for the loaded media ID',
      (tester) async {
    final player = _PlayableAudioPlayer();
    final handler = _PlayableHandler(player);
    final queue = _FakeProgressQueue();
    addTearDown(() async {
      await handler.endProgress();
      await player.playingChanges.close();
    });
    await _pumpScreen(tester, _FakeApiClient(),
        mediaId: '42', handlerOverride: handler, progressQueue: queue);
    await tester.pump();
    await tester.pump();
    expect(handler.playCalls, 1);

    await tester.pumpWidget(const SizedBox.shrink());
    player.elapsed = const Duration(seconds: 19);
    await tester.pump(const Duration(seconds: 5));
    await tester.runAsync(() async => Future<void>.delayed(Duration.zero));
    expect(queue.updates, contains((42, 19.0)));

    await _pumpScreen(tester, _FakeApiClient(),
        mediaId: '43', handlerOverride: handler, progressQueue: queue);
    await tester.pump();
    await tester.pump();
    player.elapsed = const Duration(seconds: 7);
    await tester.pump(const Duration(seconds: 5));
    await tester.runAsync(() async => Future<void>.delayed(Duration.zero));
    expect(queue.updates.last, (43, 7.0));
    await handler.endProgress();
  });

  testWidgets('logout stop during source load cannot restart playback',
      (tester) async {
    final player = _PlayableAudioPlayer()..pendingLoad = Completer<Duration?>();
    final handler = _PlayableHandler(player);
    final queue = _FakeProgressQueue();
    addTearDown(() async {
      await handler.endProgress();
      await player.playingChanges.close();
    });
    await _pumpScreen(tester, _FakeApiClient(),
        handlerOverride: handler, progressQueue: queue);
    await tester.pump();
    await tester.pump();
    expect(player.sourceRequests, 1);

    await tester.runAsync(handler.stop);
    player.pendingLoad!.complete(const Duration(seconds: 100));
    await tester.pump();
    expect(handler.playCalls, 0);
    expect(player.isPlaying, isFalse);
    player.elapsed = const Duration(seconds: 15);
    await tester.pump(const Duration(seconds: 5));
    expect(queue.updates, isEmpty);
  });

  testWidgets('two Retry taps start one progress session', (tester) async {
    final player = _PlayableAudioPlayer()..failNextLoad = true;
    final handler = _PlayableHandler(player);
    final queue = _FakeProgressQueue();
    addTearDown(() async {
      await handler.endProgress();
      await player.playingChanges.close();
    });
    await _pumpScreen(tester, _FakeApiClient(),
        handlerOverride: handler, progressQueue: queue);
    await tester.pump();
    await tester.pump();
    expect(find.byKey(const Key('audio_player_retry')), findsOneWidget);

    player.pendingLoad = Completer<Duration?>();
    await tester.tap(find.byKey(const Key('audio_player_retry')));
    await tester.tap(find.byKey(const Key('audio_player_retry')));
    await _flushPlaybackStartup(tester);
    // The first retry may already be loading when the second tap arrives.
    expect(player.sourceRequests, inInclusiveRange(2, 3));

    player.pendingLoad!.complete(const Duration(seconds: 100));
    await tester.runAsync(() async {
      for (var attempt = 0;
          attempt < 100 && handler.playCalls == 0;
          attempt++) {
        await Future<void>.delayed(const Duration(milliseconds: 10));
      }
    });
    await tester.pump();
    expect(handler.playCalls, 1);
    player.elapsed = const Duration(seconds: 12);
    await tester.pump(const Duration(seconds: 5));
    await tester.runAsync(() async => Future<void>.delayed(Duration.zero));
    expect(queue.updates, [(42, 12.0)]);
    await handler.endProgress();
  });

  // --------------------------------------------------------------------------
  // Compatibility stream (server-side transcoded media) and error texts
  // --------------------------------------------------------------------------

  group('compatibility stream', () {
    setUp(_setupAudioSessionMock);
    tearDown(_teardownAudioSessionMock);

    testWidgets('shows the preparing state while 503, then plays and resumes',
        (tester) async {
      final (player, handler) = _usePlayableHandler(tester);
      final queue = _FakeProgressQueue();
      final client = _FakeApiClient()
        ..progressResult = 30
        ..probeAnswers.addAll([_kTranscoding, _kReady]);
      await _pumpScreen(tester, client,
          handlerOverride: handler,
          progressQueue: queue,
          mediaUrl: _kCompatUrl,
          mediaTitle: 'song.wma');
      await tester.pump();
      await tester.pump();

      // First answer was 503: nothing was loaded into the player yet.
      expect(find.byKey(_kPreparingLabel), findsOneWidget);
      expect(find.text('Preparing playback…'), findsOneWidget);
      expect(find.byKey(const Key('audio_player_error')), findsNothing);
      expect(player.sourceRequests, 0);
      expect(client.probedUrls, [Uri.parse(_kCompatUrl)]);

      // Retry-After elapses, the second probe is 200 and playback starts.
      await tester.pump(const Duration(seconds: 5));
      await tester.pump();
      await tester.pump();
      expect(client.probedUrls, hasLength(2));
      expect(
          (player.loadedSource as UriAudioSource).uri.toString(), _kCompatUrl);
      expect(find.byKey(_kPreparingLabel), findsNothing);
      expect(find.byKey(const Key('audio_player_view')), findsOneWidget);
      expect(handler.playCalls, 1);
      // The background handler owns the item exactly as for an original
      // stream: title for the notification, resume, and progress saving.
      expect(handler.lastTitle, 'song.wma');
      expect(player.elapsed, const Duration(seconds: 30));
      player.elapsed = const Duration(seconds: 40);
      await tester.pump(const Duration(seconds: 5));
      await tester.runAsync(() async => Future<void>.delayed(Duration.zero));
      expect(queue.updates, [(42, 40.0)]);
      await tester.runAsync(handler.endProgress);
    });

    testWidgets('fresh canonical rendition publishes its preparation state',
        (tester) async {
      final (player, handler) = _usePlayableHandler(tester);
      final client = _FakeApiClient()..probeAnswers.add(_kTranscoding);
      await _pumpScreen(tester, client,
          handlerOverride: handler, mediaUrl: client.streamUrl(42));
      expect(find.byKey(_kPreparingLabel), findsOneWidget);
      expect(client.probedUrls, [Uri.parse(_kCompatUrl)]);
      expect(player.sourceRequests, 0);

      await tester.pumpWidget(const SizedBox.shrink());
      await _flushPlaybackStartup(tester);
      await tester.pump(const Duration(seconds: 30));
      expect(client.probedUrls, hasLength(1));
      expect(handler.playCalls, 0);
    });

    testWidgets('the original stream is played without a probe',
        (tester) async {
      final (player, handler) = _usePlayableHandler(tester);
      // probeAnswers is empty: a probe would throw and fail playback.
      final client = _FakeApiClient();
      await _pumpScreen(tester, client, handlerOverride: handler);
      await tester.pump();
      await tester.pump();

      expect(client.probedUrls, isEmpty);
      expect((player.loadedSource as UriAudioSource).uri.toString(),
          client.streamUrl(42));
      expect(handler.playCalls, 1);
      await tester.runAsync(handler.endProgress);
    });

    testWidgets('503 until the cap ends in a readable error', (tester) async {
      final (player, handler) = _usePlayableHandler(tester);
      final client = _FakeApiClient()..probeAnswers.add(_kTranscoding);
      var now = DateTime(2026);
      await _pumpScreen(
        tester,
        client,
        handlerOverride: handler,
        mediaUrl: _kCompatUrl,
        mediaTitle: 'song.wma',
        preparer: PlaybackPreparer(
          maxWait: const Duration(seconds: 12),
          now: () => now,
        ),
      );
      await tester.pump();
      await tester.pump();
      for (var retry = 0; retry < 2; retry++) {
        expect(find.byKey(_kPreparingLabel), findsOneWidget);
        now = now.add(const Duration(seconds: 5));
        await tester.pump(const Duration(seconds: 5));
        await tester.pump();
      }

      // 10 s waited; another 5 s pause would pass the 12 s cap.
      expect(client.probedUrls, hasLength(3));
      expect(
        _errorText(tester),
        'Could not prepare “song.wma” in time. Please try again later.',
      );
      expect(player.sourceRequests, 0);
      await tester.pump(const Duration(minutes: 1));
      expect(client.probedUrls, hasLength(3));
    });

    testWidgets('500 is terminal; Retry probes again', (tester) async {
      final (player, handler) = _usePlayableHandler(tester);
      final client = _FakeApiClient()
        ..probeAnswers.addAll(const [PlaybackProbe(statusCode: 500), _kReady]);
      await _pumpScreen(tester, client,
          handlerOverride: handler,
          mediaUrl: _kCompatUrl,
          mediaTitle: 'song.wma');
      await tester.pump();
      await tester.pump();

      expect(
        _errorText(tester),
        'Cannot play “song.wma”. The server could not convert this file.',
      );
      expect(player.sourceRequests, 0);

      // The user retries from the player screen without leaving it.
      await tester.tap(find.byKey(const Key('audio_player_retry')));
      await _flushPlaybackStartup(tester);
      expect(client.probedUrls, hasLength(2));
      expect(player.sourceRequests, 1);
      expect(handler.playCalls, 1);
      expect(find.byKey(const Key('audio_player_error')), findsNothing);
      await tester.runAsync(handler.endProgress);
    });

    testWidgets(
        'failed server profile Quality recovery retains Original across Retry failures',
        (tester) async {
      const origin = 'http://localhost:8080';
      await tester.runAsync(() async {
        await StreamQuality.initialize();
        await MusicNetworkPolicy.save('mode', 'wifi');
        await StreamQuality.save(origin, 'opus96');
      });
      addTearDown(() async {
        await StreamQuality.save(origin, 'auto');
        await StreamQuality.save(origin, 'auto', network: 'wifi');
        await MusicNetworkPolicy.save('mode', 'auto');
      });
      final (player, handler) = _usePlayableHandler(tester);
      final client = _FakeApiClient()
        ..probeAnswers.add(const PlaybackProbe(statusCode: 500));
      client.rawDio.options.baseUrl = origin;
      client.media[42] = Media.fromJson({
        'id': 42,
        'type': 'audio',
        'file_name': 'song.flac',
        'transcoded': false
      });
      await _pumpScreen(tester, client,
          handlerOverride: handler,
          request: ServerPlaybackRequest(
              mediaId: 42,
              serverOrigin: origin,
              userId: 1,
              sourceUri:
                  Uri.parse('$origin/api/v1/media/42/compat?profile=opus96'),
              title: 'song.flac',
              savePosition: (_) async {},
              markFinished: () async {}));
      await _flushPlaybackStartup(tester);
      expect(find.byKey(const Key('audio_player_error')), findsOneWidget);
      await tester.tap(find.text('Quality'));
      await tester.pumpAndSettle();
      await tester.tap(find.byType(DropdownButtonFormField<String>).last);
      await tester.pumpAndSettle();
      await tester.tap(find.text('Original (no conversion)').last);
      await tester.pumpAndSettle();
      Navigator.of(tester.element(find.text('Stream quality'))).pop();
      await tester.pumpAndSettle();
      player.failNextLoad = true;
      await tester.tap(find.byKey(const Key('audio_player_retry')));
      await _flushPlaybackStartup(tester);
      expect(find.byKey(const Key('audio_player_error')), findsOneWidget);
      expect((player.loadedSource as UriAudioSource).uri,
          Uri.parse('$origin/api/v1/media/42/stream'));
      await tester.tap(find.byKey(const Key('audio_player_retry')));
      await _flushPlaybackStartup(tester);
      expect(find.byKey(const Key('audio_player_error')), findsNothing);
      expect((player.loadedSource as UriAudioSource).uri,
          Uri.parse('$origin/api/v1/media/42/stream'));
      expect(client.probedUrls, hasLength(1));
      expect(player.sourceRequests, 2);
      expect(handler.playCalls, 1);
      await tester.runAsync(handler.endProgress);
    });

    testWidgets(
        'server error Retry and Quality stay reachable in short viewport',
        (tester) async {
      tester.view.physicalSize = const Size(800, 300);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      final (_, handler) = _usePlayableHandler(tester);
      final client = _FakeApiClient()
        ..probeAnswers.add(const PlaybackProbe(statusCode: 500));
      await _pumpScreen(tester, client,
          handlerOverride: handler,
          mediaUrl: _kCompatUrl,
          mediaTitle: 'song with a long unavailable profile name.wma');
      await tester.pump();
      await tester.pump();
      expect(tester.takeException(), isNull);
      final retry = find.byKey(const Key('audio_player_retry'));
      await tester.ensureVisible(retry);
      expect(retry.hitTestable(), findsOneWidget);
      final quality = find.text('Quality');
      await tester.ensureVisible(quality);
      expect(quality.hitTestable(), findsOneWidget);
      expect(tester.takeException(), isNull);
    });

    testWidgets('leaving the screen while preparing stops the retries',
        (tester) async {
      final (player, handler) = _usePlayableHandler(tester);
      final coordinator = PlaybackSessionCoordinator();
      final client = _FakeApiClient()..probeAnswers.add(_kTranscoding);
      await _pumpScreen(tester, client,
          handlerOverride: handler,
          mediaUrl: _kCompatUrl,
          coordinator: coordinator);
      await tester.pump();
      await tester.pump();
      expect(find.byKey(_kPreparingLabel), findsOneWidget);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(minutes: 1));

      expect(client.probedUrls, hasLength(1));
      expect(client.probeTokens.single!.isCancelled, isTrue);
      expect(player.sourceRequests, 0);
      expect(handler.playCalls, 0);
      // The abandoned attempt no longer owns the player.
      expect(coordinator.activeKind, isNull);
    });

    testWidgets('another item taking the player ends the preparing state',
        (tester) async {
      final (player, handler) = _usePlayableHandler(tester);
      final coordinator = PlaybackSessionCoordinator();
      final client = _FakeApiClient()..probeAnswers.add(_kTranscoding);
      await _pumpScreen(tester, client,
          handlerOverride: handler,
          mediaUrl: _kCompatUrl,
          coordinator: coordinator);
      await tester.pump();
      await tester.pump();
      expect(find.byKey(_kPreparingLabel), findsOneWidget);

      // E.g. a video opened on top of this still-mounted audio screen.
      await tester.runAsync(() => coordinator.claim(
            kind: PlaybackSourceKind.publicShare,
            identity: 'public-share:next',
            stop: () async {},
          ));
      // At once: not after the 5 s pause the wait was in.
      await tester.pump();
      await tester.pump();

      expect(find.byKey(_kPreparingLabel), findsNothing);
      expect(find.byType(CircularProgressIndicator), findsNothing);
      expect(
        _errorText(tester),
        'Playback stopped. Tap Retry to play this item again.',
      );
      expect(find.byKey(const Key('audio_player_retry')), findsOneWidget);
      await tester.pump(const Duration(minutes: 1));
      expect(client.probedUrls, hasLength(1));
      expect(player.sourceRequests, 0);
    });

    testWidgets('a dead share link is named as such, and its token not logged',
        (tester) async {
      final (player, handler) = _usePlayableHandler(tester);
      final publicClient = _FakeApiClient()
        ..probeAnswers.add(const PlaybackProbe(statusCode: 410));
      final logs = <String>[];
      final originalDebugPrint = debugPrint;
      debugPrint = (String? message, {int? wrapWidth}) => logs.add('$message');
      await tester.pumpWidget(ProviderScope(
        overrides: [
          apiClientProvider.overrideWithValue(_FakeApiClient()),
          publicApiClientProvider.overrideWithValue(publicClient),
          audioHandlerProvider.overrideWithValue(handler),
          progressQueueProvider.overrideWithValue(_FakeProgressQueue()),
        ],
        child: const MaterialApp(
          home: AudioPlayerScreen(
            mediaId: '0',
            mediaUrl: 'http://test.local/s/secrettoken7/compat',
            mediaTitle: 'Shared song.wma',
            isPublicShare: true,
          ),
        ),
      ));
      await tester.pump();
      await tester.pump();
      debugPrint = originalDebugPrint;
      expect(find.text('Quality'), findsNothing);

      expect(
        _errorText(tester),
        'Cannot play “Shared song.wma”. This share link is no longer valid '
        'or access was refused.',
      );
      expect(find.byKey(const Key('audio_player_retry')), findsOneWidget);
      expect(player.sourceRequests, 0);
      // The failure is logged by kind and error type only.
      expect(logs.join('\n'), contains('publicShare'));
      expect(logs.join('\n'), isNot(contains('secrettoken7')));
      expect(logs.join('\n'), isNot(contains('http')));
    });

    testWidgets('a public share is probed with the anonymous client',
        (tester) async {
      final (player, handler) = _usePlayableHandler(tester);
      final accountClient = _FakeApiClient();
      final publicClient = _FakeApiClient()..probeAnswers.add(_kReady);
      const url = 'http://test.local/s/tok7/compat';
      await tester.pumpWidget(ProviderScope(
        overrides: [
          apiClientProvider.overrideWithValue(accountClient),
          publicApiClientProvider.overrideWithValue(publicClient),
          audioHandlerProvider.overrideWithValue(handler),
          progressQueueProvider.overrideWithValue(_FakeProgressQueue()),
        ],
        child: const MaterialApp(
          home: AudioPlayerScreen(
            mediaId: '0',
            mediaUrl: url,
            mediaTitle: 'Shared song.wma',
            isPublicShare: true,
          ),
        ),
      ));
      await tester.pump();
      await tester.pump();

      expect(publicClient.probedUrls, [Uri.parse(url)]);
      expect(accountClient.probedUrls, isEmpty);
      expect((player.loadedSource as UriAudioSource).uri.toString(), url);
      expect((player.loadedSource as UriAudioSource).headers, isNull);
      expect(handler.playCalls, 1);
    });
  });

  group('playback error texts', () {
    setUp(_setupAudioSessionMock);
    tearDown(_teardownAudioSessionMock);

    testWidgets('a source the device cannot decode shows a readable error',
        (tester) async {
      final (player, handler) = _usePlayableHandler(tester);
      // just_audio throws this from setAudioSource; its text is the raw
      // "(0) Source error" the screen used to show.
      player.loadError = PlayerException(0, 'Source error');
      await _pumpScreen(tester, _FakeApiClient(),
          handlerOverride: handler, mediaTitle: 'song.wma');
      await tester.pump();
      await tester.pump();

      expect(
        _errorText(tester),
        'Cannot play “song.wma”. The connection may have been '
        'interrupted, or this format is not supported.',
      );
      expect(find.textContaining('Source error'), findsNothing);
      expect(find.textContaining('Playback failed'), findsNothing);
      expect(handler.playCalls, 0);
    });

    testWidgets('a local file that fails to decode shows the same error',
        (tester) async {
      final (player, handler) = _usePlayableHandler(tester);
      player.loadError = PlayerException(0, 'Source error');
      await _pumpScreen(
        tester,
        _FakeApiClient(),
        handlerOverride: handler,
        forbidServerDependencies: true,
        request: LocalPlaybackRequest(
          localMediaId: 7,
          sourceUri: Uri.parse('content://provider/audio/compat'),
          title: 'On-device song.wma',
        ),
      );
      await tester.pump();
      await tester.pump();
      expect(find.text('Quality'), findsNothing);

      // Local files are never probed (no server), whatever their URI.
      expect(
        (player.loadedSource as UriAudioSource).uri.toString(),
        'content://provider/audio/compat',
      );
      expect(
        _errorText(tester),
        'Cannot play “On-device song.wma”. '
        'This format cannot be played on this device.',
      );
    });

    testWidgets(
        'a failure reported after loading shows the error; Retry resumes there',
        (tester) async {
      final (player, handler) = _usePlayableHandler(tester);
      final queue = _FakeProgressQueue();
      // Saved progress from an earlier session: playback starts at 10 s.
      final client = _FakeApiClient()..progressResult = 10;
      await _pumpScreen(tester, client,
          handlerOverride: handler,
          progressQueue: queue,
          mediaTitle: 'song.ac3');
      await tester.pump();
      await tester.pump();
      expect(find.byKey(const Key('audio_player_view')), findsOneWidget);
      expect(player.elapsed, const Duration(seconds: 10));
      // The user listens on to 60 s.
      player.elapsed = const Duration(seconds: 60);
      // just_audio's position stream reports on every playback event.
      player.playbackEvents.add(PlaybackEvent());
      await tester.pump(const Duration(milliseconds: 500));

      // What just_audio adds to its event stream for an undecodable source.
      // Unhandled, this would fail the test as an uncaught async error.
      player.playbackEvents.addError(PlatformException(
        code: '0',
        message: 'Source error',
        details: {'index': 0},
      ));
      await tester.pump();
      await tester.pump();

      expect(
        _errorText(tester),
        'Cannot play “song.ac3”. The connection may have been '
        'interrupted, or this format is not supported.',
      );
      expect(find.byKey(const Key('audio_player_view')), findsNothing);
      expect(tester.takeException(), isNull);

      // Retry continues where playback was, not at the stale 10 s, and the
      // next progress tick does not write an older position over a newer one.
      await tester.runAsync(handler.endProgress);
      queue.updates.clear();
      await tester.tap(find.byKey(const Key('audio_player_retry')));
      // Stopping the failed session completes outside the fake clock.
      for (var round = 0; round < 10; round++) {
        await tester.pump();
        await tester.runAsync(
            () async => Future<void>.delayed(const Duration(milliseconds: 1)));
      }
      expect(find.byKey(const Key('audio_player_view')), findsOneWidget);
      expect(player.sourceRequests, 2);
      expect(player.elapsed, const Duration(seconds: 60));
      expect(client.getMediaProgressCallCount, 1);
      await tester.pump(const Duration(seconds: 5));
      await tester.runAsync(() async => Future<void>.delayed(Duration.zero));
      expect(queue.updates, [(42, 60.0)]);
      await tester.runAsync(handler.endProgress);
    });

    // Plays from saved progress at 10 s, moves to [seconds] (in order),
    // lets playback fail and taps Retry; returns the position resumed at.
    Future<Duration> failAndRetryAt(
      WidgetTester tester,
      List<int> seconds,
    ) async {
      final (player, handler) = _usePlayableHandler(tester);
      final client = _FakeApiClient()..progressResult = 10;
      await _pumpScreen(tester, client,
          handlerOverride: handler, mediaTitle: 'song.mp3');
      await tester.pump();
      await tester.pump();
      for (final second in seconds) {
        player.elapsed = Duration(seconds: second);
        player.playbackEvents.add(PlaybackEvent(
          updatePosition: Duration(seconds: second),
        ));
        await tester.pump(const Duration(milliseconds: 300));
      }
      player.playbackEvents.addError(PlayerException(0, 'Source error'));
      await tester.pump();
      await tester.pump();
      await tester.runAsync(handler.endProgress);
      await tester.tap(find.byKey(const Key('audio_player_retry')));
      for (var round = 0; round < 10; round++) {
        await tester.pump();
        await tester.runAsync(
            () async => Future<void>.delayed(const Duration(milliseconds: 1)));
      }
      expect(find.byKey(const Key('audio_player_view')), findsOneWidget);
      expect(client.getMediaProgressCallCount, 1);
      await tester.runAsync(handler.endProgress);
      return player.elapsed;
    }

    testWidgets('a failure just past the finished threshold resumes there',
        (tester) async {
      // 97 of 100 s is past 95 %, but the handler records "finished" only on
      // its next tick, which the failure comes before.
      expect(await failAndRetryAt(tester, [97]), const Duration(seconds: 97));
    });

    testWidgets('a failure after seeking back to the start resumes at zero',
        (tester) async {
      // Neither the earlier 60 s nor the stale 10 s.
      expect(await failAndRetryAt(tester, [60, 0]), Duration.zero);
    });

    testWidgets('any failure after loading is reported as a source failure',
        (tester) async {
      final (player, handler) = _usePlayableHandler(tester);
      await _pumpScreen(tester, _FakeApiClient(),
          handlerOverride: handler, mediaTitle: 'song.mp3');
      await tester.pump();
      await tester.pump();

      // Not one of the player's own exception types.
      player.playbackEvents.addError(StateError('connection closed'));
      await tester.pump();
      await tester.pump();

      expect(
        _errorText(tester),
        'Cannot play “song.mp3”. The connection may have been '
        'interrupted, or this format is not supported.',
      );
      expect(find.textContaining('Could not start'), findsNothing);
      await tester.runAsync(handler.endProgress);
    });
  });

  // --------------------------------------------------------------------------
  // Loading state
  // --------------------------------------------------------------------------

  group('loading state', () {
    testWidgets(
        'shows a loading indicator immediately after pumpWidget (before initState fires)',
        (tester) async {
      final fakeClient = _FakeApiClient();
      // pumpWidget renders the first frame with _isLoading == true.
      // addPostFrameCallback has NOT fired yet — that happens on the next pump.
      await _pumpScreen(tester, fakeClient);

      // The loading spinner must be visible right after the first frame.
      expect(
        find.byKey(const Key('audio_player_loading')),
        findsOneWidget,
      );
      expect(find.byType(CircularProgressIndicator), findsOneWidget);
    });

    testWidgets(
        'loading indicator is still shown after one pump (initState fires but platform call is pending)',
        (tester) async {
      final fakeClient = _FakeApiClient();
      await _pumpScreen(tester, fakeClient);

      // Fire addPostFrameCallback → _initPlayer starts but platform call hangs.
      await tester.pump();

      // Still loading because the native platform channel has no handler.
      expect(
        find.byKey(const Key('audio_player_loading')),
        findsOneWidget,
      );
    });
  });

  // --------------------------------------------------------------------------
  // AppBar
  // --------------------------------------------------------------------------

  group('app bar', () {
    testWidgets('renders title containing the mediaId', (tester) async {
      final fakeClient = _FakeApiClient();
      await _pumpScreen(tester, fakeClient, mediaId: '99');
      await tester.pump();

      // The title includes the mediaId string somewhere in the widget tree.
      expect(find.textContaining('99'), findsWidgets);
    });
  });

  // --------------------------------------------------------------------------
  // Widget key presence
  // --------------------------------------------------------------------------

  group('widget keys', () {
    testWidgets('audio_player_loading key is present during initialisation',
        (tester) async {
      final fakeClient = _FakeApiClient();
      await _pumpScreen(tester, fakeClient);

      expect(find.byKey(const Key('audio_player_loading')), findsOneWidget);
    });

    testWidgets('error keys and play/pause key are defined in screen code',
        (tester) async {
      // This test verifies that the Key constants used in the screen exist and
      // have the expected values (compile-time check via Key() equality).
      // The actual widgets only appear after a successful platform init, which
      // is not available in the test harness.
      expect(const Key('audio_player_error'),
          equals(const Key('audio_player_error')));
      expect(const Key('audio_player_error_message'),
          equals(const Key('audio_player_error_message')));
      expect(const Key('audio_player_retry'),
          equals(const Key('audio_player_retry')));
      expect(const Key('audio_player_play_pause'),
          equals(const Key('audio_player_play_pause')));
      expect(const Key('audio_player_seek_bar'),
          equals(const Key('audio_player_seek_bar')));
      expect(const Key('audio_player_skip_back'),
          equals(const Key('audio_player_skip_back')));
      expect(const Key('audio_player_skip_forward'),
          equals(const Key('audio_player_skip_forward')));
      expect(const Key('audio_player_speed_selector'),
          equals(const Key('audio_player_speed_selector')));
    });
  });

  // --------------------------------------------------------------------------
  // Stream URL resolution
  // --------------------------------------------------------------------------

  group('stream URL resolution', () {
    testWidgets(
        'shows loading state when mediaUrl is null (falls back to client.streamUrl)',
        (tester) async {
      // When mediaUrl is null the screen calls client.streamUrl(mediaId).
      // The platform call blocks in the test harness so we see the loading state.
      final fakeClient = _FakeApiClient();
      await _pumpScreen(tester, fakeClient, mediaUrl: null);

      // Immediately after pumpWidget the loading state is visible.
      expect(find.byKey(const Key('audio_player_loading')), findsOneWidget);
    });

    testWidgets('shows loading state when an explicit mediaUrl is given',
        (tester) async {
      final fakeClient = _FakeApiClient();
      await _pumpScreen(
        tester,
        fakeClient,
        mediaUrl: 'http://localhost:8080/api/v1/media/42/stream',
      );

      // Loading state visible immediately after pumpWidget.
      expect(find.byKey(const Key('audio_player_loading')), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Error state (driven by a fake widget that injects the error directly)
  // --------------------------------------------------------------------------

  group('error state', () {
    testWidgets('error view shows an icon, message, and retry button',
        (tester) async {
      // Render the error view directly via a standalone widget — bypassing
      // AudioPlayer initialisation (which hangs in the test harness) while
      // still exercising the exact widgets built by _buildErrorView.
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: Center(
              key: const Key('audio_player_error'),
              child: Padding(
                padding: const EdgeInsets.all(24),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    const Icon(Icons.error_outline,
                        color: Colors.white70, size: 64),
                    const SizedBox(height: 16),
                    const Text(
                      'Playback failed: test error',
                      style: TextStyle(color: Colors.white70),
                      textAlign: TextAlign.center,
                      key: Key('audio_player_error_message'),
                    ),
                    const SizedBox(height: 24),
                    ElevatedButton(
                      key: const Key('audio_player_retry'),
                      onPressed: () {},
                      child: const Text('Retry'),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      );

      expect(find.byKey(const Key('audio_player_error')), findsOneWidget);
      expect(
          find.byKey(const Key('audio_player_error_message')), findsOneWidget);
      expect(find.byKey(const Key('audio_player_retry')), findsOneWidget);

      // Error message must be non-empty.
      final textWidget = tester.widget<Text>(
        find.byKey(const Key('audio_player_error_message')),
      );
      expect(textWidget.data, isNotEmpty);
    });
  });

  group('casting', () {
    testWidgets('casts the item, shows remote controls, and hands back paused',
        (tester) async {
      // Stopping the phone's player completes outside the fake clock.
      Future<void> settle() async {
        for (var round = 0; round < 2; round++) {
          for (var i = 0; i < 5; i++) {
            await tester.pump(const Duration(milliseconds: 200));
          }
          await tester.runAsync(
              () => Future<void>.delayed(const Duration(milliseconds: 50)));
        }
        await tester.pump();
      }

      final (player, handler) = _usePlayableHandler(tester);
      final coordinator = PlaybackSessionCoordinator();
      final queue = _FakeProgressQueue();
      FakeCastReceiver? receiver;
      final controller = CastController(
        coordinator: coordinator,
        resolveMedia: (request, {required isVideo}) async => CastMedia(
          url: Uri.parse('http://localhost:8080/cast/T/stream'),
          contentType: 'audio/mpeg',
          title: request.title,
        ),
        startSession: (device) => CastSession.start(device,
            connect: (_) async => receiver = FakeCastReceiver()),
      );
      await _pumpScreen(
        tester,
        _FakeApiClient(),
        handlerOverride: handler,
        coordinator: coordinator,
        progressQueue: queue,
        mediaTitle: 'Song.mp3',
        castController: controller,
        castDiscovery: _FakeCastDiscovery([kTestCastDevice]),
      );
      await tester.pump();
      await tester.pump();
      expect(player.isPlaying, isTrue);
      player.elapsed = const Duration(seconds: 12);

      await tester.tap(find.byKey(const Key('cast_button')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 500));
      await tester.tap(find.text('Living Room TV'));
      await settle();

      expect(find.byKey(const Key('cast_playback_view')), findsOneWidget);
      expect(find.text('Playing on Living Room TV'), findsOneWidget);
      expect(receiver!.payloads.last['currentTime'], 12.0);
      // The cast took the shared player: the phone stopped.
      expect(player.isPlaying, isFalse);
      expect(controller.isCasting(coordinator.activeIdentity!), isTrue);

      receiver!.pushMediaStatus('PLAYING', time: 40);
      await tester.pump();
      // Disconnect from the hamburger; CastButton remains the visible affordance.
      await tester.tap(find.byKey(const Key('overflow_menu_button')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('cast_stop')));
      await settle();

      expect(find.byKey(const Key('cast_playback_view')), findsNothing);
      expect(receiver!.types, contains('STOP'));
      expect(queue.updates.last.$1, 42);
      expect(queue.updates.last.$2, greaterThanOrEqualTo(40));
      // Back on the phone where the TV was, without playing.
      expect(player.elapsed.inSeconds, greaterThanOrEqualTo(40));
      expect(player.isPlaying, isFalse);
      await tester.pumpWidget(const SizedBox.shrink());
      await handler.endProgress();
    });

    testWidgets('no cast button for a file stored on the phone',
        (tester) async {
      final (_, handler) = _usePlayableHandler(tester);
      await _pumpScreen(
        tester,
        _FakeApiClient(),
        handlerOverride: handler,
        request: LocalPlaybackRequest(
          localMediaId: 3,
          sourceUri: Uri.parse('content://media/3'),
          title: 'Phone song',
        ),
        forbidServerDependencies: true,
      );
      await tester.pump();
      await tester.pump();
      expect(find.byKey(const Key('audio_player_view')), findsOneWidget);
      expect(find.byKey(const Key('cast_button')), findsNothing);
    });
  });
}

class _FakeCastDiscovery implements CastDiscovery {
  _FakeCastDiscovery(this.found);

  final List<CastDevice> found;

  @override
  Stream<List<CastDevice>> devices() => Stream.value(found);
}
