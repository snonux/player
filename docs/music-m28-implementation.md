Stage: IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION.
One bounded expert review found no blockers; actual platform acceptance remains pending.

# M28: Save queue as playlist (1l3)

Pre-code U01 contract: extend the existing Now Playing footer with **Save as
playlist** beside Clear/Undo. Disable for an empty, loading or unrestored queue.
The existing queue remains playing throughout. A short existing-style dialog
contains an autofocus Name field, captured song count, “Private playlist · Keeps
this order and repeated songs”, Cancel and Save playlist. Snapshot the complete
queue when opening, rather than whichever page/current song is visible.

Web: use the existing playlist modal, theme tokens and focus restoration; Tab,
Enter and Escape remain native modal actions. No shortcut is allocated. On
success retain a short confirmation and **Open playlist** link; opening it uses
normal in-app navigation, preserves playback and closes the queue. Narrow and
light/dark layouts use the same wrapping footer and scrollable modal.
Android: labeled 48px touch action opens a scrollable native AlertDialog; Name,
private/count explanation and Cancel/Save. Back dismisses. Success supplies
Open playlist, navigates through the normal playlist route and closes the
sheet. Restore focus through the platform dialog lifecycle.

States: initial/empty (disabled), naming, saving (one request, no duplicate
submit), error (retain name and snapshot), success (Open playlist/Close).
Snapshot/account guards prevent saving an old queue under a switched account.
Cancellation during a request dismisses the form; an already accepted request
may still create the playlist. A missing/inaccessible/nonmusic song rejects the
entire save, explicitly asking to remove unavailable songs and reopen the form;
never silently save a partial listening session. Later unavailable songs use
existing playlist read filtering, preserving order of remaining occurrences.

Server: extend POST /playlists with optional media_ids, 1–10000 IDs. Validate
metadata, private visibility and all songs before calling existing transactional
ImportPlaylist(create=true), which inserts the private document and all
occurrences together. Every occurrence has its own playlist identity; saved
queue, playback index, position and repeat/shuffle are untouched.

Implementation-first stage: focused tests/review only; full suites/builds,
Chrome/Firefox actual journeys, native emulator, light/dark/narrow captures,
keyboard/touch/focus, unavailable/deleted tracks, account/restart/offline checks
remain integrated-verification requirements. Prerequisites 3k3/ck3/nj3/oj3 are
completed; no release, version bump or acceptance is claimed.

Focused implementation checks (HEAD 52e90af plus concurrent reviewed artwork
slice 6e71345; no release/build):

- `go test ./internal/service ./internal/api -run 'TestCreateFromQueueAtomicPrivateOccurrences|TestPlaylistAPIValidationOwnerAndSavedState' -count=1` passed. Real SQLite covers private ownership, repeated order, rejection/no partial document, transaction rollback and later deleted songs.
- `go vet ./internal/service ./internal/api` passed.
- `flutter analyze --no-pub lib/widgets/queue_playlist_dialog.dart lib/widgets/music_queue_sheet.dart lib/api/player_api_client.dart test/widgets/queue_playlist_dialog_test.dart` passed with zero issues.
- `flutter test --no-pub test/widgets/queue_playlist_dialog_test.dart test/widgets/music_queue_sheet_test.dart` passed 7 tests: retained input/error/retry, order/duplicates, late completion after Cancel, stale account rejection, and existing queue sheet behavior.
- `node --check web/js/queue-playlist.js` and `node --check web/js/queue-panel.js` passed; `git diff --check` passed. The scoped stacking selector puts the naming modal above the narrow queue; class/fullscreen parent clean up on Close, Escape and normal modal dismissal.

Actual Chrome/Firefox/native acceptance, full suites/builds and screenshot/focus
journeys are deferred to integrated verification. No runtime or platform proof
was produced in this implementation slice. Root owns fresh review, task stage,
service worker registration, commit and later integrated acceptance.
