import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/models.dart';
import '../providers/personal_media_provider.dart';
import '../services/personal_media_store.dart';
import '../utils/error_mappers.dart';

// ---------------------------------------------------------------------------
// NotesEditorScreen
// ---------------------------------------------------------------------------

/// Full-screen text editor for a user's personal note attached to a media item.
///
/// Changes are debounced while editing and flushed before Back navigation.
/// Saves run in order, including edits made while an earlier request is pending.
class NotesEditorScreen extends ConsumerStatefulWidget {
  /// The string form of the media ID extracted from the '/notes/:mediaId' route.
  final String mediaId;

  final PersonalMediaStore? store;
  final String? mediaTitle;
  const NotesEditorScreen(
      {super.key, required this.mediaId, this.store, this.mediaTitle});

  @override
  ConsumerState<NotesEditorScreen> createState() => _NotesEditorScreenState();
}

class _NotesEditorScreenState extends ConsumerState<NotesEditorScreen> {
  late final TextEditingController _controller;
  late PersonalMediaStore _store;
  Note? _note;
  String? _error;
  bool _isLoading = false;
  bool _loaded = false;
  bool _isSaving = false;
  bool _isClearing = false;
  bool _isLeaving = false;
  bool _didPop = false;
  bool _allowPop = false;
  Timer? _debounce;
  Future<bool>? _saveFuture;
  String _savedContent = '';

  // Async work belongs to the media item that started it. A route update must
  // not apply an old response or send its follow-up draft to the new item.
  int _generation = 0;
  bool get _dirty => _loaded && _controller.text != _savedContent;

  @override
  void initState() {
    super.initState();
    _store = widget.store ??
        serverPersonalStore(ref, int.tryParse(widget.mediaId) ?? 0);
    _controller = TextEditingController()..addListener(_onTextChanged);
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
  }

