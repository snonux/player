// Duration formatting utilities shared across screen widgets.
//
// Centralises the conversion of fractional seconds to a human-readable string
// so that [MediaGridScreen] and [FolderBrowserScreen] do not each carry their
// own copy of the same logic (DRY / Single Responsibility).
//
// All functions are pure data-transformations: no widget state, no Riverpod
// reads, no BuildContext — making them easy to unit-test in isolation.

/// Formats [seconds] as `h:mm:ss` or `m:ss`, omitting leading zeros in the
/// hours and minutes positions.
///
/// Examples:
///   - 7320.0 → "2:02:00"
///   - 210.5  → "3:30"
///   - 30.0   → "0:30"
///
/// Uses integer arithmetic only — no [Duration] dependency — so the helper
/// is lightweight and independently testable.
String formatDuration(double seconds) {
  final total = seconds.truncate();
  final h = total ~/ 3600;
  final m = (total % 3600) ~/ 60;
  final s = total % 60;
  if (h > 0) {
    return '$h:${m.toString().padLeft(2, '0')}:${s.toString().padLeft(2, '0')}';
  }
  return '$m:${s.toString().padLeft(2, '0')}';
}
