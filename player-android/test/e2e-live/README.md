# Android live end-to-end run

For M10 enrichment, keep a disposable music fixture running with
`MUSIC_ENRICHMENT_URL=http://127.0.0.1:18910/metadata` and the deterministic
provider from `player-server/test/enrichment/provider.py` using mode file
`/tmp/player-m10-mode`. Install the current APK on the one running emulator
with QEMU WAV output, then from the repository root run:

```sh
python3 -u player-android/test/e2e-live/music_enrichment_e2e.py /path/to/fixture \
  --emulator-wav /path/to/emulator-output.wav --evidence /tmp/player-m10-native
```

The runner maps both the Player and provider ports through `adb reverse`,
signs in as fixture admin and viewer, and checks touch refresh, provider
image/link, no match, failure, retained local cover, audio, offline Retry and
revoked access. It restores the provider's ready mode and viewer grant, and
clears app data on exit. Pass `--resume-device` to retain the signed-in app and
reverse mappings for a following native journey. It does not start an emulator
or server.

`music_equalizer_e2e.py` verifies M25 on an already signed-in disposable
fixture and normal installed app, using a running emulator with QEMU WAV
output. It never clears app data or restarts the server/emulator. Run it
from the repository root:

```sh
python3 -u player-android/test/e2e-live/music_equalizer_e2e.py /path/to/fixture \
  --evidence /tmp/player-eq/android --emulator-wav /path/to/emulator-output.wav
```

Use a fresh evidence directory and a fixture admin with a saved queue. The
runner saves its original queue and EQ setting, then verifies real flat,
custom, preset, bypass/reset, live preset change, background, buffered offline
and cold-unlock/source-reopen output. It restores the original queue and
settings through the public API/native controls. It temporarily disables only
the emulator's `com.android.musicfx` panel package to prove embedded EQ works
with no external panel resolver, restoring the exact package enabled state
in `finally`. The owned adb reverse mapping is similarly restored after
the offline check. It does not remove DSP libraries or simulate an unsupported
hardware effect. `--boundaries`, `--panel-recovery` and `--recovery` resume
individual stages with that same directory; pass `--restore-queue` with the
original queue snapshot for staged cleanup. Keep all WAVs/screenshots outside
git. Device effects vary, so the native preset oracle checks relative frequency
balance while bypass/reset comparisons retain the 1.5 dB tolerance.

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

Every shared `adrv.nodes()` call uses its own `/sdcard/adrv-<uuid>.xml`,
requires a successful dump naming that exact path, checks the read and parses
the complete hierarchy before returning nodes. Failed idle/write commands,
missing files and malformed XML raise with command/stdout/stderr diagnostics;
`find`, `wait` and `tap` propagate that failure instead of using a previous
screen or treating it as a missing label. The owned dump is removed even after
failure. A cleanup failure is reported without hiding an earlier error.
There is no fallback to `/sdcard/ui.xml` or automatic retry of a failed dump.
Run `python3 -m unittest test_adrv` here for the device-free regression checks.
These checks do not replace an actual native touch smoke test.

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

M26 persistent playlists runs against a fresh disposable Music fixture and the
native app on a running API34 emulator with QEMU WAV output:

```sh
python3 -u test/e2e-live/music_playlists_e2e.py /path/to/fresh/fixture \
  --apk /path/to/current/app-x86_64-release.apk \
  --emulator-wav /path/to/emulator-output.wav
```

It uses actual native create/edit/comment/Add music/delete controls, checks
saved membership/count/duration and rendered cover, force-stops and relaunches
the client, switches to the other account and back, captures original source
audio and exercises actual offline form failure after stopping the owned
fixture server and removing adb reverse, then restarts the same database.
All assertions are required, failures return a nonzero exit status, and
screenshots/state/audio are external fixture evidence.

M27 native playlist song editing uses `music_playlist_songs_e2e.py <fixture>
--apk <current-release-apk> --emulator-wav <qemu-output.wav>`. It drives actual
selected additions, an offline retained picker, Find over 47 saved occurrences,
move/remove/Undo, Back, native light/dark layouts, later-duplicate full-queue
source audio and force-stop persistence. It extends M26 helpers and supports
`--resume-playlist <id> --resume-stage playback|persist` for retained accepted
phases. Actual emulator execution and authoritative state/audio are required;
Python syntax checks alone do not accept this feature.

M27 acceptance (2026-10-21): exact release APK0.8.2/code4022 passed native API34
touch selection/find/move/remove/Undo, verified offline failure plus same-DB
restart, original full-queue occurrence46/source audio and force-stop persistence.
The final retained-device run passed in277.99s; original bootstrap/input-label
driver failures are preserved. Use `--hold-device` to leave the app/runtime for
review and `--resume-device --resume-playlist <id>` to continue the already
signed-in test device. Root accepted originals/JSON/audio then authorized owned
stop-only cleanup; fixture/evidence remain outside git. See the shared
[M27 record](../../../docs/music-m27-implementation.md) for commands and limits.

### M16 local queue recovery

