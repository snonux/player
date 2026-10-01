import 'dart:io';

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';

import 'package:player_android/services/local_document_picker.dart';
import 'package:player_android/services/local_library_manager.dart';
import 'package:player_android/services/local_library_repository.dart';

class _FakePicker implements LocalDocumentPicker {
  final results = <PickedLocalDocument?>[];
  final releasedUris = <String>[];
  final unreadableUris = <String>{};
  Object? pickError;
  Object? releaseError;

  @override
  Future<PickedLocalDocument?> pick(LocalDocumentKind kind) async {
    final error = pickError;
    if (error != null) throw error;
    if (results.isEmpty) throw StateError('No picker result configured');
    return results.removeAt(0);
  }

  @override
  Future<bool> isReadable(String uri) async => !unreadableUris.contains(uri);

  @override
  Future<void> releasePersistedReadGrant(String uri) async {
    releasedUris.add(uri);
    final error = releaseError;
    if (error != null) throw error;
  }
}

class _Usage implements LocalDocumentUsage {
  final activeUris = <String>{};

  @override
  bool isInUse(String uri) => activeUris.contains(uri);
}

PickedLocalDocument _picked(
  String uri, {
  LocalDocumentKind kind = LocalDocumentKind.audio,
  bool newlyGranted = true,
  String? name = 'Picked file',
  String? mimeType = 'audio/mpeg',
  int? sizeBytes = 1024,
}) =>
    PickedLocalDocument(
      uri: uri,
      kind: kind,
      newPersistedReadGrant: newlyGranted,
      name: name,
      mimeType: mimeType,
      sizeBytes: sizeBytes,
    );

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUpAll(() {
    sqfliteFfiInit();
    databaseFactory = databaseFactoryFfi;
  });

  test('picker cancellation leaves the library unchanged', () async {
    final database = LocalLibraryDatabase(databasePath: inMemoryDatabasePath);
    final media = SqliteLocalMediaRepository(database);
    final picker = _FakePicker()..results.add(null);
    final manager = LocalLibraryManager(
      picker: picker,
      media: media,
      cleanup: SqliteLocalGrantCleanupRepository(database),
      usage: _Usage(),
    );

    expect(await manager.addDocument(LocalDocumentKind.audio), isNull);
    expect(await media.list(), isEmpty);
    expect(picker.releasedUris, isEmpty);
    await database.close();
  });

  test('missing document metadata uses safe defaults and opaque URI', () async {
    final database = LocalLibraryDatabase(databasePath: inMemoryDatabasePath);
    final media = SqliteLocalMediaRepository(database);
    final picker = _FakePicker()
      ..results.add(_picked(
        'content://provider/no-metadata',
        kind: LocalDocumentKind.video,
        name: null,
        mimeType: null,
        sizeBytes: null,
      ));
    final manager = LocalLibraryManager(
      picker: picker,
      media: media,
      cleanup: SqliteLocalGrantCleanupRepository(database),
      usage: _Usage(),
    );

    final added = await manager.addDocument(LocalDocumentKind.video);
    expect(added!.uri, 'content://provider/no-metadata');
    expect(added.title, 'Untitled video file');
    expect(added.mimeType, 'video/*');
    expect(added.sizeBytes, isNull);
    expect(added.durationMs, isNull);
    expect(picker.releasedUris, isEmpty);
    await database.close();
  });

  test('duplicate selection keeps an existing grant and record', () async {
    final database = LocalLibraryDatabase(databasePath: inMemoryDatabasePath);
    final media = SqliteLocalMediaRepository(database);
    final picker = _FakePicker()
      ..results.addAll([
        _picked('content://provider/same', newlyGranted: false),
        // Re-granting access to an existing library record must keep the grant.
        _picked('content://provider/same', newlyGranted: true),
      ]);
    final manager = LocalLibraryManager(
      picker: picker,
      media: media,
      cleanup: SqliteLocalGrantCleanupRepository(database),
      usage: _Usage(),
    );

    final first = await manager.addDocument(LocalDocumentKind.audio);
    final second = await manager.addDocument(LocalDocumentKind.audio);
    expect(second!.id, first!.id);
    expect(second.ownsPersistedReadGrant, isTrue);
    expect(await media.list(), hasLength(1));
    expect(picker.releasedUris, isEmpty);
    final removed = await manager.remove(second.id);
    expect(removed.grantReleased, isTrue);
    expect(picker.releasedUris, ['content://provider/same']);
    await database.close();
  });

  test('removal does not release a grant the local import did not acquire',
      () async {
    final database = LocalLibraryDatabase(databasePath: inMemoryDatabasePath);
    final media = SqliteLocalMediaRepository(database);
    final added = await media.importDocument(
      uri: 'content://provider/preexisting-grant',
      title: 'Preexisting',
      mimeType: 'audio/mpeg',
      ownsPersistedReadGrant: false,
    );
    final picker = _FakePicker();
    final manager = LocalLibraryManager(
      picker: picker,
      media: media,
      cleanup: SqliteLocalGrantCleanupRepository(database),
      usage: _Usage(),
    );

    final removed = await manager.remove(added.id);
    expect(removed.removed, isTrue);
    expect(removed.grantReleased, isFalse);
    expect(removed.releaseFailed, isFalse);
    expect(picker.releasedUris, isEmpty);
    await database.close();
  });

  test('database failure releases only the newly acquired unreferenced grant',
      () async {
    final database = LocalLibraryDatabase(databasePath: inMemoryDatabasePath);
    final media = SqliteLocalMediaRepository(database);
    final db = await database.database;
    await db.execute('''
      CREATE TRIGGER reject_document BEFORE INSERT ON local_media
      BEGIN SELECT RAISE(ABORT, 'injected failure'); END
    ''');
    final picker = _FakePicker()
      ..results.add(_picked('content://provider/failed-import'));
    final manager = LocalLibraryManager(
      picker: picker,
      media: media,
      cleanup: SqliteLocalGrantCleanupRepository(database),
      usage: _Usage(),
    );

    await expectLater(
      manager.addDocument(LocalDocumentKind.audio),
      throwsA(isA<DatabaseException>()),
    );
    expect(picker.releasedUris, ['content://provider/failed-import']);
    expect(await media.list(), isEmpty);
    await database.close();
  });

  test('unpersistable selection reports an error and changes nothing',
      () async {
    final database = LocalLibraryDatabase(databasePath: inMemoryDatabasePath);
    final media = SqliteLocalMediaRepository(database);
    final picker = _FakePicker()
      ..pickError = const LocalDocumentPickerException(
        'Persistent access unavailable',
        code: 'grant_failed',
      );
    final manager = LocalLibraryManager(
      picker: picker,
      media: media,
      cleanup: SqliteLocalGrantCleanupRepository(database),
      usage: _Usage(),
    );

    await expectLater(
      manager.addDocument(LocalDocumentKind.audio),
      throwsA(isA<LocalDocumentPickerException>()),
    );
    expect(await media.list(), isEmpty);
    expect(picker.releasedUris, isEmpty);
    await database.close();
  });

  test('relink cancellation releases its new grant; start-over resets progress',
      () async {
    final database = LocalLibraryDatabase(databasePath: inMemoryDatabasePath);
    final media = SqliteLocalMediaRepository(database);
    final progress = SqliteLocalProgressRepository(database);
    final current = await media.importDocument(
      uri: 'content://provider/original',
      title: 'Original',
      mimeType: 'audio/mpeg',
      ownsPersistedReadGrant: true,
    );
    await progress.savePosition(current.id, 27);
    final picker = _FakePicker()
      ..results.addAll([
        _picked('content://provider/cancelled-replacement'),
        _picked('content://provider/replacement'),
      ]);
    final manager = LocalLibraryManager(
      picker: picker,
      media: media,
      cleanup: SqliteLocalGrantCleanupRepository(database),
      usage: _Usage(),
    );

    expect(
      await manager.relink(
        id: current.id,
        confirmRetainProgress: (_, __) async => null,
      ),
      isNull,
    );
    expect(picker.releasedUris, ['content://provider/cancelled-replacement']);
    expect((await progress.get(current.id))!.positionSeconds, 27);

    final relinked = await manager.relink(
      id: current.id,
      confirmRetainProgress: (_, __) async => false,
    );
    expect(relinked!.media.id, current.id);
    expect(relinked.media.uri, 'content://provider/replacement');
    expect(relinked.oldGrantReleaseFailed, isFalse);
    expect(await progress.get(current.id), isNull);
    expect(picker.releasedUris,
        ['content://provider/cancelled-replacement', current.uri]);
    await database.close();
  });

  test('unavailable revoked URI can be relinked without losing its record',
      () async {
    final database = LocalLibraryDatabase(databasePath: inMemoryDatabasePath);
    final media = SqliteLocalMediaRepository(database);
    final progress = SqliteLocalProgressRepository(database);
    const revokedUri = 'content://provider/revoked-file';
    final current = await media.importDocument(
      uri: revokedUri,
      title: 'Missing file',
      mimeType: 'audio/mpeg',
      ownsPersistedReadGrant: true,
    );
    await progress.savePosition(current.id, 18);
    final picker = _FakePicker()
      ..unreadableUris.add(revokedUri)
      ..results.add(_picked('content://provider/replacement'));
    final manager = LocalLibraryManager(
      picker: picker,
      media: media,
      cleanup: SqliteLocalGrantCleanupRepository(database),
      usage: _Usage(),
    );

    final result = await manager.relink(
      id: current.id,
      confirmRetainProgress: (_, __) async => true,
    );

    expect(result!.previousFileWasUnavailable, isTrue);
    expect(result.media.id, current.id);
    expect(result.media.uri, 'content://provider/replacement');
    expect((await progress.get(current.id))!.positionSeconds, 18);
    await database.close();
  });

  test('removal never deletes the source and retains grants during playback',
      () async {
    final directory = await Directory.systemTemp.createTemp('local-source-');
    final source = File('${directory.path}/source.mp3')..writeAsStringSync('x');
    final database = LocalLibraryDatabase(databasePath: inMemoryDatabasePath);
    final media = SqliteLocalMediaRepository(database);
    final progress = SqliteLocalProgressRepository(database);
    const uri = 'content://provider/source';
    final added = await media.importDocument(
      uri: uri,
      title: 'Source',
      mimeType: 'audio/mpeg',
      ownsPersistedReadGrant: true,
    );
    await progress.savePosition(added.id, 5);
    final picker = _FakePicker();
    final usage = _Usage()..activeUris.add(uri);
    final manager = LocalLibraryManager(
      picker: picker,
      media: media,
      cleanup: SqliteLocalGrantCleanupRepository(database),
      usage: usage,
    );

    final result = await manager.remove(added.id);
    expect(result.removed, isTrue);
    expect(result.grantReleased, isFalse);
    expect(await media.getById(added.id), isNull);
    expect(await progress.get(added.id), isNull);
    expect(await source.exists(), isTrue);
    expect(picker.releasedUris, isEmpty);

    usage.activeUris.remove(uri);
    expect(await manager.releaseIfUnused(uri), isTrue);
    expect(picker.releasedUris, [uri]);

    const idleUri = 'content://provider/idle-source';
    final idleItem = await media.importDocument(
      uri: idleUri,
      title: 'Idle source',
      mimeType: 'audio/mpeg',
      ownsPersistedReadGrant: true,
    );
    final idleRemoval = await manager.remove(idleItem.id);
    expect(idleRemoval.grantReleased, isTrue);
    expect(picker.releasedUris, [uri, idleUri]);

    await database.close();
    await directory.delete(recursive: true);
  });

  test('failed releases survive relink, removal, and manager restart',
      () async {
    final directory = await Directory.systemTemp.createTemp('grant-retry-');
    final path = '${directory.path}/library.db';
    try {
      final database = LocalLibraryDatabase(databasePath: path);
      final media = SqliteLocalMediaRepository(database);
      final cleanup = SqliteLocalGrantCleanupRepository(database);
      final original = await media.importDocument(
        uri: 'content://provider/relink-old',
        title: 'Original',
        mimeType: 'audio/mpeg',
        ownsPersistedReadGrant: true,
      );
      final picker = _FakePicker()
        ..results.add(_picked('content://provider/relink-new'))
        ..releaseError = StateError('Android release failed');
      final manager = LocalLibraryManager(
        picker: picker,
        media: media,
        cleanup: cleanup,
        usage: _Usage(),
      );

      final relinked = await manager.relink(
        id: original.id,
        confirmRetainProgress: (_, __) async => true,
      );
      expect(relinked!.oldGrantReleaseFailed, isTrue);
      expect(await cleanup.pendingUris(), ['content://provider/relink-old']);

      final removalTarget = await media.importDocument(
        uri: 'content://provider/removal',
        title: 'Removal target',
        mimeType: 'audio/mpeg',
        ownsPersistedReadGrant: true,
      );
      final removed = await manager.remove(removalTarget.id);
      expect(removed.releaseFailed, isTrue);
      expect(
          await cleanup.pendingUris(),
          unorderedEquals([
            'content://provider/relink-old',
            'content://provider/removal',
          ]));
      await database.close();

      final reopened = LocalLibraryDatabase(databasePath: path);
      final reopenedMedia = SqliteLocalMediaRepository(reopened);
      final reopenedCleanup = SqliteLocalGrantCleanupRepository(reopened);
      final retryPicker = _FakePicker();
      final retryManager = LocalLibraryManager(
        picker: retryPicker,
        media: reopenedMedia,
        cleanup: reopenedCleanup,
        usage: _Usage(),
      );
      await retryManager.retryPendingGrantReleases();
      expect(
          retryPicker.releasedUris,
          unorderedEquals([
            'content://provider/relink-old',
            'content://provider/removal',
          ]));
      expect(await reopenedCleanup.pendingUris(), isEmpty);
      await reopened.close();
    } finally {
      await directory.delete(recursive: true);
    }
  });

  test('method channel handles cancel, metadata, size, and grant errors',
      () async {
    const channel = MethodChannel(
      'zone.foo.player_android/document_compatibility',
    );
    final calls = <MethodCall>[];
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
      calls.add(call);
      if (call.method == 'pick') {
        return {
          'uri': 'content://provider/track',
          'name': 'Track.mp3',
          'mimeType': 'audio/mpeg',
          'sizeBytes': 123,
          'newPersistedReadGrant': true,
        };
      }
      if (call.method == 'checkReadable') return false;
      return null;
    });
    addTearDown(() => TestDefaultBinaryMessengerBinding
        .instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null));
    final picker = MethodChannelLocalDocumentPicker(channel: channel);

    final selected = await picker.pick(LocalDocumentKind.audio);
    expect(selected!.uri, 'content://provider/track');
    expect(selected.sizeBytes, 123);
    expect(selected.newPersistedReadGrant, isTrue);
    expect(calls.first.arguments, {'kind': 'audio'});
    expect(await picker.isReadable('content://provider/missing'), isFalse);
    await picker.releasePersistedReadGrant(selected.uri);
    expect(
        calls.map((call) => call.method), ['pick', 'checkReadable', 'release']);

    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async => null);
    expect(await picker.pick(LocalDocumentKind.video), isNull);
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(
            channel,
            (call) async => throw PlatformException(
                  code: 'grant_failed',
                  message: 'No persistable access',
                ));
    await expectLater(
      picker.pick(LocalDocumentKind.audio),
      throwsA(isA<LocalDocumentPickerException>()),
    );
  });
}
