# Existing file-library filters (F03 / uj3)

## Approved interaction contract

F03 extends the existing Sets grid, `SearchFilterBar` and `MediaFilter`.
Music's grouped search (M07 / 9k3) and future music metadata facets (M08 / vk3)
remain separate. The existing media API and tag editor retain their meaning.

Android's grid keeps its search, type chips, sort and favourites control. A
labeled **Filters** button opens a native, scrollable bottom sheet. Above the
grid, a wrapping summary always identifies the current scope: **Current set:
Music**, **Selected set: Restricted**, **Selected sets: Music, Restricted**, or
**All visible sets**. Active tag/range labels and **Clear filters** appear here.

```
Current set: Music                    [Filters]
Tags: keep, calm · Duration: 0–2 min   [Clear filters]
┌ Filters                              [Close] ┐
│ Sets: no selection includes all visible sets │
│ [✓] Music  [ ] Restricted                    │
│ Tags: matches every selected tag             │
│ [✓] keep   [✓] calm                          │
│ Duration (minutes)   Min [0]    Max [2]       │
│ File size (MiB)      Min [ ]    Max [10]      │
│ 1 MiB = 1,048,576 bytes; bounds inclusive     │
│ [Clear filters]       [Cancel] [Apply]        │
└──────────────────────────────────────────────┘
```

The sheet edits a draft. Apply validates and commits once; Cancel, Close,
outside dismissal and Back discard its changes. Clear in the sheet resets the
draft; Apply commits that reset. Clear above the grid immediately clears query,
type, favourites, tags and ranges, restores the current route's set, and keeps
sort. A pending debounced query cannot resurrect after a reset. Opening Filters
first commits pending search text so Apply preserves the user's latest query.
Controls use Material theme colours and typography, 48 dp targets, readable
wrapping labels and SafeArea/keyboard insets. At narrow widths fields stack;
the body scrolls while footer actions stay reachable. No custom gesture or key
is required. Back first dismisses the keyboard/sheet, then follows the route.

Sets initially select the route's current set. One or more checked sets select
their union, replacing the route scope. No checked sets means **All visible
sets**. Null model selection means the default current route, distinct from an
explicit empty list. Requests use either `set_id`, `set_ids`, or neither, never
both. Folder browsing explicitly names the route's set when the scope differs.

Tags are existing shared file tags, matched with AND. Editing them requires
owner access to the file’s set; viewers can filter and read them. Options load on opening
with progress and Retry on failure. **No tags yet** is an empty option state;
selected tags missing from refreshed options remain visible. Failed option
loads never silently clear the user's draft or broaden an applied filter.
Long set/tag names wrap. Existing owner-authorized detail tag add/remove stays available, and
returning to the grid refreshes results after edits.

Duration fields accept finite nonnegative decimal minutes, converted to seconds.
Size fields accept whole nonnegative MiB, converted to bytes. Blank omits a
bound; explicit zero is preserved. Minimum and maximum are inclusive; minimum
must not exceed maximum. Invalid input produces a field error and leaves the
sheet open without fetching or changing results. Server API semantics stay as
implemented, including unknown/null measurement handling. Existing web syntax
uses the same minutes and binary size multiplier; its size help says MiB.

Applied state survives detail/player push and Back and pull-to-refresh. Leaving
and reopening the Android grid or restarting the app starts with default current
set filters. This is local screen state, not saved search preferences. Existing
web URL/history/reload restoration stays intact. Loading/error/Retry and filtered
empty/Clear states retain the scope and summary; permission errors never switch
to another scope. Local files, Downloads, Music queues and podcasts are unchanged.

## Verification contract

Focused model/widget/grid tests cover value semantics, draft cancellation,
conversion, inclusive zero, invalid/reversed input, pending-search Clear,
single/multi/all scope, composite initial/pagination parameters and tag-edit
return. Root runs shared full gates and final server/APK builds after source
freeze; a test discovery run is not platform acceptance.

Actual installed Chrome, Firefox and native Android acceptance uses one marked
disposable fixture. Apply each existing filter alone, combine them, compare
visible IDs to authoritative expected IDs, and Clear. Include exact bounds,
zero, no matches, invalid input, tag add/remove, multi-set union, permission
filtering, long labels, light/dark/narrow layouts, keyboard/IME, Back and
refresh/reload persistence. Browser journeys preserve typing, selection and
modal focus and use existing syntax/tag/sidebar controls. Native journeys use
the visible sheet and touch/Back. Exercise real offline failure and Retry.

Actual acceptance is complete on all three platforms; results and retained
failed attempts are recorded below.

## Runners and fixture setup

After the combined final server build, create one new disposable fixture using
`music_fixture.py up` as for M07. Before either feature's acceptance, augment it:

```
python3 player-android/integration_test/support/music_filters.py "$PLAYER_MUSIC_FIXTURE"
```

The guarded helper requires the disposable marker, loopback origin and confined
media root, preserves existing IDs, and uses normal rescan/tag/favourite/ACL APIs.
It adds six uniquely named F03 files: real 60/120-second 400 Hz PCM WAVs padded
with valid RIFF JUNK chunks to exactly 1/2 MiB, plus a zero-duration 1 MiB JPEG.
Overlapping `F03 keep`/`F03 calm` tags, favourites, a viewer-visible **F03 Other**
set and an admin-only Restricted contribution distinguish AND, union and ACL
semantics. Existing Music files and embedded metadata remain intact. The helper
rejects existing F03 paths instead of silently toggling favourites on rerun.

