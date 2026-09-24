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
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/models/user.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/progress_queue_provider.dart';
import 'package:player_android/services/progress_queue.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _MemoryTokenStorage implements TokenStorage {
  String? token;
  bool failNextRead = false;
  bool failDelete = false;
  bool failWrite = false;

  @override
  Future<String?> readToken() async {
    if (failNextRead) {
      failNextRead = false;
      throw StateError('secure storage read failed');
    }
    return token;
  }

  @override
  Future<void> writeToken(String value) async {
    if (failWrite) throw StateError('secure storage write failed');
    token = value;
  }

  @override
  Future<void> deleteToken() async {
    if (failDelete) throw StateError('secure storage delete failed');
    token = null;
  }
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

class _RestorePausingStorage extends _MemoryTokenStorage {
  _RestorePausingStorage() {
    token = 'pt-restored';
  }

  final readStarted = Completer<void>();
  final releaseRead = Completer<void>();

  @override
  Future<String?> readToken() async {
    if (!readStarted.isCompleted) readStarted.complete();
    await releaseRead.future;
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
  bool revoked = false;
  bool currentTokenRevoked = false;
  bool sessionLoggedOut = false;
  bool failRevocation = false;
  int tokenMintCount = 0;
  int loginCount = 0;
  RequestOptions? lastProtectedRequest;
  String? lastRevocationAuthorization;

  @override
  Future<ResponseBody> fetch(RequestOptions options,
      Stream<Uint8List>? requestStream, Future? cancelFuture) async {
    final path = options.uri.path;
    requests.add(path);
    final headers = <String, List<String>>{
      Headers.contentTypeHeader: ['application/json']
    };
    if (path == '/api/v1/auth/login') {
      loginCount++;
      headers['set-cookie'] = [
        'session=${loginCount == 1 ? 'temporary-session' : 'temporary-session-2'}; Path=/; HttpOnly'
      ];
      return ResponseBody.fromString(
          jsonEncode({'id': 1, 'username': 'alice', 'is_admin': true}), 200,
          headers: headers);
    }
    if (path == '/api/v1/auth/tokens' &&
        options.headers['cookie']?.toString().contains('temporary-session') ==
            true) {
      tokenMintCount++;
      return ResponseBody.fromString(
          jsonEncode({
            'id': 7,
            'name': 'android-client',
            'token': tokenMintCount == 1 ? 'pt-persisted' : 'pt-persisted-2',
          }),
          200,
          headers: headers);
    }
    if (path == '/api/v1/auth/tokens/7' && options.method == 'DELETE') {
      lastRevocationAuthorization =
          options.headers['Authorization']?.toString();
      if (lastRevocationAuthorization != 'Bearer pt-persisted') {
        return ResponseBody.fromString('{}', 401, headers: headers);
      }
      if (failRevocation) {
        return ResponseBody.fromString('{}', 503, headers: headers);
      }
      revoked = true;
      return ResponseBody.fromString('', 204, headers: headers);
    }
    if (path == '/api/v1/auth/tokens/current' &&
        options.method == 'DELETE' &&
        options.headers['Authorization'] == 'Bearer pt-persisted') {
      currentTokenRevoked = true;
      revoked = true;
      return ResponseBody.fromString('', 204, headers: headers);
    }
    if (path == '/api/v1/logout' &&
        options.method == 'POST' &&
        options.headers['cookie'] == 'session=temporary-session' &&
        !options.headers.containsKey('Authorization')) {
      sessionLoggedOut = true;
      headers['set-cookie'] = ['session=; Max-Age=0; Path=/; HttpOnly'];
      return ResponseBody.fromString('', 204, headers: headers);
    }
    if (path == '/api/v1/sets' &&
        options.headers['Authorization'] == 'Bearer pt-persisted' &&
        !revoked) {
      return ResponseBody.fromString('[]', 200, headers: headers);
    }
    if (path == '/api/v1/sets' &&
        options.headers['cookie'] == 'session=temporary-session' &&
        !sessionLoggedOut) {
      return ResponseBody.fromString('[]', 200, headers: headers);
    }
    if (path == '/api/v1/sets') lastProtectedRequest = options;
    return ResponseBody.fromString('{}', 401, headers: headers);
  }

  @override
  void close({bool force = false}) {}
}

class _DelayedMintApiClient extends PlayerApiClient {
  _DelayedMintApiClient() : super(dio: Dio());

