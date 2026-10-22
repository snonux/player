# Music M20: explicit repeat

Approved before feature coding by root on 2026-10-21, based on HEAD a6cd8c8
and U01 sections 8.0, 8.4 and 12. Extends the existing Music queue owners.

## Interaction and scope

Repeat affects only explicit Music queue playback. Sets browse wrapping/resume,
podcasts, video/image, local files, downloads and source ownership keep their
existing behavior. Default Off. Reuse Now Playing dock/modal on web and native
queue sheet; no new screen or modal.

    Now Playing                                  Close
    Playing from: Night Transit
    [repeat icon  Repeat: Off / All / One]        <- full accessible mode label
    Current track marked Now / restored paused / existing error status
    [Resume queue]                               <- restored sessions only
    1 First Light · Now
    2 Long Road Home · Up next (you added)
    [Clear queue] [Undo]

Use existing theme buttons/spacing, active selected styling plus aria-pressed
for All/One; One uses repeat-one icon. Off has normal neutral styling. Native
uses repeat/repeat_one icons, labeled Material control and >= 48 dp tap target,
selected semantics and theme colors. Long labels/status wrap; existing rows
scroll. Light/dark/narrow evidence required. Mode control remains available
with an empty queue, disabled during load/restore; errors retain existing Retry
and storage feedback. There is no extra permission or offline request for a
mode change. Native Back closes sheet before navigation.

Web Y cycles Off -> All -> One globally and within queue, after native typing,
SELECT and dialog guards. At design time Y was unassigned. Existing
j/k/h/l/arrows, Enter, Space/p, /, Esc hierarchy and r remain. Button activation
and Y keep visible focus/selection; close restores opener. Pointer alternative
is the same labeled button. Help lists Y and explains queue scope.

## Transport and edits

| Action | Off | All | One |
| --- | --- | --- | --- |
| Natural completion before last | next | next | same occurrence at zero |
| Natural completion at last | stop ended | first at zero | same occurrence at zero |
| Explicit previous/next | adjacent, boundary no-op | adjacent, boundary wraps | adjacent, boundary no-op |

A deliberate repeat restarts at zero even for a long queue item. Finish the old
progress run once and start a fresh run. Repeat One never consumes future Up
next markers. All uses current edited order; reorder retains occurrence ID.
Deleting current or Clear stops; Undo remains paused where existing behavior
requires and never changes chosen repeat mode. New collection keeps mode.
Reject duplicate completion and stale completion after source/occurrence intent
changes, including same-media replacement while final progress save waits.
Native handler owns behavior after Back and through media-session transport.

## Persistence and errors

Add optional repeat to existing version 1 identifier-only snapshots, missing
field defaults Off, invalid value follows corrupt recovery. Persist mode even
for empty/cleared queues. Scope by origin + authenticated account as existing
recovery; scope change detaches callbacks and defaults mode Off until scoped
restore. Restore order/occurrence/index/position/mode paused, no autoplay.
Offline Retry/ACL removal/storage failure remain existing flows. Source takeover
retains queue preference but cannot apply it to replacement nonqueue playback.
Loader failure stays retryable and cannot silently start prior/other source.

## Original acceptance plan (fulfilled)

Targeted Node and Flutter owner/codec/widget checks cover modes, manual/natural
separation, edited order, occurrence identity, duplicate/stale completion,
next FIFO, Undo, legacy snapshots, scope and loader/source negatives. Root owns
full suites, builds, review launches, local commit and task completion.

After those gates and root runtime authorization, run installed Google Chrome,
installed Mozilla Firefox, and native API 34 emulator on a new isolated local
fixture. Actual short distinct-tone queue checks: Off last stops, All last wraps,
One same occurrence restarts zero, manual One previous/next changes track;
assert IDs, time/source and PCM output. Exercise reorder/remove/Clear+Undo/FIFO,
mode reload/force-stop recovery with explicit Resume, account isolation and
real offline recovery Retry. Keyboard Y typing/focus/Escape plus pointer in both
browsers; native touch/Back and notification skip. Capture original light/dark
and narrow/portrait screenshots. Record exact versions/commands/state/audio
outside git. The following sections record the resulting verification.

