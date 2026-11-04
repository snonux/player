import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';
import '../providers/podcast_library_provider.dart';
import '../providers/settings_provider.dart';

/// Select cached personal state without signing in or combining accounts.
class PodcastLibrarySwitcher extends ConsumerWidget {
  const PodcastLibrarySwitcher({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    if (ref.watch(settingsProvider).valueOrNull?.destination !=
        LibraryDestination.local) {
      return const SizedBox.shrink();
    }
    final scope = ref.watch(podcastScopeProvider).valueOrNull;
    return ref.watch(podcastDeviceLibrariesProvider).when(
          loading: () => const SizedBox.shrink(),
          error: (_, __) => TextButton(
              onPressed: () => ref.invalidate(podcastDeviceLibrariesProvider),
              child: const Text('Retry opening podcast libraries')),
          data: (libraries) => libraries.length <= 1
              ? const SizedBox.shrink()
              : PopupMenuButton<String>(
                  tooltip: 'Choose podcast library',
                  initialValue: scope,
                  onSelected: (selected) async {
                    final prefs = await SharedPreferences.getInstance();
                    if (!context.mounted) return;
                    try {
                      if (!await prefs.setString(
                          'podcast_device_scope', selected)) {
                        throw StateError('Could not save library');
                      }
                      if (context.mounted) {
                        ref
                            .read(podcastDeviceScopeSelectionProvider.notifier)
                            .state = selected;
                      }
                    } catch (_) {
                      if (context.mounted) {
                        ScaffoldMessenger.of(context).showSnackBar(const SnackBar(
                            content: Text(
                                'Could not switch podcast library. Retry.')));
                      }
                    }
                  },
                  itemBuilder: (_) => [
                    for (final library in libraries.entries)
                      CheckedPopupMenuItem(
                          value: library.key,
                          checked: library.key == scope,
                          child: Text(library.value))
                  ],
                  child: Padding(
                      padding: const EdgeInsets.all(12),
                      child: Row(children: [
                        const Icon(Icons.podcasts),
                        const SizedBox(width: 8),
                        Expanded(
                            child: Text(
                                'Library: ${libraries[scope] ?? 'Saved podcasts'}')),
                        const Icon(Icons.arrow_drop_down),
                      ])),
                ),
        );
  }
}
