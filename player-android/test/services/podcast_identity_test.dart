import 'dart:convert';
import 'dart:io';
import 'package:flutter_test/flutter_test.dart';
import 'package:player_android/services/podcast_identity.dart';

void main() {
  final vectors = jsonDecode(
      File('../player-server/internal/podcast/testdata/identity-vectors.json')
          .readAsStringSync()) as Map<String, dynamic>;
  List<PodcastIdentityItem> items(dynamic rows) => (rows as List)
      .map((e) => PodcastIdentityItem(
          e['guid'] as String, e['enclosure_url'] as String))
      .toList();
  List<PodcastKnownEpisode> known(dynamic rows) => (rows as List)
      .map((e) => PodcastKnownEpisode(
          e['key'] as String,
          e['enclosure_key'] as String? ?? '',
          (e['aliases'] as List? ?? []).cast<String>()))
      .toList();
  test('shared feed identity vectors', () {
    for (final v in vectors['feed_keys']) {
      expect(podcastFeedKey(v['input'] as String), v['key']);
    }
    for (final url in vectors['invalid_feed_urls']) {
      expect(() => podcastFeedKey(url as String), throwsFormatException,
          reason: url);
    }
  });
  for (final v in vectors['episode_documents']) {
    test('episode document ${v['name']}',
        () => expect(podcastEpisodeKeys(items(v['items'])), v['keys']));
  }
  for (final v in vectors['ingest_scenarios']) {
    test('ingest ${v['name']}', () {
      final matches =
          podcastMatchEpisodes(known(v['known']), items(v['items']));
      expect(
          matches
              .map((m) => {'key': m.key, 'new': m.isNew, 'alias': m.alias})
              .toList(),
          v['results']);
    });
  }
  for (final v in vectors['client_resolution']) {
    test(
        'resolution ${v['name']}',
        () => expect(
            podcastResolveEpisode(known(v['known']), v['key'] as String,
                v['enclosure_url'] as String),
            v['ok'] == true ? v['resolved'] : null));
  }
}
