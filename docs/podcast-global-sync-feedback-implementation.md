# Global synchronization feedback (4i3)

Design approved before source edits: reuse Android's GlobalSyncCoordinator and
its existing durable podcast/media/progress/queue services. Add a compact global
status and labeled Sync now/Retry action, with a sign-in instruction for blocked
authentication. Refresh persisted account status before presenting it after an
account switch. Standalone use has no synchronization hint.

The web adds a thin coordinator around existing episode/subscription retries,
paginated podcast pull, listening progress and saved queue synchronization.
Domain results must expose swallowed failures and outstanding starting edits.
Only all-domain success advances the per-account persisted full-success clock;
partial runs preserve it and retain each successfully applied pull page. Podcast
pending edits remain tab-memory only; wording must say so. Queue reconciliation
only updates storage, never live playback. Account generations guard every
asynchronous continuation. Activity/pending/never/48-hour feedback is quiet,
nonblocking and available with ordinary keyboard and touch controls.

Prerequisites zh3 (2a7b1f7), 2i3 (0c99cec) and 3i3 (73f3520) are implemented and
reviewed, awaiting integrated verification. Focused clock, partial failure,
account and UI checks run during implementation. Full suites, server/APK builds,
Chrome/Firefox and two installed Android clients remain deferred. No release,
version bump, deployment or live runtime mutation is authorized.

Implemented: Android's app builder adds a themed, safe-area status/control below
navigation, including during audio playback. It reuses the existing provider;
there is no second engine. Account changes clear the previous presentation
before asynchronous durable status loading. Late-run exceptions use the
captured account lease. Explicit retry still includes gone media; saved music
sessions now expose policy/conflict/failure outcomes and retrieve remote storage
without adopting it. Offline music-play rows are included in the pending count.
Authentication uses a clear sign-in-required instruction instead of an
ineffective Retry while the existing coordinator blocks that account.

The web exposes a header action and compact status above the library. It runs a
full pass at startup after queue restoration, while the tab is visible at a
modest one-minute interval, and on reconnect/foreground. Playback position
events refresh counts without launching all-domain passes. Auth failures stop
automatic global attempts and show Sign in. Background podcast and listening
401s now retain status codes without redirecting current playback. A partial
run preserves the prior full-success clock; committed podcast pages preserve
their cursor. Unsupported podcast capability leaves supported domains working.
Pending podcast edits remain tab-memory only, explicitly described in the UI.
Listening samples and saved sessions continue using existing localStorage.
Status metadata is keyed by origin/account and never stores credentials.

Existing episode state receives storage-only global pull/alias updates. Clean
registers unconfirmed after an epoch change replay at their original clocks;
interrupted recovery markers remain in account-scoped tab memory. No new browser
history database was introduced. Subscription retries capture their generation.
Listening flushes capture their starting IDs so ongoing playback cannot starve
a global run. Queue global pulls preserve the local revision baseline, ensuring
a later local edit cannot silently overwrite a newer remote session. Global
status, domain failure metadata and pending IDs are exposed for subsequent
automatic offline work (sl3); no closed-tab/background scheduling is promised.

Focused checks, serialized:

- From `player-android`: `flutter test --no-pub --concurrency=1
  test/widgets/global_sync_status_test.dart test/services/podcast_sync_test.dart
  test/services/saved_queue_sync_test.dart`: **18 passed**. Includes real SQLite
  metadata refresh and an account switch during a blocked run, retained partial
  timestamps, pending counts, queue policy/conflict propagation, exact 48-hour,
  negative-age, never/standalone presentation and the labeled touch callback.
  One intermediate run exposed the new queue-conflict test receiving a generic
  StateError; the wrapper now maps it to the specific conflict outcome and the
  final focused run passes. Widget checks are not installed Android proof.
- Scoped `flutter analyze --no-pub` over the six changed Dart source files and
  three test files: **no issues**. An earlier run found three brace-style infos;
  those were fixed. Dart formatting is clean.
