# Music end-to-end verification (V01 fixture and runners)

The shared harness for music features (task `oj3`, see
[`docs/music-candidate-map.md`](../../docs/music-candidate-map.md)): one
disposable local server with a synthetic tagged music library, and runners
for installed Google Chrome, Mozilla Firefox and the Android app on an
emulator. A passing run proves the harness and today's baseline; it does
not verify any future music feature. Each feature task extends the runners
with its own journey and keeps its feature unaccepted on any platform it
could not exercise.

Everything generated (media, database, credentials, APK, screenshots, audio
captures, results) lives in temporary directories outside the repository.

Network exposure: the runners reach the server over loopback, but Player has
no listen-address setting and binds every interface (`:PORT`). While a
fixture runs, its disposable accounts are reachable from the local network
(random passwords, synthetic media only). Run fixtures on trusted networks
or behind a host firewall.

## What the fixture provides

`player-android/integration_test/support/`:

| File | Role |
| --- | --- |
| `music_fixture.py` | `up` / `restart` / `status` / `down` of a detached server (default port 18766, all interfaces, see above) with its own media root and SQLite database |
| `music_library.py` | ffmpeg-generated library and `manifest.json` (expected titles, tones, levels, gains) |
| `service_doubles.py` | local ListenBrainz, Last.fm `artist.getinfo`, ICY radio and UPnP AVTransport doubles (port 18767) |
| `audio_trace.py` | analysis of captured output: segments (10 ms frames), sample-accurate gaps (runs of silence of 1 ms or more), pure-tone discontinuities (clicks), RMS/peak dBFS, tone frequency, Goertzel band levels |
| `evidence.py` | evidence record schema and `summary` (scenario × platform matrix; `*` marks results against service doubles) |
| `fixture_server.py` | server lifecycle shared with `all_formats_fixture.py` |
| `test_music_support.py` | unit tests; with `PLAYER_SERVER_BINARY` also the restart/teardown isolation test |

Library (all tones are sine waves, so the analyser can tell tracks apart):

- set `Music`: *Aurora Lane – Night Transit (2021)* (MP3 192k, MP3 320k with
  timed `.lrc`, FLAC 24-bit/96 kHz, `cover.jpg`, label/catalogue/original
  date tags, one artist `Aurora Lane feat. Kei`); two-disc compilation
  *Various Artists – Synth Compilation* (AAC, Opus, Vorbis with embedded
  lyrics and a disc subtitle); a Unicode album with a plain `.txt` lyric
  sidecar and a FLAC with two `ARTIST` values; a title/artist-only track; an
  untagged `Untagged/track_0042.mp3`; *Gain Check* (FLAC at −3 and −21 dBFS
  with opposite ReplayGain tags, an MP3 without gain tags); *EQ Probe* (100
  Hz + 1 kHz + 8 kHz); *Gapless Suite* in FLAC and MP3 (one 880 Hz tone cut
  into three 6 s tracks at exact sample boundaries); *Gap Calibration* (the
  880 Hz tone with exact silences of 5, 20 and 50 ms at 3.0 s, one per FLAC
  file, to calibrate gap detection on every platform); `Road Trip.m3u` with
  one unresolved entry. The M4A file carries iTunes atoms (`©nam`, `aART`,
  `trkn`, `disk`, `cpil`).
- set `Restricted`: one admin-only track.

Accounts: `music-admin` (admin) and `music-user` (viewer of `Music` only),
with random passwords in `auth.json` and `e2e.env` (mode 600).

Service doubles answer with `X-Player-Service-Double` and record every
request (`GET /_double/requests`). Evidence obtained against them must use
proof `service-double`: it shows what the client sent, never that a real
service or device works with Player.

## Runbook

Prerequisites: Go, ffmpeg/ffprobe, Python 3, Node with
`player-server/test/e2e-web` dependencies (`npm install`), installed Google
Chrome and Firefox, the Android SDK with an x86_64 AVD
(`Player_FDroid_Test_API34`) and Flutter. Check `free -m` first and run one
heavy step (build, browser suite, emulator) at a time.

