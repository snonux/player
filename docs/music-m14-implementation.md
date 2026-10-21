# M14: explicit song insertion

Task xk3, based on U01 §8.2 and M13 (0207999). Approved before feature code.
Existing Play album / song tap / Enter keeps its existing displayed-list replacement
behavior. Explicit menu **Play now** preserves the queue: insert selected songs
after the selected/current occurrence and jump to the first inserted occurrence.
**Play next** inserts after earlier pending Play next entries (FIFO).
**Add to queue** appends. Duplicates get distinct entry IDs. Checked album songs
are inserted in displayed order, regardless of selection order. Collection-wide
artist/playlist enqueue and persisted queues remain M15/M16.

## Layout and interaction

Web album detail, existing artwork/header/typography:

    [Play album] [3 selected] [Selected song actions] [Clear selection]
    [✓] 1  First Light · Aurora Lane  0:12 [⋮]
    [ ] 2  Long Road Home            0:12 [⋮]

The compact action popover contains Play now, Play next (e), Add to queue (E),
Song details. The selection action menu contains the same three queue actions.
Each checkbox has the song title as its accessible name. Selection survives an
insertion; closing the menu returns focus to its song/opener. Existing a and
ContextMenu/Shift+F10 open song actions; e/E use checked songs or the selected
song. Text fields/selects retain native typing. j/k/arrows/Enter, /, Space/p,
Esc/back and r retain their meanings. Sets audio menus also expose these actions;
video/image actions are disabled and e/E explains “Only audio can be queued”.
Now Playing marks pending occurrences “Up next (you added)”. Toast: “Added N
songs” with Undo; the queue's Undo button remains available afterward.

Android album detail reuses Material ListTile and 48dp menus. Select songs
reveals checkboxes and count; row ⋮ and selected-actions menus use the same
labels. Back first exits selection, then navigates normally. Native server Sets
audio detail overflow has all three explicit actions. Queue entry point, theme
colors, and native sheet remain shared. Snackbar includes Undo.

## Queue state transitions

Next/Append on an active queue preserve current occurrence, source and position.
On empty or retained inactive queues they collect without autoplay or taking
ownership from Sets/another source; explicit Play now or queue jump claims a
fresh session. Initial native enqueue installs account-scoped loader/claim
callbacks without claiming playback. Account/server changes clear callbacks,
entries, source label, FIFO markers and Undo.

Pending FIFO membership is occurrence based. Natural advance consumes passed
members and leaves remaining pending members in order. Explicit jump, backward
transport, manual reorder/remove, replacement and source takeover invalidate the FIFO block;
subsequent Play next starts a new block after the current occurrence. Append
never joins the FIFO block. Undo restores the last content edit; if the current
occurrence survives, its playback and position survive. Undo removing a currently
playing inserted occurrence stops/flushes it and restores paused contents, never
autoplays an old source. Async stop/claim/load completions use operation and
scope guards so they cannot mutate a newer queue/account. A monotonic FIFO epoch
survives Undo: restoring contents never resurrects markers invalidated by an
intervening jump/backward transport. Natural forward advance keeps the epoch and
consumes current/past markers.

## States and acceptance

Loading disables conflicting actions with visible Loading track guidance. Empty
queue explains adding songs; Next/Append remains paused. Offline enqueue edits
in-memory entries; an actual attempted inaccessible source shows existing
retry/error UI. Permission loss is verified with a real revoked media grant.
Menus wrap long labels and preserve readable title/artist; light, dark and narrow
captures verify contrast and no horizontal overflow. Reload/cold restart follows
the existing nonpersistent M13 contract (empty queue); M16 adds persistence.

Actual installed Chrome/Firefox journeys must cover single/multiple insertion,
repeated FIFO blocks, duplicates, source/position preservation, Play now, Undo,
natural tone/ID order, keyboard/focus/typing and pointer actions. Native API34
emulator must cover equivalent touch/menu/Back, background transport, scope,
error/offline and restart behavior with actual tone and authoritative progress
IDs. Evidence, fixture, audio and APKs stay outside git in xk3-specific paths.

## Implementation

