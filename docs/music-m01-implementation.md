# M01 embedded music metadata

## Interaction design (recorded before implementation)

This slice applies U01 (§6.5 and §10.1 of music-interaction-design.md) to
existing Sets browsing; album/artist navigation remains in later tasks.
All audio files qualify for tag extraction, including existing podcast and
book files; this does not create a separate Music lens or change set ACLs.
Filename, path, media ID, source/download URL and saved progress retain their
existing identities.

Web card: `[existing cover and Play/actions] / Title / Artist · Album`.
The title is the embedded title or filename stem, with one-line ellipsis,
full title in the accessible name and hover text. The secondary line uses
existing subtitle typography/theme tokens. An absent artist/album omits the
line. Existing keyboard selection → Enter/Space playback → Info → Escape
keeps the current selection and focus. Existing pointer/touch actions remain.
No shortcuts or competing navigation controls are added.

Web Info: `Title; Filename; Artist; Album artist; Album; Track; Disc; Date;
Year; Genres`, followed by current technical/path/progress rows. Missing
values are omitted, zero/invalid numbers are not invented. Existing scrollable
Info dialog, loading/error states, close control and focus restoration apply.
The full filename remains visible here even when the title equals its stem.
Player restore label, page title, bookmarks and casting use the same title.

Android folder/grid: `[existing thumbnail] Title / Artist · Album` with
ellipsis and the full text in semantics. Detail: existing title/favourite,
artist/album subtitle, existing technical row, then a labeled Music details
expansion containing all tags plus Filename/path. Expanded values wrap.
Existing Play Audio (48dp), menus, loading/retry, permission/offline behavior
and Back hierarchy remain. Playback route/notification and offline-download
labels use the same display title; original filename remains for source type
and storage. Existing Material typography and theme colors apply in light,
dark and narrow layouts.

Transition: scan → browse with titles → detail/Info → play → Back/Escape.
Rescan refreshes tags in place, including removal, without recreating rows or
changing favourites, bookmarks or progress. Untagged/whitespace tags show the
filename stem. Unicode and punctuation are preserved. Structured multi-value
artist tags remain separate; punctuation such as `feat.`, slash and semicolon
is never guessed as an artist separator. Genres are distinct from user tags.

## Verification plan

Extend the oj3 synthetic library/runners, retaining media outside the repo.
Exercise MP3/FLAC/M4A/Vorbis/Opus metadata, missing/partial/whitespace/invalid
numbers, Unicode and structured multivalues. Check current and upgraded rows,
rescan stability/removal and preserved progress/likes. Both installed Chrome
and Firefox must show tags, play the expected tone/source, support typing and
keyboard/focus restoration plus pointer navigation. Native emulator must show
the same tags, play the expected source/tone and support touch/Back. Capture
light/dark/narrow layouts. Build both server/APK from the actual feature tree;
record exact versions, commands, artifacts and any gaps before acceptance.

## Implementation and limits

The existing prober, media repository and scanner own extraction, persistence
and refresh. One nullable JSON column adds the `music` object to existing media
responses; a dedicated tags/source-size update avoids overwriting concurrent play
counts/trash/user state. The refreshed source size keeps manual downloads
consistent with the actual Content-Length after tags change. Every rescan re-probes existing audio, including empty
results to clear removed tags. A failed probe preserves the indexed values.
Uploads use the same metadata mapping. Existing plain search includes title,
artist and album without introducing an album/artist catalog or music lens.

The parser uses container tags before audio-stream tags, normalizes common
field aliases and validates positive track/disc numbers and totals. Raw date
is preserved; valid date-year wins over separate year, which is the fallback.
Repeated FLAC/Vorbis/Opus comments use bounded comment boundaries rather than
ffprobe's semicolon-flattened strings. Artist punctuation remains verbatim.
Other formats retain ffprobe's values; repeated ID3/MP4 boundary preservation
is not claimed. This is server-managed audio indexing; Android document-library
files retain their existing device behavior. Embedded genres and user tags are
independent. New offline downloads retain their tag snapshot and original
source context; old snapshots retain their existing stored titles.

The source filename/path remains separate from display title throughout file
operations, sharing, casting and Android playback requests. This keeps original
stream/download identities and the existing audiobook/visuals heuristic. The
PWA cache includes the new web module under cache revision `kiss-v25`.

## Verification evidence

External evidence root: `/home/paul/tmp/pj3-evidence/`. Shared versions remain
server 0.8.2 and Android pubspec 0.8.2+22 (APK versionCode 4022). The server and
APK were rebuilt from the feature tree at `6d7b116-dirty`; the server was rebuilt
again after the source-size repair. Final APK SHA-256:
`807b7ac9cd4d68e5d9ccdc8f3dd31a5ed30aabc813cb487dc8050a6a918f502f`
(`apk-fifo-build-info.json`, `server-build-info.txt`). No version bump or release.

