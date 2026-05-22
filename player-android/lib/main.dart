import 'package:audio_service/audio_service.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:just_audio/just_audio.dart';

import 'providers/audio_handler_provider.dart';
import 'providers/progress_queue_provider.dart';
import 'providers/theme_provider.dart';
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

  // Create the ProviderScope first so we can read providers before runApp.
  // The scope is then passed to PlayerAndroidApp so it is the single root.
  final container = ProviderContainer(
    overrides: [audioHandlerProvider.overrideWithValue(handler)],
  );

  // Initialise the offline progress queue (opens SQLite DB, subscribes to
  // connectivity).  Must be done before any player screen opens so that the
  // queue is ready to accept enqueue calls immediately.
  await container.read(progressQueueProvider).init();

  runApp(
    UncontrolledProviderScope(
      container: container,
      child: const PlayerAndroidApp(),
    ),
  );
}

// ---------------------------------------------------------------------------
// Module-level theme constants
// ---------------------------------------------------------------------------

// Built once at startup rather than on every rebuild of PlayerAndroidApp.
// ThemeData construction is not cheap, and the colour tokens never change
// at runtime — only the active ThemeMode (light/dark/system) does.
final _lightTheme = buildLightTheme();
final _darkTheme = buildDarkTheme();

// ---------------------------------------------------------------------------
// Root widget
// ---------------------------------------------------------------------------

/// Root application widget.
///
/// Uses [ConsumerWidget] to read [routerProvider] and [themeProvider] from
/// Riverpod so that the same [GoRouter] instance and the persisted [ThemeMode]
/// are both available without additional state management in the widget itself.
///
/// [MaterialApp.router] delegates all navigation decisions to go_router.
/// [themeMode] is driven by [themeProvider] so the user's light/dark preference
/// takes effect immediately on every screen and survives app restarts.
class PlayerAndroidApp extends ConsumerWidget {
  const PlayerAndroidApp({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final router = ref.watch(routerProvider);

    // Default to ThemeMode.system while the preference is loading so the app
    // does not flash an incorrect theme during startup.
    final themeMode =
        ref.watch(themeProvider).valueOrNull ?? ThemeMode.system;

    return MaterialApp.router(
      title: 'Player',
      routerConfig: router,
      // Material 3 is enabled in both ThemeData instances; see theme_provider.dart.
      theme: _lightTheme,
      darkTheme: _darkTheme,
      themeMode: themeMode,
    );
  }
}
