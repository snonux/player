import 'dart:convert';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../models/user.dart';
import 'api_client_provider.dart';
import 'audio_handler_provider.dart';
import 'progress_queue_provider.dart';

const _kLogoutRequestTimeout = Duration(seconds: 10);

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
const _kAuthTokenIdKey = 'auth_token_id';

class AuthStateNotifier extends AsyncNotifier<AuthState> {
  @override
  Future<AuthState> build() async {
    final queue = ref.read(credentialMutationQueueProvider);
    final generation = queue.generation;
    try {
      final prefs = await SharedPreferences.getInstance();
      final token = await ref.read(tokenStorageProvider).readToken();
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
        final user =
            User.fromJson(jsonDecode(userJson) as Map<String, dynamic>);
        if (user.id > 0 && user.username.isNotEmpty) {
          if (generation == queue.generation) {
            queue.enable(generation);
            return AuthState.authenticated(user: user);
          }
          return const AuthState.unauthenticated();
        }
      }
      await _clearCredentials(prefs);
    } catch (_) {
      // Restore and cleanup failures must still let the login UI start. The
      // credential gate stays closed until a valid restore or login succeeds.
      try {
        await _clearCredentials();
      } catch (_) {}
    }
    return const AuthState.unauthenticated();
  }

  /// Retains the server's authenticated user separately from credentials.
  Future<void> login(User user) async {
    final queue = ref.read(credentialMutationQueueProvider);
    final generation = queue.beginAuthChange();
    await future;
    final prefs = await SharedPreferences.getInstance();
    String? mintedToken;
    int? mintedTokenId;
    try {
      await queue.run(() async {
        if (generation != queue.generation) {
          throw StateError('Login superseded by a newer auth action');
        }
        await _clearCredentials(prefs);
      });
      if (generation != queue.generation) {
        throw StateError('Login superseded by a newer auth action');
      }
      // The login/bootstrap response sets a session cookie. Mint a dedicated
      // mobile token while that cookie is available in the shared Dio client.
      final result = await ref.read(apiClientProvider).createAPIToken(
            name: 'android-client',
            expiresInDays: 365,
          );
      final token = result['token'];
      final tokenId = result['id'];
      if (token is! String ||
          token.isEmpty ||
          tokenId is! int ||
          tokenId <= 0) {
        throw const FormatException('Token creation returned no credential');
      }
      mintedToken = token;
      mintedTokenId = tokenId;
      await queue.run(() async {
        if (generation != queue.generation) {
          throw StateError('Login superseded by a newer auth action');
        }
        await ref.read(tokenStorageProvider).writeToken(token);
        await prefs.setInt(_kAuthTokenIdKey, tokenId);
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
        if (ref.exists(progressQueueProvider)) {
          await ref.read(progressQueueProvider).resume();
        }
        if (generation != queue.generation) {
          throw StateError('Login superseded by a newer auth action');
        }
        queue.enable(generation);
        state = AsyncData(AuthState.authenticated(user: user));
      });
    } catch (_) {
      // A token minted by the server must be revoked even if persisting it or
      // resuming account work failed. Clear locally first so a slow revoke
      // cannot keep a failed login active. A newer auth action owns its state.
      if (generation == queue.generation) {
        try {
          await queue.run(() async {
            if (generation == queue.generation) {
              await clearAfterUnauthorized(advanceGeneration: false);
            }
          });
        } catch (_) {}
      }
      if (mintedToken != null && mintedTokenId != null) {
        try {
          await ref
              .read(apiClientProvider)
              .revokeAPIToken(mintedTokenId, bearerToken: mintedToken)
              .timeout(_kLogoutRequestTimeout);
        } catch (_) {}
      }
      rethrow;
    }
  }

  /// Revokes the mobile token and session when reachable, then always removes
  /// local credentials. Returns false if either server request failed.
  Future<bool> logout() async {
    ref.read(credentialMutationQueueProvider).beginAuthChange();
    final snapshot =
        await ref.read(credentialMutationQueueProvider).run(() async {
      int? tokenId;
      String? token;
      var tokenReadFailed = false;
      String? sessionCookie;
      try {
        tokenId =
            (await SharedPreferences.getInstance()).getInt(_kAuthTokenIdKey);
      } catch (_) {}
      try {
        token = await ref.read(tokenStorageProvider).readToken();
      } catch (_) {
        tokenReadFailed = true;
      }
      try {
        final cookies = await ref
            .read(cookieJarProvider)
            .loadForRequest(ref.read(playerBaseUrlProvider));
        for (final cookie in cookies) {
          if (cookie.name == 'session') {
            sessionCookie = 'session=${cookie.value}';
            break;
          }
        }
      } catch (_) {}
      final localCleared =
          await clearAfterUnauthorized(advanceGeneration: false);
      return (
        tokenId: tokenId,
        token: token,
        tokenReadFailed: tokenReadFailed,
        sessionCookie: sessionCookie,
        localCleared: localCleared
      );
    });

    var revoked = snapshot.localCleared &&
        !snapshot.tokenReadFailed &&
        snapshot.token == null;
    if (snapshot.token != null && snapshot.tokenId != null) {
      try {
        await ref
            .read(apiClientProvider)
            .revokeAPIToken(snapshot.tokenId!, bearerToken: snapshot.token)
            .timeout(_kLogoutRequestTimeout);
        revoked = snapshot.localCleared;
      } catch (_) {
        revoked = false;
      }
    } else if (snapshot.token != null) {
      try {
        await ref
            .read(apiClientProvider)
            .revokeCurrentAPIToken(bearerToken: snapshot.token!)
            .timeout(_kLogoutRequestTimeout);
        revoked = snapshot.localCleared;
      } catch (_) {
        revoked = false;
      }
    }
    if (snapshot.sessionCookie != null) {
      try {
        await ref
            .read(apiClientProvider)
            .logout(sessionCookie: snapshot.sessionCookie)
            .timeout(_kLogoutRequestTimeout);
      } catch (_) {
        revoked = false;
      }
    }
    return revoked;
  }

  /// Called only while the shared credential queue is held by a 401 handler.
  Future<bool> clearAfterUnauthorized({bool advanceGeneration = true}) async {
    if (advanceGeneration) {
      ref.read(credentialMutationQueueProvider).beginAuthChange();
    }
    var cleared = true;
    try {
      await ref.read(progressQueueProvider).clearAndSuspend();
    } catch (_) {
      cleared = false;
    }
    try {
      if (ref.exists(audioHandlerProvider)) {
        await ref.read(audioHandlerProvider).stop();
      }
    } catch (_) {
      cleared = false;
    }
    try {
      await _clearCredentials();
    } catch (_) {
      cleared = false;
    }
    try {
      await ref.read(cookieJarProvider).deleteAll();
    } catch (_) {
      cleared = false;
    }
    state = const AsyncData(AuthState.unauthenticated());
    return cleared;
  }

  Future<void> _clearCredentials([SharedPreferences? prefs]) async {
    try {
      prefs ??= await SharedPreferences.getInstance();
      await prefs.remove(_kAuthSessionPresentKey);
      await prefs.remove(_kAuthUserKey);
      await prefs.remove(_kAuthOriginKey);
      await prefs.remove(_kAuthExpiresKey);
      await prefs.remove(_kAuthTokenIdKey);
    } finally {
      await ref.read(tokenStorageProvider).deleteToken();
    }
  }
}

/// The single source of truth for authentication status, consumed by the
/// router's redirect callback and any widget that needs to gate on auth.
final authStateProvider = AsyncNotifierProvider<AuthStateNotifier, AuthState>(
  AuthStateNotifier.new,
);
