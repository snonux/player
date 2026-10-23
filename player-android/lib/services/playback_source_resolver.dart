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
    return ServerPlaybackRequest(
        mediaId: request.mediaId,
        serverOrigin: request.serverOrigin,
        userId: request.userId,
        sourceUri: Uri.parse(client.playbackUrl(media)),
        title: media.music?.title.trim().isNotEmpty == true
            ? media.displayTitle
            : request.title,
        artist: media.music?.artist ?? '',
        album: media.music?.album ?? '',
        music: media.music,
        artworkUri: media.thumbnailPath.isNotEmpty
            ? Uri.parse(client.thumbnailUrl(media.id))
            : null,
        sourceName: media.relPath.isNotEmpty ? media.relPath : media.fileName,
        startPosition: request.startPosition,
        readPosition: request.readPosition,
        savePosition: request.savePosition,
        markFinished: request.markFinished);
  } catch (_) {
    // After a timeout the request is still pending; nobody waits for it.
    cancelToken.cancel();
    return request;
  }
}

/// Android can demux MPEG-TS itself, including files named .mp4. The server's
/// /stream endpoint remuxes those for browsers; older servers produce an MP4
/// with missing AAC initialization data that ExoPlayer cannot open. /download
/// serves the original bytes with range support, without downloading the whole
/// file into Dart. Compatibility renditions must still use /compat.
PlaybackRequest resolveNativeVideoSource(PlaybackRequest request) {
  if (request is! ServerPlaybackRequest &&
      request is! PublicSharePlaybackRequest) {
    return request;
  }
  final uri = request.sourceUri;
  final path = uri.path;
  if (!RegExp(r'/(?:api/(?:v1/)?media/[0-9]+|s/[^/]+)/stream$')
      .hasMatch(path)) {
    return request;
  }
  final original = uri.replace(
    path: '${path.substring(0, path.length - 'stream'.length)}download',
  );
  return switch (request) {
    ServerPlaybackRequest() => request.withSourceUri(original),
    PublicSharePlaybackRequest() => PublicSharePlaybackRequest(
        shareToken: request.shareToken,
        sourceUri: original,
        title: request.title,
        sourceName: request.sourceName,
      ),
    _ => request,
  };
}
