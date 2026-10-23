import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http_mock_adapter/http_mock_adapter.dart';
import 'package:player_android/api/dio_player_api_client.dart';
import 'package:player_android/services/cast/cast_controller.dart';
import 'package:player_android/services/cast/cast_session.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/services/playback_session_coordinator.dart';

import 'fake_cast_receiver.dart';

class _Harness {
  _Harness({bool failLoad = false, bool failResolve = false}) {
    controller = CastController(
      coordinator: coordinator,
      progressInterval: const Duration(milliseconds: 20),
      resolveMedia: (request, {required isVideo}) async {
        if (failResolve) throw Exception('server down');
        resolvedVideo = isVideo;
        return CastMedia(
          url: Uri.parse('https://player.test/cast/T/stream'),
          contentType: 'audio/mpeg',
          title: request.title,
        );
      },
      startSession: (device) => CastSession.start(device,
          connect: (_) async =>
              receiver = FakeCastReceiver(failLoad: failLoad)),
    );
  }

  final coordinator = PlaybackSessionCoordinator();
  late final CastController controller;
  FakeCastReceiver? receiver;
  bool? resolvedVideo;
  final saved = <double>[];
  var finished = 0;

  ServerPlaybackRequest request({int mediaId = 5}) => ServerPlaybackRequest(
        mediaId: mediaId,
        serverOrigin: 'https://player.test',
        userId: 1,
        sourceUri:
            Uri.parse('https://player.test/api/v1/media/$mediaId/stream'),
        title: 'Song $mediaId',
        savePosition: (seconds) async => saved.add(seconds),
        markFinished: () async => finished++,
      );

  Future<void> cast(PlaybackRequest request, {Duration? at}) =>
      controller.start(
        device: kTestCastDevice,
        request: request,
        position: at ?? const Duration(seconds: 30),
        isVideo: true,
      );
}

