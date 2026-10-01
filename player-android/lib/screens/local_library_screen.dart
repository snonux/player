import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../app_routes.dart';
import '../providers/settings_provider.dart';

class LocalLibraryScreen extends ConsumerWidget {
  const LocalLibraryScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final settings = ref.watch(settingsProvider).valueOrNull;
    return Scaffold(
      appBar: AppBar(
        title: const Text('On this device'),
        actions: [
          IconButton(
            key: const Key('local_settings_button'),
            tooltip: 'Settings',
            onPressed: () => context.push(AppRoutes.settings),
            icon: const Icon(Icons.settings_outlined),
          ),
        ],
      ),
      body: Center(
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
                key: const Key('local_add_files'),
                onPressed: () => ScaffoldMessenger.of(context).showSnackBar(
                  const SnackBar(content: Text('File import is coming soon.')),
                ),
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
              if (settings?.configurationError != null) ...[
                const SizedBox(height: 20),
                Text(settings!.configurationError!,
                    textAlign: TextAlign.center,
                    style:
                        TextStyle(color: Theme.of(context).colorScheme.error)),
              ],
            ],
          ),
        ),
      ),
    );
  }
}
