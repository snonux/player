import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/models.dart';
import '../providers/api_client_provider.dart';
import '../utils/error_mappers.dart';

/// Admin-only permission editor.
///
/// Design notes:
///   - Primary flow: pick a user, search sets, set viewer/owner/none per set.
///     User and set labels stay on screen — no nested scrolling matrix.
///   - Advanced matrix (overflow menu) keeps the full users×sets grid for bulk
///     review; role dropdowns replace checkboxes so owner/viewer both work.
///   - Admin users always have implicit access to all sets (server-enforced);
///     their controls are disabled/grayed.
///   - Grant/revoke/role change use optimistic UI; errors revert and SnackBar.
///   - A generation counter drops stale async load results.
///   - All async continuations guard on [mounted].
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

  // Explicit permission rows: key → role ("viewer" | "owner").
  final Map<_PermKey, String> _roles = {};

  // Non-null when the last load attempt failed.
  String? _error;

  // True while the initial or refresh load is in flight.
  bool _isLoading = false;

  // Incremented on every load; async completions discard stale results.
  int _generation = 0;

  // Primary editor selection and set filter.
  int? _selectedUserId;
  String _setQuery = '';
  // Owned here so leaving for the Advanced matrix and returning keeps the
  // search text visible (a remounted TextField without a controller is empty).
  final TextEditingController _setQueryController = TextEditingController();

  // When true, show the advanced users×sets matrix instead of the editor.
  bool _showMatrix = false;

  @override
  void initState() {
    super.initState();
    // Defer until after the first frame so provider overrides in tests apply.
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
  }

  @override
  void dispose() {
    _setQueryController.dispose();
    super.dispose();
  }

  void _onSetQueryChanged(String query) {
    if (query == _setQuery) return;
    setState(() => _setQuery = query);
  }

  // ---------------------------------------------------------------------------
  // Data loading
  // ---------------------------------------------------------------------------

  /// Loads users, sets, and the permission matrix in parallel.
  Future<void> _load() async {
    if (!mounted) return;
    final generation = ++_generation;

    setState(() {
      _isLoading = true;
      _error = null;
    });

    try {
      final api = ref.read(apiClientProvider);
      final results = await Future.wait([
        api.listUsers(),
        api.listSets(),
        api.listPermissions(),
      ]);

      if (!mounted || generation != _generation) return;

      final users = (results[0] as List).cast<User>();
      final sets = (results[1] as List).cast<MediaSet>();
      final permsData = results[2] as Map<String, dynamic>;

      setState(() {
        _users = users;
        _sets = sets;
        _roles
          ..clear()
          ..addAll(_parsePermissions(permsData));
        _selectedUserId = _resolveSelectedUserId(users, _selectedUserId);
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

  /// Keeps the current selection when still present; otherwise prefers the
  /// first non-admin user, then the first user.
  int? _resolveSelectedUserId(List<User> users, int? current) {
    if (users.isEmpty) return null;
    if (current != null && users.any((u) => u.id == current)) return current;
    for (final user in users) {
      if (!user.isAdmin) return user.id;
    }
    return users.first.id;
  }

  /// Parses permission rows into a map of (userId, setId) → role.
  Map<_PermKey, String> _parsePermissions(Map<String, dynamic> data) {
    final result = <_PermKey, String>{};
    final rawList = data['permissions'];
    if (rawList is! List) return result;
    for (final item in rawList) {
      if (item is! Map<String, dynamic>) continue;
      final userId = item['user_id'] as int?;
      final setId = item['set_id'] as int?;
      final role = item['role'] as String?;
      if (userId != null &&
          setId != null &&
          (role == 'viewer' || role == 'owner')) {
        result[_PermKey(userId: userId, setId: setId)] = role!;
      }
    }
    return result;
  }

  // ---------------------------------------------------------------------------
  // Grant / revoke / role change (optimistic UI)
  // ---------------------------------------------------------------------------

  /// Sets [role] for [userId] on [setId], or revokes when [role] is empty.
  Future<void> _setRole(int userId, int setId, String role) async {
    final key = _PermKey(userId: userId, setId: setId);
    final previous = _roles[key];

    setState(() {
      if (role.isEmpty) {
        _roles.remove(key);
      } else {
        _roles[key] = role;
      }
    });

    try {
      final api = ref.read(apiClientProvider);
      if (role.isEmpty) {
        await api.revokePermission(userId: userId, setId: setId);
      } else {
        await api.grantPermission(
          userId: userId,
          setId: setId,
          role: role,
        );
      }
    } catch (e) {
      if (!mounted) return;
      setState(() {
        if (previous == null) {
          _roles.remove(key);
        } else {
          _roles[key] = previous;
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
        title: Text(_showMatrix ? 'Permissions matrix' : 'Permissions'),
        actions: [
          if (_showMatrix)
            IconButton(
              key: const Key('admin_perms_exit_matrix'),
              icon: const Icon(Icons.person_search),
              tooltip: 'User editor',
              onPressed: () => setState(() => _showMatrix = false),
            ),
          IconButton(
            key: const Key('admin_perms_refresh'),
            icon: const Icon(Icons.refresh),
            tooltip: 'Refresh',
            onPressed: _load,
          ),
          PopupMenuButton<String>(
            key: const Key('admin_perms_menu'),
            tooltip: 'More',
            onSelected: (value) {
              if (value == 'matrix') {
                setState(() => _showMatrix = true);
              } else if (value == 'editor') {
                setState(() => _showMatrix = false);
              }
            },
            itemBuilder: (_) => [
              if (!_showMatrix)
                const PopupMenuItem(
                  value: 'matrix',
                  child: Text('Advanced matrix'),
                ),
              if (_showMatrix)
                const PopupMenuItem(
                  value: 'editor',
                  child: Text('User editor'),
                ),
            ],
          ),
        ],
      ),
      body: _buildBody(context),
    );
  }

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

    if (_showMatrix) {
      return RefreshIndicator(
        onRefresh: _load,
        child: _PermissionMatrix(
          users: users,
          sets: sets,
          roles: _roles,
          onSetRole: _setRole,
        ),
      );
    }

    User? selected;
    for (final user in users) {
      if (user.id == _selectedUserId) {
        selected = user;
        break;
      }
    }

    return RefreshIndicator(
      onRefresh: _load,
      child: _UserPermissionEditor(
        users: users,
        sets: sets,
        roles: _roles,
        selectedUserId: _selectedUserId,
        setQuery: _setQuery,
        setQueryController: _setQueryController,
        selectedUser: selected,
        onSelectUser: (id) => setState(() => _selectedUserId = id),
        onQueryChanged: _onSetQueryChanged,
        onSetRole: _setRole,
      ),
    );
  }
}

// ---------------------------------------------------------------------------
// Value object for a (userId, setId) permission key
// ---------------------------------------------------------------------------

/// Immutable key that uniquely identifies a permission row.
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
// Primary editor: user picker + searchable set roles
// ---------------------------------------------------------------------------

class _UserPermissionEditor extends StatelessWidget {
  const _UserPermissionEditor({
    required this.users,
    required this.sets,
    required this.roles,
    required this.selectedUserId,
    required this.setQuery,
    required this.setQueryController,
    required this.selectedUser,
    required this.onSelectUser,
    required this.onQueryChanged,
    required this.onSetRole,
  });

  final List<User> users;
  final List<MediaSet> sets;
  final Map<_PermKey, String> roles;
  final int? selectedUserId;
  final String setQuery;
  final TextEditingController setQueryController;
  final User? selectedUser;
  final ValueChanged<int?> onSelectUser;
  final ValueChanged<String> onQueryChanged;
  final Future<void> Function(int userId, int setId, String role) onSetRole;

  static String userLabel(User user) =>
      user.isAdmin ? '${user.username} (admin)' : user.username;

  List<MediaSet> get _filteredSets {
    final q = setQuery.trim().toLowerCase();
    if (q.isEmpty) return sets;
    return sets.where((s) => s.name.toLowerCase().contains(q)).toList();
  }

  @override
  Widget build(BuildContext context) {
    final filtered = _filteredSets;
    final isAdmin = selectedUser?.isAdmin ?? false;

    return ListView(
      physics: const AlwaysScrollableScrollPhysics(),
      padding: const EdgeInsets.fromLTRB(16, 12, 16, 24),
      children: [
        Text(
          'Choose a user, then set Viewer or Owner on each set. Admins '
          'already see every set.',
          style: Theme.of(context).textTheme.bodySmall,
        ),
        const SizedBox(height: 12),
        KeyedSubtree(
          key: const Key('admin_perms_user_select'),
          child: Autocomplete<User>(
            key: ValueKey<int?>(selectedUserId),
            displayStringForOption: userLabel,
            initialValue: selectedUser == null
                ? null
                : TextEditingValue(text: userLabel(selectedUser!)),
            optionsBuilder: (editing) {
              final q = editing.text.trim().toLowerCase();
              if (q.isEmpty) return users;
              return users.where((u) {
                final label = userLabel(u).toLowerCase();
                return label.contains(q) ||
                    u.username.toLowerCase().contains(q);
              });
            },
            onSelected: (user) => onSelectUser(user.id),
            fieldViewBuilder:
                (context, textController, focusNode, onFieldSubmitted) {
              return TextField(
                key: const Key('admin_perms_user_search'),
                controller: textController,
                focusNode: focusNode,
                decoration: const InputDecoration(
                  labelText: 'User',
                  hintText: 'Search users',
                  prefixIcon: Icon(Icons.person_search),
                  border: OutlineInputBorder(),
                  isDense: true,
                ),
                onSubmitted: (_) => onFieldSubmitted(),
              );
            },
            optionsViewBuilder: (context, onSelected, options) {
              return Align(
                alignment: Alignment.topLeft,
                child: Material(
                  elevation: 4,
                  child: ConstrainedBox(
                    constraints: const BoxConstraints(
                      maxHeight: 240,
                      maxWidth: 400,
                    ),
                    child: ListView.builder(
                      key: const Key('admin_perms_user_options'),
                      padding: EdgeInsets.zero,
                      shrinkWrap: true,
                      itemCount: options.length,
                      itemBuilder: (context, index) {
                        final user = options.elementAt(index);
                        return ListTile(
                          key: Key('admin_perms_user_option_${user.id}'),
                          dense: true,
                          title: Text(userLabel(user)),
                          onTap: () => onSelected(user),
                        );
                      },
                    ),
                  ),
                ),
              );
            },
          ),
        ),
        const SizedBox(height: 12),
        TextField(
          key: const Key('admin_perms_set_search'),
          controller: setQueryController,
          decoration: const InputDecoration(
            labelText: 'Search sets',
            prefixIcon: Icon(Icons.search),
            border: OutlineInputBorder(),
            isDense: true,
          ),
          onChanged: onQueryChanged,
        ),
        if (isAdmin) ...[
          const SizedBox(height: 16),
          Card(
            key: const Key('admin_perms_admin_banner'),
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Text(
                '${selectedUser!.username} is an admin and has implicit '
                'access to every set. Role edits are disabled.',
                style: Theme.of(context).textTheme.bodyMedium,
              ),
            ),
          ),
        ],
        const SizedBox(height: 8),
        if (filtered.isEmpty)
          Padding(
            padding: const EdgeInsets.symmetric(vertical: 24),
            child: Text(
              'No sets match “$setQuery”.',
              key: const Key('admin_perms_set_filter_empty'),
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.bodyMedium,
            ),
          )
        else
          ...filtered.map((set) {
            final userId = selectedUserId;
            if (userId == null) return const SizedBox.shrink();
            final role = isAdmin
                ? 'owner'
                : (roles[_PermKey(userId: userId, setId: set.id)] ?? '');
            return ListTile(
              key: Key('admin_perms_set_tile_${set.id}'),
              contentPadding: EdgeInsets.zero,
              title: Text(set.name),
              subtitle: set.isPodcast ? const Text('Podcast') : null,
              trailing: SizedBox(
                width: 120,
                child: _RoleDropdown(
                  key: Key('perm_${userId}_${set.id}'),
                  value: role,
                  enabled: !isAdmin,
                  onChanged: (next) => onSetRole(userId, set.id, next),
                ),
              ),
            );
          }),
        // Keep a stable list key for tests that look for the editor body.
        const SizedBox(key: Key('admin_perms_set_list'), height: 0),
      ],
    );
  }
}

// ---------------------------------------------------------------------------
// Advanced permission matrix (users × sets) with sticky user labels
// ---------------------------------------------------------------------------

/// Permission matrix with a frozen user column and frozen set header row.
///
/// Vertical scroll moves users and cells together; horizontal scroll moves only
/// set headers and role cells so user names stay visible.
class _PermissionMatrix extends StatefulWidget {
  const _PermissionMatrix({
    required this.users,
    required this.sets,
    required this.roles,
    required this.onSetRole,
  });

  final List<User> users;
  final List<MediaSet> sets;
  final Map<_PermKey, String> roles;
  final Future<void> Function(int userId, int setId, String role) onSetRole;

  @override
  State<_PermissionMatrix> createState() => _PermissionMatrixState();
}

class _PermissionMatrixState extends State<_PermissionMatrix> {
  static const double _userColWidth = 132;
  static const double _cellWidth = 128;
  static const double _rowHeight = 56;

  final _headerH = ScrollController();
  final _bodyH = ScrollController();
  bool _syncingHorizontal = false;

  @override
  void initState() {
    super.initState();
    _headerH.addListener(_syncBodyFromHeader);
    _bodyH.addListener(_syncHeaderFromBody);
  }

  @override
  void dispose() {
    _headerH.removeListener(_syncBodyFromHeader);
    _bodyH.removeListener(_syncHeaderFromBody);
    _headerH.dispose();
    _bodyH.dispose();
    super.dispose();
  }

  void _syncBodyFromHeader() {
    if (_syncingHorizontal || !_bodyH.hasClients) return;
    _syncingHorizontal = true;
    if (_bodyH.offset != _headerH.offset) {
      _bodyH.jumpTo(_headerH.offset);
    }
    _syncingHorizontal = false;
  }

  void _syncHeaderFromBody() {
    if (_syncingHorizontal || !_headerH.hasClients) return;
    _syncingHorizontal = true;
    if (_headerH.offset != _bodyH.offset) {
      _headerH.jumpTo(_bodyH.offset);
    }
    _syncingHorizontal = false;
  }

  double get _setsWidth => widget.sets.length * _cellWidth;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final headerBg = theme.colorScheme.surfaceContainerHighest;

    return Column(
      key: const Key('admin_perms_table'),
      children: [
        Material(
          elevation: 1,
          color: headerBg,
          child: SizedBox(
            height: _rowHeight,
            child: Row(
              children: [
                SizedBox(
                  width: _userColWidth,
                  child: Padding(
                    padding: const EdgeInsets.symmetric(horizontal: 12),
                    child: Align(
                      alignment: Alignment.centerLeft,
                      child: Text('User', style: theme.textTheme.titleSmall),
                    ),
                  ),
                ),
                Expanded(
                  child: SingleChildScrollView(
                    controller: _headerH,
                    scrollDirection: Axis.horizontal,
                    child: SizedBox(
                      width: _setsWidth,
                      height: _rowHeight,
                      child: Row(
                        children: [
                          for (final set in widget.sets)
                            SizedBox(
                              width: _cellWidth,
                              child: Padding(
                                padding:
                                    const EdgeInsets.symmetric(horizontal: 8),
                                child: Align(
                                  alignment: Alignment.centerLeft,
                                  child: Text(
                                    set.name,
                                    maxLines: 2,
                                    overflow: TextOverflow.ellipsis,
                                    style: theme.textTheme.titleSmall,
                                  ),
                                ),
                              ),
                            ),
                        ],
                      ),
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),
        Expanded(
          child: SingleChildScrollView(
            physics: const AlwaysScrollableScrollPhysics(),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                SizedBox(
                  width: _userColWidth,
                  child: Column(
                    children: [
                      for (final user in widget.users)
                        ColoredBox(
                          color: user.isAdmin
                              ? headerBg
                              : Colors.transparent,
                          child: SizedBox(
                            height: _rowHeight,
                            child: Padding(
                              padding:
                                  const EdgeInsets.symmetric(horizontal: 12),
                              child: Align(
                                alignment: Alignment.centerLeft,
                                child: _UserCell(user: user),
                              ),
                            ),
                          ),
                        ),
                    ],
                  ),
                ),
                Expanded(
                  child: SingleChildScrollView(
                    controller: _bodyH,
                    scrollDirection: Axis.horizontal,
                    child: SizedBox(
                      width: _setsWidth,
                      child: Column(
                        children: [
                          for (final user in widget.users)
                            ColoredBox(
                              color: user.isAdmin
                                  ? headerBg
                                  : Colors.transparent,
                              child: SizedBox(
                                height: _rowHeight,
                                child: Row(
                                  children: [
                                    for (final set in widget.sets)
                                      SizedBox(
                                        width: _cellWidth,
                                        child: Padding(
                                          padding: const EdgeInsets.symmetric(
                                            horizontal: 8,
                                          ),
                                          child: _RoleDropdown(
                                            key: Key(
                                              'perm_${user.id}_${set.id}',
                                            ),
                                            value: user.isAdmin
                                                ? 'owner'
                                                : (widget.roles[_PermKey(
                                                      userId: user.id,
                                                      setId: set.id,
                                                    )] ??
                                                    ''),
                                            enabled: !user.isAdmin,
                                            onChanged: (next) =>
                                                widget.onSetRole(
                                              user.id,
                                              set.id,
                                              next,
                                            ),
                                          ),
                                        ),
                                      ),
                                  ],
                                ),
                              ),
                            ),
                        ],
                      ),
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),
      ],
    );
  }
}

// ---------------------------------------------------------------------------
// Shared role dropdown
// ---------------------------------------------------------------------------

class _RoleDropdown extends StatelessWidget {
  const _RoleDropdown({
    super.key,
    required this.value,
    required this.enabled,
    required this.onChanged,
  });

  /// Empty string means no access; otherwise `viewer` or `owner`.
  final String value;
  final bool enabled;
  final ValueChanged<String> onChanged;

  @override
  Widget build(BuildContext context) {
    return DropdownButton<String>(
      value: value,
      isDense: true,
      isExpanded: true,
      underline: const SizedBox.shrink(),
      onChanged: enabled
          ? (next) {
              if (next != null) onChanged(next);
            }
          : null,
      items: const [
        DropdownMenuItem(value: '', child: Text('—')),
        DropdownMenuItem(value: 'viewer', child: Text('Viewer')),
        DropdownMenuItem(value: 'owner', child: Text('Owner')),
      ],
    );
  }
}

// ---------------------------------------------------------------------------
// User cell widget
// ---------------------------------------------------------------------------

class _UserCell extends StatelessWidget {
  const _UserCell({required this.user});

  final User user;

  @override
  Widget build(BuildContext context) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Flexible(
          child: Text(
            user.username,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
          ),
        ),
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
            backgroundColor: Theme.of(context).colorScheme.primaryContainer,
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
