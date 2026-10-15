import 'dart:async';
import 'dart:io';
import 'dart:convert';

import 'package:chewie/chewie.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:player_android/main.dart' as app;
import 'package:player_android/models/media.dart';
import 'package:player_android/models/offline_media.dart';
import 'package:player_android/screens/offline_downloads_screen.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/audio_handler_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/offline_downloads_provider.dart';
import 'package:player_android/providers/playback_session_provider.dart';
import 'package:player_android/router.dart';
import 'package:player_android/screens/media_detail_screen.dart';
import 'package:player_android/screens/notes_editor_screen.dart';
import 'package:player_android/widgets/offline_image.dart';

const _origin = String.fromEnvironment('PERSONAL_E2E_ORIGIN',
    defaultValue: 'http://127.0.0.1:18765');
const _phase =
    String.fromEnvironment('PERSONAL_E2E_PHASE', defaultValue: 'online');
const _username = String.fromEnvironment('username');
const _password = String.fromEnvironment('password');

Future<void> _wait(WidgetTester tester, bool Function() ready) async {
  final deadline = DateTime.now().add(const Duration(seconds: 45));
  while (!ready()) {
    if (DateTime.now().isAfter(deadline)) {
      fail('Timed out in $_phase personal-media test.');
    }
    await tester.pump(const Duration(milliseconds: 100));
  }
}

Future<void> _tap(WidgetTester tester, Finder finder) async {
  await _wait(tester, () => finder.evaluate().isNotEmpty);
  await tester.pump(const Duration(milliseconds: 350));
  await tester.ensureVisible(finder);
  await tester.tap(finder);
  await tester.pump(const Duration(milliseconds: 250));
}

Future<void> _note(WidgetTester tester, String content,
    {bool existing = false}) async {
  // Reused on real server and offline players; Back must flush before returning.
  await _tap(tester, find.byTooltip('Notes'));
  await _wait(tester,
      () => find.byKey(const Key('notes_text_field')).evaluate().isNotEmpty);
  if (existing) {
    expect(
        tester
            .widget<TextField>(find.byKey(const Key('notes_text_field')))
            .controller!
            .text,
        content);
  } else {
    await tester.enterText(find.byKey(const Key('notes_text_field')), content);
  }
  await tester.binding.handlePopRoute();
  await _wait(tester, () => find.byType(NotesEditorScreen).evaluate().isEmpty);
}

Future<void> _bookmark(WidgetTester tester, String label) async {
  await _tap(tester, find.byTooltip('Bookmarks'));
  await tester.enterText(find.byKey(const Key('bookmark_label')), label);
  await _tap(tester, find.text('Save current position'));
  await _wait(
      tester, () => find.widgetWithText(ListTile, label).evaluate().isNotEmpty);
  await tester.binding.handlePopRoute();
  await tester.pump(const Duration(milliseconds: 250));
}

