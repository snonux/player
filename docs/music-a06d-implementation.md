# Automatic library scans (zl3)

Implementation and two bounded review rounds are complete. Final R2 approved
the interrupted-inventory recovery, state-protected merge, conflict listing and
configured-root symlink fixes. Integrated Chrome/Firefox/native acceptance
passed on 2026-10-27 (see below).

Design recorded before implementation, 2026-10-25. Extends U01 (nj3), the
isolated verification harness (oj3), and the existing scanner/missing policy
(hl3 prerequisite). Preflight found hl3 marked completed but no implementation
commit or retention policy in this checkout: current manual scans soft-delete
orphans. This task does not recreate hl3. Automatic scans use explicit inventory
mode: add/refresh and reuse existing unique move matching, with no orphan deletion
or clearing unrelated identity conflicts.
Manual scans preserve their existing policy; disconnected roots return an error.
Implementation-first: actual Chrome, Firefox and Android
acceptance is deferred, and existing evidence does not verify this feature.

The existing scan service owns manual, watch and scheduled requests. No second
scanner or lifecycle owner. Automation defaults off. Operator settings are
`SCAN_WATCH_SECONDS` (0 or 10–86400; polling, not native filesystem events) and
`SCAN_INTERVAL_MINUTES` (0–10080). Configuration is restart-persistent through
the deployment environment, like existing background worker settings. Roots
remain in Admin › Libraries. Both clients show effective settings and exact
environment setup instructions; they do not pretend to edit deployment files.

Web Admin (existing modal, same typography, buttons and theme tokens):

```
[Trigger rescan] [Cancel scan] [Scan settings ▾]   (other admin actions)
Scanning Library: 18/50 files · 1/3 sets · manual
  or: Idle / Complete / Cancelled / Failed — check root access, then rescan
Scan settings (collapsed initially)
  Watch: Off / polling every N seconds
  Schedule: Off / every N minutes
  [setup instructions, limits and safe missing-file behavior]
```

Status is quiet, persistent text inside Admin; the existing compact background
indicator remains. Errors stay visible without repeated toasts. Native buttons,
Tab/Enter, and existing Escape/focus return apply; no new global shortcuts.
Details disclosure keeps setup out of routine listening. Long paths wrap; narrow
screens wrap actions. Permission failures use the existing admin authorization.

Android Admin › Rescan Library keeps its AppBar/Back and status card. A scrollable
column adds Cancel scan and an ExpansionTile called Scan settings, containing
effective watch/schedule values and selectable setup instructions. Native themed
controls retain touch targets and light/dark contrast. Refresh retries offline
or permission errors. Back leaves server work running; explicit Cancel stops it.

Transitions: idle → running immediately; repeated manual requests cancel the
active run and coalesce into one replacement, maintaining existing API behavior.
Watch/schedule requests coalesce into one follow-up without cancelling work.
Cancel clears that follow-up, marks cancelling until the worker exits, then
cancelled. A later source change/schedule/manual request may run again. Failures
retain a safe actionable message; details are logged on the server.

Polling computes a constant-size source signature, including names, size and
mtime for supported media and playlist/lyrics sidecars, ignoring hidden/generated
artwork and configured caches. It detects changes after a complete successful
sweep, coalesces bursts and invokes the existing **complete inventory** scan. There is no
partial changed-directory reconciliation: unchanged rows must never be treated
as missing merely because a watcher did not visit them. Failed/cancelled discovery
does not reconcile that set. Polling cannot detect a same-size rewrite with
unchanged mtime; manual rescans provide repair. Automatic inventory persists a
source size/mtime checkpoint in existing music JSON, skips unchanged audio, and
re-probes new/changed audio through the existing catalog writer. Pre/post-probe
stats must agree before a checkpoint is saved. No separate metadata database.
Successfully discovered sets contribute genuinely absent audio to the existing
identity matcher without trashing it. Unique matches preserve the old ID and user
state; ambiguous matches append an admin choice, leaving unmatched missing rows
intact and preserving unrelated conflicts. Skipped/unreadable directories are
excluded. Missing-record repair/purge remains the absent hl3 prerequisite.
An entire top-level set rename remains outside this matching boundary: the old
set is not successfully discovered, so its records remain alongside the new set.
This is the existing scanner limitation, not whole-root move recovery.
Large/network roots
cost one metadata traversal per poll; no per-file content hashing or inotify
dependency. One poll sweep at a time, no event queue, one pending scan maximum;
probe workers and work queues are capped. The existing per-set discovery/row
snapshots remain proportional to the largest set (not a fixed-memory guarantee).

Shutdown cancels and joins scanning/observation before database close. Scans
retain the existing timeout. Cancellation cannot interrupt a blocked OS syscall.

## Integrated acceptance (2026-10-27)

Disposable fixture at `127.0.0.1:18851` with `SCAN_WATCH_SECONDS=10` and
`SCAN_INTERVAL_MINUTES=1`, server 0.8.2 / build `8691244…-dirty`, checkout
`584ba07`. Commands live in the component E2E READMEs. Evidence stays outside
git under `/home/paul/tmp/codex/player-a06d-evidence`.