  @override
  void didUpdateWidget(covariant NotesEditorScreen oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.mediaId == widget.mediaId &&
        oldWidget.store?.identity == widget.store?.identity) {
      return;
    }
    _store = widget.store ??
        serverPersonalStore(ref, int.tryParse(widget.mediaId) ?? 0);
    _generation++;
    _debounce?.cancel();
    _loaded = false;
    _note = null;
    _controller.clear();
    _savedContent = '';
    _saveFuture = null;
    _isSaving = false;
    _isClearing = false;
    _isLeaving = false;
    _didPop = false;
    _allowPop = false;
    _load();
  }

  @override
  void dispose() {
    _debounce?.cancel();
    _controller.dispose();
    super.dispose();
  }

  bool _isCurrent(int generation) =>
      mounted && !_didPop && generation == _generation;

  Future<void> _load() async {
    if (!mounted) return;
    final generation = _generation;
    setState(() {
      _isLoading = true;
      _error = null;
    });
    try {
      final note = await _store.getNote();
      if (!_isCurrent(generation)) return;
      final content = note?.content ?? '';
      _controller.text = content;
      setState(() {
        _note = note;
        _savedContent = content;
        _loaded = true;
        _isLoading = false;
      });
    } catch (e) {
      if (!_isCurrent(generation)) return;
      setState(() {
        _error = notesErrorMessage(e);
        _isLoading = false;
      });
    }
  }

  void _onTextChanged() {
    if (!_loaded || _isClearing) return;
    _debounce?.cancel();
    _debounce = Timer(const Duration(milliseconds: 800), _saveLatest);
  }

  /// All callers await the same operation. Each successful request is followed
  /// by a fresh read of the editor, so an in-flight edit cannot be dropped.
  Future<bool> _saveLatest() {
    _debounce?.cancel();
    if (_saveFuture != null) return _saveFuture!;
    if (!mounted || _isClearing) return Future.value(false);
    if (!_dirty) return Future.value(true);
    late final Future<bool> operation;
    operation = _drainSaves(_generation).whenComplete(() {
      if (identical(_saveFuture, operation)) _saveFuture = null;
    });
    _saveFuture = operation;
    return operation;
  }

  Future<bool> _drainSaves(int generation) async {
    final store = _store;
    setState(() => _isSaving = true);
    try {
      while (_isCurrent(generation) && _dirty && !_isClearing) {
        final content = _controller.text;
        final saved = await store.saveNote(content);
        if (!_isCurrent(generation)) return false;
        setState(() {
          _note = saved;
          _savedContent = content;
        });
      }
      return _isCurrent(generation) && !_dirty;
    } catch (e) {
      if (!mounted || !_isCurrent(generation)) return false;
      // Keep the latest draft. A failed request stops the drain; retry is an
      // explicit action instead of an unbounded background retry loop.
      _debounce?.cancel();
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text(notesErrorMessage(e)),
          action: SnackBarAction(label: 'Retry', onPressed: _manualSave),
        ),
      );
      return false;
    } finally {
      if (_isCurrent(generation)) setState(() => _isSaving = false);
    }
  }

  Future<void> _leave() async {
    if (_isLeaving || _isClearing) return;
    final generation = _generation;
    setState(() => _isLeaving = true);
    do {
      final saved = await _saveLatest();
      if (!_isCurrent(generation)) return;
      if (!saved) {
        setState(() => _isLeaving = false);
        return;
      }
      // Make the editor read-only before allowing navigation. Include input
      // delivered before that frame in the flush as well.
      await WidgetsBinding.instance.endOfFrame;
      if (!mounted || !_isCurrent(generation)) return;
    } while (_dirty);
    setState(() => _allowPop = true);
    await WidgetsBinding.instance.endOfFrame;
    if (!mounted || !_isCurrent(generation)) return;
    Navigator.of(context).pop();
  }

  /// Asks for confirmation before deleting the note.
  ///
  /// Shows an [AlertDialog] with Cancel / Clear actions.  On confirmation it
  /// calls [deleteNote], clears the editor, and resets local state.
  /// The dialog is shown only when a note actually exists (non-empty content)
  /// so the menu item is a no-op when the editor is already empty.
  Future<void> _clear() async {
    // Nothing to clear: the editor is already empty.
    if (_controller.text.isEmpty && _note == null) return;
    if (!mounted) return;

    final generation = _generation;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        key: const Key('notes_clear_dialog'),
        title: const Text('Clear note'),
        content: const Text(
          'This will permanently delete your note. Are you sure?',
        ),
        actions: [
          TextButton(
            key: const Key('notes_clear_cancel'),
            onPressed: () => Navigator.of(ctx).pop(false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            key: const Key('notes_clear_confirm'),
            onPressed: () => Navigator.of(ctx).pop(true),
            child: const Text('Clear'),
          ),
        ],
      ),
    );

    if (confirmed != true || !_isCurrent(generation)) return;
    _debounce?.cancel();
    setState(() => _isClearing = true);

    try {
      final store = _store;
      // A save may have started while the confirmation dialog was open.
      // Finish it first so its response cannot recreate the cleared note.
      await _saveFuture;
      if (!_isCurrent(generation)) return;
      await store.deleteNote();
      if (!_isCurrent(generation)) return;
      // Reset all local state: the note is gone.
      _controller.text = '';
      setState(() {
        _note = null;
        _savedContent = '';
        _isClearing = false;
      });
    } catch (e) {
      if (!mounted || !_isCurrent(generation)) return;
      setState(() => _isClearing = false);
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(notesErrorMessage(e))),
      );
    }
  }

  /// Manually saves the current editor content immediately (no debounce).
  ///
  /// Called from the AppBar overflow "Save" menu item so users can force a
  /// save without waiting for the debounce delay.  The debounce timer is
  /// cancelled first to avoid a double-save.
  Future<void> _manualSave() async {
    await _saveLatest();
  }

  // ---------------------------------------------------------------------------
  // Build
  // ---------------------------------------------------------------------------

  @override
  Widget build(BuildContext context) {
    return PopScope(
      // Always intercept Back until _leave has locked editing and flushed it.
      // A rendered dirty flag can lag behind a keystroke until the next frame.
      canPop: _allowPop,
      onPopInvokedWithResult: (didPop, result) {
        if (didPop) {
          _didPop = true;
          _debounce?.cancel();
        } else {
          _leave();
        }
      },
      child: Scaffold(
        appBar: _buildAppBar(),
        body: _buildBody(context),
      ),
    );
  }

  /// Builds the AppBar with title and an overflow menu for Save and Clear.
  ///
  /// [Map]-based dispatch keeps the [onSelected] handler closed for modification
  /// (Open-Closed Principle): adding a new action requires only a new enum value
  /// and one entry in [handlers], not an if/else chain to extend.
  ///
  /// Both actions are disabled while a save is in flight ([_isSaving]) to
  /// prevent concurrent API calls.
  AppBar _buildAppBar() {
    return AppBar(
      title: Text('Notes – ${widget.mediaTitle ?? widget.mediaId}'),
      actions: [
        if (_isSaving || _isClearing)
          // Unobtrusive progress indicator while a save is in flight.
          const Padding(
            padding: EdgeInsets.symmetric(horizontal: 12),
            child: SizedBox(
              key: Key('notes_saving_indicator'),
              width: 20,
              height: 20,
              child: CircularProgressIndicator(strokeWidth: 2),
            ),
          ),
        PopupMenuButton<_MenuAction>(
          key: const Key('notes_overflow_menu'),
          tooltip: 'More options',
          icon: const Icon(Icons.menu),
          style: IconButton.styleFrom(minimumSize: const Size(48, 48)),
          // Disable the menu entirely while saving to prevent concurrent calls.
          enabled: !_isSaving && !_isClearing && !_isLeaving && _loaded,
          onSelected: (action) {
            // Map-dispatch: extend by adding enum values + entries here only.
            final handlers = <_MenuAction, VoidCallback>{
              _MenuAction.save: _manualSave,
              _MenuAction.clear: _clear,
            };
            handlers[action]?.call();
          },
          itemBuilder: (_) => [
            const PopupMenuItem<_MenuAction>(
              key: Key('notes_save_menu_item'),
              value: _MenuAction.save,
              child: ListTile(
                leading: Icon(Icons.save_outlined),
                title: Text('Save'),
                contentPadding: EdgeInsets.zero,
              ),
            ),
            const PopupMenuItem<_MenuAction>(
              key: Key('notes_clear_menu_item'),
              value: _MenuAction.clear,
              child: ListTile(
                leading: Icon(Icons.delete_outline),
                title: Text('Clear'),
                contentPadding: EdgeInsets.zero,
              ),
            ),
          ],
        ),
      ],
    );
  }

  /// Delegates to the appropriate state widget based on loading/error/data.
  Widget _buildBody(BuildContext context) {
    // Full-screen spinner only on the very first load (no data yet).
    if (_isLoading) {
      return const Center(
        key: Key('notes_loading'),
        child: CircularProgressIndicator(),
      );
    }

    if (_error != null) {
      return _ErrorView(
        message: _error!,
        onRetry: _load,
      );
    }

    // The editor is shown even when no note exists yet (empty state): the user
    // can type immediately and the first auto-save will create the note.
    return _NoteEditor(
      controller: _controller,
      readOnly: _isLeaving || _isClearing,
    );
  }
}

