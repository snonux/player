import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/models.dart';
import '../providers/api_client_provider.dart';
import '../providers/current_user_provider.dart';
import '../utils/error_mappers.dart';

/// Admin-only screen for managing registered user accounts.
///
/// Design notes:
///   - Only accessible to admin users; the Settings screen gates the nav entry
///     on [currentUserProvider] → [User.isAdmin].
///   - A generation counter prevents stale async loads: if the user triggers
///     a refresh while a previous load is still in flight, the old result is
///     silently discarded when it arrives.
///   - Create and delete use optimistic UI: the list is mutated locally first,
///     then the API call is made.  On error the mutation is reverted and a
///     SnackBar reports the problem.
///   - The current user's own row omits the delete action to prevent
///     self-deletion (the server also rejects it with 400, but we hide the
///     button to make the restriction obvious in the UI).
///   - All async continuations guard on [mounted] to prevent setState / context
///     calls after widget disposal.
class AdminUsersScreen extends ConsumerStatefulWidget {
  const AdminUsersScreen({super.key});

  @override
  ConsumerState<AdminUsersScreen> createState() => _AdminUsersScreenState();
}

class _AdminUsersScreenState extends ConsumerState<AdminUsersScreen> {
  // Null while the initial load is in-flight; non-null (possibly empty) after
  // the first successful fetch.
  List<User>? _users;

  // Non-null when the last load attempt failed.
  String? _error;

  // True while a load is in flight (initial or refresh).
  bool _isLoading = false;

  // Generation counter: incremented on every load call.  Async completions
  // compare against the current generation and discard stale results.
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

  /// Fetches the full user list and updates local state.
  ///
  /// Increments [_generation] so results from a previous in-flight request are
  /// silently discarded if they arrive after a newer request has started.
  Future<void> _load() async {
    if (!mounted) return;
    final generation = ++_generation;

    setState(() {
      _isLoading = true;
      _error = null;
    });

    try {
      final users = await ref.read(apiClientProvider).listUsers();
      if (!mounted || generation != _generation) return;
      setState(() {
        _users = users;
        _isLoading = false;
      });
    } catch (e) {
      if (!mounted || generation != _generation) return;
      setState(() {
        _error = adminUserErrorMessage(e);
        _isLoading = false;
      });
    }
  }

  // ---------------------------------------------------------------------------
  // Create user action
  // ---------------------------------------------------------------------------

  /// Opens the create-user dialog and submits if the user confirms.
  ///
  /// Uses optimistic UI: the new user row is appended to [_users] immediately,
  /// then the real API response replaces it (or reverts on error).
  Future<void> _showCreateDialog() async {
    final result = await showDialog<_CreateUserInput>(
      context: context,
      builder: (_) => const _CreateUserDialog(),
    );
    if (result == null || !mounted) return;

    // Optimistic placeholder: id=0 will be replaced by the real server response.
    // Capture the index before appending so success and error paths can target
    // the exact slot without relying on reference equality (User has no == override).
    final placeholderIdx = _users?.length ?? 0;
    final placeholder = User(
      id: 0,
      username: result.username,
      isAdmin: result.isAdmin,
    );
    setState(() => _users = [...?_users, placeholder]);

    try {
      final created = await ref.read(apiClientProvider).createUser(
            username: result.username,
            password: result.password,
            isAdmin: result.isAdmin,
          );
      if (!mounted) return;
      // Replace the placeholder slot with the real user returned by the server.
      setState(() { _users![placeholderIdx] = created; });
    } catch (e) {
      if (!mounted) return;
      // Revert optimistic insertion on error by removing the known slot.
      setState(() {
        if (placeholderIdx < _users!.length) _users!.removeAt(placeholderIdx);
      });
      _showError(adminUserErrorMessage(e));
    }
  }

  // ---------------------------------------------------------------------------
  // Delete user action
  // ---------------------------------------------------------------------------

