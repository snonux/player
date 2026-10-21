import 'package:chewie/chewie.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:player_android/main.dart' as app;
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/audio_handler_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/playback_session_provider.dart';
import 'package:player_android/router.dart';
import 'package:player_android/screens/image_viewer_screen.dart';

const _origin = String.fromEnvironment('VIDEO_E2E_ORIGIN',
    defaultValue: 'http://127.0.0.1:18765');

Future<void> _wait(WidgetTester tester, bool Function() ready) async {
  final deadline = DateTime.now().add(const Duration(seconds: 60));
  while (!ready()) {
    if (DateTime.now().isAfter(deadline)) fail('Playback did not become ready');
    await tester.pump(const Duration(milliseconds: 100));
  }
}

void main() {
  final binding = IntegrationTestWidgetsFlutterBinding.ensureInitialized();
  testWidgets('every supported server media format plays on Android',
      (tester) async {
    app.main();
    await _wait(
        tester, () => find.byType(app.PlayerAndroidApp).evaluate().isNotEmpty);
    final container = ProviderScope.containerOf(
        tester.element(find.byType(app.PlayerAndroidApp)));
    final router = container.read(routerProvider);
    await container.read(authStateProvider.notifier).switchServer(_origin);
    router.go(AppRoutes.login);
    await _wait(tester,
        () => find.byKey(const Key('login_username')).evaluate().isNotEmpty);
    await tester.enterText(find.byKey(const Key('login_username')),
        const String.fromEnvironment('username'));
    await tester.enterText(find.byKey(const Key('login_password')),
        const String.fromEnvironment('password'));
    await tester.tap(find.byKey(const Key('login_submit')));
    await _wait(
        tester,
        () =>
            container.read(authStateProvider).valueOrNull?.isAuthenticated ==
            true);
    final api = container.read(apiClientProvider);
    final media = await api.listMedia(limit: 100);
    final expected = <String>{
      for (final ext in [
        'mp4',
        'mkv',
        'avi',
        'mov',
        'wmv',
        'asf',
        'flv',
        'webm',
        'mpg',
        'mpeg',
        'mp3',
        'wav',
        'flac',
        'aac',
        'ogg',
        'm4a',
        'wma',
        'm4b',
        'opus',
        'jpg',
        'jpeg',
        'png',
        'gif',
        'webp',
        'bmp',
        'avif',
        'svg'
      ])
        'sample-$ext.$ext',
      'sample-transport.mp4',
    };
    expect(media.map((m) => m.fileName).toSet(), expected);
    for (final name in ['sample-mpg.mpg', 'sample-mpeg.mpeg']) {
      expect(media.singleWhere((m) => m.fileName == name).transcoded, isTrue);
    }
    final results = <String, String>{};
    for (final item in media) {
      debugPrint(
          'Formats E2E: opening ${item.fileName} (${item.codec}, converted=${item.transcoded})');
      try {
        router.go(
            item.type == 'image'
                ? AppRoutes.imageViewerPath('${item.id}')
                : AppRoutes.playerPathForType(item.type, '${item.id}'),
            extra: {
              'mediaUrl': api.playbackUrl(item),
              'title': item.displayTitle,
              'sourceName': item.relPath.isEmpty ? item.fileName : item.relPath,
              'startPosition': 0.0,
            });
        await tester.pump(const Duration(milliseconds: 100));
        if (item.type == 'image') {
          await _wait(
              tester,
              () => find
                  .descendant(
                      of: find.byType(ImageViewerScreen),
                      matching: find.byType(RawImage))
                  .evaluate()
                  .any((e) => (e.widget as RawImage).image != null));
          expect(find.text('Image unavailable'), findsNothing);
        } else if (item.type == 'audio') {
          await _wait(
              tester,
              () => find
                  .byKey(const Key('audio_player_view'))
                  .evaluate()
                  .isNotEmpty);
          final audio = container.read(audioHandlerProvider).player;
          await _wait(tester, () => audio.position.inMilliseconds > 1200);
          expect(audio.duration?.inMilliseconds, greaterThan(2000));
          await audio.pause();
          await audio.seek(const Duration(seconds: 3));
          await _wait(tester, () => audio.position.inSeconds >= 3);
        } else {
          await _wait(tester, () => find.byType(Chewie).evaluate().isNotEmpty);
          final video = tester
              .widget<Chewie>(find.byType(Chewie))
              .controller
              .videoPlayerController;
          await _wait(tester, () => video.value.position.inMilliseconds > 1200);
          expect(video.value.errorDescription, isNull);
          expect(video.value.size.width, greaterThan(0));
          await video.pause();
          await video.seekTo(const Duration(seconds: 3));
          await _wait(tester,
              () => (video.value.position.inMilliseconds - 3000).abs() < 400);
          expect(video.value.isPlaying, isFalse);
          await video.play();
          await _wait(tester, () => video.value.position.inMilliseconds > 3500);
        }
        results[item.fileName] = 'PASS';
        debugPrint('Formats E2E: PASS ${item.fileName}');
      } catch (error) {
        results[item.fileName] = 'FAIL: $error';
        debugPrint('Formats E2E: FAIL ${item.fileName}: $error');
      } finally {
        await container.read(playbackSessionCoordinatorProvider).stopCurrent();
        router.go(AppRoutes.home);
        await tester.pumpAndSettle();
      }
    }
    // A one-use share must survive the player's multiple ranged requests.
    for (final name in [
      'sample-transport.mp4',
      'sample-mpg.mpg',
      'sample-mpeg.mpeg',
    ]) {
      final sharedItem = media.singleWhere((m) => m.fileName == name);
      final share = await api.createShare(sharedItem.id, maxUses: 1);
      try {
        router.go(AppRoutes.shareViewerPath(share.token));
        await _wait(
            tester,
            () => find
                .byKey(const Key('share_viewer_play_button'))
                .evaluate()
                .isNotEmpty);
        await tester.tap(find.byKey(const Key('share_viewer_play_button')));
        await _wait(tester, () => find.byType(Chewie).evaluate().isNotEmpty);
        final video = tester
            .widget<Chewie>(find.byType(Chewie))
            .controller
            .videoPlayerController;
        await _wait(tester, () => video.value.position.inMilliseconds > 1200);
        expect(Uri.parse(video.dataSource).path,
            '/s/${share.token}/${sharedItem.transcoded ? 'compat' : 'download'}');
        expect(Uri.parse(video.dataSource).queryParameters['view'], isNotEmpty);
        await video.pause();
        await video.seekTo(const Duration(seconds: 4));
        await _wait(tester,
            () => (video.value.position.inMilliseconds - 4000).abs() < 400);
        expect(video.value.isPlaying, isFalse);
        await video.play();
        await _wait(tester, () => video.value.position.inMilliseconds > 4500);
        expect(
            (await api.listSharesForMedia(sharedItem.id))
                .singleWhere((s) => s.token == share.token)
                .usedCount,
            1);
        results['single-use share $name'] = 'PASS';
        debugPrint('Formats E2E: PASS single-use share $name');
      } finally {
        await container.read(playbackSessionCoordinatorProvider).stopCurrent();
        router.go(AppRoutes.home);
        await tester.pumpAndSettle();
        await api.revokeShare(share.token);
      }
    }
    binding.reportData = results;
    await container.read(authStateProvider.notifier).logout();
    expect(results.values.where((v) => v != 'PASS'), isEmpty,
        reason: '$results');
    expect(tester.takeException(), isNull);
  }, timeout: const Timeout(Duration(minutes: 12)));
}
