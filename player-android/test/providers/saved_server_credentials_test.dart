import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';
import 'package:dio/dio.dart';
import 'package:cookie_jar/cookie_jar.dart';
import 'package:flutter/material.dart';
import 'package:player_android/api/dio_player_api_client.dart';

import 'package:crypto/crypto.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/models/user.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/settings_provider.dart';
import 'package:player_android/services/saved_server_credentials.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _TokenStore implements TokenStorage {
  String? token;
  @override
  Future<String?> readToken() async => token;
  @override
  Future<void> writeToken(String value) async => token = value;
  @override
  Future<void> deleteToken() async => token = null;
}

class _Biometrics implements ServerBiometricUnlock {
  bool accepted = false;
  Completer<bool>? pending;
  final started = Completer<void>();
  @override
  Future<bool> authenticate(String origin) async {
    if (!started.isCompleted) started.complete();
    if (pending != null) return await pending!.future;
    return accepted;
  }
}

class _Requests implements HttpClientAdapter {
  RequestOptions? seen;
  int status = 200;
  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future? cancelFuture,
  ) async {
    seen = options;
    return ResponseBody.fromString('{}', status);
  }

  @override
  void close({bool force = false}) {}
}

class _PausingMintApi extends DioPlayerApiClient {
  _PausingMintApi({required super.dio, required super.credentialEpoch});
  final mintStarted = Completer<void>();
  final releaseMint = Completer<void>();
  @override
  Future<Map<String, dynamic>> createAPIToken(
      {required String name, int? expiresInDays}) async {
    mintStarted.complete();
    await releaseMint.future;
    return super.createAPIToken(name: name, expiresInDays: expiresInDays);
  }
}

class _DelayedLoginRequests implements HttpClientAdapter {
  final aliceStarted = Completer<void>();
  final aliceResponse = Completer<ResponseBody>();
  String? mintedForCookie;
  @override
  Future<ResponseBody> fetch(RequestOptions options,
      Stream<Uint8List>? requestStream, Future? cancelFuture) async {
    final headers = <String, List<String>>{
      Headers.contentTypeHeader: ['application/json']
    };
    if (options.uri.path.endsWith('/auth/login')) {
      if ((options.data as Map)['username'] == 'alice') {
        aliceStarted.complete();
        return aliceResponse.future;
      }
      headers['set-cookie'] = ['session=bob-session; Path=/; HttpOnly'];
      return ResponseBody.fromString(
          jsonEncode({'id': 2, 'username': 'bob', 'is_admin': false}), 200,
          headers: headers);
    }
    if (options.uri.path.endsWith('/auth/tokens') && options.method == 'POST') {
      mintedForCookie = options.headers['cookie']?.toString();
      final isBob = mintedForCookie?.contains('bob-session') == true;
      return ResponseBody.fromString(
          jsonEncode({
            'id': isBob ? 8 : 7,
            'token': isBob ? 'token-bob' : 'token-alice'
          }),
          200,
          headers: headers);
    }
    return ResponseBody.fromString('{}', 200, headers: headers);
  }

  @override
  void close({bool force = false}) {}
}

class _PausingSaveJar implements CookieJar {
  final _delegate = CookieJar();
  final saveStarted = Completer<void>();
  final releaseSave = Completer<void>();
  @override
  bool get ignoreExpires => _delegate.ignoreExpires;
  @override
  Future<List<Cookie>> loadForRequest(Uri uri) => _delegate.loadForRequest(uri);
  @override
  Future<void> saveFromResponse(Uri uri, List<Cookie> cookies) async {
    if (cookies.any((cookie) => cookie.value == 'alice-session')) {
      saveStarted.complete();
      await releaseSave.future;
    }
    await _delegate.saveFromResponse(uri, cookies);
  }

  @override
  Future<void> delete(Uri uri, [bool withDomainSharedCookie = false]) =>
      _delegate.delete(uri, withDomainSharedCookie);
  @override
  Future<void> deleteAll() => _delegate.deleteAll();
}

class _ExpiringCredential extends SavedServerCredential {
  _ExpiringCredential()
      : super(
            origin: 'https://a.example',
            token: 'token-a',
            tokenId: 7,
            user: const User(id: 1, username: 'alice', isAdmin: false),
            expiresAt: 1);
  bool valid = true;
  @override
  bool get isValid => valid;
}

