# M05 rich release metadata

## Interaction and data contract (approved before feature code)

Task `0k3` extends U01 and the M01 tag pipeline. Existing media identity,
permissions, album grouping and legacy `date`/`year` wire values remain intact.
All added metadata is read only and stored in the existing music JSON.

Web: existing song Info modal → Title/Filename/basic tags → collapsed
**Release details** (Release date, Original date, Recording date, Version,
Release type, Label, Catalog number, Media) → collapsed **Additional music
details** (Mood, Grouping, Comment, named credits) → existing technical and
progress details/raw API. Native: existing **Music details** expansion retains
basic/filename/path rows and adds those two labeled groups inside its scroll.
Absent groups/rows are omitted. Song details from Albums uses this same surface;
no album consensus is inferred from conflicting song tags.

Dates preserve the tag's exact trimmed text and precision. Explicit release
aliases precede DATE/YEAR fallback; original and recording dates have no
fallback. Basic **Tagged date** appears only when different from Release date.
Year is omitted when it repeats a valid leading four-digit release year,
otherwise remains visible (for example Spring release plus Year 2024).
Labels and values wrap, remain selectable and preserve line breaks; markup is
text. Existing typography/theme tokens, modal focus, selection/vi shortcuts,
native touch/Back and playback controls apply. No shortcut is added.

Alias order (case insensitive): release_date/releasedate/date/year;
original_date/originaldate/originalyear; recording_date/recordingdate;
release_version/albumversion/album_version;
releasetype/release_type/musicbrainz_albumtype/musicbrainz album type;
label/publisher/organization; catalognumber/catalog_number/catalogue_number;
media/mediatype/media_type; mood; grouping/contentgroup; comment/description.
Container tags retain precedence over audio-stream tags as in M01.

Structured release types, labels, catalog numbers, media, moods and credit
names split only at actual repeated Vorbis comments or NUL boundaries.
Punctuation never splits a name. Additional credit roles are conductor,
lyricist, arranger, producer, director, engineer, mixer, remixer, djmixer and
the explicit `credit:<role>` namespace. Role keys are trimmed/lowercased;
human values retain punctuation/case/newlines. Composer and performer keep
their existing fields and browsing behavior. The existing bounded physical
tag reader remains the resource limit; no arbitrary new omission budgets.

Loading, errors, offline failures and permission denial use existing detail
states and close/Back paths. Empty metadata adds no blank sections. Rescan
refreshes and clears fields in place; failed probes preserve the prior snapshot.
New native downloads retain metadata in their existing JSON snapshot.
Downloads audio rows gain a labeled **Music details** action opening the same
grouped, scrollable/selectable metadata in a dialog labeled as a saved offline
snapshot. Old empty snapshots state that no embedded metadata was saved.
Back closes the dialog and retains the download list; no new route is added.

## Verification plan

Root owns full gates, builds and actual installed Chrome/Firefox/API34 runs.
Extend existing isolated V01 fixtures/runners with distinct partial dates,
versions/types/labels/catalog/media, repeated custom credits, Unicode, multiline
and hostile markup; check scan, removal, failed probe, stable identity/user
state, restart persistence, denied-library isolation and download snapshots.
Exercise real keyboard typing/selection/Info disclosure/scroll/close focus and
pointer paths in both browsers; native touch/disclosure/Back. Review light,
dark and narrow layouts and verify original source playback. Targeted unit
checks alone do not establish platform acceptance.

## Implemented slice and targeted checks

The parser adds typed release fields and descriptive credits to MusicMetadata;
existing repository JSON and scan/update boundaries carry them without schema
or service-interface changes. Bounded FLAC/Ogg comment extraction supplements
ffprobe for the scoped structured fields. Web disclosure summaries explicitly
participate in the modal Tab trap. Values and custom role labels wrap within
existing theme tables. The shared native widget renders both server and saved
snapshot metadata; Downloads exposes it without requesting a server API.
The app-shell cache revision is `kiss-v37` (release versions unchanged).

Targeted verification before orchestration: music parser real-container tests,
scanner refresh/clear/state tests, vanilla JS metadata mapping checks, seven
Flutter model/widget/Downloads checks (including a provider that throws on any
server API use), and four exact multiline/Unicode/capture-oracle Python checks.
Actual browser/emulator acceptance and full component gates remain pending.

