import 'dart:io';
import 'package:audio_service/audio_service.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/audio_handler_provider.dart';
import '../screens/audio_player_screen.dart';
import '../services/music_sequence.dart';

final _currentItem = StreamProvider.autoDispose<MediaItem?>(
    (ref) => ref.watch(audioHandlerProvider).mediaItem);

final _currentMusicState = StreamProvider.autoDispose<MusicSequenceState>(
    (ref) => ref.watch(audioHandlerProvider).musicChanges);

/// Existing handler owns playback. Opening this surface never selects a song.
class MusicMiniPlayer extends ConsumerWidget {
  const MusicMiniPlayer({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    ref.watch(_currentMusicState);
    final item = ref.watch(_currentItem).valueOrNull;
    if (item == null) return const SizedBox.shrink();
    final handler = ref.read(audioHandlerProvider);
    final state = handler.musicState;
    final lease = handler.sequenceLease;
    if (state == null ||
        lease?.isCurrent != true ||
        state.request.identity != item.id) {
      return const SizedBox.shrink();
    }
    final request = state.request;
    return Material(
        color: Theme.of(context).colorScheme.surfaceContainer,
        child: SafeArea(
            top: false,
            child: Row(children: [
              Expanded(
                  child: InkWell(
                      key: const Key('music_mini_open'),
                      onTap: () => Navigator.of(context).push(
                          MaterialPageRoute<void>(
                              builder: (_) => AudioPlayerScreen(
                                  mediaId: request.identity,
                                  request: request,
                                  attachCurrent: true,
                                  attachmentLease: lease,
                                  attachmentGeneration:
                                      state.sourceGeneration))),
                      child: Semantics(
                          button: true,
                          label:
                              'Open player: ${item.title}, ${request.subtitle}',
                          child: Padding(
                              padding: const EdgeInsets.all(8),
                              child: Row(children: [
                                SizedBox(
                                    width: 48,
                                    height: 48,
                                    child: item.artUri?.scheme == 'file'
                                        ? Image.file(File.fromUri(item.artUri!),
                                            fit: BoxFit.cover,
                                            errorBuilder: (_, __, ___) =>
                                                const Icon(Icons.music_note))
                                        : const Icon(Icons.music_note)),
                                const SizedBox(width: 8),
                                Expanded(
                                    child: Column(
                                        crossAxisAlignment:
                                            CrossAxisAlignment.start,
                                        mainAxisSize: MainAxisSize.min,
                                        children: [
                                      Text(item.title,
                                          maxLines: 1,
                                          overflow: TextOverflow.ellipsis),
                                      Text(request.subtitle,
                                          maxLines: 1,
                                          overflow: TextOverflow.ellipsis,
                                          style: Theme.of(context)
                                              .textTheme
                                              .bodySmall),
                                    ])),
                              ]))))),
              StreamBuilder<PlaybackState>(
                  stream: handler.playbackState,
                  builder: (context, _) => IconButton(
                      tooltip: handler.playbackState.value.playing
                          ? 'Pause'
                          : 'Play',
                      onPressed: state.loading
                          ? null
                          : () => handler.playbackState.value.playing
                              ? handler.pause()
                              : handler.play(),
                      icon: Icon(handler.playbackState.value.playing
                          ? Icons.pause
                          : Icons.play_arrow))),
              IconButton(
                  tooltip: 'Next track',
                  onPressed: state.loading ? null : handler.skipToNext,
                  icon: const Icon(Icons.skip_next)),
            ])));
  }
}
