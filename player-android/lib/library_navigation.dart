import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'app_routes.dart';
import 'providers/audio_handler_provider.dart';
import 'providers/progress_queue_provider.dart';
import 'providers/settings_provider.dart';
import 'providers/api_client_provider.dart';
import 'services/progress_queue.dart';
import 'models/user.dart';

Future<void> switchToLocalLibrary(WidgetRef ref, BuildContext context) async {
  try {
    if (ref.exists(progressQueueProvider)) {
      await ref.read(progressQueueProvider).suspend();
    }
    if (ref.exists(audioHandlerProvider)) {
      await ref.read(audioHandlerProvider).stop();
    }
    await ref
        .read(settingsProvider.notifier)
        .selectDestination(LibraryDestination.local);
    if (context.mounted) context.go(AppRoutes.localLibrary);
  } catch (_) {
    if (context.mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Could not switch libraries.')),
      );
    }
  }
}

Future<void> initializeServerProgressQueue(WidgetRef ref, User user) async {
  if (!ref.read(serverProgressQueueLifecycleProvider)) return;
  try {
    await ref.read(progressQueueProvider).init(
          scope: ProgressScope(
            origin: ref.read(playerBaseUrlProvider).origin,
            userId: user.id,
          ),
        );
  } catch (_) {
    // Authentication has already succeeded. A local DB failure must not
    // strand the user on the login page; future enqueue attempts are retried.
  }
}
