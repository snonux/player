# Music M02 album catalog and detail

Task `yj3`; interaction contract recorded before feature code, approved by the
parent against U01 (`docs/music-interaction-design.md`).

Music → Albums adds cover cards to the existing library shell. Enter/tap opens
an album with artwork, title, linked album artist, known year, song count and
duration total, followed by Play and ordered song rows. Disc/track numbers,
verbatim artist and duration stay visible. Missing numbers sort last with
filename/path/id ties. Untagged albums use their folder, labeled From folder.
Back restores the prior album card. Open album on an existing song provides
the second entry point. The artist link opens Albums by [artist], a scoped
album filter with Back to Albums; artist discography remains M03.

Play and row activation snapshot the ordered album audio, independent of
later browsing. Short music starts at zero; items longer than 20 minutes
resume. End stops. Existing Sets playback keeps M12 wrap/resume semantics.
No queue editing, future More actions or rich metadata controls are added.

Loading is explicit. Empty albums explain the Music lens with a Sets exit.
Request failures offer Retry; missing/forbidden albums say unavailable.
Offline requests retain a usable Back/Retry path. Long names wrap in detail
and have full accessible names on truncated cards/rows. Existing theme tokens,
card/row typography, native Material controls and 48dp tap targets apply.
Web uses existing selection j/k/h/l/arrows, Enter and Esc hierarchy; native
uses visible touch controls and Back. Light/dark/narrow checks and actual
Chrome/Firefox/native source playback are mandatory acceptance gates.

Catalog identity is MusicBrainz release ID when available, otherwise normalized
album artist and title; years and disc folders do not split releases. Missing
album tags use a set/folder fallback. ACL filtering precedes aggregates,
artwork selection and key lookup. Podcast and audiobook sources are excluded.
Keys can change on retag as U01 allows until A06a.

The final verification record follows the implementation notes below.

## Implemented contract and focused checks

Server albums aggregate existing media after permission-aware set and audio
reads; no schema tables or duplicated file identities. A hidden contribution
in the disposable Restricted set shares Night Transit's key, so admin totals
are four and viewer totals three. Actual SQLite API regression verifies grant,
revoke, list/detail/resolver aggregates, artwork identity and indistinguishable
missing/forbidden keys. Identity regression covers release UUID case, invalid
UUID fallback, whitespace/case, year/disc independence and set-scoped folders.

Web URLs follow U01: `music=albums`, `music=album&id=al_…`; the minimal artist
filter uses `martist`/`martist_name`. Sets parameters are excluded and ignored
while Music is active. Backspace and visible Back go up; Escape retains the
existing player/modal hierarchy. Song rows use a separate native Play button
and Info button in a group; no nested interactive role. Existing selection
and Enter follow the selected row after j/k/h/l movement.

Native collection mode extends the existing handler with `wrap: false`,
leaving Sets defaults intact. Manual boundary controls preserve the current
progress session. Natural last completion stops; replay creates a fresh
same-source progress session (a deliberate seek back is retained). An explicit
completed-collection flag prevents source-load failures from accidentally
creating target progress for an older source. Targeted tests exercise boundary
seek/save, natural end/replay/save/recompletion and failed-transition/native
Play without target-ID progress.

Focused logs in `/home/paul/tmp/yj3-evidence/`:

- `go-targeted.log`: `go test ./internal/service ./internal/api ./internal/probe
  -run 'TestMusicAlbums|TestAlbumIdentity|TestMusicReleaseID' -count=1` passed.
- `flutter-targeted-boundary.log` and `flutter-targeted-replay.log` retain
  intermediate 11-test passes; `flutter-targeted-final.log` covers the final
  boundary/replay/failure regressions and album screen tests.
- `flutter-analyze-initial.log` retains initial diagnostics; final output is
  `flutter-analyze-final.log`.
- `node-targeted.log` covers URL and keyboard Enter; URL coverage was extended
  again for U01 namespacing before the parent full Node gate.

Cross-platform runtime runners and runbooks are extended with M02 journeys.
The focused checks support the implementation; actual browser and Android
verification is recorded separately below.


Fresh review round one found a source-lifetime bug: browser history could
replace the active audio through `selectMedia` and leave an older album
snapshot behind. A regression using the production player/navigation modules
failed before repair (`node-history-before.log`, expected 999, got 103).
The snapshot now tracks the actual active media identity separately from its
desired pending index. Ordinary browsing retains the snapshot; a different
restored source discards it before Next/Previous. A second deferred long-song
resume regression failed before that distinction (`node-pending-before.log`,
expected 203, got 201) and passes after repair together with the history test
(`node-pending-after.log`). The actual browser runner includes Back to a Sets
source, then audible Sets Next, as a separate journey on both browsers.

`sw.js` advances to `kiss-v27` and precaches the new catalog module so installed
clients receive the new entry point and its module together.


## Final gates and actual runtime

Evidence is retained outside Git in `/home/paul/tmp/yj3-evidence/`. The parent
ran the full Go build, vet and race/coverage suite successfully (323.9 seconds
for race/coverage), Flutter analysis with zero issues and all 1,342 Flutter
tests successfully, and the complete Node suite successfully. The final web
suite after the scoped contrast repair passed in 2.8 seconds. Relevant logs
are `go-build-full.log`, `go-vet-full.log`, `go-test-full.log`,
`flutter-analyze-full.log`, `flutter-test-full.log`, and `web-unit-contrast.log`.
The final focused native guard checks passed 12 tests with clean analysis
(`flutter-targeted-final-guard.log`, `flutter-analyze-final-guard.log`).
Errcheck still reports 256 existing baseline diagnostics; comparison found
zero new diagnostics. This is a retained limitation, not a clean errcheck
claim (`errcheck-comparison.json`, `errcheck-full.log`).

