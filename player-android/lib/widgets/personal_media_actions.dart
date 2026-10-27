import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers/personal_media_provider.dart';
import '../services/playback_request.dart';

import '../models/media_bookmark.dart';
import '../screens/notes_editor_screen.dart';
import '../services/personal_media_store.dart';
import '../utils/duration_formatter.dart';
import 'overflow_menu.dart';

/// Keeps the account binding for the life of this playback route, including
/// rebuilds caused by playback or an account being locked in the background.
class PlaybackPersonalActions extends ConsumerStatefulWidget {
  const PlaybackPersonalActions(
      {super.key,
      required this.request,
      required this.position,
      required this.duration,
      required this.onSeek});
  final PlaybackRequest request;
  final Duration Function() position;
  final Duration? Function() duration;
  final Future<void> Function(Duration) onSeek;
  @override
  ConsumerState<PlaybackPersonalActions> createState() =>
      _PlaybackPersonalActionsState();
}

class _PlaybackPersonalActionsState
    extends ConsumerState<PlaybackPersonalActions> {
  PersonalMediaStore? _store;
  @override
  void initState() {
    super.initState();
    _store = playbackPersonalStore(ref, widget.request);
  }

  @override
  void didUpdateWidget(covariant PlaybackPersonalActions oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.request.identity != widget.request.identity) {
      _store = playbackPersonalStore(ref, widget.request);
    }
  }

  @override
  Widget build(BuildContext context) => _store == null
      ? const SizedBox.shrink()
      : PersonalMediaActions(
          store: _store!,
          title: widget.request.title,
          position: widget.position,
          duration: widget.duration,
          onSeek: widget.onSeek);
}

/// Binds the personal-data store the first time a playback request is shown
/// and keeps it for that request, like [PlaybackPersonalActions] does for its
/// own lifetime, so menu rows built later still use the original account.
class PlaybackPersonalBinding {
  String? _identity;
  PersonalMediaStore? _store;

  PersonalMediaStore? storeFor(WidgetRef ref, PlaybackRequest request) {
    if (_identity != request.identity) {
      _identity = request.identity;
      _store = playbackPersonalStore(ref, request);
    }
    return _store;
  }
}

/// Bookmarks and Notes rows for a player's [OverflowMenuButton]. [context]
/// is the player's, since the menu closes before an action runs.
List<Widget> personalMediaMenuItems(
  BuildContext context, {
  required PersonalMediaStore store,
  required String title,
  required Duration Function() position,
  required Duration? Function() duration,
  required Future<void> Function(Duration) onSeek,
}) =>
    [
      OverflowMenuItem(
        key: const Key('playback_bookmarks_item'),
        icon: Icons.bookmarks_outlined,
        label: 'Bookmarks',
        onSelected: () => showModalBottomSheet<void>(
            context: context,
            isScrollControlled: true,
            useSafeArea: true,
            builder: (_) => BookmarkSheet(
                store: store,
                position: position,
                duration: duration,
                onSeek: onSeek)),
      ),
      OverflowMenuItem(
        key: const Key('playback_notes_item'),
        icon: Icons.notes_outlined,
        label: 'Notes',
        onSelected: () => openPersonalNotes(context, store, title),
      ),
    ];

/// Visible playback actions shared by device, downloaded and server media.
class PersonalMediaActions extends StatelessWidget {
  const PersonalMediaActions(
      {super.key,
      required this.store,
      required this.title,
      this.position,
      this.duration,
      this.onSeek});
  final PersonalMediaStore store;
  final String title;
  final Duration Function()? position;
  final Duration? Function()? duration;
  final Future<void> Function(Duration)? onSeek;

  @override
  Widget build(BuildContext context) =>
      Row(mainAxisSize: MainAxisSize.min, children: [
        if (position != null)
          IconButton(
              tooltip: 'Bookmarks',
              icon: const Icon(Icons.bookmarks_outlined),
              onPressed: () => showModalBottomSheet<void>(
                  context: context,
                  isScrollControlled: true,
                  useSafeArea: true,
                  builder: (_) => BookmarkSheet(
                      store: store,
                      position: position!,
                      duration: duration,
                      onSeek: onSeek!))),
        IconButton(
            tooltip: 'Notes',
            icon: const Icon(Icons.notes_outlined),
            onPressed: () => openPersonalNotes(context, store, title)),
        if (position == null) FavoriteButton(store: store),
      ]);
}

