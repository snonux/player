# Direct Android podcast downloads (yh3)

Implementation proposal, before source edits: extend the reachable **On this
device → Podcasts on this device → podcast → cached episodes** surface. Each
episode shows Download, then Pending / Downloading with Cancel, then Available
on this device with Play and Remove download. Failure shows Retry download;
cancellation returns to Download. Restart turns unfinished transfers into an
explicit interrupted failure, retaining a retry action. Removal stops playback
of that file, removes device bytes and availability only, then offers Download
again. It never unsubscribes or deletes history.

Store device-copy records in the existing podcast database, independent of
scope, with generated opaque filenames in an app-managed podcast directory.
Feed/episode keys identify copies, including identity alias rewrites; listening
registers remain scoped. Keep copies separate from authenticated server-media
downloads: those require server media IDs and account checks and fetch lyric
snapshots. Reuse their streamed/backpressure/partial-file/atomic-rename pattern
and the existing playback coordinator, rather than those authentication hooks.

Use a fresh dart:io HTTP client for each publisher transfer, with no Player
headers, cookie jar, interceptors or logging. Follow bounded HTTP(S) redirects
manually, validate each URL, reject non-success/truncated/empty/non-media
responses, time out stalled connections and chunks, and expose only fixed
errors that cannot contain paid enclosure query strings. Only rename and mark
available after the whole response is flushed. Cancellation closes transport
and discards partial bytes; retry starts cleanly.

Play via a distinct local podcast request in the existing audio screen and
source coordinator. It has no media ID or server credentials and never invokes
music cache/lyrics/membership APIs. Position/completion callbacks write the
existing scoped podcast registers. Pre-play/end reconciliation and full episode
browsing belong to subsequent 2i3/1i3 work.

Implementation-stage checks use local HTTP fixtures and real SQLite/files for
redirects, interruption, retry, restart, scoped history and cleanup. Installed
Android offline playback and Chrome/Firefox shared behavior remain explicitly
deferred to integrated acceptance (5i3); unit tests are not platform proof.

Implementation-stage result: device copy records migrate podcast database v2
to v3. Copies use opaque IDs, so catalog aliases/merges can rewrite their keys
while an active HTTP transfer completes against its original ID. Insertion
shares the catalog transaction boundary. Playback callbacks capture their scope
and resolve catalog/account aliases at write time. Removing files stops the
shared source first and never deletes any listening-state row. The reusable
`PodcastDownloadControls` widget is installed in the cached episode list for
the later folder/all-episodes work to reuse.

Checks (from `player-android`): `flutter test --no-pub
test/services/podcast_downloads_test.dart
test/services/podcast_repository_test.dart
test/services/playback_source_resolver_test.dart
test/services/playback_session_coordinator_test.dart` passed 29 tests. The
reachable cached-list widget test passed 1 test for pending/cancel/retry,
available/play/remove labels and Back. HTTP tests use real loopback publisher
responses and SQLite/files, including credential-free redirects with a
Set-Cookie response, HTTP errors/HTML/truncation, interruption/restart,
scope-isolated history, missing/corrupt bytes, repeat download, active catalog
merge and stop-before-delete with final progress save. No server API dependency
is present. Native decoding/background playback, two-client behavior and
Chrome/Firefox shared journeys remain unverified until integrated acceptance.

Scope boundary for follow-up: server `resolved` aliases currently live in
account-scoped alias tables (zh3), while the device catalog remains shared.
This slice preserves copies under that device catalog identity, rekeys them
with publisher-observed catalog merges, and resolves account aliases for
listening registers. It does not globally apply one account's server aliases
to other accounts' shared catalog. The later account-aware episode listing
must retain that distinction when joining sync-only catalog keys to device
copies. Full end-event atomic position/completion clocks and bounded pre-play
sync remain 2i3's lifecycle work.

Legacy boundary: the pre-existing server-backed `PodcastEpisodesScreen` still
calls its server episode-download API. The standalone surface added here does
not call it. Task 1i3 replaces the native shared-server podcast entry with the
standalone library; until that is integrated, the global Android no-server-
download criterion is not yet satisfied. Server/web caching APIs remain intact.

Final scoped compile check: `flutter analyze --no-pub lib
test/services/podcast_downloads_test.dart
test/screens/podcast_download_controls_test.dart` passed with no issues;
`git diff --check` passed. No installed runtime, version, release or build was
changed for these implementation-stage checks.
