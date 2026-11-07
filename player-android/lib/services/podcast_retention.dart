import 'podcast_downloads.dart';
import 'podcast_sync_store.dart';

/// Sweeps only the selected library, including while browsing offline.
class PodcastRetention {
  PodcastRetention(this.store, this.downloads);
  final PodcastSyncStore store;
  final PodcastDownloads downloads;
  Future<void> sweep(String scope,
      {required bool Function() current, int? nowMs}) async {
    if (!current()) return;
    final due =
        await store.retainCompleted(scope, nowMs: nowMs, current: current);
    for (final row in due) {
      if (!current()) return;
      await downloads.removeExpiredCopy(
          scope, row['feed_key'] as String, row['episode_key'] as String,
          current: current, nowMs: nowMs);
    }
  }
}
