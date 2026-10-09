// Widget tests for ShareViewerScreen (share_viewer_screen.dart).
//
// Tests cover:
//   1. Shows a loading indicator while getSharedMediaPage is in flight.
//   2. Renders filename, type, and duration after a successful load.
//   3. Shows the thumbnail placeholder when hasThumb is false.
//   4. Shows the play button after a successful load.
//   5. Shows a 404 error message for an invalid/revoked token.
//   6. Shows a 410 error message for an expired token.
//   7. Shows a generic error message for network failures.
//   8. Shows the retry button on error and re-calls getSharedMediaPage on tap.
//
// Riverpod providers are overridden with fakes so tests run without a real
// server.  GoRouter is replaced with a plain MaterialApp to avoid test
// infrastructure complexity.
//
// Run with: flutter test test/screens/share_viewer_screen_test.dart

import 'dart:async';
import 'dart:convert';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/first_run_provider.dart';
import 'package:player_android/providers/public_api_client_provider.dart';
import 'package:player_android/router.dart';
import 'package:player_android/screens/share_viewer_screen.dart';
import 'package:player_android/screens/image_viewer_screen.dart';
import 'package:player_android/screens/video_player_screen.dart';
import 'package:player_android/services/playback_preparer.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/utils/error_mappers.dart';
import 'package:player_android/widgets/public_network_image.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _Unauthenticated extends AuthStateNotifier {
  @override
  Future<AuthState> build() async => const AuthState.unauthenticated();
}

class _Authenticated extends AuthStateNotifier {
  @override
  Future<AuthState> build() async => const AuthState.authenticated();
}

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

/// Controllable [PlayerApiClient] stub for [ShareViewerScreen] tests.
///
/// Overrides the two members that [ShareViewerScreen] actually calls —
/// [getSharedMediaPage] and [baseUrl] — making the contract explicit rather
/// than relying on the concrete base class throwing [UnimplementedError] for
/// the many methods the screen never touches (LSP / ISP: the fake honours
/// the subset of the contract the screen depends on).
class _FakePublicApiClient extends PlayerApiClient {
  _FakePublicApiClient()
      : super(dio: Dio(BaseOptions(baseUrl: 'http://test.local')));

  // When non-null, [getSharedMediaPage] returns this string.
  String? pageJson;

  // When non-null, [getSharedMediaPage] throws this instead of returning.
  Object? pageError;

  // Count of calls to [getSharedMediaPage] so retry tests can assert call count.
  int callCount = 0;

  /// The test base URL without a trailing slash, matching the production
  /// [PlayerApiClient.baseUrl] contract used in [ShareViewerScreen].
  String base = 'http://test.local';

  @override
  String get baseUrl => base;

  /// Status answered to the player's compatibility-stream readiness probe.
  int probeStatus = 410;
  final probedUrls = <Uri>[];

  @override
  Future<PlaybackProbe> probePlayback(
    Uri url, {
    CancelToken? cancelToken,
  }) async {
    probedUrls.add(url);
    return PlaybackProbe(statusCode: probeStatus);
  }

  @override
  Future<String> getSharedMediaPage(String token) async {
    callCount++;
    if (pageError != null) throw pageError!;
    return pageJson!;
  }
}

/// Controllable [PlayerApiClient] stub that delays [getSharedMediaPage] until
/// [complete] is called — used to inspect the mid-flight loading state.
///
/// Overrides [baseUrl] explicitly for the same reason as [_FakePublicApiClient]:
/// the screen calls [baseUrl] when constructing the thumbnail URL, so the stub
/// must provide a consistent value rather than delegating to [rawDio] internals.
class _DelayedFakePublicApiClient extends PlayerApiClient {
  _DelayedFakePublicApiClient()
      : super(dio: Dio(BaseOptions(baseUrl: 'http://test.local')));

  final _completer = Completer<String>();

  void complete(String json) => _completer.complete(json);

  /// Returns the test base URL without a trailing slash.
  @override
  String get baseUrl => 'http://test.local';

  @override
  Future<String> getSharedMediaPage(String token) => _completer.future;
}

// ---------------------------------------------------------------------------
// Sample JSON
// ---------------------------------------------------------------------------

/// Valid share-page JSON for a video file with a thumbnail.
const _kVideoShareJson = '''
{
  "media": {
    "id": 42,
    "file_name": "holiday.mp4",
    "type": "video",
    "duration": 3612.5
  },
  "has_thumb": true,
  "stream_url": "/s/abc123/stream",
  "download_url": "/s/abc123/download",
  "thumb_url": "/s/abc123/thumbnail"
}
''';

