import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/dio_player_api_client.dart';
import '../api/player_api_client.dart';
import 'api_client_provider.dart' show kPlayerBaseUrl;

/// Provides an unauthenticated [PlayerApiClient] for public endpoints.
///
/// Unlike [apiClientProvider], this client has no bearer-token interceptor
/// and no 401 → login redirect interceptor.  It is designed exclusively for
/// the share-viewer feature, where the share token is part of the URL path
/// (not an Authorization header) and no session is required.
///
/// Using a dedicated provider keeps the authenticated and public clients
/// cleanly separated (Single Responsibility, Separation of Concerns) and
/// avoids accidentally attaching a session to public requests.
final publicApiClientProvider = Provider<PlayerApiClient>((ref) {
  // Minimal Dio instance: JSON content-type, same base URL as the auth client,
  // but without any auth or redirect interceptors.
  final dio = Dio(
    BaseOptions(
      baseUrl: kPlayerBaseUrl,
      contentType: 'application/json',
      responseType: ResponseType.json,
    ),
  );

  return DioPlayerApiClient(dio: dio);
});