  /// Shows a confirmation dialog, then deletes [user] if confirmed.
  ///
  /// Optimistically removes the row first; reverts on error.
  Future<void> _deleteUser(User user, int index) async {
    final confirmed = await _confirmDelete(user.username);
    if (!confirmed || !mounted) return;

    // Optimistic removal.
    setState(() => _users!.removeAt(index));

    try {
      await ref.read(apiClientProvider).deleteUser(user.id);
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          key: const Key('admin_users_delete_snackbar'),
          content: Text('User "${user.username}" deleted.'),
          duration: const Duration(seconds: 3),
        ),
      );
    } catch (e) {
      if (!mounted) return;
      // Revert optimistic removal. Append rather than re-insert at index to
      // avoid position jitter from concurrent mutations.
      setState(() => _users = [..._users!, user]);
      _showError(adminUserErrorMessage(e));
    }
  }

  /// Shows a [AlertDialog] asking the user to confirm deletion.
  ///
  /// Returns true only when the user taps the "Delete" button.
  Future<bool> _confirmDelete(String username) async {
    final result = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Delete user'),
        content: Text('Delete "$username"? This cannot be undone.'),
        actions: [
          TextButton(
            key: const Key('admin_users_confirm_cancel'),
            onPressed: () => Navigator.of(ctx).pop(false),
            child: const Text('Cancel'),
          ),
          TextButton(
            key: const Key('admin_users_confirm_delete'),
            style: TextButton.styleFrom(
              foregroundColor: Theme.of(ctx).colorScheme.error,
            ),
            onPressed: () => Navigator.of(ctx).pop(true),
            child: const Text('Delete'),
          ),
        ],
      ),
    );
    return result ?? false;
  }

  // ---------------------------------------------------------------------------
  // Error display
  // ---------------------------------------------------------------------------

  void _showError(String message) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        key: const Key('admin_users_error_snackbar'),
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
    // Read the current user to identify the self-row (disable self-delete).
    final currentUserAsync = ref.watch(currentUserProvider);
    final currentUserId = currentUserAsync.valueOrNull?.id;

    return Scaffold(
      appBar: AppBar(
        title: const Text('Manage Users'),
        actions: [
          IconButton(
            key: const Key('admin_users_refresh'),
            icon: const Icon(Icons.refresh),
            tooltip: 'Refresh',
            onPressed: _load,
          ),
        ],
      ),
      floatingActionButton: FloatingActionButton(
        key: const Key('admin_users_fab'),
        tooltip: 'Create user',
        onPressed: _showCreateDialog,
        child: const Icon(Icons.person_add_outlined),
      ),
      body: _buildBody(context, currentUserId),
    );
  }

  /// Builds the appropriate body widget for the current state.
  Widget _buildBody(BuildContext context, int? currentUserId) {
    // Show a full-screen spinner while the very first load is in flight.
    if (_isLoading && _users == null) {
      return const Center(
        key: Key('admin_users_loading'),
        child: CircularProgressIndicator(),
      );
    }

    if (_error != null) {
      return _ErrorView(message: _error!, onRetry: _load);
    }

    return RefreshIndicator(
      onRefresh: _load,
      child: _users == null || _users!.isEmpty
          ? const _EmptyView()
          : _UserList(
              users: _users!,
              currentUserId: currentUserId,
              onDelete: _deleteUser,
            ),
    );
  }
}

// ---------------------------------------------------------------------------
// Sub-widgets
// ---------------------------------------------------------------------------

/// Scrollable list of [User] rows.
///
/// Extracted into its own stateless widget (SRP) so the state class stays
/// focused on data-loading and mutation concerns.
class _UserList extends StatelessWidget {
  const _UserList({
    required this.users,
    required this.currentUserId,
    required this.onDelete,
  });

  final List<User> users;

  /// The authenticated user's own ID; used to disable self-delete.
  final int? currentUserId;

