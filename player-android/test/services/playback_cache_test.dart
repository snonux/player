import 'package:player_android/models/music_metadata.dart';
import 'dart:async';
import 'dart:io';
import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:player_android/services/playback_cache.dart';
import 'package:player_android/services/playback_cache_access.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  HttpOverrides.global = null;
  test(
      'complete copies isolate account, quality; cancellation and clear protect playback',
      () async {
    SharedPreferences.setMockInitialValues({});
    final directory =
        await Directory.systemTemp.createTemp('player-cache-test-');
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    server.listen((request) async {
      request.response.headers.contentType = ContentType('audio', 'mpeg');
      request.response.headers.contentLength = 3;
      request.response.headers.set('etag', 'one');
      request.response.add([1, 2, 3]);
      await request.response.close();
    });
    final cache = PlaybackCache(directory: directory);
    try {
      final key = cache.key('https://canonical.test', 1, 7, 'auto');
      await cache.download(
          key: key,
          uri: Uri.parse('http://127.0.0.1:${server.port}/stream'),
          headers: {'Authorization': 'Bearer temporary'},
          title: 'Seven',
          mediaId: 7,
          sourceName: 'seven.mp3',
          music: MusicMetadata.fromJson({
            'artist': 'Saved Artist',
            'album': 'Saved Album',
            'title': 'Seven'
          }),
          origin: 'https://canonical.test',
          userId: 1,
          authorized: () => true);
      expect(cache.bytes, 3);
      final local = await cache.offlineLibrary();
      expect(local.single.key, 'cache:$key');
      expect(local.single.mediaId, 7);
      expect(local.single.music?.artist, 'Saved Artist');
      final offline =
          await cache.offlineRequest(key, 'https://canonical.test', 1);
      expect(offline.sourceUri.scheme, 'file');
      expect(offline.artist, 'Saved Artist');
      await expectLater(cache.offlineRequest(key, 'https://canonical.test', 2),
          throwsStateError);
      await expectLater(
          cache.offlineRequest(key, 'https://other.test', 1), throwsStateError);
      expect(
          await cache.lookup(cache.key('https://canonical.test', 2, 7, 'auto')),
          isNull);
      expect(
          await cache
              .lookup(cache.key('https://canonical.test', 1, 7, 'mp3-128')),
          isNull);
      final source = await cache.lookup(key);
      expect(await File.fromUri(source!).readAsBytes(), [1, 2, 3]);
      await cache.clear();
      expect(await cache.lookup(key), isNotNull,
          reason: 'playing file retained');
      cache.playingUri(null);
      await cache.clear();
      expect(await cache.lookup(key), isNull);
      expect(cache.bytes, 0);
      expect(await cache.offlineLibrary(), isEmpty);
      await expectLater(cache.offlineRequest(key, 'https://canonical.test', 1),
          throwsStateError);
      final forbidden = cache.key('https://canonical.test', 1, 8, 'auto');
      await cache.download(
          key: forbidden,
          uri: Uri.parse('http://127.0.0.1:${server.port}/stream'),
          headers: {},
          title: 'No access',
          origin: 'https://canonical.test',
          userId: 1,
          authorized: () => false);
      expect(await cache.lookup(forbidden), isNull);
    } finally {
      cache.dispose();
      await server.close(force: true);
      await directory.delete(recursive: true);
    }
  });
  test('HTTP denial is never offline permission, ETag changes invalidate',
      () async {
    var status = 200, etag = 'one';
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    server.listen((request) async {
      expect(request.method, 'HEAD');
      expect(request.headers.value('authorization'), 'Bearer test');
      request.response.statusCode = status;
      request.response.headers.set('etag', etag);
      await request.response.close();
    });
    final uri = Uri.parse('http://127.0.0.1:${server.port}/audio');
    try {
      expect(
          await validateCachedAudio(
              uri, {'Authorization': 'Bearer test'}, 'one'),
          isTrue);
      etag = 'two';
      expect(
          await validateCachedAudio(
              uri, {'Authorization': 'Bearer test'}, 'one'),
          isFalse);
      for (final code in [401, 403, 404, 410, 302, 500]) {
        status = code;
        try {
          await validateCachedAudio(
              uri, {'Authorization': 'Bearer test'}, 'one');
          fail('HTTP $code accepted');
        } catch (error) {
          expect(cacheTransportFailure(error), isFalse);
        }
      }
    } finally {
      await server.close(force: true);
    }
    expect(cacheTransportFailure(const SocketException('offline')), isTrue);
    expect(cacheTransportFailure(TimeoutException('offline')), isTrue);
    expect(
        cacheTransportFailure(DioException(
            requestOptions: RequestOptions(path: '/'),
            type: DioExceptionType.connectionError)),
        isTrue);
    expect(
        cacheTransportFailure(DioException(
            requestOptions: RequestOptions(path: '/'),
            response: Response(
                requestOptions: RequestOptions(path: '/'), statusCode: 403))),
        isFalse);
    expect(
        cacheTransportFailure(DioException(
            requestOptions: RequestOptions(path: '/'),
            type: DioExceptionType.cancel)),
        isFalse);
  });
}
