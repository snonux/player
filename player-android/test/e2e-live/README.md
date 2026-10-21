# Android live end-to-end run

`android_e2e.py` drives the installed Player app on an Android emulator
through `adb` and `uiautomator` and checks it against a deployed server. It
is the Android counterpart of the web suite in
`player-server/test/e2e-web/tests/live-deployment.live.ts` and covers the
same journeys: connect and sign in, wrong password, set and file listing,
playback of every supported format, search and type filter, favourites,
tags, notes, share links, saved progress, settings and admin screens, a
regular user with one granted set, and log out.

It is not part of `flutter test`: it needs an emulator, a released APK and a
reachable server, and it takes about 20 minutes per server.

One check is server-side only: the single-use share (`max_uses: 1`). The
script cannot open a share link inside the app (it drives the UI by taps and
sends no VIEW intent), so it acts as the app's share viewer does over HTTP:
one metadata fetch, three ranged requests of the returned `playback_url`
(which carries the viewing credential), and then a second client that must be
refused with `410`.

## Why adb and not integration_test

The run is meant to exercise the released, signed APK exactly as F-Droid
ships it (including the upgrade from the previous version), not a debug
build with a test harness linked in. Flutter exposes its semantics tree to
`uiautomator`, so buttons and labels can be found by their text.

`uiautomator dump` takes a few seconds per call. Anything that moves faster
than that (video frames, the audio seek bar) is therefore judged from two
screenshots taken a moment apart, not from the UI tree.

## Prerequisites

- Android SDK with `adb` on `PATH` and an x86_64 emulator image. The AVD
  used so far is `Player_FDroid_Test_API34`.
- Python 3 with Pillow.
- The server holds the all-formats library: run
  `player-server/testdata/gen-all-formats.sh`, copy the three sets into the
  server's `MEDIA_ROOT` and trigger a rescan.
- An admin and a regular account on the server. Put them in an env file
  outside the repository (mode 600), by default `~/.config/player-e2e.env`:

  ```
  E2E_ADMIN_USER=...
  E2E_ADMIN_PASS=...
  E2E_USER=...
  E2E_USER_PASS=...
  ```

  Set `PLAYER_E2E_ENV` to use another file. The regular account must have no
  set permissions; the run grants and revokes `test-images` itself.

## Running

```sh
emulator -avd Player_FDroid_Test_API34 -no-window -no-audio -no-boot-anim \
  -no-snapshot-save -gpu swiftshader_indirect &
adb wait-for-device
adb install -r player-vX.Y.Z-x86_64.apk      # from the GitHub release

PLAYER_E2E_OUT=/tmp/player-e2e python3 -u android_e2e.py https://player.example.org
```

Each check prints `PASS` or `FAIL` with a short detail; the last line is the
total. `results-<host>.json` and `shots-<host>/` are written below
`PLAYER_E2E_OUT` (default: the current directory).

The run expects the installed app to report the version in
`player-android/pubspec.yaml`; set `PLAYER_E2E_APP_VERSION` when testing an
APK built from another version.

The run clears the app's data (`pm clear`) twice, so do not point it at an
emulator whose Player data matters. On the server it removes the favourites,
tags, notes, shares, progress and permissions it creates.

`adrv.py` is the small driver underneath. It is also a command-line tool for
exploring a screen when a label changes:

```sh
python3 adrv.py show            # list the labelled nodes on screen
python3 adrv.py tap '^Settings$'
```

## Music baseline (`music_e2e.py`)

`music_e2e.py` runs the V01 music baseline against the disposable music
fixture (`integration_test/support/music_fixture.py`) instead of a deployed
server: sign-in, a touch journey to a track, media session and captured
audio output, tap target sizes, Back hierarchy, saved progress, light and
dark screenshots, ReplayGain fixture levels, capture calibration against
known 5/20/50 ms gaps and the EQ probe, a server restart, a cold relaunch
(remembered server, locked saved login, explicit password sign-in) and the
regular account's permissions. The online baseline checks Android network
connectivity before setup: streaming through `adb reverse` alone also works
in airplane mode, where the app deliberately queues progress offline. It
reaches the server through `adb reverse` (or `--host-alias` for 10.0.2.2)
and writes evidence to the fixture's evidence directory. Shared steps live in `app_ui.py`. See
`../../docs/music-e2e-verification.md` for the runbook, including the
emulator's QEMU wav audio backend used for output capture.

