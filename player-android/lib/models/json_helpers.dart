// JSON conversion helpers shared by all model classes.
//
// `dateTimeFromJson` is defensive against malformed server responses: it
// accepts any dynamic JSON value, rejects null/non-string/empty input up
// front, and catches `FormatException` from `DateTime.parse` so that an
// unexpected date string degrades to null instead of crashing the entire
// model deserialization. This protects every consumer (Media, MediaSet,
// User, PodcastFeed, PodcastEpisode, PlaybackHint, Share, Note, ...) from
// a single bad field propagating an exception up the JSON decode stack.
DateTime? dateTimeFromJson(Object? value) {
  if (value is! String || value.isEmpty) return null;
  try {
    return DateTime.parse(value);
  } on FormatException catch (e) {
    // Defensive: server returned an unexpected date string. Log and
    // degrade to null rather than crashing model deserialization.
    // ignore: avoid_print
    print('dateTimeFromJson: failed to parse "$value": $e');
    return null;
  }
}

String? dateTimeToJson(DateTime? value) => value?.toIso8601String();
