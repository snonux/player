# Android podcast playback lifecycle (2i3)

Proposal before source edits: extend the existing playback request and handler
progress session with an explicit natural-end-only policy and an end-position
callback. Podcast completion writes both registers in one SQLite transaction
with the same monotonic clock. Ordinary media retain their existing completion
policy. Seek, pause, stop, errors and source replacement save positions only.

Podcast requests capture their browsing scope and a matching account credential
lease. Before starting a source, retrying it or resuming via native controls,
attempt the existing three-second bounded podcast synchronization; standalone,
locked and unsupported accounts use local state. Read reconciled state once;
late replies only update storage. Use the loaded player's duration for the
five-second start-over rule and preserve independent manual completion and
Play from beginning. Background persistence remains owned by the audio handler.

Save natural-end position and completion durably before starting an immediate
unawaited synchronization. Network failures cannot hold progress or the next
source. Scope/generation checks prevent another account from receiving work.

Verification is limited to focused inexpensive lifecycle, SQLite and existing
handler tests plus scoped analysis. Full suites, builds, Chrome, Firefox and
both installed Android clients remain deferred to integrated acceptance.

Implemented: `PodcastPlaybackRequest` explicitly selects natural-end-only
completion and supplies an atomic finish-at-position callback. The handler
passes this policy through all progress creation paths; other media keep their
95-percent policy. A loaded terminal position or a seek-generated decoder end
does not complete a podcast. The handler accepts forward playback from the
start/seek target observed in its position stream or terminal native event; pause,
stop and decoder error paths cannot synthesize completion. Back saves a sample
and leaves the same handler running in the background. Native replay creates
a fresh progress session; ordinary pause/resume synchronizes storage without
seeking an already active source.

Podcast requests capture the matching account scope/credential generation.
Initial playback and Retry read reconciled storage after the existing bounded
sync, using loaded native duration (publisher duration is the pre-load fallback).
Standalone/locked/mismatched scopes skip account transport. The account lease
guards late responses; the existing exact-ID/clock merge retains newer pending
edits. Completion persists position and completion at one common clock newer
than both previous clocks, before a detached immediate sync attempt. Failed or
blocked end sync cannot hold local progress or source transitions.

Focused checks from `player-android`, each with `--no-pub --concurrency=1`:

- `flutter test test/services/audio_progress_session_test.dart
  test/services/audio_handler_progress_test.dart`: final run 17 passed.
  Actual handler logic receives synthetic native processing/position/error
  streams: initial terminal events and seek-generated events are rejected,
  real advancement re-arms the end guard, background end uses native duration,
  replay starts a fresh session, and pause/resume does not seek live audio.
- `flutter test test/services/audio_handler_progress_test.dart
  test/services/podcast_sync_test.dart`: 18 passed, including common end clocks
  despite distinct existing clocks, durable pending IDs, late acknowledgements,
  independent manual registers and actual transaction rollback after switching.
- `flutter test test/services/podcast_downloads_test.dart
  test/services/audio_handler_errors_test.dart
  test/services/gapless_playback_test.dart`: 19 passed. Real SQLite/files and
  loopback publisher checks include restart, native vs publisher duration,
  three-second timeout, proof that a late page applied without replacing newer
  local position, captured old scope after switching, and atomic local end
  before blocked/failed detached network work. The final download-only portion
  ran again as part of the screen check below: 11 passed.
- Scoped `flutter analyze --no-pub` over the eight changed Dart source files
  and four changed test files: final run no issues. `git diff --check` passed.

The additional existing `audio_player_screen_test.dart` check passed 58/59
screen tests. **Failed server profile Quality recovery retains Original across
Retry failures** left zero-duration Dio → `musicAlbums` cache timers pending
in unchanged `playback_cache_provider.dart`. The single named rerun passed.
This is an observed intermittent test failure, not a claimed clean full screen
run or a proven preexisting failure; no cache runner changes were made.

Self-review: requests never acquire server media IDs or authenticated publisher
headers. Persistence stays in the existing repository/session; late responses
have no player reference. Completion clocks are atomic; independent manual
completion/position and Play from beginning stay durable. Shared handler error,
gapless and ordinary 95-percent completion regressions passed. Callback/provider
boundaries avoid putting transport or SQLite into the handler. No new engine or
state-management framework was introduced. Go-specific checks do not apply
because no Go source changed; SOLID review found no demonstrated structural
blocker requiring additional abstraction.

Frozen manifest for parent fresh R1 (no staging, commits or task closure by
worker):

