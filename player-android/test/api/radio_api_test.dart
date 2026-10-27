import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http_mock_adapter/http_mock_adapter.dart';
import 'package:player_android/api/dio_player_api_client.dart';
import 'package:player_android/models/radio_station.dart';

const _uuid = '96202f73-0601-11e8-ae97-52543be04c81';

Map<String, dynamic> _station({bool bookmarked = false}) => {
      'uuid': _uuid,
      'name': 'Jazz Radio',
      'homepage': 'https://example.org/',
      'favicon_url': '/api/radio/stations/$_uuid/favicon',
      'stream_url': '/api/radio/stations/$_uuid/stream',
      'country': 'Germany',
      'country_code': 'DE',
      'language': 'german',
      'tags': ['jazz', 'smooth jazz'],
      'codec': 'MP3',
      'bitrate': 128,
      'votes': 1234,
      'bookmarked': bookmarked,
      if (bookmarked) 'bookmarked_at': '2026-10-20T18:00:00Z',
    };

void main() {
  late Dio dio;
  late DioAdapter adapter;
  late DioPlayerApiClient client;
  late List<Object?> epochs;

  setUp(() {
    dio = Dio(BaseOptions(baseUrl: 'https://player.test'));
    adapter = DioAdapter(dio: dio);
    epochs = [];
    dio.interceptors.add(InterceptorsWrapper(onRequest: (options, handler) {
      epochs.add(options.extra['credentialEpoch']);
      handler.next(options);
    }));
    client = DioPlayerApiClient(dio: dio, credentialEpoch: () => 3);
  });

  test('station search sends the documented filters and parses stations',
      () async {
    adapter.onGet(
      '/api/v1/radio/stations',
      (server) => server.reply(200, [
        _station(),
        {'name': 'no uuid is skipped'},
      ]),
      queryParameters: {
        'q': 'jazz',
        'tag': 'jazz',
        'country': 'DE',
        'order': 'random',
        'limit': 20,
        'offset': 40,
      },
    );
    final stations = await client.searchRadioStations(
      search: ' jazz ',
      tag: 'jazz',
      country: 'DE',
      order: RadioOrder.random,
      limit: 20,
      offset: 40,
    );
    final station = stations.single;
    expect(station.uuid, _uuid);
    expect(station.name, 'Jazz Radio');
    expect(station.countryCode, 'DE');
    expect(station.tags, ['jazz', 'smooth jazz']);
    expect(station.bitrate, 128);
    expect(station.bookmarked, isFalse);
    expect(station.summary, 'Germany · MP3 128 kbps · jazz, smooth jazz');
    expect(client.radioUrl(station.streamUrl),
        'https://player.test/api/radio/stations/$_uuid/stream');
  });

  test('default search asks for popular stations only', () async {
    adapter.onGet('/api/v1/radio/stations', (server) => server.reply(200, []),
        queryParameters: {'order': 'popular'});
    expect(await client.searchRadioStations(), isEmpty);
  });

  test('tags and countries', () async {
    adapter.onGet(
        '/api/v1/radio/tags',
        (server) => server.reply(200, [
              {'name': 'jazz', 'station_count': 812},
              {'name': '', 'station_count': 1},
            ]),
        queryParameters: {'limit': 60});
    adapter.onGet(
        '/api/v1/radio/countries',
        (server) => server.reply(200, [
              {'code': 'DE', 'name': 'Germany', 'station_count': 4321},
            ]));
    final tags = await client.listRadioTags(limit: 60);
    expect(tags.single.name, 'jazz');
    expect(tags.single.stationCount, 812);
    final countries = await client.listRadioCountries();
    expect(countries.single.code, 'DE');
    expect(countries.single.stationCount, 4321);
  });

  test('bookmarks list, add (PUT, no body) and delete', () async {
    adapter.onGet('/api/v1/radio/bookmarks',
        (server) => server.reply(200, [_station(bookmarked: true)]));
    adapter.onPut('/api/v1/radio/bookmarks/$_uuid',
        (server) => server.reply(200, _station(bookmarked: true)));
    adapter.onDelete(
        '/api/v1/radio/bookmarks/$_uuid', (server) => server.reply(204, null));
    final bookmarks = await client.listRadioBookmarks();
    expect(bookmarks.single.bookmarked, isTrue);
    expect(bookmarks.single.bookmarkedAt, DateTime.utc(2026, 10, 20, 18));
    expect((await client.addRadioBookmark(_uuid)).bookmarked, isTrue);
    await client.removeRadioBookmark(_uuid);
    expect(epochs, [3, 3, 3]);
  });
}