  final Future<void> Function(User user, int index) onDelete;

  @override
  Widget build(BuildContext context) {
    return ListView.separated(
      key: const Key('admin_users_list'),
      itemCount: users.length,
      separatorBuilder: (_, __) => const Divider(height: 1),
      itemBuilder: (_, index) {
        final user = users[index];
        // Self-delete is both hidden from the UI and rejected by the server;
        // hiding it makes the constraint visible without a server round-trip.
        final isSelf = user.id == currentUserId && currentUserId != null;
        return _UserTile(
          user: user,
          isSelf: isSelf,
          index: index,
          onDelete: onDelete,
        );
      },
    );
  }
}

/// A single user row with a role badge and an optional delete action.
///
/// The delete icon is hidden when [isSelf] is true so users cannot delete
/// their own account from this screen.
class _UserTile extends StatelessWidget {
  const _UserTile({
    required this.user,
    required this.isSelf,
    required this.index,
    required this.onDelete,
  });

  final User user;
  final bool isSelf;
  final int index;
  final Future<void> Function(User user, int index) onDelete;

  @override
  Widget build(BuildContext context) {
    return ListTile(
      key: Key('admin_user_tile_${user.id}'),
      leading: CircleAvatar(
        child: Text(
          user.username.isNotEmpty ? user.username[0].toUpperCase() : '?',
        ),
      ),
      title: Text(user.username),
      subtitle: isSelf ? const Text('(you)') : null,
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          // Role badge: visually distinguishes admin accounts from regular users.
          _RoleBadge(isAdmin: user.isAdmin),
          // Delete button is hidden for the current user's own row.
          if (!isSelf) ...[
            const SizedBox(width: 8),
            IconButton(
              key: Key('admin_user_delete_${user.id}'),
              icon: const Icon(Icons.delete_outline),
              tooltip: 'Delete user',
              color: Theme.of(context).colorScheme.error,
              onPressed: () => onDelete(user, index),
            ),
          ],
        ],
      ),
    );
  }
}

/// Compact coloured chip that shows "Admin" or "User" depending on [isAdmin].
///
/// Kept as a dedicated widget so the badge style is consistent and can be
/// updated in one place without touching [_UserTile].
class _RoleBadge extends StatelessWidget {
  const _RoleBadge({required this.isAdmin});

  final bool isAdmin;

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;
    return Chip(
      label: Text(
        isAdmin ? 'Admin' : 'User',
        style: TextStyle(
          fontSize: 12,
          color: isAdmin ? colorScheme.onPrimaryContainer : colorScheme.onSurface,
        ),
      ),
      backgroundColor: isAdmin
          ? colorScheme.primaryContainer
          : colorScheme.surfaceContainerHighest,
      padding: EdgeInsets.zero,
      visualDensity: VisualDensity.compact,
    );
  }
}

/// Full-screen empty-state shown when the user list is empty.
///
/// Wrapped in a [ListView] so the parent [RefreshIndicator] can still trigger
/// pull-to-refresh even when no content is present.
class _EmptyView extends StatelessWidget {
  const _EmptyView();

