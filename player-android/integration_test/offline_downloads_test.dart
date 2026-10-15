import 'dart:async';
import 'dart:io';

import 'package:chewie/chewie.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:player_android/main.dart' as app;
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/audio_handler_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/offline_downloads_provider.dart';
import 'package:player_android/providers/playback_session_provider.dart';
import 'package:player_android/router.dart';
import 'package:player_android/screens/media_detail_screen.dart';
import 'package:player_android/screens/video_player_screen.dart';
import 'package:player_android/widgets/offline_image.dart';

const _origin = String.fromEnvironment('OFFLINE_E2E_ORIGIN',
    defaultValue: 'http://127.0.0.1:18765');
const _phase =
    String.fromEnvironment('OFFLINE_E2E_PHASE', defaultValue: 'online');
const _username = String.fromEnvironment('username');
const _password = String.fromEnvironment('password');

Future<void> waitFor(WidgetTester tester, bool Function() ready,
    {Duration timeout = const Duration(seconds: 45)}) async {
  final deadline = DateTime.now().add(timeout);
  while (!ready()) {
    if (DateTime.now().isAfter(deadline)) fail('Timed out in $_phase phase.');
    await tester.pump(const Duration(milliseconds: 150));
  }
}

void main() {
  final binding = IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('real server downloads and native offline playback ($_phase)',
      (tester) async {
    if (_phase == 'offline') {
      final client = HttpClient()
        ..connectionTimeout = const Duration(seconds: 2);
      try {
        await expectLater(
          client
              .getUrl(Uri.parse('$_origin/healthz'))
              .then((request) => request.close())
              .timeout(const Duration(seconds: 3)),
          throwsA(anyOf(isA<SocketException>(), isA<HttpException>(),
              isA<TimeoutException>())),
        );
      } finally {
        client.close(force: true);
      }
    }
    app.main();
    await waitFor(
        tester, () => find.byType(app.PlayerAndroidApp).evaluate().isNotEmpty);
    final container = ProviderScope.containerOf(
        tester.element(find.byType(app.PlayerAndroidApp)));
    final router = container.read(routerProvider);
    final repository = container.read(offlineDownloadRepositoryProvider);

    if (_phase == 'online') {
      expect(_username, isNotEmpty);
      expect(_password, isNotEmpty);
      await container.read(authStateProvider.notifier).switchServer(_origin);
      router.go(AppRoutes.login);
      await waitFor(tester,
          () => find.byKey(const Key('login_username')).evaluate().isNotEmpty);
      await tester.enterText(
          find.byKey(const Key('login_username')), _username);
      await tester.enterText(
          find.byKey(const Key('login_password')), _password);
      await tester.ensureVisible(find.byKey(const Key('login_submit')));
      await tester.tap(find.byKey(const Key('login_submit')));
      await waitFor(
          tester,
          () =>
              container.read(authStateProvider).valueOrNull?.isAuthenticated ==
              true);
      final api = container.read(apiClientProvider);
      final media = await api.listMedia(search: 'offline-');
      expect(media, hasLength(5));
      // Clean only previous fixture downloads in this disposable test app.
      for (final item in await repository.list()) {
        if (item.origin == _origin) {
          await container.read(offlineDownloadsProvider).remove(item.key);
        }
      }
      final hashes = <String, String>{};
      for (final item in media) {
        debugPrint('Offline E2E: downloading ${item.fileName}');
        router.go(AppRoutes.home);
        await waitFor(
            tester, () => find.byType(MediaDetailScreen).evaluate().isEmpty);
        router.go(AppRoutes.mediaDetailPath(item.id));
        await waitFor(tester, () {
          final buttons = find.byKey(const Key('keep_offline')).evaluate();
          return buttons.isNotEmpty &&
              (buttons.single.widget as OutlinedButton).onPressed != null;
        });
        await tester.ensureVisible(find.byKey(const Key('keep_offline')));
        await tester.tap(find.byKey(const Key('keep_offline')));
        await waitFor(
            tester,
            () => find
                .text('Available offline · Manage downloads')
                .evaluate()
                .isNotEmpty);
        final saved = (await repository.list()).singleWhere(
            (saved) => saved.mediaId == item.id && saved.origin == _origin);
        final bytes = await repository.file(saved.key).readAsBytes();
        expect(bytes.length, item.fileSizeBytes);
        hashes[item.fileName] = sha256.convert(bytes).toString();
        debugPrint('Offline E2E: saved ${item.fileName}');
      }
      router.go(AppRoutes.offlineDownloads);
      await waitFor(
          tester, () => find.text('offline-audio.mp3').evaluate().isNotEmpty);
      expect(await repository.list(), hasLength(5));
      binding.reportData = {'phase': 'online', 'download_sha256': hashes};
      // This marker lets the host stop the fixture before the next invocation.
      // ignore: avoid_print
      print('OFFLINE_E2E_DOWNLOADS_READY $hashes');
    } else {
      expect(_phase, 'offline');
      final copies = await repository.list();
      expect(
          copies, hasLength(5)); // Persists across a real app process restart.
      expect(container.read(authStateProvider).valueOrNull?.isAuthenticated,
          isNot(true));
      router.go(AppRoutes.offlineDownloads);
      await waitFor(
          tester, () => find.text('offline-audio.mp3').evaluate().isNotEmpty);

      await tester.tap(find.text('offline-audio.mp3'));
      await waitFor(
          tester,
          () =>
              find.byKey(const Key('audio_player_view')).evaluate().isNotEmpty);
      final audio = container.read(audioHandlerProvider).player;
      await waitFor(tester, () => audio.position.inMilliseconds > 250);
      expect(audio.duration?.inMilliseconds, greaterThan(1000));
      expect(find.text('Lyrics'), findsNothing);
      await container.read(playbackSessionCoordinatorProvider).stopCurrent();
      for (final title in ['offline-video.mp4', 'offline-transport.mp4']) {
        router.go(AppRoutes.offlineDownloads);
        await waitFor(tester, () => find.text(title).evaluate().isNotEmpty);
        await tester.tap(find.text(title));
        final screen = find.byWidgetPredicate((widget) =>
            widget is VideoPlayerScreen && widget.mediaTitle == title);
        final chewie =
            find.descendant(of: screen, matching: find.byType(Chewie));
        await waitFor(tester, () => chewie.evaluate().isNotEmpty);
        final video =
            tester.widget<Chewie>(chewie).controller.videoPlayerController;
        await waitFor(tester, () => video.value.position.inMilliseconds > 250);
        expect(video.value.isInitialized, isTrue);
        final copy = copies.singleWhere((item) => item.title == title);
        expect(video.dataSource, repository.file(copy.key).uri.toString());
        debugPrint('Offline E2E: native video playing $title');
        await container.read(playbackSessionCoordinatorProvider).stopCurrent();
      }

      for (final title in ['offline-image.png', 'offline-vector.svg']) {
        router.go(AppRoutes.offlineDownloads);
        await waitFor(tester, () => find.text(title).evaluate().isNotEmpty);
        await tester.tap(find.text(title));
        final image = find.byWidgetPredicate(
            (widget) => widget is OfflineImage && widget.title == title);
        await waitFor(tester, () => image.evaluate().isNotEmpty);
        await waitFor(
            tester,
            () => find
                .descendant(of: image, matching: find.byType(RawImage))
                .evaluate()
                .any((element) => (element.widget as RawImage).image != null));
        expect(find.text('Image unavailable'), findsNothing);
      }
      router.go(AppRoutes.offlineDownloads);
      await waitFor(tester,
          () => find.byTooltip('Delete offline copy').evaluate().isNotEmpty);
      for (final item in copies) {
        final row = find.byKey(ValueKey('offline_${item.key}'));
        // A removed row can disappear during the list's loading frame. Wait
        // for the next ready list before interacting with the following row.
        await waitFor(tester, () => row.evaluate().isNotEmpty);
        await tester.ensureVisible(row);
        await tester.tap(find.descendant(
            of: row, matching: find.byTooltip('Delete offline copy')));
        await waitFor(
            tester, () => find.byType(AlertDialog).evaluate().isNotEmpty);
        await tester
            .tap(find.widgetWithText(FilledButton, 'Delete offline copy'));
        await waitFor(tester, () => row.evaluate().isEmpty);
        expect(await repository.file(item.key).exists(), isFalse);
      }
      expect(await repository.list(), isEmpty);
      await waitFor(
          tester,
          () => find
              .text('No offline copies yet. Use Keep offline on a server file.')
              .evaluate()
              .isNotEmpty);
      expect(
          find.text(
              'No offline copies yet. Use Keep offline on a server file.'),
          findsOneWidget);
      binding.reportData = {
        'phase': 'offline',
        'native_audio': true,
        'native_video': true,
        'native_mpegts': true,
        'png': true,
        'svg': true,
        'removed_local_copies': 5
      };
    }
    expect(tester.takeException(), isNull);
  });
}
