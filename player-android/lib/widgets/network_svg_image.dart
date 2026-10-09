import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers/svg_image_provider.dart';

/// Builders shared by the bitmap and SVG image widgets; the signatures are
/// those of `CachedNetworkImage`.
typedef ImagePlaceholderBuilder = Widget Function(BuildContext, String);
typedef ImageErrorBuilder = Widget Function(BuildContext, String, Object);

/// Shows an SVG document from the network.
///
/// The drawing is rasterised once into a bitmap that matches the box it is
/// shown in (times the device pixel ratio, at most
/// `SvgImageRequest.maxSide` pixels a side) and that bitmap is painted. It
/// is sharp at the size it is shown; zooming in the image viewer enlarges
/// the bitmap.
///
/// [errorWidget] is shown for a failed download and for every document
/// that is rejected: not SVG, malformed, unusable size, nothing to draw,
/// too large or expensive, or using anything outside the grammar of simple
/// vector features (see `svg_document.dart` and `svg_gate.dart`).
///
/// Sizing: an SVG has no pixel size, so without [width]/[height] the
/// drawing takes all the space its parent offers and is placed in it
/// according to [fit]. Only under unbounded constraints does it fall back
/// to the size declared in the document.
class NetworkSvgImage extends StatelessWidget {
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
  Widget build(BuildContext context) {
    final uri = Uri.tryParse(imageUrl);
    if (uri == null) {
      return errorWidget(
          context, imageUrl, const FormatException('Invalid image URL'));
    }
    final source = SvgRequest(uri: uri, headers: headers);
    final pixelRatio = MediaQuery.devicePixelRatioOf(context);
    // The bitmap is made for the box, so the box must be known first.
    return LayoutBuilder(builder: (context, constraints) {
      final boxWidth = width ?? constraints.maxWidth;
      final boxHeight = height ?? constraints.maxHeight;
      return _SvgImage(
        widget: this,
        request: SvgImageRequest(
          source: source,
          width: boxWidth * pixelRatio,
          height: boxHeight * pixelRatio,
          cover: fit == BoxFit.cover,
        ),
        constraints: constraints,
      );
    });
  }
}

/// Watches the bitmap for one box and shows it, the placeholder or the
/// error widget.
class _SvgImage extends ConsumerWidget {
  const _SvgImage({
    required this.widget,
    required this.request,
    required this.constraints,
  });

  final NetworkSvgImage widget;
  final SvgImageRequest request;
  final BoxConstraints constraints;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final url = widget.imageUrl;
    return ref.watch(svgImageProvider(request)).when(
          loading: () => widget.placeholder(context, url),
          error: (error, _) => widget.errorWidget(context, url, error),
          data: (raster) {
            final size = _boxSize(raster.size);
            return SvgImageBox(
              raster: raster,
              fit: widget.fit ?? BoxFit.contain,
              width: size.width,
              height: size.height,
            );
          },
        );
  }

  /// Explicit sizes win, then the space on offer; a side that is neither
  /// given nor bounded follows the drawing's aspect ratio.
  Size _boxSize(Size drawing) {
    final w = widget.width ??
        (constraints.hasBoundedWidth ? constraints.maxWidth : null);
    final h = widget.height ??
        (constraints.hasBoundedHeight ? constraints.maxHeight : null);
    final aspect = drawing.width / drawing.height;
    if (w != null && h != null) return Size(w, h);
    if (w != null) return Size(w, w / aspect);
    if (h != null) return Size(h * aspect, h);
    return drawing;
  }
}

/// Paints the bitmap of a rasterised SVG into a box of [width] x [height].
class SvgImageBox extends StatelessWidget {
  const SvgImageBox({
    super.key,
    required this.raster,
    required this.fit,
    required this.width,
    required this.height,
  });

  final SvgRaster raster;
  final BoxFit fit;
  final double width;
  final double height;

  @override
  Widget build(BuildContext context) => ClipRect(
        child: RawImage(
          image: raster.image,
          width: width,
          height: height,
          fit: fit,
          filterQuality: FilterQuality.medium,
        ),
      );
}

/// The runtime type of a bare `Exception('...')`.
final Type _plainExceptionType = Exception().runtimeType;

/// True when [error] from a bitmap loader means "the bytes are not a
/// bitmap this device can decode".
///
/// The engine's image codec reports that as a bare `Exception` ("Invalid
/// image data"). Transport failures all have types of their own
/// (`HttpException`, `SocketException`, `NetworkImageLoadException`, and
/// `ClientException` from package:http, which is what a dropped connection
/// becomes inside the image cache). They say nothing about the format, and
/// probing them for SVG would only repeat a request that just failed, so
/// only the exact decoder error qualifies. Comparing the type avoids both
/// parsing the message and importing a package the app does not depend on.
bool isBitmapDecodeFailure(Object error) =>
    error.runtimeType == _plainExceptionType;

/// Error handler for bitmap loaders showing an image of unknown type.
///
/// Folder and set covers and some public shares have no file name, so an
/// SVG among them is first handed to the bitmap decoder and fails there.
/// For such a failure this returns a [NetworkSvgImage], which downloads the
/// file again and accepts it only if its content is SVG. A file found not
/// to be SVG is remembered for the session, so a corrupt bitmap is probed
/// once and not on every appearance. For transport errors this returns
/// [errorWidget] right away.
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
