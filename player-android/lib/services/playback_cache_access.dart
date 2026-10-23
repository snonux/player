import 'dart:async';
import 'dart:io';
import 'package:dio/dio.dart';

class CachedAudioAccessDenied implements Exception {
  const CachedAudioAccessDenied(this.status);
  final int status;
  @override
  String toString() =>
      'Cached song access could not be verified (HTTP $status).';
}

bool cacheAccessDenied(Object error) {
  final status = error is CachedAudioAccessDenied
      ? error.status
      : error is DioException
          ? error.response?.statusCode
          : null;
  return [401, 403, 404, 410].contains(status);
}

/// Only failures without an HTTP answer may use a previously authorized copy.
bool cacheTransportFailure(Object error) =>
    error is SocketException ||
    error is TimeoutException ||
    (error is DioException &&
        error.response == null &&
        error.type != DioExceptionType.cancel);

/// Revalidates access without downloading audio; changed ETags discard a copy.
Future<bool> validateCachedAudio(
    Uri uri, Map<String, String> headers, String? etag) async {
  final probe = HttpClient()..connectionTimeout = const Duration(seconds: 10);
  try {
    final head = await probe.headUrl(uri);
    head.followRedirects = false;
    headers.forEach(head.headers.set);
    final response = await head.close().timeout(const Duration(seconds: 15));
    if (response.statusCode != 200) {
      throw CachedAudioAccessDenied(response.statusCode);
    }
    return etag == null ||
        etag.isEmpty ||
        response.headers.value('etag') == etag;
  } finally {
    probe.close(force: true);
  }
}
