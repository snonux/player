Stage: IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION.
One bounded expert review found no blockers; platform acceptance remains pending.

# M19 shuffle albums implementation

Stage: implementation; integrated acceptance pending. Predecessors 7k3/nj3/oj3/zk3 provide canonical compilation, disc/track ordering, playback and queue infrastructure.

Interaction follows U01 (`music-interaction-design.md`). Now Playing toolbar retains Shuffle toggle and Repeat, adds named secondary Shuffle tracks / Shuffle albums choices (Android: Shuffle options menu). The toggle states Off, Tracks or Albums; Reshuffle remaining uses that mode. Web native buttons support Tab/Enter, existing modal j/k/Enter and Escape focus restoration; existing r library shuffle is unchanged. Android uses labeled 48dp controls in its existing wrapping toolbar and system Back sheet dismissal. Existing typography, theme and narrow wrap apply without new styling.

Transitions: choose Albums → Loading album order → upcoming album blocks shuffled → Undo available; current occurrence, source, position and played prefix stay fixed, as does Play next FIFO. Remaining tracks of the active album follow the current/pinned segment in canonical order; other album blocks randomize. Failed/offline catalog request keeps queue and prior mode, with retry via Shuffle albums. Empty/short queues disable shuffle. Choosing Tracks switches mode; toggling Off restores canonical remaining queue order. Manual insert/move behavior remains explicit queue editing; album grouping is applied when Shuffle albums or Reshuffle remaining is chosen.

Server POST /music/queue-albums bounds input to 10,000 positive safe integer IDs and 200,000 bytes, then maps requested IDs only to canonical caller-visible MusicAlbums identity and rank; compilation/MBID/folder semantics and unknown disc/track ordering exactly reuse catalog rules. Entries outside catalog remain individual occurrence blocks. Duplicate occurrences retain stable rank ties. Portable sessions and local recovery carry additive shuffle_mode, defaulting older sessions to tracks. Repeat traverses the resulting saved queue order.

Deferred: full suites/builds, actual installed Chrome/Firefox keyboard/pointer/source checks, native Android touch/Back/source checks, light/dark/narrow visual review, restart/offline/ACL platform acceptance. Unit tests are coherence evidence, not platform proof. No release/version/push/deploy.


Focused verification commands:

- `node player-server/web/js/tests/queue-shuffle.test.js`, `queue-storage.test.js`, `queue-sync.test.js`: pass (canonical album continuation, pins, duplicates, unknown singletons, restore and mode validation).
- `go test ./internal/api ./internal/service -run 'TestMusicAlbumsRealPermissions|TestSavedQueue' -count=1` from server: first endpoint ACL/request checks passed; later service mode checks pass, API recompilation temporarily blocked by concurrent artwork GetMusicArtwork mock interface handoff.
- `flutter test --no-pub test/services/music_shuffle_test.dart test/services/saved_queue_sync_test.dart test/widgets/music_queue_sheet_test.dart`: 13 pass; includes album lookup failure with unchanged queue/source, mode snapshot/portable roundtrip, clear/Undo and named options sheet interaction.
- Additional `music_queue_recovery_test.dart` run had an existing async play-timing failure (`paused handler recovery…`, expected plays 1, got 0); retained as integrated verification gap, no baseline retries. Initial new loader local-function declaration compile issue was fixed. Initial toolbar density widget failures were fixed using native progressive disclosure and a shorter hint.
- Scoped Dart analysis and `git diff --check`; full regression and actual platform acceptance remain deferred.
