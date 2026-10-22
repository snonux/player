# M18: Playback shuffle

Task zk3, based on d930594; versions remain 0.8.2 / 0.8.2+22.
Root approved this interaction/state design and optional-input addendum before
feature code. Product implementation, both permitted reviews, final full gates
and actual installed Chrome/Firefox and native API34 acceptance are complete,
including positive-offset recovery and real permission boundaries.
Earlier freeze notes below are historical; the final evidence index records current status.

## Annotated screens and interactions

The existing Now Playing panel/sheet adds a wrapping toolbar:

```
Now Playing                                      [Close]
Playing from: collection name
[Shuffle: Off/On] [Reshuffle remaining] [Repeat: Off/All/One]
Only upcoming songs change. Library order stays the same.
[current and existing queue rows]
[Clear queue] [Undo]
```

Shuffle is selected with aria-pressed/native selected semantics and theme accent.
On permutes upcoming occurrences only, retaining history/current occurrence,
source and position. Explicit Play-next FIFO remains first and is never shuffled.
Off restores canonical relative order of the remaining occurrences, without
replaying consumed songs. Reshuffle is explicit, appears while On, and has Undo.
Fewer than two eligible upcoming occurrences disables enable/reshuffle (with
accessible explanation); switching On to Off remains available. Toggle and
reshuffle use existing one-step Undo, restoring ordering and shuffle state.
Repeat All walks the concrete permutation once each cycle, keeping it on wrap;
Repeat One retains its exact occurrence. Previous/jump retain existing semantics.

Collection detail and selection toolbars reuse existing controls:

```
[Play album/artist/playlist/selected] [Shuffle] [Queue actions ▾]
                                               Play next
                                               Add to queue
                                               Shuffle options…
```

Direct Shuffle resolves the whole collection/batch atomically, replaces queue,
shuffles all occurrences and plays the first. Canonical order is captured BEFORE
permutation. Normal collection Play replaces in ordered/off mode. Enqueue keeps
current shuffle mode and explicit requested placement. Manual move clears FIFO
markers as today and establishes current order as the new canonical baseline.
Insert/remove update canonical IDs; Undo restores prior baseline and mode.
Duplicate media remain separate occurrence IDs throughout.

Shuffle options opens a themed web modal/native scrollable AlertDialog:

```
Shuffle options
Genre (optional)             [                      ]
From year (optional)         [                      ]
To year (optional)           [                      ]
Maximum songs (optional)     [                      ]
Blank includes every song. Applies to this collection only.
[inline validation/loading/error/no-match feedback]
[Cancel]                                      [Shuffle]
```

Genre matches any embedded genre exactly, trimmed and case-insensitively.
Year bounds are inclusive integers 1..9999, either may be blank; From <= To.
Maximum is integer 1..10000, blank unlimited. Missing metadata does not match a
supplied constraint. Resolve complete collection, filter eligible occurrences,
uniformly shuffle them and select at most maximum occurrences; retain the
chosen subset's original relative order as canonical before playback shuffle.
Limit counts occurrences, including duplicates. No existing queue entries are
filtered away. Invalid input stays in dialog; no matches/failure preserve queue
and inputs for retry. Cancel/Escape/Back invalidates pending submission so late
resolution cannot replace playback. Existing account/view/source/busy guards
remain authoritative. No new API, genre discovery, radio or album-group shuffle.

Web r/header shuffle keeps existing global library-list randomization everywhere;
help/title explicitly say library listing. Playback shuffle has no new letter
shortcut: existing Tab/Enter and control mnemonics reach buttons. Q/Y, vi keys,
search typing, Escape hierarchy and pointer alternatives remain. Toolbar focus
survives queue rerenders; dialogs return focus to their opener. Native controls
use labeled touch targets >=48dp, theme colors and selected semantics. Back
closes a menu/dialog/sheet predictably, preserving queue. Long names wrap or
ellipsize using existing layouts; no new typography/color system. Capture dark,
light and narrow/portrait originals for review.

