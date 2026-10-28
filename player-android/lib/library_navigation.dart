import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'app_routes.dart';
import 'providers/audio_handler_provider.dart';
import 'providers/auth_state_provider.dart';
import 'providers/playback_session_provider.dart';
import 'providers/progress_queue_provider.dart';
import 'providers/settings_provider.dart';
import 'providers/api_client_provider.dart';
import 'services/progress_queue.dart';
import 'models/user.dart';
import 'providers/music_queue_persistence_provider.dart';

Future<void> switchToLocalLibrary(WidgetRef ref, BuildContext context) async {
  try {
    final playback = ref.read(playbackSessionCoordinatorProvider);
    final hadOwner = playback.activeKind != null;
    await playback.stopCurrent();
    if (ref.exists(audioHandlerProvider)) {
      ref.read(audioHandlerProvider).clearRetainedQueue();
    }
    if (!hadOwner && ref.exists(audioHandlerProvider)) {
      // Compatibility for an audio session created before ownership tracking.
      await ref.read(audioHandlerProvider).stop();
    }
    if (ref.exists(progressQueueProvider)) {
      await ref.read(progressQueueProvider).suspend();
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

/// Reads a provider from either a [WidgetRef] or a [ProviderContainer].
typedef ProviderReader = T Function<T>(ProviderListenable<T> provider);

Future<void> initializeServerProgressQueue(WidgetRef ref, User user) =>
    initializeServerProgressQueueWith(ref.read, user);

/// Variant for callers that may outlive their widget, such as the quick
/// server switch, which keeps working after the router replaces its screen.
Future<void> initializeServerProgressQueueWith(
    ProviderReader read, User user) async {
  final offline = read(authStateProvider).valueOrNull?.isOffline == true;
  if (!offline && read(serverProgressQueueLifecycleProvider)) {
    try {
      await read(musicQueuePersistenceProvider).initialize(user.id);
    } catch (_) {
      read(audioHandlerProvider).queueStorageFailed();
    }
  }
  if (!read(serverProgressQueueLifecycleProvider)) return;
  try {
    await read(progressQueueProvider).init(
      scope: ProgressScope(
        origin: read(playerBaseUrlProvider).origin,
        userId: user.id,
      ),
    );
  } catch (_) {
    // Authentication has already succeeded. A local DB failure must not
    // strand the user on the login page; future enqueue attempts are retried.
  }
}
