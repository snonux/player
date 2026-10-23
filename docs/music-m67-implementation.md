# Translated music UI (rj3 / M67)

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
