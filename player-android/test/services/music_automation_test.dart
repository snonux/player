import 'dart:async';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/models/music_metadata.dart';
import 'package:player_android/services/music_automation.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/services/playback_session_coordinator.dart';

void main() {
  test(
      'validated offline music needs no catalog request; unknown and foreign copies fail closed',
      () async {
    OfflinePlaybackRequest saved({bool verified = true, int user = 1}) =>
        OfflinePlaybackRequest(
            downloadKey: 'copy',
            sourceUri: Uri.file('/private/copy'),
            title: 'Song',
            serverOrigin: 'https://one',
            userId: user,
            automationMusicEligible: verified);
    var calls = 0;
    Future<bool> catalog(int id) async {
      calls++;
      throw StateError('No network');
    }

    await validateAutomationQueueMusic(
        [saved()], 'https://one', 1, catalog, () => true);
    await expectLater(
        validateAutomationQueueMusic(
            [saved(verified: false)], 'https://one', 1, catalog, () => true),
        throwsStateError);
    await expectLater(
        validateAutomationQueueMusic(
            [saved(user: 2)], 'https://one', 1, catalog, () => true),
        throwsStateError);
    expect(calls, 0);
  });
  test('locked, switched and local accounts cannot execute saved actions', () {
    bool allowed({bool unlocked = true, int user = 1, bool server = true}) =>
        musicAutomationAccountAllowed(
            origin: 'https://one',
            userId: user,
            configuredScope: musicAutomationScope('https://one', 1),
            authenticated: true,
            credentialsEnabled: unlocked,
            credentialOrigin: 'https://one',
            serverSelected: server);
    expect(allowed(), isTrue);
    expect(allowed(unlocked: false), isFalse);
    expect(allowed(user: 2), isFalse);
    expect(allowed(server: false), isFalse);
  });
  test('timeout stops its own lease but cannot stop a newer manual source',
      () async {
    final coordinator = PlaybackSessionCoordinator();
    var ownStops = 0, manualStops = 0;
    final first = MusicAutomationCancellation(coordinator);
    await first.acquired(await coordinator.claim(
        kind: PlaybackSourceKind.server,
        identity: 'music',
        stop: () async {
          ownStops++;
        }));
    await first.cancel();
    expect(ownStops, 1);
    final second = MusicAutomationCancellation(coordinator);
    await second.acquired(await coordinator.claim(
        kind: PlaybackSourceKind.server,
        identity: 'music2',
        stop: () async {
          ownStops++;
        }));
    final manual = await coordinator.claim(
        kind: PlaybackSourceKind.local,
        identity: 'manual',
        stop: () async {
          manualStops++;
        });
    await second.cancel();
    expect(manual!.isCurrent, isTrue);
    expect(manualStops, 0);
  });
  test('lease acquired after timeout is immediately invalidated', () async {
    final coordinator = PlaybackSessionCoordinator();
    final cancellation = MusicAutomationCancellation(coordinator);
    await cancellation.cancel();
    final lease = await coordinator.claim(
        kind: PlaybackSourceKind.server,
        identity: 'delayed',
        stop: () async {});
    expect(await cancellation.acquired(lease), isFalse);
    expect(lease!.isCurrent, isFalse);
  });
  ServerPlaybackRequest song(
          {String origin = 'https://one',
          int user = 1,
          bool music = true,
          String name = 'song.mp3'}) =>
      ServerPlaybackRequest(
          mediaId: 7,
          serverOrigin: origin,
          userId: user,
          title: 'Song',
          sourceName: name,
          sourceUri: Uri.parse('$origin/api/media/7/stream'),
          music: music ? MusicMetadata.fromJson({'artist': 'Artist'}) : null);

  test(
      'capability scope isolates origin and account without URLs or credentials',
      () {
    expect(musicAutomationScope('https://one', 1), hasLength(64));
    expect(musicAutomationScope('https://one', 1),
        isNot(musicAutomationScope('https://one', 2)));
    expect(musicAutomationScope('https://one', 1),
        isNot(musicAutomationScope('https://two', 1)));
  });
  test('automation ownership refuses foreign queues and device files', () {
    expect(automationMusicOwned(song(), 'https://one', 1), isTrue);
    expect(automationMusicOwned(song(user: 2), 'https://one', 1), isFalse);
    expect(automationMusicOwned(song(origin: 'https://two'), 'https://one', 1),
        isFalse);
    expect(
        automationMusicOwned(
            LocalPlaybackRequest(
                localMediaId: 1,
                sourceUri: Uri.parse('content://device/1'),
                title: 'Song'),
            'https://one',
            1),
        isFalse);
  });
  test(
      'stale account or user playback intent during offline read cancels before queue mutation',
      () async {
    var current = true, reads = 0;
    await expectLater(
        resolveAutomationMusic([song(), song()], (request) async {
          reads++;
          current = false;
          return request;
        }, () => current),
        throwsStateError);
    expect(reads, 1);
  });
  test('timeout cancellation while disk read is held prevents a playable queue',
      () async {
    final held = Completer<PlaybackRequest>();
    var wanted = true;
    final result =
        resolveAutomationMusic([song()], (_) => held.future, () => wanted);
    final assertion = expectLater(result, throwsStateError);
    wanted = false;
    held.complete(song());
    await assertion;
  });
  test('missing offline copy aborts the whole resolution', () async {
    var reads = 0;
    await expectLater(
        resolveAutomationMusic([song(), song()], (request) async {
          if (++reads == 2) throw StateError('Missing copy');
          return request;
        }, () => true),
        throwsStateError);
    expect(reads, 2);
  });
}
