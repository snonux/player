// Widget tests for NotesEditorScreen (notes_editor_screen.dart).
//
// Tests cover:
//   1. Shows a loading indicator while getNote is in flight.
//   2. Loads an existing note into the editor on init.
//   3. Shows an empty editor when getNote returns null (no note yet).
//   4. Shows an error view when getNote throws a DioException.
//   5. Retry button triggers a fresh getNote call after an error.
//   6. Auto-save fires after the 800 ms debounce when text changes.
//   7. Auto-save does NOT fire before the debounce delay elapses.
//   8. Manual "Save" menu item saves immediately without debounce.
//   9. "Clear" menu item shows a confirmation dialog.
//  10. Confirming the clear dialog calls deleteNote and empties the editor.
//  11. Cancelling the clear dialog leaves the editor unchanged.
//  12. notesErrorMessage unit tests (connection, 404, 500, generic).
//
// Riverpod providers are overridden with fakes so tests run without a real
// server or OS keychain.
//
// Run with: flutter test test/screens/notes_editor_screen_test.dart

import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/models/models.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/screens/notes_editor_screen.dart';
import 'package:player_android/utils/error_mappers.dart';

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

/// Controllable [PlayerApiClient] stub for [NotesEditorScreen] tests.
///
/// Implements [getNote], [upsertNote], and [deleteNote]; all other methods
/// remain [UnimplementedError] — the screen calls only these.
class _FakeApiClient extends PlayerApiClient {
  _FakeApiClient() : super(dio: Dio());

  /// When set, [getNote] returns this value (may be null for "no note").
  Note? noteResult;

  /// When non-null, [getNote] throws this instead of returning.
  Object? noteError;

  /// Captures the last content passed to [upsertNote].
  String? upsertedContent;

  /// Number of times [upsertNote] has been called.
  int upsertCallCount = 0;

  /// Number of times [deleteNote] has been called.
  int deleteCallCount = 0;

  /// Number of times [getNote] has been called.
  int getNoteCallCount = 0;

  /// When non-null, [upsertNote] throws this instead of returning.
  Object? upsertError;
  Future<Note> Function(int, String)? onUpsert;
  final writes = <(int, String)>[];

  /// When non-null, [deleteNote] throws this instead of returning.
  Object? deleteError;

  @override
  Future<Note?> getNote(int mediaId) async {
    getNoteCallCount++;
    if (noteError != null) throw noteError!;
    return noteResult;
  }

  @override
  Future<Note> upsertNote(int mediaId, String content) async {
    upsertCallCount++;
    upsertedContent = content;
    writes.add((mediaId, content));
    if (onUpsert != null) return onUpsert!(mediaId, content);
    if (upsertError != null) throw upsertError!;
    return Note(
      id: 1,
      mediaId: mediaId,
      userId: 1,
      content: content,
    );
  }

  @override
  Future<void> deleteNote(int mediaId) async {
    deleteCallCount++;
    if (deleteError != null) throw deleteError!;
  }
}

/// [PlayerApiClient] stub that delays [getNote] until [complete] is called.
///
/// Used to inspect mid-flight loading state before the response arrives.
class _DelayedFakeApiClient extends PlayerApiClient {
  _DelayedFakeApiClient() : super(dio: Dio());

  final _completer = Completer<Note?>();

  /// Resolves the pending [getNote] call with [note] (may be null).
  void complete(Note? note) => _completer.complete(note);

  @override
  Future<Note?> getNote(int mediaId) => _completer.future;

  @override
  Future<Note> upsertNote(int mediaId, String content) async {
    return Note(id: 1, mediaId: mediaId, userId: 1, content: content);
  }

  @override
  Future<void> deleteNote(int mediaId) async {}
}

// ---------------------------------------------------------------------------
// Sample data
// ---------------------------------------------------------------------------

/// A sample [Note] with pre-populated content.
const _kNote = Note(
  id: 42,
  mediaId: 7,
  userId: 1,
  content: 'Great scene at 01:23.',
);

/// A DioException representing a network connectivity failure.
DioException _connectionError() => DioException(
      requestOptions: RequestOptions(path: '/api/v1/media/7/notes'),
      type: DioExceptionType.connectionError,
    );

// ---------------------------------------------------------------------------
// Pump helper
// ---------------------------------------------------------------------------