## Ownership, recovery and boundaries

Persist shuffle bool and canonical occurrence IDs in existing local account+
origin queue snapshot, never URLs/auth. Legacy snapshots default ordered/off.
Validate canonical IDs as an exact occurrence permutation; after permission
revalidation drop unavailable IDs from both representations. Restore paused,
with same order/current/position and mode; account/server scope clears retained
shuffle state. Queue edits/shuffle are local and need no network; collection
resolution offline/forbidden/empty outcomes retain existing queue and expose
loading/error/no-match feedback for retry. Ordinary media/podcasts/Sets source
ownership, metadata/actions, casting and repeat behavior remain unchanged.

## Verification plan

Targeted production-controller and pure-algorithm tests cover occurrence
permutations, no lost/duplicated entries, pinned FIFO, Off after advance/insert/
remove, manual moves, Undo, current/source/position stability, repeat cycles,
legacy/corrupt/scoped persistence and unavailable recovery. Optional filters
cover boundaries/invalid/missing metadata/duplicates/uniform sampling seam;
collection tests cover no-match, resolver error, cancellation and supersession.
RNG injection belongs only at a pure algorithm seam. Actual UI tests use normal
production randomness and assert multisets/current state, never that every draw
must differ.

Actual installed Chrome and Firefox plus native API34 emulator must shuffle a
queue remainder and collection, browse independently, toggle/reshuffle/Undo,
walk one complete cycle with authoritative occurrence IDs and actual source
PCM, exercise optional-input failures/results, restore/account/offline/retry,
keyboard typing/focus and native touch/Back, dark/light/narrow layouts. Artifacts
and exact versions/commands stay outside git under fresh zk3 paths (port18813).
Root owns full gates/builds, max two fresh reviews, runtime startup authorization,
staging/commit/task completion and stop-only cleanup; worker targeted checks only.

## Historical targeted freeze and self-review

Product implementation was frozen before root's first fresh review. Pure shuffle
and option algorithms are separate from queue ownership and UI. Controllers
retain canonical occurrence order before permutation; appended IDs go to the
canonical tail, while explicit next inserts relative to its requested anchor.
Off sorts only the remaining non-pinned occurrences; consumed prefix/current
and explicit Play-next FIFO stay fixed. New modules are included in service
worker precache. No Go/API, versions, dependencies or unrelated inputs changed.

Scoped Flutter checks passed all 55 tests in `music_shuffle_test.dart`,
`music_queue_recovery_test.dart`, `music_sequence_test.dart`,
`music_collection_actions_test.dart`, and `music_queue_sheet_test.dart` with
`flutter test --no-pub --concurrency=1`. The actual dialog tests cover invalid
count, delayed submit under a non-current parent route, Cancel/Back rejecting
late results, and real no-match preserving queue+inputs before successful retry.
Provider/controller tests cover shuffle recovery/drop-unavailable/account scope,
current-source/position stability, repeated media occurrences, pinned FIFO,
canonical append, consumed prefix, removal/Clear Undo and manual moves.

Node production playback-source, queue-storage, queue-shuffle and collection-
resolver tests pass; service worker syntax and `git diff --check` pass.
The playback-source suite intentionally logs its expected failed Cast-start
fixture while passing. Scoped Flutter analysis covers all changed Dart product
and test paths. Logs are external under `/home/paul/tmp/zk3-evidence/`.

Self-review checked every snapshot construction/restoration and queue replacement
path, asynchronous scope/stop/Undo captures, collection cancellation guards,
menu/typing/focus interactions and normal ordered playback preservation.
Earlier targeted failures are retained: an edit syntax error, an invalid test
cleanup method, explicit null-canonical handling (fixed), and a lazy-list menu
test touching a clipped row (now ensures visibility). No known product defect
remained from targeted checks. At that initial freeze, runtime checks had not
run; their completed results and final artifacts are indexed below.

