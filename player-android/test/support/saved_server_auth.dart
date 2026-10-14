import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:player_android/providers/auth_state_provider.dart';
import 'package:player_android/providers/settings_provider.dart';
import 'package:player_android/services/saved_server_credentials.dart';

class AllowServerUnlock implements ServerBiometricUnlock {
  @override
  Future<bool> authenticate(String origin) async => true;
}

Future<void> unlockSelectedServer(ProviderContainer container) async {
  await container
      .read(settingsProvider.notifier)
      .selectDestination(LibraryDestination.server);
  await container.read(authStateProvider.notifier).unlockSavedServer();
}
