import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../app_routes.dart';
import '../providers/auth_state_provider.dart';
import '../providers/api_client_provider.dart';
import '../providers/continue_listening_provider.dart';
import '../providers/podcast_library_provider.dart';
import '../utils/duration_formatter.dart';

/// A compact, offline-first shortcut; opening a row uses the normal player.
class ContinueListening extends ConsumerStatefulWidget {
  const ContinueListening({super.key, this.deviceOnly = false});
  final bool deviceOnly;

  @override
  ConsumerState<ContinueListening> createState() => _ContinueListeningState();
}

class _ContinueListeningState extends ConsumerState<ContinueListening> {
  bool _expanded = false;
  bool _opening = false;

  @override
  Widget build(BuildContext context) {
    final provider = continueListeningProvider(widget.deviceOnly);
    final items = ref.watch(provider);
    return items.when(
      loading: () => const SizedBox.shrink(),
      error: (_, __) => TextButton.icon(
          onPressed: () => ref.invalidate(provider),
          icon: const Icon(Icons.refresh),
          label: const Text('Retry continue listening')),
      data: (rows) => rows.isEmpty
          ? const SizedBox.shrink()
          : Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Padding(
                  padding:
                      const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
                  child: Text('Continue listening',
                      style: Theme.of(context).textTheme.titleMedium),
                ),
                for (final item in rows.take(_expanded ? rows.length : 3))
                  ListTile(
                    leading: const Icon(Icons.play_circle_outline),
                    title: Text(item.title,
                        maxLines: 2, overflow: TextOverflow.ellipsis),
                    subtitle: Text(
                        '${item.source} · Resume at ${formatDuration(item.position)}',
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis),
                    enabled: !_opening,
                    onTap: () => _open(item),
                  ),
                if (rows.length > 3)
                  TextButton(
                      onPressed: () => setState(() => _expanded = !_expanded),
                      child: Text(_expanded ? 'Show fewer' : 'Show more')),
              ],
            ),
    );
  }

  Future<void> _open(ContinueListeningItem item) async {
    setState(() => _opening = true);
    final accountOwned = item.scope?.startsWith('account:') == true;
    final auth = accountOwned ? ref.read(authStateProvider).valueOrNull : null;
    final origin = accountOwned ? ref.read(playerBaseUrlProvider).origin : null;
    try {
      final episode = item.episode;
      if (episode == null) {
        await context.push(item.path!);
      } else {
        final request = await ref
            .read(podcastDownloadsProvider)
            .playbackRequest(
                item.scope!, episode['feed_key'] as String, episode);
        if (!mounted ||
            accountOwned &&
                (ref.read(authStateProvider).valueOrNull != auth ||
                    ref.read(playerBaseUrlProvider).origin != origin)) {
          return;
        }
        await context.push(AppRoutes.podcastPlayer, extra: request);
      }
      if (mounted) ref.invalidate(continueListeningProvider(widget.deviceOnly));
    } catch (_) {
      if (mounted) {
        ref.invalidate(continueListeningProvider(widget.deviceOnly));
        ScaffoldMessenger.of(context).showSnackBar(const SnackBar(
            content: Text(
                'This saved item is unavailable. Open its library to retry.')));
      }
    } finally {
      if (mounted) setState(() => _opening = false);
    }
  }
}
