import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:cookie_jar/cookie_jar.dart';
import 'package:player_android/api/dio_player_api_client.dart';
import 'package:flutter/material.dart';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _MemoryTokenStorage implements TokenStorage {
  String? token;

  @override
  Future<String?> readToken() async => token;
  @override
  Future<void> writeToken(String value) async => token = value;
  @override
  Future<void> deleteToken() async => token = null;
}

class _PausingTokenStorage extends _MemoryTokenStorage {
  _PausingTokenStorage() {
    token = 'pt-old';
  }
  final checked = Completer<void>();
  final proceed = Completer<void>();
  var reads = 0;

  @override
  Future<String?> readToken() async {
    reads++;
    if (reads == 2) {
      final value = token;
      checked.complete();
      await proceed.future;
      return value;
    }
    return token;
  }
}

class _CaptureAdapter implements HttpClientAdapter {
  RequestOptions? captured;

  @override
  Future<ResponseBody> fetch(RequestOptions options,
      Stream<Uint8List>? requestStream, Future? cancelFuture) async {
    captured = options;
    return ResponseBody.fromString('{}', 200);
  }

  @override
  void close({bool force = false}) {}
}

class _AuthAdapter implements HttpClientAdapter {
  _AuthAdapter(this.requests);
  final List<String> requests;

  @override
  Future<ResponseBody> fetch(RequestOptions options,
      Stream<Uint8List>? requestStream, Future? cancelFuture) async {
    final path = options.uri.path;
    requests.add(path);
    final headers = <String, List<String>>{
      Headers.contentTypeHeader: ['application/json']
    };
    if (path == '/api/v1/auth/login') {
      headers['set-cookie'] = ['session=temporary-session; Path=/; HttpOnly'];
      return ResponseBody.fromString(
          jsonEncode({'id': 1, 'username': 'alice', 'is_admin': true}), 200,
          headers: headers);
    }
    if (path == '/api/v1/auth/tokens' &&
        options.headers['cookie']?.toString().contains('temporary-session') ==
            true) {
      return ResponseBody.fromString(
          jsonEncode(
              {'id': 7, 'name': 'android-client', 'token': 'pt-persisted'}),
          200,
          headers: headers);
    }
    if (path == '/api/v1/sets' &&
        options.headers['Authorization'] == 'Bearer pt-persisted') {
      return ResponseBody.fromString('[]', 200, headers: headers);
    }
    return ResponseBody.fromString('{}', 401, headers: headers);
  }

  @override
  void close({bool force = false}) {}
}

class _DelayedUnauthorizedAdapter implements HttpClientAdapter {
  final received = Completer<RequestOptions>();
  final response = Completer<ResponseBody>();

  @override
  Future<ResponseBody> fetch(RequestOptions options,
      Stream<Uint8List>? requestStream, Future? cancelFuture) {
    received.complete(options);
    return response.future;
  }

  @override
  void close({bool force = false}) {}
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(() => SharedPreferences.setMockInitialValues({}));

  test('cold start reuses a minted token for a protected request', () async {
    final baseUrl = Uri.parse('https://player.example');
    final requests = <String>[];
    final storage = _MemoryTokenStorage();
    ProviderContainer container() => ProviderContainer(overrides: [
          tokenStorageProvider.overrideWithValue(storage),
          playerBaseUrlProvider.overrideWithValue(baseUrl),
          apiClientProvider.overrideWith((ref) {
            final client = DioClient(
              baseUrl: ref.watch(playerBaseUrlProvider),
              storage: ref.watch(tokenStorageProvider),
              navigatorKey: GlobalKey<NavigatorState>(),
            );
            client.dio.httpClientAdapter = _AuthAdapter(requests);
            return DioPlayerApiClient(dio: client.dio);
          }),
        ]);
    final first = container();
    await first.read(authStateProvider.future);
    final user = await first
        .read(apiClientProvider)
        .login(username: 'alice', password: 'secret');
    await first.read(authStateProvider.notifier).login(user);
    expect(storage.token, 'pt-persisted');
    first.dispose();

    final restarted = container();
    addTearDown(restarted.dispose);
    final auth = await restarted.read(authStateProvider.future);
    expect(auth.isAuthenticated, isTrue);
    expect(auth.user?.username, 'alice');
    expect(await restarted.read(apiClientProvider).listSets(), isEmpty);
    expect(requests,
        ['/api/v1/auth/login', '/api/v1/auth/tokens', '/api/v1/sets']);

    final wrongServer = ProviderContainer(overrides: [
      tokenStorageProvider.overrideWithValue(storage),
      playerBaseUrlProvider
          .overrideWithValue(Uri.parse('https://other.example')),
    ]);
    addTearDown(wrongServer.dispose);
    expect((await wrongServer.read(authStateProvider.future)).isUnauthenticated,
        isTrue);
    expect(storage.token, isNull);
  });

