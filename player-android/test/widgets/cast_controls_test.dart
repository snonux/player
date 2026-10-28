import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/providers/cast_provider.dart';
import 'package:player_android/services/cast/cast_controller.dart';
import 'package:player_android/services/cast/cast_device.dart';
import 'package:player_android/services/cast/cast_session.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/services/playback_session_coordinator.dart';
import 'package:player_android/widgets/cast_controls.dart';

class _RendererSession implements RemotePlaybackSession {
  @override
  final device = const CastDevice(
      id: 'opaque', name: 'Kitchen', host: '', port: 0, isRenderer: true);

  final updates = StreamController<CastMediaStatus>.broadcast(sync: true);
  final closeSignal = Completer<void>();
  final seeks = <Duration>[];

  @override
  CastMediaStatus lastStatus = const CastMediaStatus(
      playerState: CastPlayerState.playing,
      position: Duration(seconds: 40),
      duration: Duration(seconds: 120));

  @override
  Stream<CastMediaStatus> get status => updates.stream;

  @override
  Future<void> get closed => closeSignal.future;

  @override
  Duration get position => lastStatus.position;

  @override
  Future<void> load(CastMedia media,
      {Duration position = Duration.zero}) async {
    lastStatus = CastMediaStatus(
        playerState: CastPlayerState.playing,
        position: position,
        duration: const Duration(seconds: 120));
  }

  @override
  Future<void> play() async {}

  @override
  Future<void> pause() async {}

  @override
  Future<void> seek(Duration position) async {
    seeks.add(position);
    lastStatus = CastMediaStatus(
        playerState: CastPlayerState.playing,
        position: position,
        duration: lastStatus.duration);
  }

  @override
  Future<void> setVolume(double level) async {}

  @override
  Future<void> stop() async {
    if (!closeSignal.isCompleted) closeSignal.complete();
  }
}

ServerPlaybackRequest _request(int id) => ServerPlaybackRequest(
      mediaId: id,
      serverOrigin: 'http://player',
      userId: 1,
      sourceUri: Uri.parse('http://player/$id'),
      title: 'Song $id');

void main() {
  testWidgets(
      'renderer skip buttons stay enabled at queue ends so long-press ±15 works',
      (tester) async {
    final session = _RendererSession();
    final coordinator = PlaybackSessionCoordinator();
    final first = _request(1);
    final last = _request(2);
    final controller = CastController(
      coordinator: coordinator,
      resolveMedia: (request, {required isVideo}) async => CastMedia(
          url: request.sourceUri,
          title: request.title,
          contentType: 'audio/mpeg'),
      startSession: (_) async => session,
    );
    await controller.start(
      device: session.device,
      request: last,
      position: const Duration(seconds: 40),
      isVideo: false,
      rendererQueue: [first, last],
      rendererIndex: 1,
    );
    expect(controller.hasPrevious, isTrue);
    expect(controller.hasNext, isFalse);

    await tester.pumpWidget(ProviderScope(
      overrides: [
        castControllerProvider.overrideWith((ref) => controller),
      ],
      child: const MaterialApp(
        home: Scaffold(
          body: ColoredBox(
            color: Colors.black,
            child: CastPlaybackView(),
          ),
        ),
      ),
    ));
    await tester.pump();

    final back = tester.widget<IconButton>(find.byKey(const Key('cast_skip_back')));
    final forward =
        tester.widget<IconButton>(find.byKey(const Key('cast_skip_forward')));
    expect(back.onPressed, isNotNull);
    expect(forward.onPressed, isNotNull);
    expect(back.onLongPress, isNotNull);
    expect(forward.onLongPress, isNotNull);

    // Tap at end is a no-op (stays on last track); long-press still seeks.
    await tester.tap(find.byKey(const Key('cast_skip_forward')));
    await tester.pump();
    expect(controller.rendererIndex, 1);
    expect(session.seeks, isEmpty);

    await tester.longPress(find.byKey(const Key('cast_skip_forward')));
    await tester.pump();
    expect(session.seeks, [const Duration(seconds: 55)]);

    // Ticker rebuild picks up the seeked position for the next hold.
    await tester.pump(const Duration(milliseconds: 500));
    await tester.longPress(find.byKey(const Key('cast_skip_back')));
    await tester.pump();
    expect(session.seeks, [
      const Duration(seconds: 55),
      const Duration(seconds: 40),
    ]);

    await tester.runAsync(controller.stop);
  });
}
