import 'package:dio/dio.dart';
import 'player_api_client.dart';

/// Account-private history calls use the shared authenticated transport.
extension MusicHistoryApi on PlayerApiClient {
  Future<Map<String, dynamic>> listeningHistory(
      {int? beforeId, int? beforeMs, int? credentialEpoch}) async {
    final result = await rawDio.get<Map<String, dynamic>>(
        '/api/v1/music/history',
        queryParameters: {
          if (beforeId != null) 'before_id': beforeId,
          if (beforeMs != null) 'before_ms': beforeMs
        },
        options: Options(headers: {
          'Cache-Control': 'no-store'
        }, extra: {
          if (credentialEpoch != null) 'credentialEpoch': credentialEpoch
        }));
    return result.data!;
  }

  Future<void> configureListeningHistory(bool recording, int days,
      {int? credentialEpoch}) async {
    await rawDio.put<void>('/api/v1/music/history',
        data: {'recording': recording, 'retention_days': days},
        options: Options(extra: {
          if (credentialEpoch != null) 'credentialEpoch': credentialEpoch
        }));
  }

  Future<void> clearListeningHistory({int? credentialEpoch}) async {
    await rawDio.delete<void>('/api/v1/music/history',
        options: Options(extra: {
          if (credentialEpoch != null) 'credentialEpoch': credentialEpoch
        }));
  }
}