- From `player-server`: nine Node suites passed: `global-sync`,
  `global-sync-view`, `podcast-global-sync`, `queue-global-playback`, `queue-sync`,
  `listening-progress`, `podcast-subscriptions`, `podcast-episode-state`, and
  `podcast-playback-state` (each `node web/js/tests/<name>.test.js`). Tests cover
  full/partial/persisted/account status, exact clocks, new versus starting edits,
  quiet actual API 401 behavior, real view click handlers/auth links, actual
  podcast retries/pagination/alias replay and interrupted epoch recovery. The
  actual queue module executes against a monitored playback port: remote pulls
  leave active source, position, playback, queue and intent unchanged; subsequent
  local edits still encounter a real revision conflict. These are implementation
  checks, not browser playback acceptance.
- Changed JS syntax checks and `git diff --check` passed. No Go source changed,
  so Go compile/vet/errcheck/full-suite gates were not run for this slice.

Self-review: account leases guard transport continuations; presentation drops
old-account timestamps before loading; queue/listening counts and actions are
filtered to the active account. Per-episode success never advances global full
success. Failed/rejected/policy/conflict steps cannot advance it; an unsupported
podcast capability is excluded while supported domains continue. Pending edits
created during a successful run can remain visible. Playback owns all source
and seeking decisions; sync reaches storage only. Controls preserve native
buttons/links, do not move focus, use theme tokens/native themes and produce no
dialogs, sounds, toasts or playback notifications. The new web coordinator and
view are small separate modules over the existing domain services; SOLID review
found no demonstrated need for a new history layer or broad abstraction.

Initial freeze for parent fresh R1. Original 33-path worker manifest:

- `docs/podcast-global-sync-feedback-implementation.md`
- `player-android/README.md`
- `player-android/lib/main.dart`
- `player-android/lib/providers/global_sync_provider.dart`
- `player-android/lib/services/global_sync_coordinator.dart`
- `player-android/lib/services/podcast_sync.dart`
- `player-android/lib/services/saved_queue_sync.dart`
- `player-android/lib/widgets/global_sync_status.dart`
- `player-android/test/services/podcast_sync_test.dart`
- `player-android/test/services/saved_queue_sync_test.dart`
- `player-android/test/widgets/global_sync_status_test.dart`
- `player-server/README.md`
- `player-server/docs/api.md`
- `player-server/docs/podcasts.md`
- `player-server/web/css/components.css`
- `player-server/web/index.html`
- `player-server/web/js/api.js`
- `player-server/web/js/app.js`
- `player-server/web/js/global-sync-controls.js`
- `player-server/web/js/global-sync-view.js`
- `player-server/web/js/global-sync.js`
- `player-server/web/js/listening-progress.js`
- `player-server/web/js/podcast-episode-state.js`
- `player-server/web/js/podcast-global-sync.js`
- `player-server/web/js/podcast-subscriptions.js`
- `player-server/web/js/queue-sync.js`
- `player-server/web/js/queue.js`
- `player-server/web/js/tests/global-sync-view.test.js`
- `player-server/web/js/tests/global-sync.test.js`
- `player-server/web/js/tests/listening-progress.test.js`
- `player-server/web/js/tests/podcast-global-sync.test.js`
- `player-server/web/js/tests/queue-global-playback.test.js`
- `player-server/web/js/tests/queue-sync.test.js`

Remaining gaps: full regression suites, final server/APK builds, actual Chrome
and Firefox keyboard/focus/touch/light/dark/narrow status and playback-retry
journeys, and both installed native Android clients (including actual footer
layout, account/restart/foreground/outage and playback continuing during Retry).
Native authentication behavior and real storage-denial/network-policy boundaries
also need integrated acceptance. No adb, live server/emulator, account,
credential, runtime-data, version, release, push or deployment changes were
made. Existing `.serena/`, `reports/` and `research_notes/` remain untouched.
The worker did not stage, commit, annotate/close the task or spawn reviewers.

