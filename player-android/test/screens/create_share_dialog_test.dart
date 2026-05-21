// Widget tests for CreateShareDialog (create_share_dialog.dart).
//
// Tests cover:
//   1. Dialog renders the expiry date field and max-uses field.
//   2. Tapping "Change" opens the date picker.
//   3. Successful share call copies the URL to the clipboard and shows a SnackBar.
//   4. API error is displayed inline inside the dialog.
//   5. Non-numeric max-uses input shows a validation error without calling the API.
//   6. Overflow menu item opens the share dialog from MediaDetailScreen.
//
// No Dio import at test level: [_FakeApiClient] overrides only the relevant
// [PlayerApiClient] methods, keeping tests hermetic (DIP/SRP).
//
// Run with: flutter test test/screens/create_share_dialog_test.dart

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/models/models.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/screens/create_share_dialog.dart';
import 'package:player_android/screens/media_detail_screen.dart';

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

/// In-memory [TokenStorage] that avoids the OS keychain in tests.
class _FakeTokenStorage implements TokenStorage {
  const _FakeTokenStorage();

  @override
  Future<String?> readToken() async => 'test-token';

  @override
  Future<void> writeToken(String token) async {}

  @override
  Future<void> deleteToken() async {}
}

/// Controllable [PlayerApiClient] stub for share-dialog tests.
///
/// [createShare] is the primary subject; [getMedia], [toggleFavorite], and
/// [thumbnailUrl] are also overridden to let tests pump [MediaDetailScreen]
/// without hitting [UnimplementedError].
class _FakeApiClient extends PlayerApiClient {
  _FakeApiClient() : super(dio: Dio(BaseOptions(baseUrl: 'http://test.local')));

  /// When non-null [createShare] returns this share.
  Share? shareResult;

  /// When non-null [createShare] throws this instead of returning.
  Object? shareError;

  /// Captured arguments from the last [createShare] call.
  int? capturedMediaId;
  DateTime? capturedExpiresAt;
  int? capturedMaxUses;

  /// How many times [createShare] was called.
  int createShareCallCount = 0;

  @override
  Future<Share> createShare(
    int mediaId, {
    DateTime? expiresAt,
    int? maxUses,
  }) async {
    createShareCallCount++;
    capturedMediaId = mediaId;
    capturedExpiresAt = expiresAt;
    capturedMaxUses = maxUses;

    if (shareError != null) throw shareError!;
    return shareResult!;
  }

  // ---------------------------------------------------------------------------
  // Required stubs for MediaDetailScreen integration tests
  // ---------------------------------------------------------------------------

  Media? mediaResult;

  @override
  Future<Media> getMedia(int mediaId) async => mediaResult!;

  @override
  Future<bool> toggleFavorite(int mediaId) async => false;

  @override
  String thumbnailUrl(int mediaId) => '';

  @override
  String streamUrl(int mediaId) => 'http://test.local/api/v1/media/$mediaId/stream';
}

// ---------------------------------------------------------------------------
// Sample data
// ---------------------------------------------------------------------------

/// A minimal [Share] returned by the fake API.
final _kShare = Share(
  token: 'tok_abc123',
  mediaId: 42,
  createdBy: 1,
  usedCount: 0,
  expiresAt: DateTime.now().add(const Duration(days: 7)),
);

/// Expected URL derived from [_kShare] and the fake base URL.
const _kShareUrl = 'http://test.local/s/tok_abc123';

/// A minimal video [Media] item used in integration tests.
const _kMedia = Media(
  id: 42,
  setId: 1,
  relPath: 'video.mp4',
  fileName: 'video.mp4',
  absPath: '/media/video.mp4',
  type: 'video',
  duration: 60.0,
  codec: 'h264',
  resolution: '1920x1080',
  bitrate: 4000,
  fileSizeBytes: 1048576,
  width: 1920,
  height: 1080,
  thumbnailPath: '',
  playCount: 0,
  favorite: false,
  tags: [],
);

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/// Pumps [showCreateShareDialog] inside a bare [Scaffold] + [ProviderScope].
///
/// The dialog is opened immediately after pump so tests do not need to trigger
/// it through a button — they can inspect and interact with the dialog content
/// directly.
Future<_FakeApiClient> _pumpDialog(
  WidgetTester tester, {
  Share? shareResult,
  Object? shareError,
}) async {
  final fakeClient = _FakeApiClient()
    ..shareResult = shareResult
    ..shareError = shareError;

  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: Builder(
          builder: (context) {
            // Open the dialog immediately after the frame is built.
            WidgetsBinding.instance.addPostFrameCallback((_) {
              showCreateShareDialog(
                context,
                mediaId: 42,
                client: fakeClient,
              );
            });
            return const SizedBox.shrink();
          },
        ),
      ),
    ),
  );

  // First pump: renders the Scaffold.
  // Second pump (settle): executes the addPostFrameCallback and renders dialog.
  await tester.pumpAndSettle();

  return fakeClient;
}