## Two-round review corrections

The first fresh review found one blocker: Shuffle options Escape used the generic
modal close path, which selected a library card before the shared focus observer
could return to Queue actions. The scoped dialog now handles unmodified Escape
through its own close function. A production-menu/form/keyboard regression failed
before with only the wrong-opener assertion, and passed after. Delayed resolver
completion after Escape leaves queue occurrence IDs unchanged.

The second/final review found the pending-submit variant: Queue actions was
disabled during resolution, so real browsers ignored focus() while the original
fake focus method incorrectly accepted it. The fake now honors disabled; its
meaningful regression failed before. Cancellation immediately invalidates that
owner generation, releases its busy controls, then focuses the enabled opener.
A late canceled finally may only change controls/status if its captured generation
still owns them. The test starts a newer real action before resolving the canceled
request, verifies the old finally cannot unlock it or erase its loading state,
and verifies only the new result changes playback. Both pending Escape and Cancel
have exact installed-browser oracles in the runtime driver.

Root self-review caught one follow-on from that generation guard: existing
catalog selection Cancel incremented generation without releasing busy. Its scoped
cancel path now releases busy before updating selection controls. A real catalog
binding/checkbox/selection-Cancel/new-Play regression failed before (one resolver
request instead of two), and passes after; releasing the old result first keeps
the newer transaction's controls owned. Root independently reran the production
suite and accepted the bounded corrections. Both permitted fresh reviews are used;
there was no third round. Root subsequently completed full gates, normal builds
and actual three-platform verification as indexed below.

Final runners are `music-shuffle.test.ts` (five scenarios for each installed
browser) and `music_shuffle_e2e.py` (core/options/recovery/permission, plus bounded
continuation flags). Four native fresh-screen/hint oracle tests pass, rejecting
failed-idle dumps and empty accessibility trees without using old screens.
Driver parse/list/unit checks are not feature acceptance. Failed attempts,
versions, original screenshots, saved queue state and actual PCM are retained
per platform under the evidence paths below.

The M18 fixture now has an optional `--shuffle-playback` flag: all three Night
Transit encoder specifications use 45 seconds before source generation, and the
manifest derives from those same specs. Defaults remain 12 seconds. Two cheap
helper tests pass, including checking the ffmpeg tone input really contains
`d=45`; those unit checks do not replace actual ffprobe. Native preflight compares
all three physical durations, indexed server durations and manifest, requiring
45-second agreement before playback. Root authorized fresh generation and
verified actual files before runtime. Prior qk3 fixtures/evidence remain untouched.

The first full Flutter gate exposed an older album-screen regression test's
stale generic popup finder after the collection menu gained options. That test
now identifies the actual String popup by its Queue actions tooltip, retaining
its real disabled/single-submission/Back/late-result/retry assertions. This is a
test-only compatibility correction; the targeted six tests and final full rerun
passed under root coordination.


## Final artifacts and acceptance index

Root's final serialized gates passed: Flutter analyzer zero issues (2.48s), all
1,410 Flutter tests (206.57s), Go build/vet and required-FFmpeg race/coverage
(249.9s), plus the web suites (5.39s). Errcheck remains its existing 256-finding
baseline with zero new findings. The first Flutter run's stale popup-finder
failure remains preserved as attempt1; the corrected screen's six tests and
full rerun passed. No release/version change was made.

Normal final server: `/home/paul/tmp/zk3-evidence/player-final`. Normal final
release APK: `/home/paul/tmp/zk3-evidence/app-x86_64-release.apk`, version
0.8.2/code4022, SHA256
`26e3f2d722ecd4b9216e120fe9f989705301266efd4e8ed5bf679abc13b8de32`.
The independently pulled installed APK has the same hash and version.
Fresh fixture `/home/paul/tmp/zk3-fixture` uses port18813; actual ffprobe,
server and manifest agree on all three 45-second Night Transit sources
(`duration-preflight.json`). Prior qk3 fixtures/evidence were untouched.

