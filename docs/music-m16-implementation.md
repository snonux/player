# Music M16: local queue recovery

Pre-code interaction proposal for yk3, 2026-10-21, based on HEAD 89e13ac,
U01 sections 5.3/8.4 and accepted M12/M13/M14/M26/M27 behavior.

## Scope and storage contract

Recover the existing Music queue on this browser/device. No server queue sync,
new shuffle/repeat modes, podcast resume changes or release/version changes.
Persist versioned occurrence IDs, ordered media IDs, selected occurrence/index,
exact queue position in seconds, source label and existing Up next markers.
Repeated media IDs remain distinct occurrences. Persist no tokens, signed URLs,
absolute source paths, passwords or progress callbacks. Browser storage is
`player.queue.<origin>.<userId>`; Android uses the equivalent scoped preferences.
Use authenticated server identity (add user_id to authenticated config for web),
not a remembered login name. Native restores only after explicit server choice
and unlock. Scope switches detach callbacks and hide the previous queue while
retaining its disk snapshot for that same account's next unlock.

## Annotated web layout

Reuse Now Playing queue dock (wide) and full-screen modal (narrow):

    Now Playing                                  Close
    Playing from: Night Transit
    Queue restored · paused at 0:07
    [Resume queue]                     <- primary, explicit playback
    1 First Light
    2 Long Road Home · Selected · paused <- same occurrence after reorder
    3 First Light                       <- separate repeated occurrence
    [Clear queue] [Undo]

Existing queue button/Q is the entry point; a quiet restored-session notice
points to it. Resume uses the saved selected occurrence and position; choosing
a different row starts that row under existing short-song/long-item rules.
Current item remains reachable through its existing player/queue route; web
navigation restoration loads it paused with the saved queue position when it
matches the current occurrence. Explicit URL/query navigation wins: recovery never
redirects browsing or opens an old media route after login. No autoplay request followed by pause is used.
Native button activation, j/k/arrows, Enter, K/J, Delete, Q/Escape and focus
restoration keep existing contracts. No new shortcut is allocated.

## Annotated native layout

Reuse Material Now Playing sheet from the existing queue icon after server
unlock. Header/source remain; below status put a labeled Resume queue button
with a generous native tap target. Selected row says Selected · paused; status
includes the saved m:ss. Resume claims fresh authenticated ownership, seeks to
the exact saved position and starts only in response to that tap. Current item
has an Open current item action to the existing detail route, retaining access
to normal playback controls. Back closes the sheet before leaving Library.
Rows keep labeled overflow, touch reorder/removal and semantics. Long titles
use existing wrapping/ellipsis, source/status wrap, footer remains reachable.

## State transitions and failure behavior

- Queue creation/edit/advance/seek/pause updates snapshot. Periodic position
  saves bound force-stop loss; pause and background/page-exit flush immediately.
  Active source position belongs to the selected occurrence, never a media-ID
  match alone. Reorder retains it; selecting another entry resets it.
- Reload/restart -> authenticated scope -> validate saved format -> revalidate
  media through normal server authorization -> restored paused queue. Recover
  selection by occurrence ID when entries disappear before it. Removed/forbidden
  entries drop with quiet unavailable-song count. Missing current item selects
  a surviving neighbor at zero; all missing becomes empty. No media starts.
- Validation loading disables recovery actions. Network/server errors retain
  the saved snapshot, show Can't restore queue. Check your connection. Retry,
  and never mistake offline failure for deleted media. New queue/actions or
  scope changes invalidate late restore completions.
- Malformed/version-unknown/invalid snapshot is discarded with readable
  feedback and normal empty queue. Failed local storage writes leave playback
  usable and show Queue could not be saved on this device.
- Clear removes persisted contents immediately; Undo restores current-session
  contents without autoplay. Clear followed by reload stays empty. Undo history
  itself does not survive restart. Deleting current stops playback and cannot
  resurrect its saved offset on another occurrence.
- Account/server/local-library switches preserve isolation and existing source
  shutdown. Starting Sets/local/download/share playback preserves existing
  behavior and never restores a queue over an active replacement source.
  Casting source ownership stays with existing controls; recovery is local and
  always paused, without rejoining or starting a receiver.

