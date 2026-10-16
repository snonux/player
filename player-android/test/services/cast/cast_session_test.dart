import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/cast/cast_session.dart';

import 'fake_cast_receiver.dart';

CastMedia _media() => CastMedia(
      url: Uri.parse('https://player.test/cast/T/stream'),
      contentType: 'audio/mpeg',
      title: 'Song',
      imageUrl: Uri.parse('https://player.test/cast/T/thumbnail'),
    );

Future<(CastSession, FakeCastReceiver)> _start({
  bool failLoad = false,
  bool splitFrames = false,
  DateTime Function()? now,
}) async {
  final receiver =
      FakeCastReceiver(failLoad: failLoad, splitFrames: splitFrames);
  final session = await CastSession.start(
    kTestCastDevice,
    connect: (_) async => receiver,
    now: now ?? DateTime.now,
  );
  return (session, receiver);
}

void main() {
  test('launches the Default Media Receiver and loads the media', () async {
    final (session, receiver) = await _start(splitFrames: true);
    await session.load(_media(), position: const Duration(seconds: 42));

    expect(receiver.types, ['CONNECT', 'LAUNCH', 'CONNECT', 'LOAD']);
    expect(receiver.sent[1].destinationId, 'receiver-0');
    expect(receiver.payloads[1]['appId'], kDefaultMediaReceiverAppId);
    // The second CONNECT and the LOAD go to the launched app.
    expect(receiver.sent[2].destinationId, 'transport-1');
    expect(receiver.sent[3].destinationId, 'transport-1');
    final load = receiver.payloads[3];
    expect(load['currentTime'], 42.0);
    expect(load['autoplay'], true);
    expect(load['media']['contentId'], 'https://player.test/cast/T/stream');
    expect(load['media']['contentType'], 'audio/mpeg');
    expect(load['media']['metadata']['title'], 'Song');
    expect(load['media']['metadata']['images'][0]['url'],
        'https://player.test/cast/T/thumbnail');

    expect(session.lastStatus.playerState, CastPlayerState.playing);
    expect(session.lastStatus.duration, const Duration(minutes: 10));
    expect(session.lastStatus.volume, 0.5);
    await session.stop();
  });

  test('controls send the media session id and update the status', () async {
    final (session, receiver) = await _start();
    await session.load(_media());

    await session.pause();
    expect(receiver.payloads.last['type'], 'PAUSE');
    expect(receiver.payloads.last['mediaSessionId'], 1);
    expect(session.lastStatus.playerState, CastPlayerState.paused);

    await session.seek(const Duration(seconds: 90));
    expect(receiver.payloads.last['currentTime'], 90.0);
    expect(session.position, const Duration(seconds: 90));

    await session.play();
    expect(session.lastStatus.playerState, CastPlayerState.playing);

    await session.setVolume(1.5);
    expect(receiver.payloads.last['type'], 'SET_VOLUME');
    expect(receiver.payloads.last['volume'], {'level': 1.0});
    await session.stop();
  });

  test('estimates the position while playing', () async {
    var now = DateTime(2026, 1, 1, 12);
    final (session, receiver) = await _start(now: () => now);
    await session.load(_media(), position: const Duration(seconds: 10));
    now = now.add(const Duration(seconds: 5));
    expect(session.position, const Duration(seconds: 15));

    // Capped at the duration.
    now = now.add(const Duration(hours: 1));
    expect(session.position, const Duration(minutes: 10));

    receiver.pushMediaStatus('PAUSED', time: 20);
    await pumpEventQueue();
    now = now.add(const Duration(seconds: 30));
    expect(session.position, const Duration(seconds: 20));
    await session.stop();
  });

  test('answers PING with PONG', () async {
    final (session, receiver) = await _start();
    receiver.push('urn:x-cast:com.google.cast.tp.heartbeat', {'type': 'PING'},
        source: 'receiver-0');
    await pumpEventQueue();
    expect(receiver.sent.last.namespace,
        'urn:x-cast:com.google.cast.tp.heartbeat');
    expect(receiver.types.last, 'PONG');
    await session.stop();
  });

  test('a refused LOAD throws', () async {
    final (session, _) = await _start(failLoad: true);
    await expectLater(session.load(_media()), throwsA(isA<CastException>()));
    await session.stop();
  });

  test('stop ends the receiver app; disconnect leaves it playing', () async {
    final (stopped, stoppedReceiver) = await _start();
    await stopped.stop();
    expect(stoppedReceiver.types.last, 'STOP');
    expect(stoppedReceiver.payloads.last['sessionId'], 'app-session');
    expect(stoppedReceiver.closed, isTrue);
    expect(stopped.isClosed, isTrue);

    final (left, leftReceiver) = await _start();
    await left.disconnect();
    expect(leftReceiver.types.last, 'CLOSE');
    expect(leftReceiver.types, isNot(contains('STOP')));
  });

  test('the session closes when the receiver app goes away', () async {
    final (session, receiver) = await _start();
    receiver.push(
        'urn:x-cast:com.google.cast.receiver',
        {
          'type': 'RECEIVER_STATUS',
          'requestId': 0,
          'status': {'applications': []},
        },
        source: 'receiver-0');
    await session.closed.timeout(const Duration(seconds: 1));
    expect(receiver.closed, isTrue);
  });

  test('the session closes when the connection drops', () async {
    final (session, receiver) = await _start();
    await receiver.close();
    await session.closed.timeout(const Duration(seconds: 1));
    await expectLater(session.play(), throwsA(isA<CastException>()));
  });
}
