import 'dart:async';
import 'dart:io';
import 'dart:convert';
import 'dart:math';
import 'dart:typed_data';
import 'package:dio/dio.dart';
import 'package:dio/io.dart';
import '../models/server_endpoint_profile.dart';

const endpointTimeout = Duration(seconds: 3);
typedef IdentityProbe = Future<String> Function(Uri);

/// A public ID prevents accidental pairing; explicit origin trust and TLS are
/// still required. Neither the Wi-Fi name nor the ID is an auth credential.
Future<String> probeServerIdentity(Uri origin) async {
  final probe = Dio(BaseOptions(
      connectTimeout: endpointTimeout,
      receiveTimeout: endpointTimeout,
      followRedirects: false));
  try {
    final response = await probe
        .get<dynamic>(origin.resolve('/api/v1/server-identity').toString());
    final id = response.data is Map ? response.data['server_id'] : null;
    if (response.statusCode != 200 ||
        id is! String ||
        !RegExp(r'^[a-f0-9]{64}$').hasMatch(id)) {
      throw const EndpointIdentityException();
    }
    return id;
  } on DioException catch (error) {
    if ([502, 503, 504].contains(error.response?.statusCode)) {
      throw const EndpointReachabilityException();
    }
    if (error.response != null) throw const EndpointIdentityException();
    rethrow;
  } finally {
    probe.close(force: true);
  }
}

/// Selects transport independently of canonical account, queue and cache keys.
/// Only explicitly paired origins are considered; probes never carry auth.
class ServerEndpointRouter {
  ServerEndpointRouter(
      {required this.primary,
      this.profile,
      IdentityProbe? probe,
      Future<String?> Function()? wifiName,
      this.onStatus})
      : probe = probe ?? probeServerIdentity,
        wifiName = wifiName ?? (() async => null);
  final Uri primary;
  ServerEndpointProfile? profile;
  final IdentityProbe probe;
  final Future<String?> Function() wifiName;
  final void Function(String)? onStatus;
  String? _failedOrigin;
  Uri? _active;
  DateTime? _checkedAt;
  Future<Uri>? _pending;
  HttpServer? _gateway;
  Future<HttpServer>? _startingGateway;
  bool _disposed = false;
  int _revision = 0;
  final _gatewayKey = base64Url
      .encode(List<int>.generate(32, (_) => Random.secure().nextInt(256)));

  /// Native players follow redirects with supplied headers. Keep credentials
  /// behind a local streaming gateway whose upstream never follows redirects.
  Future<Uri> mediaUri(Uri uri) async {
    if (profile == null || uri.origin != primary.origin) return uri;
    if (_disposed) throw StateError('Server connection changed.');
    final gateway = _gateway ??= await (_startingGateway ??= _startGateway());
    if (_disposed) {
      await gateway.close(force: true);
      throw StateError('Server connection changed.');
    }
    return Uri(
        scheme: 'http',
        host: InternetAddress.loopbackIPv4.address,
        port: gateway.port,
        path: '/$_gatewayKey/${base64Url.encode(utf8.encode(uri.toString()))}');
  }