## Planned acceptance and checkpoints

Targeted meaningful storage/queue tests cover repeated occurrences, exact
short-song position, reorders, clear/Undo, deleted current, corrupt/unknown data,
storage failure, offline retry and stale scope/restore responses. Root owns
full suites/builds and at most two fresh review rounds after product freeze.
Actual installed Chrome, Firefox and native API34 checks follow those gates:
build/reorder repeated queue, pause partway, reload or force-stop, unlock native,
assert order/index/exact position and silence before explicit Resume; then
assert actual source/tone and normal Next. Verify same/different accounts and
server origins, clear/reload, unavailable entries and offline retry. Exercise
keyboard typing/focus and pointer/native touch/Back; capture/review original
light/dark/narrow results. Evidence/runtimes stay outside git. At this proposal
checkpoint no product code, build or runtime acceptance has run.

Root approved this proposal before feature coding on 2026-10-21. Explicit URL/query navigation remains authoritative; queue recovery never redirects browsing.

## Product and targeted-check checkpoint

Identifier-only codecs and account-scoped browser/preferences persistence extend
the existing queue owners. Authenticated config adds user_id on both prefixes;
web restores independently of routing and applies its saved offset only when
navigation targets the selected queued media. Native restore is initialized
after explicit unlock; saved IDs are resolved with fresh account requests,
epoch-guarded loaders and source ownership claims. No playback is loaded or
started by native recovery. Web paused route loading uses existing selectMedia.
Existing queue panel/sheet shows exact saved m:ss, explicit Resume queue,
selected occurrence and Retry restore when unavailable offline. The native sheet
also opens current media details without starting it. Web announces recovery.
SW kiss-v33 precaches queue-storage.js for upgrade consistency.

Targeted checks (terminal output, no runtime acceptance claim): Node snapshot
validation and playback-source checks PASS, including repeated IDs, later
occurrence/index, short-song 7.25s recovery, reordered selected occurrence,
paused route Up next retention, explicit seek, immediate pause save, clear/Undo,
403 drop, offline original bytes retained, scope-label isolation, corrupt data,
stale completion and quota errors. API scoped tests PASS for authenticated
user_id values and anonymous rejection on both /api/config and /api/v1/config.
Native focused recovery/sequence/sheet checks PASS (36 tests), including
identifier validation, no source loading/play before explicit Resume, exact
7.25s seek and 8.25s pause save, repeated occurrence reorder, scoped validation,
missing selected item at zero, permission removal, offline Retry, corrupt data,
account switch/late response, new user queue superseding recovery, Back and
recovered sheet labels. Scoped analysis has no issues after style/import fixes.
Initial new-test failures were a mock session store fixed to user1 and a
replacement-active assertion incorrectly expecting paused; both test oracles
were corrected. A new widget test initially missed its MusicQueueEntry import;
this compile failure was fixed before the 36-test pass.

Root full suites/builds, fresh review and actual installed Chrome/Firefox/native
API34 recovery/audio/account/theme journeys remain pending at this checkpoint.
No build, emulator, disposable server or browser runtime was started here.

Root pre-gate self-review found Resume was clearing the restored future Play
next block on both platforms. Dedicated Resume now retains that block across
fresh source ownership; deliberate row jumps keep their existing invalidation.
New regressions restore A(current), B/C(up next), D(tail), Resume, add E next and
require A,B,C,E,D; a later explicit row jump clears markers. Node playback-source
and native recovery/sheet (14 tests) PASS; scoped analysis is clean. No fresh
review round or full gate had run before this bounded fix.

## Acceptance runner preparation

`music-queue-recovery.test.ts` supplies two installed-browser cases per platform;
Playwright --list parses four cases without starting browsers. It exercises
reload, repeated current occurrence, exact offset, no autoplay, actual Resume
output, explicit URL priority, account roundtrip, Clear, light/dark/narrow,
typing/focus, real ACL drops, corrupt JSON, initial aborted-transport failure
followed by actual browser-offline Retry and online recovery. Native
`music_queue_recovery_e2e.py` supplies actual touch/Back, pause/force-stop/unlock,
scoped queue-only preference reads, no PLAYING before Resume, original tone,
account roundtrip, Clear/restart and real ACL revocation. py_compile and pyflakes
pass. These are prepared runners only; all actual platform acceptance is pending.
The native runner requires root adbd on the disposable userdebug emulator and
retains runtime/data for root audit and stop-only cleanup.