Web album rows expose native labeled checkboxes directly, with selection count,
Selected song actions and Clear selection. Android uses Select songs and Back
to exit selection. The service-worker cache is kiss-v29 and precaches
queue-actions.js. Confirmation Undo captures its edit token; a stale/superseded
action cannot undo a later edit or another account's queue. Canceled Play now
operations produce no successful insertion feedback. Native queue feedback
replaces queued SnackBars so the latest successful confirmation or error stays
visible, including with accessibility navigation enabled.

## Verification checkpoint — 2026-10-21

| Platform | Actual runtime | Accepted result / evidence |
| --- | --- | --- |
| Google Chrome | Installed 144.0.7559.132, Playwright chrome channel; Player 0.8.2 server built from 0207999-dirty | All five main M14 journeys plus focused batch Append/Play now accepted across retained attempts: single/batch insertion, FIFO/duplicates, current source/position, Play now/Undo, e/E/Q/a/focus/typing/native toast Space, actual natural A→C→B and duplicate B, empty/reload/offline module import, Sets actions, real 403/retry and light/dark/narrow/long labels. |
| Mozilla Firefox | Installed 156.0, /usr/lib64/firefox/firefox through moz-firefox BiDi; same server | Same five main journeys and focused batch case accepted. Actual recovered Glass City stream is media 27, 587.3 Hz on both browsers. |
| Native Android core | Player_FDroid_Test_API34, API 34, x86_64/userdebug, density 440; native release 0.8.2/code 4022, SHA256 37c42671d60d6a52ca7d5e19d34ddeba366a4890239ffa789dc935b99f3b7b42 | Clean all-core journey: touch single/batch insertion, selection Back, FIFO/duplicates/position, Play now/Undo, themes/long labels/48dp menus, natural A→C→B, fourth duplicate B, empty/offline insertion, real denied-source retry, server Sets detail, cold locked restart and actual logout/admin scope clear. |
| Native focused feedback and batch | Fresh release 0.8.2/code 4022, SHA256 777c1ce9f80de32022d7a4e180da10fd9850290476987ca5c66bb41499441151 | Focused --scenario feedback passed: batch two → append one → latest Added 1 song/no stale Added 2 songs → snackbar Undo retains the original two. Focused --scenario batch also passed: playing Append preserves source/position/order; batch Play now retains the remainder and audibly starts B then C with authoritative viewer IDs. Core engine proof above is unchanged by this UI correction. |

The focused browser batch case preserves actively playing A at the same URI:
Chrome 1.802→1.858→2.827 seconds, Firefox 1.995→2.357→3.360 seconds,
with 440 Hz output. Reverse-click batch Append yields [A,B,A,C]; reverse-click
batch Play now yields [A,B-new,C-new,B-old,A,C], preserving all remainder
occurrence IDs. Both browsers capture B523→C659 and viewer UID 2 progress
for media 2/6/4. The corresponding native focused batch also passed on the 777c UI-fix APK:
A stayed PLAYING at 4360 ms, advanced to 5940 ms with 440 Hz for 1.48 seconds,
and saved viewer UID 2/media 2 at 5.94 seconds. The six-row remainder is
visible with B Now at row 2 then C Now at row 3; actual B523.2 for 7.03 seconds
and C659.3 for 7.18 seconds save media 6/4 at 7.251/7.238 seconds.

Native source evidence uses the signed-in viewer UID 2: natural A/C media IDs
2/4 finish at 12 seconds with ordered 441.9/659.3/523.2 Hz windows. The fourth
B occurrence is visibly Now at row 4, with media 6 saved at 7.268 seconds and
523.2 Hz for 7.09 seconds. Real denied-source retry saves media 6 at 9.17 seconds
with 523.3 Hz for 8.98 seconds. Sets Next/Append preserve paused Long Road Home
at 9170 ms; explicit Play now changes to media 2, 440 Hz for 7.01 seconds.
The original empty account sheet has no previous title, source label, pending
FIFO guidance or enabled Undo.

Browser attempt 1 passed eight cases and failed two permission-response waiters
because they selected the already-cached source. Attempt 2 accidentally ran the
unchanged runner after a patch-path error and was interrupted; no acceptance is
taken from it. Attempt 3 passed Firefox's real uncached 403/retry; Chrome then
hit a retry oracle that blindly toggled a previously open queue closed.
Attempt 4 uses explicit pointer/open-state checks and passed both boundary
cases. Main keyboard/focus assertions remain in the accepted first journey.
This is acceptance across retained attempts, not a claim of one clean ten-case
run. The first focused batch attempt reached correct orders and B/C audio but paused C
before its three-second progress tick; it remains preserved. The strengthened
second attempt waits for that real tick and passed both browsers. Every failed
attempt, original screenshot and audio artifact remains.

