# Podcast reconciliation implementation (zh3)

Implementation awaits integrated verification. Chrome, Firefox and two native
Android clients have not been verified for this slice. The complete behavior is
specified in [the shared contract](../../docs/podcast-sync-contract.md).

`PodcastSyncStore` persists independent subscription, position and completion
registers per account, together with exact edit IDs, acknowledgements, rejection
records, aliases and the pull cursor. Account aliases never rewrite the shared
publisher catalog or another account's state. Interrupted epoch recovery keeps
its markers across restart and repushes missing registers with their original
clocks. Synchronization transfers state only; publisher refresh and audio
downloads are separate operations.

`PodcastSync.sync` sends bounded edit pages and pulls to the end. A scoped
`beforePlayback` attempt waits at most three seconds, including capability
lookup, then leaves playback to its caller. Late responses update storage only.
Playback and completion integration belongs to tasks `1i3` and `2i3`.

`globalSyncCoordinatorProvider` starts the quiet coordinator at app startup and
retries on edits, connectivity and foreground changes. Each run captures one
credential generation for podcast state, existing offline media playback state
and streamed progress. Only a fully successful run advances the account's saved
success time. Authentication failures block automatic retry until sign-in;
pending data remains durable. Unsupported podcast servers still run existing
media/progress synchronization. Deleted or forbidden media copies retain local
edits, are excluded from automatic retries and are retried by explicit Sync now.

`GlobalSyncState` exposes activity, pending count, error kind, never-synced state
and the 48-hour stale threshold. Task `4i3` supplies the global action and visible
feedback; this service does not display dialogs or playback notifications.

Focused checks:

```sh
flutter test --no-pub test/services/podcast_sync_test.dart
flutter test --no-pub test/services/offline_playback_sync_test.dart test/services/progress_queue_timestamp_test.dart
```

These checks do not replace the integrated full suites, builds or actual
platform acceptance required by task `5i3`.

Stage: **IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION**. Both
bounded expert rounds completed. Alias collision replay now checks pending
work on either row; the canonical winner keeps its value and clock and gets a
fresh edit ID when required. Eight focused reconciliation tests and affected
analysis pass, including a real transaction rollback on account change. The
earlier nine offline/progress regression tests passed. Platform journeys, full
suites and final builds remain deferred until backlog implementation is done.
