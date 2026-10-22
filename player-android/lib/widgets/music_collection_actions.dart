import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers/api_client_provider.dart';
import '../providers/playback_session_provider.dart';
import '../services/music_collections.dart';
import '../services/music_queue.dart';
import '../services/music_shuffle.dart';
import 'music_shuffle_options_dialog.dart';
import 'server_song_actions.dart';

class _ResolutionOwner {
  int generation = 0;
}

final _resolutionOwnerProvider = Provider((ref) => _ResolutionOwner());

/// Whole collections resolve atomically before using the normal queue owner.
class MusicCollectionActions extends ConsumerStatefulWidget {
  const MusicCollectionActions(
      {super.key,
      required this.collections,
      required this.playLabel,
      this.enabled = true,
      this.sourceLabel = 'Selected collections'});
  final List<MusicCollection> collections;
  final String playLabel, sourceLabel;
  final bool enabled;
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
    final owner = ref.read(_resolutionOwnerProvider);
    final operation = ++owner.generation;
    final departure = _departureGeneration;
    bool current() =>
        mounted &&
        operation == owner.generation &&
        departure == _departureGeneration &&
        epoch == mutations.generation &&
        source == coordinator.generation &&
        (dialogCurrent?.call() ?? (ModalRoute.of(context)?.isCurrent ?? true));
    setState(() {
      _busy = true;
      _message = 'Loading songs…';
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
        setState(() => _message =
            shuffled ? 'No songs match these options.' : 'No songs available.');
        report?.call(_message!);
        return false;
      }
      await insertServerSongs(context, ref, songs, action,
          replace: replace,
          shuffled: shuffled,
          sourceLabel: widget.sourceLabel);
      if (mounted && epoch == mutations.generation) {
        setState(() => _message = null);
        return true;
      }
    } catch (_) {
      if (current()) {
        setState(() => _message =
            'Could not load songs. Check your connection and access, then try again.');
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

  @override
  Widget build(BuildContext context) {
    final enabled = widget.enabled && widget.collections.isNotEmpty && !_busy;
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
                    ? () =>
                        _run(QueueInsertion.now, replace: true, shuffled: true)
                    : null,
                style:
                    OutlinedButton.styleFrom(minimumSize: const Size(48, 48)),
                icon: const Icon(Icons.shuffle),
                label: const Text('Shuffle')),
            // Keep the native popup semantic target within its own control;
            // otherwise the custom-child tap semantics can merge into Wrap.
            Semantics(
                container: true,
                child: PopupMenuButton<String>(
                    tooltip: 'Queue actions',
                    enabled: enabled,
                    onSelected: (action) {
                      if (action == 'options') {
                        showDialog<void>(
                            context: context,
                            builder: (_) => MusicShuffleOptionsDialog(
                                run: (options, current, report) => _run(
                                    QueueInsertion.now,
                                    replace: true,
                                    shuffled: true,
                                    options: options,
                                    dialogCurrent: current,
                                    report: report)));
                      } else {
                        _run(QueueInsertion.values.byName(action));
                      }
                    },
                    itemBuilder: (_) => [
                          for (final action in [
                            QueueInsertion.next,
                            QueueInsertion.append
                          ])
                            PopupMenuItem(
                                value: action.name,
                                child: Text(queueActionLabel(action))),
                          const PopupMenuItem(
                              value: 'options', child: Text('Shuffle options…'))
                        ],
                    child: ConstrainedBox(
                        constraints: const BoxConstraints(minHeight: 48),
                        child: const Padding(
                            padding: EdgeInsets.all(12),
                            child: Text('Queue actions ▾'))))),
          ]),
      if (_message != null)
        Padding(
            padding: const EdgeInsets.symmetric(vertical: 8),
            child: Semantics(liveRegion: true, child: Text(_message!))),
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
      this.grid = false,
      required this.onRefresh,
      this.emptyMessage = 'No collections available.',
      this.scrollController});
  final List<Map<String, dynamic>> items;
  final String kind, keyName, nameName, emptyMessage;
  final Widget Function(
      Map<String, dynamic>, Widget? checkbox, VoidCallback? toggle) itemBuilder;
  final Widget? header;
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
                  semanticLabel: 'Select ${collection[widget.nameName]}',
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
          if (widget.header != null) widget.header!,
          Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
              child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    TextButton(
                        onPressed: _selecting
                            ? _cancel
                            : () => setState(() => _selecting = true),
                        child: Text(_selecting
                            ? 'Cancel selection'
                            : 'Select $plural')),
                    if (_selecting) ...[
                      Text('${_selected.length} selected'),
                      MusicCollectionActions(
                          collections: [
                            for (final collection in widget.items)
                              if (_selected
                                  .contains(collection[widget.keyName]))
                                MusicCollection(widget.kind,
                                    collection[widget.keyName] as String)
                          ],
                          playLabel: 'Play selected',
                          sourceLabel: 'Selected $plural'),
                    ],
                  ])),
          if (widget.items.isEmpty)
            Padding(
                padding: const EdgeInsets.all(24),
                child: Text(widget.emptyMessage)),
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
                          itemCount: widget.items.length,
                          itemBuilder: (_, index) => item(index)))),
        ]));
  }
}