void main() {
  final binding = IntegrationTestWidgetsFlutterBinding.ensureInitialized();
  testWidgets(
      'native personal media controls and restart persistence ($_phase)',
      (tester) async {
    if (_phase == 'offline') {
      final client = HttpClient()
        ..connectionTimeout = const Duration(seconds: 2);
      try {
        await expectLater(
            client
                .getUrl(Uri.parse('$_origin/healthz'))
                .then((r) => r.close())
                .timeout(const Duration(seconds: 3)),
            throwsA(anyOf(isA<SocketException>(), isA<HttpException>(),
                isA<TimeoutException>())));
      } finally {
        client.close(force: true);
      }
    }
    app.main();
    await _wait(
        tester, () => find.byType(app.PlayerAndroidApp).evaluate().isNotEmpty);
    final container = ProviderScope.containerOf(
        tester.element(find.byType(app.PlayerAndroidApp)));
    final router = container.read(routerProvider);
    final repository = container.read(offlineDownloadRepositoryProvider);
    if (_phase == 'favorite') {
      final key = offlineMediaKey(_origin, 123456, 987654321);
      final bytes = base64Decode(
          'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a0e0AAAAASUVORK5CYII=');
      await repository.database;
      await repository.remove(key);
      await repository.file(key).writeAsBytes(bytes);
      await repository.add(OfflineMedia(
          key: key,
          origin: _origin,
          userId: 123456,
          mediaId: 987654321,
          title: 'favorite-return.png',
          type: 'image',
          sizeBytes: bytes.length,
          addedAt: DateTime.now()));
      router.go(AppRoutes.offlineDownloads);
      await _tap(tester, find.text('favorite-return.png'));
      await _wait(
          tester, () => find.byType(OfflineImage).evaluate().isNotEmpty);
      await _tap(tester, find.byTooltip('Add to favorites'));
      await _wait(tester,
          () => find.byTooltip('Remove from favorites').evaluate().isNotEmpty);
      await tester.binding.handlePopRoute();
      await _wait(
          tester,
          () =>
              find.byType(OfflineDownloadsScreen).evaluate().isNotEmpty &&
              find.byTooltip('Remove from favorites').evaluate().isNotEmpty);
      await _tap(tester, find.byTooltip('Remove from favorites'));
      await _wait(tester,
          () => find.byTooltip('Add to favorites').evaluate().isNotEmpty);
      expect(await repository.personalStore(key).getFavorite(), isFalse);
      await container.read(offlineDownloadsProvider).remove(key);
      binding.reportData = {
        'phase': 'favorite',
        'retained_downloads_favorite_return': true
      };
      expect(tester.takeException(), isNull);
      return;
    }
    if (_phase == 'online') {
      await container.read(authStateProvider.notifier).switchServer(_origin);
      router.go(AppRoutes.login);
      await _wait(tester,
          () => find.byKey(const Key('login_username')).evaluate().isNotEmpty);
      await tester.enterText(
          find.byKey(const Key('login_username')), _username);
      await tester.enterText(
          find.byKey(const Key('login_password')), _password);
      await _tap(tester, find.byKey(const Key('login_submit')));
      await _wait(
          tester,
          () =>
              container.read(authStateProvider).valueOrNull?.isAuthenticated ==
              true);
      final api = container.read(apiClientProvider);
      final media = await api.listMedia(search: 'personal-');
      expect(media.length, 2);
      final audioItem = media.singleWhere((item) => item.type == 'audio');
      final videoItem = media.singleWhere((item) => item.type == 'video');
      final image = (await api.listMedia(search: 'offline-image.png')).single;
      // Private fixture only: clear just these annotations from previous runs.
      for (final item in media) {
        for (final bookmark in await api.listBookmarks(item.id)) {
          await api.deleteBookmark(item.id, bookmark.id);
        }
      }
      for (final item in await repository.list()) {
        if (item.origin == _origin) {
          await container.read(offlineDownloadsProvider).remove(item.key);
        }
      }
      router.go(AppRoutes.audioPlayerPath(audioItem.id.toString()), extra: {
        'mediaUrl': api.playbackUrl(audioItem),
        'mediaTitle': audioItem.fileName
      });
      await _wait(
          tester,
          () =>
              find.byKey(const Key('audio_player_view')).evaluate().isNotEmpty);
      final audio = container.read(audioHandlerProvider);
      await _wait(tester, () => audio.player.position.inMilliseconds > 100);
      await _tap(tester, find.byKey(const Key('audio_player_play_pause')));
      await _tap(tester, find.byKey(const Key('audio_player_skip_forward')));
      final savedSeconds = audio.player.position.inMilliseconds / 1000;
      expect(savedSeconds, greaterThan(14));
      await _bookmark(tester, 'Native chapter');
      expect((await api.listBookmarks(audioItem.id)).single.label,
          'Native chapter');
      await _tap(tester, find.byKey(const Key('audio_player_skip_back')));
      await _tap(tester, find.byTooltip('Bookmarks'));
      await _tap(tester, find.text('Native chapter'));
      await _wait(tester, () => audio.player.position.inSeconds >= 14);
      await _tap(tester, find.byTooltip('Bookmarks'));
      await _tap(tester, find.byTooltip('Delete bookmark'));
      await _wait(tester,
          () => find.text('No saved positions yet.').evaluate().isNotEmpty);
      await tester.binding.handlePopRoute();
      await tester.pump(const Duration(milliseconds: 250));
      expect(await api.listBookmarks(audioItem.id), isEmpty);
      await _note(tester, 'Native server note');
      await _note(tester, 'Native server note', existing: true);
      expect((await api.getNote(audioItem.id))?.content, 'Native server note');
      await container.read(playbackSessionCoordinatorProvider).stopCurrent();

      router.go(AppRoutes.videoPlayerPath(videoItem.id.toString()), extra: {
        'mediaUrl': api.playbackUrl(videoItem),
        'mediaTitle': videoItem.fileName
      });
      await _wait(tester, () => find.byType(Chewie).evaluate().isNotEmpty);
      final video = tester
          .widget<Chewie>(find.byType(Chewie))
          .controller
          .videoPlayerController;
      await video.pause();
      await video.seekTo(const Duration(seconds: 17));
      await _bookmark(tester, 'Video chapter');
      await video.seekTo(Duration.zero);
      await _tap(tester, find.byTooltip('Bookmarks'));
      await _tap(tester, find.text('Video chapter'));
      await _wait(tester, () => video.value.position.inSeconds == 17);
      await _note(tester, 'Native video note');
      await container.read(playbackSessionCoordinatorProvider).stopCurrent();

      for (final item in <Media>[audioItem, videoItem, image]) {
        router.go(AppRoutes.home);
        await tester.pump(const Duration(milliseconds: 200));
        router.go(AppRoutes.mediaDetailPath(item.id));
        await _wait(
            tester,
            () =>
                find.byType(MediaDetailScreen).evaluate().isNotEmpty &&
                find.byKey(const Key('keep_offline')).evaluate().isNotEmpty);
        await _wait(
            tester,
            () =>
                (tester.widget<OutlinedButton>(
                        find.byKey(const Key('keep_offline'))))
                    .onPressed !=
                null);
        await _tap(tester, find.byKey(const Key('keep_offline')));
        await _wait(
            tester,
            () => find
                .text('Available offline · Manage downloads')
                .evaluate()
                .isNotEmpty);
      }
      final copies = await repository.list();
      expect(copies.length, 3);
      final offlineAudio = copies.singleWhere((item) => item.type == 'audio');
      router.go(AppRoutes.offlinePlayerPath(offlineAudio.key));
      await _wait(
          tester,
          () =>
              find.byKey(const Key('audio_player_view')).evaluate().isNotEmpty);
      await _tap(tester, find.byKey(const Key('audio_player_play_pause')));
      await _tap(tester, find.byKey(const Key('audio_player_skip_forward')));
      await _bookmark(tester, 'Offline chapter');
      await _note(tester, 'Offline audio note');
      await container.read(playbackSessionCoordinatorProvider).stopCurrent();
      final offlineVideo = copies.singleWhere((item) => item.type == 'video');
      router.go(AppRoutes.offlinePlayerPath(offlineVideo.key));
      await _wait(tester, () => find.byType(Chewie).evaluate().isNotEmpty);
      final savedVideo = tester
          .widget<Chewie>(find.byType(Chewie))
          .controller
          .videoPlayerController;
      await savedVideo.pause();
      await savedVideo.seekTo(const Duration(seconds: 21));
      await _bookmark(tester, 'Offline video chapter');
      await _note(tester, 'Offline video note');
      await container.read(playbackSessionCoordinatorProvider).stopCurrent();
      final offlineImage = copies.singleWhere((item) => item.type == 'image');
      router.go(AppRoutes.offlinePlayerPath(offlineImage.key));
      await _wait(
          tester, () => find.byType(OfflineImage).evaluate().isNotEmpty);
      await _tap(tester, find.byTooltip('Add to favorites'));
      await _wait(tester,
          () => find.byTooltip('Remove from favorites').evaluate().isNotEmpty);
      await _note(tester, 'Offline image note');
      await _note(tester, 'Offline image note', existing: true);
      expect(await repository.personalStore(offlineImage.key).getFavorite(),
          isTrue);
      // Do not logout: the next invocation starts with its persisted login locked.
      binding.reportData = {
        'phase': 'online',
        'server_audio_bookmark_save_seek_delete': true,
        'server_video_bookmark_seek': true,
        'notes_back_flush': true,
        'offline_image_favorite': true
      };
    } else {
      final copies = await repository.list();
      expect(copies.length, 3);
      expect(container.read(authStateProvider).valueOrNull?.isAuthenticated,
          isNot(true));
      final audioCopy = copies.singleWhere((item) => item.type == 'audio');
      router.go(AppRoutes.offlinePlayerPath(audioCopy.key));
      await _wait(
          tester,
          () =>
              find.byKey(const Key('audio_player_view')).evaluate().isNotEmpty);
      await _tap(tester, find.byKey(const Key('audio_player_play_pause')));
      await _tap(tester, find.byTooltip('Bookmarks'));
      await _wait(
          tester, () => find.text('Offline chapter').evaluate().isNotEmpty);
      await _tap(tester, find.text('Offline chapter'));
      final audio = container.read(audioHandlerProvider).player;
      await _wait(tester, () => audio.position.inSeconds >= 14);
      await _note(tester, 'Offline audio note', existing: true);
      await _tap(tester, find.byTooltip('Bookmarks'));
      await _tap(tester, find.byTooltip('Delete bookmark'));
      await _wait(tester,
          () => find.text('No saved positions yet.').evaluate().isNotEmpty);
      await tester.binding.handlePopRoute();
      await tester.pump(const Duration(milliseconds: 250));
      await container.read(playbackSessionCoordinatorProvider).stopCurrent();
      final videoCopy = copies.singleWhere((item) => item.type == 'video');
      router.go(AppRoutes.offlinePlayerPath(videoCopy.key));
      await _wait(tester, () => find.byType(Chewie).evaluate().isNotEmpty);
      final video = tester
          .widget<Chewie>(find.byType(Chewie))
          .controller
          .videoPlayerController;
      await video.pause();
      await _tap(tester, find.byTooltip('Bookmarks'));
      await _tap(tester, find.text('Offline video chapter'));
      await _wait(tester, () => video.value.position.inSeconds == 21);
      await _note(tester, 'Offline video note', existing: true);
      await _tap(tester, find.byTooltip('Bookmarks'));
      await _tap(tester, find.byTooltip('Delete bookmark'));
      await _wait(tester,
          () => find.text('No saved positions yet.').evaluate().isNotEmpty);
      await tester.binding.handlePopRoute();
      await tester.pump(const Duration(milliseconds: 250));
      await container.read(playbackSessionCoordinatorProvider).stopCurrent();
      final image = copies.singleWhere((item) => item.type == 'image');
      router.go(AppRoutes.offlinePlayerPath(image.key));
      await _wait(tester,
          () => find.byTooltip('Remove from favorites').evaluate().isNotEmpty);
      await _note(tester, 'Offline image note', existing: true);
      await _tap(tester, find.byTooltip('Remove from favorites'));
      expect(await repository.personalStore(image.key).getFavorite(), isFalse);
      for (final item in copies) {
        await container.read(offlineDownloadsProvider).remove(item.key);
      }
      expect(await repository.list(), isEmpty);
      binding.reportData = {
        'phase': 'offline',
        'real_process_restart': true,
        'server_unreachable': true,
        'bookmark_seek_delete': true,
        'notes_persisted': true,
        'image_favorite_persisted': true
      };
    }
    expect(tester.takeException(), isNull);
  });
}
