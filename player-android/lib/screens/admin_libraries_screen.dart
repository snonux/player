import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/models.dart';
import '../providers/api_client_provider.dart';
import '../utils/error_mappers.dart';

/// Admin-only screen for independent music library roots.
///
/// Mirrors the web Admin › Libraries workflows: list, add, rename, remove,
/// and grant/revoke viewer/owner access for every set in a library.
class AdminLibrariesScreen extends ConsumerStatefulWidget {
  const AdminLibrariesScreen({super.key});

  @override
  ConsumerState<AdminLibrariesScreen> createState() =>
      _AdminLibrariesScreenState();
}

class _AdminLibrariesScreenState extends ConsumerState<AdminLibrariesScreen> {
  List<MusicLibrary>? _libraries;
  List<User> _users = const [];
  String? _error;
  bool _isLoading = false;
  int _generation = 0;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
  }

  Future<void> _load() async {
    if (!mounted) return;
    final generation = ++_generation;
    setState(() {
      _isLoading = true;
      _error = null;
    });
    try {
      final client = ref.read(apiClientProvider);
      final results = await Future.wait([
        client.listAdminLibraries(),
        client.listUsers(),
      ]);
      if (!mounted || generation != _generation) return;
      setState(() {
        _libraries = results[0] as List<MusicLibrary>;
        _users = results[1] as List<User>;
        _isLoading = false;
      });
    } catch (e) {
      if (!mounted || generation != _generation) return;
      setState(() {
        _error = adminLibraryErrorMessage(e);
        _isLoading = false;
      });
    }
  }

  void _showError(String message) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        key: const Key('admin_libraries_error_snackbar'),
        content: Text(message),
      ),
    );
  }

  Future<void> _showAddDialog() async {
    final input = await showDialog<_LibraryFormInput>(
      context: context,
      builder: (_) => const _LibraryFormDialog(
        title: 'Add library',
        submitLabel: 'Add',
        requirePath: true,
      ),
    );
    if (input == null || !mounted) return;
    try {
      final created = await ref.read(apiClientProvider).createLibrary(
            name: input.name,
            rootPath: input.rootPath!,
          );
      if (!mounted) return;
      setState(() => _libraries = [...?_libraries, created]);
    } catch (e) {
      _showError(adminLibraryErrorMessage(e));
    }
  }

  Future<void> _rename(MusicLibrary library) async {
    final input = await showDialog<_LibraryFormInput>(
      context: context,
      builder: (_) => _LibraryFormDialog(
        title: 'Rename library',
        submitLabel: 'Rename',
        initialName: library.name,
      ),
    );
    if (input == null || !mounted) return;
    try {
      final updated = await ref.read(apiClientProvider).renameLibrary(
            libraryId: library.id,
            name: input.name,
          );
      if (!mounted) return;
      setState(() {
        _libraries = [
          for (final lib in _libraries ?? const <MusicLibrary>[])
            if (lib.id == library.id) updated else lib,
        ];
      });
    } catch (e) {
      _showError(adminLibraryErrorMessage(e));
    }
  }

  Future<void> _delete(MusicLibrary library) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Remove library?'),
        content: Text(
          'Remove "${library.name}"? It must have no sets.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(ctx, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            key: Key('admin_libraries_confirm_delete_${library.id}'),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Remove'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;
    final previous = _libraries;
    setState(() {
      _libraries = [
        for (final lib in _libraries ?? const <MusicLibrary>[])
          if (lib.id != library.id) lib,
      ];
    });
    try {
      await ref.read(apiClientProvider).deleteLibrary(library.id);
    } catch (e) {
      if (!mounted) return;
      setState(() => _libraries = previous);
      _showError(adminLibraryErrorMessage(e));
    }
  }

  Future<void> _manageAccess(MusicLibrary library) async {
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      builder: (ctx) => _LibraryAccessSheet(
        library: library,
        users: _users,
        onGrant: (userId, role) => ref.read(apiClientProvider).grantLibraryAccess(
              libraryId: library.id,
              userId: userId,
              role: role,
            ),
        onRevoke: (userId) => ref.read(apiClientProvider).revokeLibraryAccess(
              libraryId: library.id,
              userId: userId,
            ),
        onError: adminLibraryErrorMessage,
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Libraries'),
        actions: [
          IconButton(
            key: const Key('admin_libraries_refresh'),
            icon: const Icon(Icons.refresh),
            tooltip: 'Refresh',
            onPressed: _load,
          ),
        ],
      ),
      floatingActionButton: FloatingActionButton.extended(
        key: const Key('admin_libraries_add'),
        onPressed: _showAddDialog,
        icon: const Icon(Icons.add),
        label: const Text('Add library'),
      ),
      body: _buildBody(context),
    );
  }

  Widget _buildBody(BuildContext context) {
    if (_isLoading && _libraries == null) {
      return const Center(
        key: Key('admin_libraries_loading'),
        child: CircularProgressIndicator(),
      );
    }
    if (_error != null) {
      return _ErrorView(message: _error!, onRetry: _load);
    }
    return RefreshIndicator(
      onRefresh: _load,
      child: _libraries == null || _libraries!.isEmpty
          ? const _EmptyView()
          : ListView.separated(
              key: const Key('admin_libraries_list'),
              padding: const EdgeInsets.only(bottom: 88),
              itemCount: _libraries!.length,
              separatorBuilder: (_, __) => const Divider(height: 1),
              itemBuilder: (_, index) {
                final library = _libraries![index];
                final stats = library.stats;
                final statsLine = stats == null
                    ? library.rootPath
                    : '${library.rootPath}\n'
                        '${stats.setCount} sets · ${stats.songCount} songs';
                return ListTile(
                  key: Key('admin_libraries_tile_${library.id}'),
                  contentPadding:
                      const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
                  title: Text(library.name),
                  subtitle: Text(
                    statsLine,
                    maxLines: 3,
                    overflow: TextOverflow.ellipsis,
                  ),
                  isThreeLine: stats != null,
                  trailing: PopupMenuButton<String>(
                    key: Key('admin_libraries_menu_${library.id}'),
                    tooltip: 'Library actions',
                    onSelected: (value) {
                      switch (value) {
                        case 'rename':
                          _rename(library);
                        case 'access':
                          _manageAccess(library);
                        case 'delete':
                          _delete(library);
                      }
                    },
                    itemBuilder: (_) => const [
                      PopupMenuItem(value: 'rename', child: Text('Rename')),
                      PopupMenuItem(
                        value: 'access',
                        child: Text('Manage access'),
                      ),
                      PopupMenuItem(value: 'delete', child: Text('Remove')),
                    ],
                  ),
                );
              },
            ),
    );
  }
}

