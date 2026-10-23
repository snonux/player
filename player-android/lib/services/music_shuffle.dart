import 'dart:math';
import '../models/media.dart';
import 'music_queue.dart';
import 'playback_request.dart';

/// Uniform occurrence shuffle; the optional RNG is a pure algorithm test seam.
List<T> shuffledCopy<T>(List<T> values, [Random? random]) =>
    List<T>.of(values)..shuffle(random);

List<MusicQueueEntry> orderMusicRemainder(List<MusicQueueEntry> entries,
    int index, List<int> canonical, Set<int> pinned, bool shuffle,
    [Random? random]) {
  final remaining = entries.skip(index + 1).toList();
  final next = remaining.where((e) => pinned.contains(e.id));
  final rest = remaining.where((e) => !pinned.contains(e.id)).toList();
  final rank = {for (var i = 0; i < canonical.length; i++) canonical[i]: i};
  if (shuffle) {
    rest.shuffle(random);
  } else {
    rest.sort((a, b) => rank[a.id]!.compareTo(rank[b.id]!));
  }
  return [...entries.take(index + 1), ...next, ...rest];
}

class MusicShuffleOptions {
  const MusicShuffleOptions(
      {this.genre = '', this.from, this.to, this.maximum});
  final String genre;
  final int? from, to, maximum;
  factory MusicShuffleOptions.parse(
      {String genre = '',
      String from = '',
      String to = '',
      String maximum = ''}) {
    int? number(String value, int max, String name) {
      value = value.trim();
      if (value.isEmpty) return null;
      final result = int.tryParse(value);
      if (!RegExp(r'^\d+$').hasMatch(value) ||
          result == null ||
          result < 1 ||
          result > max) {
        throw FormatException('$name must be a whole number from 1 to $max.');
      }
      return result;
    }

    final first = number(from, 9999, 'From year');
    final last = number(to, 9999, 'To year');
    if (first != null && last != null && first > last) {
      throw const FormatException('From year must not be after To year.');
    }
    return MusicShuffleOptions(
        genre: genre.trim().toLowerCase(),
        from: first,
        to: last,
        maximum: number(maximum, 10000, 'Maximum songs'));
  }

  List<Media> select(List<Media> songs, [Random? random]) {
    final eligible = songs.where((song) {
      final metadata = song.music?.toJson();
      final year = metadata?['year'] as int? ?? 0;
      final genres = (metadata?['genres'] as List? ?? []).whereType<String>();
      return song.type == 'audio' &&
          (genre.isEmpty ||
              genres.any((g) => g.trim().toLowerCase() == genre)) &&
          (from == null || year >= from!) &&
          (to == null || (year > 0 && year <= to!));
    }).toList();
    if (maximum == null || eligible.length <= maximum!) return eligible;
    final indexes =
        shuffledCopy(List<int>.generate(eligible.length, (i) => i), random)
            .take(maximum!)
            .toSet();
    return [
      for (var i = 0; i < eligible.length; i++)
        if (indexes.contains(i)) eligible[i]
    ];
  }
}

/// Server catalog membership; missing entries remain independent occurrences.
List<MusicQueueEntry> shuffledMusicAlbums(
    List<MusicQueueEntry> entries, Map<String, dynamic> albums,
    [Random? random, MusicQueueEntry? current]) {
  final groups = <Object, List<MusicQueueEntry>>{};
  Map? membership(MusicQueueEntry e) => e.request is ServerPlaybackRequest
      ? albums['${(e.request as ServerPlaybackRequest).mediaId}'] as Map?
      : null;
  for (final entry in entries) {
    final key = membership(entry)?['album_key'] ?? entry;
    (groups[key] ??= []).add(entry);
  }
  for (final group in groups.values) {
    // Stable ties preserve repeated occurrences of the same song.
    final rank = {for (var i = 0; i < group.length; i++) group[i].id: i};
    group.sort((a, b) => ((membership(a)?['order'] as int? ?? 0)
                .compareTo(membership(b)?['order'] as int? ?? 0)) !=
            0
        ? (membership(a)!['order'] as int)
            .compareTo(membership(b)!['order'] as int)
        : rank[a.id]!.compareTo(rank[b.id]!));
  }
  final key = current == null ? null : membership(current)?['album_key'];
  final continuation = groups.remove(key) ?? <MusicQueueEntry>[];
  return [
    ...continuation,
    ...shuffledCopy(groups.values.toList(), random).expand((g) => g)
  ];
}