Installed Chrome144.0.7559.132 and Firefox156 ran serially, headful, one worker,
zero retries with production randomness. Chrome recovery passed attempt1,
core/options attempt2, and settled theme evidence attempt4. Firefox all three
functional scenarios passed attempt3 and settled themes attempt5. Full audible
six-occurrence cycles, exact source/current/position and canonical state,
queue-vs-browse separation, invalid/no-match/offline filtered retry, pending
Escape/Cancel late-result rejection and exact opener focus, account/reload
recovery and real narrow/wide light/dark controls passed. Accepted Cancel
contrast is 18.522:1 light and 12.991:1 dark. Attempt1 navigation failures and
early direct-theme/mislabeled screenshots are retained but excluded from theme
acceptance. Browser ownership audit confirms all owned browser processes exited.

Native API34 final-APK accepted phases: attempt7 queue remainder,
reshuffle/Undo, canonical Off/current/position and independent album browse;
attempt10 full audible six-occurrence collection cycle including duplicates;
attempt13 invalid/no-match/all four inputs, actual offline retry, filtered
source PCM, touch Cancel and native Back; attempt14 paused force-stop/account
recovery at the exact positive 3.219-second position, then actual Resume with
8.03s of matching 440Hz output; attempt15 real Music-grant revoke/restore.
Root independently matched all six cycle WAVs (7.29–7.36s) and exact IDs.
Permission denial returns the contract's 404 for the hidden album; both new
direct Shuffle and options display an error and preserve the exact queue,
Long Road Home source and paused 3.721-second offset. The exact prior viewer
grant was restored and verified in finally; retry produced the three album
occurrences and matching actual source PCM. Chrome permission-only attempt6
passed (8.1s overall); Firefox attempt7 passed (12.2s overall). Both actual
production resolver responses were [404,404,200], with exact original viewer
grant restored, retained paused source/time and queue, then matching 1.1s PCM
(440Hz Chrome, 523.2Hz Firefox). `browser-process-stop-audit-final.json`
confirms all seven browser attempts' owned PID/start/executable identities absent.

Attempts6–15 retain driver corrections and originals. Native
`uiautomator dump` omits EditText hints; a fresh actual AccessibilityNodeInfo
probe verified all four correct `getHintText()` values, with exact editable,
visible, enabled label/bounds matching for field actions. The probe waits up to
seven seconds for a fresh nonempty editable tree and fails closed on exhaustion.
Actual successful probe poll counts were [2,1,1,2,2,1], confirming transient
null-root recovery. Four scoped driver-oracle tests pass (latest 0.015s),
including stale-dump and empty-tree rejection; Python compile passed. This is
test tooling, with no product semantics change. Attempt13's recovery expected
an empty persisted admin document, but a never-used account correctly has no
document; the corrected case validates actual empty/Off/no-Resume UI and absent
or empty scoped storage. Attempt14 strengthens its earlier zero-offset check.

Synthetic media Play while paused did not activate the native session in an
earlier attempt; accepted playback uses actual visible Play/Next controls and
working scoped media Pause for stabilization. No new OS transport acceptance,
physical device or operated TalkBack claim is made. The production widget
regression covers controlled pending native Cancel/Back; real native touch/Back
exercises the dialog. Root completed verified stop-only cleanup, recorded in
`/home/paul/tmp/zk3-evidence/stop-only-results.json`: the exact owned emulator
and final server processes are absent, port18813 is free, only the verified
`player_zk3` Pulse module/sink was unloaded, shared Pulse remains responsive,
and adb has no devices. All fixtures, media, databases, authentication state,
APK and evidence remain preserved. Root independently verified the restored
viewer grant from read-only SQLite after shutdown, plus final browser source
FFT (439.904Hz Chrome / 522.837Hz Firefox) and resolver [404,404,200]. The final
six Python helper/oracle tests passed. All task files are frozen for root's
explicit staging, local commit and task completion.
