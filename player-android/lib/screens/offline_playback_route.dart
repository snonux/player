import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers/offline_downloads_provider.dart';
import '../providers/personal_media_provider.dart';
import '../widgets/personal_media_actions.dart';
import '../services/playback_request.dart';
import '../widgets/offline_image.dart';
import 'audio_player_screen.dart';
import 'video_player_screen.dart';

final _offlinePlaybackProvider = FutureProvider.autoDispose
    .family<OfflinePlaybackRequest, String>((ref, key) {
  return ref.watch(offlineDownloadRepositoryProvider).playbackRequest(key);
});

class OfflinePlaybackRoute extends ConsumerWidget {
  const OfflinePlaybackRoute({super.key, required this.downloadKey});
  final String downloadKey;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final item = ref.watch(offlineMediaProvider(downloadKey));
    final request = ref.watch(_offlinePlaybackProvider(downloadKey));
    if ((item.isLoading && !item.hasValue) ||
        (request.isLoading && !request.hasValue)) {
      return const Scaffold(body: Center(child: CircularProgressIndicator()));
    }
    if (item.hasError || request.hasError || item.valueOrNull == null) {
      return Scaffold(
          appBar: AppBar(title: const Text('Offline playback')),
          body: Center(
              child: Padding(
                  padding: const EdgeInsets.all(24),
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      const Text(
                          'Offline copy is missing or incomplete. Delete it and download again.'),
                      TextButton(
                          onPressed: () {
                            ref.invalidate(offlineMediaProvider(downloadKey));
                            ref.invalidate(
                                _offlinePlaybackProvider(downloadKey));
                          },
                          child: const Text('Retry')),
                    ],
                  ))));
    }
    final media = item.requireValue!;
    final source = request.requireValue;
    return switch (media.type) {
      'audio' => AudioPlayerScreen(
          mediaId: media.key, mediaTitle: media.title, request: source),
      'video' => VideoPlayerScreen(
          mediaId: media.key, mediaTitle: media.title, request: source),
      _ => Scaffold(
          backgroundColor: Colors.black,
          appBar: AppBar(title: Text(media.title), actions: [
            PersonalMediaActions(
                store: offlinePersonalStore(ref, downloadKey),
                title: media.title),
          ]),
          body: OfflineImage(uri: source.sourceUri, title: media.title)),
    };
  }
}