All actual runtime used the isolated server on port 18797 and generated
fixture media and accounts under `/home/paul/tmp/yj3-fixture`. No service
doubles were configured. The server identifies as 0.8.2, Go 1.26.8, revision
`d290f37501d0214d32cf9a5b7387d7a8034c49a5-dirty`. The external release APK is
`/home/paul/tmp/yj3-evidence/app-x86_64-release.apk`, version 0.8.2/code 4022,
SHA-256 `20660a2367e74587c1286541ab0187b0bf88b5623bc4528e7f8e499c311f0994`.

Installed Google Chrome 144.0.7559.132 and Mozilla Firefox 156.0 passed all
six M02 cases in `browser-m02-attempt4.log` (two minutes, exit 0). Evidence in
`runtime-attempt4/{chrome,firefox}` includes decoded artwork, viewer three
versus admin four aggregate songs, hidden album denial, ordered disc rows,
linked artist, song entry, keyboard focus, light/dark and narrow detail,
actual denied stream 403 followed by Retry of the same target, independent
browsing during natural A/B/C playback, per-song completion, stop at end, and
browser Back restoring Sets playback before Sets Next. Captured tones match
440, 523.3 and 659.3 Hz for the album and 587.3 Hz for the restored Sets Next.
Secondary text contrast measured 7.41 in dark and 8.17 in light themes.

Earlier browser attempts remain available, including their screenshots and
error contexts. Attempt one failed on runner readiness and selection issues;
attempt two exposed a runner permission-status/cleanup error, which was
repaired and the isolated viewer grant restored. Attempt three passed four
cases and failed two permission journeys because the target had already
been downloaded and remained playable after grant revocation. The final
journey denies the target before its first load, and proves the actual stream
403 rather than expecting revocation to erase already decoded bytes.

Native verification used one owned cold boot of the API 34 x86_64 emulator
with the external APK above and QEMU WAV output. Active STREAM_MUSIC volume
was read back at 15, unmuted. The initial run retained successful natural
A/B/C playback, short-song start at zero despite saved progress, independent
catalog browsing, completion and stop at end, and fresh same-track progress
on native replay after natural completion. It exited 1 because two harness
steps needed repair: a placeholder-prefixed card label matcher and a manual
Next racing natural completion during UI dumps. Those failures and successful
scenarios remain in `native-m02-attempt1.log` and `native-attempt1-artifacts`.

Focused runs on the same boot passed with exit 0:
`native-m02-catalog-recovery.log` verifies catalog/detail, decoded artwork,
ACL totals and hidden-key denial, disc order, long names, linked artist,
song entry and Back, plus a real notification shade Next in album mode.
Notification Next produced 523.2 Hz audio and fresh viewer UID 2/media 4
progress at 3.292 seconds, unfinished. `native-m02-errors-recovery.log`
verifies actual source denial, readable target-specific Retry, restored
523.3 Hz playback and the correct paused target session, actual offline
request failure and recovery, and an explicitly settled dark long-album
capture. Runtime artifacts are in `runtime-native-recovery/android`; the
original mislabeled light long-album image is preserved alongside the valid
`m02-long-album-dark-settled.png`. Original-size images were inspected by the
implementer and parent for catalog/detail in both themes, artist filter,
ordered rows, long names and failure controls.

These checks establish the M02 album behavior. They do not claim new Cast,
EQ, artist discography, rich release metadata or editable queue functionality.


The final catalog audit exposed a pending-search navigation race: a queued
Sets search could fire after explicit Albums navigation and replace it.
The first focused capture attempt retained valid catalog/artist images but
failed before the offline checks; a second attempt also failed during search
setup. Both attempts are preserved in `browser-design-final*.log` and their
external result directories. Explicit album navigation now cancels only the
pending debounce; new intentional searches continue to work. The production
regression fails before the repair and passes after it (`node-search-before.log`,
`node-search-after.log`). The actual focused browser journey checks both
intentional search and typing immediately followed by Albums navigation.


The repaired search path passed the full Node suite in 2.77 seconds
(`web-unit-search-final.log`) and a fresh embedded-server build in 0.87 seconds
(`server-build-search-final.log`). The final server binary is
`/home/paul/tmp/yj3-evidence/player-search-final`. The owned fixture was
normally restarted against the same database/accounts as PID 1536498.
The final focused actual Chrome/Firefox run passed both cases with exit 0
(`browser-design-final3.log`, artifacts `runtime-design-final3`, external
Playwright results `browser-design-final3-test-results`). It includes ordinary
intentional search, pending typing followed immediately by Albums navigation,
light/dark catalog and linked artist captures, a real browser offline request
with Back/Retry and successful online Retry, and an empty catalog produced by
actual viewer grant revocation followed by verified grant restoration.
Catalog desktop/narrow and artist-filter original images were also inspected.
The two-case command was the same browser configuration above with
`--workers=1 --grep 'M02 library and song entry'` and an external `--output`.

The native emulator PID 1528712 was stopped after its successful focused
runs; `adb devices` then showed no connected device. The isolated server
and Pulse sink were retained only until final browser evidence review.
The owned fixture server PID 1536498 was then stopped, its disposable fixture
directory removed, and Pulse module 536870916 unloaded. `runtime-cleanup.json`
confirms both owned PIDs are gone, port 18797 is closed, adb has no device,
and the owned Pulse module is absent. Evidence and `fixture-final-state.json`
remain outside Git.
No release, version bump, push or deployment was performed.
