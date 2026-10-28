import 'dart:io';
import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:player_android/app_routes.dart';
import 'package:player_android/screens/offline_playback_route.dart';
import 'package:player_android/models/offline_media.dart';
import 'package:player_android/models/music_metadata.dart';
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
        music: MusicMetadata.fromJson({
          'release_date': '2025-04',
          'original_date': '1979',
          'credits': [
            {
              'role': 'tape op',
              'names': ['A; B', '東京']
            }
          ]
        }),
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
    expect(find.textContaining('0 KB · Audio'), findsOneWidget);
    expect(find.textContaining('https://saved.example:8443'), findsNothing);
    expect(find.byKey(Key('offline_menu_$key')), findsOneWidget);
    expect(find.byTooltip('Music details'), findsNothing);
    expect(find.byTooltip('Notes'), findsNothing);
    expect(find.byTooltip('Remove download'), findsNothing);

    await tester.tap(find.byKey(Key('offline_menu_$key')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(Key('offline_details_$key')));
    await tester.pumpAndSettle();
    expect(find.text('Details'), findsWidgets);
    expect(find.textContaining('Origin: https://saved.example:8443'),
        findsOneWidget);
    expect(find.text('Account 7'), findsOneWidget);
    expect(find.text('Saved offline snapshot'), findsOneWidget);
    expect(find.text('Release date: 2025-04'), findsOneWidget);
    expect(find.text('Original date: 1979'), findsOneWidget);
    expect(find.text('tape op: A; B\n東京'), findsOneWidget);
    await tester.tap(find.text('Close'));
    await tester.pumpAndSettle();
    expect(find.text('Saved offline snapshot'), findsNothing);

    await tester.tap(find.byKey(Key('offline_menu_$key')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(Key('offline_remove_$key')));
    await tester.pumpAndSettle();
    expect(find.textContaining('The server file stays available.'),
        findsOneWidget);
    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();
    expect(repository.file(key).existsSync(), isTrue);

    // Failure to stop the native player leaves the copy available for retry.
    playback.refuseStop = true;
    await tester.tap(find.byKey(Key('offline_menu_$key')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(Key('offline_remove_$key')));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(FilledButton, 'Remove download'));
    await tester.pumpAndSettle();
    expect(find.text('Could not remove the download. Please try again.'),
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
    await tester.tap(find.byKey(Key('offline_menu_$key')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(Key('offline_remove_$key')));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(FilledButton, 'Remove download'));
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
  testWidgets('retained Downloads reloads an image favorite after viewer Back',
      (tester) async {
    final directory =
        Directory.systemTemp.createTempSync('personal-favorite-return-');
    final repository = (await tester.runAsync(
        () async => OfflineDownloadRepository(directory: directory)))!;
    final downloads = OfflineDownloads(repository, _StopControl());
    final key = offlineMediaKey('https://saved.test', 1, 10);
    final bytes = base64Decode(
        'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a0e0AAAAASUVORK5CYII=');
    await tester.runAsync(() async {
      await repository.database;
      await repository.file(key).writeAsBytes(bytes);
      await repository.add(OfflineMedia(
          key: key,
          origin: 'https://saved.test',
          userId: 1,
          mediaId: 10,
          title: 'Saved image.png',
          type: 'image',
          sizeBytes: bytes.length,
          addedAt: DateTime.now()));
    });
    final container = ProviderContainer(overrides: [
      offlineDownloadRepositoryProvider.overrideWithValue(repository),
      offlineDownloadsProvider.overrideWith((ref) => downloads),
      apiClientProvider
          .overrideWith((ref) => throw StateError('Offline favorite read API')),
    ]);
    final router =
        GoRouter(initialLocation: AppRoutes.offlineDownloads, routes: [
      GoRoute(
          path: AppRoutes.offlineDownloads,
          builder: (_, __) => const OfflineDownloadsScreen()),
      GoRoute(
          path: AppRoutes.offlinePlayer,
          builder: (_, state) => OfflinePlaybackRoute(
              downloadKey: state.pathParameters['downloadKey']!)),
    ]);
    Future<void> waitFor(bool Function() ready) async {
      for (var i = 0; i < 150 && !ready(); i++) {
        await tester.pump(const Duration(milliseconds: 20));
        await tester.runAsync(
            () => Future<void>.delayed(const Duration(milliseconds: 10)));
      }
      expect(ready(), isTrue);
    }

    await tester.pumpWidget(UncontrolledProviderScope(
        container: container, child: MaterialApp.router(routerConfig: router)));
    await waitFor(
        () => find.byKey(Key('offline_menu_$key')).evaluate().isNotEmpty);
    await tester.tap(find.byKey(Key('offline_menu_$key')));
    await tester.pumpAndSettle();
    expect(find.text('Add to favorites'), findsOneWidget);
    // Close the menu without changing favorite state.
    await tester.tapAt(const Offset(1, 1));
    await tester.pumpAndSettle();

    await tester.tap(find.text('Saved image.png'));
    await waitFor(() => find
        .descendant(
            of: find.byType(OfflinePlaybackRoute),
            matching: find.byTooltip('Add to favorites'))
        .evaluate()
        .isNotEmpty);
    await tester.pump(const Duration(milliseconds: 500));
    await tester.tap(find.byTooltip('Add to favorites'));
    await waitFor(
        () => find.byTooltip('Remove from favorites').evaluate().isNotEmpty);
    await tester.binding.handlePopRoute();
    await waitFor(() =>
        find.byType(OfflinePlaybackRoute).evaluate().isEmpty &&
        find.byKey(Key('offline_menu_$key')).evaluate().isNotEmpty);
    await tester.pump(const Duration(milliseconds: 500));
    await tester.tap(find.byKey(Key('offline_menu_$key')));
    await tester.pumpAndSettle();
    expect(find.text('Remove from favorites'), findsOneWidget);
    await tester.tap(find.byKey(Key('offline_favorite_$key')));
    await waitFor(() {
      // Menu closes after selection; reopen once favorite reload settles.
      return find.text('Remove from favorites').evaluate().isEmpty;
    });
    await tester.pump(const Duration(milliseconds: 500));
    await tester.tap(find.byKey(Key('offline_menu_$key')));
    await tester.pumpAndSettle();
    expect(find.text('Add to favorites'), findsOneWidget);
    expect(
        await tester
            .runAsync(() => repository.personalStore(key).getFavorite()),
        isFalse);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox.shrink());
    router.dispose();
    container.dispose();
    await tester.runAsync(repository.close);
    directory.deleteSync(recursive: true);
  });
}
