# Timed lyric seeking (F01 / tj3)

Implementation and two bounded review rounds are complete. Integrated local
Chrome, Firefox and native Android lyric journeys passed on 2026-10-24.
Prerequisites nj3 (U01) and oj3 (V01) are completed. Actual Cast receiver
transport remains unverified: no receiver is available.

## Layout and interaction

The existing library, public-share and detached audio stage remains the lyrics
surface. In desktop and narrow layouts, timed lines become full-width native
buttons, centered in the existing scroll region. Each row wraps long content,
has at least 44 CSS pixels of height, uses existing typography/theme tokens,
and shows hover and visible keyboard-focus treatment. Current-line accent and
bold text remain distinct from focus. Accessible names and hover titles say
“Seek to m:ss: [lyric]”; the region describes the action. Plain lines remain
paragraphs. No new dialog, global key or competing lyrics surface is introduced.

Annotated states and transitions:

```text
Audio stage                       Existing Lyrics toggle
┌───────────────────────────────┐  opens/closes the same region
│ Earlier line                  │  button: Tab + Enter / click / tap
│ Current line [accent + bold]  │  aria-current follows playback
│ Focused line [outline]        │  focus stays put as playback advances
└───────────────────────────────┘
```

Select a timed line → seek to its exact numeric timestamp, preserving paused
or playing state. Local source/metadata/error checks reject unavailable audio,
nonfinite timestamps and timestamps beyond duration. Active Cast uses the
existing remote seek transport. Retained old line nodes cannot seek a new
track. A parent window with playback detached cannot seek its stale local
element; use the detached player's lyrics instead.

Tab reaches native line buttons; Enter activates them. Space/p retain playback
behavior in library and detached playback. Public shares retain native button
Space activation (seek) and visible playback controls; no global p shortcut is
introduced there. Arrows/Page Up/Page Down/Home/End and j/k retain local reading scroll,
without changing library selection. Time updates change the highlight without
replacing rows or moving focus. Auto-follow pauses while the region contains
focus, or during wheel/touch reading. Leaving the region resumes following;
explicit line activation recenters the current line. Plain lyric scrolling
does not reset on leaving. Changing sources moves removed line focus to an
existing playback control.

R1 fix: highlight/centering reads the active transport position, including
Cast, and remote position ticks refresh lyrics without local audio events.
Selecting a Cast lyric updates from the receiver transport's seek position.

Loading, empty/unavailable, failed/offline lookup and unauthorized results keep
the existing uncluttered hidden lyrics behavior; playback remains available.
Plain/mixed-invalid timing never acquires seek actions. No persistence, network
lyrics provider or offline cache is added. CSS uses existing theme variables
for light/dark rendering and wraps long lines at narrow widths; visual checks
remain deferred.

Android's existing `LyricsPanel` is preserved: timed `ListTile.onTap` calls the
screen's `_seek`/handler path, plain rows have no `onTap`, and current-line and
retry behavior stay unchanged. No Android implementation edit was needed.

## Module checks and review

- `node --experimental-default-type=module player-server/web/js/tests/lyrics.test.js`
  passes: existing normalization/current-line/URL checks plus local seek guards,
  timestamp zero, fractional timing, paused-state preservation and failed setter.
  A minimal DOM/event boundary additionally exercises actual module rendering,
  remote position differing from paused local audio, receiver ticks, immediate
  highlight/recentering after remote seek, focused-row retention, wheel hold and
  resuming follow, old-node rejection after source change, and plain rows/scroll.
  This is module behavior coverage, not native browser activation proof.
- `node --experimental-default-type=module --check player-server/web/js/playback.js`
  passes.
- `git diff --check` passes.

Self-review: lyric rendering remains text-only; normalized timing gates native
buttons; request identity and panel membership gate stale actions; playback
owns local/Cast transport; reading and focus state stay in the lyrics module.
No extra abstraction or mutable shared transport state was introduced.

Two expert rounds completed. The first found Cast highlighting reading local
position; the fix injects the active position and refreshes on remote time
updates. Narrow final review confirmed the fix with no new blocking defect.
Targeted module checks also cover remote/local divergence, seeking, focus,
manual following, stale rows and plain lyrics.

## Integrated local acceptance, 2026-10-24

The existing real-fixture browser journey from commit `82afa83` was rerun with
upper seek bounds and a focused-row assertion, so a later time cannot satisfy
an earlier-line seek. From `player-server/test/e2e-web/`:

