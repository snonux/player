import 'dart:async';
import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';
import 'package:player_android/providers/podcast_association_provider.dart';
import 'package:player_android/services/podcast_repository.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/api/dio_player_api_client.dart';
import 'package:player_android/models/user.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/settings_provider.dart';

class AssociationAuth extends AuthStateNotifier {
  @override
  Future<AuthState> build() async => const AuthState.authenticated(
      user: User(id: 1, username: 'alice', isAdmin: false));
}

class AssociationSettings extends SettingsNotifier {
  AssociationSettings(this.destination);
  final LibraryDestination destination;
  @override
  Future<AppSettings> build() async => AppSettings(
      serverBaseUrl: 'https://server.example', destination: destination);
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  sqfliteFfiInit();
  databaseFactory = databaseFactoryFfi;
  late PodcastRepository repository;
  late PodcastAssociationController controller;
  late SharedPreferences prefs;
  setUp(() async {
    SharedPreferences.setMockInitialValues({});
    prefs = await SharedPreferences.getInstance();
    repository = PodcastRepository(databasePath: inMemoryDatabasePath);
    controller = PodcastAssociationController(repository, () async => prefs);
  });
  tearDown(() => repository.close());
  PodcastAssociationAccount account(String scope,
          {bool Function()? current, Future<bool> Function()? supported}) =>
      PodcastAssociationAccount(
          scope: scope,
          label: 'alice@https://server.example',
          current: current ?? () => true,
          supported: supported ?? () async => true);
  Future<void> localState() async {
    await (await repository.database).insert('podcast_episode_state', {
      'scope': 'local',
      'feed_key': 'feed:publisher.example/rss',
      'episode_key': 'guid:one',
      'position_seconds': 42.0,
      'position_ms': 100,
      'completed': 1,
      'completion_ms': 200,
      'completion_source': 'manual',
    });
  }

  const first = 'account:https://server.example#1';
  const second = 'account:https://server.example#2';

  test(
      'production offer lease uses quiet captured credential generation and excludes local/locked use',
      () async {
    final credentials = CredentialMutationQueue();
    final generation =
        credentials.beginAuthChange(origin: 'https://server.example');
    credentials.enable(generation, origin: 'https://server.example');
    final requests = <RequestOptions>[];
    final dio = Dio(BaseOptions(baseUrl: 'https://server.example'));
    dio.interceptors.add(InterceptorsWrapper(onRequest: (options, handler) {
      requests.add(options);
      handler.resolve(
          Response(requestOptions: options, data: {'podcast_sync': 1}));
    }));
    final container = ProviderContainer(overrides: [
      authStateProvider.overrideWith(AssociationAuth.new),
      settingsProvider
          .overrideWith(() => AssociationSettings(LibraryDestination.server)),
      credentialMutationQueueProvider.overrideWithValue(credentials),
      apiClientProvider.overrideWithValue(DioPlayerApiClient(dio: dio)),
    ]);
    addTearDown(container.dispose);
    await container.read(settingsProvider.future);
    await container.read(authStateProvider.future);
    final lease = container.read(podcastAssociationAccountProvider)!;
    expect(lease.scope, first);
    expect(lease.label, 'alice@https://server.example');
    expect(await lease.supported(), isTrue);
    expect(requests.single.extra['quietUnauthorized'], true);
    expect(requests.single.extra['credentialEpoch'], generation);
    credentials.beginAuthChange(origin: 'https://another.example');
    expect(lease.current(), isFalse);
    expect(await lease.supported(), isFalse);
    expect(requests, hasLength(1));
    container.invalidate(podcastAssociationAccountProvider);
    expect(container.read(podcastAssociationAccountProvider), isNull);

    final local = ProviderContainer(overrides: [
      authStateProvider.overrideWith(AssociationAuth.new),
      settingsProvider
          .overrideWith(() => AssociationSettings(LibraryDestination.local)),
      apiClientProvider.overrideWith(
          (ref) => throw StateError('Local use must not access API')),
    ]);
    addTearDown(local.dispose);
    await local.read(settingsProvider.future);
    await local.read(authStateProvider.future);
    expect(local.read(podcastAssociationAccountProvider), isNull);
  });

  test('offers require local registers, capability and a live captured account',
      () async {
    expect(await controller.offer(account(first)), isNull);
    await localState();
    expect(await controller.offer(account(first, supported: () async => false)),
        isNull);
    expect(
        await controller.offer(
            account(first, supported: () async => throw StateError('offline'))),
        isNull);
    expect(
        await controller.offer(account(first, current: () => false)), isNull);
    final gate = Completer<bool>();
    var current = true;
    final waiting = controller.offer(
        account(first, current: () => current, supported: () => gate.future));
    await Future<void>.delayed(const Duration(milliseconds: 10));
    current = false;
    gate.complete(true);
    expect(await waiting, isNull);
    expect(await controller.offer(account(first)), isNotNull);
  });

  test(
      'Keep separate/dismiss persists per account; settings repeats without merging',
      () async {
    await localState();
    final a = account(first);
    expect(await controller.offer(a), same(a));
    expect(await controller.markPrompted(a), isTrue);
    final restarted =
        PodcastAssociationController(repository, () async => prefs);
    expect(await restarted.offer(a), isNull);
    expect(await restarted.offer(a, repeat: true), same(a));
    expect(await restarted.offer(account(second)), isNotNull);
    final rows =
        await (await repository.database).query('podcast_episode_state');
    expect(rows, hasLength(1));
    expect(rows.single['scope'], 'local');
    expect(await repository.canAssociateLocal(first), isTrue);
  });

  test(
      'explicit consent preserves clocks and local data; association is account scoped',
      () async {
    await localState();
    final db = await repository.database;
    final original = await db.query('podcast_episode_state');
    await controller.associate(account(first));
    final copy = (await db.query('podcast_episode_state',
            where: 'scope=?', whereArgs: [first]))
        .single;
    expect(copy['position_seconds'], 42.0);
    expect(copy['position_ms'], 100);
    expect(copy['completion_ms'], 200);
    expect(copy['position_pending_edit_id'], isNotEmpty);
    expect(copy['completion_pending_edit_id'], isNotEmpty);
    expect(
        await db.query('podcast_episode_state',
            where: 'scope=?', whereArgs: ['local']),
        original);
    expect(await controller.offer(account(first), repeat: true), isNull);
    expect(await controller.offer(account(second)), isNotNull);
  });

  test('old consent cannot associate after account changes', () async {
    await localState();
    await expectLater(
        controller.associate(account(first, current: () => false)),
        throwsStateError);
    expect(
        (await (await repository.database).query('podcast_episode_state'))
            .single['scope'],
        'local');
  });

  test(
      'account equal clocks retain pending winners while newer local register copies independently',
      () async {
    await localState();
    final db = await repository.database;
    await db.insert('podcast_episode_state', {
      'scope': first,
      'feed_key': 'feed:publisher.example/rss',
      'episode_key': 'guid:one',
      'position_seconds': 81.0,
      'position_ms': 100,
      'position_pending_edit_id': 'existing-position-id',
      'completed': 0,
      'completion_ms': 50,
    });
    await controller.associate(account(first));
    final copy = (await db.query('podcast_episode_state',
            where: 'scope=?', whereArgs: [first]))
        .single;
    expect(copy['position_seconds'], 81.0);
    expect(copy['position_pending_edit_id'], 'existing-position-id');
    expect(copy['completed'], 1);
    expect(copy['completion_ms'], 200);
    expect(copy['completion_pending_edit_id'], isNotEmpty);
  });
}