## Implementation and review

The existing queue owners implement the approved state transitions. Web/native
version 1 snapshots store optional repeat; legacy snapshots default Off. UI uses
the existing labeled theme controls, Y/help and native selected semantics.
Native handler ownership covers Back/background and notification transport;
Sets, podcast/local/download behavior remains separate.

Fresh review round 1 found three real gaps: the native slider uses seekForSession
directly; Cast's sender callback omitted natural completion; the actual detached
window advanced without final finished writes. The exact-entrypoint native and
production sender/actual popup tests failed before repairs and passed afterward
(`dk3-evidence/*review1-before*.log`, `*review1-after.log`). Popup writes are
serialized position -> finished after previous writes, with source/play/seek
cycle guards and duplicate completion coalescing. Root interrupted its first
client full gate for these fixes; its partial logs are retained without a pass
claim. Root self-review and the final narrow fresh review round 2 confirmed all
three repairs without remaining blockers. Both allowed review rounds were used.

## Final gates and builds

Root final analysis: zero issues (4.49s). All 1398 Flutter tests passed (233.37s).
Go build/vet/full race+coverage passed (264.17s); full Node/SW Mage web checks
passed (6s). Errcheck has 256 existing diagnostics, zero new/resolved after
comparing expressions independently of shifted lines; errcheck is not clean.
Targeted native 44 owner/recovery/sheet tests and Node owner/codec/keyboard/
detached forwarding tests also passed. No product/unit changes followed these
gates; subsequent fixes were acceptance-runner and documentation changes only.

Fresh normal server build (1.46s): 0.8.2,
`a6cd8c86c799ad0776dc605e5a47284febc719e7-dirty`.
Fresh release x86_64 APK (63.05s): 0.8.2 / code 4022, SHA256
`dec70c1e1565e374739d366db0edb9c997680befbb8382558b8439b8ded3817a`.
Root independently checked versions/hash. Shared versions, lock and Gradle files
remain unchanged. No release, push, deployment or version bump was performed.

## Accepted installed-browser verification

Installed Google Chrome 144.0.7559.132 and installed Mozilla Firefox 156.0 each
passed all four cases, 8/8 total in 239.41s against the fresh local fixture on port 18811.
No browser retry. Exact command/result: external
`dk3-evidence/web-attempt1-{command,result}.json`; log `web-attempt1.log`.
Equivalent component-local command:

```sh
PLAYER_MUSIC_FIXTURE=/home/paul/tmp/dk3-fixture PLAYER_E2E_PULSE_SINK=player_dk3 \
  npx --no-install playwright test -c playwright.music.config.ts music-repeat --workers=1
```

Both browsers verified natural Off stop and same-source Replay, All wrap to the
edited first occurrence with a distinct actual source tone, One same-occurrence
zero reset and explicit Next/Previous adjacency. Paused reload kept exact
occurrences/index/offset/mode; explicit Resume, account isolation, current
removal/Clear/Undo/empty reload, native typing/Y focus and wide/narrow light/dark
controls passed. Actual browser-offline Retry retained the snapshot and restored
paused online. The initial transport abort creates a visible failure before
actual offline Retry; it is not claimed as a wholly offline authentication run.
Actual detached windows finalized old position+finished before One reset, popup
Y and All wrap. Root independently FFT-checked original WAVs, saved IDs/order
and original-size light/dark 390px screenshots. All eight cases are accepted.

## Native verification and retained runner failures

