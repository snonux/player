import 'dart:math';

/// Discovery order over albums already filtered for this account's access.
const albumCollections = {
  'recently_added': [
    'Recently added',
    'Latest songs added to your accessible library.',
    'No albums yet. Ask an administrator to scan music.'
  ],
  'recently_played': [
    'Recently played',
    'Your latest qualified music plays.',
    'Play music to start your listening history.'
  ],
  'most_played': [
    'Most played',
    'Albums ranked by your qualified song plays.',
    'No qualified plays yet. Listen to music to build this collection.'
  ],
  'random': [
    'Random albums',
    'A fresh selection from your accessible library.',
    'No albums available.'
  ],
  'favorites': [
    'Favorite albums',
    'Albums you have favorited.',
    'Favorite an album using its heart button.'
  ],
  'top_rated': [
    'Top rated albums',
    'Albums ranked by your own star ratings.',
    'Rate an album to add it here.'
  ],
};

List<Map<String, dynamic>> discoverAlbums(
    List<Map<String, dynamic>> albums, String collection,
    {Random? random}) {
  final items = albums
      .where((a) => switch (collection) {
            'recently_played' => a['last_played_at'] != null,
            'most_played' => (a['personal_play_count'] as num? ?? 0) > 0,
            'favorites' => a['is_favorite'] == true,
            'top_rated' => (a['rating'] as num? ?? 0) > 0,
            _ => true,
          })
      .toList();
  if (collection == 'random') return items..shuffle(random);
  int value(Map<String, dynamic> album) => switch (collection) {
        'recently_added' =>
          DateTime.tryParse('${album['added_at']}')?.millisecondsSinceEpoch ??
              0,
        'recently_played' => DateTime.tryParse('${album['last_played_at']}')
                ?.millisecondsSinceEpoch ??
            0,
        'most_played' => (album['personal_play_count'] as num? ?? 0).toInt(),
        'top_rated' => (album['rating'] as num? ?? 0).toInt(),
        _ => 0,
      };
  String title(Map<String, dynamic> a) =>
      '${a['title']}'.toLowerCase().trim().replaceAll(RegExp(r'\s+'), ' ');
  items.sort((a, b) {
    final rank = value(b).compareTo(value(a));
    if (rank != 0) return rank;
    final name = title(a).compareTo(title(b));
    return name != 0
        ? name
        : '${a['album_key']}'.compareTo('${b['album_key']}');
  });
  return items;
}
