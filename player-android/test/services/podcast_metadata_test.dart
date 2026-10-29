import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/podcast_metadata.dart';
import 'package:player_android/models/media.dart';

void main() {
  test('show notes keep paragraphs, entities, lists and copyable safe links',
      () {
    expect(
        podcastShowNotesText(
            '<p>First &amp; second&nbsp;part.</p><p><a href="https://example.com/notes">Notes</a></p><ul><li>One</li><li>Two</li></ul>'),
        'First & second part.\n\nNotes (https://example.com/notes)\n\n• One\n\n• Two');
  });
  test(
      'notes display text and exclude scripts, embedded content and unsafe links',
      () {
    final text = podcastShowNotesText(
        '<p>Good<script>steal()</script><style>hide</style><iframe src="https://bad">bad</iframe><a href="javascript:steal()">Visible</a></p>');
    expect(text, 'GoodVisible');
    expect(podcastShowNotesText('<p>Malformed<br>readable &copy; notes'),
        'Malformed\nreadable © notes');
  });
  test('missing/invalid dates remain unknown rather than invented', () {
    for (final value in [null, 0, -1, double.infinity, '2026-10-26']) {
      expect(podcastPublicationDate(value), isNull);
    }
    expect(
        podcastPublicationDate(1793023200000), DateTime.utc(2026, 10, 26, 14));
  });
  test('artwork only permits public http URLs without embedded credentials',
      () {
    expect(podcastArtworkUrl('https://example.com/a.jpg'),
        'https://example.com/a.jpg');
    for (final value in [
      '',
      'data:image/png,bytes',
      'file:///a.jpg',
      'javascript:bad',
      'https://user:password@example.com/a'
    ]) {
      expect(podcastArtworkUrl(value), isEmpty);
    }
  });
  test(
      'canonical podcast metadata wins over embedded placeholder without changing tags',
      () {
    final source = {
      'id': 5043,
      'type': 'audio',
      'file_name': 'episode.mp3',
      'music': {'title': 'Title Placeholder Episode ID:128221282'},
      'podcast': {
        'title': 'Meta Staff Engineer: What the Best Engineers Do Differently',
        'feed_title': 'Beyond Coding',
        'published_at': '2026-10-28T13:01:04Z',
        'description': '<p>Notes</p>',
        'image_url': '',
        'feed_image_url': 'https://example.com/cover.jpg'
      }
    };
    final media = Media.fromJson(source);
    expect(media.displayTitle,
        'Meta Staff Engineer: What the Best Engineers Do Differently');
    expect(media.music!.title, 'Title Placeholder Episode ID:128221282');
    expect(media.musicSubtitle, 'Beyond Coding');
    expect(Media.fromJson(media.toJson()).podcast!.description, '<p>Notes</p>');
    expect(
        Media.fromJson({
          'type': 'audio',
          'file_name': 'song.mp3',
          'music': {'title': 'Song'}
        }).displayTitle,
        'Song');
  });
}