Final APK ran on rooted online emulator 33.1.24.0,
Player_FDroid_Test_API 34, Android 14 / API 34 userdebug x86_64 / density 440. Exact
versions are in `dk3-evidence/android/versions.json`. All source/audio/state
artifacts and originals remain outside git; no full preference XML or
credentials were saved. Scoped queue JSON contains identifiers/settings only.

- Attempt 1 (`native-attempt1-command.json`, 206.80s, exit 1) passed Off last stop
  with actual B 523Hz output/11.77s audible and authoritative viewer 2 / media 1
  finished save; same-source Replay also stopped. Root accepted both phases.
  It then failed reading preference XML while All played. Subsequent five
  scoped reads and 160 direct cat reads were valid. The cause is transient
  unreadable input; atomic file replacement is possible but unproven. The M20
  reader now checks exit status and bounded-retries only read/XML failures,
  keeping XML/credentials in memory. JSON/schema errors still fail immediately.
- Attempt 2 (`native-attempt2-command.json`, 652.36s, exit 1) used the same APK/boot
  with `--resume-device --transport-from all`, skipping accepted Off. Root
  accepted All's edited first occurrence 6 / index 0 with actual B 524 -> A 440Hz;
  One's exact later occurrence 4 / index 1 reset 12.042 -> 0.001s with A 440Hz; touch
  Next/actual notification Previous with distinct expected tones and fresh
  viewer 2 saves at 7.262 / 7.530s. One's oracle uses the handler's persisted live
  offset because media-session position can remain a snapshot between events.
  Exact recovery IDs [7,8,9] / media [3,1,3] / selected 9 / index 2 / 7.749s / One stayed
  paused until explicit Resume; original light/dark portrait controls fit.
  Account Off/empty, return One, current removal/Undo/Clear/Undo and empty
  restart without autoplay also passed and were accepted. Offline setup then
  failed before any outage: a reused helper required offset < 9s, but the actual
  queue was correctly PAUSED at 9.34s. That bound is not a feature requirement.
- Attempt 3 (`native-attempt3-command.json`, 157.30s, exit 0) ran only the
  unfinished offline phase with the same APK/boot and exact retained snapshot
  oracle. Before launch, entries [1,3,2] / media [3,3,1], selected occurrence 3 /
  index 1 / 9.34s / One, actual PAUSED 9340ms First Light and offset below the
  actual 12s duration were independently verified. Visible All was selected.
  The verified owned server stopped; cold unlock showed a real connection
  failure, retained the exact queue and never autoplayed. Restarting the same
  final binary/database/media and explicit password Retry restored those exact
  entries, selected occurrence, index and offset paused in All mode. Root
  reviewed the original error/recovered screenshots and accepted all results.

All required Chrome, Firefox and native API 34 repeat journeys are accepted.
Native runners and user commands are documented in the Android E2E README;
phase-specific continuations did not repeat already accepted phases. All
historical failures, original WAVs and screenshots remain retained outside git.

## Verification limits and runtime cleanup

The web Cast sender completion callback was tested through the production sender
callback with a minimal SDK fixture; no physical Cast receiver was exercised.
Native media-session transport used the actual notification on the emulator;
no physical headset or phone was tested. Actual detached playback was verified
in both installed browsers. These limits do not replace any required browser or
native repeat acceptance above.

Root authorized stop-only after accepting the final offline evidence. Exact
server and QEMU executable/start fingerprints were checked before stopping the
owned app, removing only reverse port 18811, and terminating those processes.
Only the verified player_dk3 null-sink module was unloaded; the shared Pulse
daemon remains available. Root independently verified server, emulator and all
three runner PIDs absent, port 18811 free, adb devices empty and task audio sink
absent. The fixture remains intact with two users, 43 media rows, 47 files and
its auth data. APKs, logs, WAVs, screenshots and all evidence remain preserved.
External records: `dk3-evidence/stop-only-results.json`,
`runtime-ownership.json`, and `acceptance-attempts-summary.json`. No fixture
teardown or deletion occurred. Root owns staging, local commit and task closure.