```sh
export TMPDIR=$HOME/tmp/player-music     # optional: keep everything in one place
mkdir -p "$TMPDIR"
cd player-server && go build -o "$TMPDIR/player" ./cmd/player && cd ..

# 1. Fixture (prints state, including fixture_dir and evidence_dir)
python3 player-android/integration_test/support/music_fixture.py up "$TMPDIR/player" \
  | tee "$TMPDIR/fixture.json"
FIXTURE=$(python3 -c "import json,sys;print(json.load(open(sys.argv[1]))['fixture_dir'])" "$TMPDIR/fixture.json")

# 2. Chrome and Firefox (optional: route their sound to a null sink)
pactl load-module module-null-sink sink_name=player_e2e   # prints a module id
cd player-server/test/e2e-web
PLAYER_MUSIC_FIXTURE=$FIXTURE PLAYER_E2E_PULSE_SINK=player_e2e \
  npx playwright test -c playwright.music.config.ts
cd ../../..

# 3. Android: build, boot with the wav audio backend, run, stop
cd player-android
flutter build apk --release --split-per-abi --target-platform android-x64
git checkout -- android/gradle.properties   # only if the Flutter migrator rewrote it
QEMU_AUDIO_DRV=wav QEMU_WAV_PATH="$TMPDIR/emulator-output.wav" \
  emulator -avd Player_FDroid_Test_API34 -no-window -no-boot-anim \
  -no-snapshot-load -no-snapshot-save -read-only -gpu swiftshader_indirect &
adb wait-for-device
until [ "$(adb shell getprop sys.boot_completed | tr -d '\r')" = 1 ]; do sleep 3; done
adb shell cmd connectivity airplane-mode disable
adb shell svc wifi enable
adb shell cmd wifi status  # wait until connected to AndroidWifi
# With a responsive UI, raise volume; key events alone do not prove success.
for i in $(seq 15); do adb shell input keyevent KEYCODE_VOLUME_UP; done
adb shell dumpsys audio | sed -n '/^- STREAM_MUSIC:/,/^$/p'
# Require the active speaker Current index to equal Max (15 on this AVD).
# Repeat volume-up keys with pauses if necessary, then verify again.
python3 -u test/e2e-live/music_e2e.py "$FIXTURE" \
  --apk build/app/outputs/flutter-apk/app-x86_64-release.apk \
  --emulator-wav "$TMPDIR/emulator-output.wav"
adb emu kill
cd ..

# 4. Results and teardown
python3 player-android/integration_test/support/evidence.py summary \
  "$(python3 -c "import json,sys;print(json.load(open(sys.argv[1]))['evidence_dir'])" "$TMPDIR/fixture.json")"
python3 player-android/integration_test/support/music_fixture.py down "$FIXTURE"
pactl unload-module <module id from step 2>
```

On the API 34 verification AVD, a rapid key loop during recovery left media
volume at 5/15. Actual volume-up keys on the responsive UI eventually reached
15/15, verified using `dumpsys audio` and the read-only
`cmd media_session volume --stream 3 --get`. The corresponding `--set` command
appeared successful but AudioService rejected it through AppOps; it is not a
reliable setter on that AVD. Keep the before/after readback with audio evidence:
at low volume the quiet fixture can fall below the unchanged detector floor.

`down` stops the server and the doubles (only the processes the fixture
started, identified by PID, executable and process start time), refuses to
delete anything while the ports are still in use, then deletes the fixture
directory. The evidence directory (default
`<tmp>/player-music-evidence-<uid>/<run-id>`, mode 700) is kept.
`music_e2e.py` clears the app data and removes its `adb reverse` mapping at
the end, also when setup fails or it is stopped with SIGTERM; `--host-alias`
uses `http://10.0.2.2:<port>` instead of `adb reverse`. `restart` restarts
the server on the same database: the Android runner checks that a session
opened before survives and that the running app keeps working. Separately it
force-stops and relaunches the app: the saved server must appear in *Choose
a server*, selecting it must show a locked saved login with the remembered
username, and explicit password sign-in must open the server library. This
is the intended security contract in the Android README, including after a
cold device boot; the earlier expectation of automatic sign-in (`zn3`) was
a runner error. The native run checks the password path, not biometric
hardware. Provider tests verify that saved credentials remain disabled until
unlock; this native runner does not intercept network calls.

