import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/dio_player_api_client.dart';
import '../api/player_api_client.dart';
import 'settings_provider.dart';

/// Share intents are registered for the build-time origin, not the mutable
/// server setting. Keeping public requests on that origin prevents a link for
/// server A from sending its token to server B after an in-app server switch.
final publicShareBaseUrlProvider =
    Provider<Uri>((ref) => parseServerBaseUrl(kPlayerBaseUrl));

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
  // Minimal Dio instance: JSON content-type and no auth/redirect interceptors.
  final dio = Dio(
    BaseOptions(
      baseUrl: ref.watch(publicShareBaseUrlProvider).toString(),
      contentType: 'application/json',
      responseType: ResponseType.json,
    ),
  );

  return DioPlayerApiClient(dio: dio);
});
