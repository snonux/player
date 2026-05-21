import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/models.dart';
import '../providers/api_client_provider.dart';
import '../utils/error_mappers.dart';

// ---------------------------------------------------------------------------
// NotesEditorScreen
// ---------------------------------------------------------------------------

/// Full-screen text editor for a user's personal note attached to a media item.
///
/// Design notes:
///   - [ConsumerStatefulWidget] is used so we can manage local state for the
///     text controller, debounce timer, and loading/saving/error flags, and
///     guard async continuations with [mounted].
///   - The note is loaded via [getNote] on first mount (deferred to
///     post-frame so [ref] is fully bound in tests).
///   - Auto-save fires 800 ms after the last keystroke via a [Timer] that is
///     cancelled and restarted on every text change (debounce pattern).  The
///     timer is also cancelled in [dispose] to prevent callbacks from running
///     on a disposed widget.
///   - Manual save and clear/delete buttons live in the AppBar overflow menu,
///     using the same [Map]-dispatch / [_MenuAction] enum pattern as
///     [MediaDetailScreen] (Open-Closed Principle — adding a new action only
///     requires a new enum value and one map entry, not an if/else chain).
///   - No `dio` import — error mapping is delegated to [notesErrorMessage] in
///     error_mappers.dart (Dependency Inversion Principle).
class NotesEditorScreen extends ConsumerStatefulWidget {
  /// The string form of the media ID extracted from the '/notes/:mediaId' route.
  final String mediaId;

  const NotesEditorScreen({super.key, required this.mediaId});

  @override
  ConsumerState<NotesEditorScreen> createState() => _NotesEditorScreenState();
}

class _NotesEditorScreenState extends ConsumerState<NotesEditorScreen> {
  // Controller for the multi-line text field; initialised empty and populated
  // once [getNote] resolves.
  late final TextEditingController _controller;

  // Nullable: null means "not yet loaded" (spinner shown in place of editor).
  Note? _note;

  // Non-null when the last load or save attempt failed.
  String? _error;

  // True while the initial [getNote] call is in flight.
  bool _isLoading = false;

  // True while a [upsertNote] or [deleteNote] call is in flight.
  // Disables the AppBar action buttons to prevent concurrent calls.
  bool _isSaving = false;

  // Debounce timer: restarted on every text change; fires auto-save after
  // 800 ms of inactivity.  Cancelled in [dispose] to prevent callbacks from
  // running on a disposed widget.
  Timer? _debounce;

  // Tracks the last content successfully saved to the server so we can skip
  // unnecessary upsert calls when the user pauses without actually changing
  // the text.
  String _savedContent = '';

  // Auto-save debounce delay in milliseconds.
  static const _kDebounceMs = 800;

  @override
  void initState() {
    super.initState();
    _controller = TextEditingController();
    // Defer the first load until after the first frame so [ref] is fully bound
    // and any provider overrides in the test environment are applied.
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
  }

  @override
  void dispose() {
    // Cancel any pending debounce timer before the widget is unmounted so the
    // auto-save callback never fires on a disposed widget (mounted guard would
    // also catch it, but explicit cancellation is clearer and avoids the call).
    _debounce?.cancel();
    _controller.dispose();
    super.dispose();
  }

  // ---------------------------------------------------------------------------
  // Data loading
  // ---------------------------------------------------------------------------

  /// Fetches the existing note for this media item and populates the editor.
  ///
  /// Called on first mount.  A null response (204 No Content) means no note
  /// exists yet — the editor starts empty and the first save creates one.
  /// Errors are mapped by the top-level [notesErrorMessage] helper so no
  /// `dio` import is needed.
  Future<void> _load() async {
    if (!mounted) return;
    setState(() {
      _isLoading = true;
      _error = null;
    });

    try {
      final id = int.tryParse(widget.mediaId) ?? 0;
      final client = ref.read(apiClientProvider);
      final note = await client.getNote(id);
      if (!mounted) return;
      final content = note?.content ?? '';
      _controller.text = content;
      // Initialise _savedContent so the first auto-save does not trigger an
      // unnecessary upsert when the user taps into the editor without typing.
      setState(() {
        _note = note;
        _savedContent = content;
        _isLoading = false;
      });
      // Listen for text changes *after* the initial content is set so the
      // listener does not fire a spurious auto-save on the seed value.
      _controller.addListener(_onTextChanged);
    } catch (e) {
      if (!mounted) return;
      setState(() {
        _error = notesErrorMessage(e);
        _isLoading = false;
      });
    }
  }

  // ---------------------------------------------------------------------------
  // Auto-save (debounce)
  // ---------------------------------------------------------------------------

  /// Called on every text-field change; restarts the 800 ms debounce timer.
  ///
  /// Cancelling the previous timer before starting a new one ensures only one
  /// save fires per burst of keystrokes, not one per keystroke.
  void _onTextChanged() {
    _debounce?.cancel();
    _debounce = Timer(
      const Duration(milliseconds: _kDebounceMs),
      _autoSave,
    );
  }

  /// Fires after the debounce delay; saves only when content has actually changed.
  ///
  /// Skips the call if the text matches what was last saved to avoid hammering
  /// the server when the user pauses without typing (idempotent guard).
  Future<void> _autoSave() async {
    final content = _controller.text;
    // Skip save if nothing has changed since the last successful save.
    if (content == _savedContent) return;
    await _save(content);
  }

  // ---------------------------------------------------------------------------
  // Save / clear
  // ---------------------------------------------------------------------------

  /// Persists [content] via [upsertNote] and updates local state on success.
  ///
  /// [_isSaving] is set for the duration so the AppBar buttons are disabled.
  /// Errors are shown as a [SnackBar] rather than replacing the editor (the user
  /// should be able to keep editing even when a save temporarily fails).
  Future<void> _save(String content) async {
    if (_isSaving || !mounted) return;
    setState(() => _isSaving = true);

    try {
      final id = int.tryParse(widget.mediaId) ?? 0;
      final client = ref.read(apiClientProvider);
      final saved = await client.upsertNote(id, content);
      if (!mounted) return;
      setState(() {
        _note = saved;
        _savedContent = saved.content;
        _isSaving = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() => _isSaving = false);
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(notesErrorMessage(e))),
      );
    }
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

    if (confirmed != true || !mounted) return;

    // Cancel any pending debounce so the auto-save doesn't fire after delete.
    _debounce?.cancel();

    setState(() => _isSaving = true);

    try {
      final id = int.tryParse(widget.mediaId) ?? 0;
      final client = ref.read(apiClientProvider);
      await client.deleteNote(id);
      if (!mounted) return;
      // Reset all local state: the note is gone.
      _controller.text = '';
      setState(() {
        _note = null;
        _savedContent = '';
        _isSaving = false;
      });
    } catch (e) {
      if (!mounted) return;
      setState(() => _isSaving = false);
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
    _debounce?.cancel();
    await _save(_controller.text);
  }

  // ---------------------------------------------------------------------------
  // Build
  // ---------------------------------------------------------------------------

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: _buildAppBar(),
      body: _buildBody(context),
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
      title: Text('Notes – ${widget.mediaId}'),
      actions: [
        if (_isSaving)
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
          // Disable the menu entirely while saving to prevent concurrent calls.
          enabled: !_isSaving && !_isLoading,
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
    return _NoteEditor(controller: _controller);
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
  const _NoteEditor({required this.controller});

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