The browsers keep their default autoplay policies: every play starts from
real keyboard or pointer input, as for a user.

Python tests: `cd player-android/integration_test/support && python3 -m
unittest test_music_support` (add `PLAYER_SERVER_BINARY=$TMPDIR/player` for
the lifecycle test).

The native baseline requires an Android network reported as connected with
the `INTERNET` capability; its preflight checks `dumpsys connectivity`.
An airplane-mode AVD can stream through `adb reverse` while Android reports
offline, which intentionally keeps progress queued locally. The saved-state
check waits up to 15 seconds for authoritative server progress after Back.

## Audio output capture

- **Web:** `tests/helpers/audio-tap.ts` taps every Web Audio connection to
  the destination with an AudioWorklet, i.e. the signal after any gain or EQ
  node the page adds, recorded on the audio thread with the context's frame
  counter. Any discontinuity in the captured frames makes the capture
  invalid (recorded as a gap, never measured); an earlier ScriptProcessor
  tap lost whole blocks under load, which looked exactly like playback
  gaps. Media element events (`loadstart`, `playing`, `ended`) are stamped
  on the same clock, so a gap is located relative to the track change.
  Player routes audio through Web Audio while audio visuals are on (the
  default) and the page has user activation; with visuals Off nothing is
  captured and the helper reports `routed: false`, also a gap.
