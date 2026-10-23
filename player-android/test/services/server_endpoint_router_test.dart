import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:player_android/providers/settings_provider.dart';
import 'dart:io';
import 'dart:convert';
import 'dart:typed_data';
import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/models/server_endpoint_profile.dart';
import 'package:player_android/services/server_endpoint_router.dart';

const id = 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa';
const otherId =
    'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb';
ServerEndpointProfile profile({String wifi = ''}) => ServerEndpointProfile(
    primary: 'https://remote.example',
    local: 'http://192.168.1.2:8080',
    serverId: id,
    wifiName: wifi);

class RecordingAdapter implements HttpClientAdapter {
  final requests = <RequestOptions>[];
  bool failFirst = false;
  @override
  Future<ResponseBody> fetch(RequestOptions options, Stream<Uint8List>? body,
      Future<void>? cancelFuture) async {
    requests.add(options);
    if (failFirst && requests.length == 1) {
      throw StateError('connection failed');
    }
    return ResponseBody.fromString('{}', 200);
  }

  @override
  void close({bool force = false}) {}
}

void main() {
  test('preferred failure probes alternate with no account mutation', () async {
    final attempts = <String>[];
    final router = ServerEndpointRouter(
        primary: Uri.parse(profile().primary),
        profile: profile(),
        probe: (uri) async {
          attempts.add(uri.origin);
          if (uri.host == '192.168.1.2') throw StateError('offline');
          return id;
        });
    final result = await router
        .resolve(Uri.parse('https://remote.example/api/v1/media/3/stream?x=1'));
    expect(result.origin, profile().primary);
    expect(result.path, '/api/v1/media/3/stream');
    expect(result.query, 'x=1');
    expect(attempts, [profile().local, profile().primary]);
  });
  test('wrong installation fails closed without probing another address',
      () async {
    final attempts = <String>[];
    final router = ServerEndpointRouter(
        primary: Uri.parse(profile().primary),
        profile: profile(),
        probe: (uri) async {
          attempts.add(uri.origin);
          return otherId;
        });
    await expectLater(
        router.resolve(Uri.parse('${profile().primary}/api/config')),
        throwsA(isA<EndpointIdentityException>()));
    expect(attempts, [profile().local]);
  });
  test('SSID denied or different restricts routing to primary', () async {
    for (final name in [null, 'Other']) {
      final attempts = <String>[];
      final router = ServerEndpointRouter(
          primary: Uri.parse(profile().primary),
          profile: profile(wifi: 'Home'),
          wifiName: () async => name,
          probe: (uri) async {
            attempts.add(uri.origin);
            return id;
          });
      expect(
          (await router.resolve(Uri.parse('${profile().primary}/api/config')))
              .origin,
          profile().primary);
      expect(attempts, [profile().primary]);
    }
  });
  test('exact SSID permits preferred local transport', () async {
    final router = ServerEndpointRouter(
        primary: Uri.parse(profile().primary),
        profile: profile(wifi: 'Home'),
        wifiName: () async => 'Home',
        probe: (_) async => id);
    expect(
        (await router.resolve(Uri.parse('${profile().primary}/api/config')))
            .origin,
        profile().local);
    expect(
        (await router.resolve(Uri.parse('https://other.example/api/config')))
            .origin,
        'https://other.example');
  });
  test('adapter preserves canonical request and disables redirects', () async {
    final router = ServerEndpointRouter(
        primary: Uri.parse(profile().primary),
        profile: profile(),
        probe: (_) async => id);
    final delegate = RecordingAdapter();
    final adapter = EndpointHttpAdapter(router, delegate: delegate);
    final request = RequestOptions(
        path: '/api/config',
        baseUrl: profile().primary,
        headers: {'Authorization': 'Bearer private'},
        queryParameters: {'search': 'a b', 'page': 2});
    await adapter.fetch(request, null, null);
    expect(request.uri.origin, profile().primary);
    expect(delegate.requests.single.uri.origin, profile().local);
    expect(delegate.requests.single.followRedirects, false);
    expect(delegate.requests.single.uri.queryParametersAll, {
      'search': ['a b'],
      'page': ['2']
    });
    expect(request.queryParameters, {'search': 'a b', 'page': 2});
    expect(delegate.requests.single.headers['Authorization'], 'Bearer private');
  });
  test('ambiguous writes are never replayed', () async {
    final router = ServerEndpointRouter(
        primary: Uri.parse(profile().primary),
        profile: profile(),
        probe: (_) async => id);
    final delegate = RecordingAdapter()..failFirst = true;
    final adapter = EndpointHttpAdapter(router, delegate: delegate);
    await expectLater(
        adapter.fetch(
            RequestOptions(
                path: '/api/progress',
                baseUrl: profile().primary,
                method: 'POST'),
            null,
            null),
        throwsStateError);
    expect(delegate.requests, hasLength(1));
  });
  test('GET transport failure retries another verified origin once', () async {
    final router = ServerEndpointRouter(
        primary: Uri.parse(profile().primary),
        profile: profile(),
        probe: (_) async => id);
    final delegate = RecordingAdapter()..failFirst = true;
    await EndpointHttpAdapter(router, delegate: delegate).fetch(
        RequestOptions(path: '/api/config', baseUrl: profile().primary),
        null,
        null);
    expect(delegate.requests.map((r) => r.uri.origin),
        [profile().local, profile().primary]);
  });
  test(
      'native gateway forwards Range and rejects redirects without leaking credentials',
      () async {
    final sink = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    var sinkRequests = 0;
    sink.listen((request) async {
      sinkRequests++;
      await request.response.close();
    });
    final upstream = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final seen = <String, String?>{};
    var redirect = false;
    upstream.listen((request) async {
      seen['authorization'] = request.headers.value('authorization');
      seen['range'] = request.headers.value('range');
      if (redirect) {
        request.response.statusCode = 302;
        request.response.headers
            .set('location', 'http://127.0.0.1:${sink.port}/stolen');
      } else {
        request.response.statusCode = 206;
        request.response.headers.set('content-range', 'bytes 2-4/6');
        request.response.headers.set('content-type', 'audio/mpeg');
        request.response.add([2, 3, 4]);
      }
      await request.response.close();
    });
    final primary = Uri.parse('https://remote.example');
    final router = ServerEndpointRouter(
        primary: primary,
        profile: ServerEndpointProfile(
            primary: primary.origin,
            local: 'http://127.0.0.1:${upstream.port}',
            serverId: id),
        probe: (_) async => id);
    final client = HttpClient();
    try {
      final canonical = primary.resolve('/api/v1/media/3/stream');
      final uri = await router.mediaUri(canonical);
      expect(uri.host, '127.0.0.1');
      expect(uri.toString(), isNot(contains('private')));
      final request = await client.getUrl(uri);
      request.headers.set('authorization', 'Bearer private');
      request.headers.set('range', 'bytes=2-4');
      final response = await request.close();
      expect(response.statusCode, 206);
      expect(response.headers.value('content-range'), 'bytes 2-4/6');
      expect(await response.fold<List<int>>([], (a, b) => a..addAll(b)),
          [2, 3, 4]);
      expect(seen, {'authorization': 'Bearer private', 'range': 'bytes=2-4'});
      redirect = true;
      final redirectRequest = await client.getUrl(uri);
      redirectRequest.headers.set('authorization', 'Bearer private');
      final blocked = await redirectRequest.close();
      expect(blocked.statusCode, 502);
      expect(blocked.headers.value('location'), isNull);
      await blocked.drain<void>();
      expect(sinkRequests, 0);
      final parts = uri.pathSegments;
      final forged = uri.replace(
          path:
              '/${parts[0]}/${base64Url.encode(utf8.encode('http://127.0.0.1:${sink.port}/api/v1/media/3/stream'))}');
      final forgedResponse = await (await client.getUrl(forged)).close();
      expect(forgedResponse.statusCode, 404);
      await forgedResponse.drain<void>();
      expect(sinkRequests, 0);
      await router.close();
      await expectLater(router.mediaUri(canonical), throwsStateError);
      await expectLater(
          client.getUrl(uri).then((r) => r.close()), throwsA(anything));
    } finally {
      client.close(force: true);
      await router.close();
      await upstream.close(force: true);
      await sink.close(force: true);
    }
  });
  test('cached native address failure falls back before any response',
      () async {
    final preferred = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final alternate = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    void serve(HttpServer server, String text) {
      server.listen((request) async {
        request.response.write(text);
        await request.response.close();
      });
    }

    serve(preferred, 'local');
    serve(alternate, 'remote');
    final primary = Uri.parse('http://127.0.0.1:${alternate.port}');
    final statuses = <String>[];
    final router = ServerEndpointRouter(
        onStatus: statuses.add,
        primary: primary,
        profile: ServerEndpointProfile(
            primary: primary.origin,
            local: 'http://127.0.0.1:${preferred.port}',
            serverId: id),
        probe: (_) async => id);
    final client = HttpClient();
    Future<String> read(Uri uri) async =>
        utf8.decode(await (await (await client.getUrl(uri)).close())
            .fold<List<int>>([], (a, b) => a..addAll(b)));
    try {
      final uri =
          await router.mediaUri(primary.resolve('/api/v1/media/3/stream'));
      expect(await read(uri), 'local');
      await preferred.close(force: true);
      expect(await read(uri), 'remote');
      expect(statuses.last,
          startsWith('Preferred address unavailable · Connected using'));
    } finally {
      client.close(force: true);
      await router.close();
      await preferred.close(force: true);
      await alternate.close(force: true);
    }
  });
  test('saved paired addresses retain canonical profile and separate servers',
      () async {
    SharedPreferences.setMockInitialValues({
      'server_base_url': profile().primary,
      'saved_servers': [profile().primary],
      'library_destination': 'server'
    });
    final container = ProviderContainer();
    try {
      await container.read(settingsProvider.future);
      await container
          .read(settingsProvider.notifier)
          .saveEndpointProfile(profile().primary, profile());
      final settings = container.read(settingsProvider).requireValue;
      expect(settings.serverBaseUrl, profile().primary);
      expect(settings.savedServers, [profile().primary]);
      expect(
          settings.endpointProfiles[profile().primary]!.local, profile().local);
      final fresh = ProviderContainer();
      try {
        final restored = await fresh.read(settingsProvider.future);
        expect(restored.serverBaseUrl, profile().primary);
        expect(restored.endpointProfiles[profile().primary]!.serverId, id);
        await fresh
            .read(settingsProvider.notifier)
            .setServerBaseUrl('https://other.example');
        expect(
            fresh
                .read(settingsProvider)
                .requireValue
                .endpointProfiles['https://other.example'],
            isNull);
      } finally {
        fresh.dispose();
      }
    } finally {
      container.dispose();
    }
  });
}