```sh
python3 -u music_e2e.py <fixture-dir> --apk app-x86_64-release.apk \
  --emulator-wav "$TMPDIR/emulator-output.wav"
```

Run `python3 -m unittest test_music_e2e` here for the lightweight capture
timing and saved-login oracle regression checks (no emulator needed).

## Keeping it in sync

- New extension in `player-server/internal/mediatype/mediatype.go`: add it to
  `FORMATS` here, to the web suite and to `gen-all-formats.sh`.
- Changed button or screen labels: update the regular expressions passed to
  `adrv.tap` / `adrv.wait`.

## M12 real track transport

With a fresh APK, disposable music fixture and running emulator using the
QEMU WAV backend from the music runbook:

```sh
python3 -u music_sequence_e2e.py <fixture-dir> --apk app-x86_64-release.apk \
  --emulator-wav /path/to/emulator-output.wav
```

The runner checks touch controls and distinct 15-second seeking, three
natural completions with actual source tones and server progress by media
ID, Back/background playback, actual notification and lockscreen buttons,
light/dark layouts, missing-source Retry and audiobook source seeking. It
requires keyguard state before claiming lockscreen proof. `--scenario` selects
a focused recovery while retaining prior attempts. It uses only the disposable
fixture, adds one generated spoken-audio source there, and clears app/reverse
state at teardown. Missing native controls or audio capture are failures.

M02 albums use the disposable music fixture and an emulator started with QEMU
WAV capture, as described in `../../docs/music-e2e-verification.md`:

```sh
python3 test/e2e-live/music_albums_e2e.py /path/printed/by/music_fixture/up \
  --apk build/app/outputs/flutter-apk/app-release.apk \
  --emulator-wav /outside/repository/emulator-output.wav
```

The runner signs into the regular viewer account, compares real permissioned
catalog/detail/artwork identities with the admin-only contribution to the same
album, uses library and song entry, linked artist filtering, disc rows,
light/dark/long names and Back. It captures original audio for ordered natural
A→B→C completion after leaving the player and album screens, proves start0 and
stopend plus fresh progress on replay, uses the real notification shade Next
for the album collection with audible target/progress proof, and exercises
real album/stream ACL
failure and Retry. It removes/restores the adb reverse route for actual
offline request recovery; run without `--host-alias` for this check.
`--scenario catalog|natural|errors` supports focused recovery while preserving
failed evidence. Never point this runner at a production database.

### M13 editable Now Playing queue

Use a freshly built APK with a separate local music fixture, created with
`music_fixture.py up --no-doubles`. Run the real scale helper before either
browser or Android checks. It adds 1001 distinct media paths/IDs under the
fixture's permitted Music set; the files contain real 440 Hz audio. No media,
SQLite state, APK, WAV or screenshot belongs in git.

```sh
python3 ../../integration_test/support/music_queue_scale_fixture.py "$PLAYER_MUSIC_FIXTURE"
python3 music_queue_e2e.py "$PLAYER_MUSIC_FIXTURE" \
  --apk /absolute/external/path/app-x86_64-release.apk \
  --emulator-wav /absolute/external/path/emulator.wav
```

The emulator must already run with the QEMU WAV backend and actual unmuted
STREAM_MUSIC output. This runner uses adb reverse; omit `--host-alias` because
its offline check removes/restores only that disposable reverse mapping.
`--scenario` can isolate `editing`, `natural`, `retained`, `errors`, `themes` or
`large`, `close` or `scope` for a rerun; `account` isolates only the
logout/login portion of `scope`. Default `all` requires the scale album
and runs all eight; `remaining` runs editing/natural/retained/close/scope as a
focused rerun after independent error/theme/large proof.

Checks use actual touch/Back, the native media session, source tones in QEMU
output and saved progress by real media ID. They cover editable-order Next,
queue jump, current and other removal, Clear/Undo without autoplay, fresh
ownership after resume, Back to library, natural A→C→B while browsing elsewhere,
retained inactive queue versus Sets Next, real stream permission loss/retry,
offline edits, light/dark screenshots and scrolling a lazy 1001-entry queue to
its last source. Twelve-second sources pause immediately before UIAutomator
edits. Active-route volume keys set STREAM_MUSIC to 15, followed by strict
max/unmuted readback and a paused wait for the volume overlay to dismiss before
row-menu taps. Every audio proof reads the signed-in viewer's progress and
asserts its user ID and target media ID. The scope journey force-stops/relaunches
without clearing data, selects the remembered server and explicitly signs in to
the locked remembered login before checking Queue is empty. It then logs out
from a nonempty viewer queue and signs in as the admin through actual controls
without resetting app data. Failed steps retain screenshots, UI XML and tracebacks; a unit or widget
check does not count as native acceptance.