| Platform | Result | Evidence |
| --- | --- | --- |
| Installed Chrome 144.0.7559.132 | PASS: scheduled completion, watch add/retag/retain-on-delete, keyboard manual rescan focus restore, soft-delete trash, root outage/recovery, narrow light Scan settings | `chrome/evidence.jsonl` (pass at 15:05:22Z) |
| Installed Firefox 156.0 | PASS: same journey | `firefox/evidence.jsonl` (pass at 15:17:40Z) |
| Native API34 x86_64 (`Player_Integrated_A_API34`) | PASS 10: Settings › Rescan Library Scan settings, schedule status, watch arrival browse, retag, retain-on-delete, Cancel while running, manual trash, root error/recovery, Back | `native-1793115144882205538/android/evidence.jsonl` |

Screenshots were visually reviewed: web Admin shows Scan settings / status without
horizontal overflow on the 390×844 light viewport; native Rescan Library shows
effective watch/schedule text and the watched arrival opens as `A06d Native Arrival`.
Admin focus restore after Trigger rescan is covered by the web suite (`admin.js`).

R1 fixes (bounded, 2026-10-25): inventory audio insertions persist an
`inventory_pending` provenance flag in existing music JSON. Re-probing preserves
it; retries reconstruct only observed flagged destinations, including after a
process restart. A successful complete inventory clears flags after matching and
conflict persistence. Unrelated stable rows never become retry candidates.
Automatic adoption runs in a SQLite transaction that locks writes before checking
play count, all media-owned records, saved queues, collection shares and podcast
links. If a destination acquired saved state, both records survive with an
explicit admin conflict instead of cascading its state away. Original ID, count,
resume, favorites and playlist occurrence remain on the original record. Conflict
listing now drains/closes its rows before reading candidate paths, preventing a
deadlock on the single SQLite connection exposed by that preserved-state path.

Observation resolves the configured trusted root before walking (MEDIA_ROOT and
registered symlink roots). Nested symlinks remain skipped, and ignored cache paths
are rebased through the configured root alias.

R1 red/green evidence outside the repository: `player-zl3-r1-red.txt` in
`/home/paul/tmp/` reproduces four interrupted-rename and two symlink-root failures.
The first green attempt exposed the conflict-listing deadlock; its owned test
process was stopped with SIGQUIT and its stack is retained in
`player-zl3-r1-green.txt`. The bounded rerun `player-zl3-r1-green-2.txt` passed;
expanded scanner checks passed in `player-zl3-r1-green-final.txt` (6.094s).
`GOMAXPROCS=2 go test -race ./internal/scanner ./internal/service -run 'TestInventoryInterruptedRenameRetry|TestSourceSignatureConfiguredSymlinkRoot|TestInventoryUpdatesActualMusicCatalog|TestScanAutomation' -count=1 -timeout 45s`
passed (scanner 9.009s, service 2.811s), retained in `player-zl3-r1-race.txt`.
`go test ./... -run '^$'` passed again after fixes. UI paths are unchanged in R1;
their earlier focused results are reused. Integrated platform acceptance remains
deferred.

Focused implementation checks (2026-10-25, working tree based on f605642):

- `cd player-server && go test ./... -run '^$'`: compile-only all packages passed.
- `go test ./internal/service ./internal/scanner ./internal -run 'TestInventory|TestScanAutomation|TestIncompleteDiscovery|TestSourceSignature|TestScanService|TestAdminService_TriggerRescan|TestLoadConfig' -count=1`: passed.
- Additional scanner processing-failure/checkpoint checks: `go test ./internal/scanner -run 'TestInventory|TestIncompleteDiscovery|TestSourceSignature|TestFailedSourceProcessing' -count=1`: passed. Real SQLite catalog assertions cover new/retagged albums/artists, no unchanged-audio probes, and rename retaining ID, resume, play count, favorite and playlist occurrence. Unmatched missing/trash and unrelated conflicts remain intact.
- `GOMAXPROCS=2 go test -race ./internal/service -run 'TestScanAutomation|TestInventoryUpdatesActualMusicCatalog' -count=1`: passed.
- `go test ./internal/api -run TestScanCancelAuthorizationAndAliases -count=1`: passed; both route prefixes check anonymous/listener/admin boundaries.
- `node --experimental-default-type=module web/js/tests/admin-rescan.test.js`: passed.
- `cd player-android && flutter test test/screens/admin_rescan_screen_test.dart`: 11 passed, including automatic origin/queue, Cancel and effective settings.
- `flutter analyze --no-pub lib/api/player_api_client.dart lib/api/dio_player_api_client.dart lib/screens/admin_rescan_screen.dart test/screens/admin_rescan_screen_test.dart`: no issues.
- `errcheck ./internal/scanner ./internal/service ./internal/api ./internal/app ./internal/repository ./internal`: nonzero, 248 existing diagnostics; none on added/changed lines. Full output outside repository at `/home/paul/tmp/player-zl3-errcheck.txt`.
- `git diff --check`: clean. Full regression, final server/APK builds, screenshots/theme/narrow-screen and actual installed Chrome/Firefox/native-emulator feature acceptance were deliberately not run in this implementation phase. No release/version/push/deploy or live runtime changes.
