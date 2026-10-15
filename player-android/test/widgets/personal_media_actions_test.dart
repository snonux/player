import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/models/media_bookmark.dart';
import 'package:player_android/models/note.dart';
import 'package:player_android/services/personal_media_store.dart';
import 'package:player_android/widgets/personal_media_actions.dart';

class _Store implements PersonalMediaStore {
  _Store([this.identity = 'local:1']);
  @override
  final String identity;
  final bookmarks = <MediaBookmark>[];
  Note? note;
  bool favorite = false;
  bool fail = false;
  Completer<List<MediaBookmark>>? pending;
  Completer<bool>? pendingFavorite;
  int favoriteLoads = 0;
  @override
  Future<List<MediaBookmark>> listBookmarks() async {
    if (pending != null) return pending!.future;
    if (fail) throw StateError('Failure');
    return List.of(bookmarks);
  }

  @override
  Future<MediaBookmark> addBookmark(double seconds, String label) async {
    if (fail) throw StateError('Failure');
    final result = MediaBookmark(
        id: bookmarks.length + 1,
        positionSeconds: seconds,
        label: label.trim());
    bookmarks.add(result);
    return result;
  }

  @override
  Future<void> deleteBookmark(int id) async {
    if (fail) throw StateError('Failure');
    bookmarks.removeWhere((item) => item.id == id);
  }

  @override
  Future<Note?> getNote() async => note;
  @override
  Future<Note> saveNote(String content) async {
    if (fail) throw StateError('Failure');
    return note = Note(id: 1, mediaId: 1, userId: 0, content: content);
  }

  @override
  Future<void> deleteNote() async {
    note = null;
  }

  @override
  Future<bool> getFavorite() async {
    favoriteLoads++;
    if (fail) throw StateError('Failure');
    if (pendingFavorite != null) return pendingFavorite!.future;
    return favorite;
  }

  @override
  Future<bool> toggleFavorite() async => favorite = !favorite;
}

Future<void> _pumpActions(WidgetTester tester, _Store store,
        {bool image = false, ValueChanged<Duration>? seek}) =>
    tester.pumpWidget(ProviderScope(
        child: MaterialApp(
            home: Scaffold(
      appBar: AppBar(actions: [
        PersonalMediaActions(
            store: store,
            title: 'Track',
            position: image ? null : () => const Duration(milliseconds: 42500),
            duration: () => const Duration(minutes: 2),
            onSeek: (position) async {
              seek?.call(position);
            })
      ]),
      body: const Text('Playback'),
    ))));

