import 'dart:async';
import 'package:connectivity_plus/connectivity_plus.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../services/server_endpoint_router.dart';
import 'settings_provider.dart';

const _wifiChannel = MethodChannel('zone.foo.player_android/server_wifi');
Future<String?> currentServerWifiName({bool requestPermission = false}) async {
  try {
    return await _wifiChannel.invokeMethod<String>(
        'wifiName', {'requestPermission': requestPermission});
  } on PlatformException {
    return null;
  } on MissingPluginException {
    return null;
  }
}

final serverEndpointStatusProvider =
    StateProvider<String>((ref) => 'Using the primary server address.');
final serverEndpointRouterProvider = Provider<ServerEndpointRouter>((ref) {
  final origin =
      ref.watch(settingsProvider.select((s) => s.valueOrNull?.serverBaseUrl));
  final settings = ref.read(settingsProvider).valueOrNull;
  final primary = parseServerBaseUrl(origin ?? kPlayerBaseUrl);
  var disposed = false;
  final router = ServerEndpointRouter(
      primary: primary,
      profile: settings?.endpointProfiles[primary.origin],
      wifiName: currentServerWifiName,
      onStatus: (status) {
        if (!disposed) {
          ref.read(serverEndpointStatusProvider.notifier).state = status;
        }
      });
  ref.listen(settingsProvider, (_, next) {
    if (next.valueOrNull?.serverBaseUrl == primary.origin) {
      router.updateProfile(next.valueOrNull?.endpointProfiles[primary.origin]);
    }
  });
  final subscription =
      Connectivity().onConnectivityChanged.listen((_) => router.invalidate());
  ref.onDispose(() {
    disposed = true;
    unawaited(subscription.cancel());
    unawaited(router.close());
  });
  return router;
});
final routedServerUriProvider =
    FutureProvider.autoDispose.family<Uri, Uri>((ref, uri) {
  ref.watch(settingsProvider.select((s) => s.valueOrNull?.endpointProfiles));
  return ref.watch(serverEndpointRouterProvider).mediaUri(uri);
});
