import 'dart:async';
import 'server_endpoint_provider.dart';
import 'dart:io';
import 'dart:typed_data';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import '../api/dio_client.dart';
import '../services/playback_request.dart';
import 'api_client_provider.dart';
import 'auth_state_provider.dart';

/// Supplies an OS-readable private file; credentials never enter a MediaItem.
final mediaArtworkLoaderProvider =
    Provider<Future<Uri?> Function(PlaybackRequest, bool Function())>((ref) {
  var serial = 0;
  return (request, sourceCurrent) async {
    final (serverOrigin, userId) = switch (request) {
      ServerPlaybackRequest() => (request.serverOrigin, request.userId),
      LiveRadioPlaybackRequest() => (request.serverOrigin, request.userId),
      _ => ('', -1),
    };
    if (serverOrigin.isEmpty || request.artworkUri == null) return null;
    final base = ref.read(playerBaseUrlProvider);
    final mutations = ref.read(credentialMutationQueueProvider);
    final generation = mutations.generation;
    bool current() =>
        sourceCurrent() &&
        mutations.generation == generation &&
        ref.read(playerBaseUrlProvider).origin == serverOrigin &&
        ref.read(authStateProvider).valueOrNull?.user?.id == userId;
    if (base.origin != serverOrigin || !current()) return null;
    final uri = request.artworkUri!;
    final headers = await accountRequestHeaders(
        uri: uri,
        baseUrl: base,
        storage: ref.read(tokenStorageProvider),
        cookieJar: ref.read(cookieJarProvider),
        mutations: mutations);
    if (!current() || headers.isEmpty) return null;
    final client = HttpClient()..connectionTimeout = const Duration(seconds: 5);
    final endpointRouter = ref.read(serverEndpointRouterProvider);
    Uri? routed;
    try {
      routed = await endpointRouter.resolve(uri);
      if (!current()) return null;
      final call =
          await client.getUrl(routed).timeout(const Duration(seconds: 5));
      call.followRedirects = false;
      headers.forEach(call.headers.set);
      final response = await call.close().timeout(const Duration(seconds: 5));
      if (response.statusCode != 200 ||
          response.contentLength > 5 * 1024 * 1024 ||
          response.headers.contentType?.primaryType != 'image') {
        return null;
      }
      final bytes = BytesBuilder(copy: false);
      await for (final chunk in response.timeout(const Duration(seconds: 5))) {
        bytes.add(chunk);
        if (bytes.length > 5 * 1024 * 1024 || !current()) return null;
      }
      if (!current()) return null;
      final directory =
          Directory('${Directory.systemTemp.path}/media-session-art');
      await directory.create(recursive: true);
      if (!current()) return null;
      final file = File(
          '${directory.path}/${DateTime.now().microsecondsSinceEpoch}-${serial++}.art');
      await file.writeAsBytes(bytes.takeBytes(), flush: true);
      if (!current()) {
        await file.delete();
        return null;
      }
      // Keep a small private cache; old source files are never reused by identity.
      final old = await directory
          .list()
          .where((entry) => entry is File)
          .cast<File>()
          .toList();
      if (!current()) {
        await file.delete();
        return null;
      }
      old.sort((a, b) => a.path.compareTo(b.path));
      for (final entry in old.take((old.length - 8).clamp(0, old.length))) {
        if (!current()) return null;
        try {
          await entry.delete();
        } on FileSystemException {/* already removed */}
      }
      return file.uri;
    } on SocketException {
      if (routed != null) endpointRouter.failed(routed);
      return null;
    } on TimeoutException {
      if (routed != null) endpointRouter.failed(routed);
      return null;
    } catch (_) {
      return null;
    } finally {
      client.close(force: true);
    }
  };
});
