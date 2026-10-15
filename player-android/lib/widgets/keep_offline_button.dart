import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../app_routes.dart';
import '../models/media.dart';
import '../models/offline_media.dart';
import '../providers/api_client_provider.dart';
import '../providers/auth_state_provider.dart';
import '../providers/offline_downloads_provider.dart';
import '../services/offline_downloads.dart';

class KeepOfflineButton extends ConsumerWidget {
  const KeepOfflineButton({super.key, required this.media});
  final Media media;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final user = ref.watch(authStateProvider).valueOrNull?.user;
    if (user == null || !{'audio', 'video', 'image'}.contains(media.type)) {
      return const SizedBox.shrink();
    }
    final key = offlineMediaKey(
        ref.watch(playerBaseUrlProvider).origin, user.id, media.id);
    final downloads = ref.watch(offlineDownloadsProvider);
    final saved = ref.watch(offlineMediaProvider(key));
    final job = downloads.active;
    if (job?.key == key) {
      return OutlinedButton.icon(
        onPressed: downloads.cancel,
        icon: const Icon(Icons.cancel_outlined),
        label: Text('Cancel download (${formatOfflineBytes(job!.received)})'),
      );
    }
    if (saved.valueOrNull != null) {
      return OutlinedButton.icon(
        onPressed: () => context.push(AppRoutes.offlineDownloads),
        icon: const Icon(Icons.offline_pin_outlined),
        label: const Text('Available offline · Manage downloads'),
      );
    }
    return OutlinedButton.icon(
      key: const Key('keep_offline'),
      onPressed: job != null || saved.isLoading || saved.hasError
          ? null
          : () async {
              try {
                await keepMediaOffline(ref, media);
                if (context.mounted) {
                  ScaffoldMessenger.of(context).showSnackBar(const SnackBar(
                      content: Text('Saved for offline playback.')));
                }
              } on OfflineDownloadCancelled {
                // Cancellation is deliberate; the button returns to Keep offline.
              } catch (_) {
                if (context.mounted) {
                  ScaffoldMessenger.of(context).showSnackBar(SnackBar(
                      content: Text(downloads.error ??
                          'Could not save this file. Please try again.')));
                }
              }
            },
      icon: const Icon(Icons.download_outlined),
      label: const Text('Keep offline'),
    );
  }
}

String formatOfflineBytes(int bytes) {
  if (bytes >= 1073741824) {
    return '${(bytes / 1073741824).toStringAsFixed(1)} GB';
  }
  if (bytes >= 1048576) return '${(bytes / 1048576).toStringAsFixed(1)} MB';
  return '${(bytes / 1024).toStringAsFixed(0)} KB';
}
