import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:just_audio/just_audio.dart';
import 'package:player_android/providers/audio_handler_provider.dart';
import 'package:player_android/providers/theme_provider.dart';
import 'package:player_android/services/audio_handler.dart';
import 'package:player_android/services/music_queue.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/widgets/music_queue_sheet.dart';

class _QueuePlayer extends AudioPlayer {
  @override
  Stream<PlaybackEvent> get playbackEventStream => const Stream.empty();
  @override
  Stream<ProcessingState> get processingStateStream => const Stream.empty();
  @override
  Stream<bool> get playingStream => const Stream.empty();
  @override
  Stream<Duration> get positionStream => const Stream.empty();
  @override
  Duration get position => const Duration(seconds: 4);
  @override
  Duration get duration => const Duration(seconds: 12);
  @override
  bool get playing => false;
  @override
  Future<void> stop() async {}
}

void main() {
  testWidgets(
      'recovered queue shows paused offset and explicit Resume without loading audio',
      (tester) async {
    final handler = PlayerAudioHandler(_QueuePlayer());
    final request = ServerPlaybackRequest(
        mediaId: 1,
        serverOrigin: 'http://one',
        userId: 1,
        title: 'Saved short song',
        sourceUri: Uri.parse('http://one/1'));
    handler.restoreMusicQueue(
        entries: [MusicQueueEntry(71, request)],
        index: 0,
        label: 'Saved album',
        position: 7.25,
        upNext: {},
        claim: (_) async => null,
        load: (_, __) async => null);
    await tester.pumpWidget(ProviderScope(
        overrides: [audioHandlerProvider.overrideWithValue(handler)],
        child: const MaterialApp(
            home: Scaffold(
                appBar: PreferredSize(
                    preferredSize: Size.fromHeight(56),
                    child: MusicQueueButton())))));
    await tester.tap(find.byTooltip('Now Playing queue'));
    await tester.pumpAndSettle();
    expect(find.text('Resume queue'), findsOneWidget);
    expect(find.text('Paused at 0:07'), findsOneWidget);
    expect(find.text('Selected · paused'), findsOneWidget);
    expect(find.text('Open current item'), findsOneWidget);
    expect(handler.musicState, isNull);
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    expect(handler.musicQueue.position, 7.25);
    expect(tester.takeException(), isNull);
  });
  testWidgets(
      'native queue sheet has move/remove/Undo, empty state and Back keeps queue',
      (tester) async {
    final handler = PlayerAudioHandler(_QueuePlayer());
    final entries = List<PlaybackRequest>.generate(
        3,
        (i) => ServerPlaybackRequest(
            mediaId: i,
            serverOrigin: 'http://local',
            userId: 1,
            title: 'Song $i',
            sourceUri: Uri.parse('http://local/$i')));
    handler.activateSourceSession(handler.beginSourceSession());
    handler.startMusicSequence(
        wrap: false,
        entries: entries,
        index: 0,
        expand: () async => entries,
        changed: (_) {},
        load: (r, c) async => AudioSource.uri(r.sourceUri));
    await tester.pumpWidget(ProviderScope(
        overrides: [audioHandlerProvider.overrideWithValue(handler)],
        child: const MaterialApp(
            home: Scaffold(
                appBar: PreferredSize(
                    preferredSize: Size.fromHeight(56),
                    child: MusicQueueButton())))));
    await tester.tap(find.byTooltip('Now Playing queue'));
    await tester.pumpAndSettle();
    expect(find.text('Now Playing'), findsOneWidget);
    expect(find.text('Now'), findsOneWidget);
    await tester.tap(find.byTooltip('Queue actions for Song 1'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Move up'));
    await tester.pumpAndSettle();
    expect(handler.musicQueue.entries.first.request.title, 'Song 1');
    expect(handler.musicQueue.index, 1);
    await tester.tap(find.byTooltip('Queue actions for Song 2'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Remove'));
    await tester.pumpAndSettle();
    expect(handler.musicQueue.entries.length, 2);
    await tester.tap(find.text('Undo'));
    await tester.pumpAndSettle();
    expect(handler.musicQueue.entries.length, 3);
    await tester.runAsync(() async {
      await tester.tap(find.text('Clear queue'));
      await Future<void>.delayed(const Duration(milliseconds: 30));
    });
    await tester.pumpAndSettle();
    expect(find.text('Queue is empty. Add songs or play an album to start.'),
        findsOneWidget);
    await tester.tap(find.text('Undo'));
    await tester.pumpAndSettle();
    expect(find.text('Selected · paused'), findsOneWidget);
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    expect(find.text('Now Playing'), findsNothing);
    expect(handler.musicQueue.entries.length, 3);
    expect(tester.takeException(), isNull);
    await tester.runAsync(handler.stop);
  });
  testWidgets('scope clear removes named album context from the empty sheet',
      (tester) async {
    final handler = PlayerAudioHandler(_QueuePlayer());
    final entry = ServerPlaybackRequest(
        mediaId: 1,
        serverOrigin: 'http://local',
        userId: 1,
        title: 'Private track',
        sourceUri: Uri.parse('http://local/1'));
    handler.activateSourceSession(handler.beginSourceSession());
    handler.startMusicSequence(
        wrap: false,
        entries: [entry],
        index: 0,
        queueLabel: 'Private album',
        expand: () async => [entry],
        changed: (_) {},
        load: (r, c) async => AudioSource.uri(r.sourceUri));
    await tester.pumpWidget(ProviderScope(
        overrides: [audioHandlerProvider.overrideWithValue(handler)],
        child: MaterialApp(
            home: Scaffold(
                appBar: AppBar(actions: const [MusicQueueButton()])))));
    await tester.tap(find.byTooltip('Now Playing queue'));
    await tester.pumpAndSettle();
    expect(find.text('Playing from: Private album'), findsOneWidget);
    expect(find.text('Private track'), findsOneWidget);
    // Auth/server switching clears retained contents before stopping playback.
    handler.clearRetainedQueue();
    await tester.runAsync(handler.stop);
    await tester.pumpAndSettle();
    expect(handler.musicQueue.label, isEmpty);
    expect(handler.musicQueue.status, isEmpty);
    expect(find.text('Playing from: Private album'), findsNothing);
    expect(find.text('Private track'), findsNothing);
    expect(find.text('Queue paused. Choose a track to play.'), findsNothing);
    expect(find.text('Queue is empty. Add songs or play an album to start.'),
        findsOneWidget);
    expect(handler.musicQueue.canUndo, isFalse);
    expect(tester.takeException(), isNull);
  });
  testWidgets(
      'light sheet close glyph contrasts when opened from white-ink player AppBar',
      (tester) async {
    final handler = PlayerAudioHandler(_QueuePlayer());
    await tester.pumpWidget(ProviderScope(
        overrides: [audioHandlerProvider.overrideWithValue(handler)],
        child: MaterialApp(
            theme: buildLightTheme(),
            home: Scaffold(
                appBar: AppBar(
                    backgroundColor: Colors.black,
                    foregroundColor: Colors.white,
                    actions: const [MusicQueueButton()])))));
    await tester.tap(find.byTooltip('Now Playing queue'));
    await tester.pumpAndSettle();
    final glyph = find.descendant(
        of: find.widgetWithIcon(IconButton, Icons.close),
        matching: find.byType(RichText));
    expect(glyph, findsOneWidget);
    final paragraph = tester.renderObject<RenderParagraph>(glyph);
    final ink = paragraph.text.style!.color!;
    // A transparent IconButton Material can carry the black AppBar color
    // without painting it; the actual sheet uses this light surface token.
    final surface =
        Theme.of(tester.element(find.text('Now Playing'))).colorScheme.surface;
    final bright = ink.computeLuminance(), dark = surface.computeLuminance();
    final contrast = (bright > dark ? bright + .05 : dark + .05) /
        (bright > dark ? dark + .05 : bright + .05);
    expect(contrast, greaterThanOrEqualTo(3),
        reason:
            'the actual rendered close icon must remain visible on the light sheet');
    await tester.tap(find.byTooltip('Close queue'));
    await tester.pumpAndSettle();
    expect(find.text('Now Playing'), findsNothing);
    await tester.runAsync(handler.stop);
  });
}
