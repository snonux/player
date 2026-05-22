import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// All possible authentication states for the app.
///
/// Using a sealed-like enum keeps the router redirect logic exhaustive and
/// avoids stringly-typed checks throughout the codebase.
enum AuthStatus {
  /// Initial state while the app checks whether a stored token exists.
  loading,

  /// A valid token was found in secure storage; the user is logged in.
  authenticated,

  /// No token exists or it has been purged (e.g. after a 401 response).
  unauthenticated,
}

/// Immutable snapshot of the auth state passed through the provider graph.
///
/// Keeping this as a value object (rather than a mutable notifier field)
/// makes it safe to pass into go_router's redirect callback and to compare
/// with `==` in tests.
class AuthState {
  const AuthState({required this.status});

  final AuthStatus status;

  /// Convenience constructors reduce noise at call sites.
  const AuthState.loading() : status = AuthStatus.loading;
  const AuthState.authenticated() : status = AuthStatus.authenticated;
  const AuthState.unauthenticated() : status = AuthStatus.unauthenticated;

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
          status == other.status;

  @override
  int get hashCode => status.hashCode;
}

/// Notifier that owns the mutable [AuthState] and exposes mutation methods
/// for login / logout.
///
/// [AsyncNotifier] is used because the initial state check is async (it reads
/// the secure token store).  Downstream consumers can call [login] and
/// [logout] to drive route redirects via the router's [refreshListenable].
// SharedPreferences key for the session-presence marker.  Used in place of the
// previous bearer-token-in-secure-storage hack: the server authenticates the
// session via an HttpOnly cookie, so the client has no token to persist.  We
// only need a tiny boolean to drive the router redirect on cold start.
const _kAuthSessionPresentKey = 'auth_session_present';

class AuthStateNotifier extends AsyncNotifier<AuthState> {
  @override
  Future<AuthState> build() async {
    // The auth state on cold start is derived from a SharedPreferences marker
    // rather than from any stored bearer token.  Writing the username into
    // SecureTokenStorage (the previous behaviour) caused _AuthInterceptor to
    // attach `Authorization: Bearer <username>` to every request, which the
    // server checks before falling back to the session cookie — yielding 401
    // on every API call after login despite a valid cookie being sent.
    final prefs = await SharedPreferences.getInstance();
    final marked = prefs.getBool(_kAuthSessionPresentKey) ?? false;
    return marked
        ? const AuthState.authenticated()
        : const AuthState.unauthenticated();
  }

  /// Called after a successful login.  The [token] parameter is accepted for
  /// backwards compatibility with the call site but is intentionally unused;
  /// the real authentication artefact is the session cookie set by the server
  /// and stored by the Dio CookieManager.  See [build] for why.
  Future<void> login(String token) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setBool(_kAuthSessionPresentKey, true);
    state = const AsyncData(AuthState.authenticated());
  }

  /// Called on explicit logout or after the API returns 401.  Clears the
  /// session marker so the next cold start redirects to /login.
  Future<void> logout() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.remove(_kAuthSessionPresentKey);
    state = const AsyncData(AuthState.unauthenticated());
  }
}

/// The single source of truth for authentication status, consumed by the
/// router's redirect callback and any widget that needs to gate on auth.
final authStateProvider =
    AsyncNotifierProvider<AuthStateNotifier, AuthState>(
  AuthStateNotifier.new,
);
