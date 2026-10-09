import 'package:dio/dio.dart' show CancelToken;

import '../api/player_api_client.dart';
import 'playback_request.dart';

/// Picks the playback URL for a server request that was built from a media
/// ID alone (podcast episodes, routes opened without a route extra).
///
/// Such callers have no media object, so the screen starts from the original
/// stream URL. The media lookup tells whether the server wants this item
/// played through its compatibility stream instead. The lookup is best
/// effort and bounded by [timeout] (the API client itself sets none), after
/// which the request is cancelled rather than left running: on any
/// failure the original stream is kept, which is what the app played before
/// the compatibility stream existed. Should that item have needed the
/// compatibility stream, the player's error names both possible causes
/// (connection or format) rather than blaming the format alone.
Future<PlaybackRequest> resolveServerPlaybackSource(
  PlaybackRequest request,
  PlayerApiClient client, {
  Duration timeout = const Duration(seconds: 10),
}) async {
  if (request is! ServerPlaybackRequest) return request;
  final cancelToken = CancelToken();
  try {
    final media = await client
        .getMedia(request.mediaId, cancelToken: cancelToken)
        .timeout(timeout);
    return request.withSourceUri(Uri.parse(client.playbackUrl(media)));
  } catch (_) {
    // After a timeout the request is still pending; nobody waits for it.
    cancelToken.cancel();
    return request;
  }
}