Use the fresh root-built release APK and disposable API34 emulator with actual
QEMU WAV output. Before configuring reverse mapping, enable root adbd on that
owned userdebug emulator (`adb root`, then `adb wait-for-device`). The runner
reads just the scoped queue JSON from FlutterSharedPreferences.xml to verify
occurrence IDs and exact position, never saves full auth preferences or writes
application state. Root coordinates the runtime slot and cleanup:

```sh
python3 -u music_queue_recovery_e2e.py /home/paul/tmp/yk3-fixture \
  --apk /home/paul/tmp/yk3-evidence/app-x86_64-release.apk \
  --emulator-wav /home/paul/tmp/yk3-evidence/emulator-output.wav --hold-device
```

The runner seeds a real repeated playlist, taps its later duplicate, reorders
that occurrence and pauses partway. It force-stops/relaunches, verifies the
existing remembered-server/locked-login flow, explicitly signs in and requires
identical queue IDs/index/position and no PLAYING source before Resume. It
checks native light/dark paused-state screens and current-item detail/Back,
then actual touch Resume plus original tone output, account isolation and
original-account restoration, Clear/restart and real permission revocation.
Initial setup clears only this disposable app data; the runner leaves all
runtime and data at the end for root evidence audit and stop-only cleanup.
Failure traces/screenshots and snapshots go to the external fixture evidence.
A py_compile/pyflakes pass is preparation only, not native acceptance.

For the focused recovery-to-normal-Next supplement on the retained exact APK,
append `--resume-device --scenario normal-next`. This skips reinstall and initial
app-data clearing. It performs a verified owned-server outage during cold unlock,
checks visible connection failure with the scoped queue unchanged, restarts the
same binary/database and updates server ownership, then retries explicit sign-in.
After paused recovery, Resume and the actual notification Next control must
select the following Long Road Home occurrence with runtime position and audio.
The offline sign-in retry is distinct from queue Retry logic.

Pass optional `--ownership-path /path/to/runtime-ownership.json` when an
orchestrator tracks server PID/start identity there. The runner refreshes that
JSON after its verified owned restart only when the option is supplied.

M15 whole collections uses the same disposable Music fixture and the freshly
built native APK on the running API 34 emulator:

```sh
python3 music_collections_e2e.py /home/paul/tmp/kl3-fixture \
  --apk /path/to/current-app-x86_64-release.apk \
  --emulator-wav /path/to/owned-emulator.wav
```

The runner covers album/artist/playlist detail and catalog selection Play,
Play next and append, saved occurrence IDs/order, whole-batch Undo, actual
media-session/source PCM, touch/Back, light/dark themes, verified owned-server cutoff plus adb-reverse offline
failure and real permission revocation/retry. Select `--scenario detail`,
`selection`, `boundaries` or `recovery` for a bounded rerun. `--scenario core`
covers details, selections and boundaries without replaying recovery. Limit
a detail or selection rerun with `--kind album`, `artist` or `playlist`. Add
`--resume-device` to preserve an already signed-in device on a rerun and
`--ownership-path <root-owned JSON>` to update the verified server fingerprint
after the offline stop/restart phase. Queue preferences are read on
the rooted disposable emulator solely as authoritative evidence; all feature
mutations use visible controls. It deliberately leaves the app, fixture/server,
emulator and evidence intact for root review and coordinated stop-only cleanup.
It never invokes the baseline app-data-clearing teardown.

## Music M20 repeat modes

`music_repeat_e2e.py` drives the actual native app on the rooted disposable
API34 emulator. Transport checks let tracks naturally finish: Off stops and
same-source Replay stops again, All wraps edited occurrence order with both
source tones, One resets the exact later duplicate to zero, and touch Next /
notification Previous still change sources. Recovery checks mode/order/offset
through force-stop and explicit unlock/Resume, account separation, current
removal and Clear/Undo, empty-queue persistence and original portrait light/dark
screenshots. The offline phase stops only the owned fixture server, attempts
real cold unlock, then restarts that same binary/database for explicit retry.
The runner updates the optional ownership JSON after that restart and retains
all runtimes for root audit and stop-only cleanup. It reads only the scoped
identifier-only queue preference document, never changes preferences directly.

```sh
python3 player-android/test/e2e-live/music_repeat_e2e.py /home/paul/tmp/dk3-fixture \
  --apk /home/paul/tmp/dk3-evidence/app-x86_64-release.apk \
  --emulator-wav /home/paul/tmp/dk3-evidence/emulator-output.wav \
  --ownership-path /home/paul/tmp/dk3-evidence/runtime-ownership.json
```

Root must first authorize and start the fresh local fixture and emulator with
QEMU audio output. `--scenario transport|recovery|offline` and `--resume-device`
allow unfinished phases to run without repeating accepted ones; a resume still
force-stops/unlocks the retained app. All screenshots/audio/state remain outside
git. Compile/lint or unit results do not constitute native acceptance.

Within transport, `--transport-from all|one|manual` skips already accepted earlier
phases. M20's local preference reader retries transient failed/empty/XML reads
up to ten times and never logs full preferences; valid JSON/schema failures
remain failures. One's natural reset oracle reads the handler's persisted live
queue offset because Android media-session dumps can retain a snapshot position
between state events. It still requires the same occurrence, real PLAYING state
and measured original source audio.

