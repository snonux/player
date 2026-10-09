// A scripted `dart:io` HTTP client for tests that need real image loading.
//
// The Flutter test binding answers every request with status 400. Tests
// that must reach the bitmap decoder (`Image.network`, `CachedNetworkImage`)
// install [FakeHttpOverrides] instead, which answers from a handler.

import 'dart:async';
import 'dart:io';

/// What the fake server answers.
class FakeHttpReply {
  const FakeHttpReply(this.body, {this.status = HttpStatus.ok});

  final List<int> body;
  final int status;
}

typedef FakeHttpHandler = FakeHttpReply Function(
    Uri uri, Map<String, String> headers);

/// Routes every `HttpClient` created while installed to [handler] and
/// records the requests it saw.
class FakeHttpOverrides extends HttpOverrides {
  FakeHttpOverrides(this.handler);

  /// Replaceable, so one long-lived instance can serve several tests.
  FakeHttpHandler handler;
  final List<(Uri, Map<String, String>)> requests = [];

  @override
  HttpClient createHttpClient(SecurityContext? context) =>
      _FakeHttpClient(this);
}

class _FakeHttpClient implements HttpClient {
  _FakeHttpClient(this._overrides);

  final FakeHttpOverrides _overrides;

  @override
  bool autoUncompress = true;

  @override
  Future<HttpClientRequest> getUrl(Uri url) => openUrl('GET', url);

  @override
  Future<HttpClientRequest> openUrl(String method, Uri url) async =>
      _FakeRequest(_overrides, url);

  @override
  void close({bool force = false}) {}

  @override
  dynamic noSuchMethod(Invocation invocation) => null;
}

class _FakeRequest implements HttpClientRequest {
  _FakeRequest(this._overrides, this._uri);

  final FakeHttpOverrides _overrides;
  final Uri _uri;

  @override
  final _FakeHeaders headers = _FakeHeaders();

  @override
  bool followRedirects = true;

  @override
  int maxRedirects = 5;

  @override
  int contentLength = -1;

  @override
  bool persistentConnection = true;

  @override
  Future<void> addStream(Stream<List<int>> stream) => stream.drain<void>();

  @override
  Future<HttpClientResponse> close() async {
    _overrides.requests.add((_uri, headers.values));
    return _FakeResponse(_overrides.handler(_uri, headers.values));
  }

  @override
  dynamic noSuchMethod(Invocation invocation) => null;
}

class _FakeHeaders implements HttpHeaders {
  final Map<String, String> values = {};

  @override
  void add(String name, Object value, {bool preserveHeaderCase = false}) =>
      values[name.toLowerCase()] = '$value';

  @override
  void set(String name, Object value, {bool preserveHeaderCase = false}) =>
      values[name.toLowerCase()] = '$value';

  @override
  void forEach(void Function(String name, List<String> values) action) =>
      values.forEach((name, value) => action(name, [value]));

  @override
  String? value(String name) => values[name.toLowerCase()];

  @override
  dynamic noSuchMethod(Invocation invocation) => null;
}

class _FakeResponse extends Stream<List<int>> implements HttpClientResponse {
  _FakeResponse(this._reply);

  final FakeHttpReply _reply;

  @override
  final _FakeHeaders headers = _FakeHeaders();

  @override
  int get statusCode => _reply.status;

  @override
  String get reasonPhrase => _reply.status == HttpStatus.ok ? 'OK' : 'Error';

  @override
  int get contentLength => _reply.body.length;

  @override
  bool get isRedirect => false;

  @override
  bool get persistentConnection => false;

  @override
  List<RedirectInfo> get redirects => const [];

  @override
  HttpClientResponseCompressionState get compressionState =>
      HttpClientResponseCompressionState.notCompressed;

  @override
  StreamSubscription<List<int>> listen(
    void Function(List<int> event)? onData, {
    Function? onError,
    void Function()? onDone,
    bool? cancelOnError,
  }) =>
      Stream<List<int>>.value(_reply.body).listen(onData,
          onError: onError, onDone: onDone, cancelOnError: cancelOnError);

  @override
  dynamic noSuchMethod(Invocation invocation) => null;
}