  final started = Completer<void>();
  final release = Completer<Map<String, dynamic>>();
  int? revokedId;
  String? revokedBearer;

  @override
  Future<Map<String, dynamic>> createAPIToken(
      {required String name, int? expiresInDays}) async {
    started.complete();
    return release.future;
  }

  @override
  Future<void> revokeAPIToken(int tokenId, {String? bearerToken}) async {
    revokedId = tokenId;
    revokedBearer = bearerToken;
  }
}

class _PausingResumeQueue implements ProgressQueueBase {
  final resumeStarted = Completer<void>();
  final releaseResume = Completer<void>();
  bool suspended = false;

  @override
  Future<void> init({bool suspended = false}) async {}

  @override
  Future<void> resume() async {
    resumeStarted.complete();
    await releaseResume.future;
    suspended = false;
  }

  @override
  Future<void> clearAndSuspend() async => suspended = true;

  @override
  Future<void> enqueue(int mediaId, double positionSeconds,
      {bool finished = false}) async {}

  @override
  Future<void> dispose() async {}
}

class _BlockingRevocationAdapter extends _AuthAdapter {
  _BlockingRevocationAdapter() : super([]);

  final revocationStarted = Completer<void>();
  final releaseRevocation = Completer<void>();

  @override
  Future<ResponseBody> fetch(RequestOptions options,
      Stream<Uint8List>? requestStream, Future? cancelFuture) async {
    if (options.uri.path == '/api/v1/auth/tokens/7' &&
        options.method == 'DELETE') {
      revocationStarted.complete();
      await releaseRevocation.future;
      return ResponseBody.fromString('{}', 401);
    }
    return super.fetch(options, requestStream, cancelFuture);
  }
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

  test('logout revokes bearer and session before protected requests', () async {
    final storage = _MemoryTokenStorage();
    final requests = <String>[];
    final adapter = _AuthAdapter(requests);
    late DioClient testClient;
    final container = ProviderContainer(overrides: [
      tokenStorageProvider.overrideWithValue(storage),
      playerBaseUrlProvider
          .overrideWithValue(Uri.parse('https://player.example')),
      cookieJarProvider.overrideWith((ref) => testClient.cookieJar),
      apiClientProvider.overrideWith((ref) {
        final dio = testClient = DioClient(
          baseUrl: ref.watch(playerBaseUrlProvider),
          storage: ref.watch(tokenStorageProvider),
          mutationQueue: ref.watch(credentialMutationQueueProvider),
          navigatorKey: GlobalKey<NavigatorState>(),
        );
        dio.dio.httpClientAdapter = adapter;
        return DioPlayerApiClient(dio: dio.dio);
      }),
    ]);
    addTearDown(container.dispose);
    await container.read(authStateProvider.future);
    final user = await container
        .read(apiClientProvider)
        .login(username: 'alice', password: 'secret');
    await container.read(authStateProvider.notifier).login(user);
    expect(await container.read(apiClientProvider).listSets(), isEmpty);
    await expectLater(
        testClient.dio.get('/api/v1/sets',
            options: Options(headers: {'Authorization': ''})),
        completes);

    expect(await container.read(authStateProvider.notifier).logout(), isTrue);
    expect(adapter.revoked, isTrue);
    expect(adapter.lastRevocationAuthorization, 'Bearer pt-persisted');
    expect(adapter.sessionLoggedOut, isTrue);
    expect(storage.token, isNull);
    final prefs = await SharedPreferences.getInstance();
    expect(prefs.getInt('auth_token_id'), isNull);
    expect(
      container.read(apiClientProvider).listSets(),
      throwsA(isA<DioException>()
          .having((error) => error.response?.statusCode, 'status', 401)),
    );
    await expectLater(
      testClient.dio.get('/api/v1/sets',
          options: Options(headers: {'Authorization': ''})),
      throwsA(isA<DioException>()
          .having((error) => error.response?.statusCode, 'status', 401)),
    );
  });