/// Pumps [MediaDetailScreen] with the given [fakeClient] and waits for the
/// screen to finish loading.
Future<void> _pumpMediaDetailScreen(
  WidgetTester tester,
  _FakeApiClient fakeClient,
) async {
  final router = GoRouter(
    initialLocation: '/media/42',
    routes: [
      GoRoute(
        path: '/media/:id',
        builder: (context, state) =>
            MediaDetailScreen(mediaId: state.pathParameters['id']!),
      ),
    ],
  );

  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        tokenStorageProvider.overrideWithValue(const _FakeTokenStorage()),
        apiClientProvider.overrideWithValue(fakeClient),
      ],
      child: MaterialApp.router(routerConfig: router),
    ),
  );

  await tester.pumpAndSettle();
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

void main() {
  // ---------------------------------------------------------------------------
  // Dialog rendering
  // ---------------------------------------------------------------------------

  group('renders dialog fields', () {
    testWidgets('shows expiry date and max-uses field', (tester) async {
      await _pumpDialog(tester, shareResult: _kShare);

      expect(find.byKey(const Key('create_share_dialog')), findsOneWidget);
      expect(find.byKey(const Key('create_share_expiry_date')), findsOneWidget);
      expect(find.byKey(const Key('create_share_max_uses')), findsOneWidget);
      expect(find.byKey(const Key('create_share_submit')), findsOneWidget);
      expect(find.byKey(const Key('create_share_cancel')), findsOneWidget);
    });

    testWidgets('default expiry date is approximately today + 7 days',
        (tester) async {
      await _pumpDialog(tester, shareResult: _kShare);

      // The displayed date should be the formatted version of now + 7 days.
      final expected = DateTime.now().add(const Duration(days: 7));
      final formatted =
          '${expected.year}-${expected.month.toString().padLeft(2, '0')}-${expected.day.toString().padLeft(2, '0')}';

      expect(find.text(formatted), findsOneWidget);
    });
  });

  // ---------------------------------------------------------------------------
  // Successful share
  // ---------------------------------------------------------------------------

  group('successful share', () {
    testWidgets('copies share URL to clipboard and shows SnackBar',
        (tester) async {
      // Intercept clipboard calls so we can assert the written value.
      final List<MethodCall> clipboardCalls = [];
      tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        SystemChannels.platform,
        (call) async {
          clipboardCalls.add(call);
          // Return null for setData; Flutter's test harness expects this.
          return null;
        },
      );

      final fakeClient = await _pumpDialog(tester, shareResult: _kShare);

      // Tap the submit button.
      await tester.tap(find.byKey(const Key('create_share_submit')));
      // Pump frames to process the async _submit chain, navigator pop, and
      // SnackBar entry animation.  We pump with a short explicit duration so
      // we do not wait for the SnackBar's 4-second display timer (which would
      // cause pumpAndSettle to loop forever).  300 ms is enough to clear the
      // material dialog-close animation (~200 ms) without touching the timer.
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 300));

      // The dialog should have closed.
      expect(find.byKey(const Key('create_share_dialog')), findsNothing);

      // createShare was called once.
      expect(fakeClient.createShareCallCount, equals(1));

      // Clipboard.setData was called with the expected share URL.
      final setDataCall = clipboardCalls.firstWhere(
        (c) => c.method == 'Clipboard.setData',
        orElse: () => throw TestFailure('Clipboard.setData was not called'),
      );
      final text = (setDataCall.arguments as Map)['text'] as String?;
      expect(text, equals(_kShareUrl));

      // A SnackBar with the URL should be visible.
      expect(find.textContaining('Share link copied'), findsOneWidget);
      expect(find.textContaining(_kShareUrl), findsOneWidget);

      // Restore the default handler.
      tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        SystemChannels.platform,
        null,
      );
    });

    testWidgets('passes expiresAt and null maxUses to createShare when field is blank',
        (tester) async {
      final fakeClient = await _pumpDialog(tester, shareResult: _kShare);

      await tester.tap(find.byKey(const Key('create_share_submit')));
      // Pump twice to process the async _submit result and apply the widget
      // rebuild that follows it (dialog close, SnackBar entry).  Avoid
      // pumpAndSettle because the SnackBar timer would cause an infinite loop.
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 100));

      expect(fakeClient.capturedMaxUses, isNull);
      expect(fakeClient.capturedExpiresAt, isNotNull);
    });

    testWidgets('passes parsed maxUses to createShare when field is filled',
        (tester) async {
      final fakeClient = await _pumpDialog(tester, shareResult: _kShare);

      await tester.enterText(
        find.byKey(const Key('create_share_max_uses')),
        '5',
      );
      await tester.tap(find.byKey(const Key('create_share_submit')));
      // Pump twice to process the async _submit result and apply the widget
      // rebuild that follows it (dialog close, SnackBar entry).  Avoid
      // pumpAndSettle because the SnackBar timer would cause an infinite loop.
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 100));

      expect(fakeClient.capturedMaxUses, equals(5));
    });
  });

  // ---------------------------------------------------------------------------
  // Error display
  // ---------------------------------------------------------------------------

  group('error display', () {
    testWidgets('shows inline error when API throws a network error',
        (tester) async {
      final networkError = DioException(
        requestOptions: RequestOptions(path: '/api/v1/media/42/shares'),
        type: DioExceptionType.connectionError,
      );

      await _pumpDialog(tester, shareError: networkError);

      await tester.tap(find.byKey(const Key('create_share_submit')));
      await tester.pumpAndSettle();

      // Dialog stays open (error is inline).
      expect(find.byKey(const Key('create_share_dialog')), findsOneWidget);
      expect(find.byKey(const Key('create_share_error')), findsOneWidget);
      expect(
        find.textContaining('Could not reach the server'),
        findsOneWidget,
      );
    });

    testWidgets('shows 404 "not found" message on DioException 404',
        (tester) async {
      final notFoundError = DioException(
        requestOptions: RequestOptions(path: '/api/v1/media/42/shares'),
        type: DioExceptionType.badResponse,
        response: Response(
          requestOptions: RequestOptions(path: '/api/v1/media/42/shares'),
          statusCode: 404,
        ),
      );

      await _pumpDialog(tester, shareError: notFoundError);

      await tester.tap(find.byKey(const Key('create_share_submit')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('create_share_error')), findsOneWidget);
      expect(find.textContaining('Media not found'), findsOneWidget);
    });

    testWidgets('shows validation error when max uses is non-numeric',
        (tester) async {
      final fakeClient = await _pumpDialog(tester, shareResult: _kShare);

      await tester.enterText(
        find.byKey(const Key('create_share_max_uses')),
        'abc',
      );
      await tester.tap(find.byKey(const Key('create_share_submit')));
      await tester.pumpAndSettle();

      // Dialog stays open; API was NOT called.
      expect(find.byKey(const Key('create_share_dialog')), findsOneWidget);
      expect(fakeClient.createShareCallCount, equals(0));
      expect(find.byKey(const Key('create_share_error')), findsOneWidget);
      expect(find.textContaining('whole number'), findsOneWidget);
    });
  });

  // ---------------------------------------------------------------------------
  // Cancel
  // ---------------------------------------------------------------------------

  group('cancel', () {
    testWidgets('tapping Cancel closes the dialog without calling the API',
        (tester) async {
      final fakeClient = await _pumpDialog(tester, shareResult: _kShare);

      await tester.tap(find.byKey(const Key('create_share_cancel')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('create_share_dialog')), findsNothing);
      expect(fakeClient.createShareCallCount, equals(0));
    });
  });

  // ---------------------------------------------------------------------------
  // MediaDetailScreen overflow menu integration
  // ---------------------------------------------------------------------------

  group('MediaDetailScreen overflow menu', () {
    testWidgets('three-dot menu contains Share item', (tester) async {
      final fakeClient = _FakeApiClient()..mediaResult = _kMedia;
      await _pumpMediaDetailScreen(tester, fakeClient);

      // Open the overflow menu.
      await tester.tap(find.byKey(const Key('media_detail_overflow_menu')));
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('media_detail_share_menu_item')),
        findsOneWidget,
      );
      expect(find.text('Share'), findsOneWidget);
    });

    testWidgets('tapping Share opens the create-share dialog', (tester) async {
      final fakeClient = _FakeApiClient()
        ..mediaResult = _kMedia
        ..shareResult = _kShare;

      await _pumpMediaDetailScreen(tester, fakeClient);

      // Open the overflow menu and tap Share.
      await tester.tap(find.byKey(const Key('media_detail_overflow_menu')));
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('media_detail_share_menu_item')));
      await tester.pumpAndSettle();

      // The share dialog should be visible.
      expect(find.byKey(const Key('create_share_dialog')), findsOneWidget);
    });
  });
}
