import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/models.dart';
import '../providers/api_client_provider.dart';
import '../widgets/authenticated_network_image.dart';

/// Shows the original image from the protected media stream endpoint.
class ImageViewerScreen extends ConsumerStatefulWidget {
  const ImageViewerScreen({super.key, required this.mediaId, this.imageUrl});

  final String mediaId;
  final String? imageUrl;

  @override
  ConsumerState<ImageViewerScreen> createState() => _ImageViewerScreenState();
}

class _ImageViewerScreenState extends ConsumerState<ImageViewerScreen> {
  Future<Media>? _media;

  @override
  void initState() {
    super.initState();
    final id = int.tryParse(widget.mediaId);
    if (id != null) {
      _media = ref.read(apiClientProvider).getMedia(id);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Image')),
      backgroundColor: Colors.black,
      body: _media == null
          ? const _UnavailableImage()
          : FutureBuilder<Media>(
              future: _media,
              builder: (context, snapshot) {
                if (snapshot.connectionState != ConnectionState.done) {
                  return const Center(
                    child: CircularProgressIndicator(color: Colors.white),
                  );
                }
                if (snapshot.hasError || snapshot.data?.type != 'image') {
                  return const _UnavailableImage();
                }

                final url = widget.imageUrl ??
                    ref.read(apiClientProvider).streamUrl(snapshot.data!.id);
                return Center(
                  child: InteractiveViewer(
                    key: const Key('image_viewer_zoom'),
                    minScale: 0.5,
                    maxScale: 5,
                    child: AuthenticatedNetworkImage(
                      imageUrl: url,
                      fit: BoxFit.contain,
                      placeholder: (_, __) => const Center(
                        child: CircularProgressIndicator(color: Colors.white),
                      ),
                      errorWidget: (_, __, ___) => const _UnavailableImage(),
                    ),
                  ),
                );
              },
            ),
    );
  }
}

class _UnavailableImage extends StatelessWidget {
  const _UnavailableImage();

  @override
  Widget build(BuildContext context) => const Center(
        child: Text('Image unavailable', style: TextStyle(color: Colors.white)),
      );
}
