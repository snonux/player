import 'dart:convert';

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:vector_graphics/vector_graphics.dart';
import 'package:vector_graphics_compiler/vector_graphics_compiler.dart'
    show encodeSvg;

/// Downloads the raw bytes of an SVG document.
typedef SvgFetcher = Future<Uint8List> Function(
    Uri uri, Map<String, String> headers);

/// True when [fileName] or the path of [url] names an SVG document.
///
/// The server picks the media type from the file extension and returns the
/// original SVG for both the stream and the thumbnail of such a file. Those
/// endpoint URLs carry no extension, so callers pass the media file name;
/// the URL path is checked as well for links that do end in `.svg`.
/// Flutter's bitmap decoders cannot read SVG, so these images must take the
/// vector path instead of `Image`/`CachedNetworkImage`.
bool isSvgSource({String? fileName, required String url}) {
  bool hasSvgExtension(String? name) =>
      name != null && name.trim().toLowerCase().endsWith('.svg');
  return hasSvgExtension(fileName) || hasSvgExtension(Uri.tryParse(url)?.path);
}

/// Fetches SVG bytes with a bare Dio client.
///
/// The client has no interceptors on purpose: the caller decides which
/// headers a request carries, so a public share never receives account
/// credentials. A non-2xx status throws and becomes the image's error state.
/// Tests override this provider to serve documents without a network.
final svgFetcherProvider = Provider<SvgFetcher>((ref) {
  final dio = Dio();
  ref.onDispose(dio.close);
  return (uri, headers) async {
    final response = await dio.getUri<Uint8List>(
      uri,
      options: Options(headers: headers, responseType: ResponseType.bytes),
    );
    return response.data ?? Uint8List(0);
  };
});

/// One SVG download. Equality uses only [cacheKey], so widgets showing the
/// same image for the same account share one download and one parse.
@immutable
class SvgRequest {
  const SvgRequest({
    required this.uri,
    required this.cacheKey,
    this.headers = const {},
  });

  final Uri uri;
  final Map<String, String> headers;

  /// Must change when [headers] select another account. It must not hold
  /// credentials, as it ends up in provider debug output.
  final String cacheKey;

  @override
  bool operator ==(Object other) =>
      other is SvgRequest && other.cacheKey == cacheKey;

  @override
  int get hashCode => cacheKey.hashCode;
}

/// Downloads an SVG and compiles it to the vector_graphics binary format.
///
/// Both steps happen here rather than inside a vector_graphics loader:
/// `VectorGraphic` reports a failing loader as an unhandled async error in
/// addition to calling its error builder. Here a failed download or a
/// document that is not valid SVG is an ordinary provider error. The result
/// lives while a widget shows it; a later visit downloads again, which also
/// retries after a failure.
final compiledSvgProvider =
    FutureProvider.autoDispose.family<ByteData, SvgRequest>(
  (ref, request) async {
    final fetch = ref.watch(svgFetcherProvider);
    final bytes = await fetch(request.uri, request.headers);
    return compute(_compileSvg, bytes, debugLabel: 'Compile SVG');
  },
);

/// Parses SVG text; throws when the document is not valid SVG.
///
/// Runs in a background isolate because parsing a large drawing is slow.
/// The optimizers stay off: they need native libraries that only exist in
/// the build-time tooling, not in the app.
ByteData _compileSvg(Uint8List bytes) => encodeSvg(
      xml: utf8.decode(bytes, allowMalformed: true),
      debugName: 'network svg',
      enableMaskingOptimizer: false,
      enableClippingOptimizer: false,
      enableOverdrawOptimizer: false,
    ).buffer.asByteData();

/// Hands already compiled bytes to [VectorGraphic]; it cannot fail.
///
/// Equality is the identity of [bytes], so every widget fed by the same
/// [compiledSvgProvider] result shares one decoded picture.
class _CompiledSvgLoader extends BytesLoader {
  const _CompiledSvgLoader(this.bytes);

  final ByteData bytes;

  @override
  Future<ByteData> loadBytes(BuildContext? context) =>
      SynchronousFuture<ByteData>(bytes);

  @override
  bool operator ==(Object other) =>
      other is _CompiledSvgLoader && identical(other.bytes, bytes);

  @override
  int get hashCode => identityHashCode(bytes);
}

/// Renders an SVG document from the network.
///
/// [placeholder] and [errorWidget] use the same signatures as
/// `CachedNetworkImage`, so one pair of builders serves bitmaps and SVGs.
/// [errorWidget] is shown for a failed download as well as for a document
/// that is not valid SVG, so a broken image is never a blank area.
class NetworkSvgImage extends ConsumerWidget {
  const NetworkSvgImage({
    super.key,
    required this.imageUrl,
    required this.placeholder,
    required this.errorWidget,
    this.headers = const {},
    this.cacheKey,
    this.fit,
    this.width,
    this.height,
  });

  final String imageUrl;
  final Widget Function(BuildContext, String) placeholder;
  final Widget Function(BuildContext, String, Object) errorWidget;

  /// Request headers. Empty for public shares, whose URL holds the token.
  final Map<String, String> headers;

  /// See [SvgRequest.cacheKey]; defaults to [imageUrl], which is enough when
  /// no [headers] are sent.
  final String? cacheKey;
  final BoxFit? fit;
  final double? width;
  final double? height;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final uri = Uri.tryParse(imageUrl);
    if (uri == null) {
      return errorWidget(
          context, imageUrl, FormatException('Invalid image URL', imageUrl));
    }
    final request =
        SvgRequest(uri: uri, headers: headers, cacheKey: cacheKey ?? imageUrl);
    return ref.watch(compiledSvgProvider(request)).when(
          loading: () => placeholder(context, imageUrl),
          error: (error, _) => errorWidget(context, imageUrl, error),
          data: (bytes) => VectorGraphic(
            loader: _CompiledSvgLoader(bytes),
            fit: fit ?? BoxFit.contain,
            width: width,
            height: height,
            errorBuilder: (context, error, _) =>
                errorWidget(context, imageUrl, error),
          ),
        );
  }
}
