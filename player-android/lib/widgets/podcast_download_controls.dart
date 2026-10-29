import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/podcast_library_provider.dart';
import '../screens/audio_player_screen.dart';
import '../services/podcast_downloads.dart';
import 'overflow_menu.dart';

class PodcastDownloadControls extends ConsumerStatefulWidget {
  const PodcastDownloadControls(
      {super.key,
      required this.feed,
      required this.episode,
      this.onPlaybackReturn,
      this.scope,
      this.completeLabel,
      this.onToggleComplete,
      this.onDeleteEpisode,
      this.detailsTitle,
      this.detailsBody});
  final String feed;
  final Map<String, Object?> episode;
  final VoidCallback? onPlaybackReturn;
  final String? scope;
  final String? completeLabel;
  final VoidCallback? onToggleComplete;
  final VoidCallback? onDeleteEpisode;
  final String? detailsTitle;
  final String? detailsBody;
  @override
  ConsumerState<PodcastDownloadControls> createState() =>
      PodcastDownloadControlsState();
}

class PodcastDownloadControlsState
    extends ConsumerState<PodcastDownloadControls> {
  bool busy = false;
  String? error;
  String? _copyKey;
  Future<Map<String, Object?>?>? _copy;

  Future<Map<String, Object?>?> _copyFuture(PodcastDownloads downloads) {
    final key = '${widget.feed}\x00${widget.episode['episode_key']}';
    // Parent list rebuilds must not restart copy(); a fresh FutureBuilder
    // future leaves Download disabled while connectionState stays waiting.
    if (_copy == null || _copyKey != key) {
      _copyKey = key;
      _copy =
          downloads.copy(widget.feed, widget.episode['episode_key'] as String);
    }
    return _copy!;
  }

  void _refreshCopy() {
    _copy = null;
    _copyKey = null;
  }

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
      if (mounted) {
        _refreshCopy();
        setState(() => busy = false);
      }
    }
  }

  Future<void> play({bool fromBeginning = false}) => run(() async {
        final String scope =
            widget.scope ?? (await ref.read(podcastScopeProvider.future));
        final request = await ref
            .read(podcastDownloadsProvider)
            .playbackRequest(scope, widget.feed, widget.episode,
                fromBeginning: fromBeginning);
        if (!mounted) return;
        await Navigator.of(context).push(MaterialPageRoute<void>(
            builder: (_) => AudioPlayerScreen(
                mediaId: request.identity,
                mediaTitle: request.title,
                request: request)));
        if (mounted) widget.onPlaybackReturn?.call();
      });

  void _openMore({
    required PodcastDownloads downloads,
    required String key,
    required Object? state,
  }) {
    final available = state == 'available';
    final details = (widget.detailsBody ?? '').trim();
    showModalBottomSheet<void>(
      context: context,
      useRootNavigator: true,
      isScrollControlled: true,
      useSafeArea: true,
      showDragHandle: true,
      builder: (sheet) => SafeArea(
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              if (widget.completeLabel != null)
                OverflowMenuItem(
                  icon: widget.completeLabel == 'Mark uncompleted'
                      ? Icons.undo
                      : Icons.check,
                  label: widget.completeLabel!,
                  onSelected: busy ? null : widget.onToggleComplete,
                ),
              if (available)
                OverflowMenuItem(
                  icon: Icons.replay,
                  label: 'Play from beginning',
                  onSelected: busy ? null : () => play(fromBeginning: true),
                ),
              if (available)
                OverflowMenuItem(
                  icon: Icons.delete_outline,
                  label: 'Delete downloaded file',
                  onSelected: busy
                      ? null
                      : () => run(() => downloads.remove(widget.feed, key)),
                ),
              if (widget.onDeleteEpisode != null)
                OverflowMenuItem(
                  icon: Icons.delete_forever_outlined,
                  label: 'Delete episode',
                  onSelected: busy ? null : widget.onDeleteEpisode,
                ),
              if (details.isNotEmpty)
                OverflowMenuItem(
                  icon: Icons.info_outline,
                  label: 'Episode details',
                  onSelected: () => showDialog<void>(
                    context: context,
                    builder: (dialog) => AlertDialog(
                      title: Text(widget.detailsTitle ?? 'Episode'),
                      content: SingleChildScrollView(child: Text(details)),
                      actions: [
                        TextButton(
                            onPressed: () => Navigator.pop(dialog),
                            child: const Text('Close'))
                      ],
                    ),
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final downloads = ref.watch(podcastDownloadsProvider);
    final key = widget.episode['episode_key'] as String;
    // Refresh only while this row owns an in-flight action. Unconditional
    // clears on every list notify restarted copy() and greyed out Download.
    ref.listen(podcastDownloadsProvider, (previous, next) {
      if (!mounted || !busy) return;
      setState(_refreshCopy);
    });
    return FutureBuilder<Map<String, Object?>?>(
        future: _copyFuture(downloads),
        builder: (context, snapshot) {
          final state = snapshot.data?['state'];
          final transferring = state == 'pending' || state == 'downloading';
          final settled = snapshot.connectionState == ConnectionState.done;
          final hasMenu = widget.onDeleteEpisode != null ||
              widget.completeLabel != null ||
              state == 'available' ||
              (widget.detailsBody ?? '').trim().isNotEmpty;
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
                Row(children: [
                  Expanded(
                      child: Wrap(spacing: 8, runSpacing: 4, children: [
                    if (transferring)
                      TextButton.icon(
                          onPressed: () {
                            downloads.cancel();
                            setState(_refreshCopy);
                          },
                          icon: const Icon(Icons.close),
                          label: const Text('Cancel download'))
                    else if (state == 'available')
                      FilledButton.icon(
                          onPressed: busy ? null : () => play(),
                          icon: const Icon(Icons.play_arrow),
                          label: Text(
                              (widget.episode['position_seconds'] as num? ??
                                          0) >
                                      0
                                  ? 'Resume'
                                  : 'Play'))
                    else
                      TextButton.icon(
                          onPressed: busy || !settled
                              ? null
                              : () => run(
                                  () => downloads.download(widget.feed, key)),
                          icon: const Icon(Icons.download),
                          label: Text(state == 'failed'
                              ? 'Retry download'
                              : 'Download')),
                  ])),
                  if (hasMenu)
                    IconButton(
                      tooltip: 'More episode actions',
                      style:
                          IconButton.styleFrom(minimumSize: const Size(48, 48)),
                      icon: const Icon(Icons.menu),
                      onPressed: () => _openMore(
                          downloads: downloads, key: key, state: state),
                    ),
                ]),
              ]);
        });
  }
}
