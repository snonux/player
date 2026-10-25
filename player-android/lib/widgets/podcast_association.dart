import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../navigation_key.dart';
import '../providers/podcast_association_provider.dart';
import '../providers/podcast_library_provider.dart';

Future<void> showPodcastAssociation(BuildContext context, WidgetRef ref,
    PodcastAssociationAccount account) async {
  final controller = ref.read(podcastAssociationControllerProvider);
  final marked = await controller.markPrompted(account);
  if (!context.mounted || !ref.context.mounted || !account.current()) {
    return;
  }
  if (!marked) throw StateError('Could not save podcast consent prompt');
  final sync = await showDialog<bool>(
      context: context,
      builder: (context) => Consumer(builder: (context, ref, _) {
            ref.watch(podcastAssociationAccountProvider);
            final current = account.current();
            return AlertDialog(
              title: Text(current
                  ? "Sync this device's podcasts with ${account.label}?"
                  : 'Account changed'),
              content: Text(current
                  ? 'Copy subscriptions and listening history to this account. '
                      'Your separate device library and downloaded files stay on this device.'
                  : 'No podcasts were copied. Open Settings to sync with the selected account.'),
              actions: [
                TextButton(
                    onPressed: () => Navigator.pop(context, false),
                    child: Text(current ? 'Keep separate' : 'Close')),
                if (current)
                  FilledButton(
                      onPressed: () => Navigator.pop(context, true),
                      child: const Text('Sync')),
              ],
            );
          }));
  if (sync != true || !account.current() || !ref.context.mounted) return;
  try {
    await controller.associate(account);
    if (context.mounted && ref.context.mounted) {
      ref.invalidate(podcastSubscriptionsProvider);
    }
  } catch (_) {
    if (context.mounted && ref.context.mounted) {
      ScaffoldMessenger.maybeOf(context)?.showSnackBar(const SnackBar(
          content: Text('Podcasts were not copied. Try again from Settings.')));
    }
  }
}

/// Runs after sign-in without depending on the route currently being visited.
class PodcastAssociationHost extends ConsumerStatefulWidget {
  const PodcastAssociationHost({super.key, required this.child});
  final Widget child;
  @override
  ConsumerState<PodcastAssociationHost> createState() =>
      _PodcastAssociationHostState();
}

class _PodcastAssociationHostState
    extends ConsumerState<PodcastAssociationHost> {
  bool _showing = false;
  final _shown = <String>{};
  @override
  Widget build(BuildContext context) {
    final offer = ref.watch(podcastAssociationOfferProvider).valueOrNull;
    if (offer != null && !_showing && !_shown.contains(offer.scope)) {
      _showing = true;
      _shown.add(offer.scope);
      WidgetsBinding.instance.addPostFrameCallback((_) async {
        final target = navigatorKey.currentContext;
        try {
          if (mounted && target != null && offer.current()) {
            await showPodcastAssociation(target, ref, offer);
          }
        } catch (_) {
          // Optional automatic consent stays quiet on storage/navigation failure.
          // This scope can be retried explicitly from Settings.
        } finally {
          if (mounted) {
            _showing = false;
            if (target != null || !offer.current()) {
              ref.invalidate(podcastAssociationOfferProvider);
            }
          }
        }
      });
    }
    return widget.child;
  }
}

class PodcastAssociationSettings extends ConsumerStatefulWidget {
  const PodcastAssociationSettings({super.key});
  @override
  ConsumerState<PodcastAssociationSettings> createState() =>
      _PodcastAssociationSettingsState();
}

class _PodcastAssociationSettingsState
    extends ConsumerState<PodcastAssociationSettings> {
  bool _busy = false;
  @override
  Widget build(BuildContext context) {
    final account = ref.watch(podcastAssociationAccountProvider);
    if (account == null) return const SizedBox.shrink();
    return ListTile(
      leading: const Icon(Icons.podcasts),
      title: const Text("Sync this device's podcasts"),
      subtitle: Text(account.label),
      enabled: !_busy,
      onTap: () async {
        setState(() => _busy = true);
        try {
          final offer = await ref
              .read(podcastAssociationControllerProvider)
              .offer(account, repeat: true);
          if (!context.mounted) return;
          if (offer != null) {
            await showPodcastAssociation(context, ref, offer);
          } else if (account.current()) {
            ScaffoldMessenger.of(context).showSnackBar(const SnackBar(
                content: Text(
                    'No separate device podcasts are ready to sync. Check the server connection and try again.')));
          }
        } catch (_) {
          if (context.mounted) {
            ScaffoldMessenger.of(context).showSnackBar(const SnackBar(
                content: Text('Could not prepare podcast sync. Try again.')));
          }
        } finally {
          if (mounted) {
            setState(() => _busy = false);
            ref.invalidate(podcastAssociationOfferProvider);
          }
        }
      },
    );
  }
}
