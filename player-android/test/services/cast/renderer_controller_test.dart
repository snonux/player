import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/cast/cast_controller.dart';
import 'package:player_android/services/cast/cast_device.dart';
import 'package:player_android/services/cast/cast_session.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/services/playback_session_coordinator.dart';

class _Renderer implements RemotePlaybackSession {
  @override
  final device = const CastDevice(
      id: 'opaque', name: 'Kitchen', host: '', port: 0, isRenderer: true);
  final updates = StreamController<CastMediaStatus>.broadcast(sync: true);
  final closeSignal = Completer<void>();
  final loaded = <String>[];
  Completer<void>? loadGate;
  final loadStarted = Completer<void>();
  @override
  CastMediaStatus lastStatus = CastMediaStatus.initial;
  @override
  Stream<CastMediaStatus> get status => updates.stream;
  @override
  Future<void> get closed => closeSignal.future;
  @override
  Duration get position => lastStatus.position;
  void emit(CastMediaStatus state) {
    lastStatus = state;
    updates.add(state);
  }

  @override
  Future<void> load(CastMedia media,
      {Duration position = Duration.zero}) async {
    loaded.add(media.title);
    if (!loadStarted.isCompleted) loadStarted.complete();
    await loadGate?.future;
    lastStatus = CastMediaStatus(
        playerState: CastPlayerState.playing,
        position: position,
        duration: const Duration(seconds: 100));
  }

  @override
  Future<void> play() async {}
  @override
  Future<void> pause() async {}
  @override
  Future<void> seek(Duration position) async {
    lastStatus = CastMediaStatus(
        playerState: CastPlayerState.paused, position: position);
  }

  @override
  Future<void> setVolume(double level) async {}
  @override
  Future<void> stop() async {
    if (!closeSignal.isCompleted) closeSignal.complete();
  }
}