## Root gates, review and final artifacts

Root full gates passed: Flutter analysis clean and 1384 tests PASS (146.82s),
Go build/vet and full race/coverage suite PASS (208.23s), web Node/SW checks
PASS. Errcheck has 256 existing findings and zero new findings after accounting
for moved source locations; it is not clean. First fresh expert review was
clear, so no second review ran. Product and unit code stayed frozen during
actual acceptance; only runners and documentation changed afterward.

Final server: 0.8.2, build
`89e13ac5df11679969bbfab6dca5a5464ae025fa-dirty`, built in 0.58s.
Final release x86_64 APK: 0.8.2/code4022, built in 38.86s, SHA256
`a692102a73451d4dba8f3e7fc0d35cd0cf8c23379d044f1e0e3cf0b913c1153a`.
Root independently verified both. Generated Gradle migrator lines were removed;
versions and lock stayed unchanged. All logs, originals, state and audio remain
outside git under `/home/paul/tmp/yk3-evidence/`.

## Actual browser acceptance

Installed Google Chrome 144.0.7559.132 and Mozilla Firefox 156.0 each passed all
three M16 cases against the final server and fresh port 18809 fixture, without
service doubles. `web-acceptance-summary.json` maps the core attempts: attempt1
had 1 PASS/3 FAIL (33s); Chrome attempt2 both cases PASS (12.1s); only the
unfinished Firefox boundary case ran in attempt3 and passed (10.9s).
Supplemental normal Next passed in both browsers (19.4s total), with no replay
of accepted account/theme/error phases.

Both browsers built and reordered a real repeated playlist through UI controls.
Occurrence IDs [1,3,2], media [1,1,4] and selected index 1 survived reload.
Exact saved offsets were 5.017989s (Chrome) and 5.051383s (Firefox); media stayed
paused with unchanged time until explicit Resume. Actual resumed HTML positions
were 5.894542s and 5.947383s, bounded by saved offset plus elapsed time, and both
PCM captures contained the original 440Hz for 1.3s. Normal Next selected the
following occurrence 2 at index 2, `/api/media/4/stream`, with original 523.2Hz
output. It did not return to the earlier duplicate.

Actual logout/admin sign-in hid the old queue; returning to the original account
recovered it. Clear stayed empty after reload. An explicit Harbour URL selected
that paused source while retaining the separately saved queue. Keyboard typing,
focus/buttons and original light/dark 1366/390 screenshots passed. Root reviewed
originals, logs, snapshots, actual runtime positions and audio.

Boundary testing first aborted metadata transport to expose Retry for a known
authenticated scope, then used actual browser offline mode. Retry failed visibly,
retained the snapshot and did not play; online Retry recovered it. Real ACL
revocation dropped all three unavailable occurrences. Corrupt JSON at document
startup produced readable empty recovery. The initial abort is fault injection;
the offline Retry is a real browser network boundary. Unknown-account offline
startup follows existing authentication behavior.

Initial Chrome failures sampled the first seek's saved offset before a second
seek persisted. The runner now uses one seek and waits for persisted offset to
match actual paused HTML time. Initial Firefox corrupt setup was overwritten by
normal pagehide; injection now occurs at the next document's startup. These were
runner oracles; product code did not change and all original failures remain.

Exact external command/terminal files are `web-attempt{1,2,3}-*.json` and
`web-normal-next-*.json`. Equivalent component-local commands:

```sh
PLAYER_MUSIC_FIXTURE=/home/paul/tmp/yk3-fixture PLAYER_E2E_PULSE_SINK=player_yk3 \
  npx --no-install playwright test -c playwright.music.config.ts music-queue-recovery --workers=1
# Retained accepted cases; only unfinished cases were rerun:
# append --project=chrome
# then --project=firefox --grep 'revalidation permission'
# supplemental only: --grep 'normal Next'
```

## Actual native acceptance

