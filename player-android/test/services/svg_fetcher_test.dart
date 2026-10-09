// Tests for the real SVG downloader (svg_fetcher.dart). Dio runs for real;
// only its transport is replaced by a scripted adapter.

import 'dart:async';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/svg_document.dart';
import 'package:player_android/services/svg_fetcher.dart';

import '../support/svg_test_support.dart';

final _uri = Uri.parse('https://player.example/api/v1/media/9/thumbnail');

/// Transport that answers with [status] and the chunks of [body]. While
/// [hold] is set the response headers are not sent, like a stalled server.
class _Adapter implements HttpClientAdapter {
  _Adapter({this.status = 200, List<List<int>>? body, this.hold})
      : body = body ?? [svgBytes(kValidSvg)];

  final int status;
  final List<List<int>> body;
  final Completer<void>? hold;
  final List<RequestOptions> requests = [];

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    requests.add(options);
    await hold?.future;
    final chunks = Stream.fromIterable(body.map(Uint8List.fromList));
    return ResponseBody(chunks, status);
  }

  @override
  void close({bool force = false}) {}
}

DioSvgFetcher _fetcher(
  _Adapter adapter, {
  int maxBytes = kMaxSvgBytes,
  Duration deadline = const Duration(seconds: 30),
}) =>
    DioSvgFetcher(
      Dio()..httpClientAdapter = adapter,
      maxBytes: maxBytes,
      deadline: deadline,
    );

Matcher _httpError(int status) => throwsA(isA<DioException>()
    .having((e) => e.response?.statusCode, 'status', status));

Matcher _rejects(String message) => throwsA(
    isA<SvgException>().having((e) => e.message, 'message', contains(message)));

void main() {
  group('download', _downloadTests);
  group('limits', _limitTests);
  group('providers', _providerTests);
}

void _downloadTests() {
  test('sends the given headers and returns the body bytes', () async {
    final adapter = _Adapter(body: [
      svgBytes('<svg '),
      svgBytes('/>'),
    ]);
    final bytes = await _fetcher(adapter)(
        _uri, {'Authorization': 'Bearer t', 'Cookie': 's=1'}, CancelToken());

    expect(String.fromCharCodes(bytes), '<svg />');
    final request = adapter.requests.single;
    expect(request.uri, _uri);
    expect(request.headers['Authorization'], 'Bearer t');
    expect(request.headers['Cookie'], 's=1');
  });

  test('sends no credentials when none are given', () async {
    final adapter = _Adapter();
    await _fetcher(adapter)(_uri, const {}, CancelToken());

    final headers = adapter.requests.single.headers;
    expect(headers.containsKey('Authorization'), isFalse);
    expect(headers.containsKey('Cookie'), isFalse);
  });

  test('a 401 or 404 response is an error, not image data', () async {
    for (final status in [401, 404]) {
      final fetcher = _fetcher(_Adapter(status: status));
      await expectLater(
          fetcher(_uri, const {}, CancelToken()), _httpError(status));
    }
  });
}

void _limitTests() {
  test('aborts a body larger than the limit', () async {
    final adapter = _Adapter(body: List.filled(10, List.filled(100, 0x20)));
    final cancel = CancelToken();

    await expectLater(_fetcher(adapter, maxBytes: 250)(_uri, const {}, cancel),
        _rejects('too large'));
    expect(cancel.isCancelled, isTrue);
  });

  test('accepts a body exactly at the limit', () async {
    final adapter = _Adapter(body: [List.filled(250, 0x20)]);
    final bytes =
        await _fetcher(adapter, maxBytes: 250)(_uri, const {}, CancelToken());
    expect(bytes, hasLength(250));
  });

  test('gives up and cancels when the server stalls', () async {
    final adapter = _Adapter(hold: Completer<void>());
    final cancel = CancelToken();
    final fetcher =
        _fetcher(adapter, deadline: const Duration(milliseconds: 50));

    await expectLater(fetcher(_uri, const {}, cancel), _rejects('timed out'));
    expect(cancel.isCancelled, isTrue);
  });

  test('cancelling the token aborts a running download', () async {
    final adapter = _Adapter(hold: Completer<void>());
    final cancel = CancelToken();
    final download = _fetcher(adapter)(_uri, const {}, cancel);
    cancel.cancel('no longer shown');

    await expectLater(
        download,
        throwsA(isA<DioException>()
            .having((e) => e.type, 'type', DioExceptionType.cancel)));
  });
}

void _providerTests() {
  test('the app-wide client has timeouts and no interceptors', () {
    final container = ProviderContainer();
    addTearDown(container.dispose);
    final dio = container.read(svgDioProvider);

    expect(dio.options.connectTimeout, isNotNull);
    expect(dio.options.receiveTimeout, isNotNull);
    // Dio installs one built-in interceptor; nothing adds credentials.
    expect(dio.interceptors.whereType<InterceptorsWrapper>(), isEmpty);
    expect(dio.interceptors.length, Dio().interceptors.length);
  });

  test('the default fetcher downloads through that client', () async {
    final adapter = _Adapter();
    final container = ProviderContainer(overrides: [
      svgDioProvider.overrideWithValue(Dio()..httpClientAdapter = adapter),
    ]);
    addTearDown(container.dispose);

    final bytes = await container.read(svgFetcherProvider)(
        _uri, {'Authorization': 'Bearer t'}, CancelToken());

    expect(String.fromCharCodes(bytes), kValidSvg);
    expect(adapter.requests.single.headers['Authorization'], 'Bearer t');
  });
}