  test('logout revokes a legacy bearer without a saved token ID', () async {
    SharedPreferences.setMockInitialValues({
      'auth_session_present': true,
      'auth_user': '{"id":1,"username":"alice","is_admin":false}',
      'auth_origin': 'https://player.example',
      'auth_expires_at':
          DateTime.now().add(const Duration(days: 1)).millisecondsSinceEpoch,
    });
    final storage = _MemoryTokenStorage()..token = 'pt-persisted';
    final adapter = _AuthAdapter([]);
    final container = ProviderContainer(overrides: [
      tokenStorageProvider.overrideWithValue(storage),
      playerBaseUrlProvider
          .overrideWithValue(Uri.parse('https://player.example')),
      apiClientProvider.overrideWith((ref) {
        final dio = DioClient(
          baseUrl: ref.watch(playerBaseUrlProvider),
          storage: ref.watch(tokenStorageProvider),
          mutationQueue: ref.watch(credentialMutationQueueProvider),
          navigatorKey: GlobalKey<NavigatorState>(),
        );
        dio.dio.httpClientAdapter = adapter;
        return DioPlayerApiClient(dio: dio.dio);
      }),
    ]);
    addTearDown(container.dispose);
    expect((await container.read(authStateProvider.future)).isAuthenticated,
        isTrue);
    expect(await container.read(apiClientProvider).listSets(), isEmpty);

    expect(await container.read(authStateProvider.notifier).logout(), isTrue);
    expect(adapter.currentTokenRevoked, isTrue);
    expect(storage.token, isNull);
    await expectLater(container.read(apiClientProvider).listSets(),
        throwsA(isA<DioException>()));
    expect(adapter.lastProtectedRequest?.headers['Authorization'], isNull);
  });

  test('failed revocation still clears local credentials and session',
      () async {
    final storage = _MemoryTokenStorage();
    final adapter = _AuthAdapter([])..failRevocation = true;
    late DioClient testClient;
    final container = ProviderContainer(overrides: [
      tokenStorageProvider.overrideWithValue(storage),
      playerBaseUrlProvider
          .overrideWithValue(Uri.parse('https://player.example')),
      cookieJarProvider.overrideWith((ref) => testClient.cookieJar),
      apiClientProvider.overrideWith((ref) {
        final dio = testClient = DioClient(
          baseUrl: ref.watch(playerBaseUrlProvider),
          storage: ref.watch(tokenStorageProvider),
          mutationQueue: ref.watch(credentialMutationQueueProvider),
          navigatorKey: GlobalKey<NavigatorState>(),
        );
        dio.dio.httpClientAdapter = adapter;
        return DioPlayerApiClient(dio: dio.dio);
      }),
    ]);
    addTearDown(container.dispose);
    await container.read(authStateProvider.future);
    final user = await container
        .read(apiClientProvider)
        .login(username: 'alice', password: 'secret');
    await container.read(authStateProvider.notifier).login(user);

    expect(await container.read(authStateProvider.notifier).logout(), isFalse);
    expect(adapter.lastRevocationAuthorization, 'Bearer pt-persisted');
    expect(adapter.sessionLoggedOut, isTrue);
    expect(storage.token, isNull);
    expect(container.read(authStateProvider).valueOrNull?.isUnauthenticated,
        isTrue);
    final prefs = await SharedPreferences.getInstance();
    expect(prefs.getInt('auth_token_id'), isNull);
    expect(prefs.getBool('auth_session_present'), isNull);
    await expectLater(
        container.read(apiClientProvider).listSets(),
        throwsA(isA<DioException>()
            .having((error) => error.response?.statusCode, 'status', 401)));
    expect(adapter.lastProtectedRequest?.headers['Authorization'], isNull);
  });

