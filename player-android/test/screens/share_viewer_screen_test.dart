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

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/providers/public_api_client_provider.dart';
import 'package:player_android/screens/share_viewer_screen.dart';
import 'package:player_android/utils/error_mappers.dart';

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

  /// Returns the test base URL without a trailing slash, matching the
  /// production [PlayerApiClient.baseUrl] contract used in [ShareViewerScreen].
  @override
  String get baseUrl => 'http://test.local';

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
    testWidgets('play button is present after a successful load', (tester) async {
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
      expect(find.textContaining('invalid or has been revoked'), findsOneWidget);
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
      expect(shareViewerErrorMessage(err), contains('invalid or has been revoked'));
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
      expect(shareViewerErrorMessage(err), contains('Could not reach the server'));
    });

    test('returns generic message for non-Dio error', () {
      expect(shareViewerErrorMessage(Exception('boom')), contains('Unexpected error'));
    });
  });
}
