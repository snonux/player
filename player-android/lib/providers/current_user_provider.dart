import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/player_api_client.dart';
import '../models/models.dart';
import 'api_client_provider.dart';

/// Provides the currently authenticated [User] object.
///
/// Calls [PlayerApiClient.login] is not used here — instead, the logged-in
/// user is fetched lazily via [listUsers] or derived from the auth context.
/// Since the server does not expose a "GET /api/v1/auth/me" endpoint, we
/// obtain the current user by calling [listUsers] and matching against the
/// stored username token.
///
/// The provider is autoDispose so it is released when no screen is watching it,
/// and keepAlive is not used — a fresh fetch is acceptable when navigating back.
///
/// Returns null when the user list cannot be fetched or the username is not
/// found among the registered users (e.g. during a race with logout).
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
    return users.firstWhere(
      (u) => u.username == username,
      orElse: () => User(id: 0, username: username, isAdmin: false),
    );
  } catch (_) {
    // If listUsers fails (e.g. non-admin user, network error) fall back to a
    // minimal user object with isAdmin=false so gating logic fails safely.
    return User(id: 0, username: username, isAdmin: false);
  }
});
