// Widget tests for VideoPlayerScreen (video_player_screen.dart).
//
// Tests cover:
//   1. Loading indicator shown during the initial build before initState fires.
//   2. Error view rendered when VideoPlayerController.initialize() throws.
//   3. Retry button re-triggers initialisation and ends in an error state.
//   4. Screen renders the AppBar title containing the mediaId.
//   5. Stream URL resolution (route-extra URL and client.streamUrl fallback).
//   6. Compatibility stream: preparing state while the server answers 503,
//      terminal errors, leaving while preparing, and readable error texts.
//
// VideoPlayerController relies on native platform channels (ExoPlayer /
// AVPlayer) that are unavailable in the Flutter test harness.  We exploit the
// fact that VideoPlayerController.initialize() throws a MissingPluginException,
// turning every initialisation attempt into a predictable error path — which
// is exactly what we need for error-state coverage.
//
// Timing notes:
//   - [VideoPlayerScreen] uses addPostFrameCallback to start _initPlayer so
//     that Riverpod provider overrides are fully applied before the first read.
//   - pumpWidget() renders the first frame with _isLoading = true.
//   - pump() processes addPostFrameCallback → _initPlayer() → throws → error.
//   - Therefore the loading spinner is visible immediately after pumpWidget()
//     but NOT after a subsequent pump(); check it before pumping.
//
// Run with: flutter test test/screens/video_player_screen_test.dart

import 'dart:async';

import 'package:chewie/chewie.dart';
import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/playback_preparer_provider.dart';
import 'package:player_android/providers/progress_queue_provider.dart';
import 'package:player_android/providers/public_api_client_provider.dart';
import 'package:player_android/providers/playback_session_provider.dart';
import 'package:player_android/screens/video_player_screen.dart';
import 'package:player_android/services/progress_queue.dart';
import 'package:player_android/services/playback_preparer.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/services/playback_session_coordinator.dart';
import 'package:video_player_platform_interface/video_player_platform_interface.dart';

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

/// Controllable [PlayerApiClient] stub for [VideoPlayerScreen] tests.
///
/// Only the progress methods and [streamUrl] are implemented; all other
/// methods throw [UnimplementedError] to catch unexpected usage immediately.
class _FakeApiClient extends PlayerApiClient {
  _FakeApiClient() : super(dio: Dio());

  /// Records how many times [getMediaProgress] was called.
  int getMediaProgressCallCount = 0;

  /// When non-null, [getMediaProgress] returns this value.
  double? progressResult;

  /// Records how many times [updateProgress] was called.
  int updateProgressCallCount = 0;

  /// Records how many times [updateProgressStatus] was called.
  int updateProgressStatusCallCount = 0;

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
  }) async {
    updateProgressCallCount++;
  }

  @override
  Future<void> updateProgressStatus({
    required int mediaId,
    required String status,
  }) async {
    updateProgressStatusCallCount++;
  }

  /// Returns a synthetic stream URL so [VideoPlayerController.networkUrl] can
  /// be constructed even though it will fail to initialise (no platform).
  @override
  String streamUrl(int mediaId) =>
      'http://localhost:8080/api/v1/media/$mediaId/stream';
}

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

/// No-op [ProgressQueueBase] stub for widget tests.
///
/// Implements [ProgressQueueBase] directly rather than extending [ProgressQueue]
/// so no real SQLite database is opened and no connectivity subscription is
/// created in the test harness (Liskov Substitution — any [ProgressQueueBase]
/// can be injected wherever the interface is required).
class _FakeProgressQueue implements ProgressQueueBase {
  final positions = <(int, double)>[];
  final finishedItems = <int>[];
  @override
  Future<void> clearAndSuspend() async {}
  @override
  Future<void> init({ProgressScope? scope}) async {}

  @override
  Future<void> resume(ProgressScope scope) async {}

  @override
  Future<void> suspend() async {}

  @override
  Future<void> enqueue(int mediaId, double positionSeconds) async {
    positions.add((mediaId, positionSeconds));
  }

  @override
  Future<void> enqueueFinished(int mediaId) async {
    finishedItems.add(mediaId);
  }

  @override
  Future<void> dispose() async {}
}

class _PlayableVideoPlatform extends VideoPlayerPlatform {
  /// Events of the most recently created player. A single-subscription
  /// stream like the real plugin's, so every player gets its own.
  var events = StreamController<VideoEvent>();
  bool _created = false;
  Duration position = Duration.zero;
  DataSource? lastSource;
  Completer<void>? pendingPause;
  Completer<void>? pendingDispose;
  int playCalls = 0;
  int positionPolls = 0;
  int disposeCalls = 0;
  bool failInitialize = false;
  Object? createError;
  Duration duration = const Duration(seconds: 100);

  @override
  Future<void> init() async {}

  @override
  Future<int?> create(DataSource source) async {
    lastSource = source;
    if (createError != null) throw createError!;
    if (_created) events = StreamController<VideoEvent>();
    _created = true;
    Timer.run(() {
      if (failInitialize) {
        events.addError(
            PlatformException(code: 'unreadable', message: 'unreadable'));
        return;
      }
      events.add(VideoEvent(
        eventType: VideoEventType.initialized,
        duration: duration,
        size: const Size(400, 300),
      ));
    });
    return 1;
  }

  @override
  Stream<VideoEvent> videoEventsFor(int playerId) => events.stream;
  @override
  Future<void> dispose(int playerId) async {
    disposeCalls++;
    await pendingDispose?.future;
  }

