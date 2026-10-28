import 'dart:async';
import 'dart:convert';
import 'dart:io';

class DiscoveredPodcast {
  const DiscoveredPodcast(this.title, this.author, this.feedUrl);
  final String title, author, feedUrl;
}

/// Public directory requests use a separate client without Player credentials.
/// Only explicit searches make requests; no artwork or feeds are prefetched.
class PodcastDiscovery {
  Future<List<DiscoveredPodcast>> search(String query) async {
    query = query.trim();
    if (query.runes.length < 2 || query.runes.length > 100) {
      throw const FormatException('Enter 2 to 100 characters to search.');
    }
    final client = HttpClient()
      ..connectionTimeout = const Duration(seconds: 10);
    try {
      return await _search(client, query).timeout(const Duration(seconds: 15));
    } finally {
      client.close(force: true);
    }
  }

  Future<List<DiscoveredPodcast>> popular(String country) async {
    if (!RegExp(r'^[A-Z]{2}$').hasMatch(country)) {
      throw const FormatException('Choose a chart country.');
    }
    final client = HttpClient()
      ..connectionTimeout = const Duration(seconds: 10);
    try {
      return await _popular(client, country)
          .timeout(const Duration(seconds: 30));
    } finally {
      client.close(force: true);
    }
  }

  Future<List<DiscoveredPodcast>> _popular(
      HttpClient client, String country) async {
    final chart = jsonDecode(await _get(
            client,
            Uri.https('itunes.apple.com',
                '/${country.toLowerCase()}/rss/toppodcasts/limit=20/json')))
        as Map<String, dynamic>;
    final entries =
        ((chart['feed'] as Map<String, dynamic>)['entry'] as List<dynamic>);
    final ids = <String>[];
    for (final entry in entries.take(20)) {
      final id = entry['id']['attributes']['im:id'];
      if (id is String && RegExp(r'^[0-9]+$').hasMatch(id)) ids.add(id);
    }
    if (ids.isEmpty) return [];
    return parsePodcastDiscovery(await _get(
        client,
        Uri.https('itunes.apple.com', '/lookup',
            {'id': ids.join(','), 'entity': 'podcast', 'country': country})));
  }

  Future<List<DiscoveredPodcast>> _search(
      HttpClient client, String query) async {
    return parsePodcastDiscovery(await _get(
        client,
        Uri.https('itunes.apple.com', '/search', {
          'term': query,
          'media': 'podcast',
          'entity': 'podcast',
          'limit': '30',
          'country': 'US'
        })));
  }

  Future<String> _get(HttpClient client, Uri uri) async {
    final request = await client.getUrl(uri);
    request.followRedirects = false;
    request.headers.set(HttpHeaders.acceptHeader, 'application/json');
    final response = await request.close();
    if (response.statusCode != 200) {
      throw const HttpException(
          'Podcast directory unavailable. Retry shortly.');
    }
    final bytes = <int>[];
    await for (final chunk in response) {
      bytes.addAll(chunk);
      if (bytes.length > 2 * 1024 * 1024) {
        throw const FormatException('Podcast directory response is too large.');
      }
    }
    return utf8.decode(bytes);
  }
}

List<DiscoveredPodcast> parsePodcastDiscovery(String body) {
  final payload = jsonDecode(body) as Map<String, dynamic>;
  final seen = <String>{};
  final results = <DiscoveredPodcast>[];
  for (final item in payload['results'] as List<dynamic>) {
    if (item is! Map<String, dynamic>) continue;
    final title = item['collectionName'];
    final url = item['feedUrl'];
    final uri = url is String ? Uri.tryParse(url) : null;
    if (title is! String ||
        title.isEmpty ||
        uri == null ||
        !['http', 'https'].contains(uri.scheme) ||
        uri.host.isEmpty ||
        uri.userInfo.isNotEmpty ||
        !seen.add(url as String)) {
      continue;
    }
    results.add(
        DiscoveredPodcast(title, item['artistName'] as String? ?? '', url));
    if (results.length == 30) break;
  }
  return results;
}
