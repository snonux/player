import 'dart:async';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:dio/dio.dart';
import 'package:player_android/api/dio_player_api_client.dart';
import 'package:player_android/models/media.dart';
import 'package:player_android/models/offline_media.dart';
import 'package:player_android/services/offline_download_repository.dart';
import 'package:player_android/services/offline_downloads.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/services/playback_session_coordinator.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';

Media media(
        {int id = 11,
        int size = 4,
        String type = 'audio',
        bool transcoded = false}) =>
    Media.fromJson({
      'id': id,
      'file_name': 'tone.wav',
      'type': type,
      'file_size_bytes': size,
      'transcoded': transcoded
    });

void main() {
  late Directory directory;
  late OfflineDownloadRepository repository;
  late OfflineDownloads downloads;
  late PlaybackSessionCoordinator playback;
  late HttpServer server;
  late String origin;
  late List<String> requests;
  late Future<void> Function(HttpRequest) respond;

  setUpAll(() {
    sqfliteFfiInit();
    databaseFactory = databaseFactoryFfi;
  });

  setUp(() async {
    directory = await Directory.systemTemp.createTemp('player-offline-');
    repository = OfflineDownloadRepository(directory: directory);
    playback = PlaybackSessionCoordinator();
    downloads = OfflineDownloads(repository, playback);
    server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    origin = 'http://${server.address.address}:${server.port}';
    requests = [];
    respond = (request) async {
      request.response.headers.contentType = ContentType('audio', 'wav');
      request.response.contentLength = 4;
      request.response.add([1, 2, 3, 4]);
      await request.response.close();
    };
    server.listen((request) async {
      requests.add('${request.method} ${request.uri.path}');
      expect(request.headers.value('Authorization'), 'Bearer test-secret');
      try {
        await respond(request);
      } on SocketException {
        // Deliberate client cancellation may close a response mid-write.
      } on HttpException {
        // Deliberate client cancellation may close a response mid-write.
      }
    });
  });

  tearDown(() async {
    downloads.dispose();
    await server.close(force: true);
    await repository.close();
    await directory.delete(recursive: true);
  });

  Future<OfflineMedia> download(
          {Media? item, int user = 7, bool Function()? authorized, Uri? uri}) =>
      downloads.download(
        media: item ?? media(),
        origin: origin,
        userId: user,
        uri: uri ?? Uri.parse('$origin/api/v1/media/11/stream'),
        headers: {'Authorization': 'Bearer test-secret'},
        authorized: authorized ?? () => true,
      );

  test(
      'download retains tagged title, complete music and source context after reopen',
      () async {
    final item = Media.fromJson({
      'id': 11,
      'type': 'audio',
      'file_name': 'chapter.m4b',
      'rel_path': 'Books/chapter.m4b',
      'file_size_bytes': 4,
      'music': {
        'title': '東京 Music Title',
        'artist': 'A; B',
        'album': 'Album',
        'disc': 2,
        'genres': ['Jazz', 'Blues']
      }
    });
    final saved = await download(item: item);
    await repository.close();
    final reopened = await repository.get(saved.key);
    expect(reopened!.title, '東京 Music Title');
    expect(reopened.music!.details['Genres'], 'Jazz, Blues');
    expect(reopened.music!.details['Disc'], '2');
    expect(reopened.sourceName, 'Books/chapter.m4b');
    expect((await repository.playbackRequest(saved.key)).visualContext,
        contains('chapter.m4b'));
  });

  test(
      'actual HTTP file persists, plays offline after reopen, deletes only local copy',
      () async {
    // A real WAV file, not response mocks. Closing the server makes every
    // subsequent playback/progress/removal operation prove it is local.
    final bytes = Uint8List(16044);
    final data = ByteData.sublistView(bytes);
    bytes.setRange(0, 4, 'RIFF'.codeUnits);
    data.setUint32(4, bytes.length - 8, Endian.little);
    bytes.setRange(8, 16, 'WAVEfmt '.codeUnits);
    data.setUint32(16, 16, Endian.little);
    data.setUint16(20, 1, Endian.little);
    data.setUint16(22, 1, Endian.little);
    data.setUint32(24, 8000, Endian.little);
    data.setUint32(28, 16000, Endian.little);
    data.setUint16(32, 2, Endian.little);
    data.setUint16(34, 16, Endian.little);
    bytes.setRange(36, 40, 'data'.codeUnits);
    data.setUint32(40, bytes.length - 44, Endian.little);
    final remote = File('${directory.path}/server-original.wav');
    await remote.writeAsBytes(bytes);
    respond = (request) async {
      request.response.headers.contentType = ContentType('audio', 'wav');
      request.response.contentLength = bytes.length;
      await request.response.addStream(remote.openRead());
      await request.response.close();
    };
    final item = await download(item: media(size: bytes.length));
    expect(await repository.file(item.key).readAsBytes(), bytes);
    await repository.close();
    await server.close(force: true);
    final request = await repository.playbackRequest(item.key);
    expect(request.kind, PlaybackSourceKind.local);
    expect(request.sourceUri.scheme, 'file');
    expect(request.identity, 'offline:${item.key}');
    expect(await File.fromUri(request.sourceUri).readAsBytes(), bytes);
    await request.savePosition!(0.25);
    await repository.close();
    expect(await request.readPosition!(), 0.25);
    await request.markFinished!();
    await repository.close();
    expect(await request.readPosition!(), 0);
    var stopped = false;
    await playback.claim(
        kind: request.kind,
        identity: request.identity,
        sourceUri: request.sourceUri.toString(),
        stop: () async => stopped = true);
    await downloads.remove(item.key);
    expect(stopped, isTrue);
    expect(await repository.list(), isEmpty);
    expect(await File.fromUri(request.sourceUri).exists(), isFalse);
    await request.savePosition!(0.5); // A late callback cannot restore a row.
    expect(await repository.get(item.key), isNull);
    expect(await remote.readAsBytes(), bytes);
    expect(requests, ['GET /api/v1/media/11/stream']);
  });

  test('identical IDs remain separate across origins and accounts', () async {
    final first = await download();
    final second = await download(user: 8);
    expect(first.key, isNot(second.key));
    expect(first.key, isNot(offlineMediaKey('https://another.example', 7, 11)));
    final otherPort = server.port == 65535 ? server.port - 1 : server.port + 1;
    final otherOrigin = Uri.parse(origin).replace(port: otherPort).origin;
    expect(first.key, isNot(offlineMediaKey(otherOrigin, 7, 11)));
    await downloads.remove(first.key);
    expect(await repository.file(second.key).readAsBytes(), [1, 2, 3, 4]);
    expect(await repository.list(), hasLength(1));
  });

  test('duplicate completed download reuses existing file without network',
      () async {
    final first = await download();
    expect((await download()).key, first.key);
    expect(requests, hasLength(1));
  });

  test(
      'original download survives a differently-sized remuxed streaming endpoint',
      () async {
    // A TS source can be indexed as .mp4 without requiring transcoding. Its
    // streaming endpoint may return a remux with a different byte count.
    final original = Uint8List(376)
      ..[0] = 0x47
      ..[188] = 0x47;
    respond = (request) async {
      request.response.headers.contentType = ContentType('video', 'mp4');
      final bytes = request.uri.path.endsWith('/download')
          ? original
          : List.filled(400, 1);
      request.response.contentLength = bytes.length;
      request.response.add(bytes);
      await request.response.close();
    };
    final client = DioPlayerApiClient(dio: Dio(BaseOptions(baseUrl: origin)));
    final item = media(type: 'video', size: original.length);
    final saved = await download(
        item: item, uri: Uri.parse(client.offlineDownloadUrl(item)));
    expect(await repository.file(saved.key).readAsBytes(), original);
    expect(saved.sizeBytes, original.length);
    // Retain the integrity guard: using transformed bytes must still fail.
    await expectLater(
        download(
            item: media(id: 12, type: 'video', size: original.length),
            uri: Uri.parse('$origin/api/v1/media/12/stream')),
        throwsA(isA<HttpException>()));
    expect(await repository.list(), hasLength(1));
    expect(requests,
        ['GET /api/v1/media/11/download', 'GET /api/v1/media/12/stream']);
  });

  for (final status in [301, 401, 403, 404, 500]) {
    test('HTTP $status never publishes a copy or follows redirects', () async {
      respond = (request) async {
        request.response.statusCode = status;
        request.response.headers
            .set('Location', 'http://127.0.0.1:1/credential-leak');
        await request.response.close();
      };
      await expectLater(download(), throwsA(isA<HttpException>()));
      expect(await repository.list(), isEmpty);
      expect(downloads.active, isNull);
      expect(downloads.error, contains('HTTP $status'));
      expect(requests, hasLength(1));
      expect(
          await directory
              .list()
              .where((entry) => entry.path.endsWith('.part'))
              .isEmpty,
          isTrue);
    });
  }

  test('mismatched origin or unauthenticated account makes no request',
      () async {
    await expectLater(download(uri: Uri.parse('https://another.example/file')),
        throwsStateError);
    await expectLater(download(authorized: () => false), throwsStateError);
    expect(requests, isEmpty);
    expect(downloads.active, isNull);
  });

  test('incorrect size and non-media error bodies cannot become ready',
      () async {
    await expectLater(
        download(item: media(size: 5)), throwsA(isA<HttpException>()));
    respond = (request) async {
      request.response.headers.contentType = ContentType.json;
      request.response.write('{}');
      await request.response.close();
    };
    await expectLater(
        download(item: media(size: 0)), throwsA(isA<HttpException>()));
    expect(await repository.list(), isEmpty);
  });

  test('unknown content length streams without an arbitrary movie size cap',
      () async {
    respond = (request) async {
      request.response.headers.contentType = ContentType('video', 'mp4');
      request.response.add(List.filled(5000, 1));
      await request.response.flush();
      request.response.add(List.filled(5000, 2));
      await request.response.close();
    };
    final item =
        await download(item: media(type: 'video', size: 0, transcoded: true));
    expect(item.sizeBytes, 10000);
    expect(await repository.file(item.key).length(), 10000);
  });

  test('empty response is rejected and partial file removed', () async {
    respond = (request) async => request.response.close();
    await expectLater(
        download(item: media(size: 0)), throwsA(isA<HttpException>()));
    expect(await repository.list(), isEmpty);
    final key = offlineMediaKey(origin, 7, 11);
    expect(await repository.file(key, partial: true).exists(), isFalse);
    expect(await repository.file(key).exists(), isFalse);
  });

  test('truncated HTTP body never publishes partial bytes', () async {
    respond = (request) async {
      final socket = await request.response.detachSocket(writeHeaders: false);
      socket.add(
          'HTTP/1.1 200 OK\r\nContent-Type: audio/wav\r\nContent-Length: 8\r\nConnection: close\r\n\r\n'
              .codeUnits);
      socket.add([1, 2, 3, 4]);
      await socket.flush();
      await socket.close();
    };
    await expectLater(
        download(item: media(size: 8)), throwsA(isA<HttpException>()));
    expect(await repository.list(), isEmpty);
    final key = offlineMediaKey(origin, 7, 11);
    expect(await repository.file(key, partial: true).exists(), isFalse);
    expect(await repository.file(key).exists(), isFalse);
    expect(downloads.active, isNull);
  });

  test('chunked body exceeding metadata size is discarded', () async {
    respond = (request) async {
      request.response.headers.contentType = ContentType('audio', 'wav');
      request.response.add(List.filled(8, 1));
      await request.response.close();
    };
    await expectLater(download(), throwsA(isA<HttpException>()));
    expect(await repository.list(), isEmpty);
    expect(downloads.active, isNull);
  });

  test('cancel and account change remove partial bytes and allow retry',
      () async {
    for (final accountChange in [false, true]) {
      var authorized = true;
      final started = Completer<void>();
      final finish = Completer<void>();
      respond = (request) async {
        request.response.headers.contentType = ContentType('audio', 'wav');
        request.response.add([1, 2]);
        await request.response.flush();
        started.complete();
        await finish.future;
        request.response.add([3, 4]);
        await request.response.close();
      };
      final pending = download(authorized: () => authorized);
      final assertion =
          expectLater(pending, throwsA(isA<OfflineDownloadCancelled>()));
      await started.future;
      await expectLater(download(), throwsStateError);
      if (accountChange) {
        authorized = false;
      } else {
        downloads.cancel();
      }
      finish.complete();
      await assertion;
      expect(await repository.list(), isEmpty);
      final key = offlineMediaKey(origin, 7, 11);
      expect(await repository.file(key, partial: true).exists(), isFalse);
      expect(downloads.active, isNull);
      expect(downloads.error, isNull);
    }
  });

  test(
      'startup removes interrupted and orphan files, retains complete downloads',
      () async {
    final item = await download();
    final orphanKey = offlineMediaKey(origin, 7, 12);
    await repository.file(orphanKey).writeAsBytes([5]);
    await repository.file(orphanKey, partial: true).writeAsBytes([6]);
    final unrelated = File('${directory.path}/other.txt');
    await unrelated.writeAsString('keep');
    await repository.close();
    expect(await repository.list(), hasLength(1));
    expect(await repository.file(item.key).exists(), isTrue);
    expect(await repository.file(orphanKey).exists(), isFalse);
    expect(await repository.file(orphanKey, partial: true).exists(), isFalse);
    expect(await unrelated.readAsString(), 'keep');
  });

  test('missing or altered copy fails locally and remains removable', () async {
    final item = await download();
    await repository.file(item.key).writeAsBytes([1]);
    await expectLater(repository.playbackRequest(item.key), throwsStateError);
    await repository.file(item.key).delete();
    await expectLater(repository.playbackRequest(item.key), throwsStateError);
    await downloads.remove(item.key);
    expect(await repository.list(), isEmpty);
    expect(requests, hasLength(1));
  });

  test('invalid file keys cannot escape private storage', () {
    expect(() => repository.file('../server-media'), throwsArgumentError);
    expect(() => repository.file('/tmp/anything'), throwsArgumentError);
  });

  test('filesystem write failure publishes nothing and releases the manager',
      () async {
    await repository.database;
    final key = offlineMediaKey(origin, 7, 11);
    final obstruction = Directory(repository.file(key, partial: true).path);
    await obstruction.create();
    await expectLater(download(), throwsA(isA<FileSystemException>()));
    expect(downloads.active, isNull);
    expect(downloads.error, contains('storage'));
    expect(await repository.list(), isEmpty);
    expect(await repository.file(key).exists(), isFalse);
    await obstruction.delete();
    expect((await download()).sizeBytes, 4);
  });

  test('preparation cancellation never starts the media GET', () async {
    await expectLater(
        downloads.download(
          media: media(transcoded: true),
          origin: origin,
          userId: 7,
          uri: Uri.parse('$origin/api/v1/media/11/compat'),
          headers: {'Authorization': 'Bearer test-secret'},
          authorized: () => true,
          prepare: (job) async {
            job.cancel();
          },
        ),
        throwsA(isA<OfflineDownloadCancelled>()));
    expect(requests, isEmpty);
    expect(await repository.list(), isEmpty);
    expect(downloads.active, isNull);
  });

  test('progress rejects negative and non-finite positions', () async {
    final item = await download();
    for (final position in [-1.0, double.nan, double.infinity]) {
      await expectLater(
          repository.saveProgress(item.key, position), throwsArgumentError);
    }
    expect((await repository.get(item.key))!.positionSeconds, 0);
  });
}
