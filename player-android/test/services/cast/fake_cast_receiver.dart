import 'dart:async';
import 'dart:convert';

import 'package:player_android/services/cast/cast_device.dart';
import 'package:player_android/services/cast/cast_protocol.dart';
import 'package:player_android/services/cast/cast_session.dart';

const kTestCastDevice = CastDevice(
  id: 'tv-1',
  name: 'Living Room TV',
  host: '192.0.2.10',
  port: 8009,
  model: 'Chromecast',
);

/// A [CastTransport] that behaves like a cast device running the Default
/// Media Receiver: it answers LAUNCH, LOAD and media commands and records
/// every message the sender wrote.
class FakeCastReceiver implements CastTransport {
  FakeCastReceiver({this.failLoad = false, this.splitFrames = false});

  final bool failLoad;

  /// Delivers each reply in two chunks, to exercise frame reassembly.
  final bool splitFrames;

  final _input = StreamController<List<int>>();
  final _decoder = CastFrameDecoder();
  final sent = <CastMessage>[];
  bool closed = false;
  double currentTime = 0;
  String playerState = 'IDLE';

  @override
  Stream<List<int>> get input => _input.stream;

  /// Payloads the sender wrote, in order.
  List<Map<String, dynamic>> get payloads =>
      sent.map((m) => m.json!).toList(growable: false);

  List<String> get types =>
      payloads.map((p) => p['type'] as String).toList(growable: false);

  @override
  void add(List<int> bytes) {
    for (final message in _decoder.add(bytes)) {
      sent.add(message);
      _respond(message);
    }
  }

  @override
  Future<void> close() async {
    closed = true;
    if (!_input.isClosed) await _input.close();
  }

  /// Sends [payload] to the sender as if the receiver wrote it.
  void push(String namespace, Map<String, dynamic> payload,
      {String source = 'transport-1'}) {
    final frame = encodeCastFrame(CastMessage(
      sourceId: source,
      destinationId: 'sender-0',
      namespace: namespace,
      payload: jsonEncode(payload),
    ));
    if (splitFrames) {
      _input.add(frame.sublist(0, 3));
      _input.add(frame.sublist(3));
    } else {
      _input.add(frame);
    }
  }

  /// Reports a media status change, as the receiver does unprompted.
  void pushMediaStatus(String state,
      {double? time, String? idleReason, int? requestId}) {
    playerState = state;
    if (time != null) currentTime = time;
    push('urn:x-cast:com.google.cast.media', {
      'type': 'MEDIA_STATUS',
      'requestId': requestId ?? 0,
      'status': [
        {
          'mediaSessionId': 1,
          'playerState': state,
          'currentTime': currentTime,
          if (idleReason != null) 'idleReason': idleReason,
          'media': {'duration': 600.0},
          'volume': {'level': 0.5},
        }
      ],
    });
  }

  void _respond(CastMessage message) {
    final json = message.json!;
    final id = json['requestId'];
    switch (json['type']) {
      case 'LAUNCH':
        // A broadcast first, before the app runs, then the answer.
        push('urn:x-cast:com.google.cast.receiver',
            {'type': 'RECEIVER_STATUS', 'requestId': 0, 'status': {}},
            source: 'receiver-0');
        push(
            'urn:x-cast:com.google.cast.receiver',
            {
              'type': 'RECEIVER_STATUS',
              'requestId': id,
              'status': {
                'applications': [
                  {
                    'appId': kDefaultMediaReceiverAppId,
                    'sessionId': 'app-session',
                    'transportId': 'transport-1',
                  }
                ],
                'volume': {'level': 0.5},
              },
            },
            source: 'receiver-0');
      case 'LOAD':
        if (failLoad) {
          push('urn:x-cast:com.google.cast.media',
              {'type': 'LOAD_FAILED', 'requestId': id});
        } else {
          pushMediaStatus('PLAYING',
              time: (json['currentTime'] as num).toDouble(), requestId: id);
        }
      case 'PAUSE':
        pushMediaStatus('PAUSED', requestId: id);
      case 'PLAY':
        pushMediaStatus('PLAYING', requestId: id);
      case 'SEEK':
        pushMediaStatus(playerState,
            time: (json['currentTime'] as num).toDouble(), requestId: id);
    }
  }
}
