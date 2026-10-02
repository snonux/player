import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:just_audio/just_audio.dart';
import 'package:sqflite_common_ffi/sqflite_ffi.dart';

import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/audio_handler_provider.dart';
import 'package:player_android/providers/local_library_provider.dart';
import 'package:player_android/providers/progress_queue_provider.dart';
import 'package:player_android/screens/audio_player_screen.dart';
import 'package:player_android/screens/local_playback_route.dart';
import 'package:player_android/services/audio_handler.dart';
import 'package:player_android/services/local_library_repository.dart';

class _ReadyPlayer extends AudioPlayer {
  Duration elapsed = Duration.zero;
  bool isPlaying = false;
  AudioSource? loadedSource;

  @override
  Future<Duration?> setAudioSource(AudioSource source,
      {bool preload = true,
      int? initialIndex,
      Duration? initialPosition}) async {
    loadedSource = source;
    return const Duration(seconds: 100);
  }

  @override
  Duration get position => elapsed;
  @override
  Duration? get duration => const Duration(seconds: 100);
  @override
  bool get playing => isPlaying;
  @override
  Stream<bool> get playingStream => const Stream.empty();
  @override
  Stream<PlaybackEvent> get playbackEventStream => const Stream.empty();
  @override
  Stream<ProcessingState> get processingStateStream => const Stream.empty();
  @override
  Future<void> seek(Duration? position, {int? index}) async {
    elapsed = position ?? Duration.zero;
  }

  @override
  Future<void> play() async {
    isPlaying = true;
  }

  @override
  Future<void> stop() async {
    isPlaying = false;
  }
}

class _PendingAudioHandler extends PlayerAudioHandler {
  _PendingAudioHandler() : super(AudioPlayer());

  @override
  Future<void> loadSourceForSession(AudioSource source, int generation) =>
      Completer<void>().future;

  @override
  Future<void> stop() async {}
}

Future<void> _showRoute(
  WidgetTester tester,
  ProviderContainer container,
  int id,
) async {
  // Start SQLite work in the real async zone before the widget subscribes.
  // Waiting on a future started in FakeAsync from runAsync can deadlock.
  final subscription = await tester.runAsync(() async {
    final subscription =
        container.listen(localMediaByIdProvider(id), (_, __) {});
    await container.read(localMediaByIdProvider(id).future);
    return subscription;
  });
  await tester.pumpWidget(UncontrolledProviderScope(
    container: container,
    child: MaterialApp(home: LocalPlaybackRoute(localMediaId: id)),
  ));
  subscription!.close();
  await tester.pump(const Duration(milliseconds: 1));
}

void main() {
  setUpAll(() {
    sqfliteFfiInit();
    databaseFactory = databaseFactoryFfi;
  });

  testWidgets('durable ID reloads relinked media and rejects a removed record',
      (tester) async {
    final database = LocalLibraryDatabase(databasePath: inMemoryDatabasePath);
    final container = ProviderContainer(overrides: [
      localLibraryDatabaseProvider.overrideWithValue(database),
      audioHandlerProvider.overrideWithValue(_PendingAudioHandler()),
      apiClientProvider
          .overrideWith((_) => throw StateError('Server API read')),
      progressQueueProvider
          .overrideWith((_) => throw StateError('Server queue read')),
      tokenStorageProvider
          .overrideWith((_) => throw StateError('Server token read')),
    ]);
    addTearDown(container.dispose);
    addTearDown(() => tester.runAsync(database.close));
    final media = container.read(localMediaRepositoryProvider);
    final item = (await tester.runAsync(() => media.importDocument(
          uri: 'content://provider/original',
          title: 'Original',
          mimeType: 'audio/mpeg',
        )))!;

    await _showRoute(tester, container, item.id);
    expect(
        tester
            .widget<AudioPlayerScreen>(find.byType(AudioPlayerScreen))
            .request!
            .sourceUri
            .toString(),
        item.uri);
    await tester.pumpWidget(UncontrolledProviderScope(
        container: container, child: const SizedBox.shrink()));
    await tester.pump(const Duration(milliseconds: 1));
    await tester.runAsync(() => media.relink(
          id: item.id,
          uri: 'content://provider/replacement',
          title: 'Replacement',
          mimeType: 'audio/mpeg',
        ));

    await _showRoute(tester, container, item.id);
    expect(
        tester
            .widget<AudioPlayerScreen>(find.byType(AudioPlayerScreen))
            .request!
            .sourceUri
            .toString(),
        'content://provider/replacement');
    await tester.pumpWidget(UncontrolledProviderScope(
        container: container, child: const SizedBox.shrink()));
    await tester.pump(const Duration(milliseconds: 1));
    await tester.runAsync(() => media.remove(item.id));
    await _showRoute(tester, container, item.id);
    expect(find.text('This item is no longer in the local library.'),
        findsOneWidget);
    expect(find.byType(AudioPlayerScreen), findsNothing);
  });

  testWidgets('completed local replay starts at zero and clears completion',
      (tester) async {
    final database = LocalLibraryDatabase(databasePath: inMemoryDatabasePath);
    final player = _ReadyPlayer();
    final handler = PlayerAudioHandler(player);
    final container = ProviderContainer(overrides: [
      localLibraryDatabaseProvider.overrideWithValue(database),
      audioHandlerProvider.overrideWithValue(handler),
      apiClientProvider
          .overrideWith((_) => throw StateError('Server API read')),
      progressQueueProvider
          .overrideWith((_) => throw StateError('Server queue read')),
      tokenStorageProvider
          .overrideWith((_) => throw StateError('Server token read')),
    ]);
    addTearDown(container.dispose);
    addTearDown(() => tester.runAsync(database.close));
    addTearDown(() => tester.runAsync(handler.stop));
    final media = container.read(localMediaRepositoryProvider);
    final progress = container.read(localProgressRepositoryProvider);
    final item = (await tester.runAsync(() => media.importDocument(
          uri: 'content://provider/finished',
          title: 'Finished',
          mimeType: 'audio/mpeg',
        )))!;
    await tester.runAsync(() async {
      await progress.savePosition(item.id, 96);
      await progress.markFinished(item.id);
    });
    await _showRoute(tester, container, item.id);
    for (var attempt = 0; attempt < 100 && !player.isPlaying; attempt++) {
      await tester.pump(const Duration(milliseconds: 1));
      await tester.runAsync(
          () async => Future<void>.delayed(const Duration(milliseconds: 10)));
    }
    expect(player.isPlaying, isTrue);
    expect(player.elapsed, Duration.zero);
    expect((player.loadedSource as UriAudioSource).uri.toString(), item.uri);
    await tester.runAsync(handler.stop);
    final saved = await tester.runAsync(() => progress.get(item.id));
    expect(saved!.positionSeconds, 0);
    expect(saved.finished, isFalse);
    await tester.pumpWidget(UncontrolledProviderScope(
        container: container, child: const SizedBox.shrink()));
  });
}
