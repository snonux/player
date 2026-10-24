import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'package:cookie_jar/cookie_jar.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/models/user.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/media_artwork_provider.dart';
import 'package:player_android/services/playback_request.dart';

import '../support/server_endpoint.dart';

class _Tokens implements TokenStorage {
  @override
  Future<String?> readToken() async => 'private-test-token';
  @override
  Future<void> writeToken(String token) async {}
  @override
  Future<void> deleteToken() async {}
}

class _Auth extends AuthStateNotifier {
  @override
  Future<AuthState> build() async => const AuthState.authenticated(
      user: User(id: 1, username: 'one', isAdmin: false));
  void switchAccount() => state = const AsyncData(AuthState.unauthenticated());
}

void main() {
  test(
      'artwork HTTP boundary rejects foreign/redirect/denied/stale account and source',
      () async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    addTearDown(() => server.close(force: true));
    final origin = 'http://127.0.0.1:${server.port}';
    final seen = <HttpRequest>[];
    final delayed = <HttpRequest>[];
    final received = StreamController<void>.broadcast();
    addTearDown(received.close);
    final png = base64Decode(
        'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aZ1kAAAAASUVORK5CYII=');
    server.listen((request) async {
      seen.add(request);
      if (request.uri.path.startsWith('/delay')) {
        delayed.add(request);
        received.add(null);
        return;
      }
      if (request.uri.path == '/redirect') {
        request.response.statusCode = 302;
        request.response.headers.set('location', '$origin/leaked');
      } else if (request.uri.path == '/denied') {
        request.response.statusCode = 403;
      } else {
        request.response.headers.contentType = ContentType('image', 'png');
        request.response.add(png);
      }
      await request.response.close();
    });
    final container = ProviderContainer(overrides: [
      unpairedServerEndpointOverride(),
      playerBaseUrlProvider.overrideWithValue(Uri.parse(origin)),
      authStateProvider.overrideWith(_Auth.new),
      tokenStorageProvider.overrideWithValue(_Tokens()),
      cookieJarProvider.overrideWithValue(CookieJar()),
      credentialMutationQueueProvider
          .overrideWithValue(CredentialMutationQueue(credentialsEnabled: true)),
    ]);
    addTearDown(container.dispose);
    await container.read(authStateProvider.future);
    final load = container.read(mediaArtworkLoaderProvider);
    PlaybackRequest request(String path) => ServerPlaybackRequest(
        mediaId: 1,
        serverOrigin: origin,
        userId: 1,
        sourceUri: Uri.parse('$origin/stream'),
        title: 'Song',
        artworkUri: Uri.parse(path.startsWith('http') ? path : '$origin$path'));
    expect(await load(request('http://elsewhere.invalid/private'), () => true),
        isNull);
    expect(seen, isEmpty,
        reason: 'foreign art receives no credentials or network request');
    expect(await load(request('/redirect'), () => true), isNull);
    expect(seen.map((r) => r.uri.path), ['/redirect'],
        reason: 'credential-bearing redirects never followed');
    expect(await load(request('/denied'), () => true), isNull);
    final art = await load(request('/valid'), () => true);
    expect(art?.scheme, 'file');
    final file = File.fromUri(art!);
    addTearDown(() async {
      if (await file.exists()) await file.delete();
    });
    expect(await file.readAsBytes(), png);
    expect(
        seen.last.headers.value('authorization'), 'Bearer private-test-token');
    var current = true;
    final previous =
        List.generate(12, (i) => load(request('/delay/$i'), () => current));
    while (delayed.length < 12) {
      await received.stream.first.timeout(const Duration(seconds: 5));
    }
    current = false;
    for (final r in delayed) {
      r.response.headers.contentType = ContentType('image', 'png');
      r.response.add(png);
      await r.response.close();
    }
    expect(await Future.wait(previous), everyElement(isNull));
    expect(await file.readAsBytes(), png,
        reason: 'late superseded images cannot prune the active OS artwork');
    delayed.clear();
    final pending = load(request('/delay/account'), () => true);
    while (delayed.isEmpty) {
      await received.stream.first.timeout(const Duration(seconds: 5));
    }
    (container.read(authStateProvider.notifier) as _Auth).switchAccount();
    delayed.single.response.headers.contentType = ContentType('image', 'png');
    delayed.single.response.add(png);
    await delayed.single.response.close();
    expect(await pending, isNull,
        reason:
            'account loss while reading image rejects publication/cache write');
    expect(await file.readAsBytes(), png);
  });
}
