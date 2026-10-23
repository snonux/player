# Music M17: portable saved session

Pre-code interaction plan (ll3), following U01 sections 5.3/8.4 and the existing
M16 occurrence-based local recovery. Prerequisites nj3/oj3/yk3 are completed;
zh3 coordinator is reviewed implementation 2a7b1f7 awaiting integrated verification.
The implementation-first user instruction permits this dependent slice without
changing task dependencies.

The existing Now Playing panel/sheet adds labeled Continue session and Replace
saved session controls below its existing queue status, using native buttons,
wrapping layout, theme colors and normal keyboard/touch/Back behavior. Continue
confirms replacing a populated queue with Keep this queue / Replace, validates
fresh media access, then applies the selected occurrence and exact position
paused. Resume queue remains the explicit playback action. An empty saved
session is a valid saved clear. Stale writes display Another client saved a
session with Replace saved session / Keep other session choices. Keeping the
other session leaves local playback and local M16 recovery untouched. A future
local edit still requires an explicit Replace decision.

No new keyboard shortcut is allocated. Native Enter/Space activate focused
buttons; existing row j/k/arrows, Enter and Escape/Q retain their roles. Browser
confirmation returns focus to the initiating control; Android uses Material
confirmation with native Back. Loading disables new session actions only;
offline/auth errors keep pending data and show quiet, readable status. No modal
opens for background failures. Titles/status wrap within existing narrow/light/
dark sheet styles. Actual theme/focus/accessibility results remain unverified.

GET/PUT /api/v1/queue and /api/queue are authenticated per-account session
snapshots. A revision and quoted If-Match enforce atomic optimistic writes;
409 includes the authoritative saved session. Repeated media IDs have separate
positive occurrence IDs. Ordered entries, current occurrence, seconds, source
label and existing repeat/shuffle/canonical/Up next settings travel; credentials,
URLs, playing state and ownership never travel. The last mutation identifier
acknowledges retries of a lost response without creating another revision.

Server SQLite stores a bounded snapshot in one user-owned row, cleaned by
foreign keys, explicit user deletion and orphan startup cleanup. Writes validate
all audio permissions before persistence. Clients revalidate media when Continue
is chosen, drop unavailable tracks, select a surviving occurrence at zero if
the selected one disappeared, and preserve exact short-song positions otherwise.

Web persists pending edits and base revisions with origin/account scoping and
quiet lifecycle/online retries, throttling periodic sends. Android persists
pending edits in scoped preferences and contributes one queue domain to zh3's
global coordinator, including pending count and full-success accounting. It
adds no separate native retry timer. Recovered local queues are baselines, so
opening a client never silently overwrites another saved session. Explicit
Continue validates account credential generation, queue mutation/recovery
generation and source/playback intent through async reads. Remote responses
only update sync storage; they never navigate or jump active playback.

Focused checks passed: Node `web/js/tests/queue-sync.test.js` covers ordered
repeated occurrences, stale conflict, restart, Keep/Replace choices, late
account response and lost acknowledgement. Flutter scoped analysis of eight
affected files is clean; `test/services/saved_queue_sync_test.dart` passes all
three durable/offline/conflict/baseline/late-credential tests. Go
`go test ./internal/repository ./internal/api -run TestSavedQueue -count=1`
passes repository CAS/retry/restart/isolation/deletion and both-prefix API
authentication/precondition/409 checks. `git diff --check` is clean. These are
focused coherence checks, not platform acceptance. Root owns bounded review,
implementation commit and the pending integrated-verification task marker.
Full suites, final builds, Chrome/Firefox and native emulator acceptance are
intentionally deferred to integrated verification. Required journeys remain:
cross-browser/native save and paused continuation, repeated current occurrence,
position, reorder/repeat/shuffle/Up next preservation, offline restart/replay,
stale conflicts and both choices, permissions/deletions, auth/account switching,
late queue/source responses, actual resumed audio, keyboard focus and touch/Back.
No release, version bump, push or deployment is included.

R1 found one blocker in native continuation: an edit after restored rows became
visible but before preference writes completed could be mistaken for the
restored baseline and lose its pending upload. The fix returns the actual
applied snapshot with a post-flush unchanged flag, retaining source/account/
recovery guards. Adoption compares an edit stamp captured before restoration;
newer local captures advance it synchronously before any async load. Adoption
keeps newer pending/local contents and sets their base to the observed remote
revision, with no pending mutation after its save await. A narrow preference
write dependency makes the actual provider delay deterministic in regression
checks. Both repeat and reorder edits during the delayed flush survive restart
and subsequently upload; a separate adoption/load race test retains pending
work. Five-file scoped analysis is clean, all four sync tests and only those
two provider recovery regressions pass. Root owns the bounded second review.

Stage: **IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION**. Both
bounded expert rounds completed. Continue retains the actual applied snapshot
and an edit stamp through persistence; adopting the server session cannot
clear a newer repeat/reorder edit. Four sync tests and two actual provider
delayed-write regressions pass, including restart and upload of the retained
edit. Five-file affected analysis is clean. The final narrow review found no
new blocker. Full gates/builds and actual cross-client/platform journeys remain
deferred until backlog implementation is complete; task ll3 stays pending.
