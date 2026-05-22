import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/dio_client.dart';
import '../api/dio_player_api_client.dart';
import '../api/player_api_client.dart';
import '../navigation_key.dart';

/// Base URL for the player-server API, resolved at compile time via
/// the PLAYER_BASE_URL environment variable (or the default below).
///
/// Declared as a package-level identifier (no underscore) so it can be
/// shared by [publicApiClientProvider] in [public_api_client_provider.dart].
/// The value is set once at compile time and never changes at runtime,
/// making it safe to share across providers.
///
/// In production this is injected via `--dart-define=PLAYER_BASE_URL=...`.
/// The default points to the Android emulator host loopback address so the
/// app is runnable out-of-the-box without extra configuration.
const kPlayerBaseUrl = String.fromEnvironment(
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
    baseUrl: Uri.parse(kPlayerBaseUrl),
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
