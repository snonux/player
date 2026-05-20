import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'api_client_provider.dart';

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
class AuthStateNotifier extends AsyncNotifier<AuthState> {
  @override
  Future<AuthState> build() async {
    // Determine whether a token already exists on app startup.  This drives
    // the initial route decision inside the go_router redirect callback.
    final storage = ref.read(tokenStorageProvider);
    final token = await storage.readToken();

    return token != null && token.isNotEmpty
        ? const AuthState.authenticated()
        : const AuthState.unauthenticated();
  }

  /// Called after a successful login; persists [token] and updates state.
  Future<void> login(String token) async {
    final storage = ref.read(tokenStorageProvider);
    await storage.writeToken(token);
    state = const AsyncData(AuthState.authenticated());
  }

  /// Called on explicit logout or after [_UnauthorizedInterceptor] purges the
  /// token.  Clears the stored token and moves to the unauthenticated state.
  Future<void> logout() async {
    final storage = ref.read(tokenStorageProvider);
    await storage.deleteToken();
    state = const AsyncData(AuthState.unauthenticated());
  }
}

/// The single source of truth for authentication status, consumed by the
/// router's redirect callback and any widget that needs to gate on auth.
final authStateProvider =
    AsyncNotifierProvider<AuthStateNotifier, AuthState>(
  AuthStateNotifier.new,
);
