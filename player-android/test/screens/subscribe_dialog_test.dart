// Widget tests for SubscribeDialog (subscribe_dialog.dart).
//
// Tests cover:
//   1. Dialog renders the feed URL and set-name fields.
//   2. Submit with empty URL shows a validation error.
//   3. Successful subscribe call closes the dialog and shows a SnackBar.
//   4. API error is displayed inline inside the dialog.
//   5. Cancel closes the dialog without calling the API.
//   6. podcastErrorMessage helper unit tests.
//
// No Dio import at test level: [_FakeApiClient] overrides only the relevant
// [PlayerApiClient] methods, keeping tests hermetic (DIP/SRP).
//
// Run with: flutter test test/screens/subscribe_dialog_test.dart

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/models/models.dart';
import 'package:player_android/screens/subscribe_dialog.dart';
import 'package:player_android/utils/error_mappers.dart';

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

/// Controllable [PlayerApiClient] stub for [SubscribeDialog] tests.
///
/// Only [subscribePodcast] needs to be overridden; all other methods remain
/// [UnimplementedError] because the dialog never calls them (SRP).
class _FakeApiClient extends PlayerApiClient {
  _FakeApiClient()
      : super(dio: Dio(BaseOptions(baseUrl: 'http://test.local')));

  /// When non-null, [subscribePodcast] throws this instead of returning.
  Object? subscribeError;

  /// How many times [subscribePodcast] was called.
  int subscribeCallCount = 0;

  /// Last captured [feedUrl] argument.
  String? capturedFeedUrl;

  /// Last captured [setName] argument.
  String? capturedSetName;

  @override
  Future<PodcastFeed> subscribePodcast({
    required String feedUrl,
    String? setName,
  }) async {
    subscribeCallCount++;
    capturedFeedUrl = feedUrl;
    capturedSetName = setName;

    if (subscribeError != null) throw subscribeError!;

    return PodcastFeed(
      id: 1,
      setId: 10,
      feedUrl: feedUrl,
      title: setName ?? 'Test Podcast',
      description: '',
      imageUrl: '',
      lastETag: '',
      checkIntervalMinutes: 60,
      autoDownload: false,
      consecutiveFailures: 0,
    );
  }
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/// Pumps [showSubscribeDialog] inside a bare [Scaffold].
///
/// The dialog is opened immediately after pump so tests can inspect and
/// interact with it directly without going through a parent screen.
/// Returns the [_FakeApiClient] for assertion.
Future<_FakeApiClient> _pumpDialog(
  WidgetTester tester, {
  Object? subscribeError,
}) async {
  final fakeClient = _FakeApiClient()..subscribeError = subscribeError;

  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: Builder(
          builder: (context) {
            // Open the dialog immediately after the first frame is built.
            WidgetsBinding.instance.addPostFrameCallback((_) {
              showSubscribeDialog(context, client: fakeClient);
            });
            return const SizedBox.shrink();
          },
        ),
      ),
    ),
  );

  // First pump: renders the Scaffold.
  // Second pump (settle): executes addPostFrameCallback and renders dialog.
  await tester.pumpAndSettle();

  return fakeClient;
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