Offline-only continuation can retain an existing real paused queue rather than
repeat transport setup. `--offline-snapshot <identifier-only.json>` checks its
exact order, selected occurrence/index/offset and mode against the recorded
oracle before stopping the owned server. No arbitrary maximum offset below the
actual track duration is required.

## Rich music system session (M54)

`music_media_session_e2e.py` uses the optional `--session-artwork` music fixture
and a rooted disposable API34 emulator. It reads the real OS MediaController,
including the actual MediaMetadata bitmap, through the small `app_process`
helper, and operates visible notification and keyguard buttons. Build the
helper outside git (the script records exact javac/d8/jar commands):

```sh
# From player-android/test/e2e-live/:
python3 media_session_probe.py /tmp/qk3-evidence/session-probe
python3 -u music_media_session_e2e.py /tmp/qk3-fixture \
  --apk /tmp/qk3-evidence/app-x86_64-release.apk \
  --emulator-wav /tmp/qk3-evidence/emulator-output.wav \
  --probe-jar /tmp/qk3-evidence/session-probe/media-session-probe.jar
```

The runner verifies distinct real titles/artists/albums and blue/orange decoded
OS bitmaps after Next/Previous, actual audio tones and fresh per-ID saved
progress, visible system Play/Pause, mini/full attachment without position reset,
light/dark, untagged/no-art fallback, actual adb-reverse cutoff, recovery and
logout clearing. The outage check waits for visible Retry while the reverse
tunnel is still removed; temporary preparing state is not counted as a failure.
The actual API34 error session became inactive (no old OS identity or artwork),
while the full player identified the failed target. Existing native error
behavior is preserved: system Play while
the source is failed is a no-op; the full player's visible Retry loads the
failed target, after which system Play/Pause works normally. The run retains
its runtime for the owning orchestrator's evidence audit and stop-only cleanup;
it never targets another emulator or an external server.

The standalone probe initializes Android's real media-service manager before
reading sessions, and preserves exit status/stdout/stderr beside each bitmap.
`--resume-device` keeps the signed-in installed app; `--evidence-dir` separates
attempts, `--skip-mini` skips an already accepted attachment check, and
`--from-boundaries` resumes the current paused A queue for the remaining cases.
`--offline-only` retains the existing queue and selects/pauses C through the
visible player before the remaining outage/recovery/logout checks.

Animated system media cards can prevent `uiautomator dump` reaching idle.
System controls require a successful fresh dump from `com.android.systemui`;
failed dumps never reuse `/sdcard/ui.xml`. In `--coordinate-controls` mode the
runner instead saves each current system screenshot and waits for a matching
coordinate JSON containing `screenshot`, `dimensions`, `x`, and `y`. Inspect that
exact original before supplying the visible button center. The actual OS
metadata/bitmap, source audio, and fresh progress still determine success.

For qk3 on 2026-10-22, the installed API34 x86_64 app was version 0.8.2, code
4022 (pubspec 0.8.2+22). Its installed APK matched the final artifact SHA256
`db92e0d82b055ab4ebb38d04e1ae95bbb8a9a94d529e049f18059c471541bc2a`.
Accepted phases span native attempts 2 (attachment), 4 (all four notification
and keyguard controls, artwork, distinct audio and fresh progress), 5 (theme
contexts and strict missing album/art fallback), and 6 (actual failed-source
Retry, recovered system controls and logout). Failed attempts are preserved;
merged semantics and failed idle dumps are described in the implementation
record. Shared stale XML handling is covered by follow-up task `bo3` and the
driver contract above; the runner's existing local guard remains in place.

The final unfinished outage/clear continuation used:

```sh
python3 -u player-android/test/e2e-live/music_media_session_e2e.py \
  /home/paul/tmp/qk3-fixture-attempt2 --resume-device --offline-only \
  --coordinate-controls --evidence-dir /home/paul/tmp/qk3-evidence/native-attempt6 \
  --emulator-wav /home/paul/tmp/qk3-evidence/emulator-output.wav \
  --probe-jar /home/paul/tmp/qk3-evidence/session-probe-fixed/media-session-probe.jar
```

Run that form from the repo root only after owning and verifying the retained
runtime, installed app and signed-in account. Each coordinate-mode step prints
the current screenshot and expected JSON path; it waits for inspection rather
than inventing coordinates. In the accepted run, visible Retry appeared before
reverse restoration; recovered OS artwork/audio/progress and actual system
Play/Pause were then checked. The full audio screen retains its established
black presentation in both system theme contexts; the library mini bar follows
light/dark themes. Physical Cast receivers and headsets were not exercised.

The external `m54-native-acceptance-index.json` and `native-actual-attempt*.log`
files under `/home/paul/tmp/qk3-evidence` map originals to accepted phases.
Root subsequently stopped only verified task server/emulator/Pulse resources,
verified no task browser processes remained, and preserved both fixtures and all
evidence. Full cross-component gates, reviews and the observed Chrome activation
limits are recorded in `docs/music-m54-implementation.md` from the repo root.

## M18 playback shuffle

Use the final normal release APK, disposable API34 rooted emulator, existing
music fixture and actual QEMU output WAV. Root owns startup and stop-only cleanup:

