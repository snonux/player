import 'playback_request.dart';

enum MusicRepeat { off, all, one }

enum QueueInsertion { now, next, append }

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
      required this.status,
      this.repeat = MusicRepeat.off,
      this.shuffle = false,
      this.shuffleEligible = 0,
      this.upNextIds = const {},
      this.restored = false,
      this.position = 0,
      this.restoreFailed = false});
  final List<MusicQueueEntry> entries;
  final int index;
  final String label, status;
  final bool active, busy, canUndo;
  final bool restored, restoreFailed;
  final double position;
  final Set<int> upNextIds;
  final MusicRepeat repeat;
  final bool shuffle;
  final int shuffleEligible;
}
