import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/models/offline_media.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/offline_downloads_provider.dart';
import 'package:player_android/screens/offline_downloads_screen.dart';
import 'package:player_android/services/offline_download_repository.dart';
import 'package:player_android/services/offline_downloads.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/services/playback_session_coordinator.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';

class _StopControl extends PlaybackSessionCoordinator {
  bool refuseStop = false;

  @override
  Future<void> stopLocalSource(String uri) async {
    if (refuseStop) throw StateError('Player could not release file');
    await super.stopLocalSource(uri);
  }
}

void main() {
  setUpAll(() {
    sqfliteFfiInit();
    databaseFactory = databaseFactoryFfiNoIsolate;
  });

  testWidgets(
      'manager confirms local deletion, preserves file if stop fails, never reads API',
      (tester) async {
    final directory = Directory.systemTemp.createTempSync('player-offline-ui-');
    final repository = (await tester.runAsync(
        () async => OfflineDownloadRepository(directory: directory)))!;
    final playback = _StopControl();
    final downloads = OfflineDownloads(repository, playback);
    final key = offlineMediaKey('https://saved.example:8443', 7, 11);
    final item = OfflineMedia(
        key: key,
        origin: 'https://saved.example:8443',
        userId: 7,
        mediaId: 11,
        title: 'Saved audio.mp3',
        type: 'audio',
        sizeBytes: 4,
        addedAt: DateTime.now());
    final container = ProviderContainer(overrides: [
      offlineDownloadRepositoryProvider.overrideWithValue(repository),
      offlineDownloadsProvider.overrideWith((ref) => downloads),
      apiClientProvider.overrideWith(
          (ref) => throw StateError('Offline manager used server API')),
    ]);
    final subscription = await tester.runAsync(() async {
      await repository.database;
      await repository.file(key).writeAsBytes([1, 2, 3, 4]);
      await repository.add(item);
      final subscription =
          container.listen(offlineMediaListProvider, (_, __) {});
      await container.read(offlineMediaListProvider.future);
      return subscription;
    });
    await tester.pumpWidget(UncontrolledProviderScope(
        container: container,
        child: const MaterialApp(home: OfflineDownloadsScreen())));
    await tester.pumpAndSettle();
    expect(find.text('Saved audio.mp3'), findsOneWidget);
    expect(find.textContaining('https://saved.example:8443'), findsOneWidget);
    await tester.tap(find.byTooltip('Delete offline copy'));
    await tester.pumpAndSettle();
    expect(find.textContaining('The server file stays available.'),
        findsOneWidget);
    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();
    expect(repository.file(key).existsSync(), isTrue);

    // Failure to stop the native player leaves the copy available for retry.
    playback.refuseStop = true;
    await tester.tap(find.byTooltip('Delete offline copy'));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(FilledButton, 'Delete offline copy'));
    await tester.pumpAndSettle();
    expect(find.text('Could not delete the offline copy. Please try again.'),
        findsOneWidget);
    expect(repository.file(key).existsSync(), isTrue);

    playback.refuseStop = false;
    var stopped = false;
    await playback.claim(
        kind: PlaybackSourceKind.local,
        identity: 'offline:$key',
        sourceUri: repository.file(key).uri.toString(),
        stop: () async {
          stopped = true;
        });
    await tester.tap(find.byTooltip('Delete offline copy'));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(FilledButton, 'Delete offline copy'));
    // Advance both Flutter's fake clock and the real SQLite/filesystem work.
    for (var i = 0; i < 100; i++) {
      await tester.pump(const Duration(milliseconds: 10));
      await tester.runAsync(
          () => Future<void>.delayed(const Duration(milliseconds: 10)));
      if (!repository.file(key).existsSync() &&
          find.text('Saved audio.mp3').evaluate().isEmpty) {
        break;
      }
    }
    await tester.pumpAndSettle();
    expect(stopped, isTrue);
    expect(repository.file(key).existsSync(), isFalse);
    expect(
        find.text('No offline copies yet. Use Keep offline on a server file.'),
        findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox());
    subscription!.close();
    container.dispose();
    await tester.runAsync(repository.close);
    directory.deleteSync(recursive: true);
  });
}
