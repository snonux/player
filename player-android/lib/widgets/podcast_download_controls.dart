import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../providers/podcast_library_provider.dart';
import 'package:go_router/go_router.dart';
import '../app_routes.dart';
import '../services/podcast_downloads.dart';
import 'podcast_show_notes.dart';
import 'podcast_publication_date.dart';
import '../services/podcast_metadata.dart';

/// One episode row: [body] (artwork, title, metadata) plus compact actions.
///
/// The row keeps only the frequent actions visible: the download state
/// (download, cancel or retry), Play/Resume, and a menu. Tapping the row or
/// the menu opens the episode sheet with show notes and every other action,
/// so long lists stay compact without hiding any feature.
class PodcastDownloadControls extends ConsumerStatefulWidget {
  const PodcastDownloadControls(
      {super.key,
      required this.feed,
      required this.episode,
      this.body,
      this.cover,
      this.vertical = false,
      this.onLongPress,
      this.onPlaybackReturn,
      this.scope,
      this.completeLabel,
      this.onToggleComplete,
      this.onDeleteEpisode,
      this.detailsTitle,
      this.detailsBody});
  final String feed;
  final Map<String, Object?> episode;

  /// Row content shown before (or, when [vertical], above) the actions.
  final Widget? body;

  /// Grid cards: [cover] fills the card width with Download and Play on it,
  /// and [body] sits below. The card opens the episode sheet.
  final bool vertical;
  final Widget? cover;
  final VoidCallback? onLongPress;
  final VoidCallback? onPlaybackReturn;
  final String? scope;
  final String? completeLabel;
  final VoidCallback? onToggleComplete;
  final VoidCallback? onDeleteEpisode;
  final String? detailsTitle;

  /// Show notes; defaults to the episode description.
  final String? detailsBody;
  @override
  ConsumerState<PodcastDownloadControls> createState() =>
      PodcastDownloadControlsState();
}

class PodcastDownloadControlsState
    extends ConsumerState<PodcastDownloadControls> {
  bool busy = false;
  final _copy = _EpisodeCopy();

  String get _key => widget.episode['episode_key'] as String;
  bool get _resumes => (widget.episode['position_seconds'] as num? ?? 0) > 0;

  Future<void> play({bool fromBeginning = false}) async {
    setState(() => busy = true);
    try {
      final String scope =
          widget.scope ?? (await ref.read(podcastScopeProvider.future));
      final request = await ref.read(podcastDownloadsProvider).playbackRequest(
          scope, widget.feed, widget.episode,
          fromBeginning: fromBeginning, allowStreaming: true);
      if (!mounted) return;
      setState(() => busy = false);
      await context.push<void>(AppRoutes.podcastPlayer, extra: request);
      if (mounted) widget.onPlaybackReturn?.call();
    } catch (_) {
      if (mounted) {
        _showError(context,
            'Could not play this episode. Check the connection, then retry.');
      }
    } finally {
      if (mounted && busy) setState(() => busy = false);
    }
  }

  void openDetails() {
    showModalBottomSheet<void>(
      context: context,
      useRootNavigator: true,
      isScrollControlled: true,
      useSafeArea: true,
      showDragHandle: true,
      builder: (sheet) => _EpisodeSheet(owner: this),
    );
  }

  @override
  Widget build(BuildContext context) {
    final downloads = ref.watch(podcastDownloadsProvider);
    // Transfers outlive this row and its player route. Refresh on service
    // changes even after returning to a newly mounted list.
    ref.listen(podcastDownloadsProvider, (previous, next) {
      if (mounted) setState(_copy.reset);
    });
    return FutureBuilder<Map<String, Object?>?>(
        future: _copy.future(downloads, widget.feed, _key),
        builder: (context, snapshot) {
          final playButton =
              (widget.vertical ? IconButton.filled : IconButton.filledTonal)(
                  tooltip: _resumes ? 'Resume' : 'Play',
                  style: IconButton.styleFrom(minimumSize: const Size(48, 48)),
                  onPressed: busy ? null : () => play(),
                  icon: const Icon(Icons.play_arrow));
          final moreButton = IconButton(
            tooltip: 'More episode actions',
            style: IconButton.styleFrom(minimumSize: const Size(48, 48)),
            icon: const Icon(Icons.menu),
            onPressed: openDetails,
          );
          final actions = Row(mainAxisSize: MainAxisSize.min, children: [
            _DownloadButton(
                feed: widget.feed,
                episodeKey: _key,
                snapshot: snapshot,
                enabled: !busy),
            const SizedBox(width: 4),
            playButton,
            moreButton,
          ]);
          final body = widget.body;
          if (body == null) return actions;
          if (widget.vertical) {
            final surface = Theme.of(context).colorScheme.surface;
            return Semantics(
                onTapHint: 'Open episode actions',
                child: InkWell(
                    onTap: openDetails,
                    onLongPress: widget.onLongPress,
                    child: Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [
                          Stack(children: [
                            AspectRatio(
                                aspectRatio: 1,
                                child: widget.cover ?? const SizedBox()),
                            Positioned(
                                left: 4,
                                bottom: 4,
                                child: DecoratedBox(
                                    decoration: BoxDecoration(
                                        color: surface.withValues(alpha: 0.85),
                                        shape: BoxShape.circle),
                                    child: _DownloadButton(
                                        feed: widget.feed,
                                        episodeKey: _key,
                                        snapshot: snapshot,
                                        enabled: !busy))),
                            Positioned(right: 4, bottom: 4, child: playButton),
                            Positioned(
                                right: 4,
                                top: 4,
                                child: DecoratedBox(
                                    decoration: BoxDecoration(
                                        color: surface.withValues(alpha: 0.85),
                                        shape: BoxShape.circle),
                                    child: moreButton)),
                          ]),
                          Padding(
                              padding: const EdgeInsets.fromLTRB(8, 8, 8, 10),
                              child: body),
                        ])));
          }
          return InkWell(
              onTap: openDetails,
              onLongPress: widget.onLongPress,
              child: Padding(
                  padding: const EdgeInsets.fromLTRB(16, 8, 4, 8),
                  child: Row(children: [
                    Expanded(child: body),
                    const SizedBox(width: 4),
                    actions,
                  ])));
        });
  }
}

