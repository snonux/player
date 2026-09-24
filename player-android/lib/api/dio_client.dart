import 'dart:async';

import 'package:cookie_jar/cookie_jar.dart';
import 'package:dio/dio.dart';
import 'package:dio_cookie_manager/dio_cookie_manager.dart';
import 'package:flutter/material.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:go_router/go_router.dart';

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

/// Serializes credential writes and 401 invalidation in one provider scope.
class CredentialMutationQueue {
  Future<void> _tail = Future<void>.value();

  Future<T> run<T>(Future<T> Function() action) async {
    final previous = _tail;
    final release = Completer<void>();
    _tail = release.future;
    await previous;
    try {
      return await action();
    } finally {
      release.complete();
    }
  }
}

/// CookieJar's domain matching ignores ports, so bind it to the API origin.
class _OriginBoundCookieJar implements CookieJar {
  _OriginBoundCookieJar(this._origin) : _delegate = CookieJar();

  final String _origin;
  final CookieJar _delegate;

  bool _matchesOrigin(Uri uri) =>
      (uri.scheme == 'http' || uri.scheme == 'https') && uri.origin == _origin;

  @override
  bool get ignoreExpires => _delegate.ignoreExpires;

  @override
  Future<List<Cookie>> loadForRequest(Uri uri) =>
      _matchesOrigin(uri) ? _delegate.loadForRequest(uri) : Future.value([]);

  @override
  Future<void> saveFromResponse(Uri uri, List<Cookie> cookies) =>
      _matchesOrigin(uri)
          ? _delegate.saveFromResponse(uri, cookies)
          : Future.value();

  @override
  Future<void> delete(Uri uri, [bool withDomainSharedCookie = false]) =>
      _matchesOrigin(uri)
          ? _delegate.delete(uri, withDomainSharedCookie)
          : Future.value();

  @override
  Future<void> deleteAll() => _delegate.deleteAll();
}

/// Interceptor that attaches a Bearer token to every outgoing request.
///
/// The token is read lazily from [TokenStorage] so that changes (login /
/// logout) are picked up without restarting the Dio instance.
class _AuthInterceptor extends Interceptor {
  _AuthInterceptor(this._storage, this._origin);

  final TokenStorage _storage;
  final String _origin;

  @override
  Future<void> onRequest(
    RequestOptions options,
    RequestInterceptorHandler handler,
  ) async {
    // A caller may pass an absolute URL to Dio; never send the mobile token
    // to a different server even when it uses this shared client.
    if (options.uri.origin != _origin) {
      handler.next(options);
      return;
    }
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
    required String origin,
    required CredentialMutationQueue mutationQueue,
    Future<void> Function()? onUnauthorized,
    String loginRoute = '/login',
  })  : _storage = storage,
        _navigatorKey = navigatorKey,
        _origin = origin,
        _mutationQueue = mutationQueue,
        _onUnauthorized = onUnauthorized,
        _loginRoute = loginRoute;

  // Private fields consistent with _AuthInterceptor naming conventions.
  final TokenStorage _storage;
  final GlobalKey<NavigatorState> _navigatorKey;
  final String _origin;
  final CredentialMutationQueue _mutationQueue;
  final Future<void> Function()? _onUnauthorized;
  final String _loginRoute;

  @override
  Future<void> onError(
    DioException err,
    ErrorInterceptorHandler handler,
  ) async {
    if (err.response?.statusCode == 401 &&
        err.requestOptions.uri.origin == _origin) {
      if (err.requestOptions.path.endsWith('/auth/login') ||
          err.requestOptions.path.endsWith('/auth/bootstrap')) {
        handler.next(err);
        return;
      }
      // A response from an older request must not erase credentials minted by
      // a newer login while the request was in flight.
      final authorization = err.requestOptions.headers['Authorization'];
      final sentToken =
          authorization is String && authorization.startsWith('Bearer ')
              ? authorization.substring('Bearer '.length)
              : null;
      if (sentToken == null) {
        handler.next(err);
        return;
      }
      final invalidated = await _mutationQueue.run(() async {
        if (await _storage.readToken() != sentToken) return false;
        await _storage.deleteToken();
        await _onUnauthorized?.call();
        return true;
      });
      if (!invalidated) {
        handler.next(err);
        return;
      }

      // Redirect via go_router (the app's router) rather than the classic
      // Navigator.  pushNamedAndRemoveUntil would throw "Navigator.onGenerateRoute
      // was null" because go_router does not register named routes on the
      // underlying Navigator.  Using the navigatorKey's currentContext lets us
      // resolve the active GoRouter instance without a widget-tree BuildContext.
      final ctx = _navigatorKey.currentContext;
      if (ctx != null && ctx.mounted) {
        GoRouter.of(ctx).go(_loginRoute);
      }
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
  factory DioClient({
    required Uri baseUrl,
    required TokenStorage storage,
    required GlobalKey<NavigatorState> navigatorKey,
    CredentialMutationQueue? mutationQueue,
    Future<void> Function()? onUnauthorized,
    String loginRoute = '/login',
    BaseOptions? baseOptions,
  }) {
    final jar = _OriginBoundCookieJar(baseUrl.origin);
    final dio = _buildDio(
      baseUrl: baseUrl,
      storage: storage,
      navigatorKey: navigatorKey,
      mutationQueue: mutationQueue ?? CredentialMutationQueue(),
      onUnauthorized: onUnauthorized,
      loginRoute: loginRoute,
      baseOptions: baseOptions,
      cookieJar: jar,
    );
    return DioClient._(dio: dio, cookieJar: jar);
  }

  DioClient._({required Dio dio, required CookieJar cookieJar})
      : _dio = dio,
        _cookieJar = cookieJar;

  final Dio _dio;
  final CookieJar _cookieJar;

  /// Exposes the underlying [Dio] so that [PlayerApiClient] can issue typed
  /// requests without re-implementing the interceptor plumbing.
  Dio get dio => _dio;

  /// Exposes the cookie jar so consumers that bypass Dio (e.g. ExoPlayer via
  /// just_audio, video_player, CachedNetworkImage) can still authenticate
  /// against the session-cookie-protected media endpoints.
  CookieJar get cookieJar => _cookieJar;

  static Dio _buildDio({
    required Uri baseUrl,
    required TokenStorage storage,
    required GlobalKey<NavigatorState> navigatorKey,
    required CredentialMutationQueue mutationQueue,
    Future<void> Function()? onUnauthorized,
    required String loginRoute,
    BaseOptions? baseOptions,
    required CookieJar cookieJar,
  }) {
    final options = (baseOptions ?? BaseOptions()).copyWith(
      baseUrl: baseUrl.toString(),
      // JSON is the wire format for all API endpoints.
      contentType: 'application/json',
      responseType: ResponseType.json,
    );

    return Dio(options)
      ..interceptors.addAll([
        // Cookie manager runs first so the session cookie is replayed before
        // _AuthInterceptor decides whether to add a Bearer fallback.
        CookieManager(cookieJar),
        _AuthInterceptor(storage, baseUrl.origin),
        _UnauthorizedInterceptor(
          storage: storage,
          navigatorKey: navigatorKey,
          origin: baseUrl.origin,
          mutationQueue: mutationQueue,
          onUnauthorized: onUnauthorized,
          loginRoute: loginRoute,
        ),
      ]);
  }
}