  test('expired credential is discarded before authentication', () async {
    SharedPreferences.setMockInitialValues({
      'auth_session_present': true,
      'auth_user': '{"id":1,"username":"alice","is_admin":true}',
      'auth_origin': 'https://player.example',
      'auth_expires_at': DateTime.now()
          .subtract(const Duration(seconds: 1))
          .millisecondsSinceEpoch,
    });
    final storage = _MemoryTokenStorage()..token = 'pt-expired';
    final container = ProviderContainer(overrides: [
      tokenStorageProvider.overrideWithValue(storage),
      playerBaseUrlProvider
          .overrideWithValue(Uri.parse('https://player.example')),
    ]);
    addTearDown(container.dispose);
    expect((await container.read(authStateProvider.future)).isUnauthenticated,
        isTrue);
    expect(storage.token, isNull);
  });

  test('a stale 401 does not clear a newer login credential', () async {
    final storage = _MemoryTokenStorage()..token = 'pt-old';
    final queue = CredentialMutationQueue();
    final adapter = _DelayedUnauthorizedAdapter();
    var logoutCalls = 0;
    final client = DioClient(
      baseUrl: Uri.parse('https://player.example'),
      storage: storage,
      mutationQueue: queue,
      navigatorKey: GlobalKey<NavigatorState>(),
      onUnauthorized: () async {
        logoutCalls++;
      },
    );
    client.dio.httpClientAdapter = adapter;

    final failedRequest = expectLater(
      client.dio.get('/api/v1/sets'),
      throwsA(isA<DioException>()),
    );
    final sent = await adapter.received.future;
    expect(sent.headers['Authorization'], 'Bearer pt-old');
    await queue.run(() => storage.writeToken('pt-new'));
    adapter.response.complete(ResponseBody.fromString('{}', 401));
    await failedRequest;

    expect(storage.token, 'pt-new');
    expect(logoutCalls, 0);
  });

  test('a 401 for the current credential clears authentication', () async {
    final storage = _MemoryTokenStorage()..token = 'pt-current';
    final adapter = _DelayedUnauthorizedAdapter();
    var logoutCalls = 0;
    final client = DioClient(
      baseUrl: Uri.parse('https://player.example'),
      storage: storage,
      navigatorKey: GlobalKey<NavigatorState>(),
      onUnauthorized: () async {
        logoutCalls++;
      },
    );
    client.dio.httpClientAdapter = adapter;

    final failedRequest = expectLater(
      client.dio.get('/api/v1/sets'),
      throwsA(isA<DioException>()),
    );
    await adapter.received.future;
    adapter.response.complete(ResponseBody.fromString('{}', 401));
    await failedRequest;

    expect(storage.token, isNull);
    expect(logoutCalls, 1);
  });

  test('login commit waits for in-progress 401 invalidation', () async {
    final storage = _PausingTokenStorage();
    final queue = CredentialMutationQueue();
    final adapter = _DelayedUnauthorizedAdapter();
    final client = DioClient(
      baseUrl: Uri.parse('https://player.example'),
      storage: storage,
      mutationQueue: queue,
      navigatorKey: GlobalKey<NavigatorState>(),
    );
    client.dio.httpClientAdapter = adapter;

    final failedRequest = expectLater(
      client.dio.get('/api/v1/sets'),
      throwsA(isA<DioException>()),
    );
    await adapter.received.future;
    adapter.response.complete(ResponseBody.fromString('{}', 401));
    await storage.checked.future;
    final newLogin = queue.run(() => storage.writeToken('pt-new'));
    await Future<void>.delayed(Duration.zero);
    expect(storage.token, 'pt-old');
    storage.proceed.complete();
    await failedRequest;
    await newLogin;
    expect(storage.token, 'pt-new');
  });

  test('cookie and bearer stay on the configured origin, including port',
      () async {
    final storage = _MemoryTokenStorage()..token = 'pt-secret';
    final client = DioClient(
      baseUrl: Uri.parse('https://player.example:443'),
      storage: storage,
      navigatorKey: GlobalKey<NavigatorState>(),
    );
    await client.cookieJar.saveFromResponse(
      Uri.parse('https://player.example:443/api/v1/sets'),
      [Cookie('session', 'secret-session')],
    );
    final adapter = _CaptureAdapter();
    client.dio.httpClientAdapter = adapter;
    await client.dio.get('https://player.example:444/api/v1/sets');

    expect(adapter.captured?.headers['cookie'], isNull);
    expect(adapter.captured?.headers['Authorization'], isNull);
  });
}
