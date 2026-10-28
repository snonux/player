import 'package:flutter/material.dart';
import 'media_playback_navigation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../models/models.dart';
import '../providers/api_client_provider.dart';
import '../providers/personal_media_provider.dart';
import '../services/personal_media_store.dart';
import '../widgets/overflow_menu.dart';
import '../widgets/personal_media_actions.dart';
import '../widgets/authenticated_network_image.dart';
import '../widgets/public_network_image.dart';

/// Shows an original image with pinch zoom.
///
/// Library images load through the authenticated media stream after their
/// metadata confirms the image type. Public shares load the share stream
/// directly, never calling account APIs or sending credentials.
///
/// SVG images are rendered at the size of the screen and fill it; bitmaps
/// keep their own pixel size unless they are larger than the screen.
/// Zooming enlarges the pixels of either kind. An image of any
/// type that cannot be downloaded or decoded shows a labelled error instead
/// of a blank screen.
class ImageViewerScreen extends ConsumerStatefulWidget {
  const ImageViewerScreen({
    super.key,
    required this.mediaId,
    this.imageUrl,
    this.mediaTitle,
    this.isPublicShare = false,
    this.sequence = const [],
  });

  final String mediaId;
  final List<Media> sequence;
  final String? imageUrl;

  /// Readable file name shown in the app bar; falls back to a generic title.
  /// A public share has no metadata request, so this name is also the hint
  /// that the file is an SVG; without it the content decides.
  final String? mediaTitle;

  /// True for `/s/:token/image`, where [imageUrl] is the public share stream.
  final bool isPublicShare;

  @override
  ConsumerState<ImageViewerScreen> createState() => _ImageViewerScreenState();
}

class _ImageViewerScreenState extends ConsumerState<ImageViewerScreen> {
  final _transform = TransformationController();
  late String _currentId;
  String? _currentTitle;
  String? _currentUrl;
  Offset? _swipeStart;
  Offset? _swipeEnd;
  final _pointers = <int>{};
  bool _canSwipe = false;
  Future<Media>? _media;

  List<Media> get _images =>
      widget.sequence.where((m) => m.type == 'image').toList();
  int get _index => _images.indexWhere((m) => m.id.toString() == _currentId);

  void _resetCurrent() {
    _currentId = widget.mediaId;
    _currentTitle = widget.mediaTitle;
    _currentUrl = widget.imageUrl;
    _transform.value = Matrix4.identity();
    _resolveMedia();
  }

  void _navigate(int delta) {
    final images = _images;
    final index = _index;
    final next = index + delta;
    if (widget.isPublicShare ||
        index < 0 ||
        next < 0 ||
        next >= images.length) {
      return;
    }
    final media = images[next];
    setState(() {
      _currentId = media.id.toString();
      _currentTitle = media.displayTitle;
      _currentUrl = null;
      _transform.value = Matrix4.identity();
      _resolveMedia();
    });
  }

  void _zoom(double factor) {
    final current = _transform.value.getMaxScaleOnAxis();
    final target = (current * factor).clamp(0.5, 5.0);
    _transform.value = _transform.value.clone()
      ..scaleByDouble(target / current, target / current, 1, 1);
  }

  @override
  void dispose() {
    _transform.dispose();
    super.dispose();
  }

  PersonalMediaStore? _personalStore;

  @override
  void initState() {
    super.initState();
    _resetCurrent();
  }

