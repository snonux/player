import 'dart:async';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'svg_document.dart';

/// Downloads the raw bytes of an SVG document. Cancelling [cancel] aborts
/// the transfer.
typedef SvgFetcher = Future<Uint8List> Function(
  Uri uri,
  Map<String, String> headers,
  CancelToken cancel,
);

/// Downloads SVG documents with Dio, bounded in size and time.
///
/// The server sends the original file as the thumbnail of an SVG, so even a
/// 48 px list tile may point at a huge document. The body is therefore read
/// as a stream and the transfer is aborted once it passes [maxBytes], and
/// the whole request must finish within [deadline]. A non-2xx status makes
/// Dio throw; every failure ends up as the image's error state.
///
/// Images of unknown type are probed for SVG after the bitmap decoder gave
/// up on them, and most of those are bitmaps. The transfer is therefore
/// also aborted with a [NotSvgException] as soon as the first bytes show
/// that the body is not markup, instead of reading megabytes of a photo.
class DioSvgFetcher {
  DioSvgFetcher(
    this._dio, {
    this.maxBytes = kMaxSvgBytes,
    this.deadline = const Duration(seconds: 30),
  });

  final Dio _dio;
  final int maxBytes;
  final Duration deadline;

  Future<Uint8List> call(
    Uri uri,
    Map<String, String> headers,
    CancelToken cancel,
  ) =>
      _download(uri, headers, cancel).timeout(deadline, onTimeout: () {
        cancel.cancel('SVG download deadline');
        throw const SvgException('SVG download timed out');
      });

  Future<Uint8List> _download(
    Uri uri,
    Map<String, String> headers,
    CancelToken cancel,
  ) async {
    final response = await _dio.getUri<ResponseBody>(
      uri,
      cancelToken: cancel,
      options: Options(headers: headers, responseType: ResponseType.stream),
    );
    final body = response.data;
    if (body == null) return Uint8List(0);
    final bytes = BytesBuilder(copy: false);
    var start = SvgStart.undecided;
    await for (final chunk in body.stream) {
      if (start == SvgStart.undecided) start = classifySvgStart(chunk);
      if (start == SvgStart.other) {
        cancel.cancel('Not an SVG document');
        throw const NotSvgException();
      }
      bytes.add(chunk);
      if (bytes.length > maxBytes) {
        cancel.cancel('SVG too large');
        throw const SvgException('SVG too large');
      }
    }
    return bytes.takeBytes();
  }
}

/// Dio client for SVG downloads.
///
/// It has no interceptors on purpose: the caller decides which headers a
/// request carries, so a public share never receives account credentials.
/// The timeouts keep a stalled connection from showing a spinner forever.
final svgDioProvider = Provider<Dio>((ref) {
  final dio = Dio(BaseOptions(
    connectTimeout: const Duration(seconds: 15),
    receiveTimeout: const Duration(seconds: 15),
  ));
  ref.onDispose(dio.close);
  return dio;
});

/// The fetcher used by the SVG image widgets. Widget tests override it to
/// serve documents without a network.
final svgFetcherProvider = Provider<SvgFetcher>(
    (ref) => DioSvgFetcher(ref.watch(svgDioProvider)).call);