void main() {
  testWidgets(
      'playback saves named timestamp, lists, jumps and deletes; notes flush on Back',
      (tester) async {
    final store = _Store();
    Duration? sought;
    await _pumpActions(tester, store, seek: (position) => sought = position);
    await tester.tap(find.byTooltip('Bookmarks'));
    await tester.pumpAndSettle();
    await tester.enterText(
        find.byKey(const Key('bookmark_label')), '  Chapter two  ');
    await tester.tap(find.text('Save current position'));
    await tester.pumpAndSettle();
    expect(store.bookmarks.single.positionSeconds, 42.5);
    expect(find.text('Chapter two'), findsOneWidget);
    expect(find.text('0:42'), findsOneWidget);
    await tester.tap(find.text('Chapter two'));
    await tester.pumpAndSettle();
    expect(sought, const Duration(milliseconds: 42500));
    await tester.tap(find.byTooltip('Bookmarks'));
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('Delete bookmark'));
    await tester.pumpAndSettle();
    expect(store.bookmarks, isEmpty);
    expect(find.text('No saved positions yet.'), findsOneWidget);
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('Notes'));
    await tester.pumpAndSettle();
    await tester.enterText(
        find.byKey(const Key('notes_text_field')), 'New device note');
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    expect(store.note?.content, 'New device note');
    expect(find.text('Playback'), findsOneWidget);
    await tester.tap(find.byTooltip('Notes'));
    await tester.pumpAndSettle();
    expect(find.text('New device note'), findsOneWidget);
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
  });

  testWidgets('image favorite toggles and image notes save', (tester) async {
    final store = _Store('offline:image');
    await _pumpActions(tester, store, image: true);
    await tester.pumpAndSettle();
    expect(find.byTooltip('Bookmarks'), findsNothing);
    await tester.tap(find.byTooltip('Add to favorites'));
    await tester.pumpAndSettle();
    expect(store.favorite, isTrue);
    expect(find.byTooltip('Remove from favorites'), findsOneWidget);
    await tester.tap(find.byTooltip('Notes'));
    await tester.pumpAndSettle();
    await tester.enterText(
        find.byKey(const Key('notes_text_field')), 'Picture note');
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    expect(store.note?.content, 'Picture note');
    await tester.tap(find.byTooltip('Remove from favorites'));
    await tester.pumpAndSettle();
    expect(store.favorite, isFalse);
  });

  testWidgets('bookmark failure preserves draft and offers retry',
      (tester) async {
    final store = _Store()..fail = true;
    await _pumpActions(tester, store);
    await tester.tap(find.byTooltip('Bookmarks'));
    await tester.pumpAndSettle();
    expect(
        find.text('Could not load bookmarks. Please retry.'), findsOneWidget);
    store.fail = false;
    await tester.tap(find.text('Retry'));
    await tester.pumpAndSettle();
    await tester.enterText(
        find.byKey(const Key('bookmark_label')), 'Keep draft');
    store.fail = true;
    await tester.tap(find.text('Save current position'));
    await tester.pumpAndSettle();
    expect(find.text('Keep draft'), findsOneWidget);
    store.fail = false;
    await tester.tap(find.text('Save current position'));
    await tester.pumpAndSettle();
    expect(store.bookmarks.single.label, 'Keep draft');
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
  });

  testWidgets(
      'small landscape with keyboard keeps save reachable without overflow',
      (tester) async {
    tester.view.physicalSize = const Size(640, 320);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    final store = _Store();
    await _pumpActions(tester, store);
    await tester.tap(find.byTooltip('Bookmarks'));
    await tester.pumpAndSettle();
    tester.view.viewInsets = const FakeViewPadding(bottom: 160);
    addTearDown(tester.view.resetViewInsets);
    await tester.pumpAndSettle();
    await tester.enterText(
        find.byKey(const Key('bookmark_label')), 'Landscape');
    await tester.ensureVisible(find.text('Save current position'));
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
    await tester.tap(find.text('Save current position'));
    await tester.pumpAndSettle();
    expect(store.bookmarks.single.label, 'Landscape');
  });

  testWidgets(
      'late load from previous item cannot replace current bookmark list',
      (tester) async {
    final old = _Store('local:old')..pending = Completer<List<MediaBookmark>>();
    final next = _Store('local:next');
    Widget sheet(_Store store) => MaterialApp(
        home: Scaffold(
            body: BookmarkSheet(
                store: store,
                position: () => Duration.zero,
                onSeek: (_) async {})));
    await tester.pumpWidget(sheet(old));
    await tester.pump();
    await tester.pumpWidget(sheet(next));
    await tester.pumpAndSettle();
    old.pending!.complete(
        [const MediaBookmark(id: 1, positionSeconds: 1, label: 'Old')]);
    await tester.pumpAndSettle();
    expect(find.text('Old'), findsNothing);
    expect(find.text('No saved positions yet.'), findsOneWidget);
  });

  testWidgets('malformed bookmark positions show retry without crashing',
      (tester) async {
    final store = _Store()
      ..bookmarks.add(const MediaBookmark(
          id: 1, positionSeconds: double.infinity, label: 'Invalid'));
    await _pumpActions(tester, store);
    await tester.tap(find.byTooltip('Bookmarks'));
    await tester.pumpAndSettle();
    expect(
        find.text('Could not load bookmarks. Please retry.'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
  testWidgets('initial slow load cannot overwrite a newly saved bookmark',
      (tester) async {
    final initial = Completer<List<MediaBookmark>>();
    final store = _Store()..pending = initial;
    await _pumpActions(tester, store);
    await tester.tap(find.byTooltip('Bookmarks'));
    await tester.pump(const Duration(milliseconds: 500));
    await tester.pump(const Duration(milliseconds: 500));
    store.pending = null;
    await tester.enterText(
        find.byKey(const Key('bookmark_label')), 'New chapter');
    await tester.ensureVisible(find.text('Save current position'));
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(find.text('Save current position'));
    await tester.pumpAndSettle();
    expect(find.text('New chapter'), findsOneWidget);
    initial.complete([]);
    await tester.pumpAndSettle();
    expect(find.text('New chapter'), findsOneWidget);
  });

  testWidgets(
      'double bookmark jump and Back during seek cannot pop the playback route',
      (tester) async {
    final store = _Store()
      ..bookmarks.add(
          const MediaBookmark(id: 1, positionSeconds: 5, label: 'Chapter'));
    final seek = Completer<void>();
    var calls = 0;
    await tester.pumpWidget(MaterialApp(
        home: Builder(
            builder: (context) => Scaffold(
                body: TextButton(
                    onPressed: () => showModalBottomSheet<void>(
                        context: context,
                        builder: (_) => BookmarkSheet(
                            store: store,
                            position: () => Duration.zero,
                            onSeek: (_) async {
                              calls++;
                              await seek.future;
                            })),
                    child: const Text('Playback'))))));
    await tester.tap(find.text('Playback'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Chapter'));
    await tester.pump();
    await tester.tap(find.text('Chapter'));
    await tester.pump();
    expect(calls, 1);
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    seek.complete();
    await tester.pumpAndSettle();
    expect(find.text('Playback'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
  testWidgets('favorite reload invalidates an old in-flight load on return',
      (tester) async {
    final old = Completer<bool>();
    final store = _Store()..pendingFavorite = old;
    Widget control(int revision) => MaterialApp(
        home: Scaffold(body: FavoriteButton(store: store, revision: revision)));
    await tester.pumpWidget(control(0));
    await tester.pump();
    store.pendingFavorite = null;
    store.favorite = true;
    await tester.pumpWidget(control(1));
    await tester.pumpAndSettle();
    expect(find.byTooltip('Remove from favorites'), findsOneWidget);
    old.complete(false);
    await tester.pumpAndSettle();
    expect(find.byTooltip('Remove from favorites'), findsOneWidget);
    await tester.tap(find.byTooltip('Remove from favorites'));
    await tester.pumpAndSettle();
    expect(store.favorite, isFalse);
  });
  testWidgets(
      'favorite Retry disables overlapping reads and toggles until it finishes',
      (tester) async {
    final store = _Store()..fail = true;
    await tester.pumpWidget(
        MaterialApp(home: Scaffold(body: FavoriteButton(store: store))));
    await tester.pumpAndSettle();
    final delayed = Completer<bool>();
    store.fail = false;
    store.pendingFavorite = delayed;
    final retry = tester.widget<IconButton>(find.byType(IconButton)).onPressed!;
    retry();
    retry();
    await tester.pump();
    expect(store.favoriteLoads, 2);
    expect(
        tester.widget<IconButton>(find.byType(IconButton)).onPressed, isNull);
    delayed.complete(false);
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('Add to favorites'));
    await tester.pumpAndSettle();
    expect(store.favorite, isTrue);
    expect(find.byTooltip('Remove from favorites'), findsOneWidget);
  });
}
