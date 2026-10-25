import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/podcast_library_provider.dart';
import '../screens/audio_player_screen.dart';

class PodcastDownloadControls extends ConsumerStatefulWidget {
  const PodcastDownloadControls(
      {super.key, required this.feed, required this.episode});
  final String feed;
  final Map<String, Object?> episode;
  @override
  ConsumerState<PodcastDownloadControls> createState() =>
      PodcastDownloadControlsState();
}

class PodcastDownloadControlsState
    extends ConsumerState<PodcastDownloadControls> {
  bool busy = false;
  String? error;
  Future<void> run(Future<void> Function() action) async {
    setState(() {
      busy = true;
      error = null;
    });
    try {
      await action();
    } catch (_) {
      if (mounted) {
        setState(() => error =
            'Could not finish this action. Check publisher access, connection and device storage, then retry.');
      }
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final downloads = ref.watch(podcastDownloadsProvider);
    final key = widget.episode['episode_key'] as String;
    return FutureBuilder<Map<String, Object?>?>(
        future: downloads.copy(widget.feed, key),
        builder: (context, snapshot) {
          final state = snapshot.data?['state'];
          final transferring = state == 'pending' || state == 'downloading';
          return Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                if (snapshot.hasError)
                  const Text('Could not read device download.'),
                if (error != null) Text(error!),
                if (state == 'failed' && error == null)
                  Text(
                      snapshot.data?['error'] as String? ?? 'Download failed.'),
                if (state == 'available')
                  const Text('Available on this device'),
                if (transferring)
                  Text(
                      state == 'pending' ? 'Download pending' : 'Downloading…'),
                Wrap(spacing: 8, children: [
                  if (transferring)
                    TextButton.icon(
                        onPressed: downloads.cancel,
                        icon: const Icon(Icons.close),
                        label: const Text('Cancel download'))
                  else if (state == 'available') ...[
                    FilledButton.icon(
                        onPressed: busy
                            ? null
                            : () => run(() async {
                                  final scope = await ref
                                      .read(podcastScopeProvider.future);
                                  final request =
                                      await downloads.playbackRequest(
                                          scope, widget.feed, widget.episode);
                                  if (!context.mounted) return;
                                  await Navigator.of(context).push(
                                      MaterialPageRoute<void>(
                                          builder: (_) => AudioPlayerScreen(
                                              mediaId: request.identity,
                                              mediaTitle: request.title,
                                              request: request)));
                                }),
                        icon: const Icon(Icons.play_arrow),
                        label: const Text('Play')),
                    TextButton.icon(
                        onPressed: busy
                            ? null
                            : () =>
                                run(() => downloads.remove(widget.feed, key)),
                        icon: const Icon(Icons.delete_outline),
                        label: const Text('Remove download')),
                  ] else
                    TextButton.icon(
                        onPressed: busy ||
                                !snapshot.hasData &&
                                    snapshot.connectionState !=
                                        ConnectionState.done
                            ? null
                            : () =>
                                run(() => downloads.download(widget.feed, key)),
                        icon: const Icon(Icons.download),
                        label: Text(
                            state == 'failed' ? 'Retry download' : 'Download')),
                ]),
              ]);
        });
  }
}