```sh
python3 player-android/test/e2e-live/music_shuffle_e2e.py \
  /home/paul/tmp/zk3-fixture \
  --apk /home/paul/tmp/zk3-evidence/app-x86_64-release.apk \
  --emulator-wav /home/paul/tmp/zk3-evidence/emulator-output.wav \
  --ownership-path /home/paul/tmp/zk3-evidence/runtime-ownership.json
```

`--scenario core|options|recovery|remaining|permission` allows bounded continuation; each creates its
own isolated playlist through the actual fixture API then uses native UI to
play it. `remaining` runs options and recovery only; `--from-cycle` skips an
already accepted core queue phase. `--resume-device` follows the saved-server/password unlock flow.
Production randomness is not seeded. Assertions compare exact occurrence IDs,
canonical order, mode/current/position and expected source PCM across a full
cycle including duplicate media. Touch/Back exercises direct Shuffle, queue
remainder/reshuffle/Undo, independent album browsing, optional genre/year/max,
invalid/no-match/actual reverse-cutoff offline retry and dialog cancellation,
portrait light/dark originals, force-stop and real account roundtrip recovery.

This runner locally uses unique, checked UI dump paths before parsing; failed
uiautomator output cannot reuse an older XML screen. Touch evidence records
fresh stable semantic targets and their original screenshots. It reads only the
current origin/account's queue document from preferences, never writes app
state. The existing delayed-result production widget regression supplies the
controlled pending-resolution Cancel/Back boundary. `py_compile` is only a driver
syntax check. Native acceptance spans attempt7 (queue remainder,
reshuffle/Undo, canonical Off/current/position, independent browse), attempt10
(full six-occurrence audible cycle), attempt13 (options/offline/filtered audio
and Cancel/Back), attempt14 (exact 3.219-second paused force-stop/account
recovery plus audible Resume), and attempt15 (real ACL denial for direct
Shuffle/options, exact source/3.721-second retention and restored-grant audible
retry). The actual album-denial HTTP status was 404; no fake 403 was injected.
The permission case captures the existing fixture grant and restores its exact
role in finally, then verifies restoration. All attempts live under
`/home/paul/tmp/zk3-evidence`.

This API34 UiAutomator XML omits EditText hint attributes. The runner builds
`MusicAccessibilityProbe.java` with existing SDK javac/d8, pushes its test-only
JAR, and sequentially reads fresh actual AccessibilityNodeInfo `getHintText()`
values. Exact labels must identify one editable, enabled, visible field with
valid bounds. A fresh root/tree is polled for at most seven seconds after each
connection; empty-tree exhaustion fails rather than falling back to old XML or
ordinal fields. `test_music_shuffle_e2e.py` has four negative/oracle tests,
including failed-idle stale-dump and empty-tree rejection. Original hint JSON,
field screenshots and target coordinates are retained for every action.

Earlier attempts retain compound-card selector, active-animation idle failure,
synthetic media Play routing, transient null-root, absent unused-account storage,
and auto-play startup-race
driver corrections. Playback uses actual visible Play/Next, with working scoped
media Pause only for stabilization. No new OS transport verification, physical
hardware or operated TalkBack is claimed. No product code changed during these
runtime corrections.

Create the fresh M18 fixture with `music_fixture.py up <server> --shuffle-playback`
(the normal 12-second baseline remains unchanged). This flag passes 45 seconds
into the three Night Transit encoder source specifications before generation;
the manifest uses the same specifications. The native runner independently
ffprobes all three actual files and compares server durations and manifest before
playing. A 45-second manifest alone does not pass this preflight.

## ReplayGain/loudness (M24)

`music_loudness_e2e.py` uses an already signed-in disposable fixture and the
normal installed APK on a rooted API34 emulator with actual QEMU WAV output.
It never clears app data, installs an APK, writes preferences, or restarts the
emulator/server. Root owns startup and installation. Feature settings use native
touch controls; scoped preferences are read only as a saved-state oracle.

```sh
python3 -u player-android/test/e2e-live/music_loudness_e2e.py FIXTURE \
  --scenario attenuation --evidence EVIDENCE/attenuation --emulator-wav QEMU_WAV
python3 -u player-android/test/e2e-live/music_loudness_e2e.py FIXTURE \
  --scenario modes --evidence EVIDENCE/modes --emulator-wav QEMU_WAV
python3 -u player-android/test/e2e-live/music_loudness_e2e.py FIXTURE \
  --scenario boundaries --evidence EVIDENCE/boundaries --emulator-wav QEMU_WAV \
  --restore-settings EVIDENCE/attenuation/initial-loudness.json
```

The short attenuation phase proves actual Loud Off/Track output first. Modes
measures both tagged tones with Off/Track/Album/Automatic, preamp ±6 dB,
Album +12 dB preamp limited by the known peak, and untagged +12/-12 dB. The
clipping oracle checks measured gain, sine crest and near-full-scale sample
fraction; a saturated 16-bit WAV cannot pass merely because its peak is ≤0 dBFS.
From the repository root, `python3 -m unittest discover -s
player-android/test/e2e-live -p test_music_loudness_e2e.py` checks its negative
oracles without a device and does not constitute native acceptance.

