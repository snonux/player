import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../models/user.dart';
import 'api_client_provider.dart';

/// All possible authentication states for the app.
///
/// Using a sealed-like enum keeps the router redirect logic exhaustive and
/// avoids stringly-typed checks throughout the codebase.
enum AuthStatus {
  /// Initial state while the app checks for a saved session marker.
  loading,

  /// A prior session was marked present; the user is logged in.
  authenticated,

  /// No session is marked present (e.g. after logout).
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
/// [AsyncNotifier] is used because the initial state check is async (it reads
/// the session marker). Downstream consumers can call [login] and
/// [logout] to drive route redirects via the router's [refreshListenable].
// Legacy session marker. Cookie persistence is not yet implemented, so a
// marker cannot authenticate a fresh process and is cleared on cold start.
const _kAuthSessionPresentKey = 'auth_session_present';
const _kAuthUserKey = 'auth_user';

class AuthStateNotifier extends AsyncNotifier<AuthState> {
  @override
  Future<AuthState> build() async {
    // Dio's CookieJar is currently in memory. A marker from an earlier process
    // cannot prove that a session credential survived. Clear legacy markers
    // so the user can sign in again; real restoration belongs to task bh2.
    final prefs = await SharedPreferences.getInstance();
    await prefs.remove(_kAuthSessionPresentKey);
    await prefs.remove(_kAuthUserKey);
    await ref.read(tokenStorageProvider).deleteToken();
    return const AuthState.unauthenticated();
  }

  /// Retains the server's authenticated user separately from credentials.
  Future<void> login(User user) async {
    await future;
    final prefs = await SharedPreferences.getInstance();
    await prefs.setBool(_kAuthSessionPresentKey, true);
    state = AsyncData(AuthState.authenticated(user: user));
  }

  /// Called on explicit logout. Clears the legacy session marker and identity.
  Future<void> logout() async {
    await future;
    final prefs = await SharedPreferences.getInstance();
    await prefs.remove(_kAuthSessionPresentKey);
    await prefs.remove(_kAuthUserKey);
    state = const AsyncData(AuthState.unauthenticated());
  }
}

/// The single source of truth for authentication status, consumed by the
/// router's redirect callback and any widget that needs to gate on auth.
final authStateProvider = AsyncNotifierProvider<AuthStateNotifier, AuthState>(
  AuthStateNotifier.new,
);