void main() {
  // ---------------------------------------------------------------------------
  // Dialog rendering
  // ---------------------------------------------------------------------------

  group('renders dialog fields', () {
    testWidgets('shows feed URL field, set-name field, and action buttons',
        (tester) async {
      await _pumpDialog(tester);

      expect(find.byKey(const Key('subscribe_dialog')), findsOneWidget);
      expect(find.byKey(const Key('subscribe_feed_url')), findsOneWidget);
      expect(find.byKey(const Key('subscribe_set_name')), findsOneWidget);
      expect(find.byKey(const Key('subscribe_submit')), findsOneWidget);
      expect(find.byKey(const Key('subscribe_cancel')), findsOneWidget);
    });
  });

  // ---------------------------------------------------------------------------
  // Input validation
  // ---------------------------------------------------------------------------

  group('input validation', () {
    testWidgets('shows error when feed URL is empty on submit', (tester) async {
      final fakeClient = await _pumpDialog(tester);

      // Tap submit without entering any URL.
      await tester.tap(find.byKey(const Key('subscribe_submit')));
      await tester.pumpAndSettle();

      // Dialog stays open; API was NOT called.
      expect(find.byKey(const Key('subscribe_dialog')), findsOneWidget);
      expect(fakeClient.subscribeCallCount, equals(0));
      expect(find.byKey(const Key('subscribe_error')), findsOneWidget);
      expect(find.textContaining('Feed URL is required'), findsOneWidget);
    });
  });

  // ---------------------------------------------------------------------------
  // Successful subscribe
  // ---------------------------------------------------------------------------

  group('successful subscribe', () {
    testWidgets('closes dialog and shows SnackBar on success', (tester) async {
      await _pumpDialog(tester);

      await tester.enterText(
        find.byKey(const Key('subscribe_feed_url')),
        'https://example.com/feed.rss',
      );
      await tester.tap(find.byKey(const Key('subscribe_submit')));
      // Pump frames to process the async _submit chain, dialog-close animation,
      // and SnackBar entry.  Avoid pumpAndSettle because the SnackBar timer
      // would cause an infinite loop.  300 ms is enough to clear the dialog
      // close animation (~200 ms) without waiting the full SnackBar duration.
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 300));

      // The dialog should have closed.
      expect(find.byKey(const Key('subscribe_dialog')), findsNothing);

      // The success SnackBar must be visible.
      expect(find.textContaining('Podcast subscribed'), findsOneWidget);
    });

    testWidgets('passes feedUrl and null setName to subscribePodcast when '
        'set-name field is blank', (tester) async {
      final fakeClient = await _pumpDialog(tester);

      await tester.enterText(
        find.byKey(const Key('subscribe_feed_url')),
        'https://example.com/feed.rss',
      );
      // Leave set-name field blank.
      await tester.tap(find.byKey(const Key('subscribe_submit')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 100));

      expect(fakeClient.capturedFeedUrl, equals('https://example.com/feed.rss'));
      expect(fakeClient.capturedSetName, isNull);
    });

    testWidgets('passes setName to subscribePodcast when set-name field is '
        'filled', (tester) async {
      final fakeClient = await _pumpDialog(tester);

      await tester.enterText(
        find.byKey(const Key('subscribe_feed_url')),
        'https://example.com/feed.rss',
      );
      await tester.enterText(
        find.byKey(const Key('subscribe_set_name')),
        'My Podcast',
      );
      await tester.tap(find.byKey(const Key('subscribe_submit')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 100));

      expect(fakeClient.capturedSetName, equals('My Podcast'));
    });
  });

  // ---------------------------------------------------------------------------
  // Error display
  // ---------------------------------------------------------------------------

  group('error display', () {
    testWidgets('shows inline error when API throws a network error',
        (tester) async {
      final networkError = DioException(
        requestOptions: RequestOptions(path: '/api/v1/podcasts'),
        type: DioExceptionType.connectionError,
      );

      await _pumpDialog(tester, subscribeError: networkError);

      await tester.enterText(
        find.byKey(const Key('subscribe_feed_url')),
        'https://example.com/feed.rss',
      );
      await tester.tap(find.byKey(const Key('subscribe_submit')));
      await tester.pumpAndSettle();

      // Dialog stays open (error is inline).
      expect(find.byKey(const Key('subscribe_dialog')), findsOneWidget);
      expect(find.byKey(const Key('subscribe_error')), findsOneWidget);
      expect(
        find.textContaining('Could not reach the server'),
        findsOneWidget,
      );
    });

    testWidgets('shows 403 "not admin" message on DioException 403',
        (tester) async {
      final forbiddenError = DioException(
        requestOptions: RequestOptions(path: '/api/v1/podcasts'),
        type: DioExceptionType.badResponse,
        response: Response(
          requestOptions: RequestOptions(path: '/api/v1/podcasts'),
          statusCode: 403,
        ),
      );

      await _pumpDialog(tester, subscribeError: forbiddenError);

      await tester.enterText(
        find.byKey(const Key('subscribe_feed_url')),
        'https://example.com/feed.rss',
      );
      await tester.tap(find.byKey(const Key('subscribe_submit')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('subscribe_error')), findsOneWidget);
      expect(find.textContaining('administrators'), findsOneWidget);
    });

    testWidgets('shows 400 message on DioException 400', (tester) async {
      final badRequestError = DioException(
        requestOptions: RequestOptions(path: '/api/v1/podcasts'),
        type: DioExceptionType.badResponse,
        response: Response(
          requestOptions: RequestOptions(path: '/api/v1/podcasts'),
          statusCode: 400,
        ),
      );

      await _pumpDialog(tester, subscribeError: badRequestError);

      await tester.enterText(
        find.byKey(const Key('subscribe_feed_url')),
        'https://example.com/feed.rss',
      );
      await tester.tap(find.byKey(const Key('subscribe_submit')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('subscribe_error')), findsOneWidget);
      expect(find.textContaining('Invalid feed URL'), findsOneWidget);
    });
  });

  // ---------------------------------------------------------------------------
  // Cancel
  // ---------------------------------------------------------------------------

  group('cancel', () {
    testWidgets('tapping Cancel closes the dialog without calling the API',
        (tester) async {
      final fakeClient = await _pumpDialog(tester);

      await tester.tap(find.byKey(const Key('subscribe_cancel')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('subscribe_dialog')), findsNothing);
      expect(fakeClient.subscribeCallCount, equals(0));
    });
  });

  // ---------------------------------------------------------------------------
  // podcastErrorMessage helper
  // ---------------------------------------------------------------------------

  group('podcastErrorMessage', () {
    test('returns connectivity message for connectionError', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/api/v1/podcasts'),
        type: DioExceptionType.connectionError,
      );
      expect(podcastErrorMessage(err), contains('Could not reach the server'));
    });

    test('returns admin-required message for 403 badResponse', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/api/v1/podcasts'),
        response: Response(
          requestOptions: RequestOptions(path: '/api/v1/podcasts'),
          statusCode: 403,
        ),
        type: DioExceptionType.badResponse,
      );
      expect(podcastErrorMessage(err), contains('administrators'));
    });

    test('returns invalid-feed message for 400 badResponse', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/api/v1/podcasts'),
        response: Response(
          requestOptions: RequestOptions(path: '/api/v1/podcasts'),
          statusCode: 400,
        ),
        type: DioExceptionType.badResponse,
      );
      expect(podcastErrorMessage(err), contains('Invalid feed URL'));
    });

    test('returns generic message for unknown error type', () {
      expect(podcastErrorMessage(Exception('boom')), contains('Unexpected error'));
    });
  });
}