Boundaries opens Gain Check through actual typed Music Search, measures ordered
album Automatic and source changes, then positive Quiet Track gain through Home
and cold session recreation with explicit unlock/Resume. It captures light/dark
advanced sheets and Back, restores the original loudness values through UI and
leaves the admin Library paused. `--restore-queue ORIGINAL_QUEUE_JSON` additionally
restores a previously recorded scoped queue through the public CAS API and native
Continue/Replace; it never writes local queue preferences. All original failed
attempts, screenshots, touch bounds, WAVs, analysis and results remain external.
The attenuation phase records `initial-queue.json` before changing sources; pass
that file to restoration when it contains a pre-existing queue. `--resume-album`
continues an already established paused Gain Check album only after checking its
exact fixture-derived media order/current occurrence, avoiding repeated setup.

Positive gain uses Android 9+ DynamicsProcessing with only linear input gain;
the metadata peak already limits the requested boost. Android 7/8 remain supported
for ordinary playback and attenuation; a positive request shows the explicit
Android 9 capability message and retains unity playback. That older-API boundary
has controller/channel regression coverage, not an API24–27 device claim.

The 2026-10-24 API34 x86_64 run used the normal 0.8.2/versionCode4022 APK from
product fix `369243f`, with actual speaker volume verified 15/15. Final measured
RMS levels in dBFS were:

| Source | Off | Track | Album | Automatic outside album queue |
| --- | ---: | ---: | ---: | ---: |
| Loud | -6.02 | -15.02 | -6.52 | -15.02 |
| Quiet | -24.02 | -15.04 | -24.52 | -15.04 |

Preamp +6/-6 changed Loud Track output by +6.17/-5.87 dB. Album +12 preamp
was limited to a measured -3.15 dBFS RMS/0 dBFS peak, with 3.15 dB sine crest
and 2.98% near-full-scale samples. Untagged +12 matched Off at -15.02 dBFS;
untagged -12 measured -27.01. Ordered-album Automatic retained 17.86 dB between
Loud and Quiet. Positive Quiet Track gain measured -15.04 before Home, during
background playback and after force-stop, explicit unlock and saved Resume.
Original Off/0/0 and the exact three-occurrence queue/current/4.69-second offset
were restored through ordinary controls and CAS/Continue, leaving Library paused.

