// Widget tests for AdminRescanScreen (admin_rescan_screen.dart).
//
// Tests cover:
//   1. Trigger button visible and enabled when not scanning.
//   2. Tapping trigger calls triggerRescan and shows scanning state.
//   3. Poll timer fires and updates status.
//   4. "Scan complete" shown when done.
//   5. Error state shown when getScanProgress throws.
//   6. Loading spinner shown before first status fetch completes.
//
// Riverpod providers are overridden with fakes so tests run without a real
// server or OS keychain.
//
// Run with: flutter test test/screens/admin_rescan_screen_test.dart

import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/screens/admin_rescan_screen.dart';

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

/// In-memory [TokenStorage] that returns a fixed username without hitting
/// the OS keychain.
class _FakeTokenStorage implements TokenStorage {
  @override
  Future<String?> readToken() async => 'admin';

  @override
  Future<void> writeToken(String token) async {}

  @override
  Future<void> deleteToken() async {}
}

/// Controllable [PlayerApiClient] stub for [AdminRescanScreen] tests.
///
/// [getScanProgress] and [triggerRescan] are the primary subjects.
/// All other methods remain [UnimplementedError] — the screen calls only these.
class _FakeApiClient extends PlayerApiClient {
  _FakeApiClient() : super(dio: Dio(BaseOptions(baseUrl: 'http://test.local')));

  // ---- getScanProgress ----

  /// When non-null, [getScanProgress] returns this map.
  Map<String, dynamic>? progressResult;

  /// When non-null, [getScanProgress] throws this instead.
  Object? progressError;

  /// Number of times [getScanProgress] has been called.
  int getScanProgressCallCount = 0;

  @override
  Future<Map<String, dynamic>> getScanProgress() async {
    getScanProgressCallCount++;
    if (progressError != null) throw progressError!;
    return progressResult!;
  }

  // ---- triggerRescan ----

  /// When non-null, [triggerRescan] throws this instead of returning.
  Object? triggerError;

  /// Number of times [triggerRescan] has been called.
  int triggerRescanCallCount = 0;

  @override
  Future<void> triggerRescan() async {
    triggerRescanCallCount++;
    if (triggerError != null) throw triggerError!;
  }
}

/// [PlayerApiClient] stub whose [getScanProgress] is controlled by an external
/// [Completer] — lets tests inspect the loading state before the fetch resolves.
class _DelayedFakeApiClient extends PlayerApiClient {
  _DelayedFakeApiClient() : super(dio: Dio());

  final _completer = Completer<Map<String, dynamic>>();

  /// Resolves the pending [getScanProgress] with [result].
  void complete(Map<String, dynamic> result) => _completer.complete(result);

  @override
  Future<Map<String, dynamic>> getScanProgress() => _completer.future;
}

// ---------------------------------------------------------------------------
// Sample data
// ---------------------------------------------------------------------------

/// Progress map representing an idle scanner (no scan running, nothing scanned).
const _kIdleProgress = <String, dynamic>{
  'running': false,
  'current_set': '',
  'sets_total': 0,
  'sets_done': 0,
  'files_total': 0,
  'files_done': 0,
};

/// Progress map representing an active scan in progress.
const _kRunningProgress = <String, dynamic>{
  'running': true,
  'current_set': 'Music',
  'sets_total': 3,
  'sets_done': 1,
  'files_total': 500,
  'files_done': 200,
};

/// Progress map representing a completed scan (not running, files > 0).
const _kCompleteProgress = <String, dynamic>{
  'running': false,
  'current_set': '',
  'sets_total': 3,
  'sets_done': 3,
  'files_total': 500,
  'files_done': 500,
};

// ---------------------------------------------------------------------------
// Helper: pump AdminRescanScreen inside a minimal ProviderScope.
// ---------------------------------------------------------------------------