  @override
  Widget build(BuildContext context) {
    // Wrap in a fixed-height CustomScrollView so RefreshIndicator still works
    // on an empty list, while Expanded+Center keeps the content vertically
    // centred without hard-coding a fraction of the screen height.
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
                  Icons.people_outline,
                  size: 72,
                  color: Theme.of(context).colorScheme.onSurfaceVariant,
                ),
                const SizedBox(height: 16),
                Text(
                  'No users found',
                  key: const Key('admin_users_empty'),
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
              key: const Key('admin_users_error'),
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.bodyLarge,
            ),
            const SizedBox(height: 24),
            ElevatedButton.icon(
              key: const Key('admin_users_retry'),
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

// ---------------------------------------------------------------------------
// Create-user dialog
// ---------------------------------------------------------------------------

/// Input value returned by [_CreateUserDialog] when the user confirms.
class _CreateUserInput {
  const _CreateUserInput({
    required this.username,
    required this.password,
    required this.isAdmin,
  });

  final String username;
  final String password;
  final bool isAdmin;
}

/// Dialog for creating a new user account.
///
/// Validates that username is non-empty and password is at least 8 characters.
/// Validation is inline (shown below the fields) so the user gets immediate
/// feedback without requiring a submit attempt.
///
/// Uses [StatefulWidget] rather than [ConsumerStatefulWidget] because the
/// dialog itself makes no API calls — the parent handles the network request.
class _CreateUserDialog extends StatefulWidget {
  const _CreateUserDialog();

  @override
  State<_CreateUserDialog> createState() => _CreateUserDialogState();
}

class _CreateUserDialogState extends State<_CreateUserDialog> {
  final _formKey = GlobalKey<FormState>();
  final _usernameController = TextEditingController();
  final _passwordController = TextEditingController();
  bool _isAdmin = false;

  // Show password as plain text when true (toggle with the visibility icon).
  bool _passwordVisible = false;

  @override
  void dispose() {
    _usernameController.dispose();
    _passwordController.dispose();
    super.dispose();
  }

  /// Validates the form and pops the dialog with a [_CreateUserInput] if valid.
  void _submit() {
    if (_formKey.currentState?.validate() != true) return;
    Navigator.of(context).pop(
      _CreateUserInput(
        username: _usernameController.text.trim(),
        password: _passwordController.text,
        isAdmin: _isAdmin,
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    // _buildForm and _buildActions were single-call-site helpers; inlined here
    // to reduce indirection. The merged build() stays well under 50 lines.
    return AlertDialog(
      key: const Key('admin_create_user_dialog'),
      title: const Text('Create user'),
      content: Form(
        key: _formKey,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextFormField(
              key: const Key('admin_create_username'),
              controller: _usernameController,
              decoration: const InputDecoration(
                labelText: 'Username',
                border: OutlineInputBorder(),
              ),
              textInputAction: TextInputAction.next,
              autocorrect: false,
              validator: (value) {
                if (value == null || value.trim().isEmpty) {
                  return 'Username is required.';
                }
                return null;
              },
            ),
            const SizedBox(height: 16),
            TextFormField(
              key: const Key('admin_create_password'),
              controller: _passwordController,
              decoration: InputDecoration(
                labelText: 'Password',
                border: const OutlineInputBorder(),
                // Toggle visibility icon so the admin can verify the typed password.
                suffixIcon: IconButton(
                  icon: Icon(
                    _passwordVisible
                        ? Icons.visibility_off_outlined
                        : Icons.visibility_outlined,
                  ),
                  tooltip: _passwordVisible ? 'Hide password' : 'Show password',
                  onPressed: () =>
                      setState(() => _passwordVisible = !_passwordVisible),
                ),
              ),
              obscureText: !_passwordVisible,
              textInputAction: TextInputAction.done,
              onFieldSubmitted: (_) => _submit(),
              validator: (value) {
                if (value == null || value.isEmpty) {
                  return 'Password is required.';
                }
                if (value.length < 8) {
                  return 'Password must be at least 8 characters.';
                }
                return null;
              },
            ),
            const SizedBox(height: 8),
            CheckboxListTile(
              key: const Key('admin_create_is_admin'),
              title: const Text('Administrator'),
              subtitle: const Text('Can manage users and settings'),
              value: _isAdmin,
              contentPadding: EdgeInsets.zero,
              onChanged: (value) => setState(() => _isAdmin = value ?? false),
            ),
          ],
        ),
      ),
      actions: [
        TextButton(
          key: const Key('admin_create_cancel'),
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('Cancel'),
        ),
        FilledButton(
          key: const Key('admin_create_submit'),
          onPressed: _submit,
          child: const Text('Create'),
        ),
      ],
    );
  }
}
