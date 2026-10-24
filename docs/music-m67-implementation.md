# Translated music UI (rj3 / M67)

Integrated acceptance passed on 2026-10-24 in installed Chrome, Firefox and
native Android. The implementation-stage notes below retain their historical
deferred checks; the final verification results and limits follow them.

Implementation design, based on U01 (`nj3`) and V01 (`oj3`). Initial supported
languages are English and German. Bundled, explicit catalogs own UI prose;
artist/song/playlist names, user comments, metadata and server diagnostics remain
verbatim. Missing translations and unsupported device languages use English.

Settings keeps its current appearance and gains a full-width **Language** select
with **Device language**, **English**, **Deutsch**. A native browser select keeps
arrow keys and typing; Android uses an expanded Material dropdown with wrapping
labels and normal Back. Existing modal focus restoration and music shortcuts stay
in force. No new shortcut is introduced. The choice applies on this device for
all accounts, without a server request or permission prompt.

Choosing a language updates mounted labels without restarting playback or losing
playlist edits. The document language and Android Material controls follow the
resolved locale. Device language is the default; an explicit language persists
across restarts. Unavailable storage gives a visible localized warning and a
usable session choice; malformed stored values fall back to Device language.
Android disables the selector during initial preference loading and while saving.
No translation download or additional network/loading state is required.

Music scope covers navigation, album/artist/song browsing and search, playlist
creation/editing, queue/player actions, selection counts, empty/loading/retry
states and accessible action names. Templates keep variable metadata separate;
song/album counts use singular/plural forms. German provides longer labels for
narrow-layout checks. Existing themes, spacing, artwork and control styles remain.
Controls wrap rather than clipping; song names retain their existing ellipsis and
full accessible labels.

Targeted catalog/provider/widget checks cover locale resolution, fallback,
interpolation, plurals, storage failure and mounted switching. Full suites,
server/APK builds and actual Chrome/Firefox/native Android journeys are deferred
to integrated verification per the user workflow update. Required acceptance:
switch languages, browse/play, create/edit playlists without losing input, restart,
exercise device-language/unknown/blocked-storage cases, inspect long strings and
screen-reader names in light/dark and narrow layouts. No platform acceptance is
claimed by implementation checks.

Implementation check results (2026-10-23): focused Flutter analysis of 22 changed
sources/test targets passed with no issues; four localization provider/widget
checks passed. These cover English fallback, region/device resolution, malformed
saved data, persistence through a provider restart, German singular/plural labels,
verbatim metadata and retained unsaved input during a mounted locale switch.

Dependency-free Node localization, music-layout and music-home checks passed.
Music-search checks passed with an import-time `window.addEventListener` stub
(the standalone test otherwise encounters the pre-existing listening-progress
browser-global import). Localization checks exercise catalog placeholder parity,
UI-template metadata isolation, native option text, persisted/device/blocked
storage choices and actual production collection controls plus artist headings
switching German back to English through mounted bindings. JavaScript syntax and
`git diff --check` passed. No claim of browser/emulator acceptance follows from
these targeted checks.

Specialist uncatalogued messages use the documented English fallback. Translation
catalogs are intentionally UI-only; arbitrary server error detail, metadata,
lyrics, playlist names and user comments retain their original text. Nonmusic
administration, podcasts and the entire Settings surface are outside this initial
music-label catalog. Full regression, final builds, light/dark/narrow visual
inspection, Chrome/Firefox keyboard journeys and native playback/playlist/restart
acceptance remain pending integrated verification.

First review fixes: physical `Delete` remains a canonical keyboard token in the
queue. The localization check now inserts real queue occurrences, invokes the
production German queue keyboard handler, observes removal, then invokes the real
Undo click handler and verifies the original occurrence IDs return. Search group
headings, layout option/column/direction labels and playlist create/edit dialog
titles/submit labels retain canonical UI keys in mounted bindings. Production
render helpers are checked in both language directions; named controls are
materialized from the production editor markup, retain draft values and selected
visibility, and reject content replacement in the unit DOM double. These remain
unit checks, separate from the deferred real-browser acceptance journeys.

Final narrow R2 confirmed both R1 blockers resolved with no new blocking defects; both review rounds used. Actual platform acceptance remains deferred.

Integrated verification (2026-10-24) reuses the original Chrome/Firefox live
playback/reload run (`cac94dc`) and native German/English playlist/save/restart,
counts, Back and light/dark evidence indexed by
`/home/paul/tmp/player-native-retry-evidence/m67-native-verification.json`.
The earlier full-Flutter failure and missing native audio backend were resolved
by the integrated runtime work; their historical limitations remain in that
original evidence file.