Evidence is under `/home/paul/tmp/player-native-loudness-evidence`: final modes
are `modes-final-369243f`; boundaries are `boundaries-continuation-369243f`.
The original failed high-level boost WAVs remain in `modes-attempt1` and
`clip-recheck`: Android LoudnessEnhancer produced -9.04 dBFS where -3.02 was
required. The same-source/settings fixed capture in `clip-after-369243f`
measured -3.00 dBFS. This justified replacing that nonlinear compressor with
[DynamicsProcessing input gain](https://developer.android.com/reference/android/media/audiofx/DynamicsProcessing),
using disabled EQ/compressor/limiter stages. Earlier boundary attempts retain
catalog/detail scroll and mini-player route assumptions; the accepted continuation
uses the actual typed search/album playback already established by attempt4.
Physical devices, API24–27 execution and Cast receivers were not exercised here;
the sheet explicitly limits normalization to local playback.

# Rich release metadata (M05)

`music_release_e2e.py` requires the disposable music fixture started with
`--release-metadata`, a current release APK, and a running API34 emulator with
QEMU WAV output. Run from the repository root:

```sh
python3 player-android/test/e2e-live/music_release_e2e.py /home/paul/tmp/0k3-fixture --apk /home/paul/tmp/0k3-evidence/player.apk --emulator-wav /home/paul/tmp/0k3-evidence/emulator-output.wav
```

It touches native release/credit details across formats/themes, real rescan
change/removal/failure and restart, permission denial, source playback, then
opens a saved metadata snapshot in Downloads after removing the adb server
route. Back returns to Downloads. Failures exit nonzero and retain evidence.
Use the original marked fixture directory; retag/restart cannot use a copy.
See [`music-m05-implementation.md`](../../../docs/music-m05-implementation.md).

### M04 multi-disc albums and compilations

`music_discs_e2e.py FIXTURE --apk FINAL_APK --emulator-wav QEMU_WAV` exercises
native disc headings/subtitles/art, compilation aggregation, multiple linked
credits/Back, direct second-disc global-index audio, natural disc-boundary tones
and collection end, light/dark/long text, missing/broken art, actual offline/ACL
Retry and M04 retag/clear/failed-probe/server restart. `--scenario core|errors|
state` supports focused continuation. `--resume-device` retains data/mapping at
exit; initial isolated sign-in still clears app data. Root owns one shared
emulator and all cleanup. See [the M04 runbook](../../../docs/music-m04-implementation.md)
for guarded additive fixture setup and exact pending acceptance boundaries.

### Metadata-aware search (M07)

Use the existing disposable Music fixture and the root-owned normal APK/emulator
session with real QEMU WAV capture; no new fixture-generation mode is needed:

```sh
python3 music_search_e2e.py /home/paul/tmp/9k3-fixture \
  --emulator-wav /home/paul/tmp/9k3-evidence/emulator.wav
```

`--scenario journey` covers native Music Search entry, title/artist/album entities,
Songs-only source playback/audio, query/chip preservation on Back, real artist
punctuation, Unicode, no results, long light/dark text and retained source identity.
Unicode uses the app's existing selectable Album metadata: actual Select all/Copy,
then Paste into the actual search field and native cursor/delete operations remove
the label. The runner requires the exact editable Unicode query; adb's ASCII-only
`input text` is never treated as Unicode proof. All controls use stabilized `adrv`.

`--scenario errors` stops only the verified owned fixture server to close existing
transports, verifies actual search failure, restarts the same database/server,
restores adb reverse, then exercises Retry and permission revoke/restore. It retains
query and authoritative IDs. Run with isolated test data only. The shared teardown
clears app data and adb reverse; all external fixture/evidence files remain.
`python3 -m unittest test_music_search_e2e` checks negative audio/Unicode oracles
without a device; it does not replace actual native acceptance.

### F03 existing file-library filters

Use the same final marked disposable fixture as M07, augmented once with
`integration_test/support/music_filters.py`. With the final APK and running
emulator, from `player-android/`:

```sh
python3 test/e2e-live/music_filters_e2e.py /absolute/new/fixture \
  --apk /absolute/final/player.apk --emulator-wav /absolute/emulator-output.wav
```

This drives the native Filters sheet, draft Apply/Back, inclusive zero and
minutes/MiB bounds, tags/sets/query/type/favourites/sort, Clear, detail tag editing,
light/dark, actual offline options Retry and authoritative result identity.
Use `--scenario journey` or `--scenario errors` for bounded continuations.
`--scenario controls` restores the combined-filter state and continues with
tag permissions/editing, set scopes, type/sort, Clear and theme checks after
range acceptance. The runner checks actual keyboard visibility before grid
scrolling and dismisses it without popping a route when it is already hidden.
`--scenario scopes` resumes set scopes, type/sort, Clear and themes after tag
acceptance. Grid swipes start below the actual preview toolbar so wrapping
filter summaries cannot turn a grid gesture into toolbar or keyboard input.
Results/screenshots use the fixture evidence directory; teardown clears app data
and removes its reverse mapping. The shared emulator stays running. Fixture
setup and persistence semantics are in [the F03 contract](../../../docs/music-f03-implementation.md).

### M17 saved server sessions

`music_saved_session_e2e.py` uses an already running rooted disposable emulator,
an installed current APK and the existing fixture admin login. It does not clear
app data or reinstall the app. First run the browser core M17 journey in the
server E2E README; its final admin session has three occurrences (two repeated
First Light entries and Long Road Home) and a paused seek offset.

```sh
python3 -u music_saved_session_e2e.py /absolute/fixture \
  --evidence /absolute/evidence/m17-native-core
python3 -u music_saved_session_e2e.py /absolute/fixture \
  --scenario acl --evidence /absolute/evidence/m17-native-acl
python3 -u music_saved_session_e2e.py /absolute/fixture \
  --scenario restart --evidence /absolute/evidence/m17-native-restart
```

Core covers Continue confirmation/Back, exact browser-to-native duplicate/current/
offset adoption while paused, actual Resume touch/source/advancing position,
native Pause, failed transport and competing revision choices, delayed Continue
during newer touch playback, and native Replace for browser continuation. A
transparent proxy temporarily routes the existing adb reverse through free
localhost port18770 (`--proxy-port` overrides it); HEAD/range metadata is preserved.
Queue transport failures and delayed responses are real HTTP boundaries. Reverse
mapping and the original system night mode are restored in `finally`.

ACL requires the viewer queue prepared as described in the server E2E README:
occurrences101/102/103, revoked middle/current Restricted media. It signs in
through native controls, verifies remaining101/103 with offset zero and no
autoplay/server overwrite, then returns to admin. Restart force-stops only the
owned app, unlocks the saved admin login with its fixture password, and verifies
identical paused queue/current/offset. It leaves the emulator/server running.
Scoped queue preferences are read only; complete preferences and credentials
never enter artifacts. Results, screenshots and sanitized HTTP traces go to the
chosen external evidence directory. Warmed cache sources are validated by real
HEAD requests; media-session state and persisted progression prove playback.
These checks do not measure audible output or PCM. Finally run the browser M17
boundary suite against the native core `results.json` for reverse transfer.

## M50 selectable streaming quality

`music_quality_e2e.py` requires an already signed-in disposable fixture, current
normal APK, rooted emulator and real QEMU WAV output. It never clears app data,
installs an APK or writes preferences. Root owns runtime setup and restoration.
Open Three Tones from the fixture's EQ Probe folder through ordinary native
controls first, and leave its full player paused. Then, from the repo root:

```sh
python3 -u player-android/test/e2e-live/music_quality_e2e.py FIXTURE \
  --profile aac128 --evidence EVIDENCE/native-aac128 \
  --emulator-wav QEMU_WAV
```

Repeat for `original`, `aac256`, `mp3192` and `opus96`. The runner chooses both
network defaults through touch, then uses actual Next/Previous to reopen the
existing queue. Its owned transparent proxy on18772 records sanitized actual
source/range/status data; only this fixture's reverse mapping changes and is
restored in `finally`. QEMU PCM, current title, settled Pause position, a real
midpoint seek to six seconds and subsequent output establish playback. The
live OS session's sparse position anchor is never treated as a running clock.

Add `--boundaries` to run light/dark/Back, actual reverse removal, failed profile
list with usable Auto, buffered offline output and reconnect/reload recovery.
Use a separate evidence directory per invocation and retain failed attempts.
The nominal encoder rate is a target; use actual ffprobe data when reporting
codec/container/bitrate. Cross-component results belong in
`docs/music-m50-implementation.md`.

With the owned genuine missing-encoder fixture, open Harbour with Opus selected
and wait for its actual conversion error. `--encoder-recovery --profile original`
(or `auto`) chooses through the error's Quality sheet, taps Retry, and verifies
the actual original response, PCM and a further audible midpoint seek. This
scenario requires the normal APK containing the recovery fix; the earlier
installed APK's absent Quality control is retained as failed evidence. Restore
the server's real encoder/cache configuration after this check, then restore the
original app endpoint/account/queue through ordinary UI and public APIs.

# M53 paired local/remote addresses

Use the normal installed APK, existing owned fixture/account and rooted emulator
without clearing data. Settings → Servers → Advanced connection settings pairs
an optional local URL with the selected primary profile. Put recording proxy
aliases in front of the same backend, preserving HEAD Content-Length and Range.
Inspect the credential-free identity probe before accepting account traffic.

The 2026-10-24 native acceptance used `adrv`, `app_ui`,
`music_loudness_e2e.Run` and `music_e2e.EmulatorAudio` for actual touch/Back,
read-only scoped state and real QEMU WAV output. The owned external harness,
sanitized traces, scenario windows, permission dialogs, source/output evidence
and cleanup snapshot are retained under `/home/paul/tmp/qj3-evidence`; no new
native runner clears or installs anything. See
[`music-m53-implementation.md`](../../../docs/music-m53-implementation.md)
for the platform matrix, exact browser invocation, endpoint failure/security,
SSID deny/approximate/precise/Location-off checks, canonical download/cache keys
and original queue/profile restoration. Browser controls retain their page
origin under U01; native pairing does not create a separate saved account.

# Podcast multi-client acceptance

`podcast_sync_e2e.py` uses two already installed, persistent Android clients and
the external isolated publisher/server fixture. It never installs an APK or
clears app data. The parent establishes each client's server and publisher
reverse mappings and supplies `--serial`, `--client A|B`, and one `--stage`.
Audio stages also need `--emulator-wav` pointing at that emulator's real QEMU
output. Each run writes a separate result and sanitized evidence directory.

After the main A/B completion and cleanup journey, run `held-account` on A
while signed into the primary account with Alpha subscribed and downloaded. The
fixture must include its separate `other` account. This stage holds a real
upstream scoped reply, signs out before release, checks the other account's UI
and persisted scope, then restores the primary account. Run `end-outage-next`
with Alpha and Beta subscribed: it downloads their short episodes directly,
forces podcast sync transport failure through a real HTTP proxy, verifies local
natural completion, and requires the next episode to play audibly. Neither
stage should be claimed as passing until run on the installed APK.

Run `download-negatives` on A while signed into the primary account with
Alpha and Beta subscribed. It uses the existing publisher reverse mapping and
an owned proxy to hold Beta Episode 003's real publisher response until native
Cancel is tapped. It then returns a real HTTP `503` for Alpha Episode 003,
requires visible Retry and durable failed state, and forwards the retry to the
publisher for byte verification. The proxy restores the original mapping and
does not alter publisher fixture fault files. Run `offline-seek` after the
successful `end-outage-next` stage has left a downloaded Beta Episode 002 on A;
pass `--emulator-wav`. It temporarily removes both owned reverse mappings,
seeks through the actual Android player slider, requires continuing QEMU audio
and durable local position, then restores those mappings. Each check needs an
installed-app run before it is counted as platform evidence.

# M36 personal music listening

`music_personal_counts_e2e.py` checks the real 85-second **Qualified Listening**
and 18-second **Skipped Listening** tracks in the disposable M36 fixture. It
reads personal song, album and artist counts through authenticated user/admin
APIs; opens the same album and players by touch; checks native secondary rows,
UTC qualification dates, media-session playback and distinct 440/660 Hz QEMU
output. The online phase starts with the fixture's `user` account signed into
an already installed APK, changes to `admin` and back through the app's normal
logout/login UI, and checks account-specific rows. The offline phase saves the
album through **Keep offline** if needed, temporarily removes only the fixture's
`adb reverse` route, listens to the downloaded 85-second track for 67 real
seconds, reads a consistent read-only copy of the app's SQLite main/WAL files,
force-stops the app, checks the pending UUID survived, restores the route,
relaunches/signs in, and checks one acknowledged server count. It restores the
route in `finally` even if a check fails. It does not install/clear the app,
start an emulator, stop the server, or edit fixture media or databases.

Run with the owned fixture already live on port 18936, the native app signed in
as that fixture's `user`, `adb reverse tcp:18936 tcp:18936` established, and a
running emulator launched with QEMU WAV output. Give each attempt its own
outside-repo evidence directory. Run phases independently to preserve useful
evidence after a failure:

```sh
python3 player-android/test/e2e-live/music_personal_counts_e2e.py \
  --fixture-dir /home/paul/tmp/codex/player-music-uupeffb1 \
  --evidence-dir /tmp/player-m36-native-online \
  --emulator-wav /tmp/player-task-batch-emulator.wav \
  --scenario online
python3 player-android/test/e2e-live/music_personal_counts_e2e.py \
  --fixture-dir /home/paul/tmp/codex/player-music-uupeffb1 \
  --evidence-dir /tmp/player-m36-native-offline \
  --emulator-wav /tmp/player-task-batch-emulator.wav \
  --scenario offline
```

Pass `--apk /path/to/installed.apk` to include its SHA-256 in `versions.json`.
The offline phase rewinds a saved long copy if a prior run left it near the
end. The runner records failures instead of treating an unexecuted phase as
native acceptance. Review each `android/evidence.jsonl`, screenshots and WAV
before marking M36 complete.

# M47 pinned music collections

Use the installed release APK, the single rooted API34 emulator with QEMU WAV
output, and a disposable music fixture. The M36 fixture above has an untouched
three-song Night Transit album; create a user-owned playlist containing those
same three song IDs to check overlapping collection pins. Use one emulator and
run browser workers separately. Do not clear app data between the online and
offline steps.

In the signed-in app, open Night Transit and the playlist by touch. Pin each
through **Pin offline…**, and check the visible `3 of 3` state against
`music_offline_pins`, complete `downloads` rows, and exact saved bytes. In
Settings → Playback cache, clear a populated disposable cache and check that
the kept files remain. Remove only the fixture's `adb reverse` mapping, open
Downloads → Saved music collections, and play a pinned collection; record its
real QEMU output. Force-stop and relaunch while still disconnected, choose
**Use files on this device**, then play the other pinned collection. Restore
the reverse mapping in `finally`.

Unpin the album and confirm its files and the overlapping playlist pin remain.
Use Downloads → **Remove download** on one shared song: its confirmation must
say the server file stays available, and the playlist must become partial with
**Play downloaded** unavailable. Reconnect, sign in, and use **Retry pinning**
to restore the missing original; then unpin the playlist and confirm files are
retained. Switching to another server account must disable pin/refresh for the
first account's saved snapshot. Review the dialog in light and dark mode and
check Back/Close. Keep screenshots, WAV, exact-byte state and versions outside
the repository; no media, APK, database or coverage artifacts belong in git.

# M49 offline music browsing

With one rooted emulator already running the installed app and QEMU WAV output,
run the focused native journey against a fresh disposable music fixture:

```sh
python3 -u player-android/test/e2e-live/music_offline_browse_e2e.py \
  /home/paul/tmp/codex/player-m49-18846 \
  --emulator-wav /path/to/emulator-output.wav \
  --evidence-dir /home/paul/tmp/codex/player-m49-native-evidence
```

The runner clears only Player app data at startup, connects to the fixture,
pins Night Transit and an overlapping playlist through native controls, then
removes only that fixture's `adb reverse` mapping. After a cold relaunch it
opens Downloads → Browse offline music, checks Songs, Albums, Artists,
Playlists, search, Back and light/dark layouts, and captures actual offline
First Light output. It restores the reverse route, verifies the browser stays
local until explicit reconnection, and signs back in. The previous reverse
mapping and system theme are restored in `finally`; server and emulator stay
running. Pass an unused evidence directory outside the repository.

# M48 automatic offline music

With one rooted API34 emulator, the installed x86_64 release APK, QEMU WAV
output, and an isolated `music_fixture.py up` server, establish `adb reverse`
for the fixture port and run:

```sh
python3 -u player-android/test/e2e-live/music_automatic_offline_e2e.py \
  /absolute/fixture --emulator-wav /absolute/emulator-output.wav \
  --evidence-dir /absolute/evidence --apk /absolute/app-x86_64-release.apk
# Run again with --scenario locked on the same fresh fixture to check the
# closed-engine scheduled job and catch-up after reopening the app.
```

The runner clears only Player app data, pins an album and overlapping playlist
through touch controls, enables automatic music, verifies the Android periodic
job, changes playlist and starred membership, and checks exact saved bytes. It
tests Mobile/Wi-Fi policy and global Retry, advances the disposable device clock
to invoke a due real JobService, then checks outage retention, removal, process
restart, and captured offline playlist audio. It restores the clock and fixture
reverse route. The locked scenario checks that a due OS job does not publish
membership with no active Flutter engine and that reopening the app catches up.
Keep its evidence and all media outside the repository.

# A03b rich lyrics

Add the rich lyric fixture with
`python3 player-android/integration_test/support/music_rich_lyrics_fixture.py
/absolute/fixture` from the repo root. Run one rooted API34 emulator with QEMU
WAV output, an installed release APK and the fixture port reversed, then:

```sh
python3 -u player-android/test/e2e-live/music_rich_lyrics_e2e.py \
  /absolute/fixture --emulator-wav /absolute/emulator-output.wav \
  --evidence-dir /absolute/evidence --apk /absolute/app-x86_64-release.apk
```

The focused runner clears only Player app data, signs in, opens the base album's
rich fixture, checks overlapping voice selection, actual word-timed audio,
translated and original labels, forward/back seek, loaded lyrics with the
fixture reverse route removed, light/dark presentation, line-only fallback and
Back. It restores the reverse route and system night mode. Keep evidence and
media outside the repository.
