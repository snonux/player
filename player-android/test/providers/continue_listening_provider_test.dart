import 'dart:io';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/continue_listening_provider.dart';
import 'package:player_android/providers/local_library_provider.dart';
import 'package:player_android/providers/offline_downloads_provider.dart';
import 'package:player_android/providers/settings_provider.dart';
import 'package:player_android/services/local_library_repository.dart';
import 'package:player_android/widgets/continue_listening.dart';

class _Locked extends AuthStateNotifier {
  @override
  Future<AuthState> build() async => const AuthState.unauthenticated();
}

class _Settings extends SettingsNotifier {
  @override
  Future<AppSettings> build() async =>
      const AppSettings(serverBaseUrl: 'https://private.invalid');
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  sqfliteFfiInit();
  databaseFactory = databaseFactoryFfi;
  test(
      'signed-out shortcuts offer device files, '
      'no locked server files, and omit finished media', () async {
    SharedPreferences.setMockInitialValues({});
    final folder = await Directory.systemTemp.createTemp('continue-provider-');
    addTearDown(() => folder.delete(recursive: true));
    final local = LocalLibraryDatabase(databasePath: '${folder.path}/local.db');
    final media = SqliteLocalMediaRepository(local);
    final progress = SqliteLocalProgressRepository(local);
    for (final title in ['Audiobook', 'Finished book', 'Unstarted book']) {
      final item = await media.importDocument(
          uri: 'content://test/$title', title: title, mimeType: 'audio/mpeg');
      if (title != 'Unstarted book') await progress.savePosition(item.id, 42);
      if (title == 'Finished book') await progress.markFinished(item.id);
    }
    final container = ProviderContainer(overrides: [
      authStateProvider.overrideWith(_Locked.new),
      settingsProvider.overrideWith(_Settings.new),
      localMediaRepositoryProvider.overrideWithValue(media),
      localProgressRepositoryProvider.overrideWithValue(progress),
      apiClientProvider
          .overrideWith((ref) => throw StateError('Network forbidden')),
      offlineDownloadRepositoryProvider
          .overrideWith((ref) => throw StateError('Locked account read')),
    ]);
    try {
      await container.read(authStateProvider.future);
      await container.read(settingsProvider.future);
      final rows =
          await container.read(continueListeningProvider(false).future);
      expect(rows.map((e) => e.title), ['Audiobook']);
      expect(rows.map((e) => e.position), [42]);
    } finally {
      container.dispose();
      await local.close();
    }
  });
  test('device shortcuts do not initialize auth or its server dependencies',
      () async {
    SharedPreferences.setMockInitialValues({});
    final folder = await Directory.systemTemp.createTemp('continue-device-');
    final local = LocalLibraryDatabase(databasePath: '${folder.path}/local.db');
    final container = ProviderContainer(overrides: [
      settingsProvider.overrideWith(_Settings.new),
      authStateProvider.overrideWith(
          () => throw StateError('Device shortcuts initialized auth')),
      apiClientProvider.overrideWith(
          (ref) => throw StateError('Device shortcuts initialized API')),
      localMediaRepositoryProvider
          .overrideWithValue(SqliteLocalMediaRepository(local)),
      localProgressRepositoryProvider
          .overrideWithValue(SqliteLocalProgressRepository(local)),
    ]);
    final deviceSubscription =
        container.listen(continueListeningProvider(true), (_, __) {});
    final defaultSubscription =
        container.listen(continueListeningProvider(false), (_, __) {});
    try {
      expect(await container.read(continueListeningProvider(true).future),
          isEmpty);
      expect(container.exists(authStateProvider), isFalse);
      expect(container.exists(apiClientProvider), isFalse);
      // The generic panel is also safe before a server identity exists.
      expect(await container.read(continueListeningProvider(false).future),
          isEmpty);
      expect(container.exists(authStateProvider), isFalse);
    } finally {
      deviceSubscription.close();
      defaultSubscription.close();
      container.dispose();
      await local.close();
      await folder.delete(recursive: true);
    }
  });
  testWidgets('failed resume lookup has retry and keeps long titles readable',
      (tester) async {
    var fail = true;
    await tester.binding.setSurfaceSize(const Size(320, 480));
    addTearDown(() => tester.binding.setSurfaceSize(null));
    await tester.pumpWidget(ProviderScope(
        overrides: [
          continueListeningProvider(false).overrideWith((ref) async {
            if (fail) throw const FileSystemException('unavailable');
            return [
              const ContinueListeningItem(
                  title: 'A long audiobook title with many words and chapters',
                  source: 'On this device',
                  position: 42,
                  path: '/local/player/1')
            ];
          }),
        ],
        child: MaterialApp(
            builder: (context, child) => MediaQuery(
                data: MediaQuery.of(context)
                    .copyWith(textScaler: const TextScaler.linear(2)),
                child: child!),
            home: const Scaffold(
                body: SingleChildScrollView(child: ContinueListening())))));
    await tester.pumpAndSettle();
    expect(find.text('Retry continue listening'), findsOneWidget);
    fail = false;
    await tester.tap(find.text('Retry continue listening'));
    await tester.pumpAndSettle();
    expect(find.text('Continue listening'), findsOneWidget);
    expect(find.text('On this device · Resume at 0:42'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.binding.setSurfaceSize(const Size(640, 320));
    await tester.pump();
    expect(tester.takeException(), isNull);
  });
}