void main() {
  test('casting takes the shared player from local playback', () async {
    final h = _Harness();
    var localStopped = false;
    await h.coordinator.claim(
      kind: PlaybackSourceKind.server,
      identity: 'local',
      stop: () async => localStopped = true,
    );
    final request = h.request();
    await h.cast(request);

    expect(localStopped, isTrue);
    expect(h.resolvedVideo, isTrue);
    expect(h.controller.isCasting(request.identity), isTrue);
    expect(h.controller.active!.device, kTestCastDevice);
    expect(h.coordinator.activeIdentity, request.identity);
    expect(h.receiver!.payloads.last['currentTime'], 30.0);
  });

  test('saves progress while casting and when it stops', () async {
    final h = _Harness();
    final ended = h.controller.ended.first;
    await h.cast(h.request());
    await Future<void>.delayed(const Duration(milliseconds: 60));
    expect(h.saved, isNotEmpty);
    expect(h.saved.first, greaterThanOrEqualTo(30.0));

    final position = await h.controller.stop();
    expect(position, greaterThanOrEqualTo(const Duration(seconds: 30)));
    expect(h.receiver!.types, contains('STOP'));
    expect(h.controller.active, isNull);
    expect(h.coordinator.activeIdentity, isNull);
    final event = await ended;
    expect(event.position, isNotNull);
    expect(event.failure, isNull);
  });

  test('marks the item finished when it ends on the receiver', () async {
    final h = _Harness();
    final ended = h.controller.ended.first;
    await h.cast(h.request());
    h.receiver!.pushMediaStatus('IDLE', idleReason: 'FINISHED');
    final event = await ended.timeout(const Duration(seconds: 1));
    expect(event.position, isNull);
    expect(h.finished, 1);
    expect(h.controller.active, isNull);
  });

  test('local playback of another item stops the cast', () async {
    final h = _Harness();
    final request = h.request();
    final ended = h.controller.ended.first;
    await h.cast(request);
    await h.coordinator.claim(
      kind: PlaybackSourceKind.server,
      identity: 'other',
      stop: () async {},
    );
    expect((await ended).identity, request.identity);
    expect(h.controller.active, isNull);
    expect(h.receiver!.types, contains('STOP'));
  });

  test('a receiver error ends the cast with a reason', () async {
    final h = _Harness();
    final ended = h.controller.ended.first;
    await h.cast(h.request());
    h.receiver!.pushMediaStatus('IDLE', idleReason: 'ERROR');
    expect((await ended).failure, CastFailure.loadFailed);
  });

  test('start failures are reported and keep the local player', () async {
    final local = _Harness(failLoad: true);
    var stopped = false;
    await local.coordinator.claim(
      kind: PlaybackSourceKind.server,
      identity: 'local',
      stop: () async => stopped = true,
    );
    await expectLater(
      local.cast(local.request()),
      throwsA(isA<CastStartException>()
          .having((e) => e.failure, 'failure', CastFailure.loadFailed)),
    );
    expect(stopped, isFalse);
    expect(local.controller.active, isNull);

    final server = _Harness(failResolve: true);
    await expectLater(
      server.cast(server.request()),
      throwsA(isA<CastStartException>()
          .having((e) => e.failure, 'failure', CastFailure.serverRefused)),
    );

    final device = _Harness();
    await expectLater(
      device.cast(LocalPlaybackRequest(
        localMediaId: 1,
        sourceUri: Uri.parse('content://doc/1'),
        title: 'Phone file',
      )),
      throwsA(isA<CastStartException>()
          .having((e) => e.failure, 'failure', CastFailure.notCastable)),
    );
  });

  test('server items cast through a ticket URL, compat when needed', () async {
    final dio = Dio(BaseOptions(baseUrl: 'https://player.test'));
    void configure(bool requiresCompat) {
      final adapter = DioAdapter(dio: dio);
      adapter.onPost(
        '/api/v1/media/5/cast',
        (server) => server.reply(200, {
          'ticket': 'TICKET',
          'expires_at': '2026-10-16T23:00:00Z',
          'stream_content_type': 'audio/flac',
          'compat_content_type': 'audio/mp4',
        }),
      );
      adapter.onGet(
          '/api/v1/media/5',
          (server) => server.reply(200, {
                'media': {
                  'id': 5,
                  'type': 'audio',
                  'transcoded': requiresCompat
                }
              }));
    }

    configure(false);
    final resolve = serverCastMediaResolver(() => DioPlayerApiClient(dio: dio));
    final h = _Harness();

    final original = await resolve(h.request(), isVideo: false);
    expect(original.url.toString(), 'https://player.test/cast/TICKET/stream');
    expect(original.contentType, 'audio/flac');
    expect(original.imageUrl.toString(),
        'https://player.test/cast/TICKET/thumbnail');

    // A source snapshot and a changed default must never decide receiver policy.
    configure(true);
    final originalSnapshot = await resolve(h.request(), isVideo: false);
    expect(originalSnapshot.url.path, '/cast/TICKET/compat');
    final compat = await resolve(
      h.request().withSourceUri(
          Uri.parse('https://player.test/api/v1/media/5/compat')),
      isVideo: false,
    );
    expect(compat.url.toString(), 'https://player.test/cast/TICKET/compat');
    expect(compat.contentType, 'audio/mp4');

    configure(false);
    final chosenProfile = await resolve(
        h.request().withSourceUri(Uri.parse(
            'https://player.test/api/v1/media/5/compat?profile=opus96')),
        isVideo: false);
    expect(chosenProfile.url.path, '/cast/TICKET/stream');
    final share = await resolve(
      PublicSharePlaybackRequest(
        shareToken: 'abc',
        sourceUri: Uri.parse('https://player.test/s/abc/stream?view=v'),
        title: 'Shared',
      ),
      isVideo: true,
    );
    expect(share.url.toString(), 'https://player.test/s/abc/stream?view=v');
    expect(share.contentType, 'video/mp4');
  });
}