From `player-server/test/e2e-web/`:

```
PLAYER_MUSIC_FIXTURE=/absolute/new/fixture npx --no-install playwright test \
  -c playwright.music.config.ts tests/music-filters.test.ts --workers=1
```

From `player-android/`, with the final APK installed on the running emulator:

```
python3 test/e2e-live/music_filters_e2e.py /absolute/new/fixture \
  --apk /absolute/final/player.apk --emulator-wav /absolute/emulator-output.wav
```

`--scenario journey`, `controls`, `scopes` or `errors` gives a bounded native continuation.
The offline case stops only the marked fixture's identity-checked process and
restarts it in `finally`, recording the new process identity in state.json.
The runner uses the existing native semantics driver and records visible result
names and hand-specified expected sets alongside authoritative server values.
The baseline teardown clears app data and removes its adb reverse; the shared
emulator and fixture remain available for the next runner. No startup command
or test discovery is evidence of actual platform acceptance.

Focused checks before root review: model, sheet, search bar and grid tests,
including invalid/overflow/reversed ranges, zero conversion, timer cancellation,
all-set distinction, defensive copies, identical pagination filters and detail
return refresh. Exact commands/results are retained externally under
`/home/paul/tmp/9k3-uj3-evidence/` and reported at source freeze.


Tag-edit acceptance keeps the normal fixture user a viewer. It first verifies
viewer mutation is rejected with HTTP 403 and leaves shared tags unchanged,
then temporarily grants owner access to **Music only** for actual visible
add/remove interactions. Each runner captures the original permission row and
restores its exact role in `finally` before continuing. Restricted remains
private, and favourites keep their existing per-account identity. This does
not alter product permission rules or grant batch-wide owner/admin access.

## Accepted batch verification (2026-10-22)

F03 and M07 share the combined gates, final artifacts, commands and versions in
[the M07 verification record](music-m07-implementation.md). Both expert review
rounds are complete; blocking empty-result oracle findings were fixed and
confirmed. Full Go build/vet/race/coverage, web unit, Flutter analysis and 1,424
Flutter tests, browser oracle and fixture-support suites passed. After native
runner fixes, all 49 native Python oracle tests and scoped pyflakes passed.
Errcheck has 256 unchanged existing findings and zero new findings.

One disposable fixture/server and one Android 14/API34 read-only emulator
session were reused. Server/app are 0.8.2/+22, native x86_64 APK code 4022;
`native-install.json` verifies its exact final APK hash. Evidence root:
`/home/paul/tmp/9k3-uj3-evidence/`.

| Platform | Accepted retained attempts |
|---|---|
| Chrome 144.0.7559.132 | `browser-attempt2-chrome`: filters/bounds/zero/union/sort/Clear/reload; `browser-attempt3-chrome-affected`: tag permissions/editing/focus/themes/offline |
| Firefox 156.0 | `browser-attempt5-firefox`: both complete F03 journeys |
| Android 14/API34 | `native-attempt5-f03`: query, individual/AND tags, inclusive/equal/zero duration and size, all-dimension/type/favourite combinations, invalid-draft Back, real offline options and Retry; `native-attempt8-f03-controls`: visible viewer denial, owner UI add/remove and Back refresh with role restoration; `native-attempt9-f03-scopes`: selected union/all-visible ACL, all types, all sorts, Clear and light/dark long-label sheet |

Each native command is `python3 test/e2e-live/music_filters_e2e.py
/home/paul/tmp/9k3-uj3-fixture --emulator-wav
/home/paul/tmp/9k3-uj3-evidence/emulator-output.wav`, with `--scenario controls`
or `--scenario scopes` for the recorded continuations. Browser commands and
exact per-attempt argument arrays are retained in `command.json` files. Completed
scenarios across retained attempts provide acceptance; earlier failed scenarios
are preserved and are not counted as passes.

Root reviewed Chrome/Firefox light 390/dark 1366 layouts and native light/dark
sheet screenshots. Long labels wrap, units and scopes remain readable, and the
scrollable sheet keeps Clear/Cancel/Apply reachable. The existing web artwork
badge contrast issue remains tracked separately as mm3.

Actual browser acceptance exposed and fixed Sets Clear routing and tag-dialog
selection/focus after filtered reload; regression tests and affected browser
journeys passed. Native runner fixes address empty input hints omitted by the
UIAutomator XML format, editable query text mistaken for results, IME reopening
after Back, short-lived permission Snackbar capture, and grid gestures starting
in a wrapping toolbar. Bounds select all four actual visible editable controls
in fixed visual order. Keyboard dismissal reads the IME window's visibility,
not its persistent input-view layout flag, and fails if visibility is unknown.
Grid swipes derive their region from actual preview-toolbar bounds. Negative
checks reject stale/loading/error empties, ambiguous result rows, unknown/stuck
keyboards and absent/occluded grid regions. No product/API/permission policy was
changed to make these oracles pass, and the final native APK remained unchanged.

The viewer was denied before any scoped owner grant; the owner context restored
its exact original role even on errors. Final fixture audit verifies unchanged
media IDs, original shared tags and Restricted isolation. App teardown clears
its data/reverse mapping; root stops only identity-checked owned runtimes and
preserves all fixture, media, build and evidence files. No release/version bump,
push or deployment is part of this batch.