  test('logout clears locally before network and stale 401 preserves new login',
      () async {
    final storage = _MemoryTokenStorage();
    final adapter = _BlockingRevocationAdapter();
    late DioClient testClient;
    final container = ProviderContainer(overrides: [
      tokenStorageProvider.overrideWithValue(storage),
      playerBaseUrlProvider
          .overrideWithValue(Uri.parse('https://player.example')),
      cookieJarProvider.overrideWith((ref) => testClient.cookieJar),
      apiClientProvider.overrideWith((ref) {
        final dio = testClient = DioClient(
          baseUrl: ref.watch(playerBaseUrlProvider),
          storage: ref.watch(tokenStorageProvider),
          mutationQueue: ref.watch(credentialMutationQueueProvider),
          navigatorKey: GlobalKey<NavigatorState>(),
          onUnauthorized: () =>
              ref.read(authStateProvider.notifier).clearAfterUnauthorized(),
        );
        dio.dio.httpClientAdapter = adapter;
        return DioPlayerApiClient(dio: dio.dio);
      }),
    ]);
    addTearDown(container.dispose);
    await container.read(authStateProvider.future);
    final api = container.read(apiClientProvider);
    final firstUser = await api.login(username: 'alice', password: 'secret');
    await container.read(authStateProvider.notifier).login(firstUser);

    final logoutFuture = container.read(authStateProvider.notifier).logout();
    await adapter.revocationStarted.future;
    expect(storage.token, isNull);
    expect(container.read(authStateProvider).valueOrNull?.isUnauthenticated,
        isTrue);
    final prefs = await SharedPreferences.getInstance();
    expect(prefs.getBool('auth_session_present'), isNull);

    final secondUser = await api.login(username: 'alice', password: 'secret');
    await container.read(authStateProvider.notifier).login(secondUser);
    adapter.releaseRevocation.complete();
    expect(await logoutFuture, isFalse);
    expect(storage.token, 'pt-persisted-2');
    expect(
        container.read(authStateProvider).valueOrNull?.isAuthenticated, isTrue);
    final cookies = await testClient.cookieJar
        .loadForRequest(Uri.parse('https://player.example'));
    expect(cookies.single.value, 'temporary-session-2');
  });

  test('secure-storage read failure cannot skip local logout', () async {
    final storage = _MemoryTokenStorage();
    final adapter = _AuthAdapter([]);
    late DioClient testClient;
    final container = ProviderContainer(overrides: [
      tokenStorageProvider.overrideWithValue(storage),
      playerBaseUrlProvider
          .overrideWithValue(Uri.parse('https://player.example')),
      cookieJarProvider.overrideWith((ref) => testClient.cookieJar),
      apiClientProvider.overrideWith((ref) {
        final dio = testClient = DioClient(
          baseUrl: ref.watch(playerBaseUrlProvider),
          storage: ref.watch(tokenStorageProvider),
          mutationQueue: ref.watch(credentialMutationQueueProvider),
          navigatorKey: GlobalKey<NavigatorState>(),
        );
        dio.dio.httpClientAdapter = adapter;
        return DioPlayerApiClient(dio: dio.dio);
      }),
    ]);
    addTearDown(container.dispose);
    await container.read(authStateProvider.future);
    final user = await container
        .read(apiClientProvider)
        .login(username: 'alice', password: 'secret');
    await container.read(authStateProvider.notifier).login(user);
    storage.failNextRead = true;

    expect(await container.read(authStateProvider.notifier).logout(), isFalse);
    expect(storage.token, isNull);
    expect(container.read(authStateProvider).valueOrNull?.isUnauthenticated,
        isTrue);
  });

