import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/models.dart';
import '../providers/api_client_provider.dart';
import '../utils/error_mappers.dart';

/// Admin-only permission matrix screen.
///
/// Design notes:
///   - Rows = users, columns = sets; each cell is a checkbox indicating whether
///     that user has access to that set.
///   - Admin users always have implicit access to all sets (server-enforced);
///     their rows are rendered as disabled/grayed to make this constraint
///     visible in the UI without suggesting they can be edited.
///   - Grant/revoke use optimistic UI: the checkbox is toggled locally first,
///     then the API call is made.  On error the toggle is reverted and a
///     SnackBar reports the problem.
///   - A generation counter prevents stale async load results from overwriting
///     a newer refresh that was started while the previous one was in flight.
///   - All async continuations guard on [mounted] to prevent setState/context
///     calls after widget disposal.
class AdminPermissionsScreen extends ConsumerStatefulWidget {
  const AdminPermissionsScreen({super.key});

  @override
  ConsumerState<AdminPermissionsScreen> createState() =>
      _AdminPermissionsScreenState();
}

class _AdminPermissionsScreenState
    extends ConsumerState<AdminPermissionsScreen> {
  // Null while the initial load is in-flight.
  List<User>? _users;
  List<MediaSet>? _sets;

  // Tracks which (userId, setId) pairs have an explicit permission row.
  // Using a Set<_PermKey> keeps lookup O(1) and avoids scanning a flat list
  // on every checkbox render (important for larger permission matrices).
  final Set<_PermKey> _granted = {};

  // Non-null when the last load attempt failed.
  String? _error;

  // True while the initial or refresh load is in flight.
  bool _isLoading = false;

  // Incremented on every load; async completions discard results if the
  // generation they captured no longer matches (stale-async-cancellation).
  int _generation = 0;

  @override
  void initState() {
    super.initState();
    // Defer until after the first frame so provider overrides in tests apply.
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
  }

  // ---------------------------------------------------------------------------
  // Data loading
  // ---------------------------------------------------------------------------

  /// Loads users, sets, and the permission matrix in parallel.
  ///
  /// Running the three requests concurrently keeps the UI snappy even on
  /// connections with higher latency, because none of them depend on each other.
  Future<void> _load() async {
    if (!mounted) return;
    final generation = ++_generation;

    setState(() {
      _isLoading = true;
      _error = null;
    });

    try {
      final api = ref.read(apiClientProvider);
      // Fetch users, sets, and permissions concurrently to minimise wait time.
      final results = await Future.wait([
        api.listUsers(),
        api.listSets(),
        api.listPermissions(),
      ]);

      if (!mounted || generation != _generation) return;

      // Use .cast<T>() for list results so a type mismatch produces a useful
      // error at element access rather than silently failing on a direct cast.
      // The Map result is kept as-is with a cast because there is no cast()
      // method on Map in the Dart core library.
      final users = (results[0] as List).cast<User>();
      final sets = (results[1] as List).cast<MediaSet>();
      final permsData = results[2] as Map<String, dynamic>;

      setState(() {
        _users = users;
        _sets = sets;
        _granted
          ..clear()
          ..addAll(_parsePermissions(permsData));
        _isLoading = false;
      });
    } catch (e) {
      if (!mounted || generation != _generation) return;
      setState(() {
        _error = adminPermissionErrorMessage(e);
        _isLoading = false;
      });
    }
  }

  /// Parses the raw permission map returned by [listPermissions] into a flat
  /// set of (userId, setId) pairs.
  ///
  /// The API response has the shape:
  /// `{"permissions": [{"user_id": 1, "set_id": 2, "role": "viewer"}, ...]}`
  /// We only care about the existence of a row (not the role) for the checkbox
  /// state, so we collapse the list to a [Set<_PermKey>].
  Set<_PermKey> _parsePermissions(Map<String, dynamic> data) {
    final result = <_PermKey>{};
    final rawList = data['permissions'];
    if (rawList is! List) return result;
    for (final item in rawList) {
      if (item is! Map<String, dynamic>) continue;
      final userId = item['user_id'] as int?;
      final setId = item['set_id'] as int?;
      if (userId != null && setId != null) {
        result.add(_PermKey(userId: userId, setId: setId));
      }
    }
    return result;
  }

  // ---------------------------------------------------------------------------
  // Grant / revoke actions (optimistic UI)
  // ---------------------------------------------------------------------------

  /// Toggles the permission for [userId] on [setId].
  ///
  /// Applies the change locally first so the UI feels instant, then calls the
  /// server.  On error, the local change is reverted and a SnackBar is shown.
  Future<void> _toggle(int userId, int setId, bool newValue) async {
    final key = _PermKey(userId: userId, setId: setId);

    // Optimistic update: reflect the desired state immediately.
    setState(() {
      if (newValue) {
        _granted.add(key);
      } else {
        _granted.remove(key);
      }
    });

    try {
      final api = ref.read(apiClientProvider);
      if (newValue) {
        // Grant with the default 'viewer' role; promotion to 'owner' is out
        // of scope for the permission matrix (could be a future enhancement).
        await api.grantPermission(
          userId: userId,
          setId: setId,
          role: 'viewer',
        );
      } else {
        await api.revokePermission(userId: userId, setId: setId);
      }
    } catch (e) {
      if (!mounted) return;
      // Revert the optimistic change so the UI reflects the true server state.
      setState(() {
        if (newValue) {
          _granted.remove(key);
        } else {
          _granted.add(key);
        }
      });
      _showError(adminPermissionErrorMessage(e));
    }
  }

  // ---------------------------------------------------------------------------
  // Error display
  // ---------------------------------------------------------------------------

  void _showError(String message) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        key: const Key('admin_perms_error_snackbar'),
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
        title: const Text('Permissions'),
        actions: [
          IconButton(
            key: const Key('admin_perms_refresh'),
            icon: const Icon(Icons.refresh),
            tooltip: 'Refresh',
            onPressed: _load,
          ),
        ],
      ),
      body: _buildBody(context),
    );
  }

  /// Selects the appropriate body widget for the current state.
  Widget _buildBody(BuildContext context) {
    if (_isLoading && _users == null) {
      return const Center(
        key: Key('admin_perms_loading'),
        child: CircularProgressIndicator(),
      );
    }

    if (_error != null) {
      return _ErrorView(message: _error!, onRetry: _load);
    }

    final users = _users;
    final sets = _sets;
    if (users == null || sets == null || users.isEmpty || sets.isEmpty) {
      return const _EmptyView();
    }

    return RefreshIndicator(
      onRefresh: _load,
      child: _PermissionMatrix(
        users: users,
        sets: sets,
        granted: _granted,
        onToggle: _toggle,
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// Value object for a (userId, setId) permission key
// ---------------------------------------------------------------------------

/// Immutable key that uniquely identifies a permission row.
///
/// Used as a Set element so lookup is O(1) per checkbox render.
/// Equality is structural (both fields must match), matching the server's
/// composite primary key on the permissions table.
class _PermKey {
  const _PermKey({required this.userId, required this.setId});

  final int userId;
  final int setId;

  @override
  bool operator ==(Object other) =>
      other is _PermKey && other.userId == userId && other.setId == setId;

  @override
  int get hashCode => Object.hash(userId, setId);
}

// ---------------------------------------------------------------------------
// Permission matrix widget
// ---------------------------------------------------------------------------

/// Scrollable permission matrix: rows = users, columns = sets.
///
/// Extracted as a stateless widget (SRP) so the state class focuses on
/// data-loading and mutation, while this widget handles pure rendering.
class _PermissionMatrix extends StatelessWidget {
  const _PermissionMatrix({
    required this.users,
    required this.sets,
    required this.granted,
    required this.onToggle,
  });

  final List<User> users;
  final List<MediaSet> sets;

  /// Current permission snapshot; a key present here means the cell is checked.
  final Set<_PermKey> granted;

  /// Called when the user taps a checkbox.  [newValue] is the desired new state.
  final Future<void> Function(int userId, int setId, bool newValue) onToggle;

  @override
  Widget build(BuildContext context) {
    // Horizontal scroll wraps the full table so narrow screens can still see
    // all set columns without clipping.
    return SingleChildScrollView(
      scrollDirection: Axis.vertical,
      child: SingleChildScrollView(
        scrollDirection: Axis.horizontal,
        child: _buildTable(context),
      ),
    );
  }

  /// Builds the DataTable with a header row of set names and user data rows.
  Widget _buildTable(BuildContext context) {
    return DataTable(
      key: const Key('admin_perms_table'),
      // Each set gets one column; the user column is always first.
      columns: [
        const DataColumn(label: Text('User')),
        ...sets.map(
          (s) => DataColumn(label: Text(s.name, overflow: TextOverflow.ellipsis)),
        ),
      ],
      rows: users.map((user) => _buildRow(context, user)).toList(),
    );
  }

  /// Builds a single user row with one checkbox cell per set.
  ///
  /// Admin users have implicit access to all sets (enforced server-side), so
  /// their checkboxes are shown as disabled to make this constraint obvious
  /// without implying they can be changed.
  DataRow _buildRow(BuildContext context, User user) {
    return DataRow(
      // Visually dim admin rows to signal that their access cannot be edited.
      color: user.isAdmin
          ? WidgetStateProperty.all(
              Theme.of(context).colorScheme.surfaceContainerHighest,
            )
          : null,
      cells: [
        // First cell: username + optional "admin" badge.
        DataCell(_UserCell(user: user)),
        // One cell per set column.
        ...sets.map((s) => _buildPermCell(user, s)),
      ],
    );
  }

  /// Builds a single checkbox cell for the (user, set) intersection.
  DataCell _buildPermCell(User user, MediaSet set) {
    final key = _PermKey(userId: user.id, setId: set.id);
    // Admin users always have access; their cells are checked but non-interactive
    // to reflect the implicit access the server grants them.
    final isAdmin = user.isAdmin;
    final isChecked = isAdmin || granted.contains(key);

    return DataCell(
      Checkbox(
        key: Key('perm_${user.id}_${set.id}'),
        value: isChecked,
        // Disable interaction for admin users; their access is server-managed.
        onChanged: isAdmin
            ? null
            : (value) => onToggle(user.id, set.id, value ?? false),
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// User cell widget
// ---------------------------------------------------------------------------

/// Displays a user's name and an "Admin" badge when applicable.
///
/// Extracted to keep [_PermissionMatrix._buildRow] readable (SRP).
class _UserCell extends StatelessWidget {
  const _UserCell({required this.user});

  final User user;

  @override
  Widget build(BuildContext context) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Text(user.username),
        if (user.isAdmin) ...[
          const SizedBox(width: 6),
          Chip(
            label: Text(
              'Admin',
              style: TextStyle(
                fontSize: 11,
                color: Theme.of(context).colorScheme.onPrimaryContainer,
              ),
            ),
            backgroundColor:
                Theme.of(context).colorScheme.primaryContainer,
            padding: EdgeInsets.zero,
            visualDensity: VisualDensity.compact,
          ),
        ],
      ],
    );
  }
}

// ---------------------------------------------------------------------------
// Shared sub-widgets (empty state, error state)
// ---------------------------------------------------------------------------

/// Full-screen empty-state shown when there are no users or no sets.
class _EmptyView extends StatelessWidget {
  const _EmptyView();

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(
            Icons.lock_outline,
            size: 72,
            color: Theme.of(context).colorScheme.onSurfaceVariant,
          ),
          const SizedBox(height: 16),
          Text(
            'No users or sets found',
            key: const Key('admin_perms_empty'),
            style: Theme.of(context).textTheme.titleMedium,
          ),
        ],
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
              key: const Key('admin_perms_error'),
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.bodyLarge,
            ),
            const SizedBox(height: 24),
            ElevatedButton.icon(
              key: const Key('admin_perms_retry'),
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
