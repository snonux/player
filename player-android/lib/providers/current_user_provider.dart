import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/player_api_client.dart';
import '../models/models.dart';
import 'api_client_provider.dart';

/// Provides the currently authenticated [User] object.
///
/// The [PlayerApiClient.login] call is not used here — instead, the logged-in
/// user is fetched lazily via [listUsers] and matched against the stored
/// username token.  Since the server does not expose a "GET /api/v1/auth/me"
/// endpoint, this round-trip is the only way to obtain the [User.isAdmin] flag.
///
/// The provider is autoDispose so it is released when no screen is watching it,
/// and keepAlive is not used — a fresh fetch is acceptable when navigating back.
///
/// Returns null when the user list cannot be fetched, or when the username is
/// not found among the registered users (e.g. during a race with logout).
final currentUserProvider = FutureProvider.autoDispose<User?>((ref) async {
  // Obtain the stored username from token storage (same source as the settings
  // screen's _currentUsernameProvider) to identify which user is logged in.
  final storage = ref.watch(tokenStorageProvider);
  final username = await storage.readToken();
  if (username == null || username.isEmpty) return null;

  // Fetch the full user list to resolve the user object for the stored username.
  // This is the only way to get the User with isAdmin flag since there is no
  // dedicated /api/v1/auth/me endpoint.  Admin-only screens already require
  // admin status so this round-trip is acceptable at navigation time.
  final client = ref.watch(apiClientProvider);
  try {
    final users = await client.listUsers();
    // Cast to User? so orElse can return null when the username is not in the
    // list (e.g. the account was deleted).  null is the safest sentinel because
    // it does not collide with the id=0 placeholder used in AdminUsersScreen.
    return users.cast<User?>().firstWhere(
      (u) => u?.username == username,
      orElse: () => null,
    );
  } catch (_) {
    // If listUsers fails (e.g. non-admin user, network error) return null so
    // callers that gate on isAdmin fail safely without a fake User(id:0) object
    // that could collide with the optimistic placeholder sentinel in
    // AdminUsersScreen.
    return null;
  }
});
