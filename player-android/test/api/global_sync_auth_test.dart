import 'dart:async';
import 'dart:io';
import 'dart:typed_data';
import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:cookie_jar/cookie_jar.dart';
import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/api/dio_player_api_client.dart';
import 'package:player_android/models/offline_media.dart';
import 'package:player_android/models/user.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/global_sync_provider.dart';
import 'package:player_android/providers/offline_downloads_provider.dart';
import 'package:player_android/providers/playback_session_provider.dart';
import 'package:player_android/providers/podcast_library_provider.dart';
import 'package:player_android/providers/progress_queue_provider.dart';
import 'package:player_android/providers/saved_queue_provider.dart';
import 'package:player_android/services/global_sync_coordinator.dart';
import 'package:player_android/services/music_queue_snapshot.dart';
import 'package:player_android/services/offline_download_repository.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/services/playback_session_coordinator.dart';
import 'package:player_android/services/podcast_repository.dart';
import 'package:player_android/services/progress_queue.dart';
import 'package:player_android/services/saved_server_credentials.dart';

class _Auth extends AuthStateNotifier {
  @override
  Future<AuthState> build() async => const AuthState.authenticated(
      user: User(id: 1, username: 'one', isAdmin: false));
}

class _Tokens implements TokenStorage {
  String? token = 'account-one';
  @override
  Future<String?> readToken() async => token;
  @override
  Future<void> writeToken(String value) async {
    token = value;
  }

  @override
  Future<void> deleteToken() async {
    token = null;
  }
}

class _Saved implements SavedServerCredentials {
  @override
  Future<SavedServerCredential?> read(String origin) async => null;
  @override
  Future<void> write(SavedServerCredential credential) async {}
  @override
  Future<void> delete(String origin) async {}
}

class _Connectivity implements Connectivity {
  @override
  Future<List<ConnectivityResult>> checkConnectivity() async =>
      [ConnectivityResult.none];
  @override
  Stream<List<ConnectivityResult>> get onConnectivityChanged =>
      const Stream.empty();
}

class _Transport implements HttpClientAdapter {
  final requests = <RequestOptions>[];
  Completer<ResponseBody>? held;
  @override
  Future<ResponseBody> fetch(RequestOptions options,
      Stream<Uint8List>? requestStream, Future? cancelFuture) async {
    requests.add(options);
    if (held != null) return held!.future;
    return ResponseBody.fromString('{"error":"expired"}', 401, headers: {
      Headers.contentTypeHeader: ['application/json']
    });
  }

