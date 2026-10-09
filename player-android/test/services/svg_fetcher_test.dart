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
  _Adapter({this.status = 200, List<List<int>>? body, this.hold, this.onChunk})
      : body = body ?? [svgBytes(kValidSvg)];

  final int status;
  final List<List<int>> body;
  final Completer<void>? hold;

  /// Told the index of each chunk as it is handed to the downloader.
  final void Function(int index)? onChunk;
  final List<RequestOptions> requests = [];

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    requests.add(options);
    await hold?.future;
    var cancelled = false;
    unawaited(cancelFuture?.then((_) => cancelled = true));
    return ResponseBody(_chunks(() => cancelled), status);
  }

  /// Sends one chunk per turn of the event loop and stops when the request
  /// is cancelled, as a socket does, so the test sees how far it was read.
  Stream<Uint8List> _chunks(bool Function() isCancelled) async* {
    for (var i = 0; i < body.length && !isCancelled(); i++) {
      onChunk?.call(i);
      yield Uint8List.fromList(body[i]);
      await Future<void>.delayed(Duration.zero);
    }
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
  group('content sniffing', _sniffTests);
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

void _sniffTests() {
  Future<Uint8List> fetch(_Adapter adapter, CancelToken cancel) =>
      _fetcher(adapter)(_uri, const {}, cancel);

  test('stops at the first chunk of a body that is not markup', () async {
    // A JPEG: megabytes would follow, but the first bytes settle it.
    final served = <int>[];
    final adapter = _Adapter(body: [
      [0xff, 0xd8, 0xff, 0xe0, 0, 16],
      for (var i = 0; i < 200; i++) List.filled(1024, 0x42),
    ], onChunk: served.add);
    final cancel = CancelToken();

    await expectLater(fetch(adapter, cancel), throwsA(isA<NotSvgException>()));

    expect(cancel.isCancelled, isTrue);
    // A chunk or two may already be on the way when the request is
    // cancelled, but the 200 KB body is not transferred.
    expect(served.length, lessThan(5));
  });

  test('JSON and plain text are not markup either', () async {
    for (final body in ['{"error":"gone"}', 'Not Found', '  \n GIF89a']) {
      await expectLater(fetch(_Adapter(body: [svgBytes(body)]), CancelToken()),
          throwsA(isA<NotSvgException>()));
    }
  });

  test('white space and byte order marks before the markup are fine', () async {
    final bodies = <List<List<int>>>[
      [svgBytes('  \n\t'), svgBytes('\n<svg/>')],
      [
        [0xef, 0xbb, 0xbf, ...svgBytes('<svg/>')]
      ],
      [
        [0xff, 0xfe, 0x3c, 0x00, 0x73, 0x00]
      ],
      [
        [0xfe, 0xff, 0x00, 0x3c, 0x00, 0x73]
      ],
    ];
    for (final body in bodies) {
      final bytes = await fetch(_Adapter(body: body), CancelToken());
      expect(bytes, isNotEmpty);
    }
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
