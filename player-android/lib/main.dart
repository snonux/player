import 'package:audio_service/audio_service.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:just_audio/just_audio.dart';

import 'providers/audio_handler_provider.dart';
import 'router.dart';
import 'services/audio_handler.dart';

// ---------------------------------------------------------------------------
// Entry point
// ---------------------------------------------------------------------------

/// Entry point.
///
/// [AudioService.init] must be called before [runApp] so that the handler is
/// registered before any widget tries to obtain it via [audioHandlerProvider].
///
/// The handler is injected into [ProviderScope] via an override rather than
/// stored in a global variable, keeping the dependency explicit and testable
/// (Dependency Inversion Principle: the provider file does not need to import
/// `main.dart`; the composition root wires the graph).
void main() async {
  // Ensure platform channels are ready before calling AudioService.init.
  WidgetsFlutterBinding.ensureInitialized();

  // Register the PlayerAudioHandler as the singleton media session handler.
  // AudioService.init<T> returns the exact T from the builder.  We pass it
  // directly into ProviderScope so [audioHandlerProvider] is resolved without
  // any global mutable state.
  final handler = await AudioService.init<PlayerAudioHandler>(
    builder: () => PlayerAudioHandler(AudioPlayer()),
    config: const AudioServiceConfig(
      // Notification channel name shown in Android Settings → App info.
      androidNotificationChannelName: 'Player Audio',
      // Keep the service alive while the notification is visible so the OS
      // does not kill the process when the user swipes the app away.
      androidStopForegroundOnPause: false,
      // Allow the user to swipe away the notification to stop playback (UX
      // expectation on Android — mirrors Spotify / Podcast Addict behaviour).
      androidNotificationOngoing: false,
    ),
  );

  runApp(
    ProviderScope(
      // Override the provider with the concrete handler instance so that every
      // widget and provider that reads [audioHandlerProvider] gets the same
      // singleton without going through a global variable.
      overrides: [audioHandlerProvider.overrideWithValue(handler)],
      child: const PlayerAndroidApp(),
    ),
  );
}

// ---------------------------------------------------------------------------
// Root widget
// ---------------------------------------------------------------------------

/// Root application widget.
///
/// Uses [ConsumerWidget] to read [routerProvider] from Riverpod so that the
/// same [GoRouter] instance (and its navigator key) is reused across rebuilds.
/// [MaterialApp.router] delegates all navigation decisions to go_router.
class PlayerAndroidApp extends ConsumerWidget {
  const PlayerAndroidApp({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final router = ref.watch(routerProvider);

    return MaterialApp.router(
      title: 'Player',
      routerConfig: router,
    );
  }
}
