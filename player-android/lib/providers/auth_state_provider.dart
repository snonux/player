import 'dart:convert';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../models/user.dart';
import 'api_client_provider.dart';

/// All possible authentication states for the app.
///
/// Using a sealed-like enum keeps the router redirect logic exhaustive and
/// avoids stringly-typed checks throughout the codebase.
enum AuthStatus {
  /// Initial state while the app checks the saved credential.
  loading,

  /// A valid local credential and identity were restored or just created.
  authenticated,

  /// No usable credential is present (e.g. after logout).
  unauthenticated,
}

/// Immutable snapshot of the auth state passed through the provider graph.
///
/// Keeping this as a value object (rather than a mutable notifier field)
/// makes it safe to pass into go_router's redirect callback and to compare
/// with `==` in tests.
class AuthState {
  const AuthState({required this.status, this.user});

  final AuthStatus status;
  final User? user;

  /// Convenience constructors reduce noise at call sites.
  const AuthState.loading()
      : status = AuthStatus.loading,
        user = null;
  const AuthState.authenticated({this.user})
      : status = AuthStatus.authenticated;
  const AuthState.unauthenticated()
      : status = AuthStatus.unauthenticated,
        user = null;

  bool get isLoading => status == AuthStatus.loading;
  bool get isAuthenticated => status == AuthStatus.authenticated;
  bool get isUnauthenticated => status == AuthStatus.unauthenticated;

  @override
  String toString() => 'AuthState(${status.name})';

  @override
  bool operator ==(Object other) =>
      identical(this, other) ||
      other is AuthState &&
          runtimeType == other.runtimeType &&
          status == other.status &&
          user?.id == other.user?.id &&
          user?.username == other.user?.username &&
          user?.isAdmin == other.user?.isAdmin;

  @override
  int get hashCode =>
      Object.hash(status, user?.id, user?.username, user?.isAdmin);
}

/// Notifier that owns the mutable [AuthState] and exposes mutation methods
/// for login / logout.
///
/// [AsyncNotifier] is used because the initial state check is async. Downstream
/// consumers can call [login] and
/// [logout] to drive route redirects via the router's [refreshListenable].
const _kAuthSessionPresentKey = 'auth_session_present';
const _kAuthUserKey = 'auth_user';
const _kAuthOriginKey = 'auth_origin';
const _kAuthExpiresKey = 'auth_expires_at';

class AuthStateNotifier extends AsyncNotifier<AuthState> {
  @override
  Future<AuthState> build() async {
    final prefs = await SharedPreferences.getInstance();
    final storage = ref.read(tokenStorageProvider);
    final token = await storage.readToken();
    final origin = ref.read(playerBaseUrlProvider).origin;
    final expiresAt = prefs.getInt(_kAuthExpiresKey);
    final userJson = prefs.getString(_kAuthUserKey);
    if (token != null &&
        token.isNotEmpty &&
        prefs.getBool(_kAuthSessionPresentKey) == true &&
        prefs.getString(_kAuthOriginKey) == origin &&
        expiresAt != null &&
        DateTime.now().millisecondsSinceEpoch < expiresAt &&
        userJson != null) {
      try {
        final user =
            User.fromJson(jsonDecode(userJson) as Map<String, dynamic>);
        if (user.id > 0 && user.username.isNotEmpty) {
          return AuthState.authenticated(user: user);
        }
      } on FormatException {
        // Corrupt identity is treated like a missing credential.
      } on TypeError {
        // Malformed JSON types must never grant authenticated UI access.
      }
    }
    await _clearCredentials(prefs);
    return const AuthState.unauthenticated();
  }

  /// Retains the server's authenticated user separately from credentials.
  Future<void> login(User user) async {
    await future;
    final prefs = await SharedPreferences.getInstance();
    final queue = ref.read(credentialMutationQueueProvider);
    await queue.run(() => _clearCredentials(prefs));
    try {
      // The login/bootstrap response sets a session cookie. Mint a dedicated
      // mobile token while that cookie is available in the shared Dio client.
      final result = await ref.read(apiClientProvider).createAPIToken(
            name: 'android-client',
            expiresInDays: 365,
          );
      final token = result['token'];
      if (token is! String || token.isEmpty) {
        throw const FormatException('Token creation returned no credential');
      }
      await queue.run(() async {
        await ref.read(tokenStorageProvider).writeToken(token);
        await prefs.setString(_kAuthUserKey, jsonEncode(user.toJson()));
        await prefs.setString(
            _kAuthOriginKey, ref.read(playerBaseUrlProvider).origin);
        // The server starts the 365-day clock before sending its response.
        // Expire locally one day early to avoid presenting an expired token.
        await prefs.setInt(
            _kAuthExpiresKey,
            DateTime.now()
                .add(const Duration(days: 364))
                .millisecondsSinceEpoch);
        await prefs.setBool(_kAuthSessionPresentKey, true);
        state = AsyncData(AuthState.authenticated(user: user));
      });
    } catch (_) {
      await queue.run(() async {
        await _clearCredentials(prefs);
        await ref.read(cookieJarProvider).deleteAll();
        state = const AsyncData(AuthState.unauthenticated());
      });
      rethrow;
    }
  }

  /// Called on explicit logout or an invalid protected request.
  Future<void> logout() async {
    await future;
    await ref.read(credentialMutationQueueProvider).run(clearAfterUnauthorized);
  }

  /// Called only while the shared credential queue is held by a 401 handler.
  Future<void> clearAfterUnauthorized() async {
    final prefs = await SharedPreferences.getInstance();
    await _clearCredentials(prefs);
    await ref.read(cookieJarProvider).deleteAll();
    state = const AsyncData(AuthState.unauthenticated());
  }

  Future<void> _clearCredentials(SharedPreferences prefs) async {
    await prefs.remove(_kAuthSessionPresentKey);
    await prefs.remove(_kAuthUserKey);
    await prefs.remove(_kAuthOriginKey);
    await prefs.remove(_kAuthExpiresKey);
    await ref.read(tokenStorageProvider).deleteToken();
  }
}

/// The single source of truth for authentication status, consumed by the
/// router's redirect callback and any widget that needs to gate on auth.
final authStateProvider = AsyncNotifierProvider<AuthStateNotifier, AuthState>(
  AuthStateNotifier.new,
);
