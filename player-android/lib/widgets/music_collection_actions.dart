import 'music_pin_button.dart';
import '../l10n/music_localizations.dart';
import '../services/music_action_intent.dart';
import 'music_download_dialog.dart';
import '../screens/create_share_dialog.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers/api_client_provider.dart';
import '../providers/playback_session_provider.dart';
import '../services/music_collections.dart';
import '../services/music_queue.dart';
import '../services/music_shuffle.dart';
import 'music_shuffle_options_dialog.dart';
import 'overflow_menu.dart';
import 'server_song_actions.dart';

/// Whole collections resolve atomically before using the normal queue owner.
///
/// Detail headers show Play + Shuffle + hamburger More. Compact card embeds
/// show only the More sheet so related/discovery rows stay uncluttered.
class MusicCollectionActions extends ConsumerStatefulWidget {
  const MusicCollectionActions(
      {super.key,
      required this.collections,
      required this.playLabel,
      this.enabled = true,
      this.playOnly = false,
      this.compact = false,
      this.sourceLabel = 'Selected collections'});
  final List<MusicCollection> collections;
  final String playLabel, sourceLabel;
  final bool enabled, playOnly, compact;
  @override
  ConsumerState<MusicCollectionActions> createState() => _ActionsState();
}

class _ActionsState extends ConsumerState<MusicCollectionActions> {
  bool _busy = false;
  String? _message;
  bool _routeCurrent = true;
  int _departureGeneration = 0;

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final current = ModalRoute.of(context)?.isCurrent ?? true;
    // A pushed route permanently cancels pending resolution. Returning with
    // Back must never revive the source intent that belonged to its old visit.
    if (_routeCurrent && !current) _departureGeneration++;
    _routeCurrent = current;
  }

  Future<bool> _run(QueueInsertion action,
      {bool replace = false,
      bool shuffled = false,
      MusicShuffleOptions? options,
      bool Function()? dialogCurrent,
      void Function(String)? report}) async {
    if (_busy || !widget.enabled || widget.collections.isEmpty) return false;
    final mutations = ref.read(credentialMutationQueueProvider);
    final epoch = mutations.generation;
    final coordinator = ref.read(playbackSessionCoordinatorProvider);
    final source = coordinator.generation;
    final api = ref.read(apiClientProvider);
    final operation = beginMusicActionIntent();
    final departure = _departureGeneration;
    bool current() =>
        mounted &&
        isMusicActionIntent(operation) &&
        departure == _departureGeneration &&
        epoch == mutations.generation &&
        source == coordinator.generation &&
        (dialogCurrent?.call() ?? (ModalRoute.of(context)?.isCurrent ?? true));
    setState(() {
      _busy = true;
      _message = context.tr('Loading songs…');
      report?.call(_message!);
    });
    try {
      var songs =
          await resolveMusicCollections(api, widget.collections, current);
      if (!mounted || !current()) return false;
      if (shuffled) {
        songs = (options ?? const MusicShuffleOptions()).select(songs);
      }
      if (songs.isEmpty) {
        setState(() => _message = shuffled
            ? context.tr('No songs match these options.')
            : context.tr('No songs available.'));
        report?.call(_message!);
        return false;
      }
      await insertServerSongs(context, ref, songs, action,
          replace: replace,
          shuffled: shuffled,
          actionIntent: operation,
          sourceLabel: widget.sourceLabel);
      if (mounted && epoch == mutations.generation) {
        setState(() => _message = null);
        return true;
      }
    } catch (_) {
      if (current()) {
        setState(() => _message = context.tr(
            'Could not load songs. Check your connection and access, then try again.'));
        report?.call(_message!);
      }
    } finally {
      if (mounted) {
        setState(() {
          _busy = false;
          if (_message == 'Loading songs…') _message = null;
        });
      }
    }
    return false;
  }

  Future<void> _download({bool share = false}) async {
    if (_busy || !widget.enabled || widget.collections.isEmpty) return;
    final mutations = ref.read(credentialMutationQueueProvider);
    final epoch = mutations.generation;
    final departure = _departureGeneration;
    bool current() =>
        mounted &&
        epoch == mutations.generation &&
        departure == _departureGeneration &&
        (ModalRoute.of(context)?.isCurrent ?? true);
    setState(() {
      _busy = true;
      _message = context.tr('Loading songs…');
    });
    try {
      final songs = await resolveMusicCollections(
          ref.read(apiClientProvider), widget.collections, current);
      if (mounted && current()) {
        if (songs.isEmpty) {
          setState(() => _message = context.tr('No songs available.'));
        } else {
          setState(() => _message = null);
          if (share) {
            await showCreateShareDialog(context,
                client: ref.read(apiClientProvider),
                mediaIds: songs.map((song) => song.id).toList(),
                collectionTitle: widget.sourceLabel);
          } else {
            await showMusicDownloadDialog(context, ref, songs);
          }
        }
      }
    } catch (_) {
      if (current()) {
        setState(() => _message = context.tr(
            'Could not load songs. Check your connection and access, then retry.'));
      }
    } finally {
      if (mounted) {
        setState(() {
          _busy = false;
          if (_message == 'Loading songs…') _message = null;
        });
      }
    }
  }

  bool get _canPin =>
      widget.collections.length == 1 &&
      const ['album', 'playlist'].contains(widget.collections.single.kind);

  List<Widget> _moreItems(bool enabled, {required bool includePlayShuffle}) {
    final items = <Widget>[
      if (includePlayShuffle) ...[
        OverflowMenuItem(
            icon: Icons.play_arrow,
            label: widget.playLabel,
            onSelected: enabled
                ? () => _run(QueueInsertion.now, replace: true)
                : null),
        OverflowMenuItem(
            icon: Icons.shuffle,
            label: context.tr('Shuffle'),
            onSelected: enabled
                ? () => _run(QueueInsertion.now, replace: true, shuffled: true)
                : null),
      ],
      OverflowMenuItem(
          icon: Icons.download_outlined,
          label: context.tr('Keep offline'),
          onSelected: enabled ? _download : null),
      if (_canPin)
        OverflowMenuItem(
            icon: Icons.push_pin_outlined,
            label: 'Pin offline…',
            onSelected: enabled
                ? () => showMusicPinDialog(context,
                    collection: widget.collections.single,
                    title: widget.sourceLabel)
                : null),
      OverflowMenuItem(
          icon: Icons.share_outlined,
          label: context.tr('Share…'),
          onSelected: enabled ? () => _download(share: true) : null),
      for (final action in [QueueInsertion.next, QueueInsertion.append])
        OverflowMenuItem(
            icon: action == QueueInsertion.next
                ? Icons.playlist_play
                : Icons.playlist_add,
            label: context.tr(queueActionLabel(action)),
            onSelected: enabled ? () => _run(action) : null),
      OverflowMenuItem(
          icon: Icons.tune,
          label: context.tr('Shuffle options…'),
          onSelected: enabled
              ? () => showDialog<void>(
                  context: context,
                  builder: (_) => MusicShuffleOptionsDialog(
                      run: (options, current, report) => _run(QueueInsertion.now,
                          replace: true,
                          shuffled: true,
                          options: options,
                          dialogCurrent: current,
                          report: report)))
              : null),
    ];
    return items;
  }

  Widget _messageBanner() => _message == null
      ? const SizedBox.shrink()
      : Padding(
          padding: const EdgeInsets.symmetric(vertical: 8),
          child: Semantics(liveRegion: true, child: Text(_message!)));

  @override
  Widget build(BuildContext context) {
    final enabled = widget.enabled && widget.collections.isNotEmpty && !_busy;
    if (widget.playOnly) {
      return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        FilledButton.icon(
            onPressed: enabled
                ? () => _run(QueueInsertion.now, replace: true)
                : null,
            icon: const Icon(Icons.play_arrow),
            label: Text(widget.playLabel)),
        _messageBanner(),
      ]);
    }
    if (widget.compact) {
      return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        OverflowMenuButton(
            enabled: enabled,
            items: (_) => _moreItems(enabled, includePlayShuffle: true)),
        _messageBanner(),
      ]);
    }
    return Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
      Wrap(
          spacing: 8,
          runSpacing: 8,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: [
            FilledButton.icon(
                onPressed: enabled
                    ? () => _run(QueueInsertion.now, replace: true)
                    : null,
                icon: const Icon(Icons.play_arrow),
                label: Text(widget.playLabel)),
            OutlinedButton.icon(
                onPressed: enabled
                    ? () => _run(QueueInsertion.now,
                        replace: true, shuffled: true)
                    : null,
                style:
                    OutlinedButton.styleFrom(minimumSize: const Size(48, 48)),
                icon: const Icon(Icons.shuffle),
                label: Text(context.tr('Shuffle'))),
            OverflowMenuButton(
                enabled: enabled,
                items: (_) =>
                    _moreItems(enabled, includePlayShuffle: false)),
          ]),
      _messageBanner(),
    ]);
  }
}

