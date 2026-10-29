import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http_mock_adapter/http_mock_adapter.dart';
import 'package:player_android/api/dio_player_api_client.dart';

void main() {
  test(
      'catalogue fallback requests all listened episodes by stable feed key and page',
      () async {
    final dio = Dio(BaseOptions(baseUrl: 'https://player.test'));
    final adapter = DioAdapter(dio: dio);
    dio.interceptors.add(InterceptorsWrapper(onRequest: (options, handler) {
      expect(options.extra['credentialEpoch'], 42);
      expect(options.extra['quietUnauthorized'], true);
      handler.next(options);
    }));
    adapter.onGet(
        '/api/v1/podcasts/episodes',
        (server) => server.reply(200, [
              {'episode_key': 'guid:a'}
            ]),
        queryParameters: {
          'feed_key': 'feed:example.com/rss',
          'filter': 'all',
          'offset': 200,
          'limit': 200
        });
    final result = await DioPlayerApiClient(
            dio: dio, credentialEpoch: () => 42, quietUnauthorized: true)
        .listSubscribedPodcastEpisodes('feed:example.com/rss', offset: 200);
    expect(result.single['episode_key'], 'guid:a');
  });
}