## R1 blocker response — frozen for the sole R2

Both confirmed blockers were fixed narrowly. Global podcast polling no longer
dispatches `library:changed`, whose production app listener reloads the ordinary
grid. The pull emits a podcast-only subscription event when returned registers
actually differ; only the existing sidebar refresh listens. Identical sidebar
data retains its DOM; changed data restores its focused feed action by stable
key. Episode pulls notify only when registers/aliases actually change. Existing
episode-card updates keep selection and restore the focused action, without an
ordinary library reload. This is verified through the app's real event
registration, real global pull and real podcast view/sidebar/selection modules.

Native synchronization uses a per-request `quietUnauthorized` option on the
same Dio/interceptor stack. The existing production UnauthorizedInterceptor
passes a sync 401 back to its caller before destructive invalidation. Ordinary
interactive clients keep the existing 401 behavior. Captured global and offline
leases, podcast preplay/end leases (through captureSyncAccount), the background
ProgressQueue client and saved-session GET/PUT requests all use quiet options.
Bearer injection, credential epochs and current-account checks are unchanged;
the quiet option never bypasses the auth request interceptor or server ACLs.

The new native regression uses production DioClient/UnauthorizedInterceptor,
DioPlayerApiClient, captured global/saved-session provider requests, real SQLite
progress/offline/podcast state, real GlobalSyncCoordinator and
PlaybackSessionCoordinator, and the real AuthStateNotifier unauthorized cleanup.
Sync config/podcast/media/progress/queue 401s preserve credentials, pending work
and the active server playback lease while reporting `auth`. A normal interactive
401 still invokes actual unauthorized cleanup, clears progress and credentials,
and stops the server playback lease. A delayed old-account response preserves
the newer credential; the old lease cancels before another dispatch. Thus this
is not just a mocked onUnauthorized callback test.

Focused R1 checks (serialized):

- `flutter test --no-pub --concurrency=1 test/api/global_sync_auth_test.dart
  test/api/playback_sync_api_test.dart test/services/saved_queue_sync_test.dart
  test/widgets/global_sync_status_test.dart`: **12 passed**.
- Scoped analysis of the six changed native source files plus the new auth
  regression: **no issues** after removing one unused test import.
- Node `global-sync-app-events`, `podcast-global-sync`, `podcast-playback-state`,
  `podcast-views-play` and `global-sync`: **all passed**. The app/event regression
  checks unchanged repeated polling, changed subscription/sidebar data, changed
  visible episode progress, ordinary music selection/focus and focused episode
  and sidebar controls. Two intermediate fake-DOM failures were corrected: the
  expected time format is `00:30`, and removed fake cards need `remove()` during
  account cleanup. Neither required a production change.
- Changed JS syntax checks and `git diff --check` passed. No full suites,
  builds, browsers, emulators, live runtimes or account mutations were run.

R1 self-review: ordinary grids receive no polling invalidation, while committed
subscription and episode updates still reach their scoped views. Sidebar async
responses also check their account generation. Quiet auth is a request option,
not another client engine or a broad authentication rewrite; it preserves the
existing transport/header/epoch protection and leaves interactive invalidation
intact. Both blockers have a regression through the actual affected event or
interceptor boundaries. All previously documented platform/full-gate gaps remain.

Exact R1 delta: **12 paths**:

- `docs/podcast-global-sync-feedback-implementation.md`
- `player-android/lib/api/dio_client.dart`
- `player-android/lib/api/dio_player_api_client.dart`
- `player-android/lib/providers/global_sync_provider.dart`
- `player-android/lib/providers/offline_playback_sync_provider.dart`
- `player-android/lib/providers/progress_queue_provider.dart`
- `player-android/lib/providers/saved_queue_provider.dart`
- `player-android/test/api/global_sync_auth_test.dart`
- `player-server/web/js/podcast-episode-state.js`
- `player-server/web/js/podcast-global-sync.js`
- `player-server/web/js/podcast-views.js`
- `player-server/web/js/tests/global-sync-app-events.test.js`

