import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../services/svg_cache.dart';
import '../services/svg_compiler.dart';
import '../services/svg_document.dart';
import '../services/svg_raster.dart';

final _offlineSvgProvider = FutureProvider.autoDispose
    .family<SvgRaster, SvgImageRequest>((ref, request) async {
  var disposed = false;
  ref.onDispose(() => disposed = true);
  final compile = ref.read(svgCompilerProvider);
  final handle = await File.fromUri(request.source.uri).open();
  final bytes = await handle.read(kMaxSvgBytes + 1).whenComplete(handle.close);
  if (disposed) throw const SvgException('Image closed');
  final compiled = await compile(bytes, isCancelled: () => disposed);
  if (disposed) throw const SvgException('Image closed');
  final raster = await rasterizeSvg(compiled, request);
  if (disposed) {
    raster.dispose();
    throw const SvgException('Image closed');
  }
  ref.onDispose(raster.dispose);
  return raster;
});

/// Reads only the private file. SVGs use the same size and complexity gate as
/// network images, so saving an image never bypasses the untrusted-file checks.
class OfflineImage extends ConsumerWidget {
  const OfflineImage({super.key, required this.uri, required this.title});
  final Uri uri;
  final String title;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    const error = Center(
        child:
            Text('Image unavailable', style: TextStyle(color: Colors.white)));
    return InteractiveViewer(
      minScale: 0.5,
      maxScale: 8,
      child: !isSvgSource(fileName: title, url: uri.toString())
          ? Center(
              child: Image.file(File.fromUri(uri),
                  fit: BoxFit.contain, errorBuilder: (_, __, ___) => error))
          : LayoutBuilder(builder: (context, constraints) {
              final ratio = MediaQuery.devicePixelRatioOf(context);
              final request = SvgImageRequest(
                  source: SvgRequest(uri: uri),
                  width: constraints.maxWidth * ratio,
                  height: constraints.maxHeight * ratio);
              return ref.watch(_offlineSvgProvider(request)).when(
                    loading: () =>
                        const Center(child: CircularProgressIndicator()),
                    error: (_, __) => error,
                    data: (raster) => Center(
                        child: RawImage(
                            image: raster.image,
                            width: constraints.maxWidth,
                            height: constraints.maxHeight,
                            fit: BoxFit.contain)),
                  );
            }),
    );
  }
}
