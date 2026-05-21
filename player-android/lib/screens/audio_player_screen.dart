// ignore_for_file: unused_import
// The audio_service and just_audio imports are intentionally present even in
// this placeholder so that package resolution is verified at analysis time and
// the import graph is established before feature implementation begins.
import 'package:audio_service/audio_service.dart';
import 'package:flutter/material.dart';
import 'package:just_audio/just_audio.dart';

/// Placeholder audio player screen — full implementation is deferred.
///
/// Accepts [mediaId] (the route path parameter) and [mediaUrl] (the resolved
/// stream URL, passed as route extra) so the router wiring is established and
/// the package imports are verified before feature work begins.
///
/// TODO(audio-player): Initialise a custom [AudioHandler] that extends
///   [BaseAudioHandler].  Register it via [AudioService.init] in `main.dart`
///   and inject it through Riverpod.  Inside the handler call
///   [AudioPlayer.setUrl] with [mediaUrl] to start buffering.
///   See: https://pub.dev/packages/audio_service
///        https://pub.dev/packages/just_audio
class AudioPlayerScreen extends StatelessWidget {
  const AudioPlayerScreen({
    super.key,
    required this.mediaId,
    this.mediaUrl,
  });

  /// The media item identifier extracted from the '/audio/:mediaId' route path.
  final String mediaId;

  /// The resolved stream URL, optionally provided as a route extra.
  /// Will be required once real playback is wired up.
  final String? mediaUrl;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text('Audio – $mediaId')),
      body: const Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.headphones_outlined, size: 64),
            SizedBox(height: 16),
            Text('Audio player TODO', style: TextStyle(fontSize: 18)),
            SizedBox(height: 8),
            Text(
              'Will use just_audio + audio_service for background playback.',
              textAlign: TextAlign.center,
            ),
          ],
        ),
      ),
    );
  }
}