- `docs/podcast-playback-lifecycle-implementation.md`
- `player-android/README.md`
- `player-android/docs/standalone-podcasts.md`
- `player-android/lib/providers/podcast_library_provider.dart`
- `player-android/lib/screens/audio_player_screen.dart`
- `player-android/lib/services/audio_handler.dart`
- `player-android/lib/services/audio_handler_gapless.dart`
- `player-android/lib/services/audio_progress_session.dart`
- `player-android/lib/services/playback_request.dart`
- `player-android/lib/services/podcast_downloads.dart`
- `player-android/lib/services/podcast_sync_store.dart`
- `player-android/test/services/audio_handler_progress_test.dart`
- `player-android/test/services/audio_progress_session_test.dart`
- `player-android/test/services/podcast_downloads_test.dart`
- `player-android/test/services/podcast_sync_test.dart`

Full regression suites, final server/APK builds, Chrome, Firefox and both
installed Android clients remain unverified. Actual decoder ordering, native
notification/background/restart behavior, screen Back-save and two-client
account/outage journeys require integrated acceptance. Unit/widget checks are
implementation evidence only. No adb, live servers/emulators, accounts, local
runtime data, versions, release, deployment or untracked personal notes were
changed. Prerequisites yh3/1i3/zh3 remain implemented/reviewed pending integrated
acceptance; this implementation does not close 2i3.

Stage: **IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION**. R2 confirmed
the terminal-position fix resolves the R1 blocker with no new blockers. Both
permitted review rounds are used; platform checks and full gates remain pending.

R1 blocker response: natural playback can finish less than one position-stream
sampling interval after a seek. The completion handler now also compares the
terminal native position with the captured start/seek target. In installed
`just_audio` 0.9.46, `position` in completed state reads the native event's
`updatePosition`; it does not extrapolate using wall time. A strictly later
terminal position proves forward playback without requiring an earlier ready
sample. The existing playing/error/in-flight-seek guards still apply. An exact
seek to end and loading an unchanged terminal position remain rejected.

The narrow handler regression updates position and processing state before
delivering the native end event, with no ready position-stream sample: a seek
to one-hour duration minus 50 ms completes once when the terminal position
advances to duration; a subsequent exact seek to duration does not complete.
The fake seek now updates the synchronous position as just_audio does. Existing
loading-end, pause, error and stop negatives and background/native replay
coverage remain passing. `flutter test --no-pub --concurrency=1
test/services/audio_handler_progress_test.dart
test/services/audio_progress_session_test.dart` passed 18 tests. Scoped analysis
of the two changed Dart files and `git diff --check` passed.

R1 self-review confirms the terminal comparison only supplements the podcast
completion guard. Non-podcast behavior, synchronization, register clocks and
source ownership are unchanged. No wall-clock heuristic or runner change was
introduced. Refrozen for the sole R2; the exact R1 delta is limited to this
document, `player-android/lib/services/audio_handler.dart` and
`player-android/test/services/audio_handler_progress_test.dart`. The full
15-path manifest and integrated verification gaps above remain unchanged.

## Integrated acceptance — 2026-10-27

The retained 5i3 fixture at
`/home/paul/tmp/player-integrated-verify/podcast-5i3` verifies installed
Android A and B clients and the matching Chrome/Firefox episode state. Native
B resumed the position saved by A with its own publisher copy and audible
output, then wrote position and completion at the same clock on natural end.
Native A held a real pre-play sync response beyond the bounded startup and
confirmed that its late arrival did not seek or resume playback. A failed
end-triggered sync left completion durable and the next source audible; a
downloaded episode remained seekable and audible while both hosts were offline.
The passing `resume`, `finish`, `preplay`, `end-outage-next`, `offline-seek`
and browser stage results retain those observations. Focused lifecycle tests
above cover exact seek-to-end, pause, error, stop and short final-position
negatives that were not each recreated on the device.

One additional API 34 native check used the installed B client and the same
local publisher fixture. After **Play from beginning** on its downloaded
60-second episode, Home focused the launcher while Player's media session
remained PLAYING. QEMU captured a 441 Hz publisher-tone segment
from 22.95 to 82.58 seconds in the recording, crossing the background window;
the app then displayed 01:00/01:00 and Play. The WAV, before/background/after
session dumps and structured result are under
`/home/paul/tmp/codex/player-2i3-evidence`. The installed app's exact build
number was not read during this extra check; the earlier 5i3 run recorded
build 4026. The emulator was stopped afterwards. This completes the native
background acceptance missing from the earlier run without repeating the
other passing journeys.
