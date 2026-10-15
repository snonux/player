import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http_mock_adapter/http_mock_adapter.dart';
import 'package:player_android/api/dio_player_api_client.dart';
import 'package:player_android/models/media_bookmark.dart';
import 'package:player_android/services/personal_media_store.dart';

void main() {
  test(
      'bookmark list, create and delete use documented API contract and captured credential epoch',
      () async {
    final dio = Dio(BaseOptions(baseUrl: 'https://player.test'));
    final adapter = DioAdapter(dio: dio);
    final epochs = <Object?>[];
    dio.interceptors.add(InterceptorsWrapper(onRequest: (options, handler) {
      epochs.add(options.extra['credentialEpoch']);
      handler.next(options);
    }));
    final client = DioPlayerApiClient(dio: dio, credentialEpoch: () => 7);
    final bookmark = {
      'id': 8,
      'media_id': 42,
      'position_seconds': 90.5,
      'label': 'Chapter',
      'created_at': '2026-10-15T12:00:00Z'
    };
    adapter.onGet('/api/v1/media/42/bookmarks',
        (server) => server.reply(200, [bookmark]));
    adapter.onPost(
        '/api/v1/media/42/bookmarks', (server) => server.reply(201, bookmark),
        data: {'position_seconds': 90.5, 'label': 'Chapter'});
    adapter.onDelete(
        '/api/v1/media/42/bookmarks/8', (server) => server.reply(204, null));
    expect((await client.listBookmarks(42)).single.positionSeconds, 90.5);
    expect((await client.addBookmark(42, 90.5, '  Chapter  ')).id, 8);
    await client.deleteBookmark(42, 8);
    expect(epochs, [7, 7, 7]);
  });

  test(
      'malformed or unrepresentable API positions are recoverable format errors',
      () {
    for (final position in [double.nan, double.infinity, -1.0, 1e300]) {
      expect(
          () => MediaBookmark.fromJson(
              {'id': 1, 'position_seconds': position, 'label': ''}),
          throwsFormatException);
    }
  });

  test(
      'a server store rejects all operations after its captured account changes',
      () async {
    final dio = Dio(BaseOptions(baseUrl: 'https://player.test'));
    final adapter = DioAdapter(dio: dio);
    var current = true;
    var calls = 0;
    adapter.onGet('/api/v1/media/42/bookmarks', (server) {
      calls++;
      server.reply(200, []);
    });
    final store = ServerPersonalMediaStore(
        DioPlayerApiClient(dio: dio), 42, 'account:A',
        isCurrent: () => current);
    expect(await store.listBookmarks(), isEmpty);
    expect(calls, 1);
    current = false;
    await expectLater(store.listBookmarks(), throwsStateError);
    await expectLater(store.addBookmark(3, 'Wrong account'), throwsStateError);
    await expectLater(store.saveNote('Wrong account'), throwsStateError);
    await expectLater(store.deleteBookmark(1), throwsStateError);
    await expectLater(store.deleteNote(), throwsStateError);
    await expectLater(store.toggleFavorite(), throwsStateError);
    expect(calls, 1);
  });
}
