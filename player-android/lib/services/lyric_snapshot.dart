import 'dart:convert';
import 'dart:io';

/// Reads only Player's local/cache API. Never starts a provider lookup.
/// A final audio validator ties metadata to the completed copy's revision.
Future<Map<String, dynamic>?> fetchLyricSnapshot({
  required Uri audioUri,
  required int mediaId,
  required Map<String, String> headers,
  required String? etag,
  required bool Function() authorized,
}) async {
  if (etag == null || etag.isEmpty || !authorized()) return null;
  final client = HttpClient()..connectionTimeout = const Duration(seconds: 5);
  try {
    return await (() async {
      final uri =
          audioUri.replace(path: '/api/v1/media/$mediaId/lyrics', query: '');
      final request = await client.getUrl(uri);
      request.followRedirects = false;
      headers.forEach(request.headers.set);
      final response = await request.close();
      if (response.statusCode != 200 || response.contentLength > 1048576) {
        return null;
      }
      final bytes = <int>[];
      await for (final chunk in response) {
        if (!authorized() || bytes.length + chunk.length > 1048576) return null;
        bytes.addAll(chunk);
      }
      final data = jsonDecode(utf8.decode(bytes));
      if (data is! Map<String, dynamic> || data['lines'] is! List) return null;
      final check = await client.headUrl(audioUri);
      check.followRedirects = false;
      headers.forEach(check.headers.set);
      final checked = await check.close();
      return authorized() &&
              checked.statusCode == 200 &&
              checked.headers.value('etag') == etag
          ? data
          : null;
    })()
        .timeout(const Duration(seconds: 8));
  } catch (_) {
    return null; // Optional metadata cannot fail an otherwise valid audio copy.
  } finally {
    client.close(force: true);
  }
}
