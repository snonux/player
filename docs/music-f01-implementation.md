# Timed lyric seeking (F01 / tj3)

Implementation stage: IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION.
Prerequisites nj3 (U01) and oj3 (V01) are completed. This slice follows the
2026-10-22 implementation-first workflow; it does not claim platform acceptance.

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

## Cheap checks and verification gaps

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

Deferred integrated checks: installed Chrome and Firefox keyboard/pointer/touch
journeys with actual media time and highlight, focus/selection/scroll retention,
light/dark/narrow rendering, plain/missing/failure/offline/source-change cases,
native Android timed tap regression, and applicable Cast-device transport.
No browser/emulator run, full suite, final server/APK build or new acceptance
runner was performed for this implementation slice. Existing lyric tests and
V01 fixtures/runners remain available for that phase.

Two expert rounds completed. The first found Cast highlighting reading local
position; the fix injects the active position and refreshes on remote time
updates. Narrow final review confirmed the fix with no new blocking defect.
Targeted module checks also cover remote/local divergence, seeking, focus,
manual following, stale rows and plain lyrics. Platform acceptance remains
deferred until backlog implementation is complete.