/// Keeps one copy() future per episode, so parent rebuilds never restart it
/// (a fresh FutureBuilder future would leave Download disabled meanwhile).
class _EpisodeCopy {
  String? _key;
  Future<Map<String, Object?>?>? _future;
  Future<Map<String, Object?>?> future(
      PodcastDownloads downloads, String feed, String episode) {
    final key = '$feed\x00$episode';
    if (_future == null || _key != key) {
      _key = key;
      _future = downloads.copy(feed, episode);
    }
    return _future!;
  }

  void reset() {
    _future = null;
    _key = null;
  }
}

void _showError(BuildContext context, String message) =>
    ScaffoldMessenger.maybeOf(context)
        ?.showSnackBar(SnackBar(content: Text(message)));

const _downloadError =
    'Could not finish this action. Check publisher access, connection and device storage, then retry.';

/// Download state as one icon: download, cancel while transferring, retry
/// after a failure, or a passive "downloaded" mark.
class _DownloadButton extends ConsumerStatefulWidget {
  const _DownloadButton(
      {required this.feed,
      required this.episodeKey,
      required this.snapshot,
      required this.enabled});
  final String feed, episodeKey;
  final AsyncSnapshot<Map<String, Object?>?> snapshot;
  final bool enabled;
  @override
  ConsumerState<_DownloadButton> createState() => _DownloadButtonState();
}

class _DownloadButtonState extends ConsumerState<_DownloadButton> {
  bool starting = false;