  @override
  Future<void> play(int playerId) async {
    playCalls++;
  }

  @override
  Future<void> pause(int playerId) async {
    await pendingPause?.future;
  }

  @override
  Future<void> setLooping(int playerId, bool looping) async {}
  @override
  Future<void> setVolume(int playerId, double volume) async {}
  @override
  Future<void> setPlaybackSpeed(int playerId, double speed) async {}
  @override
  Future<void> seekTo(int playerId, Duration position) async {
    this.position = position;
  }

  @override
  Future<Duration> getPosition(int playerId) async {
    positionPolls++;
    return position;
  }

  @override
  Widget buildView(int playerId) => const SizedBox.shrink();
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/// Pumps [VideoPlayerScreen] for [mediaId] inside a [ProviderScope] with
/// overridden providers, backed by a minimal [GoRouter] for navigation.
///
/// [mediaUrl] may be supplied to exercise the route-extra URL path; when null
/// the screen falls back to [PlayerApiClient.streamUrl].
///
/// The returned widget is rendered after the first frame but BEFORE
/// addPostFrameCallback fires, so _isLoading is still true.
Future<void> _pumpScreen(
  WidgetTester tester,
  _FakeApiClient fakeClient, {
  String mediaId = '42',
  String? mediaUrl,
  String? mediaTitle,
  _FakeProgressQueue? progressQueue,
  PlaybackRequest? request,
  bool forbidServerDependencies = false,
  PlaybackSessionCoordinator? coordinator,
  PlaybackPreparer? preparer,
}) async {
  final router = GoRouter(
    initialLocation: '/video/$mediaId',
    routes: [
      GoRoute(
        path: '/video/:mediaId',
        builder: (context, state) => VideoPlayerScreen(
          mediaId: state.pathParameters['mediaId']!,
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
        if (coordinator != null)
          playbackSessionCoordinatorProvider.overrideWithValue(coordinator),
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
        // Override progressQueueProvider so no real SQLite DB is opened and
        // no connectivity subscription is created during widget tests.
        progressQueueProvider.overrideWith((ref) {
          if (forbidServerDependencies) throw StateError('Server queue read');
          return progressQueue ?? _FakeProgressQueue();
        }),
      ],
      child: MaterialApp.router(routerConfig: router),
    ),
  );
}

const _kCompatUrl = 'http://localhost:8080/api/v1/media/42/compat';
const _kPreparingLabel = Key('playback_preparing_label');
const _kTranscoding = PlaybackProbe(
  statusCode: 503,
  retryAfter: Duration(seconds: 5),
);

/// Installs a playable native video platform for one test.
_PlayableVideoPlatform _usePlayablePlatform() {
  final previousPlatform = VideoPlayerPlatform.instance;
  final platform = _PlayableVideoPlatform();
  VideoPlayerPlatform.instance = platform;
  addTearDown(() {
    VideoPlayerPlatform.instance = previousPlatform;
    // Not awaited: close() only completes once a listener saw the end, and
    // tests in which no player is created never listen.
    unawaited(platform.events.close());
  });
  return platform;
}

/// Lets a started player finish its play() call, then leaves the screen so
/// the controller and its position timer are disposed before the test ends.
/// The plugin completes both outside the fake clock, hence runAsync.
Future<void> _leavePlayingScreen(WidgetTester tester) async {
  await tester.pump(const Duration(milliseconds: 500));
  await tester.runAsync(() async => Future<void>.delayed(Duration.zero));
  await tester.pumpWidget(const SizedBox.shrink());
  await tester.runAsync(() async => Future<void>.delayed(Duration.zero));
  await tester.pumpAndSettle();
}

String _errorText(WidgetTester tester) => tester
    .widget<Text>(find.byKey(const Key('video_player_error_message')))
    .data!;

const _kStreamFailure = 'The connection may have been '
    'interrupted, or this format is not supported.';

/// Reports a failure of the playing native player — what the plugin sends
/// when the connection is lost for good or the stream turns out to be
/// undecodable further in — and pumps until the screen shows its error.
///
/// Returns every error text seen on the way, so tests can check that the
/// generic "stopped" text never shows before the real reason.
Future<List<String>> _failPlayingVideo(
  WidgetTester tester,
  _PlayableVideoPlatform platform,
) async {
  final seen = <String>[];
  platform.events.addError(
    PlatformException(code: 'VideoError', message: 'Source error'),
  );
  for (var attempt = 0; attempt < 100; attempt++) {
    await tester.pump(const Duration(milliseconds: 10));
    await tester.runAsync(
        () async => Future<void>.delayed(const Duration(milliseconds: 1)));
    final message = find.byKey(const Key('video_player_error_message'));
    if (message.evaluate().isEmpty) continue;
    seen.add(tester.widget<Text>(message).data!);
    if (seen.last.startsWith('Cannot play')) break;
  }
  // The reason shows before the failed player is fully released.
  for (var settle = 0; settle < 10; settle++) {
    await tester.pump(const Duration(milliseconds: 10));
    await tester.runAsync(
        () async => Future<void>.delayed(const Duration(milliseconds: 1)));
  }
  return seen;
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

void main() {
  testWidgets('native creation failure releases local source and shows error',
      (tester) async {
    final previousPlatform = VideoPlayerPlatform.instance;
    final platform = _PlayableVideoPlatform()
      ..createError = PlatformException(code: 'permission-denied');
    VideoPlayerPlatform.instance = platform;
    addTearDown(() async {
      VideoPlayerPlatform.instance = previousPlatform;
      unawaited(platform.events.close());
    });
    final coordinator = PlaybackSessionCoordinator();
    const uri = 'content://provider/denied';
    await _pumpScreen(
      tester,
      _FakeApiClient(),
      coordinator: coordinator,
      forbidServerDependencies: true,
      request: LocalPlaybackRequest(
        localMediaId: 7,
        sourceUri: Uri.parse(uri),
        title: 'Denied file',
      ),
    );
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('video_player_error')), findsOneWidget);
    expect(coordinator.isLocalSourceInUse(uri), isFalse);
    final next = await coordinator.claim(
      kind: PlaybackSourceKind.publicShare,
      identity: 'public-share:next',
      stop: () async {},
    );
    expect(next?.isCurrent, isTrue);
    await tester.pumpWidget(const SizedBox.shrink());
  }, variant: TargetPlatformVariant.only(TargetPlatform.android));

  testWidgets('zero-duration video waits for actual completion',
      (tester) async {
    final previousPlatform = VideoPlayerPlatform.instance;
    final platform = _PlayableVideoPlatform()..duration = Duration.zero;
    VideoPlayerPlatform.instance = platform;
    addTearDown(() async {
      VideoPlayerPlatform.instance = previousPlatform;
      await platform.events.close();
    });
    var finished = 0;
    final saved = <double>[];
    await _pumpScreen(
      tester,
      _FakeApiClient(),
      forbidServerDependencies: true,
      request: LocalPlaybackRequest(
        localMediaId: 7,
        sourceUri: Uri.parse('content://provider/unknown'),
        title: 'Unknown duration',
        readPosition: () async => 8,
        savePosition: (seconds) async => saved.add(seconds),
        markFinished: () async => finished++,
      ),
    );
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('video_unknown_duration_controls')),
        findsOneWidget);
    expect(platform.playCalls, greaterThan(0));
    expect(platform.position, const Duration(seconds: 8));
    platform.position = const Duration(seconds: 8);
    await tester.pump(const Duration(seconds: 1));
    await tester.pump();
    expect(platform.positionPolls, greaterThan(0));
    await tester.pump(const Duration(seconds: 5));
    await tester.pump();
    expect(finished, 0);
    expect(saved, contains(8));
    await tester.tap(find.byTooltip('Pause'));
    await tester.pumpAndSettle();
    expect(finished, 0);
    await tester.tap(find.byTooltip('Play'));
    await tester.pumpAndSettle();
    expect(platform.position, const Duration(seconds: 8));
    platform.events.add(VideoEvent(eventType: VideoEventType.completed));
    await tester.pumpAndSettle();
    expect(finished, 1);
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pumpAndSettle();
    expect(finished, 1);
    expect(saved, isNotEmpty);
  }, variant: TargetPlatformVariant.only(TargetPlatform.android));

  testWidgets('failed local initialization retains its URI until disposal ends',
      (tester) async {
    final previousPlatform = VideoPlayerPlatform.instance;
    final platform = _PlayableVideoPlatform()
      ..failInitialize = true
      ..pendingDispose = Completer<void>();
    VideoPlayerPlatform.instance = platform;
    addTearDown(() async {
      VideoPlayerPlatform.instance = previousPlatform;
      await platform.events.close();
    });
    final coordinator = PlaybackSessionCoordinator();
    const uri = 'content://provider/unreadable';
    await _pumpScreen(
      tester,
      _FakeApiClient(),
      coordinator: coordinator,
      forbidServerDependencies: true,
      request: LocalPlaybackRequest(
        localMediaId: 7,
        sourceUri: Uri.parse(uri),
        title: 'Unreadable file',
      ),
    );
    for (var attempt = 0;
        attempt < 100 && platform.disposeCalls == 0;
        attempt++) {
      await tester.pump(const Duration(milliseconds: 10));
      await tester.runAsync(
          () async => Future<void>.delayed(const Duration(milliseconds: 1)));
    }
    expect(platform.disposeCalls, 1);
    expect(coordinator.isLocalSourceInUse(uri), isTrue);
    var replacementStarted = false;
    final next = coordinator
        .claim(
      kind: PlaybackSourceKind.publicShare,
      identity: 'public-share:next',
      stop: () async {},
    )
        .then((lease) {
      replacementStarted = true;
      return lease;
    });
    await tester.pump();
    expect(replacementStarted, isFalse);
    expect(coordinator.isLocalSourceInUse(uri), isTrue);
    platform.pendingDispose!.complete();
    for (var attempt = 0; attempt < 100 && !replacementStarted; attempt++) {
      await tester.pump(const Duration(milliseconds: 10));
      await tester.runAsync(
          () async => Future<void>.delayed(const Duration(milliseconds: 1)));
    }
    expect(replacementStarted, isTrue);
    expect((await tester.runAsync(() => next))!.isCurrent, isTrue);
    await tester.pump();
    expect(coordinator.isLocalSourceInUse(uri), isFalse);
    // A local file the device cannot decode: no server can convert it, and
    // the raw PlatformException text must not be shown.
    expect(
      _errorText(tester),
      'Cannot play “Unreadable file”. '
      'This format cannot be played on this device.',
    );
    expect(find.textContaining('PlatformException'), findsNothing);
    await tester.pumpWidget(const SizedBox.shrink());
  }, variant: TargetPlatformVariant.only(TargetPlatform.android));

  for (final disposeRoute in [true, false]) {
    testWidgets(
        disposeRoute
            ? 'video route disposal blocks replacement until pause finishes'
            : 'retained video route shows stopped state during replacement',
        (tester) async {
      final previousPlatform = VideoPlayerPlatform.instance;
      final platform = _PlayableVideoPlatform();
      VideoPlayerPlatform.instance = platform;
      addTearDown(() async {
        VideoPlayerPlatform.instance = previousPlatform;
        await platform.events.close();
      });
      final coordinator = PlaybackSessionCoordinator();
      await _pumpScreen(
        tester,
        _FakeApiClient(),
        coordinator: coordinator,
        request: PublicSharePlaybackRequest(
          shareToken: 'example',
          sourceUri: Uri.parse('https://player.example/s/example/stream'),
          title: 'Shared video',
        ),
        forbidServerDependencies: true,
      );
      await tester.pump();
      await tester.pump();
      platform.pendingPause = Completer<void>();
      if (disposeRoute) await tester.pumpWidget(const SizedBox.shrink());
      var replacementStarted = false;
      final next = coordinator
          .claim(
        kind: PlaybackSourceKind.local,
        identity: 'local:replacement',
        stop: () async {},
      )
          .then((lease) {
        replacementStarted = true;
        return lease;
      });
      await tester.pump();
      expect(replacementStarted, isFalse);
      if (!disposeRoute) {
        expect(
            find.text('Playback stopped. Tap Retry to play this item again.'),
            findsOneWidget);
        expect(tester.takeException(), isNull);
      }
      platform.pendingPause!.complete();
      for (var attempt = 0; attempt < 100 && !replacementStarted; attempt++) {
        await tester.pump(const Duration(milliseconds: 10));
        await tester.runAsync(
            () async => Future<void>.delayed(const Duration(milliseconds: 1)));
      }
      expect(replacementStarted, isTrue);
      final lease = await tester.runAsync(() => next);
      expect(lease!.isCurrent, isTrue);
      expect(coordinator.activeIdentity, 'local:replacement');
      await tester.pumpWidget(const SizedBox.shrink());
    });
  }

  testWidgets('video lifecycle does not overwrite pending durable resume',
      (tester) async {
    final previousPlatform = VideoPlayerPlatform.instance;
    final platform = _PlayableVideoPlatform();
    VideoPlayerPlatform.instance = platform;
    addTearDown(() async {
      VideoPlayerPlatform.instance = previousPlatform;
      await platform.events.close();
    });
    final resume = Completer<double?>();
    final positions = <double>[];
    var readingResume = false;
    await _pumpScreen(
      tester,
      _FakeApiClient(),
      forbidServerDependencies: true,
      request: LocalPlaybackRequest(
        localMediaId: 92,
        sourceUri: Uri.parse('content://provider/video/92'),
        title: 'Resuming movie',
        readPosition: () {
          readingResume = true;
          return resume.future;
        },
        savePosition: (seconds) async => positions.add(seconds),
        markFinished: () async {},
      ),
    );
    for (var attempt = 0; attempt < 100 && !readingResume; attempt++) {
      await tester.pump(const Duration(milliseconds: 10));
      await tester.runAsync(
          () async => Future<void>.delayed(const Duration(milliseconds: 1)));
    }
    expect(readingResume, isTrue);
    expect(platform.position, Duration.zero);
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
    await tester.pump();
    expect(positions, isEmpty);
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    resume.complete(23);
    await tester.pumpAndSettle();
    expect(platform.position, const Duration(seconds: 23));
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
    await tester.pump();
    expect(positions, [23]);
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pumpAndSettle();
    await tester.runAsync(() async => Future<void>.delayed(Duration.zero));
    await tester.pumpAndSettle();
  }, variant: TargetPlatformVariant.only(TargetPlatform.android));

  testWidgets(
      'local video lifecycle flushes partial progress and excludes stale sources',
      (tester) async {
    final previousPlatform = VideoPlayerPlatform.instance;
    final platform = _PlayableVideoPlatform();
    VideoPlayerPlatform.instance = platform;
    addTearDown(() async {
      VideoPlayerPlatform.instance = previousPlatform;
      await platform.events.close();
    });
    final positions = <double>[];
    final coordinator = PlaybackSessionCoordinator();
    await _pumpScreen(
      tester,
      _FakeApiClient(),
      forbidServerDependencies: true,
      coordinator: coordinator,
      request: LocalPlaybackRequest(
        localMediaId: 91,
        sourceUri: Uri.parse('content://provider/video/91'),
        title: 'Local movie',
        savePosition: (seconds) async => positions.add(seconds),
        markFinished: () async {},
      ),
    );
    await tester.pumpAndSettle();
    platform.position = const Duration(milliseconds: 12300);
    await tester.pump(const Duration(milliseconds: 500));
    await tester.pump();
    final before = positions.length;
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
    await tester.pump();
    expect(positions.skip(before), [12.3]);
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    await tester.pump();
    expect(positions.length, before + 1);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pumpAndSettle();
    await tester.runAsync(() async => Future<void>.delayed(Duration.zero));
    for (var attempt = 0;
        attempt < 100 &&
            coordinator.isLocalSourceInUse('content://provider/video/91');
        attempt++) {
      await tester.pump();
      await tester.runAsync(() async => Future<void>.delayed(Duration.zero));
    }
    expect(platform.disposeCalls, 1);
    expect(
        coordinator.isLocalSourceInUse('content://provider/video/91'), isFalse);
    final afterDispose = positions.length;
    platform.position = const Duration(seconds: 88);
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
    await tester.pump();
    expect(positions.length, afterDispose);
    expect(platform.disposeCalls, 1);
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
  }, variant: TargetPlatformVariant.only(TargetPlatform.android));

  testWidgets('local video uses content URI without server providers',
      (tester) async {
    final previousPlatform = VideoPlayerPlatform.instance;
    final platform = _PlayableVideoPlatform();
    VideoPlayerPlatform.instance = platform;
    addTearDown(() async {
      VideoPlayerPlatform.instance = previousPlatform;
      await platform.events.close();
    });
    final queue = _FakeProgressQueue();
    final client = _FakeApiClient();
    final localPositions = <double>[];
    final request = LocalPlaybackRequest(
      localMediaId: 91,
      sourceUri: Uri.parse('content://provider/video/91'),
      title: 'On-device movie',
      startPosition: 23,
      readPosition: () async => 71,
      savePosition: (seconds) async => localPositions.add(seconds),
      markFinished: () async {},
    );

    await _pumpScreen(
      tester,
      client,
      progressQueue: queue,
      request: request,
      forbidServerDependencies: true,
      mediaId: '91',
    );
    await tester.pumpAndSettle();

    expect(platform.lastSource!.sourceType, DataSourceType.contentUri);
    expect(platform.lastSource!.uri, request.sourceUri.toString());
    expect(platform.lastSource!.httpHeaders, isEmpty);
    expect(platform.position, const Duration(seconds: 23));
    expect(client.getMediaProgressCallCount, 0);
    expect(queue.positions, isEmpty);
    expect(queue.finishedItems, isEmpty);
    platform.position = const Duration(seconds: 40);
    await tester.pump(const Duration(milliseconds: 500));
    await tester.pump();
    await tester.pump(const Duration(seconds: 5));
    await tester.runAsync(() async => Future<void>.delayed(Duration.zero));
    expect(localPositions, contains(40));
    platform.position = const Duration(seconds: 41);
    await tester.pump(const Duration(milliseconds: 500));
    await tester.pump();
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pumpAndSettle();
    await tester.runAsync(() async => Future<void>.delayed(Duration.zero));
    await tester.pumpAndSettle();
    await tester.runAsync(() async => Future<void>.delayed(Duration.zero));
    expect(localPositions.last, 41);
  }, variant: TargetPlatformVariant.only(TargetPlatform.android));

  testWidgets('video threshold queues one durable completion', (tester) async {
    final previousPlatform = VideoPlayerPlatform.instance;
    final platform = _PlayableVideoPlatform();
    VideoPlayerPlatform.instance = platform;
    addTearDown(() async {
      VideoPlayerPlatform.instance = previousPlatform;
      await platform.events.close();
    });
    final queue = _FakeProgressQueue();
    final client = _FakeApiClient();
    await _pumpScreen(tester, client,
        progressQueue: queue, mediaTitle: 'Example movie.mp4');
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('video_player_error')), findsNothing);

    expect(find.text('Example movie.mp4'), findsOneWidget);
    platform.position = const Duration(seconds: 96);
    await tester.pump(const Duration(milliseconds: 500));
    await tester.pump();
    await tester.pump(const Duration(seconds: 5));
    await tester.runAsync(() async => Future<void>.delayed(Duration.zero));
    expect(queue.finishedItems, [42]);
    expect(client.updateProgressStatusCallCount, 0);

    platform.position = const Duration(seconds: 99);
    await tester.pump(const Duration(seconds: 5));
    await tester.runAsync(() async => Future<void>.delayed(Duration.zero));
    expect(queue.finishedItems, [42]);
    expect(queue.positions, hasLength(1));
    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pumpAndSettle();
    await tester.runAsync(() async => Future<void>.delayed(Duration.zero));
    await tester.pumpAndSettle();
  });
  // --------------------------------------------------------------------------
  // Compatibility stream (server-side transcoded media)
  // --------------------------------------------------------------------------

  group('compatibility stream', () {
    testWidgets('shows the preparing state while 503, then plays and resumes',
        (tester) async {
      final platform = _usePlayablePlatform();
      final queue = _FakeProgressQueue();
      final client = _FakeApiClient()
        ..progressResult = 30
        ..probeAnswers
            .addAll([_kTranscoding, const PlaybackProbe(statusCode: 200)]);
      await _pumpScreen(tester, client,
          mediaUrl: _kCompatUrl, mediaTitle: 'clip.wmv', progressQueue: queue);
      await tester.pump();
      await tester.pump();

      // First answer was 503: the player has not been handed the URL yet.
      expect(find.byKey(_kPreparingLabel), findsOneWidget);
      expect(find.text('Preparing playback…'), findsOneWidget);
      expect(find.byKey(const Key('video_player_error')), findsNothing);
      expect(platform.lastSource, isNull);
      expect(client.probedUrls, [Uri.parse(_kCompatUrl)]);

      // Retry-After elapses, the second probe is 200 and playback starts.
      await tester.pump(const Duration(seconds: 5));
      await tester.pumpAndSettle();
      expect(client.probedUrls, hasLength(2));
      expect(platform.lastSource!.uri, _kCompatUrl);
      expect(find.byKey(_kPreparingLabel), findsNothing);
      expect(find.byKey(const Key('video_player_chewie')), findsOneWidget);
      expect(platform.playCalls, 1);
      // Resume and progress saving work exactly as for the original stream.
      expect(platform.position, const Duration(seconds: 30));
      platform.position = const Duration(seconds: 40);
      await tester.pump(const Duration(seconds: 5));
      await tester.runAsync(() async => Future<void>.delayed(Duration.zero));
      expect(queue.positions, [(42, 40.0)]);

      await _leavePlayingScreen(tester);
    });

    testWidgets('a ready stream is not held back by the preparing state',
        (tester) async {
      final platform = _usePlayablePlatform();
      final client = _FakeApiClient()
        ..probeAnswers.add(const PlaybackProbe(statusCode: 200));
      await _pumpScreen(tester, client, mediaUrl: _kCompatUrl);
      await tester.pumpAndSettle();

      expect(client.probedUrls, hasLength(1));
      expect(platform.lastSource!.uri, _kCompatUrl);
      expect(find.byKey(_kPreparingLabel), findsNothing);
      await _leavePlayingScreen(tester);
    });

    testWidgets('the original stream is played without a probe',
        (tester) async {
      final platform = _usePlayablePlatform();
      // probeAnswers is empty: a probe would throw and fail playback.
      final client = _FakeApiClient();
      await _pumpScreen(tester, client);
      await tester.pumpAndSettle();

      expect(client.probedUrls, isEmpty);
      expect(platform.lastSource!.uri, client.streamUrl(42));
      expect(find.byKey(const Key('video_player_error')), findsNothing);
      await _leavePlayingScreen(tester);
    });

    testWidgets('503 until the cap ends in a readable error', (tester) async {
      final platform = _usePlayablePlatform();
      final client = _FakeApiClient()..probeAnswers.add(_kTranscoding);
      var now = DateTime(2026);
      await _pumpScreen(
        tester,
        client,
        mediaUrl: _kCompatUrl,
        mediaTitle: 'clip.wmv',
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
        'Could not prepare “clip.wmv” in time. Please try again later.',
      );
      expect(find.byKey(_kPreparingLabel), findsNothing);
      expect(platform.lastSource, isNull);
      // Nothing keeps probing behind the error view.
      await tester.pump(const Duration(minutes: 1));
      expect(client.probedUrls, hasLength(3));
    });

    testWidgets('500 is terminal; Retry probes again', (tester) async {
      final platform = _usePlayablePlatform();
      final client = _FakeApiClient()
        ..probeAnswers.addAll(const [
          PlaybackProbe(statusCode: 500),
          PlaybackProbe(statusCode: 200),
        ]);
      await _pumpScreen(tester, client,
          mediaUrl: _kCompatUrl, mediaTitle: 'clip.wmv');
      await tester.pump();
      await tester.pump();

      expect(
        _errorText(tester),
        'Cannot play “clip.wmv”. The server could not convert this file.',
      );
      expect(client.probedUrls, hasLength(1));
      expect(platform.lastSource, isNull);

      await tester.tap(find.byKey(const Key('video_player_retry')));
      await tester.pumpAndSettle();
      expect(client.probedUrls, hasLength(2));
      expect(platform.lastSource!.uri, _kCompatUrl);
      expect(find.byKey(const Key('video_player_error')), findsNothing);
      await _leavePlayingScreen(tester);
    });

    testWidgets('leaving the screen while preparing stops the retries',
        (tester) async {
      final platform = _usePlayablePlatform();
      final coordinator = PlaybackSessionCoordinator();
      final client = _FakeApiClient()..probeAnswers.add(_kTranscoding);
      await _pumpScreen(tester, client,
          mediaUrl: _kCompatUrl, coordinator: coordinator);
      await tester.pump();
      await tester.pump();
      expect(find.byKey(_kPreparingLabel), findsOneWidget);

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump(const Duration(minutes: 1));

      expect(client.probedUrls, hasLength(1));
      expect(client.probeTokens.single!.isCancelled, isTrue);
      expect(platform.lastSource, isNull);
      // The abandoned attempt no longer owns the player.
      expect(coordinator.activeKind, isNull);
    });

    testWidgets('another item taking the player ends the preparing state',
        (tester) async {
      final platform = _usePlayablePlatform();
      final coordinator = PlaybackSessionCoordinator();
      final client = _FakeApiClient()..probeAnswers.add(_kTranscoding);
      await _pumpScreen(tester, client,
          mediaUrl: _kCompatUrl, coordinator: coordinator);
      await tester.pump();
      await tester.pump();
      expect(find.byKey(_kPreparingLabel), findsOneWidget);

      // E.g. a share link opened on top of this still-mounted screen.
      await coordinator.claim(
        kind: PlaybackSourceKind.publicShare,
        identity: 'public-share:next',
        stop: () async {},
      );
      // At once: not after the 5 s pause the wait was in.
      await tester.pump();

      expect(find.byKey(_kPreparingLabel), findsNothing);
      expect(find.byType(CircularProgressIndicator), findsNothing);
      expect(
        _errorText(tester),
        'Playback stopped. Tap Retry to play this item again.',
      );
      expect(find.byKey(const Key('video_player_retry')), findsOneWidget);
      await tester.pump(const Duration(minutes: 1));
      expect(client.probedUrls, hasLength(1));
      expect(platform.lastSource, isNull);
    });

    testWidgets(
        'a failure during playback shows the error view; Retry resumes there',
        (tester) async {
      final platform = _usePlayablePlatform()
        ..duration = const Duration(seconds: 3000);
      final queue = _FakeProgressQueue();
      // Saved progress from an earlier session: playback starts at 100 s.
      final client = _FakeApiClient()..progressResult = 100;
      await _pumpScreen(tester, client,
          mediaTitle: 'movie.mp4', progressQueue: queue);
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('video_player_chewie')), findsOneWidget);
      expect(platform.position, const Duration(seconds: 100));
      // The user watches on to 2000 s (the plugin polls the position).
      platform.position = const Duration(seconds: 2000);
      await tester.pump(const Duration(milliseconds: 500));
      await tester.pump();

      final seen = await _failPlayingVideo(tester, platform);

      expect(seen.toSet(), {'Cannot play “movie.mp4”. $_kStreamFailure'});
      expect(find.byKey(const Key('video_player_chewie')), findsNothing);
      // The failed player was released. The plugin's error state carries no
      // position, so nothing is written over the progress saved while playing.
      expect(platform.disposeCalls, 1);
      expect(queue.positions, isEmpty);

      // Retry continues where playback was, not at the stale 100 s, and the
      // next progress tick does not write an older position over a newer one.
      platform.position = Duration.zero;
      await tester.tap(find.byKey(const Key('video_player_retry')));
      // Creating the new native player completes outside the fake clock.
      for (var round = 0; round < 20; round++) {
        await tester.pump(const Duration(milliseconds: 10));
        await tester.runAsync(
            () async => Future<void>.delayed(const Duration(milliseconds: 1)));
      }
      expect(find.byKey(const Key('video_player_chewie')), findsOneWidget);
      expect(platform.position, const Duration(seconds: 2000));
      expect(client.getMediaProgressCallCount, 1);
      await tester.pump(const Duration(seconds: 5));
      await tester.runAsync(() async => Future<void>.delayed(Duration.zero));
      expect(queue.positions, [(42, 2000.0)]);
      await _leavePlayingScreen(tester);
    }, variant: TargetPlatformVariant.only(TargetPlatform.android));

    testWidgets('a failure in fullscreen leaves fullscreen and shows Retry',
        (tester) async {
      final platform = _usePlayablePlatform();
      final orientations = <List<Object?>>[];
      tester.binding.defaultBinaryMessenger
          .setMockMethodCallHandler(SystemChannels.platform, (call) async {
        if (call.method == 'SystemChrome.setPreferredOrientations') {
          orientations.add(call.arguments as List<Object?>);
        }
        return null;
      });
      addTearDown(() => tester.binding.defaultBinaryMessenger
          .setMockMethodCallHandler(SystemChannels.platform, null));
      await _pumpScreen(tester, _FakeApiClient(), mediaTitle: 'movie.mp4');
      await tester.pumpAndSettle();

      final chewie = tester.widget<Chewie>(find.byType(Chewie)).controller;
      chewie.enterFullScreen();
      await tester.pumpAndSettle();
      expect(chewie.isFullScreen, isTrue);
      // The screen with the error view is covered by the fullscreen route.
      expect(find.byKey(const Key('video_player_chewie')), findsNothing);
      final callsInFullScreen = orientations.length;

      final seen = await _failPlayingVideo(tester, platform);
      await tester.pumpAndSettle();

      // Back on the player screen: the reason and Retry can be seen and
      // tapped, instead of a bare error icon on a black fullscreen route.
      expect(seen.last, 'Cannot play “movie.mp4”. $_kStreamFailure');
      expect(find.byKey(const Key('video_player_error_message')).hitTestable(),
          findsOneWidget);
      expect(find.byKey(const Key('video_player_retry')).hitTestable(),
          findsOneWidget);
      expect(find.byIcon(Icons.error), findsNothing);
      expect(find.byType(Chewie), findsNothing);
      // Chewie restored all orientations when its route closed.
      expect(orientations.length, greaterThan(callsInFullScreen));
      expect(orientations.last, hasLength(DeviceOrientation.values.length));
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pumpAndSettle();
    }, variant: TargetPlatformVariant.only(TargetPlatform.android));

    testWidgets('a failing share is logged without its token or URL',
        (tester) async {
      _usePlayablePlatform();
      final publicClient = _FakeApiClient()
        ..probeAnswers.add(const PlaybackProbe(statusCode: 410));
      final logs = <String>[];
      final originalDebugPrint = debugPrint;
      debugPrint = (String? message, {int? wrapWidth}) => logs.add('$message');
      await tester.pumpWidget(ProviderScope(
        overrides: [
          apiClientProvider.overrideWithValue(_FakeApiClient()),
          publicApiClientProvider.overrideWithValue(publicClient),
          progressQueueProvider.overrideWithValue(_FakeProgressQueue()),
        ],
        child: const MaterialApp(
          home: VideoPlayerScreen(
            mediaId: '0',
            mediaUrl: 'http://test.local/s/secrettoken7/compat',
            mediaTitle: 'Shared clip.wmv',
            isPublicShare: true,
          ),
        ),
      ));
      await tester.pump();
      await tester.pump();
      debugPrint = originalDebugPrint;

      expect(
        _errorText(tester),
        'Cannot play “Shared clip.wmv”. This share link is no longer valid '
        'or access was refused.',
      );
      // The failure is logged by kind and error type only.
      expect(logs.join('\n'), contains('publicShare'));
      expect(logs.join('\n'), isNot(contains('secrettoken7')));
      expect(logs.join('\n'), isNot(contains('http')));
    });

    testWidgets('a source the device cannot decode shows a readable error',
        (tester) async {
      final platform = _usePlayablePlatform()..failInitialize = true;
      final client = _FakeApiClient();
      await _pumpScreen(tester, client, mediaTitle: 'clip.wmv');
      // No "stopped" text shows while the failed controller is cleaned up.
      final seen = <String>[];
      for (var attempt = 0; attempt < 100; attempt++) {
        await tester.pump(const Duration(milliseconds: 10));
        await tester.runAsync(
            () async => Future<void>.delayed(const Duration(milliseconds: 1)));
        final message = find.byKey(const Key('video_player_error_message'));
        if (message.evaluate().isNotEmpty) {
          seen.add(tester.widget<Text>(message).data!);
        }
        if (seen.length > 5) break;
      }
      expect(seen.toSet(), {'Cannot play “clip.wmv”. $_kStreamFailure'});

      expect(platform.lastSource, isNotNull);
      expect(
        _errorText(tester),
        'Cannot play “clip.wmv”. The connection may have been '
        'interrupted, or this format is not supported.',
      );
      expect(find.textContaining('PlatformException'), findsNothing);
      expect(find.textContaining('Playback failed'), findsNothing);
      await _leavePlayingScreen(tester);
    }, variant: TargetPlatformVariant.only(TargetPlatform.android));
  });

  // --------------------------------------------------------------------------
  // Loading state
  // --------------------------------------------------------------------------

  group('loading state', () {
    testWidgets(
        'shows a loading indicator immediately after pumpWidget (before initState callback fires)',
        (tester) async {
      final fakeClient = _FakeApiClient();
      // pumpWidget renders the first frame with _isLoading == true but does
      // NOT fire addPostFrameCallback yet — that fires on the next pump.
      await _pumpScreen(tester, fakeClient);

      // Immediately after pumpWidget the initial build has completed with
      // _isLoading = true; the spinner must be visible at this point.
      expect(
        find.byKey(const Key('video_player_loading')),
        findsOneWidget,
      );
      expect(find.byType(CircularProgressIndicator), findsOneWidget);

      // Drain remaining async work to avoid "pending timers" warnings.
      await tester.pumpAndSettle();
    });
  });

  // --------------------------------------------------------------------------
  // Error state
  // --------------------------------------------------------------------------

  group('error state', () {
    testWidgets(
        'shows error view when VideoPlayerController fails to initialise',
        (tester) async {
      final fakeClient = _FakeApiClient();
      await _pumpScreen(tester, fakeClient);
      // pumpAndSettle processes addPostFrameCallback → _initPlayer → throws →
      // error state is rendered.
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('video_player_error')), findsOneWidget);
    });

    testWidgets('error view contains a human-readable message', (tester) async {
      final fakeClient = _FakeApiClient();
      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // The error message key should be present and contain non-empty text.
      expect(
        find.byKey(const Key('video_player_error_message')),
        findsOneWidget,
      );
      // Verify the text widget is present with a non-empty string inside the
      // error_message keyed slot.
      final textWidget = tester.widget<Text>(
        find.byKey(const Key('video_player_error_message')),
      );
      expect(textWidget.data, isNotEmpty);
    });

    testWidgets('error view contains a retry button', (tester) async {
      final fakeClient = _FakeApiClient();
      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('video_player_retry')), findsOneWidget);
    });

    testWidgets(
        'tapping retry eventually shows the error state again after re-initialisation',
        (tester) async {
      final fakeClient = _FakeApiClient();
      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Confirm we start in the error state.
      expect(find.byKey(const Key('video_player_error')), findsOneWidget);

      // Tap Retry — this calls _onRetry which calls setState then _initPlayer.
      await tester.tap(find.byKey(const Key('video_player_retry')));

      // Let the second initialisation attempt complete (also fails in test
      // harness due to no platform plugin) and settle back into error state.
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('video_player_error')), findsOneWidget);
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
  // Stream URL resolution
  // --------------------------------------------------------------------------

  group('stream URL resolution', () {
    testWidgets('transitions through loading to error when mediaUrl is null',
        (tester) async {
      // When mediaUrl is null the screen calls client.streamUrl(mediaId).
      // We verify the full lifecycle: starts loading → error after init fails.
      final fakeClient = _FakeApiClient();
      await _pumpScreen(tester, fakeClient, mediaUrl: null);

      // Immediately after pumpWidget the loading state is visible.
      expect(find.byKey(const Key('video_player_loading')), findsOneWidget);

      // After settling, error state is shown (platform plugin missing).
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('video_player_error')), findsOneWidget);
    });

    testWidgets(
        'transitions through loading to error when an explicit mediaUrl is given',
        (tester) async {
      final fakeClient = _FakeApiClient();
      await _pumpScreen(
        tester,
        fakeClient,
        mediaUrl: 'http://localhost:8080/api/v1/media/42/stream',
      );

      // Loading state visible immediately after pumpWidget.
      expect(find.byKey(const Key('video_player_loading')), findsOneWidget);

      // Settles to error state (VideoPlayerController fails in test harness).
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('video_player_error')), findsOneWidget);
    });
  });
}
