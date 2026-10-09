import 'dart:collection';
import 'dart:ui' as ui;

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:vector_graphics/vector_graphics.dart';

import '../services/svg_compiler.dart';
import '../services/svg_document.dart';
import '../services/svg_fetcher.dart';

/// Most pixels all bitmaps embedded in one SVG may decode to (4 bytes each).
/// vector_graphics decodes them at full size on the UI isolate, so a small
/// file claiming a gigantic bitmap is rejected before that happens.
const int kMaxSvgEmbeddedPixels = 16 * 1024 * 1024;

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

/// Keeps recently compiled SVGs in memory, least recently used first out.
///
/// A grid card that scrolls away releases its picture; without this cache
/// scrolling back would download and parse the file again. Only successful
/// compilations are stored, so a failed image is retried on the next visit.
/// There is deliberately no disk cache: the one used for bitmaps belongs to
/// a package this app does not depend on directly, and a second private one
/// would need its own eviction and per-account separation for small files.
class SvgMemoryCache {
  SvgMemoryCache({this.maxBytes = 16 * 1024 * 1024});

  final int maxBytes;
  int _bytes = 0;
  final LinkedHashMap<SvgRequest, ByteData> _entries = LinkedHashMap();

  /// Returns the entry and marks it as most recently used.
  ByteData? get(SvgRequest request) {
    final data = _entries.remove(request);
    if (data != null) _entries[request] = data;
    return data;
  }

  void put(SvgRequest request, ByteData data) {
    // One oversized drawing must not push everything else out.
    if (data.lengthInBytes > maxBytes ~/ 4) return;
    _bytes -= _entries.remove(request)?.lengthInBytes ?? 0;
    _entries[request] = data;
    _bytes += data.lengthInBytes;
    while (_bytes > maxBytes) {
      _bytes -= _entries.remove(_entries.keys.first)!.lengthInBytes;
    }
  }
}

final svgMemoryCacheProvider = Provider<SvgMemoryCache>((ref) {
  return SvgMemoryCache();
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

    var data = cache.get(request);
    if (data == null) {
      final bytes = await fetch(request.uri, request.headers, cancel);
      // A transport may deliver a body despite the cancelled token.
      if (disposed) throw const SvgException('SVG request cancelled');
      final compiled = await compile(bytes, isCancelled: () => disposed);
      await checkEmbeddedImages(compiled.images);
      data = ByteData.sublistView(compiled.data);
      cache.put(request, data);
    }
    final info = await decodeSvgPicture(data);
    if (disposed) {
      info.picture.dispose();
      throw const SvgException('SVG request cancelled');
    }
    ref.onDispose(info.picture.dispose);
    return info;
  },
);

/// Rejects embedded bitmaps that are not decodable or decode to too many
/// pixels. Only the image headers are read here.
Future<void> checkEmbeddedImages(List<Uint8List> images) async {
  var pixels = 0;
  for (final encoded in images) {
    final buffer = await ui.ImmutableBuffer.fromUint8List(encoded);
    try {
      final descriptor = await ui.ImageDescriptor.encoded(buffer);
      pixels += descriptor.width * descriptor.height;
      descriptor.dispose();
    } on SvgException {
      rethrow;
    } catch (_) {
      throw const SvgException('SVG has an unreadable embedded image');
    } finally {
      buffer.dispose();
    }
    if (pixels > kMaxSvgEmbeddedPixels) {
      throw const SvgException('SVG has an embedded image that is too large');
    }
  }
}

/// Turns compiled SVG bytes into a picture clipped to the SVG's view box.
///
/// An embedded bitmap that fails to decode is an error in every build mode;
/// vector_graphics alone would assert in debug builds and silently leave
/// the bitmap out in release builds.
Future<PictureInfo> decodeSvgPicture(ByteData data) async {
  Object? imageError;
  final info = await vg.loadPicture(
    _CompiledSvgLoader(data),
    null,
    onError: (error, _) => imageError = error,
  );
  if (imageError != null) {
    info.picture.dispose();
    throw const SvgException('SVG has an unreadable embedded image');
  }
  return info;
}

/// Hands already compiled bytes to vector_graphics. Identity-based equality
/// keeps the decoder's bookkeeping for different drawings apart.
class _CompiledSvgLoader extends BytesLoader {
  const _CompiledSvgLoader(this.data);

  final ByteData data;

  @override
  Future<ByteData> loadBytes(BuildContext? context) =>
      SynchronousFuture<ByteData>(data);
}