  Future<void> _run(Future<void> Function() action) async {
    setState(() => starting = true);
    try {
      await action();
    } catch (_) {
      if (mounted) _showError(context, _downloadError);
    } finally {
      if (mounted) setState(() => starting = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final downloads = ref.read(podcastDownloadsProvider);
    final state = widget.snapshot.data?['state'];
    final colors = Theme.of(context).colorScheme;
    if (state == 'available') {
      return Tooltip(
          message: 'Available on this device',
          child: SizedBox(
              width: 48,
              height: 48,
              child: Icon(Icons.download_done, color: colors.primary)));
    }
    if (state == 'pending' || state == 'downloading') {
      return IconButton(
          tooltip: 'Cancel download',
          style: IconButton.styleFrom(minimumSize: const Size(48, 48)),
          onPressed: () => _run(
              () => downloads.cancelEpisode(widget.feed, widget.episodeKey)),
          icon: Icon(Icons.downloading,
              color: colors.primary,
              semanticLabel:
                  state == 'pending' ? 'Download pending' : 'Downloading'));
    }
    final failed = state == 'failed';
    return IconButton(
        tooltip: failed ? 'Retry download' : 'Download',
        style: IconButton.styleFrom(minimumSize: const Size(48, 48)),
        onPressed: !widget.enabled || starting
            ? null
            : () =>
                _run(() => downloads.download(widget.feed, widget.episodeKey)),
        icon: Icon(failed ? Icons.sync_problem : Icons.download_outlined,
            color: failed ? colors.error : null));
  }
}

/// Episode details: title, status, Play/Resume, download management, every
/// secondary action and the show notes, in one place.
class _EpisodeSheet extends ConsumerStatefulWidget {
  const _EpisodeSheet({required this.owner});
  final PodcastDownloadControlsState owner;
  @override
  ConsumerState<_EpisodeSheet> createState() => _EpisodeSheetState();
}

class _EpisodeSheetState extends ConsumerState<_EpisodeSheet> {
  final _copy = _EpisodeCopy();
  bool busy = false;
  String? error;

  PodcastDownloadControls get w => widget.owner.widget;
  String get _key => w.episode['episode_key'] as String;

  Future<void> run(Future<void> Function() action) async {
    setState(() {
      busy = true;
      error = null;
    });
    try {
      await action();
    } catch (_) {
      if (mounted) setState(() => error = _downloadError);
    } finally {
      if (mounted) {
        setState(() {
          _copy.reset();
          busy = false;
        });
      }
    }
  }

  /// Closes the sheet, then runs [action] on the episode row.
  void closeThen(VoidCallback? action) {
    Navigator.of(context).pop();
    if (widget.owner.mounted) action?.call();
  }

  @override
  Widget build(BuildContext context) {
    final downloads = ref.watch(podcastDownloadsProvider);
    ref.listen(podcastDownloadsProvider, (previous, next) {
      if (mounted) setState(_copy.reset);
    });
    final theme = Theme.of(context);
    final title = w.detailsTitle ?? w.episode['title'] as String? ?? 'Episode';
    final notes =
        (w.detailsBody ?? w.episode['description'] as String? ?? '').trim();
    final resumes = widget.owner._resumes;
    return FutureBuilder<Map<String, Object?>?>(
        future: _copy.future(downloads, w.feed, _key),
        builder: (context, snapshot) {
          final state = snapshot.data?['state'];
          final transferring = state == 'pending' || state == 'downloading';
          final available = state == 'available';
          final status = snapshot.hasError
              ? 'Could not read device download.'
              : error ??
                  switch (state) {
                    'available' => 'Available on this device',
                    'pending' => 'Download pending',
                    'downloading' => 'Downloading…',
                    'failed' =>
                      snapshot.data?['error'] as String? ?? 'Download failed.',
                    _ => 'Streams from the publisher',
                  };
          return SafeArea(
              child: SingleChildScrollView(
                  key: const Key('podcast_episode_sheet'),
                  padding: const EdgeInsets.fromLTRB(0, 0, 0, 16),
                  child: Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Padding(
                            padding: const EdgeInsets.symmetric(horizontal: 24),
                            child:
                                Text(title, style: theme.textTheme.titleLarge)),
                        Padding(
                            padding: const EdgeInsets.fromLTRB(24, 4, 24, 0),
                            child: PodcastPublicationDate(
                                date: podcastPublicationDate(
                                    w.episode['published_at']))),
                        Padding(
                            padding: const EdgeInsets.fromLTRB(24, 4, 24, 12),
                            child: Text(status,
                                style: theme.textTheme.bodyMedium?.copyWith(
                                    color: error != null || state == 'failed'
                                        ? theme.colorScheme.error
                                        : theme.colorScheme.onSurfaceVariant))),
                        Padding(
                            padding: const EdgeInsets.symmetric(horizontal: 24),
                            child: Wrap(spacing: 8, runSpacing: 8, children: [
                              FilledButton.icon(
                                  style: FilledButton.styleFrom(
                                      minimumSize: const Size(48, 48)),
                                  onPressed: widget.owner.busy
                                      ? null
                                      : () =>
                                          closeThen(() => widget.owner.play()),
                                  icon: const Icon(Icons.play_arrow),
                                  label: Text(resumes ? 'Resume' : 'Play')),
                              if (transferring)
                                OutlinedButton.icon(
                                    style: OutlinedButton.styleFrom(
                                        minimumSize: const Size(48, 48)),
                                    onPressed: () => run(() =>
                                        downloads.cancelEpisode(w.feed, _key)),
                                    icon: const Icon(Icons.close),
                                    label: const Text('Cancel download'))
                              else if (!available)
                                OutlinedButton.icon(
                                    style: OutlinedButton.styleFrom(
                                        minimumSize: const Size(48, 48)),
                                    onPressed: busy
                                        ? null
                                        : () => run(() =>
                                            downloads.download(w.feed, _key)),
                                    icon: const Icon(Icons.download_outlined),
                                    label: Text(state == 'failed'
                                        ? 'Retry download'
                                        : 'Download')),
                            ])),
                        const SizedBox(height: 8),
                        if (resumes || available)
                          ListTile(
                              leading: const Icon(Icons.replay),
                              title: const Text('Play from beginning'),
                              enabled: !widget.owner.busy,
                              onTap: () => closeThen(() =>
                                  widget.owner.play(fromBeginning: true))),
                        if (w.completeLabel != null)
                          ListTile(
                              leading: Icon(
                                  w.completeLabel == 'Mark uncompleted'
                                      ? Icons.undo
                                      : Icons.check),
                              title: Text(w.completeLabel!),
                              enabled: w.onToggleComplete != null,
                              onTap: () => closeThen(w.onToggleComplete)),
                        if (available)
                          ListTile(
                              leading: const Icon(Icons.delete_outline),
                              title: const Text('Delete downloaded file'),
                              enabled: !busy,
                              onTap: () =>
                                  run(() => downloads.remove(w.feed, _key))),
                        if (w.onDeleteEpisode != null)
                          ListTile(
                              leading:
                                  const Icon(Icons.delete_forever_outlined),
                              title: const Text('Delete episode'),
                              onTap: () => closeThen(w.onDeleteEpisode)),
                        if (notes.isNotEmpty)
                          Padding(
                              padding:
                                  const EdgeInsets.symmetric(horizontal: 24),
                              child: PodcastShowNotes(description: notes)),
                      ])));
        });
  }
}
