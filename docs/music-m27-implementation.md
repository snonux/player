# M27 edit playlist songs

Design checkpoint: baseline bfa8ff1, shared version 0.8.2 / 0.8.2+22.
Extends M26 playlists and the U01 interaction design; no new navigation lens.

## Scope and saved-state contract

Add selected catalog songs to the current playlist, remove individual saved
occurrences, move occurrences up/down, and find songs within long playlists.
Existing Add music album insertion remains available. Repeated selections
create distinct occurrences; show “Repeated songs are added again” before
saving. Multi-playlist destinations, create while adding, duplicate skip choices,
containing-list lookup, drag gestures, sharing and import/export remain later
scope. Playlist edits never delete files or alter the already-playing queue.

Use owner-scoped entry IDs for remove/move, never media ID or filtered row index.
Resolve selected additions through the permission-filtered music catalog.
Reject invalid/missing/foreign entry IDs and nonmusic/inaccessible additions
atomically. Moves operate against the adjacent accessible occurrence in the
full playlist, even while search hides other rows. Hidden ACL-revoked entries
stay saved: mutations never replace membership with the visible subset.
Remove offers Undo, reinserting the song before its saved next accessible
occurrence (or appending if that anchor disappeared); undo creates a new
occurrence ID. Concurrent operations preserve unrelated saved entries.

## Web annotated layout and transitions

    [Back to playlists] Artwork / Name / comment / totals
    [Play playlist] [Add music] [Add songs] [Edit playlist] [Delete playlist]
    Find songs [                         ] [Clear]
    Showing N of M songs
    1  Song title / Artist · Album   duration   [Song actions]

Find filters title, artist, album and filename locally without changing saved
order/totals or the playback snapshot. Rows retain their original occurrence
index; Enter plays that exact occurrence in the full playlist. Search input
keeps native typing and focus; clearing restores all rows. No matches shows
“No matching songs. Clear the search to see all songs.”

Song actions uses existing media action controls/popover: Move up, Move down,
Remove from playlist. First/last occurrence disables the impossible move.
Existing a opens actions, j/k/h/l/arrows/Enter/p/Space/r and / retain current
behavior. No new global shortcut. Tab/Enter and visible buttons give a full
keyboard and pointer path. Successful moves preserve selected occurrence and
focus; remove selects the next visible row, otherwise previous/header. An
inline status with Undo remains reachable by Tab; errors preserve the row and
show readable feedback. Undo restores a song occurrence, never a deleted file.

Add songs opens existing playlist modal with Find songs, a scrollable list
of labeled native checkboxes (title, artist, album), selected count, Cancel
and primary Add selected songs. Selection survives filtering. Empty catalog,
no matches, loading, Retry and inline save errors are explicit. Save disabled
when no selection or pending; successful save closes, updates totals and
announces success. Late callbacks cannot affect a closed/replaced dialog.
Cancel restores the opener; Escape follows the existing global dialog contract
and returns to the selected row for vi navigation. Values survive a failed or
offline save.

## Native annotated layout and transitions

Existing playlist Scaffold/detail retains artwork/header and actions; add
labeled Add songs button and Find songs TextField with Clear icon. A small
“Showing N of M songs” label precedes rows. Each row has title/artist and
48dp Song actions overflow. Menu: Move up, Move down, Remove from playlist.
No gesture is required; row taps keep occurrence-safe full-queue playback.
Move retains scroll position; remove shows a native Undo SnackBar. Back closes
menus/dialogs before leaving detail; find typing uses the native keyboard.

Add songs uses a native dialog with search TextField, bounded scrollable
CheckboxListTiles, selected count, duplicate explanation, Cancel and Add
selected songs. Loading/empty/no matches/Retry/offline save failure are
visible. Cancel and Back do not add selections. Account/view generation guards
follow M26 and prevent old responses entering a new account/screen.

## Visual/state acceptance

Use existing theme tokens, typography, .media-row/playlist-actions and Material
controls. Wrap action toolbar on narrow screens; truncate row fields with full
accessible names, scroll long lists inside modal, preserve 48dp native targets.
Capture/review desktop+narrow light/dark originals on both browsers and native.
Initial fetch and mutation errors remain explicit; missing/foreign playlists
retain existing error/retry behavior. No offline edit success is claimed.

## Verification plan

Targeted SQLite/service/API tests: selected additions with duplicates; entry
identity remove/move; missing/foreign/invalid/inaccessible IDs; atomic rollback;
ACL-hidden membership retention; filtered duplicate changes; restart persistence.
Web/native focused checks: filtering maps to original occurrences, checkbox
selection survives search, accessible controls and retained failed forms.