  @override
  void didUpdateWidget(covariant ImageViewerScreen oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.mediaId != widget.mediaId ||
        oldWidget.isPublicShare != widget.isPublicShare ||
        oldWidget.imageUrl != widget.imageUrl ||
        oldWidget.mediaTitle != widget.mediaTitle) {
      _resetCurrent();
    }
  }

  void _resolveMedia() {
    _media = null;
    _personalStore = null;
    if (widget.isPublicShare) return;
    final id = int.tryParse(_currentId);
    if (id != null) {
      _media = ref.read(apiClientProvider).getMedia(id);
      _personalStore = serverPersonalStore(ref, id);
    }
  }

  @override
  Widget build(BuildContext context) {
    final fallbackTitle = widget.isPublicShare ? 'Shared Image' : 'Image';
    return Scaffold(
      appBar: AppBar(title: Text(_currentTitle ?? fallbackTitle), actions: [
        if (_personalStore != null) FavoriteButton(store: _personalStore!),
        OverflowMenuButton(
          items: (_) => [
            OverflowMenuItem(
                icon: Icons.zoom_in,
                label: 'Zoom in',
                onSelected: () => _zoom(1.25)),
            OverflowMenuItem(
                icon: Icons.zoom_out,
                label: 'Zoom out',
                onSelected: () => _zoom(0.8)),
            OverflowMenuItem(
                icon: Icons.fit_screen,
                label: 'Reset zoom',
                onSelected: () => _transform.value = Matrix4.identity()),
            if (!widget.isPublicShare && int.tryParse(_currentId) != null)
              OverflowMenuItem(
                  key: const Key('playback_details_button'),
                  icon: Icons.description_outlined,
                  label: 'Details',
                  onSelected: () =>
                      openPlaybackDetails(context, int.parse(_currentId))),
            if (_personalStore case final store?)
              OverflowMenuItem(
                  icon: Icons.notes_outlined,
                  label: 'Notes',
                  onSelected: () => openPersonalNotes(
                      context, store, _currentTitle ?? fallbackTitle)),
          ],
        ),
      ]),
      backgroundColor: Colors.black,
      body: Listener(
        behavior: HitTestBehavior.opaque,
        onPointerDown: (event) {
          _pointers.add(event.pointer);
          if (_pointers.length == 1) {
            _swipeStart = _swipeEnd = event.position;
            _canSwipe = _transform.value.getMaxScaleOnAxis() <= 1.01;
          } else {
            _canSwipe = false;
          }
        },
        onPointerMove: (event) => _swipeEnd = event.position,
        onPointerCancel: (event) {
          _pointers.remove(event.pointer);
          _canSwipe = false;
        },
        onPointerUp: (event) {
          _pointers.remove(event.pointer);
          if (_canSwipe && _pointers.isEmpty && _swipeStart != null) {
            final distance = (_swipeEnd ?? event.position) - _swipeStart!;
            if (distance.dx.abs() >= 64 &&
                distance.dx.abs() > distance.dy.abs() * 1.5) {
              _navigate(distance.dx < 0 ? 1 : -1);
            }
          }
          _canSwipe = false;
        },
        child: widget.isPublicShare ? _publicImage() : _libraryImage(),
      ),
      bottomNavigationBar:
          !widget.isPublicShare && _index >= 0 && _images.length > 1
              ? SafeArea(
                  child: Row(
                      mainAxisAlignment: MainAxisAlignment.spaceBetween,
                      children: [
                      IconButton(
                          tooltip: 'Previous image',
                          color: Colors.white,
                          onPressed: _index > 0 ? () => _navigate(-1) : null,
                          icon: const Icon(Icons.chevron_left)),
                      Text('${_index + 1} / ${_images.length}',
                          style: const TextStyle(color: Colors.white)),
                      IconButton(
                          tooltip: 'Next image',
                          color: Colors.white,
                          onPressed: _index < _images.length - 1
                              ? () => _navigate(1)
                              : null,
                          icon: const Icon(Icons.chevron_right)),
                    ]))
              : null,
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
        final url = _currentUrl ??
            ref.read(apiClientProvider).streamUrl(snapshot.data!.id);
        return _ZoomableImage(
          controller: _transform,
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

  /// Public shares load without credentials: the token in the URL is the
  /// only secret, so no session cookie or bearer token is attached.
  Widget _publicImage() {
    final url = _currentUrl;
    if (url == null) return const _UnavailableImage();
    return _ZoomableImage(
      controller: _transform,
      child: PublicNetworkImage(
        imageUrl: url,
        sourceName: _currentTitle,
        fit: BoxFit.contain,
        placeholder: (_, __) => const _ImageLoading(),
        errorWidget: (_, __, ___) => const _UnavailableImage(),
      ),
    );
  }
}

class _ZoomableImage extends StatelessWidget {
  const _ZoomableImage({required this.child, required this.controller});

  final TransformationController controller;

  final Widget child;

  /// The full viewport accepts pinch and pan gestures, including the margins.
  /// Center loosens the image constraints so small bitmaps keep their own size;
  /// SVG images still request the available screen size.
  @override
  Widget build(BuildContext context) => InteractiveViewer(
        key: const Key('image_viewer_zoom'),
        transformationController: controller,
        minScale: 0.5,
        maxScale: 5,
        child: Center(child: child),
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
