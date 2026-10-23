import '../providers/playback_cache_provider.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/media.dart';
import '../providers/api_client_provider.dart';
import '../providers/audio_handler_provider.dart';
import '../providers/auth_state_provider.dart';
import '../providers/playback_session_provider.dart';
import '../providers/progress_queue_provider.dart';
import '../services/music_queue.dart';
import '../services/playback_request.dart';

String queueActionLabel(QueueInsertion action) => switch (action) {
      QueueInsertion.now => 'Play now',
      QueueInsertion.next => 'Play next',
      QueueInsertion.append => 'Add to queue',
    };

/// Captures account dependencies now; retained callbacks never capture a screen/ref.
Future<void> insertServerSongs(BuildContext context, WidgetRef ref,
    List<Media> songs, QueueInsertion action,
    {bool replace = false,
    bool shuffled = false,
    String sourceLabel = 'Collection'}) async {
  if (songs.isEmpty || songs.any((song) => song.type != 'audio')) return;
  final handler = ref.read(audioHandlerProvider);
  if (handler.musicQueue.busy) {
    _showQueueSnackBar(
        context, const SnackBar(content: Text('Loading track… Please wait.')));
    return;
  }
  final client = ref.read(apiClientProvider);
  final progress = ref.read(progressQueueProvider);
  final baseUrl = ref.read(playerBaseUrlProvider);
  final user = ref.read(authStateProvider).valueOrNull?.user;
  final mutations = ref.read(credentialMutationQueueProvider);
  final generation = mutations.generation;
  final coordinator = ref.read(playbackSessionCoordinatorProvider);
  if (user == null) return;
  final entries = songs
      .map((song) => ServerPlaybackRequest(
          mediaId: song.id,
          serverOrigin: baseUrl.origin,
          userId: user.id,
          sourceUri: Uri.parse(client.playbackUrl(song)),
          title: song.displayTitle,
          artist: song.music?.artist ?? '',
          album: song.music?.album ?? '',
          music: song.music,
          artworkUri: song.thumbnailPath.isNotEmpty
              ? Uri.parse(client.thumbnailUrl(song.id))
              : null,
          sourceName: song.relPath,
          startPosition: song.duration <= 1200 ? 0 : null,
          readPosition: () => client.getMediaProgress(song.id),
          savePosition: (seconds) => progress.enqueue(song.id, seconds),
          markFinished: () => progress.enqueueFinished(song.id)))
      .toList();
  try {
    final added = await handler.insertQueue(
        entries: entries,
        action: action,
        replace: replace,
        shuffled: shuffled,
        sourceLabel: sourceLabel,
        claim: (request) async {
          if (generation != mutations.generation) return null;
          return coordinator.claim(
              kind: request.kind,
              identity: request.identity,
              sourceUri: request.sourceUri.toString(),
              stop: handler.stop);
        },
        load: ref.read(cachedAudioLoaderProvider));
    if (!context.mounted || !added || generation != mutations.generation) {
      return;
    }
    final token = handler.queueUndoToken;
    if (token == null) return;
    _showQueueSnackBar(
        context,
        SnackBar(
            content: Text(
                'Added ${songs.length} ${songs.length == 1 ? 'song' : 'songs'}'),
            action: SnackBarAction(
                label: 'Undo',
                onPressed: () => handler.undoQueue(expected: token))));
  } catch (_) {
    if (context.mounted) {
      _showQueueSnackBar(
          context,
          const SnackBar(
              content: Text(
                  'Could not play track. Check your connection and try again.')));
    }
  }
}

// A persistent actionable snackbar must not hide newer queue confirmations.
void _showQueueSnackBar(BuildContext context, SnackBar snackbar) {
  final messenger = ScaffoldMessenger.of(context);
  messenger.clearSnackBars();
  messenger.showSnackBar(snackbar);
}