  @override
  void close({bool force = false}) {}
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  test(
      'production sync leases and interceptor retain auth, pending queues and server playback on 401; interactive requests still clear them',
      () async {
    sqfliteFfiInit();
    databaseFactory = databaseFactoryFfi;
    SharedPreferences.setMockInitialValues({});
    final temp = await Directory.systemTemp.createTemp('quiet-sync-auth-');
    final tokens = _Tokens(), mutations = CredentialMutationQueue();
    const origin = 'https://player.test';
    mutations.enable(mutations.generation, origin: origin);
    final transport = _Transport();
    late ProviderContainer container;
    var invalidations = 0;
    final dio = DioClient(
        baseUrl: Uri.parse(origin),
        storage: tokens,
        mutationQueue: mutations,
        navigatorKey: GlobalKey<NavigatorState>(),
        onUnauthorized: () async {
          invalidations++;
          await container
              .read(authStateProvider.notifier)
              .clearAfterUnauthorized(advanceGeneration: false);
        })
      ..dio.httpClientAdapter = transport;
    final api = DioPlayerApiClient(
        dio: dio.dio, credentialEpoch: () => mutations.generation);
    final progress = ProgressQueue(
        apiClient: DioPlayerApiClient(
            dio: dio.dio,
            credentialEpoch: () => mutations.generation,
            quietUnauthorized: true),
        connectivity: _Connectivity(),
        databasePath: '${temp.path}/progress.db',
        retryDelay: const Duration(days: 1));
    final downloads = OfflineDownloadRepository(
        directory: Directory('${temp.path}/downloads'));
    final podcasts =
        PodcastRepository(databasePath: '${temp.path}/podcasts.db');
    final playback = PlaybackSessionCoordinator();
    var stops = 0;
    final playbackLease = await playback.claim(
        kind: PlaybackSourceKind.server,
        identity: 'server:one:7',
        stop: () async {
          stops++;
        });
    container = ProviderContainer(overrides: [
      playerBaseUrlProvider.overrideWithValue(Uri.parse(origin)),
      apiClientProvider.overrideWithValue(api),
      tokenStorageProvider.overrideWithValue(tokens),
      credentialMutationQueueProvider.overrideWithValue(mutations),
      cookieJarProvider.overrideWithValue(CookieJar()),
      savedServerCredentialsProvider.overrideWithValue(_Saved()),
      authStateProvider.overrideWith(_Auth.new),
      progressQueueProvider.overrideWithValue(progress),
      offlineDownloadRepositoryProvider.overrideWithValue(downloads),
      podcastRepositoryProvider.overrideWithValue(podcasts),
      playbackSessionCoordinatorProvider.overrideWithValue(playback),
    ]);
    GlobalSyncCoordinator? coordinator;
    try {
      await container.read(authStateProvider.future);
      const progressScope = ProgressScope(origin: origin, userId: 1);
      await progress.init(scope: progressScope);
      await progress.enqueue(7, 12);
      final item = OfflineMedia(
          key: offlineMediaKey(origin, 1, 8),
          origin: origin,
          userId: 1,
          mediaId: 8,
          title: 'offline',
          type: 'audio',
          sizeBytes: 1,
          addedAt: DateTime.utc(2026));
      await downloads.add(item);
      await downloads.saveProgress(item.key, 22);
      final capture =
          Provider<GlobalSyncAccount?>((ref) => captureSyncAccount(ref));
      final active = container.read(capture)!;
      final savedCapture = Provider((ref) => captureSavedQueueAccount(ref));
      final savedAccount = container.read(savedCapture)!;
      final saved = container.read(savedQueueSyncProvider);
      await saved.capture(
          savedAccount,
          const MusicQueueSnapshot(
              entries: [(id: 1, mediaId: 7)],
              index: 0,
              position: 12,
              label: 'live',
              upNext: {}));
      final progressIDs = await progress.pendingIds(progressScope);
      coordinator = GlobalSyncCoordinator(
          container.read(podcastSyncProvider), () => active);
      await coordinator.syncNow();
      expect(coordinator.state.errorKind, 'auth');
      expect(coordinator.state.lastFullSuccessMs, isNull);
      expect(mutations.generation, 0);
      expect(mutations.credentialsEnabled, isTrue);
      expect(tokens.token, 'account-one');
      expect(invalidations, 0);
      expect(container.read(authStateProvider).requireValue.isAuthenticated,
          isTrue);
      expect(await progress.pendingIds(progressScope), progressIDs);
      expect(await saved.pending(savedAccount), 1);
      expect(playbackLease!.isCurrent, isTrue);
      expect(stops, 0);
      // Also cover direct podcast/preplay/end transport and both queue methods.
      for (final request in [
        () => active.podcast.send({}),
        () => savedAccount.write({}, 0),
        () => container
            .read(Provider((ref) => captureSavedQueueAccount(ref)))!
            .read()
      ]) {
        await expectLater(
            request(),
            throwsA(isA<DioException>()
                .having((e) => e.response?.statusCode, 'status', 401)));
      }
      expect(
          transport.requests.map((r) => r.path),
          containsAll([
            '/api/v1/config',
            '/api/v1/podcasts/sync',
            '/api/v1/media/8/playback-sync',
            '/api/v1/progress/batch',
            '/api/v1/queue'
          ]));
      for (final request in transport.requests) {
        expect(request.extra['quietUnauthorized'], true);
        expect(request.extra['credentialEpoch'], 0);
        expect(request.headers['Authorization'], 'Bearer account-one');
      }
      expect(invalidations, 0);
      expect(playbackLease.isCurrent, isTrue);

      // A dispatched old-account 401 cannot invalidate the newly enabled one.
      transport.held = Completer<ResponseBody>();
      final late = active.podcast.send({});
      final assertion = expectLater(late, throwsA(isA<DioException>()));
      while (transport.requests.last.path != '/api/v1/podcasts/sync') {
        await Future<void>.delayed(Duration.zero);
      }
      final next = mutations.beginAuthChange(origin: origin);
      await tokens.writeToken('account-two');
      mutations.enable(next, origin: origin);
      transport.held!.complete(ResponseBody.fromString('{}', 401));
      await assertion;
      expect(active.podcast.current(), isFalse);
      expect(tokens.token, 'account-two');
      expect(mutations.credentialsEnabled, isTrue);
      expect(playbackLease.isCurrent, isTrue);
      final before = transport.requests.length;
      await expectLater(
          active.podcast.send({}),
          throwsA(isA<DioException>()
              .having((e) => e.type, 'type', DioExceptionType.cancel)));
      expect(transport.requests.length, before,
          reason: 'stale lease still cancels before dispatch');
      transport.held = null;
      await expectLater(api.getConfig(), throwsA(isA<DioException>()));
      expect(invalidations, 1);
      expect(mutations.credentialsEnabled, isFalse);
      expect(tokens.token, isNull);
      expect(container.read(authStateProvider).requireValue.isUnauthenticated,
          isTrue);
      expect(await progress.pendingIds(progressScope), isEmpty);
      expect(playbackLease.isCurrent, isFalse);
      expect(stops, 1);
    } finally {
      coordinator?.dispose();
      container.dispose();
      await progress.dispose();
      await downloads.close();
      await podcasts.close();
      dio.dio.close(force: true);
      await temp.delete(recursive: true);
    }
  });
}
