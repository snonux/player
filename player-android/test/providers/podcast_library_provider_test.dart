import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/podcast_library_provider.dart';
import 'package:player_android/providers/settings_provider.dart';
import 'package:player_android/services/podcast_repository.dart';

class _SignedOut extends AuthStateNotifier {
  @override
  Future<AuthState> build() async => const AuthState.unauthenticated();
}

class _Local extends SettingsNotifier {
  @override
  Future<AppSettings> build() async => const AppSettings(
      serverBaseUrl: 'https://saved.example',
      destination: LibraryDestination.local);
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  sqfliteFfiInit();
  databaseFactory = databaseFactoryFfi;
  late PodcastRepository repository;
  const account = 'account:https://saved.example#7';
  setUp(() async {
    SharedPreferences.setMockInitialValues(
        {'podcast_account:https://saved.example': 7});
    repository = PodcastRepository(databasePath: inMemoryDatabasePath);
    final db = await repository.database;
    for (final scope in [account, 'account:https://other.example#8', 'local']) {
      await db.insert('podcast_subscriptions', {
        'scope': scope,
        'feed_key': 'feed:$scope',
        'subscribed': 1,
        'subscription_ms': 10,
        'title': scope,
      });
    }
  });
  tearDown(() => repository.close());
  Future<ProviderContainer> open() async {
    final container = ProviderContainer(overrides: [
      settingsProvider.overrideWith(_Local.new),
      authStateProvider.overrideWith(_SignedOut.new),
      podcastRepositoryProvider.overrideWithValue(repository),
      apiClientProvider.overrideWith(
          (ref) => throw StateError('Signed-out browsing used server API')),
    ]);
    await container.read(settingsProvider.future);
    await container.read(authStateProvider.future);
    return container;
  }

  test(
      'device mode migrates cached subscriptions without a login or server request',
      () async {
    final container = await open();
    addTearDown(container.dispose);
    expect(await container.read(podcastScopeProvider.future), account);
    expect(
        (await container.read(podcastSubscriptionsProvider.future))
            .single['title'],
        account);
    expect(container.read(podcastServerMetadataProvider), isNull);
    expect(await container.read(podcastServerSubscriptionsProvider.future),
        isNull);
    final libraries =
        await container.read(podcastDeviceLibrariesProvider.future);
    expect(libraries.keys,
        containsAll(['local', account, 'account:https://other.example#8']));
  });
  test(
      'explicit device library selection persists and keeps account subscriptions separate',
      () async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString('podcast_device_scope', 'local');
    var container = await open();
    expect(await container.read(podcastScopeProvider.future), 'local');
    expect(
        (await container.read(podcastSubscriptionsProvider.future))
            .single['title'],
        'local');
    await prefs.setString('podcast_device_scope', account);
    container.read(podcastDeviceScopeSelectionProvider.notifier).state =
        account;
    expect(await container.read(podcastScopeProvider.future), account);
    container.dispose();
    container = await open();
    expect(await container.read(podcastScopeProvider.future), account);
    expect(await repository.subscriptions('local'), hasLength(1));
    expect(await repository.subscriptions('account:https://other.example#8'),
        hasLength(1));
    container.dispose();
  });
  test('a missing remembered library falls back safely to device-only podcasts',
      () async {
    final db = await repository.database;
    await db.delete('podcast_subscriptions',
        where: 'scope=?', whereArgs: [account]);
    await db.insert('podcast_subscriptions', {
      'scope': 'account:https://third.example#9',
      'feed_key': 'feed:third',
      'subscribed': 1,
      'subscription_ms': 10
    });
    final container = await open();
    addTearDown(container.dispose);
    expect(await container.read(podcastScopeProvider.future), 'local');
  });
}
