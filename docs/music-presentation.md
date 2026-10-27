# Appearance preferences (sj3 / M68)

Local browser and Android verification passed. U01 (`nj3`) and fixture/runners
(`oj3`) are the implemented prerequisites. Actual Cast receiver behavior remains
unverified because no compatible receiver is available; the user requested an
additional subagent review instead. No receiver acceptance is claimed.

Web header: **Settings** → standard focused dialog → **Appearance**. Native:
existing Settings → Appearance, for both device and server destinations. Existing
Dark/Light controls remain; web adds System, native retains System. Native
selects/dropdowns expose Blue/Teal/Plum, artwork Rounded/Square/Circle, and Fit
whole image/Fill frame. The artwork preferences affect the audio player's cover,
not video, image viewing, library thumbnails, or visualizer selection. Native
keeps its existing Fill frame default; web keeps Fit whole image.

Sketch: `Settings [Close] / Appearance / Theme / Accent / Player artwork shape /
Player artwork display / saved status`. Web rows wrap on narrow screens; native
dropdowns expand to available width. Existing modal Tab containment, Escape and
focus restoration apply. Native Back uses the existing Settings route. Every
choice persists on the device and applies without restarting audio; web storage
events also update same-origin detached/share windows. Unknown values use safe
defaults. Web storage denial keeps the window's choice and explains that it may
reset; native loading disables controls and persistence failures show a retry
message. Offline choices require no server or permissions; missing artwork keeps
its existing fallback. Long labels wrap in native switch subtitles and web rows.

Native additionally offers **Keep screen on while playing**, default off, with a
foreground/battery subtitle. Local ready+playing audio or an active playing Cast
session keeps the phone window awake while Player is resumed, including other
Player screens. Pause/end, disabling the option, background/inactive lifecycle,
and activity cleanup clear the flag. An Activity-gated View keep-screen-on request provides a second lifecycle gate
and preserves the video player's separate window flag. This never changes background audio or receiver display
behavior, and requests no permission or CPU wake lock. Video retains its existing
screen behavior.

Cheap checks: `node player-server/web/js/tests/themes.test.js` covers persisted,
system and cross-window changes, storage failure, and new accent normal-text
contrast ≥4.5:1 against all unchanged surfaces and filled-button text, including
hover/active colors. Native `test/providers/presentation_provider_test.dart`
covers restart persistence, separation from theme mode, malformed/unknown values
and safe keep-awake default. Formatting and JS syntax checks are also cheap.
These checks do not establish visual or native keep-awake acceptance.

Integrated acceptance must change every accent in both modes while a track plays,
verify saved choices after restart/offline, inspect artwork shapes/fit and missing
art, retain visualizers, inspect narrow/long-label/focus/touch/Back behavior in
installed Chrome, Firefox and the native app, and inspect the actual Android
effective window keep-awake state during local/Cast playing, pause/end, background/resume and preference
disabling. Native bridge compilation and runtime evidence remain required.

Implementation checks (2026-10-23): scoped `flutter analyze --no-pub` on
main/theme/presentation/settings/artwork/awake code and the new provider test:
no issues. `flutter test --no-pub test/providers/presentation_provider_test.dart
test/widgets/audio_cover_art_test.dart`: new preferences tests 2 passed; the
existing artwork widget file's 3 tests failed with missing
`connectivity_status` platform channel and painting-debug invariant errors.
Those harness errors remain a verification gap; this is not a passing artwork
regression run. No Kotlin/APK build, browser runtime or emulator checks ran.

Review round 1 fix: appearance dropdowns now use controlled `DropdownButton`
values inside labeled decorators. A rejected persistence write restores both
actual preferences and visible selections. New actual widget test rejects
Accent/Shape/Display writes via the SharedPreferences channel returning false,
checks all three visible defaults after each attempt, then verifies a successful
shape save uses the original accent/display. Scoped analysis (2 files) passed;
`flutter test --no-pub test/providers/presentation_provider_test.dart
test/widgets/presentation_settings_test.dart` passed all 3 tests. The artwork
harness failures above were independently reproduced by the parent at baseline
`0f489b7`; the retained baseline log is `/home/paul/tmp/sj3-artwork-baseline.log`.
They remain deferred to integrated verification, with no retry or acceptance claim.

Implementation checkpoint (2026-10-23): **AWAITING INTEGRATED VERIFICATION**. Both
bounded expert rounds completed. Controlled dropdowns now restore the saved
choice after a rejected write; the negative widget test checks all three
choices and the next successful save. Three native preference/widget tests,
scoped analysis and Node preference/contrast checks pass. The old artwork
harness fails identically on pre-appearance commit 0f489b7; that baseline gap
and native/window/visual acceptance remain for integrated verification. Full
suites and final builds remain deferred until backlog implementation is done.

Integrated verification, 2026-10-24:

- Installed Chrome and Firefox passed the committed
  `music-presentation.test.ts` journey: narrow Settings, actual playback,
  dark/light and all three accents, Off/missing-art fallback, shape/fit styles,
  Escape focus, restart and offline preferences. Evidence:
  `/home/paul/tmp/player-integrated-verify-evidence`.
- Native API34 x86_64 release 0.8.2/code4022 retained Teal/Circle/Fit/keep-awake
  after a cold restart. Actual local playing, Home while still playing, return
  and Pause changed the effective window flag to true/false/true/false.
  Evidence: `/home/paul/tmp/player-native-retry-evidence`.
- On the normal APK from source `e489731`, actual native touch changed
  Blue/Teal/Plum in light Settings during an isolated 180-second artwork source.
  Both playback state observations were PLAYING for every change; read-only
  preferences and player/Settings screenshots confirm saved selections. Evidence:
  `/home/paul/tmp/sj3-native-evidence/theme-matrix.json`. The original 18766
  queue was preserved by using the disposable 18771 admin account.
