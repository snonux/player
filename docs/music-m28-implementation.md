Stage: ACCEPTED — focused Chrome, Firefox and native Android acceptance
completed 2026-10-27. One bounded expert implementation review found no
blockers. The next combined regression gate remains due at completed task 20.

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

Prerequisites 3k3/ck3/nj3/oj3 are completed. The focused actual-platform
journeys below supplement the implementation checks. No release, version bump,
push or deployment is included.

Focused implementation checks (HEAD 52e90af plus concurrent reviewed artwork
slice 6e71345; no release/build):

- `go test ./internal/service ./internal/api -run 'TestCreateFromQueueAtomicPrivateOccurrences|TestPlaylistAPIValidationOwnerAndSavedState' -count=1` passed. Real SQLite covers private ownership, repeated order, rejection/no partial document, transaction rollback and later deleted songs.
- `go vet ./internal/service ./internal/api` passed.
- `flutter analyze --no-pub lib/widgets/queue_playlist_dialog.dart lib/widgets/music_queue_sheet.dart lib/api/player_api_client.dart test/widgets/queue_playlist_dialog_test.dart` passed with zero issues.
- `flutter test --no-pub test/widgets/queue_playlist_dialog_test.dart test/widgets/music_queue_sheet_test.dart` passed 7 tests: retained input/error/retry, order/duplicates, late completion after Cancel, stale account rejection, and existing queue sheet behavior.
- `node --check web/js/queue-playlist.js` and `node --check web/js/queue-panel.js` passed; `git diff --check` passed. The scoped stacking selector puts the naming modal above the narrow queue; class/fullscreen parent clean up on Close, Escape and normal modal dismissal.

These were implementation-only checks. Actual platform acceptance follows;
the full combined regression remains on the user's ten-task cadence.

## Focused installed-platform acceptance, 2026-10-27

A disposable `music_fixture.py up` server with real 45-second tracks ran at
`http://127.0.0.1:18828` from
`/home/paul/tmp/codex/player-m28-18828` (`--no-doubles --shuffle-playback`).
The local server binary SHA-256 was
`3eb987fd2ca8d5aa8e0130750ff96c912840b580a3c4e13bc82e76576ef1d5a9`.
The Android 14/API34 read-only emulator installed the local universal APK
`0.8.2+22`, SHA-256
`5681c380851537873543bac30a26544fb140c1b562d0c49b12c10503c7f11dbf`.
No release or deployment was made. Screenshots, captured audio, server state
and path-only evidence are under `/home/paul/tmp/codex/player-m28-evidence/`.

Installed Google Chrome 144.0.7559.132 and Mozilla Firefox 156.0 each passed
the focused real-browser journey with one Playwright worker and
`GOMAXPROCS=2`:

```sh
cd player-server/test/e2e-web
PLAYER_MUSIC_FIXTURE=/home/paul/tmp/codex/player-m28-18828 \
PLAYER_M28_EVIDENCE=/home/paul/tmp/codex/player-m28-evidence/web \
PLAYER_E2E_PULSE_SINK=player_e2e_null GOMAXPROCS=2 \
  npx --no-install playwright test -c playwright.music.config.ts \
  tests/music-queue-playlist.test.ts --project=chrome --workers=1
# Repeat with --project=firefox.
```

Each browser opened a real three-occurrence queue with a repeated First Light,
used the queue's keyboard-accessible Save action, and verified name autofocus,
private status and count. A real admin deletion of Long Road Home while the
naming dialog was open made Save fail without creating a partial playlist;
the name and playing source remained. Restoring the song and pressing Save
created exactly the ordered `[First Light, Long Road Home, First Light]`
playlist. Measured 440 Hz audio continued during the save. Open playlist
navigated to its three rows without replacing the current player source.
Deleting Long Road Home later left both First Light occurrences in the saved
playlist view; the fixture restored the song in cleanup. Escape/focus and
390px light/dark dialog screenshots were captured.

Native Android used the Now Playing queue sheet and its
touch Save as playlist action on the same repeated order. Its naming dialog
displayed the three-song private snapshot in light and dark mode. Save kept
the media session PLAYING on Long Road Home and QEMU captured 10.9 seconds of
its tone during the request (`native-save-result.json/.wav`). The server stored
the exact three media IDs `[1, 3, 1]` and private visibility; Open playlist
showed the named three-song playlist. Deleting the middle song as fixture
admin reduced the reopened native playlist to two visible songs, both retained
server-side as the repeated First Light occurrence. Restore returned the
three-song playlist. A different ordinary account with Music viewer access
received 404 for this private playlist.

For native error recovery, removing `adb reverse` and disabling Wi-Fi/mobile
data before Save showed an inline connection error while retaining the typed
name and creating no playlist. Reconnecting and tapping Save created one
playlist. After a force stop and password sign-in, the original saved playlist
was still present with three songs. Only one emulator was active, and the
temporary playlists, account, server and emulator were removed after evidence
capture. Native Back returned to the playlist catalog; browser Escape closed
the naming dialog and restored focus to Save as playlist.
