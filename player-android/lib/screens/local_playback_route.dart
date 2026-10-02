import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/local_media.dart';
import '../providers/local_library_provider.dart';
import '../services/playback_request.dart';
import 'audio_player_screen.dart';
import 'video_player_screen.dart';

/// Resolves a durable local ID before constructing a player request.
class LocalPlaybackRoute extends ConsumerWidget {
  const LocalPlaybackRoute({super.key, required this.localMediaId});

  final int localMediaId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final media = ref.watch(localMediaByIdProvider(localMediaId));
    return media.when(
      loading: () => const Scaffold(
        body: Center(child: CircularProgressIndicator()),
      ),
      error: (error, _) => _LocalPlaybackError(message: error.toString()),
      data: (item) {
        if (item == null) {
          return const _LocalPlaybackError(
            message: 'This item is no longer in the local library.',
          );
        }
        final request = _requestFor(ref, item);
        return item.mimeType.toLowerCase().startsWith('video/')
            ? VideoPlayerScreen(
                mediaId: item.id.toString(),
                mediaTitle: item.title,
                request: request,
              )
            : AudioPlayerScreen(
                mediaId: item.id.toString(),
                mediaTitle: item.title,
                request: request,
              );
      },
    );
  }

  LocalPlaybackRequest _requestFor(WidgetRef ref, LocalMedia item) {
    final progress = ref.read(localProgressRepositoryProvider);
    return LocalPlaybackRequest(
      localMediaId: item.id,
      sourceUri: Uri.parse(item.uri),
      title: item.title,
      readPosition: () async {
        final saved = await progress.get(item.id);
        if (saved?.finished == true) {
          await progress.savePosition(item.id, 0);
          return 0;
        }
        return saved?.positionSeconds;
      },
      savePosition: (seconds) async {
        await progress.savePosition(item.id, seconds);
      },
      markFinished: () async {
        await progress.markFinished(item.id);
      },
    );
  }
}

class _LocalPlaybackError extends StatelessWidget {
  const _LocalPlaybackError({required this.message});

  final String message;

  @override
  Widget build(BuildContext context) => Scaffold(
        appBar: AppBar(title: const Text('On this device')),
        body: Center(
          child: Padding(
            padding: const EdgeInsets.all(24),
            child: Text(message, textAlign: TextAlign.center),
          ),
        ),
      );
}