  test('secure-storage delete failure leaves no usable API credential',
      () async {
    final storage = _MemoryTokenStorage();
    final adapter = _AuthAdapter([])..failRevocation = true;
    final queue = CredentialMutationQueue();
    late DioClient testClient;
    final container = ProviderContainer(overrides: [
      tokenStorageProvider.overrideWithValue(storage),
      credentialMutationQueueProvider.overrideWithValue(queue),
      playerBaseUrlProvider
          .overrideWithValue(Uri.parse('https://player.example')),
      cookieJarProvider.overrideWith((ref) => testClient.cookieJar),
      apiClientProvider.overrideWith((ref) {
        final dio = testClient = DioClient(
          baseUrl: ref.watch(playerBaseUrlProvider),
          storage: storage,
          mutationQueue: queue,
          navigatorKey: GlobalKey<NavigatorState>(),
        );
        dio.dio.httpClientAdapter = adapter;
        return DioPlayerApiClient(dio: dio.dio);
      }),
    ]);
    addTearDown(container.dispose);
    await container.read(authStateProvider.future);
    final user = await container
        .read(apiClientProvider)
        .login(username: 'alice', password: 'secret');
    await container.read(authStateProvider.notifier).login(user);
    storage.failDelete = true;

    expect(await container.read(authStateProvider.notifier).logout(), isFalse);
    expect(storage.token, 'pt-persisted');
    expect(queue.credentialsEnabled, isFalse);
    expect(container.read(authStateProvider).valueOrNull?.isUnauthenticated,
        isTrue);
    await expectLater(container.read(apiClientProvider).listSets(),
        throwsA(isA<DioException>()));
    expect(adapter.lastProtectedRequest?.headers['Authorization'], isNull);
    expect(adapter.lastProtectedRequest?.headers['cookie'], isNull);
  });

  test('failed secure storage write revokes the freshly minted token',
      () async {
    final storage = _MemoryTokenStorage()..failWrite = true;
    final api = _DelayedMintApiClient();
    api.release.complete({'id': 7, 'token': 'pt-orphan'});
    final container = ProviderContainer(overrides: [
      tokenStorageProvider.overrideWithValue(storage),
      apiClientProvider.overrideWithValue(api),
    ]);
    addTearDown(container.dispose);
    await container.read(authStateProvider.future);

    const user = User(id: 1, username: 'alice', isAdmin: false);
    await expectLater(container.read(authStateProvider.notifier).login(user),
        throwsA(isA<StateError>()));
    expect(api.revokedId, 7);
    expect(api.revokedBearer, 'pt-orphan');
    expect(storage.token, isNull);
    expect(container.read(authStateProvider).valueOrNull?.isUnauthenticated,
        isTrue);
  });

  test('logout invoked during token mint wins and revokes stale mint',
      () async {
    final storage = _MemoryTokenStorage();
    final api = _DelayedMintApiClient();
    final container = ProviderContainer(overrides: [
      tokenStorageProvider.overrideWithValue(storage),
      apiClientProvider.overrideWithValue(api),
    ]);
    addTearDown(container.dispose);
    await container.read(authStateProvider.future);
    const user = User(id: 1, username: 'alice', isAdmin: false);
    final loginResult = container.read(authStateProvider.notifier).login(user);
    await api.started.future;
    final logoutResult = container.read(authStateProvider.notifier).logout();
    await logoutResult;
    api.release.complete({'id': 7, 'token': 'pt-stale'});
    await expectLater(loginResult, throwsA(isA<StateError>()));
    expect(api.revokedId, 7);
    expect(storage.token, isNull);
    expect(container.read(authStateProvider).valueOrNull?.isUnauthenticated,
        isTrue);
  });

  test('login invoked after logout keeps its new credential', () async {
    final storage = _MemoryTokenStorage();
    final api = _DelayedMintApiClient();
    api.release.complete({'id': 8, 'token': 'pt-new-login'});
    final container = ProviderContainer(overrides: [
      tokenStorageProvider.overrideWithValue(storage),
      apiClientProvider.overrideWithValue(api),
    ]);
    addTearDown(container.dispose);
    await container.read(authStateProvider.future);
    const user = User(id: 1, username: 'alice', isAdmin: false);

    final logout = container.read(authStateProvider.notifier).logout();
    final login = container.read(authStateProvider.notifier).login(user);
    await Future.wait([logout, login]);
    expect(storage.token, 'pt-new-login');
    expect(
        container.read(authStateProvider).valueOrNull?.isAuthenticated, isTrue);
  });

