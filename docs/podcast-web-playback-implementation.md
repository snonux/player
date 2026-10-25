# Web podcast lifecycle (3i3)

Proposal before source edits: expose stable episode registers on authenticated
media detail after the existing access check. Reuse the web episode-state module
for independent position/completion edits, account-scoped in-memory retries and
a three-second bounded scoped reconciliation before loading a source. Late
responses merge registers only. Loaded HTML duration owns the five-second
start-over rule. Natural HTML ended writes both registers together at one clock
before starting an unawaited push; seeking, pause and errors cannot complete.

Use the existing owner/admin DELETE media soft-trash path for Delete downloaded
file. Explain shared server scope, seven-day trash and the eventual loss of
media-scoped notes/bookmarks/shares; retain subscriptions and episode registers.
Physical cleanup, confinement, thumbnails and transcoding stay with existing GC.
Keep subscription tombstones and admin Remove from server separate. Card/filter
updates adopt authoritative registers, including acknowledged superseded edits.

Only focused SQLite/service/HTTP/Node checks and scoped Go analysis run during
implementation. Full suites/builds, Chrome/Firefox and two installed Android
client checks remain deferred by the user's implementation-first workflow.

Implemented: authenticated media detail adds stable identity and raw registers
after the existing access check. Browser navigation and source startup share
the bounded preparation helper; direct library/deep-link/retry and authenticated
detached loads discover episode identity. Share playback stays public and does
not write account state. Podcast sources use the existing HTML transport;
podcast sets/candidates are excluded from gapless preparation. Cast keeps
positions and explicitly does not turn receiver transitions into podcast
completion. Notes/bookmarks/favorites/shares remain media-scoped.

The existing episode-state module now owns account-scoped in-memory registers,
exact-id retries, per-register authoritative acknowledgements, alias-collision
merges and generation leases. Account changes clear state and notify same-origin
tabs/popups. Late reconciliation has no element reference and cannot recreate
old-account storage. Detached loads await their existing authenticated config
and discard the opener's process-local lease before capturing the popup's own
generation. Ordinary listening statistics still use the existing queue at the
same observed clock; they never send a podcast legacy finish. Natural
end commits locally before unawaited sync, using one clock newer than both
registers. The native terminal position proves forward playback after the last
start/seek target, including a final fraction of a second with no timeupdate.
Paused native properties at end do not invalidate prior playback intent.

Episode views retain their location/filter, update progress and remove completed
rows after the end handler selects the next card. Buttons keep native focus;
removal moves to a neighbor/filter. Manual completion and subscription feedback
adopt server truth even when an edit was acknowledged but superseded. Replay
and Delete downloaded file are visible text buttons. The delete action uses
only DELETE media; a failed/lost reply rechecks availability, keeps retry controls
when the copy is still live, and leaves physical cleanup to existing Trash/GC.
There is no new file deletion service or parallel history.

Hooks for task 4i3: `podcastSyncStatus` reports this domain's account/generation,
pending count, last error and **lastEpisodeSuccess** (not a global full-success
timestamp); `retryPodcastEpisodeEdits` retries known scoped state, and subscription
helpers expose `pendingPodcastSubscriptions` / `retryPodcastSubscriptions`.
The virtual view's quiet hint is tab-local. Global coordination, stale/never-
synced presentation and final integrated evidence remain task 4i3/5i3 work.

Focused checks (serialized, GOMAXPROCS=2 for Go):

