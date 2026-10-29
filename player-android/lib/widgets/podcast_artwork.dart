import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import '../services/podcast_metadata.dart';

/// Compact public publisher artwork with disk caching for later offline use.
class PodcastArtwork extends StatelessWidget {
  const PodcastArtwork(
      {super.key,
      required this.imageUrl,
      this.fallbackUrl = '',
      required this.title,
      this.size = 56,
      this.fit = BoxFit.cover,
      this.unavailable});
  final String imageUrl, fallbackUrl, title;
  final double? size;
  final BoxFit fit;
  final Widget? unavailable;

  Widget placeholder() => const ColoredBox(
      color: Colors.transparent, child: Center(child: Icon(Icons.podcasts)));

  Widget image(String url, String fallback) => CachedNetworkImage(
      imageUrl: url,
      width: size,
      height: size,
      memCacheWidth: size == null ? 512 : (size! * 3).round(),
      fit: fit,
      placeholder: (_, __) => placeholder(),
      errorWidget: (_, __, ___) => fallback.isNotEmpty && fallback != url
          ? image(fallback, '')
          : unavailable ?? placeholder());

  @override
  Widget build(BuildContext context) {
    final primary = podcastArtworkUrl(imageUrl);
    final fallback = podcastArtworkUrl(fallbackUrl);
    final source = primary.isNotEmpty ? primary : fallback;
    return Semantics(
        label: 'Cover for $title',
        child: ExcludeSemantics(
            child: ClipRRect(
                borderRadius:
                    size == null ? BorderRadius.zero : BorderRadius.circular(6),
                child: SizedBox(
                    width: size,
                    height: size,
                    child: source.isEmpty
                        ? unavailable ?? placeholder()
                        : image(source, fallback)))));
  }
}