/// Discoverable native selection shared by album, artist and playlist catalogs.
class MusicCollectionCatalog extends StatefulWidget {
  const MusicCollectionCatalog(
      {super.key,
      required this.items,
      required this.kind,
      required this.keyName,
      required this.nameName,
      required this.itemBuilder,
      this.header,
      this.listHeader,
      this.footer,
      this.grid = false,
      required this.onRefresh,
      this.emptyMessage = 'No collections available.',
      this.scrollController});
  final List<Map<String, dynamic>> items;
  final String kind, keyName, nameName, emptyMessage;
  final Widget Function(
      Map<String, dynamic>, Widget? checkbox, VoidCallback? toggle) itemBuilder;
  final Widget? header, listHeader, footer;
  final bool grid;
  final ScrollController? scrollController;
  final Future<void> Function() onRefresh;
  @override
  State<MusicCollectionCatalog> createState() => _CatalogState();
}

class _CatalogState extends State<MusicCollectionCatalog> {
  bool _selecting = false;
  final _selected = <String>{};
  void _cancel() => setState(() {
        _selecting = false;
        _selected.clear();
      });
  @override
  Widget build(BuildContext context) {
    final plural = '${widget.kind}s';
    Widget item(int index) {
      final collection = widget.items[index];
      final key = collection[widget.keyName] as String;
      void toggle() => setState(() {
            if (!_selected.add(key)) _selected.remove(key);
          });
      return widget.itemBuilder(
          collection,
          _selecting
              ? Checkbox(
                  value: _selected.contains(key),
                  semanticLabel: context.tr(
                      'Select {name}', {'name': collection[widget.nameName]}),
                  onChanged: (_) => toggle())
              : null,
          _selecting ? toggle : null);
    }

    return PopScope(
        canPop: !_selecting,
        onPopInvokedWithResult: (didPop, _) {
          if (!didPop && _selecting) _cancel();
        },
        child:
            Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
          // Browsing controls take most of a narrow screen once selection
          // actions appear. Keep the collection list reachable by touch.
          if (!_selecting && widget.header != null) widget.header!,
          Padding(
              padding: const EdgeInsets.symmetric(horizontal: 8),
              child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    TextButton(
                        onPressed: _selecting
                            ? _cancel
                            : () => setState(() => _selecting = true),
                        child: Text(_selecting
                            ? context.tr('Cancel selection')
                            : context.tr('Select $plural'))),
                    if (_selecting) ...[
                      Text(context
                          .tr('{count} selected', {'count': _selected.length})),
                      MusicCollectionActions(
                          collections: [
                            for (final collection in widget.items)
                              if (_selected
                                  .contains(collection[widget.keyName]))
                                MusicCollection(widget.kind,
                                    collection[widget.keyName] as String)
                          ],
                          playLabel: context.tr('Play selected'),
                          sourceLabel: context.tr('Selected $plural')),
                    ],
                  ])),
          if (widget.items.isEmpty)
            Padding(
                padding: const EdgeInsets.all(24),
                child: Text(context.tr(widget.emptyMessage))),
          Expanded(
              child: RefreshIndicator(
                  onRefresh: widget.onRefresh,
                  child: widget.grid
                      ? LayoutBuilder(
                          builder: (context, box) => GridView.builder(
                              padding: const EdgeInsets.all(12),
                              gridDelegate:
                                  SliverGridDelegateWithFixedCrossAxisCount(
                                      crossAxisCount: (box.maxWidth / 180)
                                          .floor()
                                          .clamp(2, 6),
                                      crossAxisSpacing: 12,
                                      mainAxisSpacing: 12,
                                      childAspectRatio: 0.72),
                              itemCount: widget.items.length,
                              itemBuilder: (_, index) => item(index)))
                      : ListView.builder(
                          controller: widget.scrollController,
                          padding: const EdgeInsets.all(16),
                          itemCount: widget.items.length +
                              (widget.listHeader == null ? 0 : 1) +
                              (widget.footer == null ? 0 : 1),
                          itemBuilder: (_, index) {
                            if (widget.listHeader != null && index == 0) {
                              return widget.listHeader!;
                            }
                            final at =
                                index - (widget.listHeader == null ? 0 : 1);
                            return at == widget.items.length
                                ? widget.footer!
                                : item(at);
                          }))),
        ]));
  }
}
