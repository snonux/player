import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/models.dart';
import '../providers/api_client_provider.dart';
import '../utils/error_mappers.dart';

/// Admin-only screen that lists soft-deleted media items (the trash).
///
/// Design notes:
///   - Items are loaded from GET /api/v1/admin/trash via [listTrash].
///   - Restore action calls [restoreMedia]; optimistic UI removes the item
///     from the trash list immediately and reverts on error.
///   - Hard-delete shows a confirmation dialog before calling [deleteMedia]
///     (permanent purge from the perspective of this UI, even though the
///     underlying API still performs a soft-delete — the server's GC worker
///     completes the physical removal).
///   - A generation counter prevents stale loads from overwriting a newer
///     refresh that started while the previous was still in flight.
///   - All async continuations guard on [mounted] to prevent setState/context
///     calls after widget disposal.
class AdminTrashScreen extends ConsumerStatefulWidget {
  const AdminTrashScreen({super.key});

  @override
  ConsumerState<AdminTrashScreen> createState() => _AdminTrashScreenState();
}

class _AdminTrashScreenState extends ConsumerState<AdminTrashScreen> {
  // Null while the initial load is in flight.
  List<Media>? _items;

  // Non-null when the last load attempt failed.
  String? _error;

  // True while a load is in flight (initial or refresh).
  bool _isLoading = false;

  // Generation counter: incremented on every load call so stale completions
  // from a previous request are silently discarded when they arrive.
  int _generation = 0;

  @override
  void initState() {
    super.initState();
    // Defer until after first frame so provider overrides in tests are applied.
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
  }

  // ---------------------------------------------------------------------------
  // Data loading
  // ---------------------------------------------------------------------------

  /// Fetches the current trash list and updates local state.
  Future<void> _load() async {
    if (!mounted) return;
    final generation = ++_generation;

    setState(() {
      _isLoading = true;
      _error = null;
    });

    try {
      final items = await ref.read(apiClientProvider).listTrash();
      if (!mounted || generation != _generation) return;
      setState(() {
        _items = items;
        _isLoading = false;
      });
    } catch (e) {
      if (!mounted || generation != _generation) return;
      setState(() {
        _error = adminTrashErrorMessage(e);
        _isLoading = false;
      });
    }
  }

  // ---------------------------------------------------------------------------
  // Restore action (optimistic UI)
  // ---------------------------------------------------------------------------

  /// Restores [item] from trash and removes it from the local list optimistically.
  ///
  /// On error the item is re-appended to the list and a SnackBar reports the
  /// problem.  Re-appending (rather than re-inserting at the original index)
  /// avoids position jitter from concurrent mutations.
  Future<void> _restore(Media item) async {
    // Identity-based removal (by id) avoids position drift from concurrent
    // operations that could shift list indices between tap and setState.
    setState(() => _items!.removeWhere((e) => e.id == item.id));

    try {
      await ref.read(apiClientProvider).restoreMedia(item.id);
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          key: const Key('admin_trash_restore_snackbar'),
          content: Text('"${item.fileName}" restored.'),
          duration: const Duration(seconds: 3),
        ),
      );
    } catch (e) {
      if (!mounted) return;
      // Revert: re-append the item so it remains visible even after the error.
      setState(() => _items = [..._items!, item]);
      _showError(adminTrashErrorMessage(e));
    }
  }

  // ---------------------------------------------------------------------------
  // Hard delete action (confirmation + optimistic UI)
  // ---------------------------------------------------------------------------

  /// Shows a confirmation dialog, then hard-deletes [item] if confirmed.
  ///
  /// "Hard delete" from the UI's perspective means the item is flagged for
  /// permanent removal.  The server's GC worker completes the physical file
  /// removal.  The item is removed from the local list optimistically and
  /// reverted on error.
  Future<void> _hardDelete(Media item) async {
    final confirmed = await _confirmHardDelete(item.fileName);
    if (!confirmed || !mounted) return;

    // Identity-based removal (by id) avoids position drift from concurrent
    // operations that could shift list indices between tap and setState.
    setState(() => _items!.removeWhere((e) => e.id == item.id));

    try {
      // deleteMedia soft-deletes again (no-op for an already-deleted item)
      // so the item stays flagged for GC rather than being restored.
      await ref.read(apiClientProvider).deleteMedia(item.id);
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          key: const Key('admin_trash_delete_snackbar'),
          content: Text('"${item.fileName}" marked for permanent deletion.'),
          duration: const Duration(seconds: 3),
        ),
      );
    } catch (e) {
      if (!mounted) return;
      // Revert: re-append the item so it remains visible after the error.
      setState(() => _items = [..._items!, item]);
      _showError(adminTrashErrorMessage(e));
    }
  }

  /// Shows a confirmation [AlertDialog] before permanent deletion.
  ///
  /// Returns true only when the user taps the "Delete" button.
  Future<bool> _confirmHardDelete(String fileName) async {
    final result = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Permanently delete?'),
        content: Text(
          '"$fileName" will be flagged for permanent removal and cannot be '
          'restored. Are you sure?',
        ),
        actions: [
          TextButton(
            key: const Key('admin_trash_confirm_cancel'),
            onPressed: () => Navigator.of(ctx).pop(false),
            child: const Text('Cancel'),
          ),
          TextButton(
            key: const Key('admin_trash_confirm_delete'),
            style: TextButton.styleFrom(
              foregroundColor: Theme.of(ctx).colorScheme.error,
            ),
            onPressed: () => Navigator.of(ctx).pop(true),
            child: const Text('Delete permanently'),
          ),
        ],
      ),
    );
    // Tapping outside the dialog returns null; treat as cancel (no deletion).
    return result ?? false;
  }

  // ---------------------------------------------------------------------------
  // Error display
  // ---------------------------------------------------------------------------

  void _showError(String message) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        key: const Key('admin_trash_error_snackbar'),
        content: Text(message),
        backgroundColor: Theme.of(context).colorScheme.error,
      ),
    );
  }

  // ---------------------------------------------------------------------------
  // Build
  // ---------------------------------------------------------------------------

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Trash'),
        actions: [
          IconButton(
            key: const Key('admin_trash_refresh'),
            icon: const Icon(Icons.refresh),
            tooltip: 'Refresh',
            onPressed: _load,
          ),
        ],
      ),
      body: _buildBody(context),
    );
  }

  /// Builds the appropriate body widget for the current state.
  Widget _buildBody(BuildContext context) {
    // Show a full-screen spinner while the very first load is in flight.
    if (_isLoading && _items == null) {
      return const Center(
        key: Key('admin_trash_loading'),
        child: CircularProgressIndicator(),
      );
    }

    if (_error != null) {
      return _ErrorView(message: _error!, onRetry: _load);
    }

    return RefreshIndicator(
      onRefresh: _load,
      child: _items == null || _items!.isEmpty
          ? const _EmptyView()
          : _TrashList(
              items: _items!,
              onRestore: _restore,
              onHardDelete: _hardDelete,
            ),
    );
  }
}

