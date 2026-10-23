import 'package:dio/dio.dart';
import 'player_api_client.dart';

/// Presence is transient and never queued as offline listening history.
extension MusicPresenceApi on PlayerApiClient {
  Future<List<Map<String, dynamic>>> musicListeners(int user, int epoch) async {
    final result = await rawDio.get<List<dynamic>>('/api/v1/music/presence',
        queryParameters: {'expected_user_id': user},
        options: _presenceOptions(epoch));
    return (result.data ?? []).cast<Map<String, dynamic>>();
  }

  Future<void> publishMusicPresence(
      int user, int epoch, Map<String, dynamic> body) async {
    await rawDio.put<void>('/api/v1/music/presence',
        data: body,
        queryParameters: {'expected_user_id': user},
        options: _presenceOptions(epoch));
  }

  Future<void> removeMusicPresence(
      int user, int epoch, String client, int sequence) async {
    await rawDio.delete<void>('/api/v1/music/presence',
        data: {'client_id': client, 'sequence': sequence},
        queryParameters: {'expected_user_id': user},
        options: _presenceOptions(epoch));
  }
}

Options _presenceOptions(int epoch) => Options(
    sendTimeout: const Duration(seconds: 8),
    receiveTimeout: const Duration(seconds: 8),
    headers: {'Cache-Control': 'no-store'},
    extra: {'credentialEpoch': epoch});