- **Android:** the emulator's PulseAudio backend did not start on the test
  laptop (`Failed to initialize PA context`), so the emulator runs with
  QEMU's wav backend and the runner reads byte ranges of that file: the real
  mixed output of the device at the current media volume (set it to
  maximum). The file only grows while Android has an output stream open, so
  time in a capture is stream time, not wall time; silence inside an open
  stream is captured. The runner allows up to 20 seconds for `PLAYING`, then
  captures the requested playback duration; buffering does not consume that
  duration. Its byte mark stays before Play so calibration captures the
  complete track. The runner calibrates it with the *Gap Calibration*
  tracks and the EQ probe: known gaps are measured to within 0.4 ms, but the
  stream starts and stops with a few ms of silence (2.6–8.6 ms seen; ignored
  within 50 ms of the stream's ends) and the emulator's output carries
  clicks inside a continuous tone, so click detection is recorded as a gap
  on Android. Android cannot change tracks automatically yet, so gapless
  across tracks is recorded as a gap there too.
- Neither path is "heard": no test listens to speakers. Absolute levels
  depend on volume settings; compare levels within one capture setup.

Earlier calibration baseline (fixture run `20261021T062222Z`, server 0.8.2
built from `6f2a69a` with uncommitted changes in the tree, Chrome
144.0.7559.132, Firefox 156.0, API 34 emulator with APK 0.8.2/4022):

| Measurement | Chrome | Firefox | Android |
| --- | --- | --- | --- |
| Loud − Quiet (expected 18 dB) | 18.00 dB | 18.00 dB | 17.99 dB |
| EQ probe band spread (flat) | 0.00 dB | 0.00 dB | 1.09 dB |
| Known 5 / 20 / 50 ms gaps | 4.60 / 19.56 / 49.81 ms | 4.58 / 19.54 / 49.81 ms | 4.76 / 19.89 / 49.95 ms |
| Gap at automatic change Movement I → II | 26.5 ms | 58.0 ms | gap: no automatic track change |
| Capture dropouts | 0 | 0 | n/a (stream file) |

These are today's numbers, not targets; M21 (gapless) must bring the
track-change gap to zero on every platform.

Native rerun after correcting the relaunch oracle (fixture
`20261021T073608Z`): emulator 33.1.24, API 34 x86_64, cold-booted with
`-no-snapshot-load -no-snapshot-save -read-only`. The existing APK was
0.8.2/4022 (SHA-256 `dfe343059a283f407944545fb5e4c8f4549ef117a5fa67230b9435f8b954d5fc`),
built during the earlier `6f2a69a`-dirty run; the existing server binary was
also 0.8.2 built from `6f2a69a`-dirty. Neither was rebuilt from the current
checkout for these runner-only corrections.

The latest result for each native scenario is **26 pass, 2 gap**. The full
run passed the chooser → locked saved login → explicit password sign-in
journey, restart, permissions, tone, gain (17.97 dB) and EQ. Its saved-state
check failed because the AVD started in airplane mode: streaming through
`adb reverse` worked while progress correctly stayed queued offline. Two
calibration captures were also disturbed. A single focused online recovery
with a connected Android network passed touch → playback → Back and saved
12 seconds on the server, measured 523.3 Hz for 7.01 seconds, recovered the
known gaps at 4.74/19.80/50.00 ms, and measured EQ spread 0.82 dB. No
threshold or edge exclusion changed. The two remaining gaps are emulator
click detection (21 extra discontinuities) and Android automatic track
advance; they do not verify future gapless playback.

Artifacts are under the external `oj3/relaunch-fix/` directory:
`native.log` and `full-evidence/` retain the full run's 23 pass/3 fail/2 gap;
`online-recovery.log` and `evidence/` retain its focused recovery;
`consolidated.json` includes every attempt record and the latest scenario
matrix. Earlier launcher-ANR and buffering failures are retained separately.
The full run's `cold-relaunch-server-chooser.png`,
`cold-relaunch-locked-login.png` and `cold-relaunch-signed-in.png` show the
intended security flow. TalkBack, biometric hardware and native network
interception remain unverified.

## Known runner limits

- Playwright cannot emulate touch in system Firefox over WebDriver BiDi;
  the Firefox run uses the mouse for the pointer journey and records the
  missing touch check as a gap. Its double-click also arrives as two single
  clicks, so cards are played with their ▶ button.
- Card action buttons sit inside `role="button"` cards and are therefore
  hidden from the accessibility tree; tests find them by `data-action`.
- `uiautomator dump` waits for an idle UI and can take seconds on the
  animated player screen; the Android runner reads playback state from
  `dumpsys media_session` instead of on-screen time labels.
- TalkBack itself is not driven; Android checks use the semantics labels
  that TalkBack would read.
- App relaunch after `am force-stop` requires choosing the remembered server
  and explicitly unlocking or signing in. Earlier runs incorrectly treated
  the intended lock screen as bug `zn3`; direct authenticated entry is not
  the expected behavior. Web browser sessions have a separate cookie policy.
- `adrv.adb` gives up after 120 s: after a host suspend, `uiautomator dump`
  hung forever on the old emulator; reboot the emulator in that case.

## M01 embedded tags

The same fixture now includes `Tag Verification/source_001.flac` with a title
unrelated to its filename, whitespace/malformed fields, long Unicode tags and
real repeated FLAC artists/genres. The baseline runners use the manifest's
human title for visible selectors and media-session labels, retaining media
IDs/filenames/tone output for source identity. The existing all-formats runner
still checks formats independently.

After building both components from the feature checkout, run the fixture as
above, then `npx playwright test -c playwright.music.config.ts` (includes
`music-metadata.test.ts`). For focused native M01 verification use
`python3 -u test/e2e-live/music_e2e.py "$FIXTURE" --metadata-only --apk <new-apk>
--emulator-wav <wav>`. Without `--metadata-only` the full baseline includes M01.
Each runner discloses complete metadata in existing Info/details, checks missing
fallbacks/Unicode/multivalues, and mutates only one known synthetic file through
`music_metadata.py`. Changed/removed/restored tags must appear after rescan with
unchanged media ID and saved progress. Fixture media and all evidence stay
outside the repository. The Python mutation helper refuses non-fixture roots.

M01 indexes server-managed audio. Standalone Android document-library files
retain their existing device title behavior; this slice does not claim a local
SAF tag index. New server downloads retain embedded metadata and their display
title/source context in the offline SQLite snapshot; older snapshots keep their
existing stored titles and continue to play. No shared version bump or release
is needed for this unreleased feature change.
