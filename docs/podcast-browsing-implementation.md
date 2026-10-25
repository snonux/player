# Standalone Android podcast browsing (1i3)

Proposal before source edits: make the existing standalone subscription library
the native Podcasts destination, with one folder per subscription and a virtual
All podcast episodes list. Both episode lists default to Uncompleted, include
started episodes, offer Completed and All, and page newest first with stable
identity tie breakers. The combined list names each podcast.

Join the selected scope's subscriptions and listening registers to the shared
device catalog through that account's aliases. Preserve device catalog keys for
download/playback; use resolved scope keys for manual completion. Never rewrite
another account's catalog or copies from a server alias. Hydrate missing catalog
metadata from the subscription's publisher URL when Refresh is selected.

Reuse PodcastDownloads, PodcastDownloadControls, the current audio player and
scopedPodcastSyncStore. Completion edits retain position and queue durable edits;
file deletion retains subscriptions/history. Reload after playback without
replacing the route or its scroll/filter/page state. Replace the legacy numeric
podcast episode screen with the standalone destination, removing its server
download action. Ordinary server media-set browsing remains ordinary media
browsing, including preexisting server copies.

Targeted SQLite/widget checks and analysis are implementation evidence only.
Full suites/builds, actual Chrome/Firefox and both Android client acceptance are
deferred by the implementation-first workflow. Task 2i3 owns bounded pre-play
sync and exact natural-end lifecycle; this slice does not claim those behaviors.

Implemented: native Podcasts and saved numeric podcast links now open device
subscriptions. No reachable podcast screen calls server list/toggle/download
APIs. Ordinary home/media-set views deliberately retain server media browsing;
existing server/offline-media copies and their history are not migrated/deleted.
The old server SubscribeDialog remains an unused compatibility helper, rather
than the subscription entry used by the new native screens.

The repository builds account alias/state lookup maps in a SQLite transaction,
joins device episodes, collapses resolved duplicates (preferring a device copy),
then sorts and slices the virtual result. Each UI page requests 41 rows to show
40 and detect Next. This is local in-memory pagination over the catalog, not
SQL-limited catalog ingestion; very large catalog scalability remains unmeasured.
Provider results carry their captured scope. Retained rows are hidden across
account changes until matching rows load, and completion/playback actions use
the row's scope. Refresh-all captures one scope before querying/iterating.

Manual completion uses the existing `podcastSyncStoreProvider`; it changes only
completion, never position. Sync invalidation and return from playback reload
the visible query while retaining its filter, page and scroll controller. Play
from beginning uses the same download request with a zero start reader and
writes no history merely by starting. The exact natural-end/start reconciliation
rules remain 2i3 work, including the preexisting completed-position start rule.

Targeted checks from `player-android`:

- `flutter test --no-pub test/services/podcast_browsing_test.dart`: 3 passed
  (durable registers/restart, newest-first pages, alias/copy deduplication,
  account/unsubscribe isolation, sync-only publisher hydration without clock edits).
- `flutter test --no-pub test/screens/podcast_list_screen_test.dart
  test/screens/podcast_episodes_screen_test.dart
  test/screens/podcast_download_controls_test.dart`: 8 widgets passed across
  focused runs; covers folders/combined labels/default filters, completion and
  error retry, page reset, delayed account switch, return callback retaining
  location, and download/cancel/retry/delete controls. API client creation throws
  in browsing tests to catch unintended server dependencies.
- `flutter test --no-pub test/screens/podcast_episodes_screen_test.dart
  test/services/podcast_downloads_test.dart`: final affected run, 13 passed,
  including Play from beginning preserving registers and existing direct-copy
  restart/removal/HTTP isolation regressions. The return callback widget check
  simulates returning; it is not installed-player/native playback proof.
- `flutter analyze --no-pub lib test/services/podcast_browsing_test.dart
  test/services/podcast_downloads_test.dart
  test/screens/podcast_browsing_fakes.dart
  test/screens/podcast_list_screen_test.dart
  test/screens/podcast_episodes_screen_test.dart
  test/screens/podcast_download_controls_test.dart`: no issues. The final
  repository lookup-map refactor additionally passed its own targeted analyzer
  and the 3 SQLite tests. `git diff --check` passed.

Frozen implementation manifest for parent review (no staging/commits by worker):

- `docs/podcast-browsing-implementation.md`
- `player-android/README.md`
- `player-android/docs/standalone-podcasts.md` (parent documentation correction after review)
- `player-android/lib/app_routes.dart`
- `player-android/lib/providers/podcast_library_provider.dart`
- `player-android/lib/router.dart`
- `player-android/lib/screens/podcast_list_screen.dart`
- `player-android/lib/screens/podcast_episodes_screen.dart`
- `player-android/lib/screens/standalone_podcasts_screen.dart`
- `player-android/lib/services/podcast_downloads.dart`
- `player-android/lib/services/podcast_repository.dart`
- `player-android/lib/widgets/podcast_download_controls.dart`
- `player-android/test/screens/podcast_browsing_fakes.dart`
- `player-android/test/screens/podcast_download_controls_test.dart`
- `player-android/test/screens/podcast_episodes_screen_test.dart`
- `player-android/test/screens/podcast_list_screen_test.dart`
- `player-android/test/services/podcast_browsing_test.dart`
- `player-android/test/services/podcast_downloads_test.dart`

No full suites, APK/server builds, browser/emulator/device checks or live data
mutations ran. Chrome, Firefox, both installed Android clients, native decoding,
background/resume, and actual player-return journeys remain integrated acceptance
gaps. Existing emulator/server sessions, versions and untracked personal notes
were untouched. This slice is implemented and reviewed, awaiting integrated
verification; it does not close task 1i3.

R1 response: fixed the single blocker in duplicate availability selection.
Available audio now ranks above pending/downloading transfers, then failed,
missing and absent copy records. Equal ranks preserve the existing deterministic
catalog order. Ranking takes the maximum per device identity and selects the
actual winning device keys, so controls can still find Play/Resume and Delete.
No copy or history rows are rewritten by selection.

Narrow regression: `flutter test --no-pub
test/services/podcast_browsing_test.dart` passed 13 tests, including ten new
mixed-state/order cases. Available audio beats pending, downloading, failed,
missing and no-copy duplicates with the available catalog row encountered both
first and last. Assertions inspect the selected device keys and their actual
available copy record. `flutter analyze --no-pub
lib/services/podcast_repository.dart test/services/podcast_browsing_test.dart`
and `git diff --check` passed.

Self-review confirms ranking affects only which device catalog row represents
an already-collapsed scoped episode; scoped register matching, filters, ordering,
downloads and history remain unchanged. Refrozen for the sole permitted R2;
the narrow R1 delta is limited to this document,
`player-android/lib/services/podcast_repository.dart` and
`player-android/test/services/podcast_browsing_test.dart`. The full frozen
worker manifest was unchanged. R2 confirmed the blocker resolved with no new
blocking findings. Parent corrected the existing standalone guide to describe
the new routes and browsing controls; the final manifest contains 18 paths.
No broader checks or runtime actions ran.
