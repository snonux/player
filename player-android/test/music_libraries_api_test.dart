import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/player_api_client.dart';

void main() {
  test('musicAlbums sends library_ids when selected', () async {
    String? seen;
    final dio = Dio()
      ..httpClientAdapter = _Adapter((options) {
        seen = options.queryParameters['library_ids']?.toString();
        return ResponseBody.fromString('[]', 200, headers: {
          Headers.contentTypeHeader: [Headers.jsonContentType],
        });
      });
    final client = PlayerApiClient(dio: dio);
    await client.musicAlbums(libraryIds: [3, 7]);
    expect(seen, '3,7');
  });
}

class _Adapter implements HttpClientAdapter {
  _Adapter(this._handler);
  final ResponseBody Function(RequestOptions) _handler;
  @override
  void close({bool force = false}) {}
  @override
  Future<ResponseBody> fetch(RequestOptions options,
      Stream<Uint8List>? requestStream, Future? cancelFuture) async {
    return _handler(options);
  }
}
