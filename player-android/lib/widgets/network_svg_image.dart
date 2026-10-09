import 'dart:io';
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:vector_graphics/vector_graphics.dart' show PictureInfo;

import '../providers/svg_picture_provider.dart';

/// Builders shared by the bitmap and SVG image widgets; the signatures are
/// those of `CachedNetworkImage`.
typedef ImagePlaceholderBuilder = Widget Function(BuildContext, String);
typedef ImageErrorBuilder = Widget Function(BuildContext, String, Object);

/// Renders an SVG document from the network as a vector drawing.
///
/// [errorWidget] is shown for a failed download and for every document
/// that is rejected (not SVG, malformed, unusable size, nothing to draw,
/// too large or too complex; see `svg_document.dart`).
///
/// Sizing: an SVG has no pixel size, so without [width]/[height] the
/// drawing takes all the space its parent offers and is placed in it
/// according to [fit]. Only under unbounded constraints does it fall back
/// to the size declared in the document.
class NetworkSvgImage extends ConsumerWidget {
  const NetworkSvgImage({
    super.key,
    required this.imageUrl,
    required this.placeholder,
    required this.errorWidget,
    this.headers = const {},
    this.fit,
    this.width,
    this.height,
  });

  final String imageUrl;
  final ImagePlaceholderBuilder placeholder;
  final ImageErrorBuilder errorWidget;

  /// Request headers. Empty for public shares, whose URL holds the token.
  final Map<String, String> headers;
  final BoxFit? fit;
  final double? width;
  final double? height;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final uri = Uri.tryParse(imageUrl);
    if (uri == null) {
      return errorWidget(
          context, imageUrl, const FormatException('Invalid image URL'));
    }
    final request = SvgRequest(uri: uri, headers: headers);
    return ref.watch(svgPictureProvider(request)).when(
          loading: () => placeholder(context, imageUrl),
          error: (error, _) => errorWidget(context, imageUrl, error),
          data: (info) => SvgPictureBox(
            info: info,
            fit: fit ?? BoxFit.contain,
            width: width,
            height: height,
          ),
        );
  }
}

/// Paints a decoded SVG picture scaled into a box.
///
/// The picture is replayed by the canvas at the final scale: nothing is
/// rasterised up front, so the drawing stays sharp at any zoom level and its
/// declared size (which the file controls) never becomes a bitmap size.
class SvgPictureBox extends StatelessWidget {
  const SvgPictureBox({
    super.key,
    required this.info,
    required this.fit,
    this.width,
    this.height,
  });

  final PictureInfo info;
  final BoxFit fit;
  final double? width;
  final double? height;

  @override
  Widget build(BuildContext context) => LayoutBuilder(
        builder: (context, constraints) {
          final size = _boxSize(constraints);
          return SizedBox(
            width: size.width,
            height: size.height,
            child: FittedBox(
              fit: fit,
              clipBehavior: Clip.hardEdge,
              child: SizedBox.fromSize(
                size: info.size,
                child: CustomPaint(painter: _PicturePainter(info.picture)),
              ),
            ),
          );
        },
      );

  /// Explicit sizes win, then the space on offer; a side that is neither
  /// given nor bounded follows the drawing's aspect ratio.
  Size _boxSize(BoxConstraints constraints) {
    final w =
        width ?? (constraints.hasBoundedWidth ? constraints.maxWidth : null);
    final h =
        height ?? (constraints.hasBoundedHeight ? constraints.maxHeight : null);
    final aspect = info.size.width / info.size.height;
    if (w != null && h != null) return Size(w, h);
    if (w != null) return Size(w, w / aspect);
    if (h != null) return Size(h * aspect, h);
    return info.size;
  }
}

class _PicturePainter extends CustomPainter {
  const _PicturePainter(this.picture);

  final ui.Picture picture;

  @override
  void paint(Canvas canvas, Size size) => canvas.drawPicture(picture);

  @override
  bool shouldRepaint(_PicturePainter oldDelegate) =>
      !identical(oldDelegate.picture, picture);
}

/// True when [error] from a bitmap loader may mean "this is not a bitmap".
///
/// Transport failures (HTTP status, socket, TLS) say nothing about the
/// format, and retrying them as SVG would only repeat a request that just
/// failed. Anything else is treated as a decode failure.
bool isBitmapDecodeFailure(Object error) =>
    error is! IOException && error is! NetworkImageLoadException;

/// Error handler for bitmap loaders showing an image of unknown type.
///
/// Folder and set covers and some public shares have no file name, so an
/// SVG among them is first handed to the bitmap decoder and fails there.
/// For such a failure this returns a [NetworkSvgImage], which downloads the
/// file again and accepts it only if its content is SVG; otherwise, and for
/// transport errors, it returns [errorWidget] right away.
Widget bitmapErrorOrSvg(
  BuildContext context, {
  required Object error,
  required String imageUrl,
  required ImagePlaceholderBuilder placeholder,
  required ImageErrorBuilder errorWidget,
  Map<String, String> headers = const {},
  BoxFit? fit,
  double? width,
  double? height,
}) {
  if (!isBitmapDecodeFailure(error)) {
    return errorWidget(context, imageUrl, error);
  }
  return NetworkSvgImage(
    imageUrl: imageUrl,
    headers: headers,
    fit: fit,
    width: width,
    height: height,
    placeholder: placeholder,
    // Report the bitmap failure: it describes the image the caller asked for.
    errorWidget: (context, url, _) => errorWidget(context, url, error),
  );
}
