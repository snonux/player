import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/models.dart';
import '../providers/api_client_provider.dart';
import '../utils/error_mappers.dart';

/// Admin-only screen for ambiguous music identity conflicts.
///
/// Mirrors web Admin › Music identity: keep separate (safe) or merge into the
/// older song so playlists and history stay attached.
class AdminMusicIdentityScreen extends ConsumerStatefulWidget {
  const AdminMusicIdentityScreen({super.key});

  @override
  ConsumerState<AdminMusicIdentityScreen> createState() =>
      _AdminMusicIdentityScreenState();
}

class _AdminMusicIdentityScreenState
    extends ConsumerState<AdminMusicIdentityScreen> {
  List<MusicIdentityConflict>? _conflicts;
  String? _error;
  bool _isLoading = false;
  final Set<int> _busy = {};
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
      final list =
          await ref.read(apiClientProvider).listMusicIdentityConflicts();
      if (!mounted || generation != _generation) return;
      setState(() {
        _conflicts = list;
        _isLoading = false;
      });
    } catch (e) {
      if (!mounted || generation != _generation) return;
      setState(() {
        _error = adminIdentityErrorMessage(e);
        _isLoading = false;
      });
    }
  }

  void _showError(String message) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        key: const Key('admin_identity_error_snackbar'),
        content: Text(message),
      ),
    );
  }

  Future<void> _resolve({
    required MusicIdentityConflict conflict,
    required String action,
    int? candidateMediaId,
  }) async {
    if (_busy.contains(conflict.id)) return;
    setState(() => _busy.add(conflict.id));
    try {
      await ref.read(apiClientProvider).resolveMusicIdentityConflict(
            conflictId: conflict.id,
            action: action,
            candidateMediaId: candidateMediaId,
          );
      if (!mounted) return;
      setState(() {
        _conflicts = [
          for (final item in _conflicts ?? const <MusicIdentityConflict>[])
            if (item.id != conflict.id) item,
        ];
        _busy.remove(conflict.id);
      });
      final messenger = ScaffoldMessenger.of(context);
      messenger.showSnackBar(SnackBar(
        content: Text(action == 'merge'
            ? 'Merged — playlists and history kept on the older song'
            : 'Kept separate'),
      ));
    } catch (e) {
      if (!mounted) return;
      setState(() => _busy.remove(conflict.id));
      _showError(adminIdentityErrorMessage(e));
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Music identity'),
        actions: [
          IconButton(
            key: const Key('admin_identity_refresh'),
            icon: const Icon(Icons.refresh),
            tooltip: 'Refresh',
            onPressed: _load,
          ),
        ],
      ),
      body: _buildBody(context),
    );
  }

  Widget _buildBody(BuildContext context) {
    if (_isLoading && _conflicts == null) {
      return const Center(
        key: Key('admin_identity_loading'),
        child: CircularProgressIndicator(),
      );
    }
    if (_error != null) {
      return _ErrorView(message: _error!, onRetry: _load);
    }
    final conflicts = _conflicts ?? const <MusicIdentityConflict>[];
    return RefreshIndicator(
      onRefresh: _load,
      child: conflicts.isEmpty
          ? const _EmptyView()
          : ListView.separated(
              key: const Key('admin_identity_list'),
              itemCount: conflicts.length,
              separatorBuilder: (_, __) => const Divider(height: 1),
              itemBuilder: (_, index) {
                final conflict = conflicts[index];
                final busy = _busy.contains(conflict.id);
                final title = conflict.missingPath.isNotEmpty
                    ? conflict.missingPath
                    : '#${conflict.missingMediaId}';
                final candidates = conflict.candidatePaths.isNotEmpty
                    ? conflict.candidatePaths.join(', ')
                    : 'unknown path';
                return Padding(
                  key: Key('admin_identity_tile_${conflict.id}'),
                  padding:
                      const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      Text(title, style: Theme.of(context).textTheme.titleSmall),
                      const SizedBox(height: 4),
                      Text(
                        'Candidates: $candidates',
                        style: Theme.of(context).textTheme.bodySmall,
                      ),
                      if (conflict.reason.isNotEmpty) ...[
                        const SizedBox(height: 4),
                        Text(
                          conflict.reason,
                          style: Theme.of(context).textTheme.bodySmall,
                        ),
                      ],
                      const SizedBox(height: 12),
                      Wrap(
                        spacing: 8,
                        runSpacing: 8,
                        children: [
                          OutlinedButton(
                            key: Key(
                                'admin_identity_keep_${conflict.id}'),
                            onPressed: busy
                                ? null
                                : () => _resolve(
                                      conflict: conflict,
                                      action: 'keep_separate',
                                    ),
                            child: const Text('Keep separate'),
                          ),
                          for (var i = 0;
                              i < conflict.candidateMediaIds.length;
                              i++)
                            FilledButton(
                              key: Key(
                                  'admin_identity_merge_${conflict.id}_${conflict.candidateMediaIds[i]}'),
                              onPressed: busy
                                  ? null
                                  : () => _resolve(
                                        conflict: conflict,
                                        action: 'merge',
                                        candidateMediaId:
                                            conflict.candidateMediaIds[i],
                                      ),
                              child: Text(
                                'Merge → ${i < conflict.candidatePaths.length ? conflict.candidatePaths[i] : conflict.candidateMediaIds[i]}',
                                overflow: TextOverflow.ellipsis,
                              ),
                            ),
                        ],
                      ),
                    ],
                  ),
                );
              },
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
                    Icons.fingerprint_outlined,
                    size: 72,
                    color: Theme.of(context).colorScheme.onSurfaceVariant,
                  ),
                  const SizedBox(height: 16),
                  Text(
                    'No identity conflicts',
                    key: const Key('admin_identity_empty'),
                    style: Theme.of(context).textTheme.titleMedium,
                    textAlign: TextAlign.center,
                  ),
                  const SizedBox(height: 8),
                  Text(
                    'Moved songs keep playlists and history automatically. '
                    'Only ambiguous matches appear here.',
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
              key: const Key('admin_identity_error'),
              textAlign: TextAlign.center,
            ),
            const SizedBox(height: 16),
            FilledButton(
              key: const Key('admin_identity_retry'),
              onPressed: onRetry,
              child: const Text('Retry'),
            ),
          ],
        ),
      ),
    );
  }
}