- `go test -p 1 ./internal/repository ./internal/service ./internal/app -run
  '^(TestPodcastWebLifecycleDetailAndTrash|TestMediaService_GetMediaDetail|TestGCWorker_SkipsDBHardDeleteWhenFileRemoveFails|TestPodcastService_DownloadEpisode_Success)$'
  passed. Repository compiled with no matching tests. The new app check uses
  real HTTP routing/auth, service and SQLite: state predates copy creation,
  both API prefixes expose identity, unauthorized readers/viewer deletion fail,
  admin Trash preserves episode/history/subscription, and actual ENOTEMPTY
  cleanup failure retains its link until a successful GC retry.
- `go vet -p 1 ./internal/repository ./internal/service ./internal/app` passed.
  `errcheck` on those packages ran and reports 229 existing diagnostics in
  untouched files; none in changed Go files after fixing two test calls.
  It is not a clean package-wide errcheck result. No full build ran.
- Node: podcast-playback-state, podcast-episode-state, podcast-subscriptions,
  podcast-views-play, podcast-views-api, podcast-episodes-api, playback-source,
  cast, detach-repeat and navigation-url passed. The source harness exercises
  actual player wiring with realistic paused-terminal pause/ended ordering,
  exact-end seek rejection, final-fraction advancement, duplicate end, loaded
  duration and account changes. Register tests cover blocked end transport,
  bounded fallback, late updates, retries, superseded acknowledgements and
  canonical collisions retaining newer pending target registers. Existing
  playback-source deliberately logs its injected Cast start failure while the
  test passes; this is not a real receiver check.
- Syntax checks for changed JS modules, gofmt and `git diff --check` passed.

Self-review: source ownership and account leases guard asynchronous work;
registers merge independently, exact pending IDs survive retries, and natural
end cannot wait for network work. Physical deletion stays behind existing ACL,
path, thumbnail/transcode and GC rules. The SQLite test confirms a failed unlink
does not discard the episode/media retry link. No new broad storage/service
abstraction or alternative player was added. The existing filter pagination
does not refill a partially emptied page; actual focus/touch, detached, Cast,
cross-browser/native decoder ordering and two-device journeys still require
integrated verification. Tests do not prove those platform behaviors.

No full suites, final server/APK builds, browser/emulator/device tests, live
runtime mutations, versions, releases, commits, task closure, deployment or
untracked personal notes were touched. Frozen for parent fresh-context R1;
implementation is awaiting review and integrated verification.

Exact worker manifest:

- `docs/podcast-web-playback-implementation.md`
- `player-server/README.md`
- `player-server/docs/api.md`
- `player-server/docs/podcasts.md`
- `player-server/internal/app/podcast_web_lifecycle_test.go`
- `player-server/internal/repository/mock_podcast_sync.go`
- `player-server/internal/repository/podcast_episode_register.go`
- `player-server/internal/service/browse.go`
- `player-server/internal/service/service.go`
- `player-server/web/js/app.js`
- `player-server/web/js/cast.js`
- `player-server/web/js/detach.js`
- `player-server/web/js/detachPopup.js`
- `player-server/web/js/playback.js`
- `player-server/web/js/podcast-episode-state.js`
- `player-server/web/js/podcast-playback.js`
- `player-server/web/js/podcast-detached.js`
- `player-server/web/js/podcast-subscriptions.js`
- `player-server/web/js/podcast-views.js`
- `player-server/web/js/podcasts.js`
- `player-server/web/js/tests/playback-source.test.js`
- `player-server/web/js/tests/cast.test.js`
- `player-server/web/js/tests/podcast-detached.test.js`

Stage: **IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION**. R2 confirmed
all three R1 blockers resolved with no new blocking findings. Both permitted
review rounds are used. Full gates and the documented platform checks remain
pending; this does not close task 3i3.
- `player-server/web/js/tests/podcast-episode-state.test.js`
- `player-server/web/js/tests/podcast-playback-state.test.js`
- `player-server/web/js/tests/podcast-subscriptions.test.js`
- `player-server/web/js/views/playback-nav.js`

R1 response (three confirmed blockers; refrozen for the sole R2):

- Cast preparation holds candidate episode identity/lease locally. It adopts
  those only together with the active media id after ticket/receiver LOAD
  succeed and current ownership checks pass. Delayed/failed replacement leaves
  the previous source's timer, positions and end policy intact.
- Detached transfers preserve stable account identity while dropping process-
  local leases in both directions. The receiving source captures its own lease;
  a different account's return is rejected. The actual source harness now runs
  detach → popup close → reattach → loadMedia → startSource after several account
  generations, including a fresh popup lease of 1 and a wrong-account negative.
- A shared popup startup deadline covers fresh authenticated config discovery
  and scoped reconciliation together. Hung config starts from the transferred
  local snapshot at the deadline, with a null lease that never authorizes
  account writes after a late sign-in. Late config cannot mutate that source;
  an account change during discovery cancels it. Fresh discovery on every load
  avoids reusing a cached config across account switches. Authoritative signout
  clears account state; ordinary popup progress is also disabled for an
  unknown-account fallback.

Focused R1 checks: Node `podcast-detached`, `cast`, `podcast-playback-state`,
`detach-repeat` and `playback-source` passed. The deterministic deadline test
checks a full 3000 ms budget including config, remaining reconciliation budget,
normal startup, hung/late config, signout and account-switch negatives. Cast
tests exercise old A progress during delayed B ticket, failed ticket/LOAD,
ordinary replacement not synthesizing A completion, and successful B adoption.
The injected failure messages in the passing Cast tests are intentional. The
existing actual-popup harness now supplies authenticated config and awaits the
real asynchronous load boundary. Changed JS syntax and diff checks passed.
No Go files changed in R1; prior focused Go evidence and the documented errcheck
baseline are reused. Platform and full-gate gaps remain unchanged.

R1 self-review: candidate state cannot acquire an earlier receiver's media id;
lease transfer uses stable account identity rather than comparing generation
numbers from different windows. Unknown-account playback carries a null lease
through local source/popup progress paths and does not acquire permissions from
a late response. One deadline owns config plus sync, without sequential full
timeouts. No broader feature/runner work or additional review rounds ran.

Exact R1 delta (11 paths; full frozen manifest above now has 27 paths):

- `docs/podcast-web-playback-implementation.md`
- `player-server/web/js/cast.js`
- `player-server/web/js/detach.js`
- `player-server/web/js/detachPopup.js`
- `player-server/web/js/playback.js`
- `player-server/web/js/podcast-detached.js`
- `player-server/web/js/podcast-playback.js`
- `player-server/web/js/views/playback-nav.js`
- `player-server/web/js/tests/cast.test.js`
- `player-server/web/js/tests/playback-source.test.js`
- `player-server/web/js/tests/podcast-detached.test.js`
