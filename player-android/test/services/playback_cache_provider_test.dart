import 'package:player_android/services/network_policy.dart';
import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'package:cookie_jar/cookie_jar.dart';
import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:just_audio/just_audio.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/models/media.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/audio_handler_provider.dart';
import 'package:player_android/providers/playback_cache_provider.dart';
import 'package:player_android/providers/server_endpoint_provider.dart';
import 'package:player_android/services/audio_handler.dart';
import 'package:player_android/services/music_queue.dart';
import 'package:player_android/services/playback_cache.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/services/server_endpoint_router.dart';

class _Token implements TokenStorage {
  @override
  Future<String?> readToken() async => 'test';
  @override
  Future<void> writeToken(String token) async {}
  @override
  Future<void> deleteToken() async {}
}

class _Client extends PlayerApiClient {
  _Client(String origin) : super(dio: Dio(BaseOptions(baseUrl: origin)));
  Object? failure;
  @override
  Future<Media> getMedia(int id, {CancelToken? cancelToken}) async {
    if (failure != null) throw failure!;
    return Media.fromJson({'id': id, 'type': 'audio', 'file_name': '$id.mp3'});
  }
}

class _Player extends AudioPlayer {
  @override
  Stream<PlaybackEvent> get playbackEventStream => const Stream.empty();
  @override
  Stream<bool> get playingStream => const Stream.empty();
  @override
  Stream<ProcessingState> get processingStateStream => const Stream.empty();
  @override
  Stream<SequenceState?> get sequenceStateStream => const Stream.empty();
  @override
  Future<void> setVolume(double volume) async {}
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  HttpOverrides.global = null;
  for (final scenario in [
    'lookup-write-failure',
    'head-denial',
    'api-denial',
    'cache-hit-prefetch',
    'complete-buffer-prefetch',
    'network-restricted'
  ]) {
    test('production cache loader: $scenario', () async {
      // Cache-prefetch scenarios run with an explicit Wi-Fi policy.
      SharedPreferences.setMockInitialValues({'music-network-mode': 'wifi'});
      final directory =
          await Directory.systemTemp.createTemp('cache-provider-');
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      var headStatus = 200;
      final gets = <String>[];
      server.listen((request) async {
        request.response.statusCode =
            request.method == 'HEAD' ? headStatus : 200;
        request.response.headers.contentType = ContentType('audio', 'mpeg');
        request.response.headers.set('etag', 'one');
        if (request.method == 'GET') {
          gets.add(request.uri.path);
          request.response.headers.contentLength = 3;
          request.response.add([1, 2, 3]);
        }
        await request.response.close();
      });
      final origin = 'http://127.0.0.1:${server.port}',
          client = _Client('http://127.0.0.1:${server.port}');
      final cache = PlaybackCache(directory: directory);
      final player = _Player(), handler = PlayerAudioHandler(_Player());
      final mutations = CredentialMutationQueue()..enable(0, origin: origin);
      final router = ServerEndpointRouter(primary: Uri.parse(origin));
      final container = ProviderContainer(overrides: [
        playbackCacheProvider.overrideWith((ref) => cache),
        apiClientProvider.overrideWithValue(client),
        playerBaseUrlProvider.overrideWithValue(Uri.parse(origin)),
        serverEndpointRouterProvider.overrideWithValue(router),
        tokenStorageProvider.overrideWithValue(_Token()),
        cookieJarProvider.overrideWithValue(CookieJar()),
        credentialMutationQueueProvider.overrideWithValue(mutations),
        audioHandlerProvider.overrideWithValue(handler),
      ]);
      ServerPlaybackRequest song(int id) => ServerPlaybackRequest(
          mediaId: id,
          serverOrigin: origin,
          userId: 1,
          sourceUri: Uri.parse('$origin/api/v1/media/$id/stream'),
          title: 'Song $id');
      final key = cache.key(origin, 1, 1, 'auto');
      try {
        await cache.initialize();
        if (scenario != 'complete-buffer-prefetch') {
          await cache.download(
              key: key,
              uri: song(1).sourceUri,
              headers: {},
              title: 'Song 1',
              origin: origin,
              userId: 1,
              authorized: () => true);
        }
        gets.clear();
        final load = container.read(cachedAudioLoaderProvider);
        if (scenario == 'network-restricted') {
          await MusicNetworkPolicy.initialize();
          await MusicNetworkPolicy.save('mode', 'mobile');
          await MusicNetworkPolicy.save('stream-wifi', true);
          client.failure = const SocketException('offline');
          await expectLater(load(song(1), () => true),
              throwsA(isA<MusicNetworkRestricted>()));
          expect(gets, isEmpty,
              reason: 'a cached copy never bypasses the Wi-Fi restriction');
        } else if (scenario == 'lookup-write-failure') {
          await Directory('${directory.path}/index.tmp').create();
          final source = await load(song(1), () => true) as UriAudioSource;
          expect(source.uri.scheme, 'http',
              reason: 'failed LRU persistence must stream normally');
        } else if (scenario == 'head-denial' || scenario == 'api-denial') {
          final file = await cache.lookup(key);
          if (scenario == 'head-denial') {
            headStatus = 403;
          } else {
            client.failure = DioException(
                requestOptions: RequestOptions(path: '/'),
                response: Response(
                    requestOptions: RequestOptions(path: '/'),
                    statusCode: 404));
          }
          await expectLater(load(song(1), () => true), throwsA(anything));
          expect(await File.fromUri(file!).exists(), isTrue,
              reason: 'playing bytes retained');
          expect(
              (jsonDecode(await File('${directory.path}/index.json')
                      .readAsString()) as Map)
                  .containsKey(key),
              isFalse,
              reason: 'revocation persisted without unlinking playing bytes');
          expect(await cache.lookup(key), isNull,
              reason: 'denial prevents future cached opens');
          client.failure = const SocketException('offline');
          await expectLater(
              load(song(1), () => true), throwsA(isA<SocketException>()));
        } else {
          await cache.configure(
              next: true, buffer: scenario == 'complete-buffer-prefetch');
          handler.restoreMusicQueue(
              entries: [
                MusicQueueEntry(1, song(1)),
                MusicQueueEntry(2, song(2))
              ],
              index: 0,
              label: 'Test',
              position: 0,
              upNext: {},
              claim: (_) async => null,
              load: (_, __) async => null);
          final source = await load(song(1), () => true) as UriAudioSource;
          expect(source.uri.scheme, 'file');
          for (var i = 0;
              i < 100 && !gets.any((path) => path.contains('/2/'));
              i++) {
            await Future<void>.delayed(const Duration(milliseconds: 10));
          }
          expect(gets.any((path) => path.contains('/2/')), isTrue,
              reason: 'next song prefetched from $scenario');
        }
      } finally {
        await MusicNetworkPolicy.save('stream-wifi', false);
        await MusicNetworkPolicy.save('mode', 'wifi');
        cache.cancel();
        for (var i = 0; i < 100 && cache.busy; i++) {
          await Future<void>.delayed(const Duration(milliseconds: 10));
        }
        container.dispose();
        await router.close();
        await player.dispose();
        await handler.player.dispose();
        await server.close(force: true);
        if (await directory.exists()) await directory.delete(recursive: true);
      }
    });
  }
}