  test('logout during paused resume keeps progress sync suspended', () async {
    final storage = _MemoryTokenStorage();
    final queue = _PausingResumeQueue();
    final api = _DelayedMintApiClient();
    api.release.complete({'id': 7, 'token': 'pt-login'});
    final container = ProviderContainer(overrides: [
      tokenStorageProvider.overrideWithValue(storage),
      apiClientProvider.overrideWithValue(api),
      progressQueueProvider.overrideWithValue(queue),
    ]);
    addTearDown(container.dispose);
    await container.read(authStateProvider.future);
    container.read(progressQueueProvider);
    const user = User(id: 1, username: 'alice', isAdmin: false);
    final loginResult = container.read(authStateProvider.notifier).login(user);
    await queue.resumeStarted.future;
    final logoutResult = container.read(authStateProvider.notifier).logout();
    queue.releaseResume.complete();
    await expectLater(loginResult, throwsA(isA<StateError>()));
    await logoutResult;
    expect(queue.suspended, isTrue);
    expect(storage.token, isNull);
    expect(container.read(authStateProvider).valueOrNull?.isUnauthenticated,
        isTrue);
  });

  test('pending restore cannot re-enable bearer after logout starts', () async {
    SharedPreferences.setMockInitialValues({
      'auth_session_present': true,
      'auth_user': '{"id":1,"username":"alice","is_admin":false}',
      'auth_origin': 'https://player.example',
      'auth_token_id': 7,
      'auth_expires_at':
          DateTime.now().add(const Duration(days: 1)).millisecondsSinceEpoch,
    });
    final storage = _RestorePausingStorage();
    final queue = CredentialMutationQueue();
    final api = _DelayedMintApiClient();
    final container = ProviderContainer(overrides: [
      tokenStorageProvider.overrideWithValue(storage),
      credentialMutationQueueProvider.overrideWithValue(queue),
      apiClientProvider.overrideWithValue(api),
      playerBaseUrlProvider
          .overrideWithValue(Uri.parse('https://player.example')),
    ]);
    addTearDown(container.dispose);
    final restore = container.read(authStateProvider.future);
    await storage.readStarted.future;
    final logout = container.read(authStateProvider.notifier).logout();
    storage.releaseRead.complete();
    expect((await restore).isUnauthenticated, isTrue);
    await logout;
    expect(queue.credentialsEnabled, isFalse);
    expect(api.revokedId, 7);
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

  test('failed secure-token cleanup on cold start still opens login state',
      () async {
    SharedPreferences.setMockInitialValues({
      'auth_session_present': true,
      'auth_user': '{"id":1,"username":"alice","is_admin":false}',
      'auth_origin': 'https://player.example',
      'auth_expires_at': DateTime.now()
          .subtract(const Duration(seconds: 1))
          .millisecondsSinceEpoch,
    });
    final storage = _MemoryTokenStorage()
      ..token = 'pt-expired'
      ..failDelete = true;
    final queue = CredentialMutationQueue();
    final container = ProviderContainer(overrides: [
      tokenStorageProvider.overrideWithValue(storage),
      credentialMutationQueueProvider.overrideWithValue(queue),
      playerBaseUrlProvider
          .overrideWithValue(Uri.parse('https://player.example')),
    ]);
    addTearDown(container.dispose);
    expect((await container.read(authStateProvider.future)).isUnauthenticated,
        isTrue);
    expect(queue.credentialsEnabled, isFalse);
  });

  test('401 cleanup completes when secure-token deletion fails', () async {
    SharedPreferences.setMockInitialValues({
      'auth_session_present': true,
      'auth_user': '{"id":1,"username":"alice","is_admin":false}',
      'auth_origin': 'https://player.example',
      'auth_expires_at':
          DateTime.now().add(const Duration(days: 1)).millisecondsSinceEpoch,
    });
    final storage = _MemoryTokenStorage()..token = 'pt-current';
    final queue = CredentialMutationQueue();
    final progress = _PausingResumeQueue();
    final adapter = _DelayedUnauthorizedAdapter();
    late DioClient dio;
    final container = ProviderContainer(overrides: [
      tokenStorageProvider.overrideWithValue(storage),
      credentialMutationQueueProvider.overrideWithValue(queue),
      progressQueueProvider.overrideWithValue(progress),
      playerBaseUrlProvider
          .overrideWithValue(Uri.parse('https://player.example')),
      cookieJarProvider.overrideWith((ref) => dio.cookieJar),
      apiClientProvider.overrideWith((ref) {
        dio = DioClient(
          baseUrl: ref.read(playerBaseUrlProvider),
          storage: storage,
          mutationQueue: queue,
          navigatorKey: GlobalKey<NavigatorState>(),
          onUnauthorized: () async {
            await ref
                .read(authStateProvider.notifier)
                .clearAfterUnauthorized(advanceGeneration: false);
          },
        );
        dio.dio.httpClientAdapter = adapter;
        return DioPlayerApiClient(dio: dio.dio);
      }),
    ]);
    addTearDown(container.dispose);
    expect((await container.read(authStateProvider.future)).isAuthenticated,
        isTrue);
    container.read(apiClientProvider);
    await container.read(cookieJarProvider).saveFromResponse(
        Uri.parse('https://player.example/api/v1/sets'),
        [Cookie('session', 'old-session')]);
    storage.failDelete = true;
    final request = expectLater(container.read(apiClientProvider).listSets(),
        throwsA(isA<DioException>()));
    await adapter.received.future;
    adapter.response.complete(ResponseBody.fromString('{}', 401));
    await request;
    expect(queue.credentialsEnabled, isFalse);
    expect(storage.token, 'pt-current');
    expect(progress.suspended, isTrue);
    expect(container.read(authStateProvider).valueOrNull?.isUnauthenticated,
        isTrue);
    expect(
        await container
            .read(cookieJarProvider)
            .loadForRequest(Uri.parse('https://player.example/api/v1/sets')),
        isEmpty);
  });

  test('a stale 401 does not clear a newer login credential', () async {
    final storage = _MemoryTokenStorage()..token = 'pt-old';
    final queue = CredentialMutationQueue();
    queue.enable(queue.generation);
    final adapter = _DelayedUnauthorizedAdapter();
    var logoutCalls = 0;
    final client = DioClient(
      baseUrl: Uri.parse('https://player.example'),
      storage: storage,
      mutationQueue: queue,
      navigatorKey: GlobalKey<NavigatorState>(),
      onUnauthorized: () async {
        await storage.deleteToken();
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

  test('old progress request cannot attach a newer account token', () async {
    final storage = _PausingTokenStorage();
    await storage.readToken(); // The next read pauses inside the interceptor.
    final queue = CredentialMutationQueue();
    queue.enable(queue.generation);
    final adapter = _CaptureAdapter();
    final dio = DioClient(
      baseUrl: Uri.parse('https://player.example'),
      storage: storage,
      mutationQueue: queue,
      navigatorKey: GlobalKey<NavigatorState>(),
    );
    dio.dio.httpClientAdapter = adapter;
    final api = DioPlayerApiClient(
      dio: dio.dio,
      credentialEpoch: () => queue.generation,
    );
    final request = api.batchUpdateProgress([
      {
        'media_id': 1,
        'position_seconds': 1.0,
        'observed_at': DateTime.now().toUtc().toIso8601String(),
      }
    ]);
    await storage.checked.future;
    final newGeneration = queue.beginAuthChange();
    await storage.writeToken('pt-new-account');
    queue.enable(newGeneration);
    storage.proceed.complete();
    await expectLater(
        request,
        throwsA(isA<DioException>()
            .having((error) => error.type, 'type', DioExceptionType.cancel)));
    expect(adapter.captured, isNull);
  });

  test('native media headers discard a token read across logout', () async {
    final storage = _PausingTokenStorage();
    await storage.readToken();
    final gate = CredentialMutationQueue(credentialsEnabled: true);
    final jar = CookieJar();
    final uri = Uri.parse('https://player.example/api/v1/media/1/stream');
    await jar.saveFromResponse(uri, [Cookie('session', 'old-session')]);
    final headers = accountRequestHeaders(
      uri: uri,
      baseUrl: Uri.parse('https://player.example'),
      storage: storage,
      cookieJar: jar,
      mutations: gate,
    );
    await storage.checked.future;
    gate.beginAuthChange();
    storage.proceed.complete();
    expect(await headers, isEmpty);
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
        await storage.deleteToken();
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
    queue.enable(queue.generation);
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
