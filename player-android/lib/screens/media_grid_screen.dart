import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

/// Placeholder screen that will display the media items inside a [MediaSet].
///
/// Navigation target reached when the user taps a set card on [SetsListScreen].
/// The [setId] identifies which set to show; the actual media-listing logic
/// will be implemented in a future task.
///
/// Design notes:
///   - [ConsumerWidget] is used so the screen can later watch Riverpod
///     providers for media data without changing the class hierarchy.
///   - [setId] is passed as a constructor parameter (not via global state) so
///     the screen is independently testable and reusable for any set.
class MediaGridScreen extends ConsumerWidget {
  /// The numeric identifier of the set whose media items will be displayed.
  final int setId;

  const MediaGridScreen({super.key, required this.setId});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Scaffold(
      appBar: AppBar(
        title: Text('Set $setId'),
      ),
      body: Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            const Icon(Icons.construction_outlined, size: 56),
            const SizedBox(height: 16),
            Text(
              'TODO: media grid for set $setId',
              key: const Key('media_grid_todo'),
              style: Theme.of(context).textTheme.bodyLarge,
            ),
          ],
        ),
      ),
    );
  }
}
