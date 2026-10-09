import 'dart:convert';

import 'package:cached_network_image/cached_network_image.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers/api_client_provider.dart';
import '../providers/auth_state_provider.dart';
import '../api/dio_client.dart';
import '../services/svg_document.dart';
import 'network_svg_image.dart';

/// Keep protected image caches separate across credentials and accounts.
/// Only the digest is stored in the cache key, never the token or cookie.
String authenticatedImageCacheKey(
    String imageUrl, Map<String, String> headers) {
  final credential =
      '${headers['Authorization'] ?? ''}\u0000${headers['Cookie'] ?? ''}';
  final digest = sha256.convert(utf8.encode(credential));
  return '$imageUrl#$digest';
}

/// Credentials for image loaders that do not use Dio's interceptors.
/// Wait for this provider before creating the image, or its first request may
/// reach a protected endpoint without the saved bearer token.
final authenticatedImageHeadersProvider = FutureProvider.autoDispose
    .family<Map<String, String>?, Uri>((ref, uri) async {
  if (uri.origin != ref.read(playerBaseUrlProvider).origin) return null;
  if (ref.exists(authStateProvider)) ref.watch(authStateProvider);
  return accountRequestHeaders(
    uri: uri,
    baseUrl: ref.read(playerBaseUrlProvider),
    storage: ref.read(tokenStorageProvider),
    cookieJar: ref.read(cookieJarProvider),
    mutations: ref.read(credentialMutationQueueProvider),
  );
});

/// Image whose request uses the same credentials as protected API calls.
///
/// Bitmaps load through [CachedNetworkImage]. SVG documents load through
/// [NetworkSvgImage] with the same headers. An SVG is recognised up front
/// by [sourceName] or the URL (see [isSvgSource]); an image without a name,
/// such as a folder cover, is recognised by content after the bitmap
/// decoder rejected it (see [bitmapErrorOrSvg]). In every case [errorWidget]
/// replaces an image that cannot be downloaded or decoded.
class AuthenticatedNetworkImage extends ConsumerWidget {
  const AuthenticatedNetworkImage({
    super.key,
    required this.imageUrl,
    required this.placeholder,
    required this.errorWidget,
    this.sourceName,
    this.fit,
    this.width,
    this.height,
  });

  final String imageUrl;

  /// File name of the media behind [imageUrl], used to recognise SVG:
  /// thumbnail and stream URLs do not carry the file extension.
  final String? sourceName;
  final ImagePlaceholderBuilder placeholder;
  final ImageErrorBuilder errorWidget;
  final BoxFit? fit;
  final double? width;
  final double? height;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    // A malformed URL is an image error like any other, not a build failure.
    final uri = Uri.tryParse(imageUrl);
    if (uri == null) {
      return errorWidget(
          context, imageUrl, const FormatException('Invalid image URL'));
    }
    final headers = ref.watch(authenticatedImageHeadersProvider(uri));
    return headers.when(
      loading: () => placeholder(context, imageUrl),
      error: (error, _) => errorWidget(context, imageUrl, error),
      data: (value) => value == null
          ? errorWidget(context, imageUrl,
              StateError('Image origin does not match server'))
          : _image(value),
    );
  }

  Widget _image(Map<String, String> headers) {
    if (isSvgSource(fileName: sourceName, url: imageUrl)) {
      return NetworkSvgImage(
        imageUrl: imageUrl,
        headers: headers,
        fit: fit,
        width: width,
        height: height,
        placeholder: placeholder,
        errorWidget: errorWidget,
      );
    }
    return CachedNetworkImage(
      imageUrl: imageUrl,
      cacheKey: authenticatedImageCacheKey(imageUrl, headers),
      httpHeaders: headers,
      fit: fit,
      width: width,
      height: height,
      placeholder: placeholder,
      errorWidget: (context, _, error) => bitmapErrorOrSvg(
        context,
        error: error,
        imageUrl: imageUrl,
        headers: headers,
        fit: fit,
        width: width,
        height: height,
        placeholder: placeholder,
        errorWidget: errorWidget,
      ),
    );
  }
}
