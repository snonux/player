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
/// include the credentials.) Work still in flight at that moment notices
/// through the caches' generation counters and does not store its result.
///
/// The root widget watches this provider, which keeps the two listeners
/// alive for the lifetime of the app. It is separate from the caches
/// themselves so that showing an SVG does not require an auth setup, as on
/// the public share screens.
final svgCacheResetProvider = Provider<void>((ref) {
  void clear() {
    ref.read(svgMemoryCacheProvider).clear();
    ref.read(svgImageCacheProvider).reset();
  }

  ref.listen(authStateProvider, (_, __) => clear());
  ref.listen(playerBaseUrlProvider, (_, __) => clear());
});

/// Downloads and compiles each SVG once, however many boxes ask for it.
final svgSourceLoaderProvider = Provider<SvgSourceLoader>((ref) {
  return SvgSourceLoader(
    ref.watch(svgMemoryCacheProvider),
    ref.watch(svgFetcherProvider),
    ref.watch(svgCompilerProvider),
  );
});

/// One download and compilation in progress, shared by its [waiters].
class _Pending {
  _Pending(this.cancel);

  final CancelToken cancel;
  late final Future<CompiledSvg> future;
  int waiters = 0;
}

/// A caller's share of a download: the result, and how to give it up.
typedef SvgLoad = ({Future<CompiledSvg> compiled, void Function() release});

/// Loads compiled SVGs, sharing work between requests for the same source.
///
/// A grid tile and the detail banner, or a tile and the viewer, often ask
/// for the same drawing at different sizes at the same moment. They share
/// one download and one compilation here; the transfer is cancelled only
/// when every one of them has given up.
class SvgSourceLoader {
  SvgSourceLoader(this._cache, this._fetch, this._compile);

  final SvgMemoryCache _cache;
  final SvgFetcher _fetch;
  final SvgCompiler _compile;
  final Map<SvgRequest, _Pending> _pending = {};

  /// The compiled drawing for [request], from the cache or a (possibly
  /// already running) download. Call `release` when it is no longer wanted.
  SvgLoad load(SvgRequest request) {
    final cached = _cache.get(request);
    if (cached != null) return (compiled: Future.value(cached), release: () {});
    final pending = _pending[request] ??= _start(request);
    pending.waiters++;
    var released = false;
    return (
      compiled: pending.future,
      release: () {
        if (released) return;
        released = true;
        if (--pending.waiters == 0) pending.cancel.cancel('SVG not shown');
      },
    );
  }

  _Pending _start(SvgRequest request) {
    final pending = _Pending(CancelToken());
    pending.future = _download(request, pending)
        .whenComplete(() => _pending.remove(request));
    // Waiters handle the error; an abandoned download has none left.
    pending.future.ignore();
    return pending;
  }

  /// Fetches and compiles [request]. Content that is not SVG is noted in
  /// the cache, whether the downloader noticed it in the first bytes or the
  /// complete body fails the check.
  Future<CompiledSvg> _download(SvgRequest request, _Pending pending) async {
    bool abandoned() => pending.waiters == 0;
    if (_cache.isKnownNotSvg(request)) throw const NotSvgException();
    final generation = _cache.generation;
    try {
      final bytes = await _fetch(request.uri, request.headers, pending.cancel);
      // A transport may deliver a body despite the cancelled token.
      if (abandoned()) throw const SvgException('SVG request cancelled');
      if (!looksLikeSvg(bytes)) throw const NotSvgException();
      return await _compile(bytes, isCancelled: abandoned);
    } on NotSvgException {
      if (_cache.generation == generation) _cache.markNotSvg(request);
      rethrow;
    }
  }
}

/// Downloads, compiles and rasterises an SVG into a bitmap for one box.
///
/// All fallible work happens here rather than in a `VectorGraphic` widget:
/// that widget rasterises the drawing at the size the file declares (so a
/// file can ask for gigabytes), and it reports loader failures as unhandled
/// errors. Here every failure is an ordinary provider error that the widget
/// maps to its error state, and the bitmap has the size of the box it is
/// shown in, never more than `SvgImageRequest.maxSide` pixels a side.
///
/// The bitmap lives while a widget shows it. Disposing the provider gives
/// up its share of a running download.
final svgImageProvider =
    FutureProvider.autoDispose.family<SvgRaster, SvgImageRequest>(
  (ref, request) async {
    var disposed = false;
    ref.onDispose(() => disposed = true);
    final images = ref.watch(svgImageCacheProvider);
    final compiledCache = ref.watch(svgMemoryCacheProvider);
    final loader = ref.watch(svgSourceLoaderProvider);
    final generations = (compiledCache.generation, images.generation);

    var raster = images.get(request);
    if (raster == null) {
      final load = loader.load(request.source);
      ref.onDispose(load.release);
      final compiled = await load.compiled;
      raster = await rasterizeSvg(compiled, request);
      // Stored only now, so that bytes that fail to decode are never
      // cached, and only if no logout emptied the caches in the meantime.
      if (generations == (compiledCache.generation, images.generation)) {
        compiledCache.put(request.source, compiled);
        images.put(request, raster);
      }
    }
    if (disposed) {
      raster.dispose();
      throw const SvgException('SVG request cancelled');
    }
    ref.onDispose(raster.dispose);
    return raster;
  },
);
