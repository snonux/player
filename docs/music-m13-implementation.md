# Music M13 visible editable Now Playing queue

Design proposal before feature code (task ck3, 2026-10-21).

Scope is the existing Music album snapshot created by M02. Sets remains a live
browse sequence with M12 wrap/resume; opening Queue in Sets shows a retained album queue, or explains that playing
an album creates an editable queue when none exists. No insertion, persistence, shuffle, repeat,
playlist save, or new server API belongs to this task.

Web: a persistent Queue (Q) button beside existing navigation/player controls
opens a right docked panel without blocking library browsing on wide screens.
Below 700px it is a full-screen modal using the existing overlay and focus
rules. Header: Now Playing, source album, Close. List: numbered two-line title /
artist rows, visible Now label and aria-current on the active entry. Rows have
Play, Move up, Move down, Remove controls; no drag-only action. Footer: Clear,
Undo last edit, status. Empty: “Queue is empty. Play an album to start a queue.”
Long titles wrap; a 200-row window with Previous/Next page and Jump to now keeps
1000+ entries reachable without creating thousands of controls. Opening focuses
Now; j/k or arrows select, Enter jumps, K/J move, Delete removes. Q/Escape close
and restore the opener. Native button Enter/Space and text controls keep native
behavior. Moving/removing retains entry identity and focus at the edited place.

Android: a reusable Queue icon button in the player and library navigation opens
a full-height scrollable sheet. Handler-owned album entries remain available
after Back. Lazy rows show Now, title and position; tap jumps; labeled overflow
provides Move up, Move down and Remove with 48dp targets and custom semantic
actions. Clear and Undo remain visible. Back closes only the sheet. Standard
Material colors/text follow the current theme, including narrow/large-text use.

State transitions: reordering preserves the active entry and audio position,
including duplicates. Removing another entry does not reload the source.
Removing the current entry pauses/stops its progress session and selects the
next surviving entry (previous if last); playback requires explicit Play/jump.
Clear stops the queue source, flushes progress under the old media ID, empties
entries and leaves library browsing intact. Undo restores the previous order
and selection, without restarting or replacing any source automatically; an
explicit jump/Play resumes the restored queue. New playback invalidates Undo.
Edits are disabled during source transitions; asynchronous resume/source work
must never resurrect a cleared/removed entry. Failed jumps retain visible entry
and existing Retry; offline edits need no network, playing still reports source
errors. Starting Sets or another source in the same account retains inactive queue
contents while invalidating active ownership and Undo. Explicit queue jump
reclaims playback with a fresh native session; Sets Next stays separate. Account
or server changes clear retained contents and callbacks. Entry IDs, not media IDs,
identify edited occurrences.

Verification planned: targeted sequence/UI regressions, then root full gates and
fresh review before disposable actual Chrome, Firefox and native Android runs.
Actual acceptance must exercise source tones/order, browse elsewhere, jump,
move, remove including current, clear/Undo, focused keys/native typing, touch /
Back, denied/missing source and offline boundaries, plus light/dark/narrow UI.
At this pre-code proposal checkpoint no platform acceptance had run.

## Implementation and review checkpoint

Web `queue.js` owns album occurrence identities and active/inactive state;
`queue-panel.js` renders that owner. Sets navigation shares only an asynchronous
source-intent counter so an older resume read cannot replace a newer album or
queue jump. Browsing and editing inactive contents do not cancel another
owner's pending Sets play. Queue buttons retain native Enter/Space on desktop
and narrow layouts; row-local actions precede generic button Escape blur.
Detached Q focuses the main queue. Removing/Clearing an active popup source
closes it without handing the stopped source back; its actual ID/position is
flushed. Cast stop uses the existing receiver-session stop. These ownership
unit checks do not claim actual receiver acceptance or add M60 sequencing.

Android retains album contents and loader separately from the active sequence.
Explicit resume obtains a new coordinator lease and the visible player follows
that lease, current title and error/retry. Queue operation revisions prevent a
delayed remove/Clear from restoring old-account Undo or editing a replacement
album after scope/new-album changes; a stale stop cannot stop the replacement
source. Account/server/local-library changes clear in-memory callbacks.

At the initial implementation checkpoint, Node playback/source and keyboard
tests passed. Four files of Flutter sequence, queue-sheet, album and player
checks passed 65 tests, with zero analyzer issues. Review round 1 found and fixed deferred Sets/queue source ordering,
desktop queue Space handling, and native scope/edit async races. The source and
keyboard regressions fail before and pass after, as do four native delayed
progress cases (Clear/current remove × scope clear/new album). A separate
negative case proves inactive Clear/Undo leaves pending Sets resume intact.
Root subsequently completed both allowed fresh reviews with no remaining blockers. Evidence logs are under
`/home/paul/tmp/ck3-evidence`; this checkpoint preceded the actual platform runs recorded below.

