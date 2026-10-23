import 'player_api_client.dart';

/// Smart rule writes use the existing authenticated client and playlist contract.
extension PlaylistRulesApi on PlayerApiClient {
  Future<Map<String, dynamic>> savePlaylistRules(
          String id, Map<String, dynamic>? rules) async =>
      (await rawDio.put<Map<String, dynamic>>(
              '/api/v1/playlists/${Uri.encodeComponent(id)}/rules',
              data: {'rules': rules}))
          .data!;
}
