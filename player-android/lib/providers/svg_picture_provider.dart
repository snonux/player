import 'dart:collection';

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:vector_graphics/vector_graphics.dart';

import '../services/svg_compiler.dart';
import '../services/svg_document.dart';
import '../services/svg_fetcher.dart';
import 'api_client_provider.dart';
import 'auth_state_provider.dart';

/// One SVG download: where from and with which credentials.
///
/// Two requests are equal only when URL and headers both match, so another
/// account (or a public share with another token) never reuses a picture.
/// [toString] is redacted because the URL may hold a share token and the
/// headers hold credentials, and Riverpod prints family arguments in logs.
@immutable
class SvgRequest {
  const SvgRequest({required this.uri, this.headers = const {}});

  final Uri uri;
  final Map<String, String> headers;

  @override
  bool operator ==(Object other) =>
      other is SvgRequest &&
      other.uri == uri &&
      mapEquals(other.headers, headers);

  @override
  int get hashCode => Object.hash(
        uri,
        Object.hashAllUnordered(
          headers.entries.map((e) => Object.hash(e.key, e.value)),
        ),
      );

  @override
  String toString() =>
      'SvgRequest(${uri.host}, #${hashCode.toRadixString(16)})';
}

/// Remembers, in memory, what recent SVG requests turned out to be.
///
/// Compiled drawings are kept least-recently-used first out: a grid card
/// that scrolls away releases its picture, and without this scrolling back
/// would download and parse the file again. Every drawing the compiler
/// accepts fits (see `SvgLimits.maxCompiledBytes`).
///
/// Requests whose content was not SVG at all are remembered too. Images
/// without a file name are probed for SVG content after the bitmap decoder
/// rejected them; without this note a corrupt bitmap would be downloaded
/// again every time it scrolls into view.
///
/// Download failures and rejected SVG documents are not remembered, so they
/// are retried on the next visit. A drawing is stored only after it was
/// decoded successfully, so the cache never holds bytes that cannot be
/// shown.
///
/// There is deliberately no disk cache: the one used for bitmaps belongs to
/// a package this app does not depend on directly, and a second private
/// one would need its own eviction and per-account separation for small
/// files.
class SvgMemoryCache {
  SvgMemoryCache({this.maxBytes = 16 * 1024 * 1024, this.maxNotSvg = 512});

  final int maxBytes;
  final int maxNotSvg;
  int _bytes = 0;
  final LinkedHashMap<SvgRequest, ByteData> _entries = LinkedHashMap();
  final LinkedHashSet<SvgRequest> _notSvg = LinkedHashSet();

  /// Returns the entry and marks it as most recently used.
  ByteData? get(SvgRequest request) {
    final data = _entries.remove(request);
    if (data != null) _entries[request] = data;
    return data;
  }

  void put(SvgRequest request, ByteData data) {
    _bytes -= _entries.remove(request)?.lengthInBytes ?? 0;
    _entries[request] = data;
    _bytes += data.lengthInBytes;
    while (_bytes > maxBytes) {
      _bytes -= _entries.remove(_entries.keys.first)!.lengthInBytes;
    }
  }

  bool isKnownNotSvg(SvgRequest request) => _notSvg.contains(request);

  void markNotSvg(SvgRequest request) {
    _notSvg.add(request);
    if (_notSvg.length > maxNotSvg) _notSvg.remove(_notSvg.first);
  }

  /// Forgets everything.
  void clear() {
    _entries.clear();
    _notSvg.clear();
    _bytes = 0;
  }
}

/// The app-wide cache. See [svgCacheResetProvider] for when it is emptied.
final svgMemoryCacheProvider =
    Provider<SvgMemoryCache>((ref) => SvgMemoryCache());

/// Empties the SVG cache the moment the auth state or the server changes,
/// so drawings fetched for one account do not stay in memory after logout.
/// (They could not be shown to another account anyway: the cache keys
/// include the credentials.)
///
/// The root widget watches this provider, which keeps the two listeners
/// alive for the lifetime of the app. It is separate from the cache itself
/// so that showing an SVG does not require an auth setup, as on the public
/// share screens.
final svgCacheResetProvider = Provider<void>((ref) {
  void clear() => ref.read(svgMemoryCacheProvider).clear();
  ref.listen(authStateProvider, (_, __) => clear());
  ref.listen(playerBaseUrlProvider, (_, __) => clear());
});

/// Downloads, compiles and decodes an SVG into a picture ready to paint.
///
/// All fallible work happens here rather than in a `VectorGraphic` widget:
/// that widget rasterises the drawing at its declared size (so a file can
/// ask for gigabytes), and it reports loader failures as unhandled errors.
/// Here every failure is an ordinary provider error that the widget maps to
/// its error state, and the result is a `ui.Picture` that is replayed at any
/// scale without an intermediate bitmap.
///
/// The picture lives while a widget shows it. Disposing the provider
/// cancels a running download and skips a compilation that has not started.
final svgPictureProvider =
    FutureProvider.autoDispose.family<PictureInfo, SvgRequest>(
  (ref, request) async {
    final cancel = CancelToken();
    var disposed = false;
    ref.onDispose(() {
      disposed = true;
      cancel.cancel('SVG no longer shown');
    });
    final cache = ref.watch(svgMemoryCacheProvider);
    final fetch = ref.watch(svgFetcherProvider);
    final compile = ref.watch(svgCompilerProvider);

    final cached = cache.get(request);
    final data = cached ??
        await _download(cache, request, fetch, compile, cancel, () => disposed);
    final info = await decodeSvgPicture(data);
    // Stored only now: bytes that fail to decode must not be cached.
    if (cached == null) cache.put(request, data);
    if (disposed) {
      info.picture.dispose();
      throw const SvgException('SVG request cancelled');
    }
    ref.onDispose(info.picture.dispose);
    return info;
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

/// Turns compiled SVG bytes into a picture clipped to the SVG's view box.
///
/// Decoding only records drawing commands: the features that would decode
/// or allocate bitmaps here (patterns, embedded images) cannot get this
/// far, see `svg_document.dart`.
Future<PictureInfo> decodeSvgPicture(ByteData data) =>
    vg.loadPicture(_CompiledSvgLoader(data), null);

/// Hands already compiled bytes to vector_graphics. Identity-based equality
/// keeps the decoder's bookkeeping for different drawings apart.
class _CompiledSvgLoader extends BytesLoader {
  const _CompiledSvgLoader(this.data);

  final ByteData data;

  @override
  Future<ByteData> loadBytes(BuildContext? context) =>
      SynchronousFuture<ByteData>(data);
}