The extended `music-language.test.ts` uses the disposable fixture viewer account
to create a playlist with the keyboard, add songs through real controls and
verify 0/1/2 German totals and authoritative saved name/comment/two songs.
A second real Settings tab switches language while the create/edit dialogs stay
mounted, preserving drafts and focused input. Firefox requires bringing the
editor tab to the foreground before asserting native focus. A browser
`navigator.language = fr-FR` override verifies English fallback; a scoped
`Storage.setItem` exception verifies the visible German session-only warning.
These two controlled browser faults are explicitly distinguished from an OS
locale or physical storage failure. Chrome and installed Firefox passed.

Actual Android app-locale changes (`adb shell cmd locale set-app-locales
zone.foo.player_android --locales de-DE`, then `fr-FR`) translated the mounted
playlist editor and then resolved unsupported French to English, retaining the
exact unsaved name/comment. The original empty app-locale list and explicit
English choice were restored and the draft cancelled without saving. Fresh
semantic dumps and original screenshots are in
`/home/paul/tmp/rj3-acceptance/native/`; `draft-de.png` was visually inspected:
the long title wraps and the primary Create action fits. No preference files
were written. Actual native preference-storage failure was not forced; no
operated TalkBack claim is made.

Acceptance inspection found an untranslated core native collection selection
button/checkbox. The narrow correction keeps canonical collection kinds and
metadata unchanged, localizes Select/Selected albums/artists/playlists and uses
the existing Select-name semantic template. Three production catalog widget
regressions assert the German action and verbatim metadata in checkbox
semantics; all seven focused localization tests pass. These focused checks
preceded the final gates, APK build and corrected-control verification below.

The parent accepted the narrow correction as `e489731`: clean Flutter analysis
and all 1679 tests passed. The normal release x86_64 APK is 0.8.2/code4022,
SHA256 `270c7280639c52347c7c88f70fc9e29888c6f902c190e8469ea0b2e541228e6f`;
gate/build/install records are `/home/paul/tmp/player-language-gates/`.
An initial `--no-pub` build reused host-test plugin metadata and failed in the
generated registrant; the ordinary pub-enabled release build passed without
editing generated sources.

On this APK, actual native German collection selection and checkbox semantics
passed with verbatim `M67 Select Deutsch Play` metadata. Actual QEMU output
remained audible for 4.13 seconds at 1000 Hz/-6.02 dBFS through live OS German
then English changes; the same Loud media session stayed PLAYING and settled
Pause reported 4092 ms. The live session's sparse zero position anchors are
not used as a running clock. Fresh screenshot/semantic dump and output WAV,
analysis and before/after session states are under
`/home/paul/tmp/rj3-acceptance/native-final/`. This used owned fixture 18771;
the original 18766 queue was left untouched. The external focused harness is
`PYTHONPATH=player-android/test/e2e-live python3 -u
/home/paul/tmp/rj3-acceptance/native-audio.py`; failed attempts retain the
initial post-install locked-session assumption and corrected German menu-label
selector. Explicit unlock used ordinary native controls.

Final browser command, from `player-server/test/e2e-web/`:

```sh
PLAYER_MUSIC_FIXTURE=/home/paul/tmp/player-quality-fixture \
PLAYER_E2E_PULSE_SINK=player_e2e_null \
npx --no-install playwright test -c playwright.music.config.ts \
  tests/music-language.test.ts --workers=1
```

All four installed Chrome/Firefox cases passed (31.6 s), including captured
post-German PCM and advancing currentTime with unchanged source. The two
playlist cases passed again (15.8 s) after waiting for the closing-dialog
transition before screenshots. Narrow German light/dark originals were visually
reviewed: long titles/comments wrap, translated Back/Play controls and two-song
totals fit. Logs, browser versions, M67 result records, screenshots and WAVs are
collected in `/home/paul/tmp/rj3-acceptance/`; original fixture evidence remains
under `/home/paul/tmp/player-quality-evidence/`. Browser runtime backend was
`c7b6aea-dirty`, with current filesystem web assets; it is not described as a
HEAD backend build. Native original mounted-editor/restart proof and new OS
locale/audio proof supplement each other; no repeated full feature run is
claimed.

Final restoration verified exact equality of the original 18766/admin scoped
queue, including duplicate occurrence IDs, current index, canonical order and
4.69-second position, with
`/home/paul/tmp/player-native-loudness-evidence/original-queue.json`. The app is
paused in that account's Library, explicit English/System appearance; OS app
locale is its original empty list. Only the owned 18771 adb mapping and native
selection fixture playlist were removed; original 18766 mapping/runtimes remain.
No release, version bump, deployment, data clear or direct preference write was
performed. Native forced preference-storage failure remains unexercised because
no safe actual-device injection was introduced; browser session-only failure
handling is exercised explicitly. TalkBack was not operated; actual fresh
native semantics and browser accessible labels were inspected.