Extend component-local M26 Playwright/native deterministic runners. Installed
Google Chrome and Mozilla Firefox: actual keyboard + pointer add selections,
find/clear, move/remove/Undo duplicates in a long list, refresh persistence,
failed offline edit, visible focus, themes/narrow screenshots and authoritative
saved state. Native API34: touch/Back dialog/menu controls, same long-list
operations, force-stop/relaunch persistence and authoritative saved state.
Play a filtered later duplicate and verify full original queue occurrence plus
actual media/audio output where applicable. Isolated fixture/media/database,
credentials, builds and evidence stay outside git. Root coordinates runtime
slots, full gates, builds, reviews and final acceptance; no release/version bump.

Root approved this design before feature coding on 2026-10-21.

## Implementation and targeted verification

Owner/session-gated `POST .../playlists/{playlist_id}/songs` accepts
add/remove/up/down. Service checks additions and selected occurrence identities
against current ACLs. Repository transactions read latest complete membership,
delete only the selected occurrence and reassign positions without losing
hidden entries. Web Find preserves original full sequence indices; native
retains M26 sequenceIndex handling. Totals and artwork update after saved edits.

Targeted commands actually passed on 2026-10-21:

- `GOMAXPROCS=2 go test -p 1 ./internal/service ./internal/repository
  ./internal/api -run TestPlaylist -count=1`: real SQLite duplicate identity,
  removal/Undo insertion anchor, moving across a hidden saved occurrence,
  missing/foreign/hidden IDs, mixed invalid/nonmusic atomic rejection,
  FK rollback, missing swap and database reopen.
- `flutter test --no-pub test/screens/music_playlists_screen_test.dart
  --reporter expanded`: 5 PASS. Filtered later-duplicate mutation, failed edit
  retention, selection across search and offline retained picker are covered.
  Scoped `flutter analyze --no-pub` of client/screen/test: no issues.
- JS API/playlist syntax checks and `git diff --check`: PASS.

These targeted results were tool terminal output, not separately retained log
files. Initial compile needed mockPingStore forwarding for the new repository
method. Initial widget drivers needed an adequate viewport and checkbox-scoped
finders to avoid a SnackBar overlap and input/title ambiguity; final tests pass.

## Final builds, gates and reviews

Root full gates: Flutter analysis no issues; all 1373 Flutter tests PASS
(159.06s); Go build, vet and full FFmpeg race/coverage suite PASS (213.21s);
all web unit/service-worker checks PASS. Errcheck reports 256 existing findings,
zero new findings; it is not clean. External logs and results live under
`/home/paul/tmp/nl3-evidence/`.

Both fresh review rounds are complete (maximum two, no third). Round 1 found
a blocking web pending-save race: checkbox changes could re-enable Submit.
The repair adds a local saving guard to submit/checkbox handlers and disables
existing/newly filtered choices until completion. Root self-review, post-fix
web checks and narrow final review confirmed it resolved, with no new blocker.
The actual delayed-response acceptance below verifies the final behavior.
Go/native product code remained unchanged after their full gates.

Server final: version 0.8.2, build
`bfa8ff1344989ab8cfe39aab3f76dc9d272233fd-dirty`.
Native final APK: version 0.8.2/code4022 (`pubspec 0.8.2+22`), SHA256
`0b5b91435eecf5bcfe7b50b93c0302d9bbe42f6f53f3fae5eceb16793b45b8f2`.
Both final builds passed; no product change was made during actual acceptance.

## Actual browser acceptance

Installed Google Chrome 144.0.7559.132 and Mozilla Firefox 156.0 each passed
all three M27 scenarios against the final isolated server on port 18808.
`web-acceptance-summary.json` maps retained runs: initial 3 PASS / 3 FAIL,
53.2s; corrected boundary-only 2 PASS,10.2s; final Firefox pending-only 1 PASS,
5.7s. Accepted earlier scenarios were retained rather than replayed.

Both browsers exercised selected additions across search, 47 distinct saved
occurrences, Find showing 16 matching Harbour rows, playing the later filtered
duplicate at original full queue index46, original Harbour output, keyboard
and pointer move/remove/Undo, restored occurrence focus, offline failed move,
refresh persistence, revoked-permission failed add with retained selections and
successful retry, owner isolation, Cancel opener focus and Escape selected-row
focus. Source WAVs and authoritative saved/moved/removed/restored JSON live in
`run/{chrome,firefox}/`.

Original `m27-songs-{dark,light}-{1366,390}.png` screenshots intentionally retain
the tested offline error feedback. They show wrapped actions, Find,47-song
9:24 totals and visible selection without horizontal overflow. Root reviewed
Chrome dark/narrow and Firefox light/narrow originals.

