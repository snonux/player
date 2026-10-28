import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/podcast_discovery.dart';

void main() {
  test(
      'directory results preserve publishers and usable feed URLs, without duplicates',
      () {
    final rows = parsePodcastDiscovery('''{"results":[
      {"collectionName":"Science & Space","artistName":"Publisher","feedUrl":"https://example.org/feed"},
      {"collectionName":"Duplicate","feedUrl":"https://example.org/feed"},
      {"collectionName":"Missing feed"},
      {"collectionName":"Unsafe","feedUrl":"javascript:alert(1)"},
      {"collectionName":"Credentials","feedUrl":"https://user:pass@example.org/feed"}
    ]}''');
    expect(rows.length, 1);
    expect(rows.single.title, 'Science & Space');
    expect(rows.single.author, 'Publisher');
    expect(rows.single.feedUrl, 'https://example.org/feed');
  });
  test('invalid queries never contact the directory', () async {
    for (final query in ['', 'a', 'x' * 101]) {
      await expectLater(
          PodcastDiscovery().search(query), throwsFormatException);
    }
  });
}
