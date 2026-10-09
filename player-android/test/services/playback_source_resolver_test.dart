// Unit tests for resolveServerPlaybackSource: a player opened with only a
// media ID asks the server whether to play the compatibility stream.

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/api/player_api_client.dart';
import 'package:player_android/models/models.dart';
import 'package:player_android/services/playback_request.dart';
import 'package:player_android/services/playback_source_resolver.dart';

class _Client extends PlayerApiClient {
  _Client() : super(dio: Dio(BaseOptions(baseUrl: 'https://player.test')));

  Media? media;
  int lookups = 0;

  @override
  Future<Media> getMedia(int mediaId) async {
    lookups++;
    final found = media;
    if (found == null) throw StateError('lookup failed');
    return found;
  }
}

ServerPlaybackRequest _request() => ServerPlaybackRequest(
      mediaId: 7,
      serverOrigin: 'https://player.test',
      userId: 3,
      sourceUri: Uri.parse('https://player.test/api/v1/media/7/stream'),
      title: 'episode.wma',
      startPosition: 12,
    );

void main() {
  test('a transcoded item is switched to the compatibility stream', () async {
    final client = _Client()
      ..media = Media.fromJson({'id': 7, 'transcoded': true});
    final resolved = await resolveServerPlaybackSource(_request(), client);

    expect(
      resolved.sourceUri.toString(),
      'https://player.test/api/v1/media/7/compat',
    );
    // Identity, title and resume position belong to the item, not the URL.
    expect(resolved.identity, _request().identity);
    expect(resolved.title, 'episode.wma');
    expect(resolved.startPosition, 12);
  });

  test('an ordinary item keeps the original stream', () async {
    final client = _Client()..media = Media.fromJson({'id': 7});
    final resolved = await resolveServerPlaybackSource(_request(), client);
    expect(
      resolved.sourceUri.toString(),
      'https://player.test/api/v1/media/7/stream',
    );
  });

  test('a failed lookup falls back to the original stream', () async {
    final client = _Client();
    final resolved = await resolveServerPlaybackSource(_request(), client);
    expect(client.lookups, 1);
    expect(
      resolved.sourceUri.toString(),
      'https://player.test/api/v1/media/7/stream',
    );
  });

  test('local and public-share requests are never looked up', () async {
    final client = _Client();
    final local = LocalPlaybackRequest(
      localMediaId: 1,
      sourceUri: Uri.parse('content://provider/song.wma'),
      title: 'song.wma',
    );
    final share = PublicSharePlaybackRequest(
      shareToken: 'tok',
      sourceUri: Uri.parse('https://player.test/s/tok/compat'),
      title: 'shared.wmv',
    );
    expect(await resolveServerPlaybackSource(local, client), same(local));
    expect(await resolveServerPlaybackSource(share, client), same(share));
    expect(client.lookups, 0);
  });
}
