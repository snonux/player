import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../services/cast/cast_controller.dart';
import '../services/cast/cast_device.dart';
import 'api_client_provider.dart';
import 'playback_session_provider.dart';

/// Finds cast devices on the local network.
final castDiscoveryProvider =
    Provider<CastDiscovery>((ref) => PlatformCastDiscovery());

/// The app-wide cast, which outlives the player screen that started it.
final castControllerProvider = ChangeNotifierProvider<CastController>((ref) {
  return CastController(
    coordinator: ref.read(playbackSessionCoordinatorProvider),
    // Read on use: the API client changes when the server or account does.
    resolveMedia: serverCastMediaResolver(() => ref.read(apiClientProvider)),
  );
});
