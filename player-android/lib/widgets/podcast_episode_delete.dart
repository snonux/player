import 'package:flutter/material.dart';

/// Asks before deleting one podcast episode. The episode row action, the
/// list's right-to-left swipe and the podcast player share this dialog.
Future<bool> confirmEpisodeDelete(BuildContext context) async =>
    await showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
              title: const Text('Delete episode?'),
              content: const Text(
                  'Remove this episode from your podcast lists and delete its download on this device. Listening history is kept.'),
              actions: [
                TextButton(
                    onPressed: () => Navigator.pop(context, false),
                    child: const Text('Cancel')),
                TextButton(
                    onPressed: () => Navigator.pop(context, true),
                    child: const Text('Delete episode')),
              ],
            )) ==
    true;

/// Swiping a list row from right to left (end to start) deletes the episode after the
/// same confirmation. The row stays in place; the list reloads without it.
class EpisodeSwipeToDelete extends StatelessWidget {
  const EpisodeSwipeToDelete(
      {super.key,
      required this.episode,
      required this.onDelete,
      required this.child});

  /// The row's identity, so the swipe state follows its episode.
  final Object episode;

  /// Confirms and deletes; the swipe itself never removes the row.
  final Future<void> Function() onDelete;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    return Dismissible(
      key: ValueKey(('episode_swipe_delete', episode)),
      direction: DismissDirection.endToStart,
      background: ColoredBox(
        color: colors.errorContainer,
        child: Align(
          alignment: AlignmentDirectional.centerEnd,
          child: Padding(
            padding: const EdgeInsetsDirectional.only(end: 24),
            child: Row(mainAxisSize: MainAxisSize.min, children: [
              Text('Delete episode',
                  style: TextStyle(color: colors.onErrorContainer)),
              const SizedBox(width: 8),
              Icon(Icons.delete_outline, color: colors.onErrorContainer),
            ]),
          ),
        ),
      ),
      confirmDismiss: (_) async {
        await onDelete();
        return false;
      },
      child: child,
    );
  }
}
