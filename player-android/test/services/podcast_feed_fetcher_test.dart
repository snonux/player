import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/podcast_feed_fetcher.dart';

void main() {
  final expected = DateTime.utc(2026, 10, 26, 14).millisecondsSinceEpoch;
  for (final date in [
    'Mon, 26 Oct 2026 14:00:00 +0000',
    'Mon, 26 Oct 2026 14:00:00 -0000',
    'Mon, 26 Oct 2026 19:30:00 +0530',
    'Mon, 26 Oct 2026 10:30:00 -0330',
    'Tue, 27 Oct 2026 03:00:00 +1300',
    'Mon, 26 Oct 2026 02:00:00 -1200',
    'Mon, 26 Oct 2026 14:00:00 GMT',
    'Mon, 26 Oct 2026 14:00:00 UT',
    'Mon, 26 Oct 2026 14:00:00 UTC',
    'Mon, 26 Oct 2026 09:00:00 EST',
    'Mon, 26 Oct 2026 10:00:00 EDT',
    'Mon, 26 Oct 2026 08:00:00 CST',
    'Mon, 26 Oct 2026 09:00:00 CDT',
    'Mon, 26 Oct 2026 07:00:00 MST',
    'Mon, 26 Oct 2026 08:00:00 MDT',
    'Mon, 26 Oct 2026 06:00:00 PST',
    'Mon, 26 Oct 2026 07:00:00 PDT',
    '2026-10-26T19:30:00+05:30',
    '2026-10-26T14:00:00Z',
    '  Mon, 26 Oct 2026 14:00:00 +0000  ',
  ]) {
    test('RSS publication date $date', () {
      final feed = parsePodcastFeed(
          '<rss><channel><item><guid>stable</guid><pubDate>$date</pubDate>'
          '<enclosure url="https://example.com/episode.mp3"/></item></channel></rss>',
          fetchUrl: 'https://example.com/feed');
      expect(feed.episodes.single.publishedAt, expected);
      expect(feed.episodes.single.identity.guid, 'stable');
    });
  }
  for (final sample in <String, DateTime>{
    'Wed, 28 Oct 2026 12:57:50 +0000': DateTime.utc(2026, 10, 28, 12, 57, 50),
    'Tue, 06 Oct 2026 09:25:40 PDT': DateTime.utc(2026, 10, 6, 16, 25, 40),
    'Thu, 11 Jan 2024 00:00:00 PST': DateTime.utc(2024, 1, 11, 8),
  }.entries) {
    test('observed Planet Money / FLOSS publication date ${sample.key}', () {
      final feed = parsePodcastFeed(
          '<rss><channel><item><guid>stable</guid><pubDate>${sample.key}</pubDate><enclosure url="https://example.com/a.mp3"/></item></channel></rss>',
          fetchUrl: 'https://example.com/feed');
      expect(feed.episodes.single.publishedAt,
          sample.value.millisecondsSinceEpoch);
    });
  }
  test('Atom publication date preserves ISO offset support', () {
    final feed = parsePodcastFeed(
        '<feed xmlns="http://www.w3.org/2005/Atom"><entry><id>stable</id>'
        '<published>2026-10-26T10:30:00-03:30</published></entry></feed>',
        fetchUrl: 'https://example.com/feed');
    expect(feed.episodes.single.publishedAt, expected);
  });
  for (final date in [
    '',
    'not a date',
    'Mon, 26 Oct 2026 14:00:00 IST',
    'Mon, 26 Oct 2026 14:00:00 +2400',
    'Mon, 26 Oct 2026 14:00:00 -0360',
    'Mon, 26 Oct 2026 14:00:00 +000',
    'Mon, 26 Oct 2026 14:00:00 +abcd',
    'Mon, 26 Bad 2026 14:00:00 +0000',
  ]) {
    test('malformed RSS publication date stays undated: $date', () {
      final feed = parsePodcastFeed(
          '<rss><channel><item><pubDate>$date</pubDate></item></channel></rss>',
          fetchUrl: 'https://example.com/feed');
      expect(feed.episodes.single.publishedAt, 0);
    });
  }
  test(
      'episode artwork and full notes prefer publisher content with feed fallback',
      () {
    final feed = parsePodcastFeed(
        '''<rss xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd" xmlns:content="http://purl.org/rss/1.0/modules/content/"><channel>
      <title>Publisher</title><itunes:image href="/feed.jpg"/>
      <item><guid>one</guid><itunes:image href="../one.jpg"/><description>Short summary</description><content:encoded><![CDATA[<p>Full &amp; useful notes</p>]]></content:encoded></item>
      <item><guid>two</guid><itunes:image href="javascript:bad"/><description>Only summary</description></item>
    </channel></rss>''',
        fetchUrl: 'https://publisher.example/rss/feed');
    expect(feed.imageUrl, 'https://publisher.example/feed.jpg');
    expect(feed.episodes.first.imageUrl, 'https://publisher.example/one.jpg');
    expect(feed.episodes.first.description, '<p>Full &amp; useful notes</p>');
    expect(feed.episodes.last.imageUrl, isEmpty);
    expect(feed.episodes.last.description, 'Only summary');
  });
  test('Atom logo and XHTML notes retain readable content and date', () {
    final feed = parsePodcastFeed(
        '''<feed xmlns="http://www.w3.org/2005/Atom"><logo>/logo.png</logo><entry><id>one</id><published>2026-10-26T14:00:00Z</published><summary>Short</summary><content type="xhtml"><div xmlns="http://www.w3.org/1999/xhtml"><p>Paragraph</p><a href="https://example.com">Link</a></div></content></entry></feed>''',
        fetchUrl: 'https://publisher.example/rss');
    expect(feed.imageUrl, 'https://publisher.example/logo.png');
    expect(feed.episodes.single.description, contains('<p>Paragraph</p>'));
    expect(feed.episodes.single.publishedAt, expected);
  });
  test('publisher artwork rejects credential-bearing and non-http sources', () {
    final feed = parsePodcastFeed(
        '''<rss xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd"><channel><itunes:image href="https://user:pass@example.com/a.jpg"/><item><itunes:image href="data:image/png,bytes"/></item></channel></rss>''',
        fetchUrl: 'https://publisher.example/feed');
    expect(feed.imageUrl, isEmpty);
    expect(feed.episodes.single.imageUrl, isEmpty);
  });
}