## Reproducible feature commands (root orchestrator owns execution)

Use new disposable paths only. After building the actual source and APK:

```sh
python3 player-android/integration_test/support/music_fixture.py up /home/paul/tmp/0k3-player --dir /home/paul/tmp/0k3-fixture --port 18814 --no-doubles --release-metadata --evidence /home/paul/tmp/0k3-evidence/run
```

From `player-server/test/e2e-web`:

```sh
PLAYER_MUSIC_FIXTURE=/home/paul/tmp/0k3-fixture npx playwright test -c playwright.music.config.ts tests/music-release.test.ts --workers=1
```

From the repository root with the running QEMU WAV emulator and freshly built
APK (substitute the actual artifact paths):

```sh
python3 player-android/test/e2e-live/music_release_e2e.py /home/paul/tmp/0k3-fixture --apk /home/paul/tmp/0k3-evidence/player.apk --emulator-wav /home/paul/tmp/0k3-evidence/emulator-output.wav
```

The optional fixture adds rich FLAC (actual repeated comments), MP3, Opus and
M4A sources plus a restricted source. The rich track is 45 seconds at 613 Hz,
clearly distinct from baseline tracks. Only `music_release.py` retags this
known fixture track, guarded by the real disposable-fixture marker and confined
media root. `changed`, `cleared`, `broken`, `initial` states support real rescan
checks; initial restores repeat boundaries. Runners restart that same fixture,
so orchestration must refresh PID/start identity from its updated state.json.
Never pass a copied fixture view to guarded retag/restart operations.

Browser journeys assert full rows, native disclosure Tab/Enter/Space behavior,
text safety, selection focus, typing, pointer alternatives, light/dark/narrow,
original source output, stable saved state, removal, failed-probe retention,
restart persistence and denied-set isolation. Native journeys exercise the same
fields via touch/Back, actual MP3/Opus/M4A details, light/dark, rescans and server
restart; then download the restored release and display its complete saved
snapshot after removing the adb server route. The capture uses visible Play
and source/session/audio proof, followed by a real media-pause key to stabilize
the animated UI before fresh semantics reads; this is not an OS-control claim.
The native runner exits nonzero on any recorded failure.

## Verified acceptance, 2026-10-22

M05 passed on installed Google Chrome 144.0.7559.132, Mozilla Firefox 156.0
and the native release app on Android 14/API 34. The shared 0k3/7k3 batch
reused the disposable port-18814 server/database and one read-only emulator
session. Both browser journeys passed on each browser: complete grouped
fields, real disclosure keys and focus, pointer/typing, dark desktop and light
narrow layouts, offline error/retry, original source audio, real tag changes,
clearing, failed-probe preservation, restart and permission isolation.

Native acceptance passed all rich fields in both themes, physical MP3/Opus/M4A
aliases, changed/cleared/failed-probe persistence with stable identity and user
state, same-database server restart, restricted-source API denial, and actual
source playback. A real download retained every rich field in both themes
after removing the server route; Back closed the saved-details dialog and
retained Downloads. Original screenshots were reviewed for readable wrapping,
dates, participants, comments and literal markup. Independent output analysis
found 613 Hz in the native capture and approximately 613 Hz in both browsers.

Evidence is outside the repository at
`/home/paul/tmp/0k3-evidence/batch-final`: Chrome M05 cases in
`browser-attempt1-chrome`, Firefox in `browser-attempt4-firefox`, native in
`native-attempt1-m05`, plus command/version records, screenshots and audio.
The accepted APK SHA-256 is
`abb079b8ae86bc24eb2410193401322aff24d2aff0fe4d155075a48b6a1defee`;
the installed APK matched it. Versions remain server/app 0.8.2, Android build
22 (x86_64 APK code 4022). No release, version bump, push or deployment ran.

Combined product gates passed: Flutter analysis, all 1,412 Flutter tests,
Go build/vet and full race/coverage tests with required real FFmpeg checks,
web units, 38 native-runner oracle tests and 34 fixture-support tests. Normal
server and release x86_64 APK builds passed. Errcheck retained 256 existing
diagnostics with none added; it is not clean. M04 fixture/oracle refinements
receive focused reruns without rebuilding unchanged product code. M04 acceptance also passed; the two tasks are committed and closed together.