Main native run passed all seven logged phases in 310s on the exact final APK:
owned coldboot Player_FDroid_Test_API34, Android 14/API 34 x86_64 userdebug,
emulator 33.1.24, 1080×2220/density 440. Root adbd preceded reverse mapping and
actual validated Wi-Fi was verified. Only scoped queue JSON was extracted;
full auth preferences were never printed or saved.

Actual touch selected the later duplicate, reordered and paused it. Force-stop
and relaunch preserved the remembered server and locked login; explicit password
unlock recovered IDs [1,3,2], media [1,1,4], index 1 and exact 6.635s. No PLAYING
source appeared and the offset stayed unchanged before Resume. Light/dark
originals show paused 0:06, selected second occurrence, Resume and Open current
item. Details/Back did not start playback. Explicit touch Resume published
First Light PLAYING at 6635ms; a later pause saved 11.31s at the same index.
Original QEMU output proves 440Hz for 4.49s.

Actual logout/admin sign-in hid the prior queue and metadata; returning to the
original user recovered the same 11.31s session. Clear stayed empty after a
force-stop/locked-login restart. Real server permission revocation dropped all
three unavailable occurrences. Root accepted these originals, state and audio.
Exact main command and terminal are in `native-attempt1-*.json`; runtime and data
remain held for root audit and stop-only cleanup.

The focused native supplement passed all four phases in 88s. A fingerprint-checked
cutoff stopped the owned server before cold unlock. Actual password sign-in
showed connection failure, kept IDs [4,6,5], media [1,1,4], index 1 and 4.578s
unchanged, and published no playing source. The same final binary/database/media
restarted with refreshed process ownership. Explicit online password retry
recovered that exact paused queue. This verifies offline unlock retry, distinct
from queue Retry provider logic verified by unit tests.

After Resume, actual notification Next selected Long Road Home at index 2,
occurrence 5/media 4, with runtime position 0ms. Authoritative user 2/media 4
progress saved 6.24s, and original QEMU output contained 523.4Hz for 6.1s. Root
reviewed offline/recovered/notification originals, state, terminal and audio.
`native-normal-next-offline-*.json` records the exact supplemental command and
terminal. The earlier unaccepted supplemental setup was deliberately stopped
when the offline boundary was added; its log and terminal remain preserved.
Accepted main account/theme/Clear/ACL phases were not replayed.

The main native command from `player-android/` was:

```sh
python3 -u test/e2e-live/music_queue_recovery_e2e.py /home/paul/tmp/yk3-fixture \
  --apk /home/paul/tmp/yk3-evidence/app-x86_64-release.apk \
  --emulator-wav /home/paul/tmp/yk3-evidence/emulator-output.wav --hold-device
# Focused supplement on the retained APK/device adds:
# --resume-device --scenario normal-next
```

The runner now accepts optional `--ownership-path` for orchestration metadata,
instead of hardcoding this session's external directory. It updates that file
only when supplied; py_compile/pyflakes passed after this portability-only fix.
No product or unit code changed, and no accepted journey needed replay.

## Acceptance, cleanup and limits

All required Chrome, Firefox and native API 34 journeys are accepted by root,
including recovery, exact current occurrence/position, no surprise autoplay,
Resume/normal Next audio, account isolation, clear, relevant permission/error
boundaries and readable platform controls. Native evidence is from the emulator;
physical hardware and operated TalkBack were not exercised. Browser narrow
checks used real keyboard/pointer input, without physical touch hardware. Queue
recovery stays local to each server/account/browser/device, separate from
server queue synchronization, per-file progress and podcast synchronization.

Root-authorized stop-only cleanup rechecked server 1719441/start42792388 and
QEMU 1715620/start42728771 against their exact executables, then stopped only
those runtimes, the owned app session/reverse mapping and Pulse module 536870916
whose arguments identified player_yk3. `cleanup-stop-only.json` confirms absent
owned processes, unreachable health, free port 18809, settled empty adb devices,
and missing owned Pulse module. An initial transient adb offline entry is
retained alongside the settled result. Fixture database/media/accounts and all
builds, logs, originals, JSON and WAV evidence remain preserved outside git.
No release, push, deployment or version bump occurred. Root owns final staging,
local commit and task completion.
