import 'package:flutter_localizations/flutter_localizations.dart';
import 'providers/language_provider.dart';
import 'l10n/music_localizations.dart';
import 'services/playback_request.dart';
import 'services/stream_quality.dart';
import 'services/headset_preferences.dart';
import 'providers/headset_preferences_provider.dart';
import 'providers/android_auto_provider.dart';
import 'services/playback_widget_observer.dart';
import 'package:audio_service/audio_service.dart';
import 'package:audio_session/audio_session.dart';
import 'package:flutter/material.dart';
import 'providers/music_automation_provider.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:just_audio/just_audio.dart';

import 'providers/audio_handler_provider.dart';
import 'providers/media_artwork_provider.dart';
import 'providers/auth_state_provider.dart';
import 'providers/progress_queue_provider.dart';
import 'providers/global_sync_provider.dart';
import 'providers/api_client_provider.dart';
import 'providers/settings_provider.dart';
import 'providers/svg_image_provider.dart';
import 'providers/theme_provider.dart';
import 'providers/presentation_provider.dart';
import 'widgets/playback_screen_awake.dart';
import 'navigation_key.dart';
import 'router.dart';
import 'services/audio_handler.dart';
import 'services/loudness_effect.dart';
import 'services/audio_progress_lifecycle.dart';
import 'services/progress_queue.dart';
import 'widgets/keyboard_access.dart';

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
  await StreamQuality.initialize();

  // Register the PlayerAudioHandler as the singleton media session handler.
  // AudioService.init<T> returns the exact T from the builder.  We pass it
  // directly into ProviderScope so [audioHandlerProvider] is resolved without
  // any global mutable state.
  // HeadsetPolicy handles noisy and focus events together. Session activation
  // remains with just_audio so denied audio focus cannot start playback.
  final headphonePreferences = HeadsetPreferences();
  await headphonePreferences.restore();
  final audioPlayer = AudioPlayer(handleInterruptions: false);
  final loudnessEnhancer = SessionLoudnessEffect(audioPlayer);
  final handler = await AudioService.init<PlayerAudioHandler>(
    builder: () => PlayerAudioHandler(audioPlayer,
        loudnessEnhancer: loudnessEnhancer,
        headsetPreferences: headphonePreferences),
    config: const AudioServiceConfig(
      // Notification channel name shown in Android Settings → App info.
      androidNotificationChannelName: 'Player Audio',
      // Required by audio_service: Android refuses to post a notification
      // without a small-icon drawable.  Re-use the launcher icon — it is a
      // resource the project already ships and AudioService accepts any
      // drawable here.  Without this the foreground service fails with
      // "Invalid notification (no valid small icon)" and crashes the app
      // when AudioPlayer.play() triggers media-session metadata.
      androidNotificationIcon: 'drawable/ic_launcher',
      // Keep the service alive while the notification is visible so the OS
      // does not kill the process when the user swipes the app away.
      androidStopForegroundOnPause: false,
      // Allow the user to swipe away the notification to stop playback (UX
      // expectation on Android — mirrors Spotify / Podcast Addict behaviour).
      androidNotificationOngoing: false,
    ),
  );

  await handler.loudness.restore();
  handler.headset.attach(await AudioSession.instance);

  // Flush the active source at lifecycle boundaries while audio keeps playing.
  WidgetsBinding.instance.addObserver(
    AudioProgressLifecycle(handler.saveProgress),
  );

  // Create the ProviderScope first so we can read providers before runApp.
  // The scope is then passed to PlayerAndroidApp so it is the single root.
  final container = ProviderContainer(
    overrides: [
      audioHandlerProvider.overrideWithValue(handler),
      headsetPreferencesProvider.overrideWithValue(headphonePreferences),
      serverProgressQueueLifecycleProvider.overrideWithValue(true),
    ],
  );

  // Resolve local/server selection before the router is created. A fresh local
  // install never restores credentials or creates network services here.
  await container.read(settingsProvider.future);
  if (container.read(settingsProvider).requireValue.destination ==
      LibraryDestination.server) {
    final auth = await container.read(authStateProvider.future);
    if (auth.isAuthenticated) {
      await container.read(progressQueueProvider).init(
            scope: ProgressScope(
              origin: container.read(playerBaseUrlProvider).origin,
              userId: auth.user!.id,
            ),
          );
    }
  }

  final playbackWidgets = PlaybackWidgetObserver(handler, allowMetadata: () {
    return playbackWidgetMetadataAllowed(handler.currentMetadataRequest,
        origin: container.read(playerBaseUrlProvider).origin,
        userId: container.read(authStateProvider).valueOrNull?.user?.id,
        credentialsEnabled:
            container.read(credentialMutationQueueProvider).credentialsEnabled);
  });
  // Auth and selected-server changes explicitly invalidate launcher identity,
  // even when no subsequent audio event arrives. Local playback stays usable.
  container.listen(authStateProvider, (_, __) => playbackWidgets.refresh());
  container.listen(playerBaseUrlProvider, (_, __) => playbackWidgets.refresh());

  AudioService.notificationClicked.where((clicked) => clicked).listen((_) {
    final request = handler.currentMetadataRequest;
    final auth = container.read(authStateProvider).valueOrNull;
    if (request is! LiveRadioPlaybackRequest ||
        auth?.isAuthenticated != true ||
        auth?.user?.id != request.userId ||
        container.read(playerBaseUrlProvider).origin != request.serverOrigin ||
        !container.read(credentialMutationQueueProvider).credentialsEnabled) {
      return;
    }
    final router = container.read(routerProvider);
    if (router.routeInformationProvider.value.uri.path != AppRoutes.radio) {
      router.push(AppRoutes.radio);
    }
  });

  handler.artworkLoader = container.read(mediaArtworkLoaderProvider);
  handler.autoCatalog = container.read(androidAutoCatalogProvider);
  container.read(musicAutomationProvider);
  handler.refreshAutoChildren();
  container.read(globalSyncCoordinatorProvider);

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
// Cache both brightness variants per accent so changing appearance does not
// repeatedly build ThemeData or recreate the router/audio handler.
final _lightThemes = {
  for (final accent in PlayerAccent.values) accent: buildLightTheme(accent)
};
final _darkThemes = {
  for (final accent in PlayerAccent.values) accent: buildDarkTheme(accent)
};

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
    // Keeps the listeners alive that empty the SVG cache on logout and on a
    // server change.
    ref.watch(svgCacheResetProvider);

    // Default to ThemeMode.system while the preference is loading so the app
    // does not flash an incorrect theme during startup.
    final themeMode = ref.watch(themeProvider).valueOrNull ?? ThemeMode.system;

    final accent = ref.watch(presentationProvider).valueOrNull?.accent ??
        PlayerAccent.blue;
    return MaterialApp.router(
      scaffoldMessengerKey: appMessengerKey,
      title: 'Player',
      locale: ref.watch(languageProvider).valueOrNull?.choice == 'system' ||
              ref.watch(languageProvider).valueOrNull == null
          ? null
          : Locale(ref.watch(languageProvider).valueOrNull!.choice),
      supportedLocales: MusicLocalizations.supportedLocales,
      localizationsDelegates: const [
        MusicLocalizations.delegate,
        GlobalMaterialLocalizations.delegate,
        GlobalWidgetsLocalizations.delegate,
        GlobalCupertinoLocalizations.delegate,
      ],
      routerConfig: router,
      // Material 3 is enabled in both ThemeData instances; see theme_provider.dart.
      theme: _lightThemes[accent],
      darkTheme: _darkThemes[accent],
      themeMode: themeMode,
      builder: (context, child) => PlaybackScreenAwake(
          child: KeyboardAccess(child: child ?? const SizedBox.shrink())),
    );
  }
}
