import 'dart:io';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/lyric_snapshot.dart';

void main() {
  HttpOverrides.global = null;
  for (final route in [
    'download',
    'stream',
    'compat?profile=aac128',
    'compat?profile=opus96'
  ]) {
    test('snapshot rejects preserved-mtime replacement for $route', () async {
      final directory =
          await Directory.systemTemp.createTemp('lyric-revision-');
      final source = File('${directory.path}/source.audio');
      final modified = DateTime.utc(2020);
      await source.writeAsString('AAAA');
      await source.setLastModified(modified);
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      // A deterministic local server models the backend's change-time validator;
      // Go tests prove the production original/rendition linkage for all profiles.
      server.listen((request) async {
        if (request.uri.path.endsWith('/lyrics')) {
          await source.writeAsString('BBBB');
          await source.setLastModified(modified);
          request.response.write(
              '{"source":"sidecar","synced":true,"lines":[{"text":"B lyrics","start":0}]}');
        } else {
          final stat = await source.stat();
          request.response.headers.set('etag',
              '"${stat.size}-${stat.modified.microsecondsSinceEpoch}-${stat.changed.microsecondsSinceEpoch}"');
          request.response.headers.contentType = ContentType('audio', 'mpeg');
          if (request.method != 'HEAD') {
            request.response.add(await source.readAsBytes());
          }
        }
        await request.response.close();
      });
      final client = HttpClient();
      try {
        final uri =
            Uri.parse('http://127.0.0.1:${server.port}/api/v1/media/11/$route');
        final response = await (await client.getUrl(uri)).close();
        final etag = response.headers.value('etag');
        final audio = await response
            .fold<List<int>>([], (bytes, chunk) => bytes..addAll(chunk));
        expect(String.fromCharCodes(audio), 'AAAA');
        final lyrics = await fetchLyricSnapshot(
            audioUri: uri,
            mediaId: 11,
            headers: {},
            etag: etag,
            authorized: () => true);
        expect((await source.stat()).size, 4);
        expect((await source.stat()).modified.toUtc(), modified);
        expect(lyrics, isNull,
            reason: 'B lyrics must never attach to completed A audio');
        expect(String.fromCharCodes(audio), 'AAAA',
            reason: 'optional metadata rejection preserves valid audio');
      } finally {
        client.close(force: true);
        await server.close(force: true);
        await directory.delete(recursive: true);
      }
    });
  }
}