  Future<HttpServer> _startGateway() async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    server.listen((request) => unawaited(_forwardMedia(request)));
    return server;
  }

  Future<void> close() async {
    _disposed = true;
    final server = _gateway ?? await _startingGateway;
    await server?.close(force: true);
  }

  Future<void> _forwardMedia(HttpRequest request) async {
    HttpClient? client;
    try {
      final parts = request.uri.pathSegments;
      if (_disposed ||
          parts.length != 2 ||
          parts[0] != _gatewayKey ||
          !{'GET', 'HEAD'}.contains(request.method)) {
        request.response.statusCode = HttpStatus.notFound;
        return;
      }
      final canonical = Uri.parse(utf8.decode(base64Url.decode(parts[1])));
      if (canonical.origin != primary.origin ||
          canonical.userInfo.isNotEmpty ||
          !RegExp(r'^/api/(v1/)?(media/[0-9]+/(stream|compat|download|thumbnail)|sets/[0-9]+/cover)$')
              .hasMatch(canonical.path)) {
        request.response.statusCode = HttpStatus.notFound;
        return;
      }
      final target = await resolve(canonical);
      Future<HttpClientResponse> send(Uri destination) async {
        if (_disposed) throw StateError('Server connection changed.');
        client?.close(force: true);
        client = HttpClient()
          ..autoUncompress = false
          ..connectionTimeout = endpointTimeout;
        final upstream = await client!.openUrl(request.method, destination);
        upstream.followRedirects = false;
        if (_disposed) throw StateError('Server connection changed.');
        for (final name in [
          'authorization',
          'cookie',
          'range',
          'if-range',
          'if-none-match',
          'if-modified-since'
        ]) {
          final value = request.headers.value(name);
          if (value != null) upstream.headers.set(name, value);
        }
        return upstream.close().timeout(const Duration(seconds: 30));
      }

      HttpClientResponse response;
      try {
        response = await send(target);
      } catch (_) {
        // No upstream response yet: GET/HEAD can safely try the verified pair.
        // Once headers/body arrive, auth/status/stream errors are never replayed.
        failed(target);
        final alternate = await resolve(canonical);
        if (alternate.origin == target.origin) rethrow;
        response = await send(alternate);
      }
      // Never expose a redirect Location to a native player with auth headers.
      if (response.statusCode >= 300 &&
          response.statusCode < 400 &&
          response.statusCode != 304) {
        request.response.statusCode = HttpStatus.badGateway;
        return;
      }
      request.response.statusCode = response.statusCode;
      for (final name in [
        'content-type',
        'content-length',
        'content-range',
        'accept-ranges',
        'etag',
        'last-modified',
        'cache-control',
        'content-encoding'
      ]) {
        final value = response.headers.value(name);
        if (value != null) request.response.headers.set(name, value);
      }
      await request.response.addStream(response);
    } catch (_) {
      try {
        request.response.statusCode = HttpStatus.badGateway;
      } catch (_) {/* response already started */}
    } finally {
      client?.close(force: true);
      try {
        await request.response.close();
      } catch (_) {/* native player disconnected */}
    }
  }

  void updateProfile(ServerEndpointProfile? next) {
    if (next != null && next.primary != primary.origin) {
      throw const FormatException('Paired address belongs to another profile.');
    }
    _revision++;
    profile = next;
    _active = null;
    _failedOrigin = null;
    invalidate();
  }

  void invalidate() {
    _checkedAt = null;
  }

  void failed(Uri uri) {
    _failedOrigin = uri.origin;
    invalidate();
  }

  Future<Uri> resolve(Uri uri) async {
    if (_disposed) throw StateError('Server connection changed.');
    if (uri.origin != primary.origin || profile == null) return uri;
    final active =
        await (_pending ??= _select().whenComplete(() => _pending = null));
    if (_disposed) throw StateError('Server connection changed.');
    return uri.replace(
        scheme: active.scheme, host: active.host, port: active.port);
  }

  Future<Uri> _select() async {
    if (_active != null &&
        _checkedAt != null &&
        DateTime.now().difference(_checkedAt!) < const Duration(seconds: 5)) {
      return _active!;
    }
    final revision = _revision;
    final p = profile!;
    final wifi = p.wifiName.isEmpty ? null : await wifiName();
    if (revision != _revision) {
      return profile == null ? primary : await _select();
    }
    final localPreferred =
        p.preferLocal && (p.wifiName.isEmpty || wifi == p.wifiName);
    final origins = localPreferred
        ? [Uri.parse(p.local), primary]
        : [primary, Uri.parse(p.local)];
    // A Wi-Fi rule restricts local routing; unknown/denied SSID stays remote.
    if (p.wifiName.isNotEmpty && wifi != p.wifiName) {
      origins.removeWhere((u) => u.origin == p.local);
    }
    final preferredOrigin = origins.first.origin;
    if (_failedOrigin != null &&
        origins.length > 1 &&
        origins.first.origin == _failedOrigin) {
      origins.add(origins.removeAt(0));
    }
    for (final origin in origins) {
      try {
        final id = await probe(origin);
        if (_disposed) throw StateError('Server connection changed.');
        if (revision != _revision) {
          return profile == null ? primary : await _select();
        }
        if (id != p.serverId) {
          onStatus?.call(
              'Address belongs to a different server. Connection blocked.');
          throw const EndpointIdentityException();
        }
        _active = origin;
        _checkedAt = DateTime.now();
        _failedOrigin = null;
        final fallback = origin.origin != preferredOrigin;
        onStatus?.call(
            '${fallback ? 'Preferred address unavailable · ' : ''}Connected using ${origin.origin}');
        return origin;
      } on EndpointIdentityException {
        rethrow;
      } catch (error) {
        // Only known transport outages permit a fallback. TLS, malformed
        // identities and unexpected failures must never become offline access.
        if (!_isReachabilityFailure(error)) rethrow;
      }
    }
    onStatus?.call(
        'Cannot reach either server address. Check the network and retry.');
    throw const EndpointReachabilityException();
  }
}

/// Every address allowed by the selected profile failed a transport probe.
/// This typed boundary also survives Dio's wrapping of adapter exceptions.
class EndpointReachabilityException implements Exception {
  const EndpointReachabilityException();
  @override
  String toString() => 'Cannot reach the paired server addresses.';
}

bool _isReachabilityFailure(Object error) {
  if (error is EndpointReachabilityException ||
      error is SocketException ||
      error is TimeoutException) {
    return true;
  }
  if (error is! DioException || error.response != null) return false;
  return {
        DioExceptionType.connectionTimeout,
        DioExceptionType.receiveTimeout,
        DioExceptionType.sendTimeout,
      }.contains(error.type) ||
      (error.type == DioExceptionType.connectionError &&
          error.error is SocketException);
}

class EndpointIdentityException implements Exception {
  const EndpointIdentityException();
  @override
  String toString() =>
      'Address belongs to a different server. Connection blocked.';
}

/// Keep Dio request identity canonical for the existing auth interceptors.
/// Rewrite only at the transport adapter; redirects never receive credentials.
class EndpointHttpAdapter implements HttpClientAdapter {
  EndpointHttpAdapter(this.router, {HttpClientAdapter? delegate})
      : delegate = delegate ?? IOHttpClientAdapter();
  final ServerEndpointRouter router;
  final HttpClientAdapter delegate;
  @override
  Future<ResponseBody> fetch(RequestOptions options, Stream<Uint8List>? body,
      Future<void>? cancelFuture) async {
    final uri = await router.resolve(options.uri);
    final routed = options.copyWith(
        path: uri.toString(),
        baseUrl: '',
        queryParameters: {},
        followRedirects: false);
    try {
      return await delegate.fetch(routed, body, cancelFuture);
    } catch (_) {
      router.failed(uri);
      // Requests may have reached the server: never replay a write or body.
      if ((options.method != 'GET' && options.method != 'HEAD') ||
          body != null ||
          router.profile == null) {
        rethrow;
      }
      final alternate = await router.resolve(options.uri);
      if (alternate.origin == uri.origin) rethrow;
      return delegate.fetch(
          options.copyWith(
              path: alternate.toString(),
              baseUrl: '',
              queryParameters: {},
              followRedirects: false),
          null,
          cancelFuture);
    }
  }

  @override
  void close({bool force = false}) => delegate.close(force: force);
}