class _MemoryVault implements SavedServerCredentials {
  final entries = <String, SavedServerCredential>{};
  @override
  Future<SavedServerCredential?> read(String origin) async => entries[origin];
  @override
  Future<void> write(SavedServerCredential credential) async =>
      entries[credential.origin] = credential;
  @override
  Future<void> delete(String origin) async => entries.remove(origin);
}

SavedServerCredential _credential(String origin, String token) =>
    SavedServerCredential(
      origin: origin,
      token: token,
      tokenId: 7,
      user: const User(id: 1, username: 'alice', isAdmin: false),
      expiresAt:
          DateTime.now().add(const Duration(days: 1)).millisecondsSinceEpoch,
    );

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  setUp(() {
    SharedPreferences.setMockInitialValues({});
    FlutterSecureStorage.setMockInitialValues({});
  });

  test(
    'saved server list canonicalizes origins and survives restarts',
    () async {
      final container = ProviderContainer();
      await container.read(settingsProvider.future);
      final settings = container.read(settingsProvider.notifier);
      await settings.setServerBaseUrl(' HTTPS://EXAMPLE.COM:443/ ');
      await settings.setServerBaseUrl('https://example.com');
      await settings.setServerBaseUrl('https://example.com:8443');
      expect(container.read(settingsProvider).requireValue.savedServers, [
        'https://example.com',
        'https://example.com:8443',
      ]);
      container.dispose();
      final restarted = ProviderContainer();
      addTearDown(restarted.dispose);
      expect(
        (await restarted.read(settingsProvider.future)).selectionRequired,
        isTrue,
      );
      await restarted
          .read(settingsProvider.notifier)
          .selectDestination(LibraryDestination.local);
      expect(
        restarted.read(settingsProvider).requireValue.selectionRequired,
        isFalse,
      );
      expect(
        restarted.read(settingsProvider).requireValue.savedServers,
        hasLength(2),
      );
    },
  );

  test(
    'origin vault isolates scheme and port and does not store secrets in preferences',
    () async {
      final vault = SecureSavedServerCredentials();
      await vault.write(_credential('https://example.com', 'token-a'));
      await vault.write(_credential('https://example.com:8443', 'token-b'));
      expect((await vault.read('https://EXAMPLE.COM:443/'))?.token, 'token-a');
      expect((await vault.read('https://example.com:8443'))?.token, 'token-b');
      expect(await vault.read('http://example.com'), isNull);
      await vault.delete('https://example.com');
      expect(await vault.read('https://example.com'), isNull);
      expect((await vault.read('https://example.com:8443'))?.token, 'token-b');
      expect((await SharedPreferences.getInstance()).getKeys(), isEmpty);
    },
  );

  test(
    'corrupt, mismatched and expired secure records cannot unlock',
    () async {
      final vault = SecureSavedServerCredentials();
      const origin = 'https://example.com';
      final key = 'server_credential_${sha256.convert(utf8.encode(origin))}';
      const storage = FlutterSecureStorage();
      for (final record in [
        'invalid json',
        jsonEncode({
          ..._credential(origin, 'token').toJson(),
          'origin': 'https://other.example',
        }),
        jsonEncode({..._credential(origin, 'token').toJson(), 'expiresAt': 1}),
        jsonEncode({..._credential(origin, 'token').toJson(), 'token': 42}),
      ]) {
        await storage.write(key: key, value: record);
        expect(await vault.read(origin), isNull);
      }
    },
  );

  test(
    'cancelled biometric prompt leaves credentials disabled; retry restores selected server only',
    () async {
      final storage = _TokenStore();
      final biometric = _Biometrics();
      final container = ProviderContainer(
        overrides: [
          tokenStorageProvider.overrideWithValue(storage),
          serverBiometricUnlockProvider.overrideWithValue(biometric),
        ],
      );
      addTearDown(container.dispose);
      final vault = container.read(savedServerCredentialsProvider);
      await vault.write(_credential('https://a.example', 'token-a'));
      await vault.write(_credential('https://b.example', 'token-b'));
      final auth = container.read(authStateProvider.notifier);
      await auth.switchServer('https://a.example');
      expect(await auth.unlockSavedServer(), isFalse);
      expect(storage.token, isNull);
      expect(
        container.read(credentialMutationQueueProvider).credentialsEnabled,
        isFalse,
      );
      biometric.accepted = true;
      expect(await auth.unlockSavedServer(), isTrue);
      expect(storage.token, 'token-a');
      await auth.switchServer('https://b.example');
      expect(storage.token, isNull);
      expect((await vault.read('https://a.example'))?.token, 'token-a');
      expect(await auth.unlockSavedServer(), isTrue);
      expect(storage.token, 'token-b');
    },
  );

  test(
    'retained client cannot send a new server token or invalidate its login',
    () async {
      final storage = _TokenStore();
      final biometric = _Biometrics()..accepted = true;
      final container = ProviderContainer(
        overrides: [
          tokenStorageProvider.overrideWithValue(storage),
          serverBiometricUnlockProvider.overrideWithValue(biometric),
        ],
      );
      addTearDown(container.dispose);
      final vault = container.read(savedServerCredentialsProvider);
      await vault.write(_credential('https://a.example', 'token-a'));
      await vault.write(_credential('https://b.example', 'token-b'));
      final auth = container.read(authStateProvider.notifier);
      await auth.switchServer('https://a.example');
      await auth.unlockSavedServer();
      final oldClient = container.read(apiClientProvider);
      final requests = _Requests();
      oldClient.rawDio.httpClientAdapter = requests;
      await oldClient.rawDio.get<void>('/healthz');
      expect(requests.seen!.headers['Authorization'], 'Bearer token-a');
      await auth.switchServer('https://b.example');
      await auth.unlockSavedServer();
      requests.status = 401;
      await expectLater(
        oldClient.rawDio.get<void>('/healthz'),
        throwsA(isA<DioException>()),
      );
      expect(requests.seen!.headers['Authorization'], isNull);
      expect(requests.seen!.headers['cookie'], isNull);
      expect(storage.token, 'token-b');
      expect(
        container.read(authStateProvider).requireValue.isAuthenticated,
        isTrue,
      );
      expect(
        container.read(credentialMutationQueueProvider).credentialOrigin,
        'https://b.example',
      );
      expect((await vault.read('https://b.example'))?.token, 'token-b');
    },
  );

  test('delayed biometric success cannot unlock a different server', () async {
    final storage = _TokenStore();
    final biometric = _Biometrics()..pending = Completer<bool>();
    final container = ProviderContainer(
      overrides: [
        tokenStorageProvider.overrideWithValue(storage),
        serverBiometricUnlockProvider.overrideWithValue(biometric),
      ],
    );
    addTearDown(container.dispose);
    await container
        .read(savedServerCredentialsProvider)
        .write(_credential('https://a.example', 'token-a'));
    final auth = container.read(authStateProvider.notifier);
    await auth.switchServer('https://a.example');
    final unlock = auth.unlockSavedServer();
    await biometric.started.future;
    await auth.switchServer('https://b.example');
    biometric.pending!.complete(true);
    expect(await unlock, isFalse);
    expect(storage.token, isNull);
    expect(
      container.read(authStateProvider).requireValue.isUnauthenticated,
      isTrue,
    );
    expect(
      container.read(credentialMutationQueueProvider).credentialsEnabled,
      isFalse,
    );
  });

  for (final delayedStatus in [200, 401]) {
    test(
        'same-origin reselect rejects stale login cookies/identity ($delayedStatus) before minting Bob token',
        () async {
      final storage = _TokenStore();
      final requests = _DelayedLoginRequests();
      late DioClient dioClient;
      final container = ProviderContainer(overrides: [
        tokenStorageProvider.overrideWithValue(storage),
        cookieJarProvider.overrideWith((ref) {
          ref.watch(apiClientProvider);
          return dioClient.cookieJar;
        }),
        apiClientProvider.overrideWith((ref) {
          dioClient = DioClient(
              baseUrl: ref.watch(playerBaseUrlProvider),
              storage: storage,
              mutationQueue: ref.watch(credentialMutationQueueProvider),
              navigatorKey: GlobalKey<NavigatorState>());
          dioClient.dio.httpClientAdapter = requests;
          return _PausingMintApi(
              dio: dioClient.dio,
              credentialEpoch: () =>
                  ref.read(credentialMutationQueueProvider).generation);
        }),
      ]);
      addTearDown(container.dispose);
      final auth = container.read(authStateProvider.notifier);
      await auth.switchServer('https://a.example');
      final api = container.read(apiClientProvider) as _PausingMintApi;
      final oldLogin = expectLater(
          api.login(username: 'alice', password: 'old'),
          throwsA(isA<DioException>().having(
              (e) => e.type,
              'type',
              delayedStatus == 200
                  ? DioExceptionType.cancel
                  : DioExceptionType.badResponse)));
      await requests.aliceStarted.future;
      await auth.switchServer('https://a.example');
      expect(container.read(apiClientProvider), same(api));
      final bob = await api.login(username: 'bob', password: 'new');
      final persistBob = auth.login(bob);
      await api.mintStarted.future;
      requests.aliceResponse.complete(ResponseBody.fromString(
          jsonEncode({'id': 1, 'username': 'alice', 'is_admin': true}),
          delayedStatus,
          headers: {
            Headers.contentTypeHeader: ['application/json'],
            'set-cookie': ['session=alice-session; Path=/; HttpOnly']
          }));
      await oldLogin;
      api.releaseMint.complete();
      await persistBob;
      expect(requests.mintedForCookie, contains('bob-session'));
      expect(storage.token, 'token-bob');
      expect(
          container.read(authStateProvider).requireValue.user?.username, 'bob');
      final saved = await container
          .read(savedServerCredentialsProvider)
          .read('https://a.example');
      expect(saved?.token, 'token-bob');
      expect(saved?.user.username, 'bob');
    });
  }

  test(
      'generation change during cookie save clears stale cookie before a new login can read it',
      () async {
    final storage = _TokenStore();
    final requests = _DelayedLoginRequests();
    final jar = _PausingSaveJar();
    late DioClient dioClient;
    final container = ProviderContainer(overrides: [
      tokenStorageProvider.overrideWithValue(storage),
      cookieJarProvider.overrideWith((ref) {
        ref.watch(apiClientProvider);
        return dioClient.cookieJar;
      }),
      apiClientProvider.overrideWith((ref) {
        dioClient = DioClient(
            baseUrl: ref.watch(playerBaseUrlProvider),
            storage: storage,
            mutationQueue: ref.watch(credentialMutationQueueProvider),
            cookieJar: jar,
            navigatorKey: GlobalKey<NavigatorState>());
        dioClient.dio.httpClientAdapter = requests;
        return _PausingMintApi(
            dio: dioClient.dio,
            credentialEpoch: () =>
                ref.read(credentialMutationQueueProvider).generation);
      }),
    ]);
    addTearDown(container.dispose);
    final auth = container.read(authStateProvider.notifier);
    await auth.switchServer('https://a.example');
    final api = container.read(apiClientProvider) as _PausingMintApi;
    final oldLogin = expectLater(
        api.login(username: 'alice', password: 'old'),
        throwsA(isA<DioException>()
            .having((e) => e.type, 'type', DioExceptionType.cancel)));
    await requests.aliceStarted.future;
    requests.aliceResponse.complete(ResponseBody.fromString(
        jsonEncode({'id': 1, 'username': 'alice', 'is_admin': true}), 200,
        headers: {
          Headers.contentTypeHeader: ['application/json'],
          'set-cookie': ['session=alice-session; Path=/; HttpOnly']
        }));
    await jar.saveStarted.future;
    await auth.switchServer('https://a.example');
    final newLogin = api.login(username: 'bob', password: 'new');
    jar.releaseSave.complete();
    await oldLogin;
    final bob = await newLogin;
    api.releaseMint.complete();
    await auth.login(bob);
    expect(requests.mintedForCookie, contains('bob-session'));
    expect(storage.token, 'token-bob');
    expect(
        container.read(authStateProvider).requireValue.user?.username, 'bob');
    expect(
        (await jar.loadForRequest(Uri.parse('https://a.example'))).single.value,
        'bob-session');
  });

  test('expiry during an outstanding biometric prompt leaves login locked',
      () async {
    final credential = _ExpiringCredential();
    final vault = _MemoryVault()..entries[credential.origin] = credential;
    final storage = _TokenStore();
    final biometric = _Biometrics()..pending = Completer<bool>();
    final container = ProviderContainer(overrides: [
      tokenStorageProvider.overrideWithValue(storage),
      savedServerCredentialsProvider.overrideWithValue(vault),
      serverBiometricUnlockProvider.overrideWithValue(biometric),
    ]);
    addTearDown(container.dispose);
    final auth = container.read(authStateProvider.notifier);
    await auth.switchServer(credential.origin);
    final unlock = auth.unlockSavedServer();
    await biometric.started.future;
    credential.valid = false;
    biometric.pending!.complete(true);
    expect(await unlock, isFalse);
    expect(storage.token, isNull);
    expect(container.read(credentialMutationQueueProvider).credentialsEnabled,
        isFalse);
    expect(container.read(authStateProvider).requireValue.isUnauthenticated,
        isTrue);
  });

  test('invalid ports are rejected', () {
    for (final origin in [
      'https://example.com:0',
      'https://example.com:65536',
    ]) {
      expect(() => parseServerBaseUrl(origin), throwsFormatException);
    }
  });
}