void main() {
  for (final replacement in [false, true]) {
    test(
        'held completion cannot ${replacement ? 'stop a replacement session' : 'skip a manually selected duplicate occurrence'}',
        () async {
      final firstSession = _Renderer(), replacementSession = _Renderer();
      final coordinator = PlaybackSessionCoordinator();
      final finishStarted = Completer<void>(), finishGate = Completer<void>();
      final request = ServerPlaybackRequest(
          mediaId: 5,
          serverOrigin: 'http://player',
          userId: 1,
          sourceUri: Uri.parse('http://player/5'),
          title: 'Same song',
          markFinished: () async {
            finishStarted.complete();
            await finishGate.future;
          });
      final third = ServerPlaybackRequest(
          mediaId: 6,
          serverOrigin: 'http://player',
          userId: 1,
          sourceUri: Uri.parse('http://player/6'),
          title: 'Third song');
      var starts = 0;
      final controller = CastController(
          coordinator: coordinator,
          resolveMedia: (request, {required isVideo}) async => CastMedia(
              url: request.sourceUri,
              title: request.title,
              contentType: 'audio/mpeg'),
          startSession: (_) async =>
              starts++ == 0 ? firstSession : replacementSession);
      await controller.start(
          device: firstSession.device,
          request: request,
          position: Duration.zero,
          isVideo: false,
          rendererQueue: [request, request, third],
          existingOccurrence: 'a' * 32);
      firstSession.emit(const CastMediaStatus(
          playerState: CastPlayerState.idle,
          position: Duration(seconds: 100),
          duration: Duration(seconds: 100),
          idleReason: 'FINISHED'));
      await finishStarted.future;
      if (replacement) {
        // Reusing both media identity and listening ID still belongs to a new session.
        await controller.start(
            device: replacementSession.device,
            request: request,
            position: Duration.zero,
            isVideo: false,
            existingOccurrence: 'a' * 32);
      } else {
        await controller.moveRendererQueue(1);
      }
      final selectedOccurrence = controller.currentOccurrenceID;
      finishGate.complete();
      await Future<void>.delayed(const Duration(milliseconds: 10));
      expect(controller.currentRequest, same(request));
      expect(controller.currentOccurrenceID, selectedOccurrence);
      expect(coordinator.activeIdentity, request.identity);
      if (replacement) {
        expect(replacementSession.closeSignal.isCompleted, isFalse);
        expect(replacementSession.loaded, ['Same song']);
      } else {
        expect(controller.rendererIndex, 1);
        expect(firstSession.loaded, ['Same song', 'Same song']);
        expect(selectedOccurrence, isNot('a' * 32));
      }
      await controller.stop();
      controller.dispose();
      await firstSession.updates.close();
      await replacementSession.updates.close();
    });
  }

  test('cancel pending renderer load cannot acquire a source after Stop',
      () async {
    final session = _Renderer()..loadGate = Completer<void>();
    final coordinator = PlaybackSessionCoordinator();
    final controller = CastController(
        coordinator: coordinator,
        resolveMedia: (request, {required isVideo}) async => CastMedia(
            url: request.sourceUri,
            title: request.title,
            contentType: 'audio/mpeg'),
        startSession: (_) async => session);
    final request = ServerPlaybackRequest(
        mediaId: 5,
        serverOrigin: 'http://player',
        userId: 1,
        sourceUri: Uri.parse('http://player/5'),
        title: 'Old selection');
    final starting = controller.start(
        device: session.device,
        request: request,
        position: Duration.zero,
        isVideo: false);
    await session.loadStarted.future;
    await controller.stop();
    session.loadGate!.complete();
    await starting;
    expect(controller.active, isNull);
    expect(coordinator.activeIdentity, isNull);
    controller.dispose();
    await session.updates.close();
  });

  test('new local source during held renderer load retains playback ownership',
      () async {
    final session = _Renderer()..loadGate = Completer<void>();
    final coordinator = PlaybackSessionCoordinator();
    final controller = CastController(
        coordinator: coordinator,
        resolveMedia: (request, {required isVideo}) async => CastMedia(
            url: request.sourceUri,
            title: request.title,
            contentType: 'audio/mpeg'),
        startSession: (_) async => session);
    final request = ServerPlaybackRequest(
        mediaId: 5,
        serverOrigin: 'http://player',
        userId: 1,
        sourceUri: Uri.parse('http://player/5'),
        title: 'Old selection');
    final starting = controller.start(
        device: session.device,
        request: request,
        position: Duration.zero,
        isVideo: false);
    await session.loadStarted.future;
    await coordinator.claim(
        kind: PlaybackSourceKind.server,
        identity: 'new-local-source',
        stop: () async {});
    session.loadGate!.complete();
    await starting;
    expect(controller.active, isNull);
    expect(coordinator.activeIdentity, 'new-local-source');
    controller.dispose();
    await session.updates.close();
  });

  test(
      'renderer keeps queue occurrences and lease, uses handoff ID then new ID for replay',
      () async {
    final session = _Renderer();
    final coordinator = PlaybackSessionCoordinator();
    final savedIDs = <String>[];
    var finished = 0;
    ServerPlaybackRequest request(int id) => ServerPlaybackRequest(
          mediaId: id,
          serverOrigin: 'https://player.test',
          userId: 1,
          sourceUri: Uri.parse('https://player.test/api/v1/media/$id/stream'),
          title: 'Song $id',
          savePosition: (_) async {},
          saveListeningPosition: (_, occurrence) async {
            savedIDs.add(occurrence);
          },
          markFinished: () async {
            finished++;
          },
        );
    final first = request(5), second = request(6), replay = request(5);
    final controller = CastController(
        coordinator: coordinator,
        resolveMedia: (request, {required isVideo}) async => CastMedia(
            url: request.sourceUri,
            title: request.title,
            contentType: 'audio/mpeg'),
        startSession: (_) async => session,
        progressInterval: const Duration(milliseconds: 5));
    await controller.start(
        device: session.device,
        request: first,
        position: const Duration(seconds: 4),
        isVideo: false,
        rendererQueue: [first, second, replay],
        existingOccurrence: 'a' * 32);
    await Future<void>.delayed(const Duration(milliseconds: 15));
    expect(savedIDs, contains('a' * 32));
    final generation = coordinator.generation;
    session.emit(const CastMediaStatus(
        playerState: CastPlayerState.idle,
        position: Duration(seconds: 100),
        duration: Duration(seconds: 100),
        idleReason: 'FINISHED'));
    await Future<void>.delayed(const Duration(milliseconds: 10));
    expect(controller.currentRequest, second);
    expect(controller.rendererIndex, 1);
    expect(finished, 1);
    expect(coordinator.activeIdentity, second.identity);
    expect(coordinator.generation, generation,
        reason: 'queue transition retains remote lease');
    final secondOccurrence = controller.currentOccurrenceID;
    await controller.moveRendererQueue(1);
    expect(controller.currentRequest, replay);
    expect(controller.rendererIndex, 2);
    expect(controller.currentOccurrenceID, isNot(secondOccurrence));
    expect(controller.currentOccurrenceID, isNot('a' * 32));
    expect(session.loaded, ['Song 5', 'Song 6', 'Song 5']);
    await controller.seek(const Duration(seconds: 12));
    final ended = controller.ended.first;
    await controller.stop();
    expect((await ended).position, const Duration(seconds: 12));
    expect(coordinator.activeIdentity, isNull);
    controller.dispose();
    await session.updates.close();
  });
}