/// Pumps [NotesEditorScreen] inside a [ProviderScope] with overrides.
///
/// [mediaId] defaults to '7' to match [_kNote].  The screen is mounted at
/// `/notes/7` so go_router can match the route if needed.
Future<void> _pumpScreen(
  WidgetTester tester,
  PlayerApiClient fakeClient, {
  String mediaId = '7',
}) async {
  final router = GoRouter(
    initialLocation: '/notes/$mediaId',
    routes: [
      GoRoute(
        path: '/',
        builder: (_, state) => const Scaffold(body: Text('Library')),
        routes: [
          GoRoute(
            path: 'notes/:mediaId',
            builder: (_, state) => NotesEditorScreen(
              mediaId: state.pathParameters['mediaId']!,
            ),
          ),
        ],
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
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

void main() {
  // --------------------------------------------------------------------------
  // Loading state
  // --------------------------------------------------------------------------

  group('loading state', () {
    testWidgets('shows loading indicator while getNote is in flight',
        (tester) async {
      final fakeClient = _DelayedFakeApiClient();

      await _pumpScreen(tester, fakeClient);
      // Pump one frame: addPostFrameCallback fires but Future not yet resolved.
      await tester.pump();

      expect(find.byKey(const Key('notes_loading')), findsOneWidget);
      expect(find.byType(CircularProgressIndicator), findsAtLeast(1));

      // Resolve to avoid "async work pending" warnings at test teardown.
      fakeClient.complete(_kNote);
      await tester.pumpAndSettle();
    });
  });

  // --------------------------------------------------------------------------
  // Loads existing note
  // --------------------------------------------------------------------------

  group('loads existing note', () {
    testWidgets('populates editor with note content on successful load',
        (tester) async {
      final fakeClient = _FakeApiClient()..noteResult = _kNote;

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // The text field should contain the note's content.
      expect(find.byKey(const Key('notes_text_field')), findsOneWidget);
      expect(find.text('Great scene at 01:23.'), findsOneWidget);
    });

    testWidgets('shows empty editor when getNote returns null', (tester) async {
      final fakeClient = _FakeApiClient()..noteResult = null;

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('notes_text_field')), findsOneWidget);
      // The hint text is shown when the controller is empty.
      expect(find.text('Write your notes here…'), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // Error state
  // --------------------------------------------------------------------------

  group('error state', () {
    testWidgets('shows error view when getNote throws', (tester) async {
      final fakeClient = _FakeApiClient()..noteError = _connectionError();

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('notes_error')), findsOneWidget);
      expect(find.textContaining('Could not reach the server'), findsOneWidget);
    });

    testWidgets('retry button triggers a fresh getNote call', (tester) async {
      final fakeClient = _FakeApiClient()..noteError = _connectionError();

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('notes_retry')), findsOneWidget);

      // Fix the error so the retry succeeds.
      fakeClient
        ..noteError = null
        ..noteResult = _kNote;

      await tester.tap(find.byKey(const Key('notes_retry')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('notes_text_field')), findsOneWidget);
      expect(find.text('Great scene at 01:23.'), findsOneWidget);
      // getNote was called twice: once on init, once on retry.
      expect(fakeClient.getNoteCallCount, equals(2));
    });
  });

  // --------------------------------------------------------------------------
  // Auto-save (debounce)
  // --------------------------------------------------------------------------

  group('auto-save debounce', () {
    testWidgets('auto-save fires after the debounce delay', (tester) async {
      final fakeClient = _FakeApiClient()..noteResult = _kNote;

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Modify the text in the editor.
      await tester.enterText(
        find.byKey(const Key('notes_text_field')),
        'Updated note.',
      );

      // Before the debounce delay expires, no upsert should have been called.
      await tester.pump(const Duration(milliseconds: 500));
      expect(fakeClient.upsertCallCount, equals(0));

      // After 800 ms the auto-save fires.
      await tester.pump(const Duration(milliseconds: 400));
      expect(fakeClient.upsertCallCount, equals(1));
      expect(fakeClient.upsertedContent, equals('Updated note.'));
    });

    testWidgets('auto-save does NOT fire before the debounce delay',
        (tester) async {
      final fakeClient = _FakeApiClient()..noteResult = _kNote;

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.enterText(
        find.byKey(const Key('notes_text_field')),
        'Typing…',
      );

      // 700 ms — still within the debounce window; no call yet.
      await tester.pump(const Duration(milliseconds: 700));
      expect(fakeClient.upsertCallCount, equals(0));

      // Let the timer expire cleanly to avoid "pending timers" warnings.
      await tester.pump(const Duration(milliseconds: 200));
    });

    testWidgets('auto-save does not fire when content is unchanged',
        (tester) async {
      final fakeClient = _FakeApiClient()..noteResult = _kNote;

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // The editor was seeded with _kNote.content; entering the same value
      // should not trigger an upsert because _savedContent matches.
      await tester.enterText(
        find.byKey(const Key('notes_text_field')),
        _kNote.content,
      );

      await tester.pump(const Duration(milliseconds: 1000));
      expect(fakeClient.upsertCallCount, equals(0));
    });
  });

  group('draft persistence', () {
    const editor = Key('notes_text_field');
    Note saved(String content, {int mediaId = 7}) =>
        Note(id: 1, mediaId: mediaId, userId: 1, content: content);

    testWidgets('app bar Back saves an edit delivered before the next frame',
        (tester) async {
      final pending = Completer<Note>();
      final client = _FakeApiClient()..onUpsert = (_, __) => pending.future;
      await _pumpScreen(tester, client);
      await tester.pumpAndSettle();
      await tester.enterText(find.byKey(editor), 'Quick draft');
      // No frame between the edit and Back: PopScope still has its old value.
      await tester.tap(find.byType(BackButton));
      await tester.pump();
      expect(client.writes, [(7, 'Quick draft')]);
      expect(find.byKey(editor), findsOneWidget);
      expect(tester.widget<TextField>(find.byKey(editor)).readOnly, isTrue);

      pending.complete(saved('Quick draft'));
      await tester.pumpAndSettle();
      expect(find.byKey(editor), findsNothing);
      expect(find.text('Library'), findsOneWidget);
    });

    testWidgets('system Back saves an edit delivered before the next frame',
        (tester) async {
      final client = _FakeApiClient();
      await _pumpScreen(tester, client);
      await tester.pumpAndSettle();
      await tester.enterText(find.byKey(editor), 'Sub-frame draft');
      // Do not pump: Back must not depend on the dirty-state rebuild.
      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      expect(client.writes, [(7, 'Sub-frame draft')]);
      expect(find.byKey(editor), findsNothing);
    });

    testWidgets('Back leaves an unchanged editor without a write',
        (tester) async {
      final client = _FakeApiClient()..noteResult = _kNote;
      await _pumpScreen(tester, client);
      await tester.pumpAndSettle();
      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      expect(client.writes, isEmpty);
      expect(find.byKey(editor), findsNothing);
      expect(find.text('Library'), findsOneWidget);
    });

    testWidgets('edits during a slow save are serialized and saved next',
        (tester) async {
      final first = Completer<Note>();
      final second = Completer<Note>();
      final client = _FakeApiClient();
      client.onUpsert =
          (_, __) => client.upsertCallCount == 1 ? first.future : second.future;
      await _pumpScreen(tester, client);
      await tester.pumpAndSettle();
      await tester.enterText(find.byKey(editor), 'First');
      await tester.pump(const Duration(milliseconds: 900));
      await tester.enterText(find.byKey(editor), 'Latest');
      await tester.pump(const Duration(milliseconds: 900));
      expect(client.writes, [(7, 'First')]);

      first.complete(saved('First'));
      await tester.pump();
      expect(client.writes, [(7, 'First'), (7, 'Latest')]);
      expect(find.text('Latest'), findsOneWidget);
      second.complete(saved('Latest'));
      await tester.pumpAndSettle();
      await tester.pump(const Duration(seconds: 1));
      expect(client.upsertCallCount, 2);
    });

    testWidgets('Back waits for the follow-up save without duplicate requests',
        (tester) async {
      final first = Completer<Note>();
      final second = Completer<Note>();
      final client = _FakeApiClient();
      client.onUpsert =
          (_, __) => client.upsertCallCount == 1 ? first.future : second.future;
      await _pumpScreen(tester, client);
      await tester.pumpAndSettle();
      await tester.enterText(find.byKey(editor), 'First');
      await tester.pump(const Duration(milliseconds: 900));
      await tester.enterText(find.byKey(editor), 'Latest');
      await tester.pump();
      await tester.binding.handlePopRoute();
      await tester.binding.handlePopRoute();
      await tester.pump();
      first.complete(saved('First'));
      await tester.pump();
      expect(find.byKey(editor), findsOneWidget);
      expect(client.writes, [(7, 'First'), (7, 'Latest')]);
      second.complete(saved('Latest'));
      await tester.pumpAndSettle();
      expect(find.byKey(editor), findsNothing);
      expect(client.upsertCallCount, 2);
    });

    testWidgets('a completed pop cannot make the pending Back pop again',
        (tester) async {
      final pending = Completer<Note>();
      final client = _FakeApiClient()..onUpsert = (_, __) => pending.future;
      await _pumpScreen(tester, client);
      await tester.pumpAndSettle();
      await tester.enterText(find.byKey(editor), 'Draft');
      await tester.pump();
      await tester.binding.handlePopRoute();
      final editorContext = tester.element(find.byKey(editor));
      // A separate navigation can complete while Back awaits the save. The
      // outgoing screen remains mounted throughout the route animation.
      unawaited(pending.future.then((_) => Navigator.of(editorContext).pop()));
      pending.complete(saved('Draft'));
      await tester.pumpAndSettle();
      expect(find.text('Library'), findsOneWidget);
      expect(find.byKey(editor), findsNothing);
      expect(tester.takeException(), isNull);
      expect(client.upsertCallCount, 1);
    });

    testWidgets('reverting during an in-flight save restores original content',
        (tester) async {
      final first = Completer<Note>();
      final client = _FakeApiClient()..noteResult = _kNote;
      client.onUpsert = (_, content) => client.upsertCallCount == 1
          ? first.future
          : Future.value(saved(content));
      await _pumpScreen(tester, client);
      await tester.pumpAndSettle();
      await tester.enterText(find.byKey(editor), 'Temporary');
      await tester.pump(const Duration(milliseconds: 900));
      await tester.enterText(find.byKey(editor), _kNote.content);
      first.complete(saved('Temporary'));
      await tester.pumpAndSettle();
      expect(client.writes, [(7, 'Temporary'), (7, _kNote.content)]);
      await tester.pump(const Duration(seconds: 1));
    });

    testWidgets('failed Back keeps the latest draft and Retry saves it',
        (tester) async {
      final first = Completer<Note>();
      final client = _FakeApiClient();
      client.onUpsert = (_, content) => client.upsertCallCount == 1
          ? first.future
          : Future.value(saved(content));
      await _pumpScreen(tester, client);
      await tester.pumpAndSettle();
      await tester.enterText(find.byKey(editor), 'First');
      await tester.pump(const Duration(milliseconds: 900));
      await tester.enterText(find.byKey(editor), 'Latest');
      await tester.pump();
      await tester.binding.handlePopRoute();
      first.completeError(_connectionError());
      await tester.pumpAndSettle();
      expect(find.text('Latest'), findsOneWidget);
      expect(tester.widget<TextField>(find.byKey(editor)).readOnly, isFalse);
      expect(find.textContaining('Could not reach the server'), findsOneWidget);
      await tester.pump(const Duration(seconds: 1));
      expect(client.upsertCallCount, 1);

      await tester.tap(find.text('Retry'));
      await tester.pumpAndSettle();
      expect(client.writes, [(7, 'First'), (7, 'Latest')]);
      expect(find.byKey(editor), findsOneWidget);
      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      expect(find.byKey(editor), findsNothing);
      expect(client.upsertCallCount, 2);
    });

    testWidgets('failed follow-up stays open and Back retries the latest draft',
        (tester) async {
      final first = Completer<Note>();
      final second = Completer<Note>();
      final client = _FakeApiClient();
      client.onUpsert = (_, content) => switch (client.upsertCallCount) {
            1 => first.future,
            2 => second.future,
            _ => Future.value(saved(content)),
          };
      await _pumpScreen(tester, client);
      await tester.pumpAndSettle();
      await tester.enterText(find.byKey(editor), 'First');
      await tester.pump(const Duration(milliseconds: 900));
      await tester.enterText(find.byKey(editor), 'Latest');
      await tester.pump();
      await tester.binding.handlePopRoute();
      first.complete(saved('First'));
      await tester.pump();
      second.completeError(_connectionError());
      await tester.pumpAndSettle();
      expect(find.text('Latest'), findsOneWidget);
      expect(client.upsertCallCount, 2);
      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      expect(client.writes, [(7, 'First'), (7, 'Latest'), (7, 'Latest')]);
      expect(find.byKey(editor), findsNothing);
    });

    testWidgets('disposal cancels debounce without starting requests',
        (tester) async {
      final client = _FakeApiClient();
      await _pumpScreen(tester, client);
      await tester.pumpAndSettle();
      await tester.enterText(find.byKey(editor), 'Draft');
      await tester.pumpWidget(const SizedBox());
      await tester.pump(const Duration(seconds: 1));
      expect(client.writes, isEmpty);
      expect(tester.takeException(), isNull);
    });

    testWidgets('late completion after disposal does not start a follow-up',
        (tester) async {
      final pending = Completer<Note>();
      final client = _FakeApiClient()..onUpsert = (_, __) => pending.future;
      await _pumpScreen(tester, client);
      await tester.pumpAndSettle();
      await tester.enterText(find.byKey(editor), 'First');
      await tester.pump(const Duration(milliseconds: 900));
      await tester.enterText(find.byKey(editor), 'Latest');
      await tester.pumpWidget(const SizedBox());
      pending.complete(saved('First'));
      await tester.pump(const Duration(seconds: 1));
      expect(client.writes, [(7, 'First')]);
      expect(tester.takeException(), isNull);
    });

    testWidgets('media change ignores old save and never sends its follow-up',
        (tester) async {
      final pending = Completer<Note>();
      final client = _FakeApiClient()..onUpsert = (_, __) => pending.future;
      final media = ValueNotifier('7');
      addTearDown(media.dispose);
      await tester.pumpWidget(ProviderScope(
        overrides: [apiClientProvider.overrideWithValue(client)],
        child: MaterialApp(
          home: ValueListenableBuilder(
            valueListenable: media,
            builder: (_, value, __) => NotesEditorScreen(mediaId: value),
          ),
        ),
      ));
      await tester.pumpAndSettle();
      await tester.enterText(find.byKey(editor), 'First');
      await tester.pump(const Duration(milliseconds: 900));
      await tester.enterText(find.byKey(editor), 'Old latest');
      client.noteResult = saved('Other media', mediaId: 8);
      media.value = '8';
      await tester.pumpAndSettle();
      pending.complete(saved('First'));
      await tester.pumpAndSettle();
      await tester.pump(const Duration(seconds: 1));
      expect(find.text('Other media'), findsOneWidget);
      expect(client.writes, [(7, 'First')]);
      expect(tester.takeException(), isNull);
    });
  });

  // --------------------------------------------------------------------------
  // Manual Save
  // --------------------------------------------------------------------------

  group('manual save', () {
    testWidgets('Save menu item calls upsertNote immediately', (tester) async {
      final fakeClient = _FakeApiClient()..noteResult = _kNote;

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      // Change the text without settling: pump only one frame so the text
      // change listener fires but the 800 ms debounce timer has NOT yet elapsed.
      await tester.enterText(
        find.byKey(const Key('notes_text_field')),
        'Manual save test.',
      );
      // Pump a short duration well below the 800 ms debounce threshold so the
      // timer has not fired yet — the menu tap below should be the only save.
      await tester.pump(const Duration(milliseconds: 100));

      // Open the overflow menu.  Use pump (not pumpAndSettle) to avoid
      // advancing the fake clock past the 800 ms debounce threshold.
      await tester.tap(find.byKey(const Key('notes_overflow_menu')));
      await tester.pump();
      await tester.pump();

      // Tap Save.  _manualSave cancels the pending debounce timer first, so
      // the subsequent pumpAndSettle will NOT fire the debounce callback —
      // only the manual upsert call is made.
      // warnIfMissed: false because popup menu items render outside the normal
      // widget tree bounds in tests (they appear in an overlay).
      await tester.tap(
        find.byKey(const Key('notes_save_menu_item')),
        warnIfMissed: false,
      );
      await tester.pumpAndSettle();

      expect(fakeClient.upsertCallCount, equals(1));
      expect(fakeClient.upsertedContent, equals('Manual save test.'));
    });
  });

  // --------------------------------------------------------------------------
  // Clear / delete
  // --------------------------------------------------------------------------

  group('clear note', () {
    testWidgets('Clear menu item shows a confirmation dialog', (tester) async {
      final fakeClient = _FakeApiClient()..noteResult = _kNote;

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('notes_overflow_menu')));
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('notes_clear_menu_item')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('notes_clear_dialog')), findsOneWidget);
    });

    testWidgets('confirming clear calls deleteNote and empties the editor',
        (tester) async {
      final fakeClient = _FakeApiClient()..noteResult = _kNote;

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('notes_overflow_menu')));
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('notes_clear_menu_item')));
      await tester.pumpAndSettle();

      // Tap the "Clear" confirm button inside the dialog.
      await tester.tap(find.byKey(const Key('notes_clear_confirm')));
      await tester.pumpAndSettle();

      expect(fakeClient.deleteCallCount, equals(1));
      // Editor should now be empty.
      final textField = tester.widget<TextField>(
        find.byKey(const Key('notes_text_field')),
      );
      expect(textField.controller?.text, equals(''));
    });

    testWidgets('clear waits for an autosave started during confirmation',
        (tester) async {
      final pending = Completer<Note>();
      final client = _FakeApiClient()
        ..noteResult = _kNote
        ..onUpsert = (_, __) => pending.future;
      await _pumpScreen(tester, client);
      await tester.pumpAndSettle();
      await tester.enterText(
        find.byKey(const Key('notes_text_field')),
        'Unsaved draft',
      );
      await tester.tap(find.byKey(const Key('notes_overflow_menu')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 200));
      await tester.tap(find.byKey(const Key('notes_clear_menu_item')));
      await tester.pump(const Duration(seconds: 1));
      await tester.pump(const Duration(milliseconds: 300));
      expect(client.upsertCallCount, 1);
      await tester.tap(find.byKey(const Key('notes_clear_confirm')));
      await tester.pump();
      expect(client.deleteCallCount, 0);
      pending.complete(const Note(
        id: 1,
        mediaId: 7,
        userId: 1,
        content: 'Unsaved draft',
      ));
      await tester.pumpAndSettle();
      expect(client.deleteCallCount, 1);
      final field = tester.widget<TextField>(
        find.byKey(const Key('notes_text_field')),
      );
      expect(field.controller!.text, isEmpty);
      await tester.pump(const Duration(seconds: 1));
      expect(client.upsertCallCount, 1);
    });

    testWidgets('cancelling clear dialog leaves the editor unchanged',
        (tester) async {
      final fakeClient = _FakeApiClient()..noteResult = _kNote;

      await _pumpScreen(tester, fakeClient);
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('notes_overflow_menu')));
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('notes_clear_menu_item')));
      await tester.pumpAndSettle();

      // Tap the "Cancel" button inside the dialog.
      await tester.tap(find.byKey(const Key('notes_clear_cancel')));
      await tester.pumpAndSettle();

      // deleteNote should NOT have been called.
      expect(fakeClient.deleteCallCount, equals(0));
      // Editor content should be unchanged.
      expect(find.text('Great scene at 01:23.'), findsOneWidget);
    });
  });

  // --------------------------------------------------------------------------
  // notesErrorMessage unit tests
  // --------------------------------------------------------------------------

  group('notesErrorMessage', () {
    test('returns connectivity message for connectionError', () {
      expect(
        notesErrorMessage(_connectionError()),
        contains('Could not reach the server'),
      );
    });

    test('returns "not found" message for 404 badResponse', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/api/v1/media/7/notes'),
        response: Response(
          requestOptions: RequestOptions(path: '/api/v1/media/7/notes'),
          statusCode: 404,
        ),
        type: DioExceptionType.badResponse,
      );
      expect(notesErrorMessage(err), contains('not found'));
    });

    test('returns server-error message for 500 badResponse', () {
      final err = DioException(
        requestOptions: RequestOptions(path: '/api/v1/media/7/notes'),
        response: Response(
          requestOptions: RequestOptions(path: '/api/v1/media/7/notes'),
          statusCode: 500,
        ),
        type: DioExceptionType.badResponse,
      );
      expect(notesErrorMessage(err), contains('500'));
    });

    test('returns generic message for non-DioException', () {
      expect(
        notesErrorMessage(Exception('something broke')),
        contains('Unexpected error'),
      );
    });
  });
}
