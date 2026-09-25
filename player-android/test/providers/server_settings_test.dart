import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/first_run_provider.dart';
import 'package:player_android/providers/public_api_client_provider.dart';
import 'package:player_android/providers/settings_provider.dart';
import 'package:player_android/router.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _TokenStore implements TokenStorage {
  String? token = 'pt-old-server';

  @override
  Future<String?> readToken() async => token;
  @override
  Future<void> writeToken(String value) async => token = value;
  @override
  Future<void> deleteToken() async => token = null;
}

class _Requests implements HttpClientAdapter {
  final seen = <RequestOptions>[];

  @override
  Future<ResponseBody> fetch(RequestOptions options,
      Stream<Uint8List>? requestStream, Future? cancelFuture) async {
    seen.add(options);
    return ResponseBody.fromString('{}', 200);
  }

  @override
  void close({bool force = false}) {}
}

class _Unauthenticated extends AuthStateNotifier {
  @override
  Future<AuthState> build() async => const AuthState.unauthenticated();
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test('server switch moves authenticated client and drops old credentials',
      () async {
    SharedPreferences.setMockInitialValues({
      'server_base_url': 'https://old.example/',
      'auth_session_present': true,
      'auth_origin': 'https://old.example',
      'auth_expires_at':
          DateTime.now().add(const Duration(days: 1)).millisecondsSinceEpoch,
      'auth_user': '{"id":1,"username":"alice","is_admin":false}',
      'auth_token_id': 7,
    });
    final storage = _TokenStore();
    final container = ProviderContainer(overrides: [
      tokenStorageProvider.overrideWithValue(storage),
    ]);
    addTearDown(container.dispose);

    final auth = await container.read(authStateProvider.future);
    expect(auth.isAuthenticated, isTrue);
    final oldClient = container.read(apiClientProvider);
    final oldRequests = _Requests();
    oldClient.rawDio.httpClientAdapter = oldRequests;
    final oldPublic = container.read(publicApiClientProvider);
    expect(oldClient.baseUrl, 'https://old.example');
    expect(oldPublic.baseUrl, parseServerBaseUrl(kPlayerBaseUrl).origin);
    expect(
        oldClient.streamUrl(42), 'https://old.example/api/v1/media/42/stream');
    await oldClient.rawDio.get<void>('/healthz');
    expect(
        oldRequests.seen.last.headers['Authorization'], 'Bearer pt-old-server');

    await container
        .read(authStateProvider.notifier)
        .switchServer(' https://new.example:8443/ ');
    expect(storage.token, isNull);
    expect(oldRequests.seen.singleWhere((r) => r.method == 'DELETE').uri.origin,
        'https://old.example');
    expect(container.read(settingsProvider).requireValue.serverBaseUrl,
        'https://new.example:8443');
    expect(container.read(playerBaseUrlProvider).origin,
        'https://new.example:8443');

    final newClient = container.read(apiClientProvider);
    final newPublic = container.read(publicApiClientProvider);
    expect(newClient.baseUrl, 'https://new.example:8443');
    expect(newPublic.baseUrl, parseServerBaseUrl(kPlayerBaseUrl).origin);
    expect(newClient.thumbnailUrl(42),
        'https://new.example:8443/api/v1/media/42/thumbnail');
    expect(newClient.shareUrl('abc'), 'https://new.example:8443/s/abc');
    final newRequests = _Requests();
    newClient.rawDio.httpClientAdapter = newRequests;
    await newClient.rawDio.get<void>('/healthz');
    expect(newRequests.seen.single.uri.origin, 'https://new.example:8443');
    expect(newRequests.seen.single.headers['Authorization'], isNull);
    final publicRequests = _Requests();
    newPublic.rawDio.httpClientAdapter = publicRequests;
    await newPublic.rawDio.get<void>('/healthz');
    expect(publicRequests.seen.single.uri.origin,
        parseServerBaseUrl(kPlayerBaseUrl).origin);
    expect(publicRequests.seen.single.headers['Authorization'], isNull);
    await oldClient.rawDio.get<void>('/healthz');
    expect(oldRequests.seen.last.headers['Authorization'], isNull);

    final prefs = await SharedPreferences.getInstance();
    expect(prefs.getString('server_base_url'), 'https://new.example:8443');
    expect(prefs.getString('auth_origin'), isNull);
  });

