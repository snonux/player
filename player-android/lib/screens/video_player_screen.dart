// ignore_for_file: unused_import
// The chewie and video_player imports are intentionally present even in this
// placeholder so that package resolution is verified at analysis time and the
// import graph is established before feature implementation begins.
import 'package:chewie/chewie.dart';
import 'package:flutter/material.dart';
import 'package:video_player/video_player.dart';

/// Placeholder video player screen — full implementation is deferred.
///
/// Accepts [mediaId] (the route path parameter) and [mediaUrl] (the resolved
/// stream URL, passed as route extra) so the router wiring is established and
/// the package imports are verified before feature work begins.
///
/// TODO(video-player): Convert to [StatefulWidget].  In [State.initState]
///   create [VideoPlayerController.networkUrl] from [mediaUrl], then wrap it
///   in a [ChewieController] with `aspectRatio`, `autoPlay`, etc.  Dispose
///   both controllers in [State.dispose].
///   See: https://pub.dev/packages/chewie
///        https://pub.dev/packages/video_player
class VideoPlayerScreen extends StatelessWidget {
  const VideoPlayerScreen({
    super.key,
    required this.mediaId,
    this.mediaUrl,
  });

  /// The media item identifier extracted from the '/video/:mediaId' route path.
  final String mediaId;

  /// The resolved HLS/direct stream URL, optionally provided as a route extra.
  /// Will be required once real playback is wired up.
  final String? mediaUrl;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text('Video – $mediaId')),
      body: const Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.videocam_outlined, size: 64),
            SizedBox(height: 16),
            Text('Video player TODO', style: TextStyle(fontSize: 18)),
            SizedBox(height: 8),
            Text(
              'Will use video_player + chewie for playback controls.',
              textAlign: TextAlign.center,
            ),
          ],
        ),
      ),
    );
  }
}