- Screenshot inspection found light-mode Sleep timer text inheriting light
  accent colors on the player's permanent black background. Actual Blue/Teal/
  Plum text contrast was 3.87/3.86/3.08:1. These screenshots are failed contrast
  evidence. Fix `bd478ab` applies the existing selected-accent dark theme only
  to the black player. Three production-screen regressions verify actual Sleep
  timer button contrast ≥4.5:1 for every accent while the surrounding app remains
  light. On the final installed APK, original screenshot pixels confirm Blue/
  Teal/Plum text contrast against black of 7.81/11.78/9.87:1.
- The user confirmed that no compatible Cast receiver is available and requested
  an additional subagent review. That review found a finished receiver status
  waiting for a progress network request before notifying listeners, leaving
  remote keep-awake state stale. The controller fix and regression checks do
  not establish actual receiver acceptance. Real Cast playing/pause/end,
  background/resume and preference-disable window behavior remain unverified.
- Source `bd478ab` passes full Flutter analysis and all 1685 tests with two test
  workers. The unchanged 2000 ms SVG timing assertion failed at 2172 ms in the
  earlier broader worker pool and passed in this final run; both logs are kept
  under `/home/paul/tmp/player-appearance-gates`. Normal pub-enabled release APK
  build passed and installed without clearing data: version 0.8.2/code4022,
  SHA256 `c783857aa3e8f4058ff1f80c703d170bd2ec36b13a28c48ac0d7d2e485ce7a7c`.
- Final native API34 x86_64 checks passed all six light/dark × accent changes
  while PLAYING and their saved state. A real 640×320 test-pattern cover verifies
  all six Rounded/Square/Circle × Fill/Fit combinations, changed through native
  controls during playback. Five actual visualizer modes displayed after the
  normal audio permission prompt; each has 3.35–4.43 seconds of captured audible
  QEMU output. Off returns to artwork. Evidence:
  `/home/paul/tmp/sj3-native-final-evidence/{theme-matrix-light.json,theme-matrix-dark.json,artwork-matrix.json,visuals.json,native-player-contrast.json}`.
- Final local window checks passed enabled foreground → disabled during playback
  → reenabled → Home → resumed → Pause: effective KEEP_SCREEN_ON was
  true/false/true/false/true/false. The playing session position is a sparse
  18314 ms anchor, not a running clock; the settled Pause is 23710 ms and real
  PCM was captured throughout. See `keep-awake.json` in the same directory.
- Additional installed Chrome 144.0.7559.132 and Firefox 156.0 checks passed
  actual non-square artwork, shape/fit rendering, all six theme/accent palettes
  and all five visualizer modes using the existing compact player/More controls.
  Both browsers supplied actual nonzero analyser samples (frequency peak246,
  waveform deviation17) during playback of the real local 440 Hz source.
  Evidence: `/home/paul/tmp/sj3-browser-evidence/{chrome.json,firefox.json}` and
  original screenshots. A steady tone may legitimately produce identical
  frequency-geometry frames; sample data, not frame noise, is the signal oracle.
- Cleanup restored the original System/Teal/Circle/Fit/keep-awake preferences,
  denied audio permission and sole 18766 adb reverse mapping. Public CAS and
  native Continue/Replace restored the isolated 18771 queue and 2.687-second
  offset. The original 18766 admin Library has playback stopped; server and
  read-only native queue snapshots still match occurrences `(3,3),(1,3),(2,5)`,
  current2/index2/4.69 seconds. See `restored-original-queue.json`,
  `restored-18771-state.json` and `restored-preferences.json` in the native
  evidence directory. No app data or saved login was cleared.

The external native harness is `/home/paul/tmp/sj3-native-acceptance.py`; it never
clears data, installs an APK or writes preferences. Early idle/selector attempts
are retained as failures and are excluded from passing scenario records.
Reproduction commands from the repository root (external evidence/media only):

```sh
M68_EVIDENCE=/home/paul/tmp/sj3-native-final-evidence python3 -u /home/paul/tmp/sj3-native-acceptance.py matrix
M68_EVIDENCE=/home/paul/tmp/sj3-native-final-evidence python3 -u /home/paul/tmp/sj3-native-acceptance.py artwork
M68_EVIDENCE=/home/paul/tmp/sj3-native-final-evidence python3 -u /home/paul/tmp/sj3-native-acceptance.py visuals
M68_EVIDENCE=/home/paul/tmp/sj3-native-final-evidence python3 -u /home/paul/tmp/sj3-native-acceptance.py awake
node /home/paul/tmp/sj3-browser-acceptance.cjs
```

The native runner requires the existing signed-in disposable fixture and its
paused source; `pattern` adds the owned 180-second real source and non-square art.
The browser harness is `/home/paul/tmp/sj3-browser-acceptance.cjs`. Failed external
attempts retain their logs: initial desktop options collapsed the floating stage,
so artwork checks use the normal compact player, and the first animation oracle
incorrectly required changing geometry for a steady tone. Original PNGs are
unaltered. A chat image viewer intermittently omitted white regions that remain
present and opaque in the original PNG pixels; this was not a product failure.
Actual Cast boundaries remain unverified (no compatible receiver). Local
keep-awake, appearance, artwork, visualizer, and Chrome/Firefox presentation
evidence above stand. Reconfirmed 2026-10-27 on API34: Dark + Plum + Circle +
Fit (`contain`) + keepScreenOn persist across force-stop
(`/home/paul/tmp/codex/player-m68-evidence/m68-native-remainder.json`).
