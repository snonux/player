# M51 selectable download quality

Stage: ACCEPTED — focused Chrome, Firefox and native Android verification
completed 2026-10-27. The combined regression gate remains due at completed
task 20 under the user's ten-task cadence.
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

The browser export and native offline journeys below exercise the feature on
all three required platforms. The full regression suite remains scheduled for
the ten-task gate. Native low-storage failure and permission revocation were
not separately forced on the emulator; focused implementation tests cover the
storage/error path, and both installed browsers exercised a real ACL denial.

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
round 2 later confirmed both fixes.

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
that review did not rerun checks.

## Focused installed-platform acceptance, 2026-10-27

The local server was rebuilt with `GOMAXPROCS=2 go build -p 2 -o player
./cmd/player` in `player-server/` and run in isolated
`/home/paul/tmp/codex/player-m51-18851` on port 18851 with `--no-doubles
--shuffle-playback`. The server binary SHA-256 is
`3eb987fd2ca8d5aa8e0130750ff96c912840b580a3c4e13bc82e76576ef1d5a9`.
The local universal Android APK was built after implementation and installed
on one read-only Android 14/API34 emulator; its SHA-256 is
`5681c380851537873543bac30a26544fb140c1b562d0c49b12c10503c7f11dbf`
(`0.8.2+22`). Neither build is a release. Evidence is outside the repository
at `/home/paul/tmp/codex/player-m51-evidence/`.

Installed Google Chrome 144.0.7559.132 and Mozilla Firefox 156.0 each passed
two focused tests, serially with one Playwright worker and `GOMAXPROCS=2`:

```sh
cd player-server/test/e2e-web
PLAYER_MUSIC_FIXTURE=/home/paul/tmp/codex/player-m51-18851 \
PLAYER_M51_EVIDENCE=/home/paul/tmp/codex/player-m51-evidence/web \
PLAYER_E2E_PULSE_SINK=player_e2e_null GOMAXPROCS=2 \
  npx --no-install playwright test -c playwright.music.config.ts \
  tests/music-download-quality.test.ts --project=chrome --workers=1
# Repeat with --project=firefox.
```

The keyboard-opened album dialog selected AAC128 and Opus96 independently of
stream quality. Real three-song ZIP responses contained `.m4a`/AAC at about
125–128 kbps and `.ogg`/Opus at about 123–132 kbps according to `ffprobe`.
Opus96 is a nominal encoder target and the actual Ogg files exceeded 96 kbps
for these fixtures. Extracted files played through each installed browser
with networking disabled; captured output included the expected 440 Hz song.
The selected download choice persisted in the dialog; Escape restored focus.
Narrow 390px dark/light screenshots show the dialog without horizontal
overflow. Revoking the Music set permission after opening the selected AAC
dialog produced HTTP 403 and visible Retry; restoring permission let the same
dialog complete a real ZIP. See `web/{chrome,firefox}/`.

On Android, the direct Settings sheet chose MP3 192 kbps, and Night Transit's
Keep music offline dialog displayed that persisted choice. Touching Keep
offline saved all three songs. The copied device bytes probed as MP3 at exactly
192000 bps, with `.mp3` source names, `audio/mpeg` MIME and `mp3192` profile
in the private download index (`native-mp3-copies.json`). With Wi-Fi and mobile
data disabled and the `adb reverse` server mapping removed, First Light played
from its saved copy; QEMU output contained 44.9 seconds at 440 Hz
(`native-offline-mp3.json/.wav`). The capture used a −55 dBFS threshold because
this emulator mix measured −51 dBFS RMS. After a force stop and offline relaunch,
all three downloads remained available under On this device. Reconnecting to
the fixture and signing in again showed the MP3 preference still selected;
changing it to AAC256 required no new download. Touch and Back were used for
the Settings sheet and album flow, with light/dark sheet screenshots recorded.
