import 'dart:convert';
import 'package:crypto/crypto.dart';
import 'playback_request.dart';
import 'playback_session_coordinator.dart';

/// Cancellation belongs to one invocation and can only stop its own lease.
class MusicAutomationCancellation {
  MusicAutomationCancellation(this.coordinator);
  final PlaybackSessionCoordinator coordinator;
  bool cancelled = false;
  PlaybackSessionLease? lease;
  Future<void> Function()? cancelResume;
  Future<void> cancel() async {
    cancelled = true;
    final stopping =
        lease?.isCurrent == true ? coordinator.stopCurrent() : null;
    await cancelResume?.call();
    await stopping;
  }

  Future<bool> acquired(PlaybackSessionLease? value) async {
    lease = value;
    if (cancelled) {
      if (value?.isCurrent == true) await coordinator.stopCurrent();
      return false;
    }
    return value?.isCurrent == true;
  }
}

bool musicAutomationAccountAllowed(
        {required String origin,
        required int? userId,
        required String? configuredScope,
        required bool authenticated,
        required bool credentialsEnabled,
        required String? credentialOrigin,
        required bool serverSelected}) =>
    userId != null &&
    authenticated &&
    credentialsEnabled &&
    serverSelected &&
    credentialOrigin == origin &&
    configuredScope == musicAutomationScope(origin, userId);

String musicAutomationScope(String origin, int userId) =>
    sha256.convert(utf8.encode(jsonEncode([origin, userId]))).toString();

/// A retained queue must never redirect automation into spoken/local/share audio.
bool automationMusicOwned(PlaybackRequest request, String origin, int userId) {
  return switch (request) {
    ServerPlaybackRequest r => r.serverOrigin == origin && r.userId == userId,
    OfflinePlaybackRequest r => r.serverOrigin == origin && r.userId == userId,
    _ => false,
  };
}

/// Catalog membership supplies source-set/podcast classification; tags and
/// filenames cannot establish music eligibility. Server snapshots are checked
/// afresh, while offline copies retain validated membership from download time.
Future<void> validateAutomationQueueMusic(
    List<PlaybackRequest> requests,
    String origin,
    int userId,
    Future<bool> Function(int) catalogMusic,
    bool Function() current) async {
  final checked = <int, bool>{};
  for (final request in requests) {
    if (!current()) throw StateError('Music action superseded. Retry.');
    if (!automationMusicOwned(request, origin, userId)) {
      throw StateError(
          'Queue music for this account before running automation.');
    }
    if (request is ServerPlaybackRequest) {
      if (request.mediaId <= 0) {
        throw StateError(
            'Saved music metadata is missing. Choose music in Player again.');
      }
      checked[request.mediaId] ??= await catalogMusic(request.mediaId);
      if (!current()) throw StateError('Music action superseded. Retry.');
      if (checked[request.mediaId] != true) {
        throw StateError(
            'Music automation cannot play podcasts or other spoken audio. Choose music in Player first.');
      }
    } else if (request is OfflinePlaybackRequest &&
        !request.automationMusicEligible) {
      throw StateError(
          'This offline copy has not been validated as music. Allow online music and run Start once while connected to validate it.');
    }
  }
}

/// Revalidate after every disk read, before any queue/source mutation.
Future<List<PlaybackRequest>> resolveAutomationMusic(
    List<PlaybackRequest> requests,
    Future<PlaybackRequest> Function(PlaybackRequest) resolve,
    bool Function() current) async {
  final result = <PlaybackRequest>[];
  for (final request in requests) {
    if (!current()) throw StateError('Music action superseded. Retry.');
    result.add(await resolve(request));
    if (!current()) throw StateError('Music action superseded. Retry.');
  }
  return result;
}