Future<void> openPersonalNotes(
        BuildContext context, PersonalMediaStore store, String title) =>
    Navigator.of(context).push<void>(MaterialPageRoute(
        builder: (_) => NotesEditorScreen(
            mediaId: store.identity, mediaTitle: title, store: store)));

class FavoriteButton extends StatefulWidget {
  const FavoriteButton({super.key, required this.store, this.revision = 0});
  final PersonalMediaStore store;
  final int revision;
  @override
  State<FavoriteButton> createState() => _FavoriteButtonState();
}

class _FavoriteButtonState extends State<FavoriteButton> {
  bool? _favorite;
  bool _busy = false;
  Object? _error;
  int _generation = 0;
  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void didUpdateWidget(covariant FavoriteButton oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.store.identity != widget.store.identity ||
        oldWidget.revision != widget.revision) {
      _generation++;
      _favorite = null;
      _busy = false;
      _error = null;
      _load();
    }
  }

  Future<void> _load() async {
    if (_busy) return;
    final generation = _generation;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final favorite = await widget.store.getFavorite();
      if (mounted && generation == _generation) {
        setState(() {
          _favorite = favorite;
          _error = null;
        });
      }
    } catch (error) {
      if (mounted && generation == _generation) {
        setState(() => _error = error);
      }
    } finally {
      if (mounted && generation == _generation) setState(() => _busy = false);
    }
  }

  Future<void> _toggle() async {
    if (_busy) return;
    if (_error != null) {
      await _load();
      return;
    }
    final generation = _generation;
    setState(() => _busy = true);
    try {
      final favorite = await widget.store.toggleFavorite();
      if (mounted && generation == _generation) {
        setState(() => _favorite = favorite);
      }
    } catch (_) {
      if (mounted && generation == _generation) {
        ScaffoldMessenger.of(context).showSnackBar(const SnackBar(
            content: Text('Could not save favorite. Please try again.')));
      }
    } finally {
      if (mounted && generation == _generation) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => IconButton(
      tooltip: _error != null
          ? 'Retry favorite'
          : _favorite == true
              ? 'Remove from favorites'
              : 'Add to favorites',
      onPressed:
          _busy || (_favorite == null && _error == null) ? null : _toggle,
      icon: Icon(_error != null
          ? Icons.refresh
          : _favorite == true
              ? Icons.favorite
              : Icons.favorite_border));
}

class BookmarkSheet extends StatefulWidget {
  const BookmarkSheet(
      {super.key,
      required this.store,
      required this.position,
      required this.onSeek,
      this.duration});
  final PersonalMediaStore store;
  final Duration Function() position;
  final Duration? Function()? duration;
  final Future<void> Function(Duration) onSeek;
  @override
  State<BookmarkSheet> createState() => _BookmarkSheetState();
}

class _BookmarkSheetState extends State<BookmarkSheet> {
  List<MediaBookmark>? _bookmarks;
  final _label = TextEditingController();
  String? _error;
  bool _busy = false;
  int _generation = 0;
  int _loadRequest = 0;
  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void didUpdateWidget(covariant BookmarkSheet oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.store.identity != widget.store.identity) {
      _generation++;
      _bookmarks = null;
      _busy = false;
      _error = null;
      _label.clear();
      _load();
    }
  }

  @override
  void dispose() {
    _label.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    final generation = _generation;
    final request = ++_loadRequest;
    try {
      final bookmarks = await widget.store.listBookmarks();
      if (bookmarks.any(
          (bookmark) => !validBookmarkPosition(bookmark.positionSeconds))) {
        throw const FormatException('Invalid bookmark position.');
      }
      if (mounted && generation == _generation && request == _loadRequest) {
        setState(() {
          _bookmarks = bookmarks;
          _error = null;
        });
      }
    } catch (_) {
      if (mounted && generation == _generation && request == _loadRequest) {
        setState(() => _error = 'Could not load bookmarks. Please retry.');
      }
    }
  }

  Future<void> _mutate(Future<void> Function(PersonalMediaStore) action,
      {bool clearLabel = false}) async {
    if (_busy) return;
    final generation = _generation;
    final store = widget.store;
    _loadRequest++;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await action(store);
      if (!mounted || generation != _generation) return;
      if (clearLabel) _label.clear();
      await _load();
    } catch (_) {
      if (mounted && generation == _generation) {
        setState(
            () => _error = 'Could not save bookmark change. Please try again.');
      }
    } finally {
      if (mounted && generation == _generation) setState(() => _busy = false);
    }
  }

  Future<void> _save() async {
    late final Duration position;
    Duration? duration;
    try {
      position = widget.position();
      duration = widget.duration?.call();
    } catch (_) {
      setState(() => _error =
          'Playback changed. Reopen bookmarks from the current player.');
      return;
    }
    final seconds = position.inMilliseconds / 1000;
    if (_label.text.trim().runes.length > 200) {
      setState(() => _error = 'Use a label of at most 200 characters.');
      return;
    }
    if (position < Duration.zero ||
        (duration != null && duration > Duration.zero && position > duration)) {
      setState(() => _error = 'The current position is outside this file.');
      return;
    }
    final label = _label.text;
    await _mutate((store) async {
      await store.addBookmark(seconds, label);
    }, clearLabel: true);
  }

  Future<void> _jump(MediaBookmark bookmark) async {
    if (_busy) return;
    setState(() => _busy = true);
    final generation = _generation;
    try {
      final duration = widget.duration?.call();
      var position =
          Duration(milliseconds: (bookmark.positionSeconds * 1000).round());
      if (duration != null && duration > Duration.zero && position > duration) {
        position = duration;
      }
      await widget.onSeek(position);
      if (mounted &&
          generation == _generation &&
          ModalRoute.of(context)?.isCurrent == true) {
        Navigator.pop(context);
      }
    } catch (_) {
      if (mounted && generation == _generation) {
        setState(() {
          _busy = false;
          _error = 'Could not jump to this bookmark. Please try again.';
        });
      }
    }
  }

  @override
  Widget build(BuildContext context) => SingleChildScrollView(
        padding: EdgeInsets.fromLTRB(
            16, 16, 16, 16 + MediaQuery.viewInsetsOf(context).bottom),
        child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text('Bookmarks', style: Theme.of(context).textTheme.titleLarge),
              TextField(
                  key: const Key('bookmark_label'),
                  controller: _label,
                  readOnly: _busy,
                  decoration: const InputDecoration(
                      labelText: 'Bookmark label (optional)')),
              const SizedBox(height: 8),
              FilledButton.icon(
                  onPressed: _busy ? null : _save,
                  icon: const Icon(Icons.bookmark_add_outlined),
                  label: const Text('Save current position')),
              if (_busy) const LinearProgressIndicator(),
              if (_error != null)
                Row(children: [
                  Expanded(child: Text(_error!)),
                  TextButton(
                      onPressed: _busy ? null : _load,
                      child: const Text('Retry'))
                ]),
              if (_bookmarks == null && _error == null)
                const Padding(
                    padding: EdgeInsets.all(24),
                    child: Center(child: CircularProgressIndicator())),
              if (_bookmarks?.isEmpty == true)
                const Padding(
                    padding: EdgeInsets.all(24),
                    child: Text('No saved positions yet.',
                        textAlign: TextAlign.center)),
              if (_bookmarks != null)
                for (final bookmark in _bookmarks!)
                  ListTile(
                    title: Text(bookmark.label.isEmpty
                        ? 'Saved position'
                        : bookmark.label),
                    subtitle: Text(formatDuration(bookmark.positionSeconds)),
                    leading: const Icon(Icons.play_arrow),
                    onTap: _busy ? null : () => _jump(bookmark),
                    trailing: IconButton(
                        tooltip: 'Delete bookmark',
                        icon: const Icon(Icons.delete_outline),
                        onPressed: _busy
                            ? null
                            : () => _mutate(
                                (store) => store.deleteBookmark(bookmark.id))),
                  ),
            ]),
      );
}
