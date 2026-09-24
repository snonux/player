import 'package:cookie_jar/cookie_jar.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/dio_client.dart';
import '../api/dio_player_api_client.dart';
import '../api/player_api_client.dart';
import '../navigation_key.dart';
import 'auth_state_provider.dart';
import 'settings_provider.dart';

/// A synchronous URL for clients, with the compile-time default only while
/// settings are loading. App startup awaits settings before restoring auth.
final playerBaseUrlProvider = Provider<Uri>((ref) {
  final saved = ref.watch(settingsProvider).valueOrNull?.serverBaseUrl;
  return parseServerBaseUrl(saved ?? kPlayerBaseUrl);
});

/// Provides the production [TokenStorage] backed by the OS keychain.
///
/// Riverpod keeps a single instance for the lifetime of [ProviderScope], so
/// there is exactly one [SecureTokenStorage] in the app — consistent with the
/// singleton intent of flutter_secure_storage.
final tokenStorageProvider = Provider<TokenStorage>((ref) {
  return SecureTokenStorage();
});

final credentialMutationQueueProvider =
    Provider<CredentialMutationQueue>((ref) => CredentialMutationQueue());

/// Provides a fully configured [PlayerApiClient] wired with bearer-token
/// injection and global 401 → /login redirect.
///
/// Depends on [tokenStorageProvider] and [navigatorKey] (both singletons) so
/// the same [Dio] instance is reused for every call site — avoiding redundant
/// interceptor stacks.
// Single shared DioClient instance: cached as a Riverpod Provider so both the
// API client and the cookie-jar provider observe the same cookie store.  The
// API client uses Dio; ExoPlayer/video_player/CachedNetworkImage bypass Dio
// and need the cookie jar to attach the session cookie manually.
final _dioClientProvider = Provider<DioClient>((ref) {
  final storage = ref.watch(tokenStorageProvider);
  return DioClient(
    baseUrl: ref.watch(playerBaseUrlProvider),
    storage: storage,
    mutationQueue: ref.watch(credentialMutationQueueProvider),
    // Share the navigator key with go_router so 401 redirects go through the
    // correct router instance rather than the raw Navigator.
    navigatorKey: navigatorKey,
    onUnauthorized: () async {
      await ref
          .read(authStateProvider.notifier)
          .clearAfterUnauthorized(advanceGeneration: false);
    },
    loginRoute: '/login',
  );
});

final apiClientProvider = Provider<PlayerApiClient>((ref) {
  // Use DioPlayerApiClient — the concrete implementation that maps every
  // PlayerApiClient method to a real HTTP call via Dio.  The base class now
  // acts as the public interface (dependency inversion); callers depend on
  // PlayerApiClient, not on this concrete class.
  return DioPlayerApiClient(
    dio: ref.watch(_dioClientProvider).dio,
    credentialEpoch: () => ref.read(credentialMutationQueueProvider).generation,
  );
});

/// Provides the same [CookieJar] backing [apiClientProvider]'s Dio stack so
/// non-Dio HTTP clients (ExoPlayer, video_player, CachedNetworkImage) can
/// authenticate streaming/thumbnail requests using the same session cookie.
final cookieJarProvider = Provider<CookieJar>((ref) {
  return ref.watch(_dioClientProvider).cookieJar;
});
