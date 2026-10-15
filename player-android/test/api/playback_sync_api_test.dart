import 'dart:async';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http_mock_adapter/http_mock_adapter.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/api/dio_player_api_client.dart';

class _Tokens implements TokenStorage {
  String token = 'old-account';
  final readStarted = Completer<void>();
  final releaseRead = Completer<void>();
  @override
  Future<String?> readToken() async {
    final captured = token;
    if (!readStarted.isCompleted) readStarted.complete();
    await releaseRead.future;
    return captured;
  }

  @override
  Future<void> writeToken(String value) async => token = value;
  @override
  Future<void> deleteToken() async {}
}

class _Transport implements HttpClientAdapter {
  final requests = <RequestOptions>[];
  @override
  Future<ResponseBody> fetch(RequestOptions options,
      Stream<Uint8List>? requestStream, Future? cancelFuture) async {
    requests.add(options);
    return ResponseBody.fromString('{}', 200);
  }

  @override
  void close({bool force = false}) {}
}

void main() {
  test('sync and completion preserve edit times, tombstones and captured epoch',
      () async {
    final dio = Dio(BaseOptions(baseUrl: 'https://player.test'));
    final adapter = DioAdapter(dio: dio);
    final epochs = <Object?>[];
    dio.interceptors.add(InterceptorsWrapper(onRequest: (options, handler) {
      epochs.add(options.extra['credentialEpoch']);
      handler.next(options);
    }));
    final client = DioPlayerApiClient(dio: dio, credentialEpoch: () => 7);
    final state = <String, dynamic>{
      'progress': {
        'position_seconds': 42.5,
        'finished': false,
        'observed_ms': 1792065600123
      },
      'bookmarks': [
        {
          'sync_key': 'device-chapter',
          'position_seconds': 12.5,
          'label': 'Chapter',
          'observed_ms': 1792065600456,
          'deleted': true
        }
      ]
    };
    adapter.onPost(
        '/api/v1/media/42/playback-sync', (server) => server.reply(200, state),
        data: state);
    const observed = '2026-10-15T12:00:00.123Z';
    adapter.onPost('/api/v1/progress/status',
        (server) => server.reply(200, {'status': 'ok'}),
        data: {'media_id': 42, 'status': 'finished', 'observed_at': observed});
    expect(await client.syncPlayback(42, state), state);
    await client.updateProgressStatusAt(
        mediaId: 42, status: 'finished', observedAt: observed);
    expect(epochs, [7, 7]);
  });

  test('sync rejection and unsupported endpoints propagate for durable retry',
      () async {
    final dio = Dio(BaseOptions(baseUrl: 'https://player.test'));
    final adapter = DioAdapter(dio: dio);
    final client = DioPlayerApiClient(dio: dio);
    for (final code in [401, 403, 404, 503]) {
      adapter.onPost('/api/v1/media/$code/playback-sync',
          (server) => server.reply(code, {'error': 'unavailable'}),
          data: <String, dynamic>{});
      await expectLater(
          client.syncPlayback(code, {}),
          throwsA(isA<DioException>()
              .having((error) => error.response?.statusCode, 'status', code)));
    }
  });

  for (final completion in [false, true]) {
    test(
        'account change during credential read cancels ${completion ? 'completion' : 'download sync'} before dispatch',
        () async {
      final tokens = _Tokens();
      final queue = CredentialMutationQueue(credentialsEnabled: true);
      final capturedEpoch = queue.generation;
      final transport = _Transport();
      final dio = DioClient(
          baseUrl: Uri.parse('https://player.test'),
          storage: tokens,
          mutationQueue: queue,
          navigatorKey: GlobalKey<NavigatorState>())
        ..dio.httpClientAdapter = transport;
      final client = DioPlayerApiClient(
          dio: dio.dio, credentialEpoch: () => capturedEpoch);
      final request = completion
          ? client.updateProgressStatusAt(
              mediaId: 42,
              status: 'finished',
              observedAt: '2026-10-15T12:00:00Z')
          : client.syncPlayback(42, {});
      final assertion = expectLater(
          request,
          throwsA(isA<DioException>()
              .having((error) => error.type, 'type', DioExceptionType.cancel)));
      await tokens.readStarted.future;
      final nextEpoch = queue.beginAuthChange();
      await tokens.writeToken('new-account');
      queue.enable(nextEpoch);
      tokens.releaseRead.complete();
      await assertion;
      expect(transport.requests, isEmpty);
      dio.dio.close();
    });
  }
}
