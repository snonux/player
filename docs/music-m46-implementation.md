Stage: IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION.
Two bounded expert rounds complete; R1 keyboard/playback-independence fixes passed R2.
Late selected-menu focus restoration fixed and regression-tested without a third review.

# M46 bulk music downloads (pk3)

Implementation stage: awaiting bounded review and integrated verification.
Predecessor album/playlist/selection code is reused; no release/version changes.

Interaction design recorded before edits in the task design/path proposal:
collection header actions gain Download… (web) / Keep offline (native), beside
existing queue actions. Selected-song menus offer the same action. Selection
order resolves through the existing collection expander, then repeated IDs are
saved once. No new shortcuts; native inputs and existing vi navigation remain.

Web layout: existing theme-token modal → unique-song count + explicit Original
ZIP policy + 500-song/512 MiB limit + Download ZIP / Close. Preparing state
checks access/builds an atomic archive; downloading state shows received bytes
and Content-Length progress; Cancel/Escape closes the request and restores
opener focus. Success names the archive and directs users to browser downloads.
Failure provides Retry; oversized selections require smaller groups. Extracted
originals need a compatible player; no browser offline-library claim.

Native layout: standard AlertDialog → unique-song count + original/compatibility
policy + Keep offline / Close. Running state shows aggregate saved/failed count,
current song and received bytes; Cancel downloads retains successes, Hide
progress returns to listening. Downloads shows the aggregate alongside existing
per-file progress and saved files. Failed or canceled batches offer retry;
already saved files are skipped. Back dismisses the dialog without stopping
foreground work. Credential generation changes cancel pending/current work.
Empty/connection/permission/storage failures have concise messages. Long names
wrap or ellipsize; existing Material dialog scrolling, tap targets, theme and
web theme tokens are reused. Light/dark/narrow actual renders remain unverified.

Atomic web archives intentionally fail together if any source becomes unavailable
before serving. Native failures are per-song and retain completed copies.
Downloads remain separate from automatic streaming caches and streaming-quality
preferences. No collection syncing/background scheduling/byte-range resumption.

Verification commands/results will be recorded after focused checks. Full suites,
final builds and actual installed Chrome/Firefox/native Android acceptance are
deferred by the implementation-first user instruction. Required acceptance:
album/playlist/selection; dedupe; cancellation/retry; real access and account
changes; large collection limit; interrupted transfer; offline playback of every
saved entry; keyboard/focus/typing and touch/Back; light/dark/narrow.

Focused checks (2026-10-23, pk3 working tree over acbe057):

- `go test ./internal/service -run TestMusicDownloadArchive -count=1`: PASS.
  ZIP preserves original bytes, deduplicates IDs, confines source opening,
  rejects forbidden sources, cancellation and excessive item count.
- `go test ./internal/service ./internal/api -run 'TestMusicDownload(Archive|sHTTP)$' -count=1`:
  service PASS; API compile temporarily blocked by concurrent enrichment Store
  additions (`mockPingStore` missing `GetMusicEnrichment`). API check awaits
  that lane's test-adapter update; no API-test success claimed yet.
- `flutter analyze --no-pub` on six changed download/collection/album paths:
  PASS, no issues. `flutter test --no-pub test/services/offline_downloads_test.dart`:
  26 PASS, including three bulk checks. Actual loopback HTTP downloads verify
  partial-failure retention, duplicate suppression and retry fetching only the
  missing copy. Existing tests cover original/compatibility policy, credential
  isolation, cancellation, interrupted/oversize bodies and offline persisted
  copies. These are service tests, not Android platform acceptance.
- `node player-server/web/js/tests/music-downloads.test.js` and
  `node player-server/web/js/tests/collection-resolver.test.js`: PASS.
- `git diff --check`: PASS at this stage.

No Chrome/Firefox journeys, native emulator journeys, screenshots, audio checks,
full regression suites, final builds, or errcheck run for this slice yet.

Bounded review round 1 found two web blockers, both repaired with failure-before /
success-after checks: the dialog's local Tab handler doubled the existing
shared document-capture focus trap, and collection download resolution inherited
playback-intent cancellation. The dialog now delegates Tab to the shared trap;
only queue actions require the same playback intent. Download resolution still
cancels on account/navigation/owner changes. New tests exercise actual capture
then bubble dispatch, Tab/Shift+Tab/Escape/opener focus, deferred resolution with
track intent changes, and account/navigation cancellation. Commands
`node player-server/web/js/tests/music-downloads.test.js` and
`node player-server/web/js/tests/playback-source.test.js` PASS; the latter also
keeps existing queue supersession and selection cancellation regressions passing.
Root reported the API download test passed in the enrichment worker's focused Go
invocation after its Store test adapters became coherent. No native code changed
for the web review fixes, so the passing 26-test native service run/scoped
analysis was retained. Round 2 is restricted to these confirmed blockers.

Round 2 confirmed both review blockers resolved, with no further review round.
A final narrow self-review composition fix prevents the selected-song popover's
queued close callback from stealing focus from a newly opened download dialog.
The existing playback source suite now invokes that real popover callback after
opening the dialog and checks focus stays in the modal and restores to the
selection opener on Close. The focused Node suite passes with this repair.
