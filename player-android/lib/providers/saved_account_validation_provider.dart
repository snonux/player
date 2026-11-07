import 'dart:io';

import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../services/saved_server_credentials.dart';
import '../services/server_endpoint_router.dart';
import 'server_endpoint_provider.dart';

/// Only outages permit offline access; rejected credentials, bad certificates,
/// redirects and invalid responses must keep the account locked.
bool isServerUnavailable(DioException error) {
  if ([502, 503, 504].contains(error.response?.statusCode)) return true;
  if (error.response != null) return false;
  return error.error is EndpointReachabilityException ||
      {
        DioExceptionType.connectionTimeout,
        DioExceptionType.receiveTimeout,
        DioExceptionType.sendTimeout
      }.contains(error.type) ||
      (error.type == DioExceptionType.connectionError &&
          error.error is SocketException);
}

final savedAccountValidationTransportProvider =
    Provider<HttpClientAdapter Function()>((ref) {
  final router = ref.watch(serverEndpointRouterProvider);
  return () => EndpointHttpAdapter(router);
});

/// A separate read-only client keeps the general credential gate closed until
/// this saved bearer has been accepted. It never carries session cookies or
/// follows redirects, including redirects to another origin.
final savedAccountValidationProvider =
    Provider<Future<bool> Function(SavedServerCredential)>((ref) {
  final transport = ref.watch(savedAccountValidationTransportProvider);
  return (credential) async {
    final dio = Dio(BaseOptions(
        baseUrl: credential.origin,
        connectTimeout: const Duration(seconds: 4),
        receiveTimeout: const Duration(seconds: 4),
        followRedirects: false,
        headers: {'Authorization': 'Bearer ${credential.token}'}));
    dio.httpClientAdapter = transport();
    try {
      final response = await dio.get<List<dynamic>>('/api/v1/auth/tokens');
      final tokens = response.data;
      if (tokens == null ||
          (credential.tokenId != null &&
              !tokens.any(
                  (row) => row is Map && row['id'] == credential.tokenId))) {
        throw const FormatException('Saved account could not be validated');
      }
      return true;
    } on DioException catch (error) {
      // Gateway failures also describe a backend outage behind a live ingress.
      // Authentication denial, malformed responses and certificate errors fail shut.
      if (isServerUnavailable(error)) return false;
      rethrow;
    } finally {
      dio.close(force: true);
    }
  };
});