/// Pumps [AdminRescanScreen] with a [ProviderScope] that overrides
/// [apiClientProvider] with [fakeClient] and [tokenStorageProvider] with an
/// in-memory fake.  Using [MaterialApp] is sufficient because the screen does
/// not navigate away — it only shows SnackBars and updates its own state.
Future<void> _pumpRescanScreen(
  WidgetTester tester,
  PlayerApiClient fakeClient,
) async {
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        tokenStorageProvider.overrideWithValue(_FakeTokenStorage()),
        apiClientProvider.overrideWithValue(fakeClient),
      ],
      child: const MaterialApp(home: AdminRescanScreen()),
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
    testWidgets('shows loading spinner before first status fetch completes',
        (tester) async {
      final fakeClient = _DelayedFakeApiClient();

      await _pumpRescanScreen(tester, fakeClient);
      // One pump so addPostFrameCallback fires but Future has not resolved.
      await tester.pump();

      expect(
        find.byKey(const Key('admin_rescan_status_loading')),
        findsOneWidget,
      );

      // Resolve to avoid dangling-async warnings.
      fakeClient.complete(_kIdleProgress);
      await tester.pumpAndSettle();
    });
  });

  // --------------------------------------------------------------------------
  // Idle state
  // --------------------------------------------------------------------------

  group('idle state', () {
    testWidgets('trigger button is visible and enabled when not scanning',
        (tester) async {
      final fakeClient = _FakeApiClient()..progressResult = _kIdleProgress;

      await _pumpRescanScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('admin_rescan_trigger')), findsOneWidget);

      // The trigger button key is on a FilledButton.icon widget — locate the
      // FilledButton by key directly using its widget type.
      final btn = tester.widgetList<FilledButton>(find.byType(FilledButton)).first;
      expect(btn.onPressed, isNotNull);
    });

    testWidgets('shows idle label when no scan has run', (tester) async {
      final fakeClient = _FakeApiClient()..progressResult = _kIdleProgress;

      await _pumpRescanScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(
        find.byKey(const Key('admin_rescan_idle_label')),
        findsOneWidget,
      );
    });
  });

  // --------------------------------------------------------------------------
  // Trigger action
  // --------------------------------------------------------------------------

  group('trigger action', () {
    testWidgets('tapping trigger calls triggerRescan', (tester) async {
      // Use idle progress so no polling timer is started — pumpAndSettle is
      // safe when there is no active periodic timer.
      final fakeClient = _FakeApiClient()..progressResult = _kIdleProgress;

      await _pumpRescanScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('admin_rescan_trigger')));
      await tester.pumpAndSettle();

      expect(fakeClient.triggerRescanCallCount, equals(1));
    });

    testWidgets('shows scanning state after trigger when scan is running',
        (tester) async {
      // After trigger+fetch, the screen sees a running scan and starts a polling
      // timer.  Use pump() with an explicit duration instead of pumpAndSettle()
      // so the test does not time out waiting for the periodic timer to stop.
      final fakeClient = _FakeApiClient()..progressResult = _kRunningProgress;

      await _pumpRescanScreen(tester, fakeClient);
      // Allow initState fetch to complete.
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 50));

      await tester.tap(find.byKey(const Key('admin_rescan_trigger')));
      // Allow trigger + post-trigger fetch to complete.
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 50));

      // Running label should be visible.
      expect(
        find.byKey(const Key('admin_rescan_running_label')),
        findsOneWidget,
      );
    });

    testWidgets('trigger button is disabled while scan is running',
        (tester) async {
      // Same approach: after trigger+fetch the scan is running and the polling
      // timer is active, so we avoid pumpAndSettle.
      final fakeClient = _FakeApiClient()..progressResult = _kRunningProgress;

      await _pumpRescanScreen(tester, fakeClient);
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 50));

      // Trigger the scan.
      await tester.tap(find.byKey(const Key('admin_rescan_trigger')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 50));

      // Button should now be disabled (scan is running).
      final btn = tester.widgetList<FilledButton>(find.byType(FilledButton)).first;
      expect(btn.onPressed, isNull);
    });

    testWidgets('shows error snackbar when triggerRescan throws', (tester) async {
      final fakeClient = _FakeApiClient()
        ..progressResult = _kIdleProgress
        ..triggerError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/admin/scan'),
          type: DioExceptionType.connectionError,
        );

      await _pumpRescanScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('admin_rescan_trigger')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('admin_rescan_error')), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Poll timer
  // --------------------------------------------------------------------------

  group('poll timer', () {
    testWidgets('poll timer fires and updates status from running to complete',
        (tester) async {
      // Start with a running scan so the polling timer begins.
      final fakeClient = _FakeApiClient()
        ..progressResult = _kRunningProgress;

      await _pumpRescanScreen(tester, fakeClient);
      // Allow the initial fetch to complete without pumpAndSettle (the timer
      // is now active and would cause pumpAndSettle to spin forever).
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 50));

      final callCountAfterInit = fakeClient.getScanProgressCallCount;
      expect(find.byKey(const Key('admin_rescan_running_label')), findsOneWidget);

      // Switch the fake to return "complete" so the next poll stops the timer.
      fakeClient.progressResult = _kCompleteProgress;

      // Advance time past the 2-second poll interval so the timer fires.
      await tester.pump(const Duration(seconds: 2));
      // Allow the polled fetch to settle (timer is now cancelled because scan
      // is no longer running, so pumpAndSettle is safe again).
      await tester.pumpAndSettle();

      // At least one more poll occurred.
      expect(
        fakeClient.getScanProgressCallCount,
        greaterThan(callCountAfterInit),
      );

      // Status card now shows "Scan complete".
      expect(find.byKey(const Key('admin_rescan_idle_label')), findsOneWidget);
      expect(find.textContaining('Scan complete'), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Scan complete state
  // --------------------------------------------------------------------------

  group('scan complete state', () {
    testWidgets('shows Scan complete label when files have been scanned',
        (tester) async {
      final fakeClient = _FakeApiClient()
        ..progressResult = _kCompleteProgress;

      await _pumpRescanScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('admin_rescan_idle_label')), findsOneWidget);
      expect(find.textContaining('Scan complete'), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Error state
  // --------------------------------------------------------------------------

  group('error state', () {
    testWidgets('shows error message when getScanProgress throws', (tester) async {
      final fakeClient = _FakeApiClient()
        ..progressError = DioException(
          requestOptions: RequestOptions(path: '/api/v1/admin/scan-progress'),
          type: DioExceptionType.connectionError,
        );

      await _pumpRescanScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('admin_rescan_error')), findsOneWidget);
      expect(find.textContaining('Could not reach the server'), findsOneWidget);
    });
  });
}
