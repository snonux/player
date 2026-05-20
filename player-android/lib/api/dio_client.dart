import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

// Storage key under which the bearer token is persisted across app restarts.
const _kTokenKey = 'bearer_token';

/// Abstraction over the secure token store so the interceptor can be tested
/// without platform code (Liskov substitution / dependency inversion).
abstract interface class TokenStorage {
  Future<String?> readToken();
  Future<void> writeToken(String token);
  Future<void> deleteToken();
}

/// Production implementation backed by [FlutterSecureStorage].
///
/// Uses AES encryption on Android and the iOS Keychain on Apple platforms.
class SecureTokenStorage implements TokenStorage {
  SecureTokenStorage({FlutterSecureStorage? storage})
      : _storage = storage ?? const FlutterSecureStorage();

  final FlutterSecureStorage _storage;

  @override
  Future<String?> readToken() => _storage.read(key: _kTokenKey);

  @override
  Future<void> writeToken(String token) =>
      _storage.write(key: _kTokenKey, value: token);

  @override
  Future<void> deleteToken() => _storage.delete(key: _kTokenKey);
}

/// Interceptor that attaches a Bearer token to every outgoing request.
///
/// The token is read lazily from [TokenStorage] so that changes (login /
/// logout) are picked up without restarting the Dio instance.
class _AuthInterceptor extends Interceptor {
  _AuthInterceptor(this._storage);

  final TokenStorage _storage;

  @override
  Future<void> onRequest(
    RequestOptions options,
    RequestInterceptorHandler handler,
  ) async {
    final token = await _storage.readToken();
    if (token != null && token.isNotEmpty) {
      // Only attach the bearer token when no Authorization header has been set
      // explicitly by the caller (e.g. public endpoints may supply their own
      // credentials and must not be overwritten).
      if (!options.headers.containsKey('Authorization')) {
        options.headers['Authorization'] = 'Bearer $token';
      }
    }
    handler.next(options);
  }
}

/// Interceptor that intercepts 401 Unauthorized responses and redirects the
/// user to the login route via the supplied [NavigatorKey].
///
/// On 401, the stored token is removed (it is no longer valid) and the
/// navigator pushes a named replacement so that the back-stack cannot return
/// the user to an authenticated screen.
class _UnauthorizedInterceptor extends Interceptor {
  _UnauthorizedInterceptor({
    required TokenStorage storage,
    required GlobalKey<NavigatorState> navigatorKey,
    String loginRoute = '/login',
  })  : _storage = storage,
        _navigatorKey = navigatorKey,
        _loginRoute = loginRoute;

  // Private fields consistent with _AuthInterceptor naming conventions.
  final TokenStorage _storage;
  final GlobalKey<NavigatorState> _navigatorKey;
  final String _loginRoute;

  @override
  Future<void> onError(
    DioException err,
    ErrorInterceptorHandler handler,
  ) async {
    if (err.response?.statusCode == 401) {
      // Purge the stale token so subsequent requests start unauthenticated.
      await _storage.deleteToken();

      // Use the navigator key to redirect without needing a BuildContext.
      _navigatorKey.currentState
          ?.pushNamedAndRemoveUntil(_loginRoute, (_) => false);
    }
    handler.next(err);
  }
}

/// Factory that assembles a fully configured [Dio] instance wired with:
///   - bearer-token injection on every request, and
///   - global 401 → login redirect.
///
/// Callers own the returned [Dio] and may add further interceptors on top.
/// Separating construction from usage (SRP) keeps this class testable.
class DioClient {
  DioClient({
    required Uri baseUrl,
    required TokenStorage storage,
    required GlobalKey<NavigatorState> navigatorKey,
    String loginRoute = '/login',
    BaseOptions? baseOptions,
  }) : _dio = _buildDio(
          baseUrl: baseUrl,
          storage: storage,
          navigatorKey: navigatorKey,
          loginRoute: loginRoute,
          baseOptions: baseOptions,
        );

  final Dio _dio;

  /// Exposes the underlying [Dio] so that [PlayerApiClient] can issue typed
  /// requests without re-implementing the interceptor plumbing.
  Dio get dio => _dio;

  static Dio _buildDio({
    required Uri baseUrl,
    required TokenStorage storage,
    required GlobalKey<NavigatorState> navigatorKey,
    required String loginRoute,
    BaseOptions? baseOptions,
  }) {
    final options = (baseOptions ?? BaseOptions()).copyWith(
      baseUrl: baseUrl.toString(),
      // JSON is the wire format for all API endpoints.
      contentType: 'application/json',
      responseType: ResponseType.json,
    );

    return Dio(options)
      ..interceptors.addAll([
        // Auth must run before the 401 handler so the token is attached first.
        _AuthInterceptor(storage),
        _UnauthorizedInterceptor(
          storage: storage,
          navigatorKey: navigatorKey,
          loginRoute: loginRoute,
        ),
      ]);
  }
}