class _LibraryFormInput {
  const _LibraryFormInput({required this.name, this.rootPath});
  final String name;
  final String? rootPath;
}

class _LibraryFormDialog extends StatefulWidget {
  const _LibraryFormDialog({
    required this.title,
    required this.submitLabel,
    this.initialName = '',
    this.requirePath = false,
  });

  final String title;
  final String submitLabel;
  final String initialName;
  final bool requirePath;

  @override
  State<_LibraryFormDialog> createState() => _LibraryFormDialogState();
}

class _LibraryFormDialogState extends State<_LibraryFormDialog> {
  late final TextEditingController _name =
      TextEditingController(text: widget.initialName);
  final TextEditingController _path = TextEditingController();
  final _formKey = GlobalKey<FormState>();

  @override
  void dispose() {
    _name.dispose();
    _path.dispose();
    super.dispose();
  }

  void _submit() {
    if (!_formKey.currentState!.validate()) return;
    Navigator.pop(
      context,
      _LibraryFormInput(
        name: _name.text.trim(),
        rootPath: widget.requirePath ? _path.text.trim() : null,
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      title: Text(widget.title),
      content: Form(
        key: _formKey,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextFormField(
              key: const Key('admin_libraries_name_field'),
              controller: _name,
              autofocus: true,
              decoration: const InputDecoration(labelText: 'Name'),
              textInputAction: widget.requirePath
                  ? TextInputAction.next
                  : TextInputAction.done,
              onFieldSubmitted: (_) {
                if (!widget.requirePath) _submit();
              },
              validator: (value) =>
                  (value == null || value.trim().isEmpty) ? 'Required' : null,
            ),
            if (widget.requirePath) ...[
              const SizedBox(height: 12),
              TextFormField(
                key: const Key('admin_libraries_path_field'),
                controller: _path,
                decoration: const InputDecoration(
                  labelText: 'Filesystem path',
                  hintText: '/media/other',
                ),
                textInputAction: TextInputAction.done,
                onFieldSubmitted: (_) => _submit(),
                validator: (value) =>
                    (value == null || value.trim().isEmpty) ? 'Required' : null,
              ),
            ],
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context),
          child: const Text('Cancel'),
        ),
        FilledButton(
          key: const Key('admin_libraries_form_submit'),
          onPressed: _submit,
          child: Text(widget.submitLabel),
        ),
      ],
    );
  }
}

