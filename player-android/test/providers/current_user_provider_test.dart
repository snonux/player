import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:dio/dio.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/models/models.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/current_user_provider.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _MemoryTokenStorage implements TokenStorage {
  _MemoryTokenStorage([this.token]);

  String? token;

  @override
  Future<String?> readToken() async => token;

  @override
  Future<void> writeToken(String value) async => token = value;

  @override
  Future<void> deleteToken() async => token = null;
}

class _FakeApiClient extends PlayerApiClient {
  _FakeApiClient() : super(dio: Dio());

  @override
  Future<Map<String, dynamic>> createAPIToken(
          {required String name, int? expiresInDays}) async =>
      {'token': 'pt-test-token'};
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() => SharedPreferences.setMockInitialValues({}));

  for (final user in [
    const User(id: 1, username: 'admin', isAdmin: true),
    const User(id: 2, username: 'listener', isAdmin: false),
  ]) {
    test('login retains ${user.username} identity for this session', () async {
      final container = ProviderContainer(overrides: [
        tokenStorageProvider.overrideWithValue(_MemoryTokenStorage()),
        apiClientProvider.overrideWithValue(_FakeApiClient()),
      ]);
      addTearDown(container.dispose);
      await container.read(authStateProvider.future);
      await container.read(authStateProvider.notifier).login(user);

      final current = await container.read(currentUserProvider.future);
      expect(current?.username, user.username);
      expect(current?.isAdmin, user.isAdmin);

      // A new process has no cookie, so a saved marker must not grant admin UI.
      final restarted = ProviderContainer(overrides: [
        tokenStorageProvider.overrideWithValue(_MemoryTokenStorage()),
      ]);
      addTearDown(restarted.dispose);
      final restored = await restarted.read(currentUserProvider.future);
      expect(restored, isNull);
      expect((await restarted.read(authStateProvider.future)).isUnauthenticated,
          isTrue);

      await container.read(authStateProvider.notifier).logout();
      expect(await container.read(currentUserProvider.future), isNull);
      final prefs = await SharedPreferences.getInstance();
      expect(prefs.getString('auth_user'), isNull);
    });
  }

  test('legacy saved identity never grants admin controls on cold start',
      () async {
    SharedPreferences.setMockInitialValues({
      'auth_session_present': true,
      'auth_user': '{"id":1,"username":"admin","is_admin":true}',
    });
    final storage = _MemoryTokenStorage('legacy-bearer');
    final container = ProviderContainer(overrides: [
      tokenStorageProvider.overrideWithValue(storage),
    ]);
    addTearDown(container.dispose);
    expect(await container.read(currentUserProvider.future), isNull);
    expect((await container.read(authStateProvider.future)).isUnauthenticated,
        isTrue);
    expect(storage.token, isNull);
    final prefs = await SharedPreferences.getInstance();
    expect(prefs.getBool('auth_session_present'), isNull);
  });

  test('saved profile without a session marker is ignored', () async {
    SharedPreferences.setMockInitialValues({
      'auth_user': '{"id":1,"username":"admin","is_admin":true}',
    });
    final container = ProviderContainer(overrides: [
      tokenStorageProvider.overrideWithValue(_MemoryTokenStorage()),
    ]);
    addTearDown(container.dispose);
    expect(await container.read(currentUserProvider.future), isNull);
    expect((await container.read(authStateProvider.future)).isUnauthenticated,
        isTrue);
  });
}
