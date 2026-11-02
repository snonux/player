import 'package:chewie/chewie.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:player_android/main.dart' as app;
import 'package:player_android/models/media.dart';
import 'package:player_android/providers/api_client_provider.dart';
import 'package:player_android/providers/audio_handler_provider.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/router.dart';

const _origin = String.fromEnvironment('VIDEO_E2E_ORIGIN');

Future<void> _wait(WidgetTester tester, bool Function() ready,
    {String reason = 'native playback', int seconds = 60}) async {
  final deadline = DateTime.now().add(Duration(seconds: seconds));
  while (!ready()) {
    if (DateTime.now().isAfter(deadline)) fail('Timed out: $reason');
    await tester.pump(const Duration(milliseconds: 100));
  }
}

ChewieController? _video(WidgetTester tester) {
  final found = find.byType(Chewie, skipOffstage: false);
  return found.evaluate().isEmpty
      ? null
      : tester.widget<Chewie>(found).controller;
}

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();
  testWidgets(
      'real native video fullscreen transitions and folder audio shuffle',
      (tester) async {
    expect(_origin, isNotEmpty,
        reason: 'Use the disposable continuous_video_fixture.py server');
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
    FocusManager.instance.primaryFocus?.unfocus();
    await tester.pump(const Duration(milliseconds: 500));
    await tester.ensureVisible(find.byKey(const Key('login_submit')));
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(find.byKey(const Key('login_submit')));
    await _wait(
        tester,
        () =>
            container.read(authStateProvider).valueOrNull?.isAuthenticated ==
            true);
    final api = container.read(apiClientProvider);
    final sets = await api.listSets();
    final videoSet = sets.singleWhere((set) => set.name == 'Videos');
    final videos = (await api.listMedia(setId: videoSet.id, limit: 100))
        .where((media) => media.type == 'video')
        .toList()
      ..sort((a, b) => a.fileName.compareTo(b.fileName));
    expect(videos.length, 3);
    router.go(AppRoutes.videoPlayerPath('${videos.first.id}'), extra: {
      'mediaUrl': api.playbackUrl(videos.first),
      'title': videos.first.fileName,
      'sequence': videos,
    });
    await _wait(
        tester,
        () =>
            _video(tester)
                    ?.videoPlayerController
                    .value
                    .position
                    .inMilliseconds !=
                null &&
            _video(tester)!
                    .videoPlayerController
                    .value
                    .position
                    .inMilliseconds >
                500);
    _video(tester)!.enterFullScreen();
    await _wait(tester, () => _video(tester)?.isFullScreen == true,
        reason: 'actual Chewie fullscreen entry');
    var previousVideo = _video(tester);
    for (final media in [videos[1], videos[2], videos[0]]) {
      await _wait(tester, () {
        final controller = _video(tester);
        return controller != null &&
            !identical(controller, previousVideo) &&
            find
                .text(media.fileName, skipOffstage: false)
                .evaluate()
                .isNotEmpty &&
            controller.videoPlayerController.value.isPlaying &&
            controller.videoPlayerController.value.position.inMilliseconds >
                500;
      }, reason: 'natural native completion into ${media.fileName}');
      expect(_video(tester)!.isFullScreen, isTrue);
      expect(
          _video(tester)!.videoPlayerController.value.errorDescription, isNull);
      debugPrint('PASS native fullscreen completion: ${media.fileName}, '
          'size=${tester.view.physicalSize}, source=${_video(tester)!.videoPlayerController.dataSource}');
      previousVideo = _video(tester);
    }
    _video(tester)!.exitFullScreen();
    await _wait(tester, () => _video(tester)?.isFullScreen == false);
    final musicSet = sets.singleWhere((set) => set.name == 'Music');
    final music = await api.listMedia(setId: musicSet.id, limit: 1000);
    final songs = <Media>[
      for (final name in [
        '01 - First Light.mp3',
        '02 - Long Road Home.mp3',
        '03 - Harbour.flac'
      ])
        music.singleWhere((media) => media.fileName == name),
    ];
    router.go(AppRoutes.audioPlayerPath('${songs.first.id}'), extra: {
      'mediaUrl': api.playbackUrl(songs.first),
      'title': songs.first.displayTitle,
      'sequence': songs,
    });
    final handler = container.read(audioHandlerProvider);
    await _wait(
        tester,
        () =>
            handler.musicState != null &&
            handler.player.playing &&
            handler.player.position.inMilliseconds > 500);
    final identity = handler.musicState!.request.identity;
    final position = handler.player.position;
    await tester.tap(find.byTooltip('More options'));
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(find.byKey(const Key('audio_folder_shuffle')));
    await _wait(tester, () => handler.folderShuffle);
    expect(handler.musicState!.request.identity, identity);
    expect(handler.player.position, greaterThanOrEqualTo(position));
    await _wait(
        tester,
        () =>
            handler.musicState?.request.identity != identity &&
            handler.player.playing &&
            handler.player.position.inMilliseconds > 500,
        reason:
            'real audio natural completion while folder shuffle is enabled');
    debugPrint(
        'PASS native folder audio shuffle preserved source/position and advanced: '
        '${handler.musicState!.request.title}');
    await tester.tap(find.byTooltip('More options'));
    await tester.pump(const Duration(milliseconds: 300));
    await tester.tap(find.byKey(const Key('audio_folder_shuffle')));
    await _wait(tester, () => !handler.folderShuffle);
    await handler.stop();
    router.go(AppRoutes.home);
    await tester.pump(const Duration(milliseconds: 300));
  }, timeout: const Timeout(Duration(minutes: 10)));
}
