import 'dart:convert';
import 'dart:io';
import 'package:xml/xml.dart';
import 'podcast_identity.dart';

class PublisherEpisode {
  const PublisherEpisode(this.identity, this.title, this.description,
      this.publishedAt, this.durationSeconds);
  final PodcastIdentityItem identity;
  final String title, description;
  final int publishedAt, durationSeconds;
}

class PublisherFeed {
  const PublisherFeed(
      {required this.title,
      required this.description,
      required this.imageUrl,
      required this.fetchUrl,
      required this.aliasUrls,
      required this.episodes,
      this.etag = '',
      this.lastModified = '',
      this.notModified = false});
  final String title, description, imageUrl, fetchUrl, etag, lastModified;
  final List<String> aliasUrls;
  final List<PublisherEpisode> episodes;
  final bool notModified;
}

/// Separate client: publisher requests never carry Player tokens or cookies.
class PodcastFeedFetcher {
  Future<PublisherFeed> fetch(String url,
      {String etag = '', String lastModified = ''}) async {
    final client = HttpClient()
      ..connectionTimeout = const Duration(seconds: 15);
    try {
      return await _fetch(client, url, etag, lastModified)
          .timeout(const Duration(seconds: 30));
    } finally {
      client.close(force: true);
    }
  }

  Future<PublisherFeed> _fetch(
      HttpClient client, String url, String etag, String modified) async {
    podcastFeedKey(url);
    var uri = Uri.parse(url);
    var fetchUrl = url;
    final aliases = <String>[];
    HttpClientResponse response;
    var hops = 0;
    while (true) {
      if (uri.scheme != 'http' && uri.scheme != 'https') {
        throw const FormatException('Publisher URL needs http or https');
      }
      final request = await client.getUrl(uri);
      request.followRedirects = false;
      request.headers.set(HttpHeaders.acceptHeader,
          'application/rss+xml, application/atom+xml, application/xml, text/xml');
      if (etag.isNotEmpty) {
        request.headers.set(HttpHeaders.ifNoneMatchHeader, etag);
      }
      if (modified.isNotEmpty) {
        request.headers.set(HttpHeaders.ifModifiedSinceHeader, modified);
      }
      response = await request.close();
      if (![301, 302, 303, 307, 308].contains(response.statusCode)) break;
      final location = response.headers.value(HttpHeaders.locationHeader);
      if (location == null || ++hops > 10) {
        throw const HttpException('Too many or invalid feed redirects');
      }
      final target = uri.resolve(location);
      podcastFeedKey(target.toString());
      if (response.statusCode == 301 || response.statusCode == 308) {
        aliases.add(target.toString());
        fetchUrl = target.toString();
      }
      await response.drain<void>();
      uri = target;
    }
    if (response.statusCode == 304) {
      return PublisherFeed(
          title: '',
          description: '',
          imageUrl: '',
          fetchUrl: fetchUrl,
          aliasUrls: aliases,
          episodes: const [],
          notModified: true);
    }
    if (response.statusCode != 200) {
      throw HttpException('Publisher returned HTTP ${response.statusCode}');
    }
    final bytes = <int>[];
    await for (final chunk in response) {
      bytes.addAll(chunk);
      if (bytes.length > 8 * 1024 * 1024) {
        throw const FormatException('Feed exceeds 8 MB');
      }
    }
    final parsed = parsePodcastFeed(utf8.decode(bytes),
        fetchUrl: fetchUrl, aliasUrls: aliases);
    return PublisherFeed(
        title: parsed.title,
        description: parsed.description,
        imageUrl: parsed.imageUrl,
        fetchUrl: parsed.fetchUrl,
        aliasUrls: parsed.aliasUrls,
        episodes: parsed.episodes,
        etag: response.headers.value(HttpHeaders.etagHeader) ?? '',
        lastModified:
            response.headers.value(HttpHeaders.lastModifiedHeader) ?? '');
  }
}

String _text(XmlElement node, String name) =>
    node.childElements
        .where((e) => e.name.local == name)
        .firstOrNull
        ?.innerText ??
    '';

DateTime? _publicationDate(String value) {
  final date = value.trim();
  final iso = DateTime.tryParse(date);
  if (iso != null) return iso;
  // HttpDate only accepts GMT, while RSS commonly uses RFC 822 offsets.
  final numericZone = RegExp(r'^(.*\s)([+-])(\d{2})(\d{2})$').firstMatch(date);
  try {
    if (numericZone == null) return HttpDate.parse(date);
    final hours = int.parse(numericZone[3]!);
    final minutes = int.parse(numericZone[4]!);
    if (hours > 23 || minutes > 59) return null;
    final local = HttpDate.parse('${numericZone[1]}GMT');
    final offset = Duration(hours: hours, minutes: minutes);
    return numericZone[2] == '+' ? local.subtract(offset) : local.add(offset);
  } on HttpException {
    return null; // Undated and malformed episodes sort last.
  }
}

PublisherFeed parsePodcastFeed(String xml,
    {required String fetchUrl, List<String> aliasUrls = const []}) {
  final root = XmlDocument.parse(xml).rootElement;
  final atom = root.name.local == 'feed';
  final channel = atom
      ? root
      : root.childElements.where((e) => e.name.local == 'channel').firstOrNull;
  if (channel == null) {
    throw const FormatException('Not an RSS or Atom podcast feed');
  }
  var image = '';
  for (final e in channel.childElements.where((e) => e.name.local == 'image')) {
    image = e.getAttribute('href') ?? _text(e, 'url');
    if (image.isNotEmpty) break;
  }
  final moved = _text(channel, 'new-feed-url');
  if (moved.isNotEmpty) {
    podcastFeedKey(moved);
    aliasUrls = [...aliasUrls, moved];
    fetchUrl = moved;
  }
  final episodes = <PublisherEpisode>[];
  for (final item in channel.childElements
      .where((e) => e.name.local == (atom ? 'entry' : 'item'))) {
    final enclosure = item.childElements
        .where((e) => atom
            ? e.name.local == 'link' && e.getAttribute('rel') == 'enclosure'
            : e.name.local == 'enclosure')
        .firstOrNull;
    final url = enclosure?.getAttribute(atom ? 'href' : 'url') ?? '';
    final date = _text(item, atom ? 'published' : 'pubDate');
    final published = _publicationDate(date);
    final parts = _text(item, 'duration').split(':');
    var duration = 0;
    for (final part in parts) {
      duration = duration * 60 + (int.tryParse(part) ?? 0);
    }
    episodes.add(PublisherEpisode(
        PodcastIdentityItem(_text(item, atom ? 'id' : 'guid'), url),
        _text(item, 'title'),
        _text(item, atom ? 'summary' : 'description'),
        published?.millisecondsSinceEpoch ?? 0,
        duration));
  }
  return PublisherFeed(
      title: _text(channel, 'title'),
      description: _text(channel, atom ? 'subtitle' : 'description'),
      imageUrl: image,
      fetchUrl: fetchUrl,
      aliasUrls: aliasUrls,
      episodes: episodes);
}