Actual runners: `music-queue.test.ts` has five Chrome/Firefox journeys, and
`music_queue_e2e.py` has eight native scenarios, with a focused `remaining`
selection for the final UI-only APK. The marked disposable fixture
scale helper adds a real 1001-entry album without doubles. Component E2E READMEs
record exact invocation and distinguish discovery/units from platform proof.

## Actual browser acceptance

Installed Google Chrome 144.0.7559.132 and Mozilla Firefox 156 passed all five
scenarios per browser against the owned no-double fixture on port 18798:
editing/focus/typing/themes, edited Next button and natural A→C→B with finished
IDs, retained inactive ownership plus real ACL retry/offline edits, 1001 real
entries and last-ID/tone, and narrow light/dark long-title queue captures.

Commands from `player-server/test/e2e-web` used
`PLAYER_MUSIC_FIXTURE=/home/paul/tmp/ck3-fixture PLAYER_QUEUE_SCALE=1
PLAYER_E2E_PULSE_SINK=player_ck3 npx playwright test -c
playwright.music.config.ts music-queue.test.ts` and external `--output` paths.
Attempt 1: 5 passed, 3 failed (2.6 min); the retained-source runner clicked a
minimized player's hidden Play control on both browsers, and Firefox's full-page
86171px screenshot exceeded its 65535px driver limit after the actual 1001st
source/tone succeeded. The runner now uses Restore and captures the fixed queue
viewport. Attempt 2: 5 passed, 1 failed (53.2s), with both scale and long-content
cases passing. Its added Chrome offline reload targeted an uncached history-only
query URL, outside the existing exact-URL service-worker cache. The supported
precache root was used for the small cache smoke instead. Attempt 3: both
retained/error/offline scenarios passed (33.6s), including loading the cached
root and new queue modules offline. Every feature/source/error oracle remained.

The service worker now precaches all three new modules. Root reran full Node
checks and built the cache-final server; the original server PID/start token was
verified and stopped before changing fixture ownership to the new binary. The
same disposable database/accounts/media were preserved. Browser screenshots
and source WAVs remain in `actual-attempt1`, `actual-attempt2` and
`actual-attempt3` under the external evidence directory; failed attempts remain.
The uncached query-URL offline reload limitation is not claimed as new behavior.


## Actual native acceptance and recovery

Same-boot Android 14/API34 x86_64 emulator 33.1.24.0 uses real QEMU WAV output.
The initial e92f APK and all partial runs remain external. Attempt 1 was stopped
intentionally after a strict STREAM_MUSIC precondition failed at 8/15 and root
identified an incorrect admin-client progress helper. No feature acceptance is
claimed from it. The M13 helper now resets/reads progress through the signed-in
viewer and asserts both its user ID and target media ID with the actual tone.
Attempt 2 retained real ACL retry (UID2/media6, position9.307, 523Hz), offline
edits, normal/long theme captures and the actual 1001st lazy row/source proof.
Its early editing/retained preconditions failed at volume 14; natural reorder
missed the overflow while the system volume overlay occupied that location.
A manual unchanged-APK diagnostic, after actual volume keys, strict 15/unmuted
readback, PAUSED and a settling wait, showed the real menu and moved A/B/C to
A/C/B. No popup behavior was changed. Active-route volume is now established by
actual volume keys, because the emulator's shell stream-set was ineffective.

The actual light player-opened Close glyph was white on white. The sole product
correction explicitly uses the sheet's onSurface token. A test opened the queue
from the actual white-ink AppBar with Player's light theme and measured the
RenderParagraph glyph contrast: fail-before ratio 1.0, pass-after ≥3, with Close
activation preserved. At the Close-fix checkpoint, root analysis had zero
issues and all 1351 Flutter tests passed (85.73s). That external x64 APK SHA256 is
`b63288ef5c909a60094d1dbed9aa32e876660f708b0a655f43fd27f2ef3fbb7f`;
version 0.8.2 / code 4022 remains synchronized. Attempt 3 passed actual touch Next,
notification Next/Previous and explicit jump with viewer-specific ID/progress
and tones; current removal and Clear/Undo stayed stopped, and explicit resume
acquired a fresh source. Reordered natural A→C→B produced 440/659/523Hz, all
three viewer IDs finished at 12s, then stopped while browsing the catalog.
Inactive edits preserved Sets playback; Sets Next and explicit album reclaim
both passed actual audio/progress. Player-opened light/dark Close captures
passed visible glyph contrast and actual touch-close.

