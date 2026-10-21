import 'dart:convert';

/// Durable identifiers only. Authentication and sources are rebuilt after unlock.
class MusicQueueSnapshot {
  const MusicQueueSnapshot(
      {required this.entries,
      required this.index,
      required this.position,
      required this.label,
      required this.upNext});
  final List<({int id, int mediaId})> entries;
  final int index;
  final double position;
  final String label;
  final Set<int> upNext;
  static String key(String origin, int userId) =>
      'player.queue.$origin.$userId';

  static MusicQueueSnapshot decode(String raw) {
    final json = jsonDecode(raw);
    if (json is! Map ||
        json['version'] != 1 ||
        json['entries'] is! List ||
        (json['entries'] as List).length > 10000 ||
        json['index'] is! int ||
        json['position'] is! num ||
        !(json['position'] as num).isFinite ||
        json['position'] < 0 ||
        json['label'] is! String ||
        (json['label'] as String).length > 2000 ||
        json['upNext'] is! List) {
      throw const FormatException('Invalid queue');
    }
    final entries = <({int id, int mediaId})>[];
    final ids = <int>{};
    for (final entry in json['entries']) {
      if (entry is! Map ||
          entry['id'] is! int ||
          entry['id'] <= 0 ||
          !ids.add(entry['id'] as int) ||
          entry['mediaId'] is! int ||
          entry['mediaId'] <= 0) {
        throw const FormatException('Invalid queue entry');
      }
      entries.add((id: entry['id'] as int, mediaId: entry['mediaId'] as int));
    }
    final index = json['index'] as int;
    if (index < -1 ||
        index >= entries.length ||
        (entries.isNotEmpty && index < 0) ||
        (json['upNext'] as List).any((id) => id is! int || !ids.contains(id))) {
      throw const FormatException('Invalid queue selection');
    }
    return MusicQueueSnapshot(
        entries: entries,
        index: index,
        position: (json['position'] as num).toDouble(),
        label: json['label'] as String,
        upNext: (json['upNext'] as List).cast<int>().toSet());
  }

  String encode() => jsonEncode({
        'version': 1,
        'entries':
            entries.map((e) => {'id': e.id, 'mediaId': e.mediaId}).toList(),
        'index': index,
        'position': position,
        'label': label,
        'upNext': upNext.toList()
      });
}
