import 'dart:convert';

import 'package:cached_network_image/cached_network_image.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../providers/api_client_provider.dart';
import '../providers/auth_state_provider.dart';
import '../api/dio_client.dart';
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
/// Bitmaps load through [CachedNetworkImage]. SVG documents, recognised by
/// [sourceName] or the URL (see [isSvgSource]), load through
/// [NetworkSvgImage] with the same headers and an equally account-specific
/// cache key. In both cases [errorWidget] replaces an image that cannot be
/// downloaded or decoded.
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
  final Widget Function(BuildContext, String) placeholder;
  final Widget Function(BuildContext, String, Object) errorWidget;
  final BoxFit? fit;
  final double? width;
  final double? height;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final headers =
        ref.watch(authenticatedImageHeadersProvider(Uri.parse(imageUrl)));
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
    final cacheKey = authenticatedImageCacheKey(imageUrl, headers);
    if (isSvgSource(fileName: sourceName, url: imageUrl)) {
      return NetworkSvgImage(
        imageUrl: imageUrl,
        cacheKey: cacheKey,
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
      cacheKey: cacheKey,
      httpHeaders: headers,
      fit: fit,
      width: width,
      height: height,
      placeholder: placeholder,
      errorWidget: errorWidget,
    );
  }
}