/// Share-page JSON for a video the server transcodes: `playback_url` is the
/// compatibility stream while `stream_url` stays the original file.
const _kTranscodedShareJson = '''
{
  "media": {
    "id": 42,
    "file_name": "holiday.wmv",
    "type": "video",
    "duration": 60.0,
    "transcoded": true
  },
  "has_thumb": false,
  "transcoded": true,
  "stream_url": "/s/abc123/stream",
  "playback_url": "/s/abc123/compat",
  "download_url": "/s/abc123/download",
  "thumb_url": ""
}
''';

/// Opens the share viewer for [token] through the app's real router and taps
/// Play; returns the video player screen the router built.
Future<VideoPlayerScreen> _playShareThroughRouter(
  WidgetTester tester,
  _FakePublicApiClient client, {
  String token = 'abc123',
}) async {
  SharedPreferences.setMockInitialValues({});
  final base = Uri.parse(client.base);
  final container = ProviderContainer(overrides: [
    authStateProvider.overrideWith(_Unauthenticated.new),
    firstRunProvider.overrideWith((ref) async => false),
    publicApiClientProvider.overrideWithValue(client),
    publicShareBaseUrlProvider.overrideWithValue(base),
    apiClientProvider.overrideWith((ref) => throw StateError('No account API')),
  ]);
  addTearDown(container.dispose);
  final router = container.read(routerProvider);
  addTearDown(router.dispose);
  await tester.pumpWidget(UncontrolledProviderScope(
    container: container,
    child: MaterialApp.router(routerConfig: router),
  ));
  router.go(AppRoutes.shareViewerPath(token));
  await tester.pumpAndSettle();
  await tester.ensureVisible(find.byKey(const Key('share_viewer_play_button')));
  await tester.tap(find.byKey(const Key('share_viewer_play_button')));
  await tester.pumpAndSettle();
  return tester.widget<VideoPlayerScreen>(find.byType(VideoPlayerScreen));
}

/// Valid share-page JSON for an audio file without a thumbnail.
const _kAudioShareJson = '''
{
  "media": {
    "id": 7,
    "file_name": "podcast.mp3",
    "type": "audio",
    "duration": 1800.0
  },
  "has_thumb": false,
  "stream_url": "/s/tok7/stream",
  "download_url": "/s/tok7/download",
  "thumb_url": ""
}
''';

// ---------------------------------------------------------------------------
// Helper: pump ShareViewerScreen inside a ProviderScope.
// ---------------------------------------------------------------------------

/// Pumps [ShareViewerScreen] inside a [ProviderScope] that overrides
/// [publicApiClientProvider] with a fake, wrapped in a [MaterialApp] so
/// widgets like SnackBar and routes work correctly.
///
/// [goRouterOverride] is passed as the [MaterialApp] router if supplied;
/// the default is a plain [MaterialApp] with no named routes so that
/// [context.go] calls in the screen under test do not throw.
Future<void> _pumpShareViewerScreen(
  WidgetTester tester,
  PlayerApiClient fakeClient, {
  String token = 'abc123',
}) async {
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        publicApiClientProvider.overrideWithValue(fakeClient),
      ],
      child: MaterialApp(
        // A minimal route table so context.go does not crash when the play
        // button is tapped.  We only verify the button exists in these tests,
        // not that navigation succeeds.
        onGenerateRoute: (settings) => MaterialPageRoute<void>(
          settings: settings,
          builder: (_) => const Scaffold(body: Text('Player')),
        ),
        home: ShareViewerScreen(token: token),
      ),
    ),
  );
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

