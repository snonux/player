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
  -no-snapshot-save -read-only -gpu swiftshader_indirect &
adb wait-for-device
until [ "$(adb shell getprop sys.boot_completed | tr -d '\r')" = 1 ]; do sleep 3; done
for i in $(seq 15); do adb shell input keyevent KEYCODE_VOLUME_UP; done   # media volume to max
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
force-stops and relaunches the app, which should reopen signed in; on a
freshly booted emulator it does not (open bug `zn3`), and that check fails
until the bug is fixed.

The browsers keep their default autoplay policies: every play starts from
real keyboard or pointer input, as for a user.

Python tests: `cd player-android/integration_test/support && python3 -m
unittest test_music_support` (add `PLAYER_SERVER_BINARY=$TMPDIR/player` for
the lifecycle test).

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
  stream is captured. The runner calibrates it with the *Gap Calibration*
  tracks and the EQ probe: known gaps are measured to within 0.4 ms, but the
  stream starts and stops with a few ms of silence (2.6–8.6 ms seen; ignored
  within 50 ms of the stream's ends) and the emulator's output carries
  clicks inside a continuous tone, so click detection is recorded as a gap
  on Android. Android cannot change tracks automatically yet, so gapless
  across tracks is recorded as a gap there too.
- Neither path is "heard": no test listens to speakers. Absolute levels
  depend on volume settings; compare levels within one capture setup.

Baseline of the final run (fixture run `20261021T062222Z`, server 0.8.2
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
- App relaunch after `am force-stop` behaved differently on two boots of
  the same AVD: one long-running emulator reopened straight into the
  Library, a freshly booted one showed *Choose a server* and then *Welcome
  back* (password or fingerprint) even without a server restart. This is
  bug `zn3`; the runner records the cold-relaunch check as failed and signs
  in again so the remaining steps run.
- `adrv.adb` gives up after 120 s: after a host suspend, `uiautomator dump`
  hung forever on the old emulator; reboot the emulator in that case.
