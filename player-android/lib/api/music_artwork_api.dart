import 'dart:typed_data';
import 'package:dio/dio.dart';
import 'player_api_client.dart';

/// Authenticated custom artwork shares the ordinary bearer/cookie interceptors.
extension MusicArtworkApi on PlayerApiClient {
  String _artPath(String kind, String key) =>
      '/api/v1/music/artwork/$kind/${Uri.encodeComponent(key)}';
  Future<Map<String, dynamic>> musicArtwork(String kind, String key) async =>
      (await rawDio.get<Map<String, dynamic>>(_artPath(kind, key))).data!;
  Future<Uint8List> musicArtworkImage(
          String kind, String key, String revision) async =>
      Uint8List.fromList((await rawDio.get<List<int>>(
              '${_artPath(kind, key)}/image',
              queryParameters: {'revision': revision},
              options: Options(responseType: ResponseType.bytes)))
          .data!);
  Future<Map<String, dynamic>> saveMusicArtwork(
          String kind, String key, Uint8List? bytes) async =>
      (await rawDio.request<Map<String, dynamic>>(_artPath(kind, key),
              data: bytes,
              options: Options(
                  method: bytes == null ? 'DELETE' : 'PUT',
                  contentType: 'application/octet-stream')))
          .data!;
}