  test('rejects paths, non-web schemes, and embedded credentials', () {
    expect(() => parseServerBaseUrl('https://example.com/api'),
        throwsFormatException);
    expect(
        () => parseServerBaseUrl('file:///tmp/player'), throwsFormatException);
    expect(() => parseServerBaseUrl('https://alice:secret@example.com'),
        throwsFormatException);
    expect(() => parseServerBaseUrl('https://example.com?token=secret'),
        throwsFormatException);
    expect(parseServerBaseUrl('https://example.com/').toString(),
        'https://example.com');
  });

  test('share URLs omit explicit default HTTP and HTTPS ports', () {
    for (final (input, expected) in [
      ('http://example.com:80', 'http://example.com/s/abc'),
      ('https://example.com:443', 'https://example.com/s/abc'),
    ]) {
      final baseUrl = parseServerBaseUrl(input).toString();
      expect(
          PlayerApiClient(dio: Dio(BaseOptions(baseUrl: baseUrl)))
              .shareUrl('abc'),
          expected);
    }
  });

  test('share token stays on the build origin after switching server',
      () async {
    SharedPreferences.setMockInitialValues({
      'server_base_url': 'https://other.example',
    });
    final container = ProviderContainer();
    addTearDown(container.dispose);
    await container.read(settingsProvider.future);
    expect(
        container.read(playerBaseUrlProvider).origin, 'https://other.example');

    final client = container.read(publicApiClientProvider);
    final requests = _Requests();
    client.rawDio.httpClientAdapter = requests;
    await client.getSharedMediaPage('private-token');

    expect(requests.seen.single.uri.origin,
        parseServerBaseUrl(kPlayerBaseUrl).origin);
    expect(requests.seen.single.uri.path, '/s/private-token');
    expect(requests.seen.single.uri.origin, isNot('https://other.example'));
    expect(requests.seen.single.headers['Authorization'], isNull);
  });

  testWidgets('server setup is reachable from login without authentication',
      (tester) async {
    SharedPreferences.setMockInitialValues({});
    final container = ProviderContainer(overrides: [
      authStateProvider.overrideWith(_Unauthenticated.new),
      firstRunProvider.overrideWith((ref) async => false),
    ]);
    addTearDown(container.dispose);
    await tester.pumpWidget(UncontrolledProviderScope(
      container: container,
      child: MaterialApp.router(routerConfig: container.read(routerProvider)),
    ));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('login_server_setup')), findsOneWidget);
    await tester.tap(find.byKey(const Key('login_server_setup')));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('settings_base_url')), findsOneWidget);
    expect(find.byKey(const Key('settings_logout')), findsNothing);
  });

  testWidgets('old share intent never sends token to the newly saved server',
      (tester) async {
    SharedPreferences.setMockInitialValues({
      'server_base_url': 'https://other.example',
    });
    final container = ProviderContainer(overrides: [
      authStateProvider.overrideWith(_Unauthenticated.new),
      firstRunProvider.overrideWith((ref) async => false),
    ]);
    addTearDown(container.dispose);
    await container.read(settingsProvider.future);
    final requests = _Requests();
    container.read(publicApiClientProvider).rawDio.httpClientAdapter = requests;
    final router = container.read(routerProvider);
    addTearDown(router.dispose);
    await tester.pumpWidget(UncontrolledProviderScope(
      container: container,
      child: MaterialApp.router(routerConfig: router),
    ));

    router.go(AppRoutes.shareViewerPath('private-token'));
    await tester.pumpAndSettle();
    expect(requests.seen.single.uri.path, '/s/private-token');
    expect(requests.seen.single.uri.origin,
        parseServerBaseUrl(kPlayerBaseUrl).origin);
    expect(requests.seen.single.uri.origin, isNot('https://other.example'));
  });
}