// ---------------------------------------------------------------------------
// Sub-widgets
// ---------------------------------------------------------------------------

/// Scrollable list of trashed [Media] items.
///
/// Extracted as a stateless widget (SRP) so [_AdminTrashScreenState] focuses
/// on data-loading and mutation concerns.
class _TrashList extends StatelessWidget {
  const _TrashList({
    required this.items,
    required this.onRestore,
    required this.onHardDelete,
  });

  final List<Media> items;
  final Future<void> Function(Media item) onRestore;
  final Future<void> Function(Media item) onHardDelete;

  @override
  Widget build(BuildContext context) {
    return ListView.separated(
      key: const Key('admin_trash_list'),
      itemCount: items.length,
      separatorBuilder: (_, __) => const Divider(height: 1),
      itemBuilder: (_, index) => _TrashTile(
        item: items[index],
        onRestore: onRestore,
        onHardDelete: onHardDelete,
      ),
    );
  }
}

/// A single trash item row with restore and hard-delete actions.
class _TrashTile extends StatelessWidget {
  const _TrashTile({
    required this.item,
    required this.onRestore,
    required this.onHardDelete,
  });

  final Media item;
  final Future<void> Function(Media item) onRestore;
  final Future<void> Function(Media item) onHardDelete;

  @override
  Widget build(BuildContext context) {
    return ListTile(
      key: Key('admin_trash_tile_${item.id}'),
      leading: _TypeIcon(type: item.type),
      title: Text(item.fileName, overflow: TextOverflow.ellipsis),
      subtitle: Text(
        item.absPath,
        overflow: TextOverflow.ellipsis,
        maxLines: 1,
        style: Theme.of(context).textTheme.bodySmall,
      ),
      // Restore and hard-delete actions side by side in the trailing slot.
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          IconButton(
            key: Key('admin_trash_restore_${item.id}'),
            icon: const Icon(Icons.restore_outlined),
            tooltip: 'Restore',
            onPressed: () => onRestore(item),
          ),
          IconButton(
            key: Key('admin_trash_delete_${item.id}'),
            icon: const Icon(Icons.delete_forever_outlined),
            tooltip: 'Delete permanently',
            color: Theme.of(context).colorScheme.error,
            onPressed: () => onHardDelete(item),
          ),
        ],
      ),
    );
  }
}

/// Small icon that visually distinguishes video items from audio items.
class _TypeIcon extends StatelessWidget {
  const _TypeIcon({required this.type});

  final String type;

  @override
  Widget build(BuildContext context) {
    final isAudio = type == 'audio';
    return Icon(
      isAudio ? Icons.audio_file_outlined : Icons.video_file_outlined,
      color: Theme.of(context).colorScheme.onSurfaceVariant,
    );
  }
}

/// Full-screen empty-state shown when trash is empty.
///
/// Wrapped in a scrollable so the parent [RefreshIndicator] can trigger
/// pull-to-refresh even when no content is present.
class _EmptyView extends StatelessWidget {
  const _EmptyView();

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) => SingleChildScrollView(
        physics: const AlwaysScrollableScrollPhysics(),
        child: SizedBox(
          height: constraints.maxHeight,
          child: Center(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Icon(
                  Icons.delete_outline,
                  size: 72,
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
                const SizedBox(height: 16),
                Text(
                  'Trash is empty',
                  key: const Key('admin_trash_empty'),
                  style: Theme.of(context).textTheme.titleMedium,
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// Full-screen error view with a retry button.
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
              key: const Key('admin_trash_error'),
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.bodyLarge,
            ),
            const SizedBox(height: 24),
            ElevatedButton.icon(
              key: const Key('admin_trash_retry'),
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