```sh
PLAYER_MUSIC_FIXTURE=/home/paul/tmp/player-integrated-verify \
PLAYER_E2E_PULSE_SINK=player_e2e_null \
npx --no-install playwright test -c playwright.music.config.ts \
  tests/music-lyrics.test.ts --workers=1 \
  --output=/home/paul/tmp/tj3-browser-current
```

Installed Google Chrome 144.0.7559.132 and Mozilla Firefox 156.0 both passed
(6.4s/10.2s). Each plays the real Long Road Home source at 390px width, uses
Enter on the focused 0:04 line, clicks 0:08, verifies bounded actual media time
and current highlight, then clicks back to 0:04 while offline with the same
source. Unicode plain lyrics remain text without seek buttons. Browser evidence
and screenshots remain in the existing fixture's evidence directory. This
journey verifies real playback time/source; it does not measure browser PCM.
Firefox uses the pointer alternative; system Firefox touch injection remains
the existing V01 limitation. Module tests cover manual-follow/stale-request,
invalid/missing/plain and Cast/local divergence boundaries separately.

Native acceptance used the running Player_FDroid_Test_API34 emulator,
Android 14/API34 x86_64, and the installed normal release 0.8.2/code4022 from
bd478ab (APK SHA256
`c783857aa3e8f4058ff1f80c703d170bd2ec36b13a28c48ac0d7d2e485ce7a7c`).
The owned normal server18771 uses c7b6aea-dirty; the existing18766 browser
server binary is463eb32-dirty with current checkout web assets. These are
local verification builds, not a release. The native harness uses existing
`adrv`, `app_ui`, `music_loudness_e2e.Run`, and `music_e2e.EmulatorAudio`:

```sh
PYTHONPATH=player-android/test/e2e-live python3 -u \
  /home/paul/tmp/tj3-native-evidence/acceptance.py
PYTHONPATH=player-android/test/e2e-live python3 -u \
  /home/paul/tmp/tj3-native-evidence/boundaries.py
```

The first harness verifies the native core, then its original strict offline
assertion fails as described below. `boundaries.py` finishes the distinct
offline, recovery and plain cases. Retained screenshots, fresh semantics,
versions, scripts, WAVs and exact observations are outside the repository at
`/home/paul/tmp/tj3-native-evidence`:

- Timed native taps while paused reach exactly 8000ms and 4000ms, preserving
  Pause and matching the selected row and visible Current heading. A tap to
  the later line during PLAYING settles at10250ms after2.2s; real QEMU PCM
  contains the fixture's523.3Hz tone. These are actual native touches, not
  widget-test assertions.
- With reverse18771 removed, the final warmed source seeks to4000ms and
  settles PAUSED **before reconnect**, with Every mile selected and displayed
  as Current. The fresh snapshot is `offline-buffering-retains-lyrics.json`
  (the filename predates the final settled result).
- Earlier offline attempts remained BUFFERING at their requested positions
  (4000ms or 8000ms), including a 15-second wait.
  They are unsuccessful seek completions, not a pass: a streaming seek can
  require another range request. Loaded lyrics remain visible. No downloaded
  playback guarantee is introduced by this feature.
- After reconnect, playback from the earlier line settles at6451ms and
  produces2.48s of523.3Hz PCM (`reconnected-earlier-playback.wav`). That audio
  was measured **online**, not while offline. Offline audible output was not
  measured. Readability and the final warmed offline seek are separate proof.
- Native Café Ñoño plain lyrics have no Tap to seek action. Tapping the actual
  nonclickable Unicode text preserves the exact paused position. The text
  stays readable offline; light and dark screenshots retain readable contrast.
- Back returns to Library. Both the owned18771 session and original18766
  session were restored through public queue CAS and native Continue where
  needed. The final18766 selected profile/admin Library has the exact original
  three occurrences/current index2/position4.69/repeat Off/shuffle false.
  Teal/Circle/Fit/visuals Off/System preferences and OS night yes are preserved;
  no audio plays. Only the original18766 adb reverse remains. No data clear,
  preference write, emulator restart, release, push or deployment was used.

The targeted actual-module lyrics check and `git diff --check` pass. Full
server/Flutter gates and the normal final APK build are shared with the
integrated batch; this acceptance changed only the browser oracle and notes.
Real Cast receiver execution remains unavailable. The existing two reviews
and module tests cover transport routing/highlight without claiming hardware
proof; no additional review round was opened.
