// Unit tests for resolveServerPlaybackSource: a player opened with only a
// media ID asks the server whether to play the compatibility stream.

import 'dart:async';

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

  /// When set, [getMedia] never answers (a hung connection).
  bool hang = false;

  CancelToken? lastToken;

  @override
  Future<Media> getMedia(int mediaId, {CancelToken? cancelToken}) async {
    lastToken = cancelToken;
    lookups++;
    if (hang) return Completer<Media>().future;
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
  group('native video source', () {
    test(
        'uses ranged original bytes and preserves server identity and progress',
        () async {
      var saved = 0.0;
      final source = ServerPlaybackRequest(
        mediaId: 7,
        serverOrigin: 'https://player.test',
        userId: 3,
        sourceUri: Uri.parse('https://player.test/api/v1/media/7/stream'),
        title: 'mpeg-ts.mp4',
        startPosition: 12,
        readPosition: () async => 12,
        savePosition: (seconds) async => saved = seconds,
      );
      final resolved = resolveNativeVideoSource(source);
      expect(resolved.sourceUri.toString(),
          'https://player.test/api/v1/media/7/download');
      expect(resolved.identity, source.identity);
      expect(resolved.title, source.title);
      expect(resolved.startPosition, 12);
      expect(await resolved.readPosition!(), 12);
      await resolved.savePosition!(25);
      expect(saved, 25);
    });

    test('public original keeps its viewing credential and path prefix', () {
      final source = PublicSharePlaybackRequest(
        shareToken: 'tok',
        sourceUri:
            Uri.parse('https://player.test/prefix/s/tok/stream?view=abc.def'),
        title: 'Shared video',
      );
      final resolved = resolveNativeVideoSource(source);
      expect(resolved.sourceUri.toString(),
          'https://player.test/prefix/s/tok/download?view=abc.def');
      expect(resolved.identity, source.identity);
      expect(resolved.title, source.title);
    });

    test('compatibility streams and unrelated URLs stay unchanged', () {
      for (final path in [
        '/api/v1/media/7/compat',
        '/api/v1/media/7/download',
        '/s/tok/compat?view=abc.def',
        '/elsewhere/stream',
      ]) {
        final source =
            _request().withSourceUri(Uri.parse('https://player.test$path'));
        expect(resolveNativeVideoSource(source), same(source));
      }
    });

    test('legacy API endpoint and server path prefix use original bytes', () {
      final source = _request().withSourceUri(
          Uri.parse('https://player.test/prefix/api/media/7/stream'));
      expect(resolveNativeVideoSource(source).sourceUri.toString(),
          'https://player.test/prefix/api/media/7/download');
    });

    test('device and offline sources stay unchanged', () {
      final sources = [
        LocalPlaybackRequest(
            localMediaId: 1,
            sourceUri: Uri.parse('content://provider/video/1'),
            title: 'video'),
        OfflinePlaybackRequest(
            downloadKey: 'copy',
            sourceUri: Uri.parse('file:///private/video.mp4'),
            title: 'video'),
      ];
      for (final source in sources) {
        expect(resolveNativeVideoSource(source), same(source));
      }
    });
  });

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

  test('a lookup that never answers is given up after the timeout', () async {
    final client = _Client()..hang = true;
    final resolved = await resolveServerPlaybackSource(
      _request(),
      client,
      timeout: const Duration(milliseconds: 20),
    );
    expect(
      resolved.sourceUri.toString(),
      'https://player.test/api/v1/media/7/stream',
    );
    // The abandoned request is cancelled, not left running.
    expect(client.lastToken!.isCancelled, isTrue);
  });

  test('a lookup that answers in time is not cancelled', () async {
    final client = _Client()..media = Media.fromJson({'id': 7});
    await resolveServerPlaybackSource(_request(), client);
    expect(client.lastToken!.isCancelled, isFalse);
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
