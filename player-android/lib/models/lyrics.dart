import 'dart:convert';

/// A fragment with absolute playback timing, independent of its voice.
class LyricsWord {
  const LyricsWord({required this.text, required this.start, this.end});
  final String text;
  final Duration start;
  final Duration? end;
}

/// Lyrics returned by the server, with optional timestamps in seconds.
class LyricsLine {
  const LyricsLine(
      {required this.text,
      this.start,
      this.end,
      this.voice = '',
      this.words = const [],
      this.translations = const {}});
  final String text;
  final Duration? start;
  final Duration? end;
  final String voice;
  final List<LyricsWord> words;
  final Map<String, String> translations;

  String displayText(String language) => translations[language] ?? text;
  int currentWord(Duration position) {
    if (end != null && position >= end!) return -1;
    var current = -1;
    for (var i = 0; i < words.length; i++) {
      if (words[i].start > position) break;
      current = i;
    }
    if (current < 0) return -1;
    final until = words[current].end ??
        (current + 1 < words.length ? words[current + 1].start : end);
    return until == null || position < until ? current : -1;
  }
}

Duration? _time(Object? raw) =>
    raw is num && raw.isFinite && raw >= 0 && raw <= 1e9
        ? Duration(microseconds: (raw * 1000000).round())
        : null;
bool _language(Object? value) =>
    value is String &&
    value.length <= 64 &&
    RegExp(r'^[A-Za-z]{2,8}(?:-[A-Za-z0-9]{1,8})*$').hasMatch(value);

class Lyrics {
  const Lyrics(
      {this.source = '',
      this.synced = false,
      this.lines = const [],
      this.language = '',
      this.providerEnabled = false,
      this.providerState = '',
      this.saved = false});
  final bool saved;
  final bool providerEnabled;
  final String providerState;
  final String source;
  final bool synced;
  final List<LyricsLine> lines;
  final String language;
  List<String> get languages =>
      (lines.expand((line) => line.translations.keys).toSet().toList()..sort());

  factory Lyrics.fromJson(Object? json) {
    if (json is! Map || json['lines'] is! List) return const Lyrics();
    final lines = <LyricsLine>[];
    for (final entry in json['lines'] as List) {
      if (entry is! Map || entry['text'] is! String) continue;
      final start = _time(entry['start']);
      var end = _time(entry['end']);
      if (start == null || end != null && end <= start) end = null;
      final translations = <String, String>{};
      final rawTranslations = entry['translations'];
      if (rawTranslations is Map && rawTranslations.length <= 32) {
        for (final translation in rawTranslations.entries) {
          if (_language(translation.key) &&
              translation.value is String &&
              (translation.value as String).trim().isNotEmpty) {
            translations[translation.key as String] =
                translation.value as String;
          }
        }
      }
      var words = <LyricsWord>[];
      final rawWords = entry['words'];
      if (start != null && rawWords is List && rawWords.length <= 32768) {
        for (final word in rawWords) {
          final wordStart = word is Map ? _time(word['start']) : null;
          final wordEnd = word is Map ? _time(word['end']) : null;
          if (word is! Map ||
              word['text'] is! String ||
              wordStart == null ||
              wordStart < start ||
              words.isNotEmpty && wordStart < words.last.start ||
              end != null && wordStart >= end ||
              word['end'] != null &&
                  (wordEnd == null ||
                      wordEnd <= wordStart ||
                      end != null && wordEnd > end)) {
            words = [];
            break;
          }
          words.add(LyricsWord(
              text: word['text'] as String, start: wordStart, end: wordEnd));
        }
        if (words.map((word) => word.text).join() != entry['text']) words = [];
      }
      lines.add(LyricsLine(
          text: entry['text'] as String,
          start: start,
          end: end,
          voice: entry['voice'] is String &&
                  utf8.encode(entry['voice'] as String).length <= 128
              ? entry['voice'] as String
              : '',
          words: List.unmodifiable(words),
          translations: Map.unmodifiable(translations)));
    }
    final synced = json['synced'] == true &&
        lines.isNotEmpty &&
        lines.every((line) => line.start != null) &&
        List.generate(
                lines.length - 1, (i) => lines[i].start! <= lines[i + 1].start!)
            .every((ordered) => ordered);
    return Lyrics(
        saved: json['saved'] == true,
        providerEnabled: json['provider_enabled'] == true,
        providerState: json['provider_state'] is String
            ? json['provider_state'] as String
            : '',
        source: json['source'] is String ? json['source'] as String : '',
        language: _language(json['language']) ? json['language'] as String : '',
        synced: synced,
        lines: List.unmodifiable(lines));
  }

  /// Explicit ends permit overlaps; implicit ends use the next distinct start.
  Set<int> activeLines(Duration position) {
    if (!synced) return {};
    final active = <int>{};
    Duration? next;
    for (var i = lines.length - 1; i >= 0; i--) {
      if (i + 1 < lines.length && lines[i + 1].start! > lines[i].start!) {
        next = lines[i + 1].start;
      }
      final until = lines[i].end ?? next;
      if (position >= lines[i].start! && (until == null || position < until)) {
        active.add(i);
      }
    }
    return active;
  }

  /// Last active row is the current banner/follow anchor, including after seeks.
  int currentLine(Duration position) {
    final active = activeLines(position);
    return active.isEmpty ? -1 : active.first;
  }
}
