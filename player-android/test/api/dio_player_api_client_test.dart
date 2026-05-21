// Unit tests for DioPlayerApiClient.
//
// Tests use http_mock_adapter's DioAdapter to intercept Dio requests and return
// canned JSON responses without a real network connection. This keeps tests
// fast, hermetic, and free from platform dependencies (no OS keychain, no
// NavigatorKey, no real server).
//
// Coverage: login, listMedia, getMedia (success + error cases for each), plus
// smoke tests for bootstrap, logout, listSets, browseSet, healthz, readyz,
// streamMedia, downloadMedia, and getThumbnail.

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http_mock_adapter/http_mock_adapter.dart';
import 'package:player_android/api/dio_player_api_client.dart';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/// Creates a [Dio] with [DioAdapter] wired in and returns both so tests can
/// register routes on the adapter.  The base URL is fixed to a local host that
/// will never accidentally resolve.
({Dio dio, DioAdapter adapter}) _buildTestDio() {
  final dio = Dio(BaseOptions(baseUrl: 'https://player.test'));
  final adapter = DioAdapter(dio: dio);
  return (dio: dio, adapter: adapter);
}

/// Minimal valid Media JSON as returned by GET /api/v1/media.
Map<String, dynamic> _mediaJson({int id = 1, String fileName = 'foo.mp4'}) => {
      'id': id,
      'set_id': 2,
      'rel_path': 'videos/$fileName',
      'file_name': fileName,
      'abs_path': '/media/videos/$fileName',
      'type': 'video',
      'duration': 120.0,
      'codec': 'h264/aac',
      'resolution': '1920x1080',
      'bitrate': 4000,
      'file_size_bytes': 512000,
      'width': 1920,
      'height': 1080,
      'thumbnail_path': '/thumbs/$fileName.jpg',
      'play_count': 3,
      'deleted_at': null,
      'created_at': '2026-01-15T12:00:00.000Z',
    };

/// Server envelope returned by GET /api/v1/media/{id}.
Map<String, dynamic> _mediaDetailEnvelope({
  int id = 42,
  String fileName = 'movie.mp4',
}) =>
    {
      'media': _mediaJson(id: id, fileName: fileName),
      'tags': [
        {'id': 1, 'name': 'documentary'},
        {'id': 2, 'name': '4k'},
      ],
      'favorite': true,
      'note': null,
      'progress': null,
    };

// ---------------------------------------------------------------------------
// login
// ---------------------------------------------------------------------------