Initial boundary failures expected Escape to restore Add songs; the existing
component contract returns to the selected row for vi navigation. Corrected
runners verify that and separately verify Cancel restores the opener. Firefox
pending regression initially hit BiDi route.fetch timeout before assertions;
a further transport attempt exposed missing intercepted postData and raced
failed-test cleanup. An exploratory body-fallback PASS is preserved but excluded
from acceptance. Final Firefox proof uses controlled window.fetch response
promise delay around untouched original arguments/body/auth/network. This is
fault injection, not an unmodified transport. While the real saved response is
held, Submit and newly filtered choices stay disabled and authoritative API
state proves exactly one two-song insertion. Chrome passed the held-real-response
route case. All original failed logs/screenshots remain outside git.

Actual commands from `player-server/test/e2e-web/`:

```sh
PLAYER_MUSIC_FIXTURE=/home/paul/tmp/nl3-fixture PLAYER_E2E_PULSE_SINK=player_nl3 \
  npx playwright test -c playwright.music.config.ts music-playlist-songs --workers=1
# Retained accepted cases; rerun only unfinished boundary cases:
# append --grep 'selected add offline'
# then only unfinished Firefox delayed-response case:
# append --project=firefox --grep 'pending selected add'
```

## Actual native acceptance

Native Android14/API34 x86_64 on Player_FDroid_Test_API34,1080×2220,density440,
ran the exact final APK. `native-final.log` ends terminal0,277.99s with all
remaining phases PASS; native-terminal.json records its exact command.

Actual touch controls selected Harbour then First Light across search and
saved them in catalog order, preserving duplicate occurrence IDs in a47-song
playlist. Verified owned-server cutoff plus adb-reverse removal produced a real
offline add failure with retained First Light query and two checked selections.
The same binary restarted on the same SQLite DB; unchanged membership was
verified before retry succeeded. Touch Find, full-order up movement of later
Harbour, removal of that occurrence only, Undo and Back passed; authoritative
counts47→47→46→47 and47 distinct restored IDs are recorded in evidence.jsonl.

A filtered actual later row tap selected full original queue occurrence46.
The native queue was scrolled to that occurrence for assertion (no queue jump
substitute), and QEMU output proves Harbour659.3Hz for2.98s. Force-stop/relaunch
required saved-server sign-in; all47 entry IDs/order and564-second duration
persisted, foreign-admin mutation returned404 and Back returned to Library.
Root reviewed offline picker, light/dark Add/Find, original source player and
restarted screenshots plus saved JSON/audio. m27-add-light intentionally keeps
the earlier offline feedback visible, with retained selections and usable retry.

One bootstrap attempt ended before installation/journey because Wi-Fi had not
yet connected; actual CONNECTED+INTERNET was then verified. The first journey
stopped at the empty Find-input label wait: Android UIAutomator omits empty
EditText hints, although the original screenshot shows the correct visible
field/keyboard. The runner now identifies the actual EditText. It resumed the
existing45-entry playlist and held signed-in device, avoiding reseeding or
replaying accepted offline/edit phases. These original failure artifacts remain
in native-first-attempt-artifacts and separate external logs.

Actual native commands from `player-android/`:

```sh
python3 -u test/e2e-live/music_playlist_songs_e2e.py /home/paul/tmp/nl3-fixture \
  --apk /home/paul/tmp/nl3-evidence/app-x86_64-release.apk \
  --emulator-wav /home/paul/tmp/nl3-evidence/emulator-output.wav --hold-device
# Existing45-entry playlist resumed after driver correction:
python3 -u test/e2e-live/music_playlist_songs_e2e.py /home/paul/tmp/nl3-fixture \
  --emulator-wav /home/paul/tmp/nl3-evidence/emulator-output.wav --hold-device \
  --resume-device --resume-playlist pl_b40698d2c742b3b8b7a8ce9b62948be2
```

## Acceptance, cleanup and limits

Root accepted all required actual Chrome, Firefox and native API34 journeys,
original screenshots, authoritative state and source audio. After holding the
owned runtimes idle for review, authorized stop-only cleanup fingerprint-checked
and stopped server1693636/start42512679 and QEMU1692099/start42485082, removed
its reverse mapping and unloaded only Pulse module536870916/player_nl3.
`cleanup-stop-only.json` records the checks: health unreachable, no18808 listener
(SO_REUSEADDR bind succeeds; initial plain bind encountered TIME_WAIT), adb empty,
and owned Pulse module absent. Fixture DB/media/accounts, APK, originals, logs
and WAV remain preserved outside git. No release/push/deploy/version bump occurred.
Root owns final staging, local commit and task completion.

Verification limits: native proof is the API34 emulator, not physical hardware
or operated TalkBack. Browser narrow controls used real keyboard/pointer inputs;
physical browser touch hardware was not driven. Multi-playlist destinations,
duplicate-skip choices, containing lists, drag gestures, sharing, import/export
and offline playlist storage remain outside M27. All M27-required platforms are
verified; no required feature/platform gap remains.