Google Chrome 144.0.7559.132 and Mozilla Firefox 156.0 passed all 18 journeys
(nine each, `web-full.log`): actual login, Sets/folder keyboard and pointer/touch
navigation, typing/focus, audible source, tags/Unicode/multivalues/missing fields,
themes and rescan change/removal with stable identity/favourite/saved progress.
Settled theme screenshots passed two focused journeys (`web-settled.log`);
light mode asserts the real persisted theme and computed colors. Reviewed
screenshots and WAVs are in `run/{chrome,firefox}/`. A stronger duplicate oracle
passed both rescan journeys (`web-duplicates.log`), checking total set size and a
unique original path/ID. After the size repair, both browsers passed the final
rescan journeys again (`web-size-download.log`, two passes), comparing indexed
size with HEAD Content-Length, actual downloaded bytes and original suggested
filename for changed, removed and restored tags. Firefox's BiDi adapter lacks
touch emulation, so its actual pointer journey uses mouse input; this existing
harness limitation remains explicit.

Native verification used the actual final release APK on the cold-booted API 34
x86_64 `Player_FDroid_Test_API34` emulator with QEMU WAV output. All required M01
journeys passed across `native-final.log` and the bounded
`native-focused-recovery.log`: all fields, structured repeated artists/genres,
Unicode, MP3/FLAC/M4A/Vorbis/Opus, missing/partial/whitespace tags, long light/dark
titles, changed/removed tags, stable filename/ID/count/progress, media-session
title and actual source audio. The title/source capture measured 417.5 Hz for
6.98 seconds against 415.3 Hz expected, within the existing native tolerance.

The focused native run also proved a changed-tag source downloads successfully
after rescan, with indexed size equal to actual source bytes. The Downloads UI
kept `Changed 東京 Title`; after removing the adb reverse server route, that
saved copy played with the same media-session title and original source tone
(423.2 Hz for 6.74 seconds, within the existing tolerance). This exercises the
persisted offline snapshot in actual playback, beyond JSON round-trip tests.
Initial title and size were restored and polled before teardown. Screenshots,
raw audio, full session readings and API identity checks remain in `run/android/`.

Earlier failed attempts are retained. Initial System UI/launcher ANRs and a
Material expansion selector mismatch are recorded in `startup-systemui-anr.log`,
`native-expansion-selector.log` and their screenshots/XML. The final default run
recorded 35 passes, four failures and two gaps. Two failures were harness misses:
the existing browse API flattens single-file folders, so Ogg and untagged targets
were already visible in the parent. The guarded helper now skips a missing
segment only when the requested media is visible. Both original failures and
screenshots remain in `native-flatten-attempt/`; explicit focused recovery rows
for those same files passed full details and original filename/path checks.

The original gain/EQ captures ran at actual Android media volume 5/15, which
pushed quiet signals below the unchanged detector floor. `--set` appeared to
succeed but AudioService rejected it through AppOps; actual volume keys on the
responsive device reached verified 15/15. Diagnostics are in
`audio-volume-{failed-attempt,recovery}.txt` and
`audio-volume-command-logcat.txt`. Focused source/title/duration checks passed for
both gain files and their measured difference was 17.99 dB against 18 dB expected.
EQ initially failed a strict exact-frequency Goertzel comparison (12.51 dB
spread) despite correct title and 6.61 audible seconds. Source/capture
neighborhood spectra in `eq-spectrum-diagnosis.json` show shifted captured
peaks and exact-bin cancellation from emulator timing/phase drift. Task
**6o3** fixed measurement in `audio_trace.py` (short-frame peak Goertzel;
tolerance unchanged); synthetic equal/unequal and the retained
`recovery-eq.wav` assert the calibration. Full native EQ re-run remains for
integrated verification.
Emulator click measurement (seven discontinuities beyond known gaps) and absent
automatic track advance remain explicit gaps. All three known gap calibrations
passed at 5.01, 20.02 and 50.05 ms. Baseline touch/audio, 48dp targets, Back,
saved progress, themes, server restart, locked relaunch and permissions passed.

Full final gates passed: server build/vet and FFmpeg-backed Go race/coverage
(`go-{build,vet,full}-size.log`), Flutter analysis with no issues and all 1,329
tests (`flutter-{analyze,full}-fifo.log`), and all 23 web unit files
(`web-unit-final.log`). Full errcheck reports 256 existing diagnostics; parent
compared each diagnostic's code with HEAD and found no new diagnostic code
(`errcheck-size.log`); errcheck is not claimed clean. Targeted native model,
source and offline tests passed 37 cases; native runner regression tests passed
14 cases. The existing all-formats harness was updated for the display contract,
but a fresh heavy all-formats device suite was not run for this slice.

Two preservation repairs have deterministic evidence. The existing progress
queue sorted cross-media per-item wire clocks rather than durable insertion IDs.
A frozen-clock regression failed before repair (`queue-fixed-clock-before.log`)
and all 32 queue tests passed after row-ID FIFO ordering
(`queue-fixed-clock-after.log`), with wire clocks unchanged, followed by the full
Flutter pass and final APK rebuild. A real scanner regression confirmed that
tag-only refresh left size stale (20,385 indexed versus 20,388 actual bytes),
causing Android downloads to reject the source (`source-size-before.log`). The
narrow atomic tags/source-size patch passed real title changes, a long Unicode
tag and removal with stable identity/count/progress (`source-size-after.log`),
then full Go gates and actual browser/native download checks.

Both fresh review rounds completed; the blocking native silent-skip oracle was
fixed and covered by a negative regression. No third review round was launched.
The owned disposable server, emulator and Pulse sink were stopped; app data and
adb reverse were cleared, and fixture teardown confirmed the port free
(`fixture-final-teardown.json`, `emulator-final-teardown.txt`). No generated
media, databases, APKs, binaries or coverage files are part of the source diff.
