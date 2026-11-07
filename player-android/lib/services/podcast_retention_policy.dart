/// Device expiry is a derived view of registers, never a synchronized deletion.
/// A remote replay, restore or completion replacement immediately removes the
/// hide even before another maintenance pass observes the new completion.
bool podcastRetentionExpired(Map<String, Object?> row, int nowMs) {
  final completion = row['completion_ms'] as int? ?? 0;
  final seen = row['retention_seen_ms'] as int? ?? 0;
  return row['completed'] == 1 &&
      completion > 0 &&
      row['retention_completion_ms'] == completion &&
      seen > 0 &&
      seen <= nowMs - const Duration(days: 7).inMilliseconds &&
      (row['position_ms'] as int? ?? 0) <= completion &&
      (row['deleted'] == 1 || (row['deletion_ms'] as int? ?? 0) < completion);
}
