import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../services/cast/cast_controller.dart';
import '../services/cast/cast_device.dart';
import '../services/cast/cast_session.dart';
import '../services/cast/renderer_session.dart';
import 'api_client_provider.dart';
import 'audio_handler_provider.dart';
import 'playback_session_provider.dart';

/// Finds cast devices on the local network.
final castDiscoveryProvider = Provider<CastDiscovery>((ref) =>
    RendererDiscovery(
        () => ref.read(apiClientProvider), PlatformCastDiscovery()));

/// The app-wide cast, which outlives the player screen that started it.
final castControllerProvider = ChangeNotifierProvider<CastController>((ref) {
  final timer = ref.read(audioHandlerProvider).sleepTimer;
  final controller = CastController(
    sleepTimer: timer,
    startSession: (device) async => device.isRenderer
        ? RendererSession(device, ref.read(apiClientProvider))
        : await CastSession.start(device),
    coordinator: ref.read(playbackSessionCoordinatorProvider),
    // Read on use: the API client changes when the server or account does.
    resolveMedia: serverCastMediaResolver(() => ref.read(apiClientProvider)),
  );
  ref.read(audioHandlerProvider).pauseSleepRemote = controller.pause;
  return controller;
});
