import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../services/svg_cache.dart';
import '../services/svg_compiler.dart';
import '../services/svg_document.dart';
import '../services/svg_fetcher.dart';
import '../services/svg_raster.dart';
import 'api_client_provider.dart';
import 'auth_state_provider.dart';

export '../services/svg_cache.dart';

/// The app-wide caches. See [svgCacheResetProvider] for when they are
/// emptied.
final svgMemoryCacheProvider =
    Provider<SvgMemoryCache>((ref) => SvgMemoryCache());
final svgImageCacheProvider = Provider<SvgImageCache>((ref) {
  final cache = SvgImageCache();
  ref.onDispose(cache.clear);
  return cache;
});

/// Empties the SVG caches the moment the auth state or the server changes,
/// so drawings fetched for one account do not stay in memory after logout.
/// (They could not be shown to another account anyway: the cache keys
/// include the credentials.)
///
/// The root widget watches this provider, which keeps the two listeners
/// alive for the lifetime of the app. It is separate from the caches
/// themselves so that showing an SVG does not require an auth setup, as on
/// the public share screens.
final svgCacheResetProvider = Provider<void>((ref) {
  void clear() {
    ref.read(svgMemoryCacheProvider).clear();
    ref.read(svgImageCacheProvider).clear();
  }

  ref.listen(authStateProvider, (_, __) => clear());
  ref.listen(playerBaseUrlProvider, (_, __) => clear());
});

/// Downloads, compiles and rasterises an SVG into a bitmap for one box.
///
/// All fallible work happens here rather than in a `VectorGraphic` widget:
/// that widget rasterises the drawing at the size the file declares (so a
/// file can ask for gigabytes), and it reports loader failures as unhandled
/// errors. Here every failure is an ordinary provider error that the widget
/// maps to its error state, and the bitmap has the size of the box it is
/// shown in, never more than `SvgImageRequest.maxSide` pixels a side.
///
/// The bitmap lives while a widget shows it. Disposing the provider
/// cancels a running download and skips a compilation that has not started.
final svgImageProvider =
    FutureProvider.autoDispose.family<SvgRaster, SvgImageRequest>(
  (ref, request) async {
    final cancel = CancelToken();
    var disposed = false;
    ref.onDispose(() {
      disposed = true;
      cancel.cancel('SVG no longer shown');
    });
    final images = ref.watch(svgImageCacheProvider);
    final compiled = ref.watch(svgMemoryCacheProvider);
    final fetch = ref.watch(svgFetcherProvider);
    final compile = ref.watch(svgCompilerProvider);

    var raster = images.get(request);
    if (raster == null) {
      final source = request.source;
      final cached = compiled.get(source);
      final data = cached ??
          await _download(
              compiled, source, fetch, compile, cancel, () => disposed);
      raster = await rasterizeSvg(data, request);
      // Stored only now: bytes that fail to decode must not be cached.
      if (cached == null) compiled.put(source, data);
      images.put(request, raster);
    }
    if (disposed) {
      raster.dispose();
      throw const SvgException('SVG request cancelled');
    }
    ref.onDispose(raster.dispose);
    return raster;
  },
);

/// Fetches and compiles [request]. Content that is not SVG is noted in
/// [cache], whether the downloader noticed it in the first bytes or the
/// complete body fails the check.
Future<ByteData> _download(
  SvgMemoryCache cache,
  SvgRequest request,
  SvgFetcher fetch,
  SvgCompiler compile,
  CancelToken cancel,
  bool Function() isDisposed,
) async {
  if (cache.isKnownNotSvg(request)) throw const NotSvgException();
  try {
    final bytes = await fetch(request.uri, request.headers, cancel);
    // A transport may deliver a body despite the cancelled token.
    if (isDisposed()) throw const SvgException('SVG request cancelled');
    if (!looksLikeSvg(bytes)) throw const NotSvgException();
    final compiled = await compile(bytes, isCancelled: isDisposed);
    return ByteData.sublistView(compiled);
  } on NotSvgException {
    cache.markNotSvg(request);
    rethrow;
  }
}
