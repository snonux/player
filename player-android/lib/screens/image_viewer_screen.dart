import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/models.dart';
import '../providers/api_client_provider.dart';
import '../widgets/authenticated_network_image.dart';
import '../widgets/network_svg_image.dart';

/// Shows an original image with pinch zoom.
///
/// Library images load through the authenticated media stream after their
/// metadata confirms the image type. Public shares load the share stream
/// directly, never calling account APIs or sending credentials.
///
/// SVG images are drawn as vectors; an image of any type that cannot be
/// downloaded or decoded shows a labelled error instead of a blank screen.
class ImageViewerScreen extends ConsumerStatefulWidget {
  const ImageViewerScreen({
    super.key,
    required this.mediaId,
    this.imageUrl,
    this.mediaTitle,
    this.isPublicShare = false,
  });

  final String mediaId;
  final String? imageUrl;

  /// Readable file name shown in the app bar; falls back to a generic title.
  /// A public share has no metadata request, so its `.svg` extension is also
  /// what selects the vector renderer there.
  final String? mediaTitle;

  /// True for `/s/:token/image`, where [imageUrl] is the public share stream.
  final bool isPublicShare;

  @override
  ConsumerState<ImageViewerScreen> createState() => _ImageViewerScreenState();
}

class _ImageViewerScreenState extends ConsumerState<ImageViewerScreen> {
  Future<Media>? _media;

  @override
  void initState() {
    super.initState();
    if (widget.isPublicShare) return;
    final id = int.tryParse(widget.mediaId);
    if (id != null) {
      _media = ref.read(apiClientProvider).getMedia(id);
    }
  }

  @override
  Widget build(BuildContext context) {
    final fallbackTitle = widget.isPublicShare ? 'Shared Image' : 'Image';
    return Scaffold(
      appBar: AppBar(title: Text(widget.mediaTitle ?? fallbackTitle)),
      backgroundColor: Colors.black,
      body: widget.isPublicShare ? _publicImage() : _libraryImage(),
    );
  }

  /// Loads media metadata first so non-image IDs show an error, not a
  /// broken decoder.
  Widget _libraryImage() {
    if (_media == null) return const _UnavailableImage();
    return FutureBuilder<Media>(
      future: _media,
      builder: (context, snapshot) {
        if (snapshot.connectionState != ConnectionState.done) {
          return const _ImageLoading();
        }
        if (snapshot.hasError || snapshot.data?.type != 'image') {
          return const _UnavailableImage();
        }
        final url = widget.imageUrl ??
            ref.read(apiClientProvider).streamUrl(snapshot.data!.id);
        return _ZoomableImage(
          child: AuthenticatedNetworkImage(
            imageUrl: url,
            sourceName: snapshot.data!.fileName,
            fit: BoxFit.contain,
            placeholder: (_, __) => const _ImageLoading(),
            errorWidget: (_, __, ___) => const _UnavailableImage(),
          ),
        );
      },
    );
  }

  /// Public shares use a plain network image: the token in the URL is the
  /// only credential, so no session cookie or bearer token is attached.
  Widget _publicImage() {
    final url = widget.imageUrl;
    if (url == null) return const _UnavailableImage();
    if (isSvgSource(fileName: widget.mediaTitle, url: url)) {
      return _ZoomableImage(
        child: NetworkSvgImage(
          imageUrl: url,
          fit: BoxFit.contain,
          placeholder: (_, __) => const _ImageLoading(),
          errorWidget: (_, __, ___) => const _UnavailableImage(),
        ),
      );
    }
    return _ZoomableImage(
      child: Image.network(
        url,
        fit: BoxFit.contain,
        loadingBuilder: (_, child, progress) =>
            progress == null ? child : const _ImageLoading(),
        errorBuilder: (_, __, ___) => const _UnavailableImage(),
      ),
    );
  }
}

class _ZoomableImage extends StatelessWidget {
  const _ZoomableImage({required this.child});

  final Widget child;

  /// The child fills the viewport so every image is laid out in the whole
  /// screen: an SVG with a small intrinsic size (an icon, say) is scaled up
  /// like a bitmap instead of staying tiny, and the loading and error states
  /// stay centred.
  @override
  Widget build(BuildContext context) => InteractiveViewer(
        key: const Key('image_viewer_zoom'),
        minScale: 0.5,
        maxScale: 5,
        child: SizedBox.expand(child: child),
      );
}

class _ImageLoading extends StatelessWidget {
  const _ImageLoading();

  @override
  Widget build(BuildContext context) =>
      const Center(child: CircularProgressIndicator(color: Colors.white));
}

/// Error state for an image that is missing, not an image, or undecodable.
///
/// Icon and text are white because the viewer background is always black.
class _UnavailableImage extends StatelessWidget {
  const _UnavailableImage();

  @override
  Widget build(BuildContext context) => const Center(
        key: Key('image_viewer_error'),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.broken_image_outlined, color: Colors.white, size: 64),
            SizedBox(height: 12),
            Text('Image unavailable', style: TextStyle(color: Colors.white)),
          ],
        ),
      );
}
