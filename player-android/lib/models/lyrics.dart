/// Lyrics returned by the server, with optional timestamps in seconds.
class LyricsLine {
  const LyricsLine({required this.text, this.start});

  final String text;
  final Duration? start;
}

class Lyrics {
  const Lyrics({this.source = '', this.synced = false, this.lines = const []});

  final String source;
  final bool synced;
  final List<LyricsLine> lines;

  factory Lyrics.fromJson(Object? json) {
    if (json is! Map || json['lines'] is! List) return const Lyrics();
    final lines = <LyricsLine>[];
    for (final entry in json['lines'] as List) {
      if (entry is! Map || entry['text'] is! String) continue;
      final raw = entry['start'];
      // Bound conversion to a representable Duration, including hostile data.
      final start = raw is num && raw.isFinite && raw >= 0 && raw <= 315360000
          ? Duration(microseconds: (raw * 1000000).round())
          : null;
      lines.add(LyricsLine(text: entry['text'] as String, start: start));
    }
    // Unordered or incomplete timing is still readable as plain lyrics.
    final synced = json['synced'] == true &&
        lines.isNotEmpty &&
        lines.every((line) => line.start != null) &&
        List.generate(
                lines.length - 1, (i) => lines[i].start! <= lines[i + 1].start!)
            .every((ordered) => ordered);
    return Lyrics(
      source: json['source'] is String ? json['source'] as String : '',
      synced: synced,
      lines: List.unmodifiable(lines),
    );
  }

  /// Last line whose timestamp has been reached, including after a seek back.
  int currentLine(Duration position) {
    if (!synced) return -1;
    var current = -1;
    for (var i = 0; i < lines.length; i++) {
      if (lines[i].start! > position) break;
      current = i;
    }
    return current;
  }
}