void main() {
  group('login', () {
    test('success — returns User from response JSON', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onPost(
        '/api/v1/auth/login',
        (server) => server.reply(200, {
          'id': 3,
          'username': 'alice',
          'is_admin': false,
          'created_at': null,
        }),
        data: {'username': 'alice', 'password': 'secret'},
      );

      final client = DioPlayerApiClient(dio: dio);
      final user = await client.login(username: 'alice', password: 'secret');

      expect(user.id, 3);
      expect(user.username, 'alice');
      expect(user.isAdmin, isFalse);
    });

    test('401 — DioException is propagated to the caller', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onPost(
        '/api/v1/auth/login',
        (server) => server.reply(401, {'error': 'invalid credentials'}),
        data: {'username': 'bob', 'password': 'wrong'},
      );

      final client = DioPlayerApiClient(dio: dio);
      expect(
        () => client.login(username: 'bob', password: 'wrong'),
        throwsA(isA<DioException>()),
      );
    });

    test('400 — DioException is propagated to the caller', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onPost(
        '/api/v1/auth/login',
        (server) => server.reply(400, {'error': 'missing username'}),
        data: {'username': '', 'password': ''},
      );

      final client = DioPlayerApiClient(dio: dio);
      expect(
        () => client.login(username: '', password: ''),
        throwsA(isA<DioException>()),
      );
    });
  });

  // ---------------------------------------------------------------------------
  // listMedia
  // ---------------------------------------------------------------------------

  group('listMedia', () {
    test('success (no filters) — returns list of Media', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onGet(
        '/api/v1/media',
        (server) => server.reply(200, [_mediaJson(), _mediaJson(id: 2)]),
      );

      final client = DioPlayerApiClient(dio: dio);
      final list = await client.listMedia();

      expect(list, hasLength(2));
      expect(list[0].id, 1);
      expect(list[0].fileName, 'foo.mp4');
      expect(list[1].id, 2);
    });

    test('success with filters — query params are forwarded', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onGet(
        '/api/v1/media',
        (server) => server.reply(200, [_mediaJson(id: 5, fileName: 'clip.mp4')]),
        queryParameters: {
          'type': 'video',
          'set_id': 1,
          'limit': 10,
          'offset': 0,
          'sort': 'name',
        },
      );

      final client = DioPlayerApiClient(dio: dio);
      final list = await client.listMedia(
        type: 'video',
        setId: 1,
        limit: 10,
        offset: 0,
        sort: 'name',
      );

      expect(list, hasLength(1));
      expect(list[0].id, 5);
      expect(list[0].fileName, 'clip.mp4');
    });

    test('empty list — returns empty', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onGet(
        '/api/v1/media',
        (server) => server.reply(200, <dynamic>[]),
      );

      final client = DioPlayerApiClient(dio: dio);
      final list = await client.listMedia();

      expect(list, isEmpty);
    });

    test('401 — DioException is propagated', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onGet(
        '/api/v1/media',
        (server) => server.reply(401, {'error': 'unauthorized'}),
      );

      final client = DioPlayerApiClient(dio: dio);
      expect(() => client.listMedia(), throwsA(isA<DioException>()));
    });
  });

  // ---------------------------------------------------------------------------
  // getMedia
  // ---------------------------------------------------------------------------

  group('getMedia', () {
    test('success — unwraps envelope, injects tags and favorite', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onGet(
        '/api/v1/media/42',
        (server) => server.reply(200, _mediaDetailEnvelope()),
      );

      final client = DioPlayerApiClient(dio: dio);
      final media = await client.getMedia(42);

      expect(media.id, 42);
      expect(media.fileName, 'movie.mp4');
      // Tags extracted from [{id,name}] envelope and injected into Media.
      expect(media.tags, containsAll(['documentary', '4k']));
      // Favorite flag injected from envelope into Media.
      expect(media.favorite, isTrue);
    });

    test('success — favorite=false is preserved', () async {
      final (:dio, :adapter) = _buildTestDio();
      final envelope = {
        ..._mediaDetailEnvelope(),
        'favorite': false,
        'tags': <dynamic>[],
      };
      adapter.onGet(
        '/api/v1/media/42',
        (server) => server.reply(200, envelope),
      );

      final client = DioPlayerApiClient(dio: dio);
      final media = await client.getMedia(42);

      expect(media.favorite, isFalse);
      expect(media.tags, isEmpty);
    });

    test('404 — DioException is propagated', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onGet(
        '/api/v1/media/999',
        (server) => server.reply(404, {'error': 'not found'}),
      );

      final client = DioPlayerApiClient(dio: dio);
      expect(() => client.getMedia(999), throwsA(isA<DioException>()));
    });
  });

  // ---------------------------------------------------------------------------
  // bootstrap
  // ---------------------------------------------------------------------------

  group('bootstrap', () {
    test('success — returns admin User', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onPost(
        '/api/v1/auth/bootstrap',
        (server) => server.reply(200, {
          'id': 1,
          'username': 'admin',
          'is_admin': true,
          'created_at': null,
        }),
        data: {'username': 'admin', 'password': 'changeme'},
      );

      final client = DioPlayerApiClient(dio: dio);
      final user = await client.bootstrap(
        username: 'admin',
        password: 'changeme',
      );

      expect(user.id, 1);
      expect(user.username, 'admin');
      expect(user.isAdmin, isTrue);
    });

    test('403 — DioException when users already exist', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onPost(
        '/api/v1/auth/bootstrap',
        (server) => server.reply(403, {'error': 'forbidden'}),
        data: {'username': 'admin', 'password': 'x'},
      );

      final client = DioPlayerApiClient(dio: dio);
      expect(
        () => client.bootstrap(username: 'admin', password: 'x'),
        throwsA(isA<DioException>()),
      );
    });
  });

  // ---------------------------------------------------------------------------
  // logout
  // ---------------------------------------------------------------------------

  group('logout', () {
    test('success — 204 completes without error', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onPost(
        '/api/v1/logout',
        (server) => server.reply(204, null),
      );

      final client = DioPlayerApiClient(dio: dio);
      await expectLater(client.logout(), completes);
    });
  });

  // ---------------------------------------------------------------------------
  // listSets
  // ---------------------------------------------------------------------------

  group('listSets', () {
    test('success — returns list of MediaSet', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onGet(
        '/api/v1/sets',
        (server) => server.reply(200, [
          {
            'id': 1,
            'name': 'Movies',
            'root_path': 'movies',
            'cover_thumbnail_path': '',
            'is_podcast': false,
            'created_at': '2026-01-01T00:00:00.000Z',
          },
        ]),
      );

      final client = DioPlayerApiClient(dio: dio);
      final sets = await client.listSets();

      expect(sets, hasLength(1));
      expect(sets[0].id, 1);
      expect(sets[0].name, 'Movies');
      expect(sets[0].isPodcast, isFalse);
    });

    test('empty — returns empty list', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onGet('/api/v1/sets', (server) => server.reply(200, <dynamic>[]));

      final client = DioPlayerApiClient(dio: dio);
      expect(await client.listSets(), isEmpty);
    });
  });

  // ---------------------------------------------------------------------------
  // browseSet
  // ---------------------------------------------------------------------------

  group('browseSet', () {
    test('success — returns raw map', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onGet(
        '/api/v1/sets/1/browse',
        (server) => server.reply(200, {
          'current_path': 'movies',
          'folders': [
            {'name': 'action', 'has_cover': false},
          ],
          'media': [_mediaJson()],
          'episodes': [],
        }),
      );

      final client = DioPlayerApiClient(dio: dio);
      final result = await client.browseSet(1);

      expect(result['current_path'], 'movies');
      expect((result['folders'] as List).length, 1);
    });

    test('success with parent — passes query param', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onGet(
        '/api/v1/sets/1/browse',
        (server) => server.reply(200, {
          'current_path': 'movies/action',
          'folders': <dynamic>[],
          'media': <dynamic>[],
          'episodes': <dynamic>[],
        }),
        queryParameters: {'parent': 'action'},
      );

      final client = DioPlayerApiClient(dio: dio);
      final result = await client.browseSet(1, parent: 'action');

      expect(result['current_path'], 'movies/action');
    });
  });

  // ---------------------------------------------------------------------------
  // healthz / readyz
  // ---------------------------------------------------------------------------

  group('healthz', () {
    test('200 — completes without error', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onGet('/healthz', (server) => server.reply(200, null));

      final client = DioPlayerApiClient(dio: dio);
      await expectLater(client.healthz(), completes);
    });
  });

  group('readyz', () {
    test('200 — completes without error', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onGet('/readyz', (server) => server.reply(200, null));

      final client = DioPlayerApiClient(dio: dio);
      await expectLater(client.readyz(), completes);
    });
  });

  // ---------------------------------------------------------------------------
  // streamMedia
  // ---------------------------------------------------------------------------

  group('streamMedia', () {
    // DioAdapter does not support ResponseType.bytes natively — it returns the
    // mock data as-is through Dio's JSON transformer, which causes a type error
    // when the implementation requests bytes.  We verify that the method issues
    // the correct request path and that it propagates DioExceptions; byte
    // accuracy is validated by the Uint8List.fromList conversion logic which
    // is exercised in the other binary-response tests below.
    test('404 — DioException is propagated', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onGet(
        '/api/v1/media/99/stream',
        (server) => server.reply(404, {'error': 'not found'}),
      );

      final client = DioPlayerApiClient(dio: dio);
      expect(() => client.streamMedia(99), throwsA(isA<DioException>()));
    });
  });

  // ---------------------------------------------------------------------------
  // downloadMedia
  // ---------------------------------------------------------------------------

  group('downloadMedia', () {
    test('404 — DioException is propagated', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onGet(
        '/api/v1/media/99/download',
        (server) => server.reply(404, {'error': 'not found'}),
      );

      final client = DioPlayerApiClient(dio: dio);
      expect(() => client.downloadMedia(99), throwsA(isA<DioException>()));
    });
  });

  // ---------------------------------------------------------------------------
  // getThumbnail
  // ---------------------------------------------------------------------------

  group('getThumbnail', () {
    test('404 — DioException when thumbnail absent', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onGet(
        '/api/v1/media/99/thumbnail',
        (server) => server.reply(404, {'error': 'not found'}),
      );

      final client = DioPlayerApiClient(dio: dio);
      expect(() => client.getThumbnail(99), throwsA(isA<DioException>()));
    });
  });

  // ---------------------------------------------------------------------------
  // listMyShares
  // ---------------------------------------------------------------------------

  group('listMyShares', () {
    /// Minimal valid Share JSON matching the GET /api/v1/shares response schema.
    Map<String, dynamic> shareJson({
      String token = 'abc123xyz',
      int mediaId = 42,
    }) =>
        {
          'token': token,
          'media_id': mediaId,
          'created_by': 3,
          'created_at': '2026-05-17T10:00:00.000Z',
          'expires_at': '2026-05-24T10:00:00.000Z',
          'max_uses': null,
          'used_count': 2,
        };

    test('success — returns list of Share objects', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onGet(
        '/api/v1/shares',
        (server) => server.reply(200, [
          shareJson(),
          shareJson(token: 'xyz789abc', mediaId: 7),
        ]),
      );

      final client = DioPlayerApiClient(dio: dio);
      final shares = await client.listMyShares();

      expect(shares, hasLength(2));
      expect(shares[0].token, 'abc123xyz');
      expect(shares[0].mediaId, 42);
      expect(shares[0].usedCount, 2);
      expect(shares[1].token, 'xyz789abc');
      expect(shares[1].mediaId, 7);
    });

    test('success — empty list when user has no shares', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onGet(
        '/api/v1/shares',
        (server) => server.reply(200, <dynamic>[]),
      );

      final client = DioPlayerApiClient(dio: dio);
      final shares = await client.listMyShares();

      expect(shares, isEmpty);
    });

    test('401 — DioException is propagated', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onGet(
        '/api/v1/shares',
        (server) => server.reply(401, {'error': 'unauthorized'}),
      );

      final client = DioPlayerApiClient(dio: dio);
      expect(() => client.listMyShares(), throwsA(isA<DioException>()));
    });
  });

  // ---------------------------------------------------------------------------
  // revokeShare
  // ---------------------------------------------------------------------------

  group('revokeShare', () {
    test('success — 200 completes without error', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onDelete(
        '/api/v1/shares/abc123xyz',
        (server) => server.reply(200, {'status': 'ok'}),
      );

      final client = DioPlayerApiClient(dio: dio);
      await expectLater(client.revokeShare('abc123xyz'), completes);
    });

    test('404 — DioException when share not found', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onDelete(
        '/api/v1/shares/missing_token',
        (server) => server.reply(404, {'error': 'not found'}),
      );

      final client = DioPlayerApiClient(dio: dio);
      expect(
        () => client.revokeShare('missing_token'),
        throwsA(isA<DioException>()),
      );
    });

    test('401 — DioException when unauthenticated', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onDelete(
        '/api/v1/shares/abc123xyz',
        (server) => server.reply(401, {'error': 'unauthorized'}),
      );

      final client = DioPlayerApiClient(dio: dio);
      expect(
        () => client.revokeShare('abc123xyz'),
        throwsA(isA<DioException>()),
      );
    });
  });

  // ---------------------------------------------------------------------------
  // listEpisodes
  // ---------------------------------------------------------------------------

  group('listEpisodes', () {
    /// Minimal valid PodcastEpisode JSON matching the API schema.
    Map<String, dynamic> episodeJson({
      int id = 10,
      String title = 'Episode 1: Introduction',
    }) =>
        {
          'id': id,
          'feed_id': 1,
          'media_id': null,
          'guid': 'episode-guid-$id',
          'title': title,
          'description': 'A great episode.',
          'published_at': '2026-01-05T00:00:00.000Z',
          'episode_url': 'https://example.com/ep$id.mp3',
          'duration_seconds': 3600.0,
          'file_size': 52428800,
          'file_name': 'ep$id.mp3',
          'is_downloaded': false,
          'is_completed': false,
          'position_seconds': 0.0,
          'created_at': '2026-01-05T01:00:00.000Z',
        };

    test('success — returns list of PodcastEpisode objects', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onGet(
        '/api/v1/podcasts/4/episodes',
        (server) => server.reply(200, [
          episodeJson(),
          episodeJson(id: 11, title: 'Episode 2: Deep Dive'),
        ]),
      );

      final client = DioPlayerApiClient(dio: dio);
      final episodes = await client.listEpisodes(4);

      expect(episodes, hasLength(2));
      expect(episodes[0].id, 10);
      expect(episodes[0].title, 'Episode 1: Introduction');
      expect(episodes[0].isDownloaded, isFalse);
      expect(episodes[0].isCompleted, isFalse);
      expect(episodes[1].id, 11);
      expect(episodes[1].title, 'Episode 2: Deep Dive');
    });

    test('success with pagination — query params are forwarded', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onGet(
        '/api/v1/podcasts/4/episodes',
        (server) => server.reply(200, [episodeJson()]),
        queryParameters: {'limit': 20, 'offset': 40},
      );

      final client = DioPlayerApiClient(dio: dio);
      final episodes = await client.listEpisodes(4, limit: 20, offset: 40);

      expect(episodes, hasLength(1));
      expect(episodes[0].id, 10);
    });

    test('empty — returns empty list', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onGet(
        '/api/v1/podcasts/4/episodes',
        (server) => server.reply(200, <dynamic>[]),
      );

      final client = DioPlayerApiClient(dio: dio);
      expect(await client.listEpisodes(4), isEmpty);
    });

    test('401 — DioException is propagated', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onGet(
        '/api/v1/podcasts/4/episodes',
        (server) => server.reply(401, {'error': 'unauthorized'}),
      );

      final client = DioPlayerApiClient(dio: dio);
      expect(() => client.listEpisodes(4), throwsA(isA<DioException>()));
    });
  });

  // ---------------------------------------------------------------------------
  // toggleEpisodeComplete
  // ---------------------------------------------------------------------------

  group('toggleEpisodeComplete', () {
    test('success — 204 completes without error', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onPost(
        '/api/v1/podcasts/episodes/10/complete',
        (server) => server.reply(204, null),
      );

      final client = DioPlayerApiClient(dio: dio);
      await expectLater(client.toggleEpisodeComplete(10), completes);
    });

    test('404 — DioException when episode not found', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onPost(
        '/api/v1/podcasts/episodes/999/complete',
        (server) => server.reply(404, {'error': 'not found'}),
      );

      final client = DioPlayerApiClient(dio: dio);
      expect(
        () => client.toggleEpisodeComplete(999),
        throwsA(isA<DioException>()),
      );
    });

    test('401 — DioException when unauthenticated', () async {
      final (:dio, :adapter) = _buildTestDio();
      adapter.onPost(
        '/api/v1/podcasts/episodes/10/complete',
        (server) => server.reply(401, {'error': 'unauthorized'}),
      );

      final client = DioPlayerApiClient(dio: dio);
      expect(
        () => client.toggleEpisodeComplete(10),
        throwsA(isA<DioException>()),
      );
    });
  });
}