### M14 explicit song insertion

On the fresh disposable music fixture and running API34 emulator with actual
QEMU WAV output, run:

```sh
python3 music_insertion_e2e.py <fixture> --apk <fresh-x86_64.apk> \
  --emulator-wav <owned-emulator-output.wav>
```

Use `--scenario editing|natural|boundaries|themes|scope|feedback|batch` for a focused recovery;
`all` includes the theme journey under editing. The runner exercises actual
single/multiple selection in displayed order, repeated FIFO Play next, append,
duplicate occurrences, source/position preservation, Play now with captured
expected tone/viewer media ID, Undo without autoplay, natural A→C→B audible
order and authoritative viewer finished IDs, native Sets file-detail actions,
real revoked-source retry, offline queue insertion, light/dark/long labels and
48dp menus, selection Back, cold locked restart and actual logout/admin scope.
It reuses M13's strict viewer-ID/source-output helpers, never their old artifacts.
Failed attempts retain screenshot/UI XML/traceback. APK/audio/database/evidence
must remain outside git. Run the real browser and native journeys before M14
acceptance; unit/widget checks and this runbook alone are insufficient.

`--scenario feedback` isolates the UI correction: fresh empty queue, batch two
songs with visible Added 2 songs, then append one with visible Added 1 song and
no stale batch message. It taps that snackbar's Undo using bounds from the same
semantic snapshot and proves the queue retains exactly the original two songs.
No audio/volume loop is needed because this path only verifies confirmation UI.

M14 verification checkpoint (2026-10-21): native API34/x86_64 release 0.8.2/code
4022 (37c42671 FIFO-fix APK) passed all core scenarios in one clean run, with
viewer UID 2 progress, actual 440/659/523 Hz order, duplicate transport, real
permission recovery, offline insertion, cold restart and account clear. Parent
screenshot review caught stale queued confirmation feedback; a widget regression
now passes after replacing old feedback. A fresh 777c1ce9 UI-fix APK and full 1,361-test Flutter gate pass; the focused
feedback smoke above passed with the latest Added 1 song confirmation and exact
Undo restoring the original two. The focused batch scenario also passed on 777c: actively playing A preserved
through Append, then retained six-row order and actual B523→C659 with viewer
UID 2 media IDs 2/6/4. All exact single/multiple action combinations are accepted
on native, Chrome and Firefox. See
[the M14 verification record](../../../docs/music-m14-implementation.md) for
the build distinction, commands and retained evidence.

`--scenario batch` starts with two occurrences, prepares reverse-order selection
while paused, then verifies batch Append preserves the actively playing A source
and forward position with 440 Hz output. Batch Play now inserts displayed B,C
contiguously, retains the remainder, and verifies B523→C659 with viewer media IDs.


M03 artist acceptance: on the running API34 emulator with a freshly built APK,
run `python3 -u music_artists_e2e.py /home/paul/tmp/zj3-fixture --apk
/path/to/player-x86_64.apk --emulator-wav /home/paul/tmp/zj3-evidence/emulator-output.wav`.
The real native runner covers common Artists plus track/album/composer/performer
roles, structured credit links, discography and Back with role retention,
light/dark, song Composer/Performer details and original source audio. Its errors
journey stops only the verified owned fixture server to close existing keepalive
connections, removes reverse, then restarts the same binary/database/media and
restores reverse before Retry. It then revokes/restores
actual viewer set permission and retries the same artist. `--scenario journey`, `--scenario position`, `--scenario errors` or
`--scenario restart` permit focused recovery while preserving failed evidence.

M03 final acceptance passed on installed Chrome144/Firefox156 (six browser cases)
and native API34/x86_64 APK0.8.2/code4022. Native core/position/cold-restart passed
in the initial run, and focused errors-only recovery passed after replacing the
ineffective reverse-only cutoff with the verified owned-server cutoff. Exact
commands, versions, original failures, original screenshots and source audio
remain external; the full truthful record is in
`../../../docs/music-m03-implementation.md`.
