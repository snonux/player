import 'dart:convert';
import 'music_queue.dart';

/// Durable identifiers only. Authentication and sources are rebuilt after unlock.
class MusicQueueSnapshot {
  const MusicQueueSnapshot(
      {required this.entries,
      required this.index,
      required this.position,
      required this.label,
      required this.upNext,
      this.repeat = MusicRepeat.off,
      this.shuffle = false,
      this.canonical = const []});
  final List<({int id, int mediaId})> entries;
  final int index;
  final double position;
  final String label;
  final Set<int> upNext;
  final MusicRepeat repeat;
  final bool shuffle;
  final List<int> canonical;
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
        json['upNext'] is! List ||
        (json.containsKey('shuffle') && json['shuffle'] is! bool) ||
        (json.containsKey('repeat') &&
            !MusicRepeat.values.any((mode) => mode.name == json['repeat']))) {
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
    final canonical = json.containsKey('canonical')
        ? json['canonical']
        : entries.map((e) => e.id).toList();
    if (canonical is! List ||
        canonical.length != ids.length ||
        canonical.toSet().length != ids.length ||
        canonical.any((id) => id is! int || !ids.contains(id)) ||
        (json['shuffle'] == true && !json.containsKey('canonical'))) {
      throw const FormatException('Invalid canonical order');
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
        shuffle: json['shuffle'] as bool? ?? false,
        canonical: canonical.cast<int>(),
        index: index,
        position: (json['position'] as num).toDouble(),
        label: json['label'] as String,
        upNext: (json['upNext'] as List).cast<int>().toSet(),
        repeat: MusicRepeat.values.firstWhere(
            (mode) => mode.name == json['repeat'],
            orElse: () => MusicRepeat.off));
  }

  String encode() => jsonEncode({
        'version': 1,
        'entries':
            entries.map((e) => {'id': e.id, 'mediaId': e.mediaId}).toList(),
        'index': index,
        'position': position,
        'label': label,
        'upNext': upNext.toList(),
        'repeat': repeat.name,
        'shuffle': shuffle,
        'canonical':
            canonical.isEmpty ? entries.map((e) => e.id).toList() : canonical
      });
}
