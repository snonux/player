# Appearance preferences (sj3 / M68)

Implementation stage; actual Chrome, Firefox and native Android acceptance is
still pending the integrated verification phase. U01 (`nj3`) and fixture/runners
(`oj3`) are the implemented prerequisites.

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

Stage: **IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION**. Both
bounded expert rounds completed. Controlled dropdowns now restore the saved
choice after a rejected write; the negative widget test checks all three
choices and the next successful save. Three native preference/widget tests,
scoped analysis and Node preference/contrast checks pass. The old artwork
harness fails identically on pre-appearance commit 0f489b7; that baseline gap
and native/window/visual acceptance remain for integrated verification. Full
suites and final builds remain deferred until backlog implementation is done.
