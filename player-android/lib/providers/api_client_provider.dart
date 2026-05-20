import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/dio_client.dart';
import '../api/dio_player_api_client.dart';
import '../api/player_api_client.dart';
import '../navigation_key.dart';

/// Base URL for the player-server API.
///
/// In production this is injected from the environment or a config file.
/// The default points to a local dev instance so the app is runnable
/// without extra configuration.
const _kBaseUrl = String.fromEnvironment(
  'PLAYER_BASE_URL',
  defaultValue: 'http://10.0.2.2:8080',
);

/// Provides the production [TokenStorage] backed by the OS keychain.
///
/// Riverpod keeps a single instance for the lifetime of [ProviderScope], so
/// there is exactly one [SecureTokenStorage] in the app — consistent with the
/// singleton intent of flutter_secure_storage.
final tokenStorageProvider = Provider<TokenStorage>((ref) {
  return SecureTokenStorage();
});

/// Provides a fully configured [PlayerApiClient] wired with bearer-token
/// injection and global 401 → /login redirect.
///
/// Depends on [tokenStorageProvider] and [navigatorKey] (both singletons) so
/// the same [Dio] instance is reused for every call site — avoiding redundant
/// interceptor stacks.
final apiClientProvider = Provider<PlayerApiClient>((ref) {
  final storage = ref.watch(tokenStorageProvider);

  final dioClient = DioClient(
    baseUrl: Uri.parse(_kBaseUrl),
    storage: storage,
    // Share the navigator key with go_router so 401 redirects go through the
    // correct router instance rather than the raw Navigator.
    navigatorKey: navigatorKey,
    loginRoute: '/login',
  );

  // Use DioPlayerApiClient — the concrete implementation that maps every
  // PlayerApiClient method to a real HTTP call via Dio.  The base class now
  // acts as the public interface (dependency inversion); callers depend on
  // PlayerApiClient, not on this concrete class.
  return DioPlayerApiClient(dio: dioClient.dio);
});