class _LibraryAccessSheet extends StatefulWidget {
  const _LibraryAccessSheet({
    required this.library,
    required this.users,
    required this.onGrant,
    required this.onRevoke,
    required this.onError,
  });

  final MusicLibrary library;
  final List<User> users;
  final Future<void> Function(int userId, String role) onGrant;
  final Future<void> Function(int userId) onRevoke;
  final String Function(Object error) onError;

  @override
  State<_LibraryAccessSheet> createState() => _LibraryAccessSheetState();
}

class _LibraryAccessSheetState extends State<_LibraryAccessSheet> {
  final Map<int, String> _roles = {};
  final Set<int> _busy = {};
  String? _status;

  Future<void> _setRole(User user, String? role) async {
    setState(() {
      _busy.add(user.id);
      _status = null;
    });
    try {
      if (role == null || role.isEmpty) {
        await widget.onRevoke(user.id);
        if (!mounted) return;
        setState(() => _roles[user.id] = '');
      } else {
        await widget.onGrant(user.id, role);
        if (!mounted) return;
        setState(() => _roles[user.id] = role);
      }
    } catch (e) {
      if (!mounted) return;
      setState(() => _status = widget.onError(e));
    } finally {
      if (mounted) setState(() => _busy.remove(user.id));
    }
  }

  @override
  Widget build(BuildContext context) {
    final bottom = MediaQuery.viewInsetsOf(context).bottom;
    return Padding(
      padding: EdgeInsets.only(bottom: bottom),
      child: SafeArea(
        child: SizedBox(
          height: MediaQuery.sizeOf(context).height * 0.55,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
                child: Text(
                  'Access · ${widget.library.name}',
                  style: Theme.of(context).textTheme.titleMedium,
                ),
              ),
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 16),
                child: Text(
                  'Grants the same viewer/owner roles as Permissions on every '
                  'set in this library.',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ),
              if (_status != null)
                Padding(
                  padding: const EdgeInsets.all(16),
                  child: Text(
                    _status!,
                    key: const Key('admin_libraries_access_status'),
                    style: TextStyle(color: Theme.of(context).colorScheme.error),
                  ),
                ),
              Expanded(
                child: widget.users.isEmpty
                    ? const Center(
                        child: Text('No users available.'),
                      )
                    : ListView.builder(
                        key: const Key('admin_libraries_access_list'),
                        itemCount: widget.users.length,
                        itemBuilder: (_, index) {
                          final user = widget.users[index];
                          final value = _roles[user.id] ?? '';
                          return ListTile(
                            title: Text(user.username),
                            subtitle: user.isAdmin
                                ? const Text('Admin (implicit access)')
                                : null,
                            trailing: DropdownButton<String>(
                              key: Key(
                                  'admin_libraries_access_${widget.library.id}_${user.id}'),
                              value: value,
                              onChanged: _busy.contains(user.id)
                                  ? null
                                  : (role) => _setRole(user, role),
                              items: const [
                                DropdownMenuItem(
                                    value: '', child: Text('—')),
                                DropdownMenuItem(
                                    value: 'viewer', child: Text('Viewer')),
                                DropdownMenuItem(
                                    value: 'owner', child: Text('Owner')),
                              ],
                            ),
                          );
                        },
                      ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

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
            child: Padding(
              padding: const EdgeInsets.all(24),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Icon(
                    Icons.library_music_outlined,
                    size: 72,
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
                  const SizedBox(height: 16),
                  Text(
                    'No libraries yet',
                    key: const Key('admin_libraries_empty'),
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                  const SizedBox(height: 8),
                  Text(
                    'Add a filesystem root to create another music library.',
                    textAlign: TextAlign.center,
                    style: Theme.of(context).textTheme.bodyMedium,
                  ),
                ],
              ),
            ),
          ),
        ),
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
            Text(
              message,
              key: const Key('admin_libraries_error'),
              textAlign: TextAlign.center,
            ),
            const SizedBox(height: 16),
            FilledButton(
              key: const Key('admin_libraries_retry'),
              onPressed: onRetry,
              child: const Text('Retry'),
            ),
          ],
        ),
      ),
    );
  }
}