Both allowed fresh review rounds are complete. Round 1's FIFO-marker Undo
finding has production FAIL-before/PASS-after regressions on both clients;
round 2 found no blocker. Parent screenshot review then found the stale native
SnackBar; its accessibility-mode widget journey fails before the correction
and passes afterward, including Undo removing only the newest occurrence.
No third review was run.

Full gates on the FIFO-fix revision passed: Flutter analysis with zero issues,
1,360 Flutter tests, Go build/vet/race-cover, all Node tests and service-worker
syntax checks. Earlier unchanged progress-timer and SVG timing failures are
retained; a serial full run passed without changing assertions or thresholds.
The final feedback-only gate passed: analysis reports zero issues and all 1,361
Flutter tests pass (136.92 seconds); its fresh release APK build also passes.
Focused screen/detail/sheet checks for that correction pass 32 tests; scoped
analysis reports zero issues. Go/web sources did not change in that correction.

Evidence is external under /home/paul/tmp/xk3-evidence:
actual-browser-attempt1 (main four cases per browser), actual-browser-attempt3/4
(real permission recovery), actual-browser-batch-attempt2 (both strengthened batch cases),
actual-native-attempt1 (all core native scenarios),
actual-native-feedback-attempt1 (latest confirmation and exact Undo),
actual-native-batch-attempt1 (playing Append and batch Play now),
full-gate logs, FAIL-before/PASS-after logs and APKs. The disposable marked
fixture is /home/paul/tmp/xk3-fixture on port 18799; runtime-ownership.json records
owned server/QEMU PID+start time, sink and exact runner arguments. No media,
database, APK or generated Android output is committed.

Reproduction commands (component-local):

```sh
# From repository root, with a current external server binary:
python3 player-android/integration_test/support/music_fixture.py up \
  /home/paul/tmp/xk3-evidence/player-final --port 18799 --no-doubles \
  --dir /home/paul/tmp/xk3-fixture \
  --evidence /home/paul/tmp/xk3-evidence/actual-browser-attempt1

# From player-server/test/e2e-web; an owned player_xk3 null sink is active:
PLAYER_MUSIC_FIXTURE=/home/paul/tmp/xk3-fixture \
PLAYER_E2E_PULSE_SINK=player_xk3 \
npx playwright test -c playwright.music.config.ts --grep M14 --workers=1 \
  --output /home/paul/tmp/xk3-evidence/browser-attempt1-playwright
# Boundary-only recovery uses --grep 'M14 Sets audio' and a new evidence dir.
# Focused batch verification uses --grep 'M14 batch Append' and
# --output /home/paul/tmp/xk3-evidence/browser-batch-attempt2-playwright.

# From player-android/test/e2e-live, on the owned cold-booted API34 emulator:
python3 music_insertion_e2e.py /home/paul/tmp/xk3-fixture \
  --apk /home/paul/tmp/xk3-evidence/app-x86_64-fifo-fix-release.apk \
  --emulator-wav /home/paul/tmp/xk3-evidence/emulator-output-attempt1.wav
# After a fresh UI-fix build, focused feedback verification:
python3 music_insertion_e2e.py /home/paul/tmp/xk3-fixture \
  --apk /home/paul/tmp/xk3-evidence/app-x86_64-feedback-fix-release.apk \
  --emulator-wav /home/paul/tmp/xk3-evidence/emulator-output-attempt1.wav \
  --scenario feedback
# Focused batch verification uses the same UI-fix APK and --scenario batch,
# with a new actual-native-batch-attempt1 evidence directory.
```

Each attempt gets a distinct external evidence directory before starting; do
not overwrite prior captures or reuse old WAV/APK evidence as a new execution.
Versions remain synchronized at 0.8.2 / 0.8.2+22. No release, push or deployment.

After acceptance, owned runtime was stopped at 2026-10-21 15:29:49 UTC:
server PID 1602658 and QEMU PID 1608870 matched recorded process start times
and binaries before SIGTERM; only the verified player_xk3 null-sink module
536870916 was unloaded. cleanup.json records ownership checks. All fixture
DB/media/accounts and external evidence/APKs/WAVs remain preserved.
