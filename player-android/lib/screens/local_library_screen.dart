import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../app_routes.dart';
import '../models/local_media.dart';
import '../providers/local_library_provider.dart';
import '../providers/settings_provider.dart';
import '../services/local_document_picker.dart';

enum _LocalMediaAction { relink, remove }

class LocalLibraryScreen extends ConsumerStatefulWidget {
  const LocalLibraryScreen({super.key});

  @override
  ConsumerState<LocalLibraryScreen> createState() => _LocalLibraryScreenState();
}

class _LocalLibraryScreenState extends ConsumerState<LocalLibraryScreen> {
  bool _busy = false;

  @override
  Widget build(BuildContext context) {
    final settings = ref.watch(settingsProvider).valueOrNull;
    final library = ref.watch(localMediaListProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('On this device'),
        actions: [
          IconButton(
            key: const Key('local_add_files'),
            tooltip: 'Add files',
            onPressed: _busy ? null : _addFiles,
            icon: const Icon(Icons.add),
          ),
          IconButton(
            tooltip: 'Connect to server',
            onPressed: () => context.push(AppRoutes.server),
            icon: const Icon(Icons.cloud_outlined),
          ),
          IconButton(
            key: const Key('local_settings_button'),
            tooltip: 'Settings',
            onPressed: () => context.push(AppRoutes.settings),
            icon: const Icon(Icons.settings_outlined),
          ),
        ],
      ),
      body: library.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (error, _) => _LibraryError(
          error: error,
          retry: () => ref.invalidate(localMediaListProvider),
        ),
        data: (items) => items.isEmpty
            ? _emptyLibrary(settings?.configurationError)
            : _mediaList(items),
      ),
    );
  }

  Widget _emptyLibrary(String? configurationError) => Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const Icon(Icons.library_music_outlined, size: 56),
              const SizedBox(height: 16),
              Text('Your device library is empty',
                  style: Theme.of(context).textTheme.titleLarge),
              const SizedBox(height: 8),
              const Text('Add audio or video files stored on this device.'),
              const SizedBox(height: 24),
              FilledButton.icon(
                onPressed: _busy ? null : _addFiles,
                icon: const Icon(Icons.add),
                label: const Text('Add files'),
              ),
              const SizedBox(height: 12),
              OutlinedButton.icon(
                key: const Key('local_connect_server'),
                onPressed: () => context.push(AppRoutes.server),
                icon: const Icon(Icons.cloud_outlined),
                label: const Text('Connect to server'),
              ),
              if (configurationError != null) ...[
                const SizedBox(height: 20),
                Text(configurationError,
                    textAlign: TextAlign.center,
                    style:
                        TextStyle(color: Theme.of(context).colorScheme.error)),
              ],
            ],
          ),
        ),
      );

  Widget _mediaList(List<LocalMedia> items) => ListView(
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 20, 16, 8),
            child:
                Text('Recent', style: Theme.of(context).textTheme.titleLarge),
          ),
          for (final media in items)
            ListTile(
              key: ValueKey('local_media_${media.id}'),
              leading: Icon(_iconFor(media.mimeType)),
              title: Text(media.title,
                  maxLines: 1, overflow: TextOverflow.ellipsis),
              subtitle: Text(_subtitle(media)),
              trailing: PopupMenuButton<_LocalMediaAction>(
                tooltip: 'Manage ${media.title}',
                onSelected: (action) => switch (action) {
                  _LocalMediaAction.relink => _relink(media),
                  _LocalMediaAction.remove => _remove(media),
                },
                itemBuilder: (context) => const [
                  PopupMenuItem(
                    value: _LocalMediaAction.relink,
                    child: Text('Choose file again'),
                  ),
                  PopupMenuItem(
                    value: _LocalMediaAction.remove,
                    child: Text('Remove from library'),
                  ),
                ],
              ),
            ),
        ],
      );

  Future<void> _addFiles() async {
    final kind = await showModalBottomSheet<LocalDocumentKind>(
      context: context,
      builder: (context) => SafeArea(
        child: Wrap(
          children: [
            ListTile(
              leading: const Icon(Icons.headphones),
              title: const Text('Choose audio'),
              onTap: () => Navigator.pop(context, LocalDocumentKind.audio),
            ),
            ListTile(
              leading: const Icon(Icons.movie_outlined),
              title: const Text('Choose video'),
              onTap: () => Navigator.pop(context, LocalDocumentKind.video),
            ),
          ],
        ),
      ),
    );
    if (kind == null || !mounted) return;
    await _runOperation(() async {
      final added =
          await ref.read(localLibraryManagerProvider).addDocument(kind);
      if (added != null) {
        ref.invalidate(localMediaListProvider);
        _showMessage('Added ${added.title}');
      }
    });
  }

  Future<void> _relink(LocalMedia media) async {
    await _runOperation(() async {
      final relinked = await ref.read(localLibraryManagerProvider).relink(
            id: media.id,
            confirmRetainProgress: (current, replacement) async {
              if (!mounted) return null;
              return showDialog<bool>(
                context: context,
                builder: (context) => AlertDialog(
                  title: const Text('Replace this file?'),
                  content: const Text(
                    'Keep the current resume position for the replacement, or start it from the beginning?',
                  ),
                  actions: [
                    TextButton(
                      onPressed: () => Navigator.pop(context),
                      child: const Text('Cancel'),
                    ),
                    TextButton(
                      onPressed: () => Navigator.pop(context, false),
                      child: const Text('Start over'),
                    ),
                    FilledButton(
                      onPressed: () => Navigator.pop(context, true),
                      child: const Text('Keep progress'),
                    ),
                  ],
                ),
              );
            },
          );
      if (relinked != null) {
        ref.invalidate(localMediaListProvider);
        final message = relinked.previousFileWasUnavailable
            ? 'The previous file was unavailable. Updated ${relinked.media.title}.'
            : 'Updated ${relinked.media.title}';
        _showMessage(relinked.oldGrantReleaseFailed
            ? '$message Android could not release the old file permission; Player will retry.'
            : message);
      }
    });
  }

  Future<void> _remove(LocalMedia media) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Remove from library?'),
        content: Text(
          '${media.title} will be removed from Player. The original file will stay on your device.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('Remove'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;
    await _runOperation(() async {
      final result =
          await ref.read(localLibraryManagerProvider).remove(media.id);
      ref.invalidate(localMediaListProvider);
      if (result.releaseFailed) {
        _showMessage(
          'Removed from Player, but Android could not release the file permission.',
        );
      } else if (result.removed) {
        _showMessage('Removed from library');
      }
    });
  }

  Future<void> _runOperation(Future<void> Function() operation) async {
    setState(() => _busy = true);
    try {
      await operation();
    } catch (error) {
      if (mounted) _showMessage(error.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  void _showMessage(String message) {
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(SnackBar(content: Text(message)));
  }

  IconData _iconFor(String mimeType) =>
      mimeType.toLowerCase().startsWith('video/')
          ? Icons.movie_outlined
          : Icons.headphones;

  String _subtitle(LocalMedia media) {
    final size = media.sizeBytes;
    if (size == null) return media.mimeType;
    final label = size < 1024 * 1024
        ? '${(size / 1024).ceil()} KB'
        : '${(size / (1024 * 1024)).toStringAsFixed(1)} MB';
    return '${media.mimeType} · $label';
  }
}

class _LibraryError extends StatelessWidget {
  const _LibraryError({required this.error, required this.retry});

  final Object error;
  final VoidCallback retry;

  @override
  Widget build(BuildContext context) => Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const Icon(Icons.error_outline, size: 48),
              const SizedBox(height: 12),
              Text('Could not open the local library: $error',
                  textAlign: TextAlign.center),
              const SizedBox(height: 16),
              OutlinedButton(onPressed: retry, child: const Text('Retry')),
            ],
          ),
        ),
      );
}
