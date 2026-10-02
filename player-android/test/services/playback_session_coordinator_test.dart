import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/services/playback_session_coordinator.dart';

void main() {
  test('new source stops the previous owner and invalidates its lease',
      () async {
    final coordinator = PlaybackSessionCoordinator();
    var serverStops = 0;
    final server = await coordinator.claim(
      kind: PlaybackSourceKind.server,
      identity: 'server:https://player.example:3:42',
      stop: () async {
        serverStops++;
      },
    );

    final local = await coordinator.claim(
      kind: PlaybackSourceKind.local,
      identity: 'local:7',
      stop: () async {},
    );

    expect(serverStops, 1);
    expect(server!.isCurrent, isFalse);
    expect(local!.isCurrent, isTrue);
    expect(coordinator.activeIdentity, 'local:7');
  });

  test('a slower superseded claim cannot become the active session', () async {
    final coordinator = PlaybackSessionCoordinator();
    final stopStarted = Completer<void>();
    final finishStop = Completer<void>();
    final first = await coordinator.claim(
      kind: PlaybackSourceKind.server,
      identity: 'server:first',
      stop: () async {
        stopStarted.complete();
        await finishStop.future;
      },
    );
    final slowerClaim = coordinator.claim(
      kind: PlaybackSourceKind.local,
      identity: 'local:1',
      stop: () async {},
    );
    await stopStarted.future;
    final fasterClaimPending = coordinator.claim(
      kind: PlaybackSourceKind.publicShare,
      identity: 'public-share:token',
      stop: () async {},
    );
    var replacementStarted = false;
    unawaited(fasterClaimPending.then((_) => replacementStarted = true));
    await Future<void>.delayed(Duration.zero);
    expect(replacementStarted, isFalse);
    finishStop.complete();
    final fasterClaim = await fasterClaimPending;
    final superseded = await slowerClaim;

    expect(superseded, isNull);
    expect(first!.isCurrent, isFalse);
    expect(fasterClaim!.isCurrent, isTrue);
    expect(coordinator.activeKind, PlaybackSourceKind.publicShare);
  });

  test('server cleanup cancels a claim waiting for the old source to stop',
      () async {
    final coordinator = PlaybackSessionCoordinator();
    final stopStarted = Completer<void>();
    final finishStop = Completer<void>();
    await coordinator.claim(
      kind: PlaybackSourceKind.local,
      identity: 'local:1',
      stop: () async {
        stopStarted.complete();
        await finishStop.future;
      },
    );
    final pendingServer = coordinator.claim(
      kind: PlaybackSourceKind.server,
      identity: 'server:pending',
      stop: () async {},
    );
    await stopStarted.future;
    final cleanup = coordinator.stopServerSession();
    finishStop.complete();
    await cleanup;
    expect(await pendingServer, isNull);
    expect(coordinator.activeKind, isNull);
  });

  test('server cleanup leaves local and public playback alone', () async {
    final coordinator = PlaybackSessionCoordinator();
    var stops = 0;
    final local = await coordinator.claim(
      kind: PlaybackSourceKind.local,
      identity: 'local:4',
      stop: () async {
        stops++;
      },
    );

    await coordinator.stopServerSession();
    expect(stops, 0);
    expect(local!.isCurrent, isTrue);

    final share = await coordinator.claim(
      kind: PlaybackSourceKind.publicShare,
      identity: 'public-share:token',
      stop: () async {
        stops++;
      },
    );
    await coordinator.stopServerSession();
    expect(stops, 1);
    expect(share!.isCurrent, isTrue);
  });
}
