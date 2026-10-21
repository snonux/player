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
