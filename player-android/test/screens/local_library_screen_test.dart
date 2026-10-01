import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:player_android/models/local_media.dart';
import 'package:player_android/providers/local_library_provider.dart';
import 'package:player_android/screens/local_library_screen.dart';
import 'package:player_android/services/local_document_picker.dart';
import 'package:player_android/services/local_library_manager.dart';
import 'package:player_android/services/local_library_repository.dart';

class _FakePicker implements LocalDocumentPicker {
  final results = <PickedLocalDocument?>[];
  final released = <String>[];
  final unreadableUris = <String>{};

  @override
  Future<PickedLocalDocument?> pick(LocalDocumentKind kind) async =>
      results.removeAt(0);

  @override
  Future<bool> isReadable(String uri) async => !unreadableUris.contains(uri);

  @override
  Future<void> releasePersistedReadGrant(String uri) async {
    released.add(uri);
  }
}

class _MemoryMediaRepository implements LocalMediaRepository {
  final items = <LocalMedia>[];
  int _nextId = 1;

  @override
  Future<List<LocalMedia>> list() async => List.unmodifiable(items);

  @override
  Future<LocalMedia?> getById(int id) async {
    final matches = items.where((item) => item.id == id);
    return matches.isEmpty ? null : matches.first;
  }

  @override
  Future<LocalMedia?> getByUri(String uri) async {
    final matches = items.where((item) => item.uri == uri);
    return matches.isEmpty ? null : matches.first;
  }

  @override
  Future<LocalMedia> importDocument({
    required String uri,
    required String title,
    required String mimeType,
    int? sizeBytes,
    int? durationMs,
    bool ownsPersistedReadGrant = false,
  }) async {
    final existing = await getByUri(uri);
    if (existing != null) return existing;
    final added = LocalMedia(
      id: _nextId++,
      uri: uri,
      title: title,
      mimeType: mimeType,
      ownsPersistedReadGrant: ownsPersistedReadGrant,
      sizeBytes: sizeBytes,
      durationMs: durationMs,
      addedAt: DateTime.now().toUtc(),
    );
    items.add(added);
    return added;
  }

  @override
  Future<LocalMedia> relink({
    required int id,
    required String uri,
    required String title,
    required String mimeType,
    int? sizeBytes,
    int? durationMs,
    bool retainProgress = true,
    bool? ownsPersistedReadGrant,
  }) async {
    final index = items.indexWhere((item) => item.id == id);
    final updated = LocalMedia(
      id: id,
      uri: uri,
      title: title,
      mimeType: mimeType,
      ownsPersistedReadGrant: ownsPersistedReadGrant ?? false,
      sizeBytes: sizeBytes,
      durationMs: durationMs,
      addedAt: items[index].addedAt,
      lastOpenedAt: items[index].lastOpenedAt,
    );
    items[index] = updated;
    return updated;
  }

  @override
  Future<bool> markOpened(int id, {DateTime? at}) async => false;

  @override
  Future<bool> updateDuration(int id, int? durationMs) async => false;

  @override
  Future<bool> remove(int id) async {
    final length = items.length;
    items.removeWhere((item) => item.id == id);
    return items.length < length;
  }
}

class _MemoryUsage implements LocalDocumentUsage {
  @override
  bool isInUse(String uri) => false;
}

class _MemoryGrantCleanup implements LocalGrantCleanupRepository {
  final pending = <String>{};

  @override
  Future<List<String>> pendingUris() async => pending.toList();

  @override
  Future<void> scheduleRelease(String uri) async {
    pending.add(uri);
  }

  @override
  Future<void> forget(String uri) async {
    pending.remove(uri);
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() => SharedPreferences.setMockInitialValues({
        'library_destination': 'local',
      }));

  testWidgets('empty library offers file import and server connection',
      (tester) async {
    final media = _MemoryMediaRepository();
    await tester.pumpWidget(ProviderScope(
      overrides: [
        localMediaRepositoryProvider.overrideWithValue(media),
        localMediaListProvider.overrideWith((ref) => media.list()),
        localDocumentPickerProvider.overrideWithValue(_FakePicker()),
        localLibraryManagerProvider.overrideWithValue(LocalLibraryManager(
          picker: _FakePicker(),
          media: media,
          cleanup: _MemoryGrantCleanup(),
          usage: _MemoryUsage(),
        )),
      ],
      child: const MaterialApp(home: LocalLibraryScreen()),
    ));
    await tester.pumpAndSettle();

    expect(find.text('Your device library is empty'), findsOneWidget);
    expect(find.text('Add files'), findsOneWidget);
    expect(find.text('Connect to server'), findsOneWidget);
  });

  testWidgets('adds picked documents and exposes relink and remove actions',
      (tester) async {
    final media = _MemoryMediaRepository();
    final picker = _FakePicker()
      ..unreadableUris.add('content://provider/selected-track')
      ..results.addAll([
        const PickedLocalDocument(
          uri: 'content://provider/selected-track',
          kind: LocalDocumentKind.audio,
          newPersistedReadGrant: true,
          name: 'Selected track.mp3',
          mimeType: 'audio/mpeg',
          sizeBytes: 2048,
        ),
        const PickedLocalDocument(
          uri: 'content://provider/replacement-track',
          kind: LocalDocumentKind.audio,
          newPersistedReadGrant: true,
          name: 'Replacement track.mp3',
          mimeType: 'audio/mpeg',
          sizeBytes: 4096,
        ),
      ]);
    await tester.pumpWidget(ProviderScope(
      overrides: [
        localMediaRepositoryProvider.overrideWithValue(media),
        localMediaListProvider.overrideWith((ref) => media.list()),
        localDocumentPickerProvider.overrideWithValue(picker),
        localLibraryManagerProvider.overrideWithValue(LocalLibraryManager(
          picker: picker,
          media: media,
          cleanup: _MemoryGrantCleanup(),
          usage: _MemoryUsage(),
        )),
      ],
      child: const MaterialApp(home: LocalLibraryScreen()),
    ));
    await tester.pumpAndSettle();

    await tester.tap(find.byTooltip('Add files'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Choose audio'));
    await tester.pumpAndSettle();
    await tester.pump(const Duration(milliseconds: 300));
    await tester.pumpAndSettle();

    expect(find.text('Selected track.mp3'), findsOneWidget);
    expect(find.text('audio/mpeg · 2 KB'), findsOneWidget);
    await tester.tap(find.byTooltip('Manage Selected track.mp3'));
    await tester.pumpAndSettle();
    expect(find.text('Choose file again'), findsOneWidget);
    expect(find.text('Remove from library'), findsOneWidget);
    await tester.tap(find.text('Choose file again'));
    await tester.pumpAndSettle();
    expect(find.text('Replace this file?'), findsOneWidget);
    await tester.tap(find.text('Keep progress'));
    await tester.pumpAndSettle();
    expect(find.text('Replacement track.mp3'), findsOneWidget);
    expect(find.textContaining('The previous file was unavailable'),
        findsOneWidget);

    await tester.tap(find.byTooltip('Manage Replacement track.mp3'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Remove from library'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Remove').last);
    await tester.pumpAndSettle();

    expect(find.text('Your device library is empty'), findsOneWidget);
    expect(picker.released, [
      'content://provider/selected-track',
      'content://provider/replacement-track',
    ]);
  });
}
