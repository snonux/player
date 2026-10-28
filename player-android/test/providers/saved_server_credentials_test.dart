import 'package:player_android/models/server_endpoint_profile.dart';
import 'package:player_android/services/server_endpoint_router.dart';
import 'dart:io';
import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';
import 'package:player_android/providers/progress_queue_provider.dart';
import 'package:player_android/services/progress_queue.dart';
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
import 'package:player_android/providers/saved_account_validation_provider.dart';
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

class _Connected implements Connectivity {
  @override
  Stream<List<ConnectivityResult>> get onConnectivityChanged =>
      const Stream.empty();
  @override
  Future<List<ConnectivityResult>> checkConnectivity() async =>
      [ConnectivityResult.wifi];
}

class _Requests implements HttpClientAdapter {
  RequestOptions? seen;
  final requests = <RequestOptions>[];
  int status = 200;
  String body = '{}';
  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future? cancelFuture,
  ) async {
    seen = options;
    requests.add(options);
    return ResponseBody.fromString(body, status, headers: {
      Headers.contentTypeHeader: ['application/json']
    });
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
          savedAccountValidationProvider.overrideWithValue((_) async => true),
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
          savedAccountValidationProvider.overrideWithValue((_) async => true),
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
        savedAccountValidationProvider.overrideWithValue((_) async => true),
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
        savedAccountValidationProvider.overrideWithValue((_) async => true),
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
      savedAccountValidationProvider.overrideWithValue((_) async => true),
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
      savedAccountValidationProvider.overrideWithValue((_) async => true),
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

  test('expiry during server validation cannot grant an offline unlock',
      () async {
    final credential = _ExpiringCredential();
    final validationStarted = Completer<void>();
    final validation = Completer<bool>();
    final vault = _MemoryVault()..entries[credential.origin] = credential;
    final container = ProviderContainer(overrides: [
      tokenStorageProvider.overrideWithValue(_TokenStore()),
      savedServerCredentialsProvider.overrideWithValue(vault),
      serverBiometricUnlockProvider
          .overrideWithValue(_Biometrics()..accepted = true),
      savedAccountValidationProvider.overrideWithValue((_) async {
        validationStarted.complete();
        return validation.future;
      }),
    ]);
    addTearDown(container.dispose);
    final auth = container.read(authStateProvider.notifier);
    await auth.switchServer(credential.origin);
    final unlock = auth.unlockSavedServer();
    await validationStarted.future;
    credential.valid = false;
    validation.complete(false);
    expect(await unlock, isFalse);
    expect(container.read(authStateProvider).requireValue.isUnauthenticated,
        isTrue);
    expect(container.read(credentialMutationQueueProvider).credentialsEnabled,
        isFalse);
  });

  for (final failure in [
    'socket',
    'identity',
    'certificate',
    'wrapped_certificate',
    'malformed',
    'unexpected'
  ]) {
    test(
        'real paired endpoint adapter only unlocks offline for transport outage: $failure',
        () async {
      const profile = ServerEndpointProfile(
          primary: 'https://a.example',
          local: 'http://192.168.1.2:8080',
          serverId:
              'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa');
      final attempts = <Uri>[];
      final requests = _Requests();
      final router = ServerEndpointRouter(
          primary: Uri.parse(profile.primary),
          profile: profile,
          probe: (uri) async {
            attempts.add(uri);
            switch (failure) {
              case 'socket':
                throw const SocketException('No route to endpoint');
              case 'identity':
                return 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb';
              case 'certificate':
                throw DioException(
                    requestOptions: RequestOptions(path: uri.toString()),
                    type: DioExceptionType.badCertificate,
                    error: const HandshakeException('Untrusted certificate'));
              case 'wrapped_certificate':
                throw DioException(
                    requestOptions: RequestOptions(path: uri.toString()),
                    type: DioExceptionType.connectionError,
                    error: const HandshakeException('Untrusted certificate'));
              case 'malformed':
                throw const FormatException('Malformed server identity');
              default:
                throw StateError('Unexpected probe failure');
            }
          });
      addTearDown(router.close);
      final container = ProviderContainer(overrides: [
        tokenStorageProvider.overrideWithValue(_TokenStore()),
        serverBiometricUnlockProvider
            .overrideWithValue(_Biometrics()..accepted = true),
        savedAccountValidationTransportProvider.overrideWithValue(
            () => EndpointHttpAdapter(router, delegate: requests)),
      ]);
      addTearDown(container.dispose);
      final auth = container.read(authStateProvider.notifier);
      await auth.switchServer(profile.primary);
      await container
          .read(savedServerCredentialsProvider)
          .write(_credential(profile.primary, 'token-a'));
      if (failure == 'socket') {
        expect(await auth.unlockSavedServer(), isTrue);
        expect(
            container.read(authStateProvider).requireValue.isOffline, isTrue);
        expect(attempts.map((uri) => uri.origin),
            [profile.local, profile.primary]);
      } else {
        await expectLater(
            auth.unlockSavedServer(), throwsA(isA<DioException>()));
        expect(container.read(authStateProvider).requireValue.isUnauthenticated,
            isTrue);
        expect(attempts, hasLength(1));
      }
      expect(requests.requests, isEmpty);
      expect(container.read(credentialMutationQueueProvider).credentialsEnabled,
          isFalse);
    });
  }

  for (final status in [200, 401, 403, 500, 502, 503, 504]) {
    test(
        'saved bearer probe distinguishes gateway outage from rejection ($status)',
        () async {
      final requests = _Requests()..status = status;
      final container = ProviderContainer(overrides: [
        savedAccountValidationTransportProvider
            .overrideWithValue(() => requests),
      ]);
      addTearDown(container.dispose);
      // A response with the right token ID verifies the persisted bearer.
      requests.body = '[{"id":7}]';
      final validate = container.read(savedAccountValidationProvider);
      final result = validate(_credential('https://a.example', 'token-a'));
      if (status == 200 || [502, 503, 504].contains(status)) {
        expect(await result, status == 200);
      } else {
        await expectLater(result, throwsA(isA<DioException>()));
      }
      expect(requests.seen!.uri.toString(),
          'https://a.example/api/v1/auth/tokens');
      expect(requests.seen!.headers['Authorization'], 'Bearer token-a');
      expect(requests.seen!.headers['cookie'], isNull);
      expect(requests.seen!.followRedirects, isFalse);
    });
  }

  for (final status in [401, 403, 503]) {
    test(
        'fingerprint with server response $status only unlocks during an outage',
        () async {
      final requests = _Requests()..status = status;
      final container = ProviderContainer(overrides: [
        tokenStorageProvider.overrideWithValue(_TokenStore()),
        serverBiometricUnlockProvider
            .overrideWithValue(_Biometrics()..accepted = true),
        savedAccountValidationTransportProvider
            .overrideWithValue(() => requests),
      ]);
      addTearDown(container.dispose);
      final auth = container.read(authStateProvider.notifier);
      await auth.switchServer('https://a.example');
      await container
          .read(savedServerCredentialsProvider)
          .write(_credential('https://a.example', 'token-a'));
      if (status == 503) {
        expect(await auth.unlockSavedServer(), isTrue);
        expect(
            container.read(authStateProvider).requireValue.isOffline, isTrue);
      } else {
        await expectLater(
            auth.unlockSavedServer(), throwsA(isA<DioException>()));
        expect(container.read(authStateProvider).requireValue.isUnauthenticated,
            isTrue);
      }
      expect(container.read(credentialMutationQueueProvider).credentialsEnabled,
          isFalse);
    });
  }

  test('delayed reconnect cannot enable credentials after a server switch',
      () async {
    Completer<bool>? reconnect;
    final container = ProviderContainer(overrides: [
      tokenStorageProvider.overrideWithValue(_TokenStore()),
      serverBiometricUnlockProvider
          .overrideWithValue(_Biometrics()..accepted = true),
      savedAccountValidationProvider.overrideWithValue((_) async {
        final pending = reconnect;
        return pending == null ? false : await pending.future;
      }),
    ]);
    addTearDown(container.dispose);
    final auth = container.read(authStateProvider.notifier);
    await auth.switchServer('https://a.example');
    await container
        .read(savedServerCredentialsProvider)
        .write(_credential('https://a.example', 'token-a'));
    await auth.unlockSavedServer();
    reconnect = Completer<bool>();
    final validating = auth.reconnectSavedAccount();
    await Future<void>.delayed(Duration.zero);
    await auth.switchServer('https://b.example');
    reconnect.complete(true);
    expect(await validating, isFalse);
    expect(container.read(authStateProvider).requireValue.isUnauthenticated,
        isTrue);
    expect(container.read(credentialMutationQueueProvider).credentialsEnabled,
        isFalse);
  });

  test(
      'offline fingerprint unlock keeps API gate closed until same account validates',
      () async {
    var online = false;
    final storage = _TokenStore();
    final container = ProviderContainer(overrides: [
      tokenStorageProvider.overrideWithValue(storage),
      serverBiometricUnlockProvider
          .overrideWithValue(_Biometrics()..accepted = true),
      savedAccountValidationProvider.overrideWithValue((_) async => online),
    ]);
    addTearDown(container.dispose);
    final auth = container.read(authStateProvider.notifier);
    await auth.switchServer('https://a.example');
    await container
        .read(savedServerCredentialsProvider)
        .write(_credential('https://a.example', 'token-a'));
    expect(await auth.unlockSavedServer(), isTrue);
    expect(container.read(authStateProvider).requireValue.isOffline, isTrue);
    final gate = container.read(credentialMutationQueueProvider);
    expect(gate.credentialsEnabled, isFalse);
    final api = container.read(apiClientProvider);
    final requests = _Requests();
    api.rawDio.httpClientAdapter = requests;
    await expectLater(api.rawDio.post<void>('/api/v1/admin/rescan'),
        throwsA(isA<DioException>()));
    expect(requests.seen, isNull);
    expect(await auth.reconnectSavedAccount(), isFalse);
    expect(gate.credentialsEnabled, isFalse);
    online = true;
    expect(await auth.reconnectSavedAccount(), isTrue);
    expect(container.read(authStateProvider).requireValue.isOffline, isFalse);
    expect(gate.credentialsEnabled, isTrue);
    await api.rawDio.get<void>('/healthz');
    expect(requests.seen!.headers['Authorization'], 'Bearer token-a');
  });

  test(
      'offline queued cache progress survives logout/restart and flushes only after account validation',
      () async {
    sqfliteFfiInit();
    databaseFactory = databaseFactoryFfiNoIsolate;
    final directory =
        await Directory.systemTemp.createTemp('offline-unlock-progress-');
    final path = '${directory.path}/progress.db';
    const scope = ProgressScope(origin: 'https://a.example', userId: 1);
    var online = false;
    var denied = false;
    for (final restart in [false, true]) {
      final storage = _TokenStore();
      final requests = _Requests();
      final container = ProviderContainer(overrides: [
        tokenStorageProvider.overrideWithValue(storage),
        serverBiometricUnlockProvider
            .overrideWithValue(_Biometrics()..accepted = true),
        savedAccountValidationProvider.overrideWithValue((_) async {
          if (denied) {
            final options = RequestOptions(path: '/api/v1/auth/tokens');
            throw DioException(
                requestOptions: options,
                type: DioExceptionType.badResponse,
                response: Response(requestOptions: options, statusCode: 401));
          }
          return online;
        }),
        progressQueueProvider.overrideWith((ref) => ProgressQueue(
            apiClient: ref.watch(apiClientProvider),
            databasePath: path,
            connectivity: _Connected())),
      ]);
      final auth = container.read(authStateProvider.notifier);
      await auth.switchServer(scope.origin);
      await container
          .read(savedServerCredentialsProvider)
          .write(_credential(scope.origin, 'token-a'));
      expect(await auth.unlockSavedServer(), isTrue);
      final api = container.read(apiClientProvider);
      api.rawDio.httpClientAdapter = requests;
      final queue = container.read(progressQueueProvider) as ProgressQueue;
      await queue.resume(scope);
      if (!restart) {
        await queue.enqueue(42, 37);
        expect(await queue.latestPosition(scope, 42), 37);
        expect(requests.requests, isEmpty);
        await auth.logout();
        expect(await queue.pendingIds(scope), isNotEmpty);
      } else {
        expect(await queue.latestPosition(scope, 42), 37);
        // Network availability alone never releases the saved account gate.
        await expectLater(queue.flushForSync(scope),
            throwsA(anyOf(isA<StateError>(), isA<DioException>())));
        expect(requests.requests, isEmpty);
        expect(await queue.pendingIds(scope), isNotEmpty);
        denied = true;
        expect(await auth.reconnectSavedAccount(), isFalse);
        expect(container.read(authStateProvider).requireValue.isUnauthenticated,
            isTrue);
        expect(await queue.pendingIds(scope), isNotEmpty);
        denied = false;
        await container
            .read(savedServerCredentialsProvider)
            .write(_credential(scope.origin, 'token-a'));
        expect(await auth.unlockSavedServer(), isTrue);
        await queue.resume(scope);
        online = true;
        expect(await auth.reconnectSavedAccount(), isTrue);
        await queue.flushForSync(scope);
        expect(await queue.pendingIds(scope), isEmpty);
        expect(requests.requests, isNotEmpty);
        expect(
            requests.requests.every((request) =>
                request.headers['Authorization'] == 'Bearer token-a'),
            isTrue);
      }
      await queue.dispose();
      container.dispose();
    }
    await directory.delete(recursive: true);
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
