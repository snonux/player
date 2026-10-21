import 'playback_request.dart';

/// Identifies an occurrence independently of its media identity.
class MusicQueueEntry {
  const MusicQueueEntry(this.id, this.request);
  final int id;
  final PlaybackRequest request;
}

/// In-memory album contents; independent of Sets sequencing and source ownership.
class MusicQueueState {
  const MusicQueueState(
      {required this.entries,
      required this.index,
      required this.label,
      required this.active,
      required this.busy,
      required this.canUndo,
      required this.status});
  final List<MusicQueueEntry> entries;
  final int index;
  final String label, status;
  final bool active, busy, canUndo;
}
