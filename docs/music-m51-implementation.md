# M51 selectable download quality

Stage: IMPLEMENTED/REVIEWED — integrated verification pending.
Prerequisites: accepted U01/V01 (nj3/oj3), reviewed streaming quality 4k3
(`9cd5343`) and bulk downloads pk3 (`65f6086`).

The existing Download music dialog now places a labeled **Download quality**
select between the song count and the storage guidance. Original is the default;
server profiles show AAC 128/256, MP3 192 and Opus 96 kbps. The browser remembers
this preference independently of Stream quality. Single audio downloads from
the card or D use this dialog too. Other file downloads retain their policy.
The existing modal layout, theme tokens, native select, Tab trap and Esc return
to the opener continue to apply. No shortcut is added. At narrow widths the
existing modal scrolls, and technical labels remain in the select.

Android's Keep music offline dialog uses the same labeled control above its
progress. Download quality also has its own scrollable bottom sheet reachable
from Stream quality and a separately labeled direct Download quality entry in Settings.
Material controls supply touch targets and theme colors; Back closes the sheet.
The primary action remains Keep offline. Existing per-song copies are retained
even after a preference change; the UI explains removing a copy before saving
another quality. Downloads → Music details shows recorded quality, MIME type
and source filename; older copies explicitly say quality was not recorded.

State transitions: loading profiles → Original plus saved selection → profiles
ready; failed loading retains Original and Reload download profiles. A saved
unavailable profile stays visible and fails explicitly when used. Saving errors
retain the previous choice and display a status. Starting a batch freezes its
choice and disables the select; preparation → byte progress → saved, or a
visible retry/cancel state. Stream settings and playing sources are unaffected.
Native cancel retains completed songs; a server archive failure discards all
temporary ZIP bytes. Existing sign-in/access, account-generation and origin
checks apply during preparation and transfer.

The server reuses QualityStream and the bounded rendition cache. It prepares
and copies each song sequentially into the temporary archive so a collection
can exceed the disposable cache budget without preflight evicting earlier
songs. Each song has a five-minute preparation cap, cancellation follows the
request context, and no partial archive is served. Archive and native saved
source filenames use the resulting container extension. Native schema version
7 records the immutable profile and response MIME type; profile labels include
codec and target bitrate. Existing copies migrate with unknown quality rather
than an invented Original label. The local hash filename remains an internal
opaque `.media` path and its playback metadata carries the real extension.

Integrated acceptance still required: actual Chrome and Firefox keyboard,
pointer, light/dark/narrow and canceled/retried archive journeys; native
touch/Back, restart, account, cancellation and storage errors; ffprobe actual
downloaded codec/bitrate/container and playback of each kept copy offline.
Full suites and final server/APK builds remain deferred by the implementation
first workflow. Focused checks and bounded review outcomes will be recorded
before this slice is committed. No runtime acceptance is claimed here.

Focused checks (2026-10-23): `GOMAXPROCS=2 go test -p 1
./internal/service ./internal/api -run 'TestMusicDownload' -short` passes;
`flutter analyze --no-pub` on the eleven changed native source/test paths is
clean after repairing three brace-style infos. `flutter test --no-pub
 test/services/offline_downloads_test.dart test/services/download_quality_test.dart
 --concurrency=1 --reporter=expanded` passes 30 checks. Node production-module
checks `download-quality.test.js`, `music-downloads.test.js` and
`stream-quality.test.js` pass. These checks cover selected archive bytes/name,
quality isolation, saved encoding metadata, MIME mismatch, retained copies,
cancel/account boundaries and modal focus, without claiming real encoded-media
or browser/device acceptance. Full-package errcheck was not run in this focused
compiler slot and remains an integrated verification check.

Review round 1 found two blockers, repaired together. Cold archive preparation
now suspends the response write deadline and restarts the ordinary write budget
before either success or error delivery. A real httptest HTTP server with a
20 ms write deadline and a 60 ms delayed quality service verifies complete ZIP
success and readable 507 failure responses. Native download start stays disabled
until the initial preference is known and while a selection is being saved;
failed saves restore the prior visible choice. Three widget regressions exercise
delayed initialization, delayed successful persistence and delayed failed
persistence, preventing a start with the old profile in each pending state.
`GOMAXPROCS=2 go test -p 1 ./internal/api -run 'TestMusicDownloadsHTTP' -short`
passes; native scoped analysis of the picker, dialog, Settings and widget test
is clean; the picker widget and preference tests pass 5 checks. Narrow review
round 2 is pending; these results do not alter the deferred runtime acceptance.

Round 1 found two blockers: cold archive preparation outlasted the server write
deadline, and the native start action could freeze an older quality while the
visible preference was loading or saving. Preparation now suspends the deadline
and restores a fresh response budget before success or error. Real HTTP tests
with a 20 ms write deadline and a 60 ms quality stub verify both ZIP delivery and
a delayed storage error. The native picker publishes readiness with its durable
selection; the dialog disables starting until ready and preserves that choice
for the batch. Three delayed load/save widget checks pass, including rollback
after a rejected save. Scoped analysis, including the direct Settings entry,
remains clean. Round 2 confirmed both fixes and found no new blocking defects;
that review did not rerun checks. Integrated acceptance remains pending.
