import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/services/cast/cast_device.dart';
import 'package:player_android/services/cast/cast_session.dart';
import 'package:player_android/services/cast/renderer_session.dart';

class _Server implements HttpClientAdapter {
  final commands = <Map<String, dynamic>>[];
  Completer<void>? loadGate;
  var state = 'PLAYING';
  var position = 98;
  @override
  Future<ResponseBody> fetch(RequestOptions options,
      Stream<Uint8List>? requestStream, Future<void>? cancelFuture) async {
    final command = Map<String, dynamic>.from(options.data as Map);
    commands.add(command);
    if (command['action'] == 'load') await loadGate?.future;
    final result = command['action'] == 'status'
        ? {'state': state, 'position': position, 'duration': 100}
        : {'state': 'PLAYING', 'duration': 100};
    return ResponseBody.fromString(jsonEncode(result), 200, headers: {
      Headers.contentTypeHeader: ['application/json']
    });
  }

  @override
  void close({bool force = false}) {}
}

void main() {
  test(
      'Stop during pending renderer load carries newer revision and prevents polling restart',
      () async {
    final server = _Server()..loadGate = Completer<void>();
    final dio = Dio(BaseOptions(baseUrl: 'http://player.test'))
      ..httpClientAdapter = server;
    final session = RendererSession(
        const CastDevice(
            id: 'opaque', name: 'Kitchen', host: '', port: 0, isRenderer: true),
        PlayerApiClient(dio: dio));
    final loading = session.load(CastMedia(
        mediaId: 5,
        url: Uri.parse('http://player.test/cast/T/stream'),
        contentType: 'audio/mpeg',
        title: 'Song'));
    await Future<void>.delayed(Duration.zero);
    await session.stop();
    server.loadGate!.complete();
    await loading;
    expect(
        server.commands.map((command) => command['action']), ['load', 'stop']);
    expect(server.commands.last['revision'],
        greaterThan(server.commands.first['revision'] as int));
    await session.closed;
  });

  test(
      'renderer sends IDs through server and detects end despite reset RelTime',
      () async {
    final server = _Server();
    final dio = Dio(BaseOptions(baseUrl: 'http://player.test'))
      ..httpClientAdapter = server;
    final api = PlayerApiClient(dio: dio);
    const device = CastDevice(
        id: 'opaque', name: 'Kitchen', host: '', port: 0, isRenderer: true);
    final session = RendererSession(device, api);
    await session.load(
        CastMedia(
            mediaId: 5,
            url: Uri.parse('http://player.test/cast/T/stream'),
            contentType: 'audio/mpeg',
            title: 'Song'),
        position: const Duration(seconds: 4));
    expect(
        {...server.commands.first}
          ..removeWhere((key, _) => key == 'session_id' || key == 'revision'),
        {
          'device_id': 'opaque',
          'action': 'load',
          'media_id': 5,
          'position': 4.0
        });
    expect(server.commands.first['session_id'],
        matches(RegExp(r'^[a-f0-9]{32}$')));
    expect(server.commands.first['revision'], 1);
    expect(session.lastStatus.playerState, CastPlayerState.playing);
    expect(session.position, const Duration(seconds: 98));
    server.state = 'STOPPED';
    server.position = 0;
    await session.seek(const Duration(seconds: 99));
    expect(session.lastStatus.finished, isTrue);
    await session.stop();
    await session.closed;
    expect(server.commands.last['action'], 'stop');
    expect(server.commands.last['revision'], 2);
    expect(server.commands.every((command) => !command.containsKey('url')),
        isTrue);
  });
}