The full frozen manifest is **41 paths**: the original 33 paths above plus
these exact eight additions:

- `player-android/lib/api/dio_client.dart`
- `player-android/lib/api/dio_player_api_client.dart`
- `player-android/lib/providers/offline_playback_sync_provider.dart`
- `player-android/lib/providers/progress_queue_provider.dart`
- `player-android/lib/providers/saved_queue_provider.dart`
- `player-android/test/api/global_sync_auth_test.dart`
- `player-server/web/js/podcast-views.js`
- `player-server/web/js/tests/global-sync-app-events.test.js`

Frozen for the sole R2. No additional reviewers, task writes, staging, commits,
version changes or runtime mutations were performed by the worker.

## Final R2 response — frozen for parent critical inspection

R2 confirmed the original two blockers resolved, including native quiet auth,
but found two new blockers in the sidebar fix. Both permitted fresh review
rounds are now used; no third review was spawned or requested. The fixes below
are awaiting the parent's final critical inspection and focused verification,
not claimed as fresh-review clearance or platform acceptance.

Background `podcast:subscriptions-changed` sidebar refreshes now pass an explicit
quiet-auth option through the actual `API.podcasts` helper. A background 401
therefore cannot navigate away from playback. Ordinary interactive calls keep
their existing login redirect. Sidebar failure continuations check the captured
account generation before touching the DOM. A failed background refresh retains
previously successful controls, fingerprint and focus; an initial/rendered error
has its fingerprint removed so an identical later success reconstructs controls.
Each refresh also captures a request revision, so overlapping initial/event
requests in the same account cannot apply an older success or failure after a
newer response. The regression covers both same-account stale outcomes and
preserved focused controls.

The existing actual app/event integration regression now restores the production
API helper and drives real fetch responses through the registered sidebar event.
It verifies background 401 without navigation, success → failure → identical
success retaining the same controls and focus, initial error → success rebuilding
controls, and an old-account failed response arriving after a new-account
successful sidebar. It also verifies an ordinary interactive 401 still redirects.
The earlier music selection, focused episode progress and sidebar update checks
remain in the same passing regression.

Final targeted checks: six Node suites passed (`global-sync-app-events`,
`podcast-global-sync`, `podcast-playback-state`, `podcast-views-play`,
`global-sync`, `podcast-subscriptions`), plus changed JS syntax checks and
`git diff --check`. No native source changed in this response; the previous
12-test native/interceptor result and clean scoped native analysis are reused.
No full suites, builds, platform checks or live runtime/account changes ran.

Final self-review: the quiet option is supplied only by the background sidebar
path and defaults off for normal callers. Both success and error continuations
check account generation. A fingerprint describes successful rendered controls;
preserving successful DOM on a background failure keeps that relationship true,
and deleting it before an initial error ensures recovery cannot incorrectly
short-circuit. The targeted test uses the production event listener, API helper,
response status handling and sidebar renderer, rather than replacing
`API.podcasts` for these failure cases.

Exact final-response delta: **4 paths**, all already in the full **41-path**
manifest above (no additions to that manifest):

- `docs/podcast-global-sync-feedback-implementation.md`
- `player-server/web/js/api.js`
- `player-server/web/js/podcast-views.js`
- `player-server/web/js/tests/global-sync-app-events.test.js`

Frozen for parent inspection. The original full/platform verification gaps
remain. No further review agents, task writes, staging or commits were performed.

Stage: **IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION**. Both review
rounds are used. Parent inspected the final sidebar request/account guards and
reran `global-sync-app-events`, `podcast-global-sync` and `global-sync`; all
passed. No third review ran. The documented full-suite and platform gaps remain.