Attempt 3 exited 1 only because its scope runner expected Library immediately
after force-stop. Existing startup requires selecting the saved server and
explicitly unlocking/signing in to the remembered login. Attempt 4
passed that remembered-server/locked-login/password path and confirmed Queue
empty without clearing data. Its account portion used a grid-only helper that
rejected the visible 132px Log Out button; this was corrected to the normal
button lookup. Attempt 5 passed the focused nonempty viewer logout/admin login
and entries/Undo assertions, but original screenshot review found prior album
context remaining in the empty sheet. That is recorded as a product defect,
not accepted account-boundary proof. Scope clear now erases the retained label,
and an empty queue without an actual edit status omits paused-track guidance.
A named-queue → scope-clear → stop widget regression fails before and passes
after, including absent prior title/album and paused guidance. All three sheet
tests and targeted analysis pass. The actual account oracle now checks these
visible details as well; root's final full suite passes all 1352 tests (116.64s), with zero analyzer
issues (3.57s). The metadata-fix APK SHA256 is
`b9d63472c3c0e359a9f0a705ccded00ac2f6eee298f871604bcd5624c63f646a`,
with unchanged 0.8.2 / code 4022. Its first build failed compiling stale generated
plugin registration after `--no-pub`; the failed log remains. Offline pub get
and a normal build regenerated ignored plugin state and passed (1.21s + 8.69s),
without changing the lockfile. The first new-boot attempt (6) stopped before installation/sign-in because the
strict Android online-network precondition was not yet met. Actual Wi-Fi/mobile
were enabled and CONNECTED INTERNET was verified on that same boot. Focused
attempt 7 then passed on the metadata-fix APK: a visibly nonempty viewer queue,
actual Log Out, admin sign-in without an intervening data reset, and an empty
queue with disabled Undo and no old album label, track titles or paused guidance.
Root independently accepted its original screenshot. Normal final app-data and
adb-reverse teardown passed. `native-attempt7.log` and
`actual-native-attempt7/android` retain this final account proof.
Earlier native record names saying
“long title/artist” should be read as long title/album/source text: the native
sheet uses existing source context, not new artist enrichment.


## Final automated gates

Root completed the two permitted fresh review rounds and all final automated
checks. Flutter analysis reported zero issues and the full 1352-test suite
passed after the scope metadata regression (116.64s). Full web Node checks and
service-worker syntax passed after the three module cache entries. Go build,
vet and race/coverage checks passed; no Go feature changes were needed. Final
logs include `flutter-analyze-metadata-final.log`, `flutter-test-metadata-final.log`,
`web-unit-cache-final.log`, `go-build-full.log`, `go-vet-full.log` and
`go-test-full.log` in the external evidence directory. Python runner compile
and pyflakes checks and `git diff --check` also pass. Platform evidence above
is separate from these automated gates.


## Owned runtime cleanup

After attempt 5, the marked fixture was stopped and its disposable DB/media/
accounts removed; all external evidence was retained. Server PID 1555919 and
QEMU PID 1558409 were verified by binary/arguments/start token before stopping.
Only owned Pulse module 536870916 (`player_ck3`) was unloaded. The first immediate
adb read briefly retained a stale emulator registration; a settled recheck
confirmed both owned PIDs absent, port 18798 free, adb devices empty and the
owned sink absent. `runtime-cleanup.json` and `runtime-cleanup.log` retain the
ownership and cleanup evidence. For final metadata verification, a fresh no-double fixture on port 18798 and the
same API34 AVD were recreated without the scale album, using new
`emulator-output-metadata.wav` so the earlier audio remained intact.

After final attempt 7, only owned server PID 1579338 / start 41292833, emulator
PID 1579438 / start 41294625, and the recorded `player_ck3` sink module were
stopped. The recreated fixture DB/media/accounts and every evidence artifact
remain external. `runtime-cleanup-metadata.json` and its log record verified
binary/arguments/start/WAV ownership and settled checks: owned PIDs absent,
port 18798 free, adb devices empty, owned Pulse sink absent. No runtime remains.

All five browser scenarios passed in both installed Chrome and Firefox, and all
eight native scenario areas are verified across the retained runs, including
final source, UI and account-boundary corrections. No platform acceptance gap
remains within M13. Cast/receiver enhancements, persistence and other future
Music controls were not part of this slice.
