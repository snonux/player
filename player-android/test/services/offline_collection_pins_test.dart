import 'dart:io';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/models/media.dart';
import 'package:player_android/models/offline_media.dart';
import 'package:player_android/services/music_collections.dart';
import 'package:player_android/services/offline_collection_pins.dart';
import 'package:player_android/services/offline_download_repository.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';

void main() {
  setUpAll(() {
    sqfliteFfiInit();
    databaseFactory = databaseFactoryFfi;
  });
  test(
      'pins survive reopen, share durable downloads, isolate account and derive readiness from bytes',
      () async {
    final directory = await Directory.systemTemp.createTemp('player-pins-');
    var repository = OfflineDownloadRepository(directory: directory);
    addTearDown(() async {
      await repository.close();
      await directory.delete(recursive: true);
    });
    var pins = OfflineCollectionPins(repository);
    const origin = 'http://player.test:8080';
    const album = MusicCollection('album', 'a');
    const playlist = MusicCollection('playlist', 'p');
    final song =
        Media.fromJson({'id': 7, 'type': 'audio', 'file_name': 'Seven.mp3'});
    final item = OfflineMedia(
        key: offlineMediaKey(origin, 1, 7),
        origin: origin,
        userId: 1,
        mediaId: 7,
        title: 'Seven',
        type: 'audio',
        sizeBytes: 3,
        addedAt: DateTime.now());
    await repository.database;
    await repository.file(item.key).writeAsBytes([1, 2, 3]);
    await repository.add(item);
    await pins.remember(origin, 1, album, 'Album', [song, song], () => true);
    await pins.remember(origin, 1, playlist, 'Playlist', [song], () => true);
    expect((await pins.status(origin, 1, album))!.mediaIds, [7]);
    await repository.close();
    repository = OfflineDownloadRepository(directory: directory);
    pins = OfflineCollectionPins(repository);
    expect((await pins.status(origin, 1, album))!.complete, isTrue);
    final playback = (await pins.playback(origin, 1, album)).single;
    expect(playback.sourceUri, repository.file(item.key).uri);
    expect(playback.serverOrigin, origin);
    expect(playback.userId, 1);
    expect(await File.fromUri(playback.sourceUri).readAsBytes(), [1, 2, 3]);
    expect(await pins.status(origin, 2, album), isNull);
    expect(await pins.status('https://player.test:8080', 1, album), isNull);
    await pins.unpin(origin, 1, album);
    expect((await pins.status(origin, 1, album))!.pinned, isFalse);
    expect((await pins.status(origin, 1, playlist))!.complete, isTrue);
    expect(await repository.file(item.key).exists(), isTrue);
    await repository.file(item.key).writeAsBytes([1]);
    expect((await pins.status(origin, 1, playlist))!.complete, isFalse);
    await expectLater(pins.playback(origin, 1, playlist), throwsStateError);
    await expectLater(
        pins.remember(origin, 1, album, 'Album', [song], () => false),
        throwsStateError);
  });
}