// ---------------------------------------------------------------------------
// _MenuAction
// ---------------------------------------------------------------------------

/// Enum of available overflow-menu actions in [NotesEditorScreen].
///
/// Typed enum keeps [PopupMenuButton] type-safe and the [onSelected] map
/// dispatch closed for modification (Open-Closed Principle).
enum _MenuAction { save, clear }

// ---------------------------------------------------------------------------
// _NoteEditor
// ---------------------------------------------------------------------------

/// Full-screen multi-line text editor for the note content.
///
/// Extracted from [_NotesEditorScreenState] so the state class stays concise
/// and this widget is independently testable.  The [TextEditingController] is
/// injected so the parent retains ownership and control of the text value.
class _NoteEditor extends StatelessWidget {
  const _NoteEditor({required this.controller, required this.readOnly});

  final bool readOnly;

  /// Injected controller so the parent [_NotesEditorScreenState] can read the
  /// current text and receive change notifications.
  final TextEditingController controller;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.all(16),
      child: TextField(
        key: const Key('notes_text_field'),
        controller: controller,
        readOnly: readOnly,
        // Expand the text field to fill the available vertical space, making
        // the full screen feel like a proper editor rather than a small input.
        expands: true,
        maxLines: null,
        minLines: null,
        textAlignVertical: TextAlignVertical.top,
        decoration: const InputDecoration(
          hintText: 'Write your notes here…',
          border: InputBorder.none,
          // Disable all visual borders — the full-screen layout is the
          // container; extra decoration would be distracting.
          enabledBorder: InputBorder.none,
          focusedBorder: InputBorder.none,
          contentPadding: EdgeInsets.zero,
        ),
        style: Theme.of(context).textTheme.bodyLarge,
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// _ErrorView
// ---------------------------------------------------------------------------

/// Full-screen error view with a retry button.
///
/// Shown when [getNote] throws on initial load.  [message] comes from
/// [notesErrorMessage]; [onRetry] triggers a fresh [_load] call.
class _ErrorView extends StatelessWidget {
  const _ErrorView({required this.message, required this.onRetry});

  final String message;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Icon(
              Icons.error_outline,
              size: 56,
              color: Theme.of(context).colorScheme.error,
            ),
            const SizedBox(height: 16),
            Text(
              message,
              key: const Key('notes_error'),
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.bodyLarge,
            ),
            const SizedBox(height: 24),
            ElevatedButton.icon(
              key: const Key('notes_retry'),
              onPressed: onRetry,
              icon: const Icon(Icons.refresh),
              label: const Text('Retry'),
            ),
          ],
        ),
      ),
    );
  }
}
