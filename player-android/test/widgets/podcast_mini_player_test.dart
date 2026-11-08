import 'package:audio_service/audio_service.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:player_android/app_routes.dart';
import 'package:player_android/providers/audio_handler_provider.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/widgets/music_mini_player.dart';

import '../support/idle_playback.dart';

void main() {
  testWidgets('background podcast shows a mini player that reopens it',
      (tester) async {
    Object? opened;
    final router = GoRouter(initialLocation: '/test', routes: [
      GoRoute(
          path: '/test',
          builder: (_, __) =>
              const Scaffold(bottomNavigationBar: NowPlayingBar())),
      GoRoute(
          path: AppRoutes.podcastPlayer,
          builder: (_, state) {
            opened = state.extra;
            return const Text('Podcast player');
          }),
    ]);
    addTearDown(router.dispose);
    await tester.pumpWidget(ProviderScope(
        overrides: idlePlaybackOverrides(),
        child: MaterialApp.router(routerConfig: router)));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('podcast_mini_player')), findsNothing);

    final container =
        ProviderScope.containerOf(tester.element(find.byType(NowPlayingBar)));
    final handler = container.read(audioHandlerProvider);
    final request = PodcastPlaybackRequest(
        identity: 'podcast:episode',
        sourceUri: Uri.parse('https://example.com/a.mp3'),
        title: 'Episode one',
        sourceName: 'My podcast');
    handler.setMediaRequest(request);
    handler.playbackState.add(PlaybackState(
        processingState: AudioProcessingState.ready, playing: true));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('podcast_mini_player')), findsOneWidget);
    expect(find.text('Episode one'), findsOneWidget);
    expect(find.text('My podcast'), findsOneWidget);
    expect(find.byTooltip('Pause'), findsOneWidget);

    await tester.tap(find.byKey(const Key('podcast_mini_open')));
    await tester.pumpAndSettle();
    expect(find.text('Podcast player'), findsOneWidget);
    expect(opened, same(request));

    router.go('/test');
    handler.playbackState
        .add(PlaybackState(processingState: AudioProcessingState.idle));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('podcast_mini_player')), findsNothing);
  });

  testWidgets('screens without playback services hide the bar', (tester) async {
    await tester.pumpWidget(const ProviderScope(
        child:
            MaterialApp(home: Scaffold(bottomNavigationBar: NowPlayingBar()))));
    await tester.pumpAndSettle();
    expect(find.byType(MusicMiniPlayer), findsNothing);
    expect(tester.takeException(), isNull);
  });
}