void main() {
  testWidgets('public image share opens without account requests or headers',
      (tester) async {
    SharedPreferences.setMockInitialValues({});
    final client = _FakePublicApiClient()
      ..pageJson = jsonEncode({
        'media': {'file_name': 'photo.jpg', 'type': 'image', 'duration': 0.04},
        'has_thumb': false,
      });
    final container = ProviderContainer(overrides: [
      authStateProvider.overrideWith(_Unauthenticated.new),
      firstRunProvider.overrideWith((ref) async => false),
      publicApiClientProvider.overrideWithValue(client),
      publicShareBaseUrlProvider
          .overrideWithValue(Uri.parse('https://share.example')),
      apiClientProvider
          .overrideWith((ref) => throw StateError('No account API')),
    ]);
    addTearDown(container.dispose);
    final router = container.read(routerProvider);
    addTearDown(router.dispose);
    await tester.pumpWidget(UncontrolledProviderScope(
      container: container,
      child: MaterialApp.router(routerConfig: router),
    ));
    router.go(AppRoutes.shareViewerPath('image-token'));
    await tester.pumpAndSettle();
    expect(find.text('View Image'), findsOneWidget);
    // A still image's tiny probe duration is not shown as 0:00.
    expect(find.text('Image'), findsOneWidget);
    expect(find.textContaining('0:00'), findsNothing);
    await tester.ensureVisible(find.text('View Image'));
    await tester.tap(find.text('View Image'));
    await tester.pumpAndSettle();
    expect(find.byType(ImageViewerScreen), findsOneWidget);
    // The viewer reuses the loaded share file name rather than a generic label.
    expect(
      find.descendant(
          of: find.byType(AppBar), matching: find.text('photo.jpg')),
      findsOneWidget,
    );
    final image = tester.widget<Image>(find.byType(Image));
    final source = image.image as NetworkImage;
    expect(source.url, 'https://share.example/s/image-token/stream');
    expect(source.headers, isNull);
    expect(find.byKey(const Key('image_viewer_zoom')), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('real router opens /s token without a session', (tester) async {
    SharedPreferences.setMockInitialValues({});
    final client = _FakePublicApiClient()..pageJson = _kAudioShareJson;
    final container = ProviderContainer(overrides: [
      authStateProvider.overrideWith(_Unauthenticated.new),
      firstRunProvider.overrideWith((ref) async => false),
      publicApiClientProvider.overrideWithValue(client),
    ]);
    addTearDown(container.dispose);
    final router = container.read(routerProvider);
    addTearDown(router.dispose);
    await tester.pumpWidget(UncontrolledProviderScope(
      container: container,
      child: MaterialApp.router(routerConfig: router),
    ));
    router.go(AppRoutes.shareViewerPath('tok7'));
    await tester.pumpAndSettle();
    expect(find.text('podcast.mp3'), findsOneWidget);
    expect(find.byKey(const Key('login_username')), findsNothing);
    expect(client.callCount, 1);
    // Anonymous viewers get no library shortcut.
    expect(find.byKey(const Key('share_viewer_library_button')), findsNothing);
  });

  testWidgets('a share opened from inside the app keeps its back arrow',
      (tester) async {
    final client = _FakePublicApiClient()..pageJson = _kAudioShareJson;
    final router = GoRouter(initialLocation: AppRoutes.home, routes: [
      GoRoute(
        path: AppRoutes.home,
        builder: (context, state) => const Text('library home'),
      ),
      GoRoute(
        path: AppRoutes.shareViewer,
        builder: (context, state) =>
            ShareViewerScreen(token: state.pathParameters['token']!),
      ),
    ]);
    addTearDown(router.dispose);
    await tester.pumpWidget(ProviderScope(
      overrides: [
        authStateProvider.overrideWith(_Authenticated.new),
        publicApiClientProvider.overrideWithValue(client),
      ],
      child: MaterialApp.router(routerConfig: router),
    ));
    router.push(AppRoutes.shareViewerPath('tok7'));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('share_viewer_library_button')), findsNothing);
    expect(find.byType(BackButton), findsOneWidget);
  });

  testWidgets('signed-in users can leave a share link for their library',
      (tester) async {
    final client = _FakePublicApiClient()..pageJson = _kAudioShareJson;
    final router = GoRouter(initialLocation: '/s/tok7', routes: [
      GoRoute(
        path: AppRoutes.shareViewer,
        builder: (context, state) =>
            ShareViewerScreen(token: state.pathParameters['token']!),
      ),
      GoRoute(
        path: AppRoutes.home,
        builder: (context, state) => const Text('library home'),
      ),
    ]);
    addTearDown(router.dispose);
    await tester.pumpWidget(ProviderScope(
      overrides: [
        authStateProvider.overrideWith(_Authenticated.new),
        publicApiClientProvider.overrideWithValue(client),
      ],
      child: MaterialApp.router(routerConfig: router),
    ));
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('share_viewer_library_button')));
    await tester.pumpAndSettle();
    expect(find.text('library home'), findsOneWidget);
  });

  testWidgets('Play opens a public share player path', (tester) async {
    final client = _FakePublicApiClient()..pageJson = _kAudioShareJson;
    final router = GoRouter(initialLocation: '/s/tok7', routes: [
      GoRoute(
        path: AppRoutes.shareViewer,
        builder: (context, state) =>
            ShareViewerScreen(token: state.pathParameters['token']!),
      ),
      GoRoute(
        path: AppRoutes.sharedAudioPlayer,
        builder: (context, state) =>
            Text('playing ${state.pathParameters['token']}'),
      ),
    ]);
    addTearDown(router.dispose);
    await tester.pumpWidget(ProviderScope(
      overrides: [publicApiClientProvider.overrideWithValue(client)],
      child: MaterialApp.router(routerConfig: router),
    ));
    await tester.pumpAndSettle();
    await tester
        .ensureVisible(find.byKey(const Key('share_viewer_play_button')));
    await tester.tap(find.byKey(const Key('share_viewer_play_button')));
    await tester.pumpAndSettle();
    expect(find.text('playing tok7'), findsOneWidget);
  });
  group('playback URL', () {
    test('playback_url is parsed; without it the original stream is used', () {
      final transcoded = SharePageMetadata.fromJson(_kTranscodedShareJson);
      expect(transcoded.playbackUrl, '/s/abc123/compat');
      expect(transcoded.streamUrl, '/s/abc123/stream');

      // A server that predates the compatibility stream.
      final original = SharePageMetadata.fromJson(_kVideoShareJson);
      expect(original.playbackUrl, '/s/abc123/stream');
    });

    testWidgets('a transcoded share plays the compatibility stream',
        (tester) async {
      final client = _FakePublicApiClient()..pageJson = _kTranscodedShareJson;
      final player = await _playShareThroughRouter(tester, client);

      expect(player.mediaUrl, 'http://test.local/s/abc123/compat');
      expect(player.isPublicShare, isTrue);
      expect(player.mediaTitle, 'holiday.wmv');
      // The readiness probe went to the anonymous client (the account
      // client throws when read). Its 410 means the link expired meanwhile.
      expect(
          client.probedUrls, [Uri.parse('http://test.local/s/abc123/compat')]);
      expect(
        find.text('Cannot play “holiday.wmv”. This share link is no longer '
            'valid or access was refused.'),
        findsOneWidget,
      );
      expect(find.byKey(const Key('video_player_retry')), findsOneWidget);
    });

    testWidgets('a server under a path prefix keeps the prefix',
        (tester) async {
      final client = _FakePublicApiClient()
        ..base = 'http://test.local/player'
        ..pageJson = _kTranscodedShareJson;
      final player = await _playShareThroughRouter(tester, client);

      expect(player.mediaUrl, 'http://test.local/player/s/abc123/compat');
      expect(client.probedUrls,
          [Uri.parse('http://test.local/player/s/abc123/compat')]);
    });

    testWidgets('an ordinary share plays the original stream', (tester) async {
      final client = _FakePublicApiClient()..pageJson = _kVideoShareJson;
      final player = await _playShareThroughRouter(tester, client);

      expect(player.mediaUrl, 'http://test.local/s/abc123/stream');
    });

    testWidgets('a playback_url for another share token is not played',
        (tester) async {
      final client = _FakePublicApiClient()
        ..pageJson = _kTranscodedShareJson.replaceAll(
            '/s/abc123/compat', '/s/other/compat');
      final player = await _playShareThroughRouter(tester, client);

      expect(player.mediaUrl, 'http://test.local/s/abc123/stream');
    });
  });

  group('sharePlaybackUrl', () {
    final root = Uri.parse('https://share.example');
    final prefixed = Uri.parse('https://share.example/player/');

    String pick(String? candidate, {Uri? base}) => sharePlaybackUrl(
          base: base ?? root,
          token: 'tok7',
          candidate: candidate,
        );

    test('accepts exactly this share\'s compat and stream endpoints', () {
      expect(pick('https://share.example/s/tok7/compat'),
          'https://share.example/s/tok7/compat');
      expect(pick('https://share.example/s/tok7/stream'),
          'https://share.example/s/tok7/stream');
      expect(pick(null), 'https://share.example/s/tok7/stream');
    });

    test('includes the path prefix of the configured base URL', () {
      expect(
        pick('https://share.example/player/s/tok7/compat', base: prefixed),
        'https://share.example/player/s/tok7/compat',
      );
      expect(pick(null, base: prefixed),
          'https://share.example/player/s/tok7/stream');
      // The same path without the prefix is a different endpoint.
      expect(pick('https://share.example/s/tok7/compat', base: prefixed),
          'https://share.example/player/s/tok7/stream');
    });

    test('rejects everything else in favour of the original stream', () {
      const fallback = 'https://share.example/s/tok7/stream';
      for (final candidate in [
        // Dot segments that normalise to another same-origin path.
        'https://share.example/s/tok7/../../api/v1/media/1/stream',
        'https://share.example/s/tok7/compat/../../other/compat',
        'https://share.example/s/tok7/%2e%2e/other/compat',
        // Another share, another endpoint, extra parts.
        'https://share.example/s/other/compat',
        'https://share.example/s/tok7/download',
        'https://share.example/s/tok7/compat/extra',
        'https://share.example/s/tok7/compat?next=/x',
        'https://share.example/s/tok7/compat#x',
        // Another origin or scheme.
        'https://evil.example/s/tok7/compat',
        'http://share.example/s/tok7/compat',
        'https://share.example:8443/s/tok7/compat',
        'https://user@share.example/s/tok7/compat',
        '/s/tok7/compat',
        'not a url',
        '',
      ]) {
        expect(pick(candidate), fallback, reason: candidate);
      }
    });

    test('dot segments that stay on the endpoint are normalised away', () {
      expect(pick('https://share.example/s/./tok7/x/../compat'),
          'https://share.example/s/tok7/compat');
    });

    test('keeps the viewing credential, the only query it accepts', () {
      const view = '1791570469.t1SgGrk2dxCyNpl4ugVK0eAaNUbwCSDJvJ8G9rDi4DU';
      expect(pick('https://share.example/s/tok7/compat?view=$view'),
          'https://share.example/s/tok7/compat?view=$view');
      expect(pick('https://share.example/s/tok7/stream?view=$view'),
          'https://share.example/s/tok7/stream?view=$view');
      expect(
        pick('https://share.example/player/s/tok7/compat?view=$view',
            base: prefixed),
        'https://share.example/player/s/tok7/compat?view=$view',
      );
      // Normalising the path leaves the credential alone.
      expect(pick('https://share.example/s/./tok7/x/../compat?view=$view'),
          'https://share.example/s/tok7/compat?view=$view');
      // Every character of the credential alphabet.
      expect(pick('https://share.example/s/tok7/stream?view=aZ09._-'),
          'https://share.example/s/tok7/stream?view=aZ09._-');
      // The longest value still taken for a credential.
      final longest = 'a' * 128;
      expect(pick('https://share.example/s/tok7/stream?view=$longest'),
          'https://share.example/s/tok7/stream?view=$longest');
    });

    test('rejects any other query, also dressed up as a credential', () {
      const fallback = 'https://share.example/s/tok7/stream';
      for (final query in [
        // Not the credential parameter, or not only it.
        'next=/x',
        'view=abc&next=/x',
        'next=/x&view=abc',
        'view=abc&view=def',
        'view=abc&',
        '&view=abc',
        'View=abc',
        'view',
        'view=',
        '',
        // Values outside the credential alphabet, raw or percent-encoded.
        'view=a b',
        'view=a+b',
        'view=a/b',
        'view=a%2Fb',
        'view=a%26next%3D1',
        'view=a%00',
        'view=a%0d%0aX-Injected:1',
        'view=a;b',
        'view=a=b',
        'view=a,b',
        'view=a@evil.example',
        'view=https://evil.example/',
        'view=..%2F..%2Fapi',
        'view=ä',
        'view=%C3%A4',
        // Too long to be a credential.
        'view=${'a' * 129}',
        'view=${'a' * 5000}',
      ]) {
        expect(pick('https://share.example/s/tok7/compat?$query'), fallback,
            reason: query);
      }
      // A credential does not make a foreign URL acceptable.
      for (final candidate in [
        'https://evil.example/s/tok7/compat?view=abc',
        'https://share.example/s/other/compat?view=abc',
        'https://share.example/s/tok7/download?view=abc',
        'https://share.example/s/tok7/../other/compat?view=abc',
        'https://share.example/s/tok7/compat?view=abc#frag',
        'https://share.example/s/tok7/compat#?view=abc',
      ]) {
        expect(pick(candidate), fallback, reason: candidate);
      }
    });
  });

  group('viewing credential', () {
    const view = '1791570469.t1SgGrk2dxCyNpl4ugVK0eAaNUbwCSDJvJ8G9rDi4DU';

    /// Share JSON as a server with viewings sends it: every URL carries the
    /// credential.
    String shareJson({
      required String type,
      required String fileName,
      String endpoint = 'stream',
      bool hasThumb = false,
    }) =>
        jsonEncode({
          'media': {'id': 42, 'file_name': fileName, 'type': type},
          'has_thumb': hasThumb,
          'transcoded': endpoint == 'compat',
          'stream_url': '/s/abc123/stream?view=$view',
          'playback_url': '/s/abc123/$endpoint?view=$view',
          'download_url': '/s/abc123/download?view=$view',
          if (hasThumb) 'thumb_url': '/s/abc123/thumbnail?view=$view',
          'view': view,
          'view_expires_at': '2026-10-09T18:27:49Z',
        });

    test('the model keeps the credentialed URLs exactly as sent', () {
      final page = SharePageMetadata.fromJson(shareJson(
          type: 'video',
          fileName: 'holiday.wmv',
          endpoint: 'compat',
          hasThumb: true));
      expect(page.streamUrl, '/s/abc123/stream?view=$view');
      expect(page.playbackUrl, '/s/abc123/compat?view=$view');
      expect(page.downloadUrl, '/s/abc123/download?view=$view');
      expect(page.thumbUrl, '/s/abc123/thumbnail?view=$view');
    });

    test('without playback_url the credentialed stream_url is played', () {
      final json = jsonDecode(shareJson(type: 'video', fileName: 'a.mp4'))
          as Map<String, dynamic>
        ..remove('playback_url');
      expect(SharePageMetadata.fromJson(jsonEncode(json)).playbackUrl,
          '/s/abc123/stream?view=$view');
    });

    testWidgets('the player and its readiness probe carry the credential',
        (tester) async {
      final client = _FakePublicApiClient()
        ..pageJson = shareJson(
            type: 'video', fileName: 'holiday.wmv', endpoint: 'compat');
      final player = await _playShareThroughRouter(tester, client);

      expect(player.mediaUrl, 'http://test.local/s/abc123/compat?view=$view');
      expect(client.probedUrls,
          [Uri.parse('http://test.local/s/abc123/compat?view=$view')]);
      // One metadata fetch is one viewing: playing must not fetch again.
      expect(client.callCount, 1);
    });

    testWidgets('an ordinary share plays the credentialed original stream',
        (tester) async {
      final client = _FakePublicApiClient()
        ..pageJson = shareJson(type: 'video', fileName: 'holiday.mp4');
      final player = await _playShareThroughRouter(tester, client);

      expect(player.mediaUrl, 'http://test.local/s/abc123/stream?view=$view');
      expect(client.callCount, 1);
    });

    testWidgets('a tampered credential falls back to the plain stream',
        (tester) async {
      final client = _FakePublicApiClient()
        ..pageJson = shareJson(type: 'video', fileName: 'holiday.mp4')
            .replaceAll('?view=$view', '?view=$view&next=/api/v1/media');
      final player = await _playShareThroughRouter(tester, client);

      expect(player.mediaUrl, 'http://test.local/s/abc123/stream');
    });

    testWidgets('the thumbnail is requested with the credential',
        (tester) async {
      final client = _FakePublicApiClient()
        ..pageJson =
            shareJson(type: 'video', fileName: 'holiday.mp4', hasThumb: true);
      await _pumpShareViewerScreen(tester, client);
      await tester.pumpAndSettle();

      final thumbnail = tester.widget<PublicNetworkImage>(
          find.byKey(const Key('share_viewer_thumbnail')));
      expect(
          thumbnail.imageUrl, 'http://test.local/s/abc123/thumbnail?view=$view');
    });

    testWidgets('an image share opens the credentialed original',
        (tester) async {
      SharedPreferences.setMockInitialValues({});
      final client = _FakePublicApiClient()
        ..pageJson = shareJson(type: 'image', fileName: 'photo.jpg');
      final container = ProviderContainer(overrides: [
        authStateProvider.overrideWith(_Unauthenticated.new),
        firstRunProvider.overrideWith((ref) async => false),
        publicApiClientProvider.overrideWithValue(client),
        publicShareBaseUrlProvider.overrideWithValue(Uri.parse(client.base)),
        apiClientProvider
            .overrideWith((ref) => throw StateError('No account API')),
      ]);
      addTearDown(container.dispose);
      final router = container.read(routerProvider);
      addTearDown(router.dispose);
      await tester.pumpWidget(UncontrolledProviderScope(
        container: container,
        child: MaterialApp.router(routerConfig: router),
      ));
      router.go(AppRoutes.shareViewerPath('abc123'));
      await tester.pumpAndSettle();
      await tester.ensureVisible(find.text('View Image'));
      await tester.tap(find.text('View Image'));
      await tester.pumpAndSettle();

      final viewer =
          tester.widget<ImageViewerScreen>(find.byType(ImageViewerScreen));
      expect(viewer.imageUrl, 'http://test.local/s/abc123/stream?view=$view');
      expect(viewer.isPublicShare, isTrue);
      expect(tester.takeException(), isNull);
    });
  });

  // --------------------------------------------------------------------------
  // Loading state
  // --------------------------------------------------------------------------

  group('loading state', () {
    testWidgets('shows loading indicator while getSharedMediaPage is in flight',
        (tester) async {
      final fakeClient = _DelayedFakePublicApiClient();

      await _pumpShareViewerScreen(tester, fakeClient);

      // Pump one frame so initState + addPostFrameCallback fire, but the
      // Future has not yet resolved.
      await tester.pump();

      expect(find.byKey(const Key('share_viewer_loading')), findsOneWidget);
      expect(find.byType(CircularProgressIndicator), findsOneWidget);

      // Resolve to avoid "async work pending" warnings in the test output.
      fakeClient.complete(_kVideoShareJson);
      await tester.pumpAndSettle();
    });
  });

  // --------------------------------------------------------------------------
  // Renders metadata
  // --------------------------------------------------------------------------

  group('renders metadata', () {
    testWidgets('shows filename after a successful load', (tester) async {
      final fakeClient = _FakePublicApiClient()..pageJson = _kVideoShareJson;

      await _pumpShareViewerScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('share_viewer_filename')), findsOneWidget);
      expect(find.text('holiday.mp4'), findsOneWidget);
    });

    testWidgets('shows type and formatted duration in metadata row',
        (tester) async {
      final fakeClient = _FakePublicApiClient()..pageJson = _kVideoShareJson;

      await _pumpShareViewerScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // 3612.5 seconds → "1:00:12".
      expect(find.byKey(const Key('share_viewer_metadata')), findsOneWidget);
      expect(find.textContaining('Video'), findsOneWidget);
      expect(find.textContaining('1:00:12'), findsOneWidget);
    });

    testWidgets('shows audio type label for audio shares', (tester) async {
      final fakeClient = _FakePublicApiClient()..pageJson = _kAudioShareJson;

      await _pumpShareViewerScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.textContaining('Audio'), findsOneWidget);
      expect(find.text('podcast.mp3'), findsOneWidget);
    });

    testWidgets('shows thumbnail placeholder when hasThumb is false',
        (tester) async {
      final fakeClient = _FakePublicApiClient()..pageJson = _kAudioShareJson;

      await _pumpShareViewerScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('share_viewer_thumbnail_placeholder')),
        findsOneWidget,
      );
    });

    testWidgets('shows Image.network widget when hasThumb is true',
        (tester) async {
      final fakeClient = _FakePublicApiClient()..pageJson = _kVideoShareJson;

      await _pumpShareViewerScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // The Image.network widget (key = share_viewer_thumbnail) is rendered in
      // the widget tree when hasThumb is true.  In tests the image may not
      // actually load from the network, but the widget is present and the
      // fallback is controlled by the errorBuilder — we only verify that the
      // Image.network widget itself was constructed (not the static placeholder
      // icon used when hasThumb is false).
      expect(find.byKey(const Key('share_viewer_thumbnail')), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Play button
  // --------------------------------------------------------------------------

  group('play button', () {
    testWidgets('play button is present after a successful load',
        (tester) async {
      final fakeClient = _FakePublicApiClient()..pageJson = _kVideoShareJson;

      await _pumpShareViewerScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('share_viewer_play_button')), findsOneWidget);
    });

    testWidgets('play button is absent while loading', (tester) async {
      final fakeClient = _DelayedFakePublicApiClient();

      await _pumpShareViewerScreen(tester, fakeClient);
      await tester.pump();

      expect(
        find.byKey(const Key('share_viewer_play_button')),
        findsNothing,
      );

      fakeClient.complete(_kVideoShareJson);
      await tester.pumpAndSettle();
    });

    testWidgets('play button is absent on error', (tester) async {
      final fakeClient = _FakePublicApiClient()
        ..pageError = DioException(
          requestOptions: RequestOptions(path: '/s/bad'),
          response: Response(
            requestOptions: RequestOptions(path: '/s/bad'),
            statusCode: 404,
          ),
          type: DioExceptionType.badResponse,
        );

      await _pumpShareViewerScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('share_viewer_play_button')),
        findsNothing,
      );
    });
  });

  // --------------------------------------------------------------------------
  // Error states
  // --------------------------------------------------------------------------

  group('error states', () {
    testWidgets('shows 404 error message for an invalid/revoked token',
        (tester) async {
      final fakeClient = _FakePublicApiClient()
        ..pageError = DioException(
          requestOptions: RequestOptions(path: '/s/bad'),
          response: Response(
            requestOptions: RequestOptions(path: '/s/bad'),
            statusCode: 404,
          ),
          type: DioExceptionType.badResponse,
        );

      await _pumpShareViewerScreen(tester, fakeClient, token: 'bad');
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('share_viewer_error')), findsOneWidget);
      expect(
          find.textContaining('invalid or has been revoked'), findsOneWidget);
    });

    testWidgets('shows 410 error message for an expired token', (tester) async {
      final fakeClient = _FakePublicApiClient()
        ..pageError = DioException(
          requestOptions: RequestOptions(path: '/s/expired'),
          response: Response(
            requestOptions: RequestOptions(path: '/s/expired'),
            statusCode: 410,
          ),
          type: DioExceptionType.badResponse,
        );

      await _pumpShareViewerScreen(tester, fakeClient, token: 'expired');
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('share_viewer_error')), findsOneWidget);
      expect(find.textContaining('expired'), findsOneWidget);
    });

    testWidgets('shows network error message for a connection failure',
        (tester) async {
      final fakeClient = _FakePublicApiClient()
        ..pageError = DioException(
          requestOptions: RequestOptions(path: '/s/tok'),
          type: DioExceptionType.connectionError,
        );

      await _pumpShareViewerScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('share_viewer_error')), findsOneWidget);
      expect(find.textContaining('Could not reach the server'), findsOneWidget);
    });

    testWidgets('shows retry button on error', (tester) async {
      final fakeClient = _FakePublicApiClient()
        ..pageError = DioException(
          requestOptions: RequestOptions(path: '/s/tok'),
          type: DioExceptionType.connectionError,
        );

      await _pumpShareViewerScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('share_viewer_retry')), findsOneWidget);
    });

    testWidgets('retry button re-calls getSharedMediaPage', (tester) async {
      final fakeClient = _FakePublicApiClient()
        ..pageError = DioException(
          requestOptions: RequestOptions(path: '/s/tok'),
          type: DioExceptionType.connectionError,
        );

      await _pumpShareViewerScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Clear the error and provide a valid response for the retry.
      fakeClient
        ..pageError = null
        ..pageJson = _kVideoShareJson;

      await tester.tap(find.byKey(const Key('share_viewer_retry')));
      await tester.pumpAndSettle();

      // After a successful retry the metadata view is shown.
      expect(find.byKey(const Key('share_viewer_filename')), findsOneWidget);
      // getSharedMediaPage was called twice: once on init, once on retry.
      expect(fakeClient.callCount, equals(2));
    });
  });

  // --------------------------------------------------------------------------
  // shareViewerErrorMessage helper
  // --------------------------------------------------------------------------

  group('shareViewerErrorMessage', () {
    test('returns invalid-link message for 404', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/s/bad'),
        response: Response(
          requestOptions: RequestOptions(path: '/s/bad'),
          statusCode: 404,
        ),
        type: DioExceptionType.badResponse,
      );
      expect(shareViewerErrorMessage(err),
          contains('invalid or has been revoked'));
    });

    test('returns expired-link message for 410', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/s/expired'),
        response: Response(
          requestOptions: RequestOptions(path: '/s/expired'),
          statusCode: 410,
        ),
        type: DioExceptionType.badResponse,
      );
      expect(shareViewerErrorMessage(err), contains('expired'));
    });

    test('returns connectivity message for connectionError', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/s/tok'),
        type: DioExceptionType.connectionError,
      );
      expect(
          shareViewerErrorMessage(err), contains('Could not reach the server'));
    });

    test('returns generic message for non-Dio error', () {
      expect(shareViewerErrorMessage(Exception('boom')),
          contains('Unexpected error'));
    });
  });
}
