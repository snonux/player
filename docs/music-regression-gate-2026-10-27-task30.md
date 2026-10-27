# Regression gate after task 30

The next broad gate is due after task 40. Between gates, run focused checks for
the feature being accepted. This gate used one Android emulator at a time and
ran the browser projects serially. All disposable servers and emulators were
stopped; evidence remains under `/home/paul/tmp/codex/player-gate30-*`.

## Results

| Check | Result |
| --- | --- |
| Go race and coverage suite | Passed after the 3i3 server fix; no Go source changed between that run and this gate |
| Web unit suite (`mage webTest`) | Passed |
| Flutter analysis | Passed |
| Full Flutter widget suite | 1,751 passed; one SVG raster timing check took 2.23 s under concurrent browser load against a 2 s limit. The same case passed alone in 651 ms. |
| Chrome music suite | 84 passed, 35 fixture-specific skips, 5 failed, 1 did not run in the standard fixture. All five failures passed focused reruns with the required fixture data or longer shuffle sources. |
| Firefox music suite | 84 passed, 36 fixture-specific skips, 4 failed, 1 did not run in the standard fixture. All four failures passed focused reruns; the timed-out playlist case passed alone. |
| Internet radio (M61) | Real station, audio, permission, reconnect and keyboard journey passed in installed Chrome and Firefox. Native acceptance passed earlier in task 21. |
| Encoded shares (M65) | All three real public-share, edit and album cases passed in installed Chrome and Firefox. Native acceptance passed earlier in task 22. |
| Installed Android baseline | Login, seven metadata variants, rescan, touch playback with captured audio, Back, progress, themes, loudness, EQ, server restart, cold relaunch and account permissions passed. One touch-target assertion and three calibration assertions exposed issues fixed below. |
| Final installed Android checks | Rebuilt APK: sign-in, touch playback and captured audio passed; all 20 inspected targets passed, including 48 × 48 dp secondary skip controls. Revised calibration measured 4.69, 19.77 and 50.0 ms for the 5, 20 and 50 ms source gaps; EQ passed. |

The standard browser fixture omits specialized media and external service
scenarios. Its M08 and M19 failures came from missing release metadata and
multi-disc tracks. The M18 Chrome pause timing failures passed against the
longer shuffle tracks; Firefox had passed them in the standard fixture. The
Firefox M26 delayed-dialog timeout passed in isolation. These reruns used the
same installed browser binaries as the broad suites.

## Fixes during the gate

- The native baseline's wrong-password check now presses the visible Sign In
  button. IME Enter had only dismissed the keyboard; the corrected check saw
  the app's `invalid credentials` message on the installed APK.
- Audio capture pauses through Android's media control and verifies the media
  session stopped. A `uiautomator` lookup during active playback had failed to
  reach idle state and left audio running into later checks.
- Calibration now stops before its selected track ends. The previous window
  continued into M21 automatic next-track playback and counted that handoff
  alongside the intended 5/20/50 ms source gap. The obsolete baseline claim
  that Android cannot auto-advance was removed; M21 has its own acceptance.
- The secondary 15-second skip controls now have explicit 48 dp minimum bounds
  and sit fully above the fixed sync footer on the tested viewport. The native
  runner scrolls a partially visible control into view before judging its
  touch size. The final device check measured both skip controls at 48 × 48 dp
  and the speed choices at 48 dp high.

`python3 -m unittest test_music_e2e` passed all 15 checks after the runner
changes. The focused audio-player widget file passed all 59 tests after the UI
change, and scoped Flutter analysis was clean. The final release APK build
passed. The existing local `android/gradle.properties` change was preserved
byte-for-byte.

## Remaining verification limits

The emulator's QEMU output still has tone discontinuities outside the known
source gaps, so this capture path does not establish click-free Android audio.
M21's handoff to a separate compatible remote receiver remains unverified;
no receiver was available. Specialized browser fixture cases and every Android
feature driver were not all repeated at this gate; the relevant features from
tasks 21–30 retain their focused Chrome, Firefox and native acceptance records.

No version bump, release, deployment or push was performed.
