import 'package:flutter/material.dart';

import '../services/svg_document.dart';
import 'network_svg_image.dart';

/// Image from a public share URL, loaded without any credentials: the share
/// token in the URL is the only secret, so neither bearer token nor session
/// cookie is attached.
///
/// A file known to be SVG ([sourceName] or the URL, see [isSvgSource]) is
/// drawn as a vector. Anything else goes to the bitmap decoder; if that
/// cannot decode it, the content is checked for SVG before [errorWidget] is
/// shown (see [bitmapErrorOrSvg]). This covers a share opened without its
/// file name, for example after the route was restored.
class PublicNetworkImage extends StatelessWidget {
  const PublicNetworkImage({
    super.key,
    required this.imageUrl,
    required this.placeholder,
    required this.errorWidget,
    this.sourceName,
    this.fit,
  });

  final String imageUrl;
  final ImagePlaceholderBuilder placeholder;
  final ImageErrorBuilder errorWidget;

  /// File name of the shared media, when known.
  final String? sourceName;
  final BoxFit? fit;

  @override
  Widget build(BuildContext context) {
    if (isSvgSource(fileName: sourceName, url: imageUrl)) {
      return NetworkSvgImage(
        imageUrl: imageUrl,
        fit: fit,
        placeholder: placeholder,
        errorWidget: errorWidget,
      );
    }
    return Image.network(
      imageUrl,
      fit: fit,
      loadingBuilder: (context, child, progress) =>
          progress == null ? child : placeholder(context, imageUrl),
      errorBuilder: (context, error, _) => bitmapErrorOrSvg(
        context,
        error: error,
        imageUrl: imageUrl,
        fit: fit,
        placeholder: placeholder,
        errorWidget: errorWidget,
      ),
    );
  }
}
