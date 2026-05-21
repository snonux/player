import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../services/audio_handler.dart';

// ---------------------------------------------------------------------------
// audioHandlerProvider
// ---------------------------------------------------------------------------

/// Provides the [PlayerAudioHandler] singleton that [AudioService.init]
/// registered at app startup.
///
/// This provider has **no default implementation** — it must be overridden at
/// the composition root ([ProviderScope] in [main]) with the instance returned
/// by [AudioService.init].  Attempting to read it without an override throws a
/// [StateError] so misconfiguration is caught immediately.
///
///   - Using a [ProviderScope] override (rather than reading a global mutable
///     variable from `main.dart`) follows the Dependency Inversion Principle:
///     the provider file does not depend on `main.dart`, and the wiring is
///     explicit and visible in the composition root.
///   - Tests can supply a fake handler by overriding this provider in the
///     test's [ProviderScope], without touching the production entry point.
///
/// Usage in widgets:
/// ```dart
/// final handler = ref.read(audioHandlerProvider);
/// await handler.play();
/// ```
///
/// Composition root wiring (see `main.dart`):
/// ```dart
/// final handler = await AudioService.init<PlayerAudioHandler>(...);
/// runApp(ProviderScope(
///   overrides: [audioHandlerProvider.overrideWithValue(handler)],
///   child: const PlayerAndroidApp(),
/// ));
/// ```
final audioHandlerProvider = Provider<PlayerAudioHandler>(
  (_) => throw StateError(
    'audioHandlerProvider has no value: override it with the '
    'PlayerAudioHandler instance from AudioService.init() in ProviderScope.',
  ),
);
