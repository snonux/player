import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:player_android/api/dio_client.dart';
import 'package:player_android/models/offline_media.dart';
import 'package:player_android/models/user.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/first_run_provider.dart';
import 'package:player_android/providers/offline_downloads_provider.dart';
import 'package:player_android/router.dart';
import 'package:player_android/services/offline_download_repository.dart';
import 'package:player_android/services/saved_server_credentials.dart';
import 'package:shared_preferences/shared_preferences.dart';

class _Tokens implements TokenStorage {
  String? token;
  @override
  Future<String?> readToken() async => token;
  @override
  Future<void> writeToken(String value) async => token = value;
  @override
  Future<void> deleteToken() async => token = null;
}

class _Vault implements SavedServerCredentials {
  final entries = <String, SavedServerCredential>{};
  @override
  Future<SavedServerCredential?> read(String origin) async => entries[origin];
  @override
  Future<void> write(SavedServerCredential value) async =>
      entries[value.origin] = value;
  @override
  Future<void> delete(String origin) async => entries.remove(origin);
}

// The native journey exercises the actual router, HTTP validation and views.
// OS fingerprint enrollment/prompt is controlled separately from this fixture.
class _Approval implements ServerBiometricUnlock {
  var calls = 0;
  bool accepted = false;
  @override
  Future<bool> authenticate(String origin) async {
    calls++;
    return accepted;
  }
}

Future<void> _settleRequest(WidgetTester tester) async {
  await tester
      .runAsync(() => Future<void>.delayed(const Duration(milliseconds: 300)));
  await tester.pump();
  await tester.pump(const Duration(milliseconds: 300));
}

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();
  testWidgets(
      'selected server outage offers isolated offline account after approval',
      (tester) async {
    SharedPreferences.setMockInitialValues({});
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final origin = 'http://127.0.0.1:${server.port}';
    var status = 401;
    var body = '{}';
    server.listen((request) async {
      request.response.statusCode = status;
      request.response.headers.contentType = ContentType.json;
      request.response.write(body);
      await request.response.close();
    });
    final folder = await Directory.systemTemp.createTemp('offline-offer-');
    final repository = OfflineDownloadRepository(directory: folder);
    final tokens = _Tokens();
    final vault = _Vault();
    final biometric = _Approval();
    final container = ProviderContainer(overrides: [
      tokenStorageProvider.overrideWithValue(tokens),
      savedServerCredentialsProvider.overrideWithValue(vault),
      serverBiometricUnlockProvider.overrideWithValue(biometric),
      firstRunProvider.overrideWith((ref) async => false),
      offlineDownloadRepositoryProvider.overrideWithValue(repository),
    ]);
    try {
      debugPrint('F34 setup: switch selected server');
      final auth = container.read(authStateProvider.notifier);
      await tester.runAsync(() => auth.switchServer(origin));
      debugPrint('F34 setup: save scoped files');
      await vault.write(SavedServerCredential(
          origin: origin,
          token: 'saved-token',
          tokenId: 8,
          user: const User(id: 7, username: 'listener', isAdmin: false),
          expiresAt: DateTime.now()
              .add(const Duration(days: 1))
              .millisecondsSinceEpoch));
      await tester.runAsync(() async {
        // Opening storage sweeps orphan files. Create fixture copies only
        // after that sweep, matching the real download setup order.
        await repository.database;
        for (final user in [7, 8]) {
          final key = offlineMediaKey(origin, user, 11);
          await repository.file(key).writeAsBytes([1, 2, 3]);
          await repository.add(OfflineMedia(
              key: key,
              origin: origin,
              userId: user,
              mediaId: 11,
              title: user == 7 ? 'My saved song' : 'Other account song',
              type: 'audio',
              sizeBytes: 3,
              addedAt: DateTime.now()));
          expect((await repository.validatePlaybackCopy(key)).userId, user);
        }
        expect((await repository.list()).map((item) => item.userId).toSet(),
            {7, 8});
      });
      final listSubscription =
          container.listen(offlineMediaListProvider, (_, value) {
        debugPrint(
            'F34 downloads: ${value.when(data: (items) => items.map((item) => '${item.origin} / ${item.userId} / ${item.title}').join(', '), loading: () => 'loading', error: (error, stack) => 'ERROR: $error\n$stack')}');
      }, fireImmediately: true);
      addTearDown(listSubscription.close);
      debugPrint('F34 setup: show login');
      await tester.pumpWidget(UncontrolledProviderScope(
          container: container,
          child: MaterialApp.router(
              routerConfig: container.read(routerProvider))));
      await tester.pumpAndSettle();
      expect(find.text('Saved account: listener'), findsOneWidget);
      await tester.tap(find.byKey(const Key('login_fingerprint')));
      await _settleRequest(tester);
      expect(biometric.calls, 1);
      expect(tokens.token, isNull);
      expect(find.text('Server unavailable'), findsNothing);
      biometric.accepted = true;
      // A reachable server's denial and malformed response never offer offline.
      for (final response in [(401, '{}'), (200, '{}')]) {
        status = response.$1;
        body = response.$2;
        await tester.tap(find.byKey(const Key('login_fingerprint')));
        await _settleRequest(tester);
        expect(find.text('Server unavailable'), findsNothing);
        expect(tokens.token, isNull);
      }
      // Then make the selected server unreachable using a real socket failure.
      await server.close(force: true);
      await tester.tap(find.byKey(const Key('login_fingerprint')));
      await _settleRequest(tester);
      expect(find.text('Server unavailable'), findsOneWidget);
      expect(
          find.textContaining('$origin could not be reached'), findsOneWidget);
      expect(tokens.token, isNull);
      await tester.tap(find.text('Stay signed out'));
      await tester.pumpAndSettle();
      expect(container.read(authStateProvider).requireValue.isUnauthenticated,
          isTrue);
      await tester.tap(find.byKey(const Key('login_fingerprint')));
      await _settleRequest(tester);
      await tester.tap(find.byKey(const Key('use_server_offline')));
      await tester.pumpAndSettle();
      final deadline = DateTime.now().add(const Duration(seconds: 10));
      while (find.text('My saved song').evaluate().isEmpty &&
          DateTime.now().isBefore(deadline)) {
        await tester.runAsync(
            () => Future<void>.delayed(const Duration(milliseconds: 100)));
        await tester.pump(const Duration(milliseconds: 100));
      }
      await tester.pumpAndSettle();
      expect(find.text('Offline downloads'), findsOneWidget);
      expect(
          find.textContaining('Offline · listener\n$origin'), findsOneWidget);
      expect(find.text('My saved song'), findsOneWidget);
      expect(find.text('Other account song'), findsNothing);
      expect(container.read(authStateProvider).requireValue.isOffline, isTrue);
      expect(container.read(credentialMutationQueueProvider).credentialsEnabled,
          isFalse);
      expect(tester.takeException(), isNull);
    } finally {
      await tester.pumpWidget(const SizedBox());
      container.dispose();
      await repository.close();
      await server.close(force: true);
      await folder.delete(recursive: true);
    }
  });
}
