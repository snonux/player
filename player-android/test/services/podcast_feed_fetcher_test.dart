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
}
