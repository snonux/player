# M52 network-aware music settings

Task `6l3`: verified in Chrome, Firefox and native Android. Prerequisites `nj3`/`oj3` accepted;
stream quality `4k3` (`9cd5343`), playback cache `ok3` (`8a7256e`) and separate
download quality `5l3` (`8612150`) supply the existing playback policies.
Focused platform acceptance is recorded below. The next combined full
regression gate is due after twenty completed tasks under the user's
ten-task cadence. Optional cover data saver is outside this implementation.

## Interaction and state layout

Uses U01's existing Settings modal / Material settings page and existing
player Stream quality disclosure / bottom sheet. No new shortcut or modal
hierarchy. Native switches retain Material tap targets; web uses labeled
native selects and checkboxes within the existing focus-trapped settings.
Theme colors and type inherit the existing theme. Narrow content wraps on web;
Android settings and quality sheet scroll. Escape/Back retain existing behavior.
Light/dark/narrow visual review and actual keyboard/touch journeys remain
required in integrated acceptance.

```
Settings > Music network (Android: expandable)
  Using Wi-Fi / Mobile / Mobile settings (network type unavailable)
  Network: Auto | Wi-Fi (manual) | Mobile (manual)
  Allow prefetch on Wi-Fi [on]
  Allow prefetch on Mobile / unknown [off]
  Stream library audio on Wi-Fi only [off]
  Sync saved music session on Wi-Fi only [off]
  [Persistent policy/status text; save failure inline]

Player > Stream quality
  Network settings to edit: Wi-Fi | Mobile / unknown
  Quality: Auto | Original | server-advertised profiles
  Server profiles status + Reload profiles
  Android: existing Download quality… link
```

Auto uses Android connectivity_plus bearer data. Browser Auto uses only an
explicit `connection.type` bearer, when available: Wi-Fi/Ethernet or cellular.
`effectiveType=4g` is speed and never treated as cellular evidence. Firefox and
other browsers without bearer data visibly use Mobile settings until the user
chooses a manual override. Wi-Fi includes Ethernet; a hotspot can still incur
data charges. Manual choice is persistent and clearly labeled.

Each quality inherits the previously selected stream quality until explicitly
changed. Native quality remains scoped to the existing canonical server key;
web quality remains browser-local as before. Download quality stays separate.
Changing network/profile affects the next opened source, never rewrites the
current source's URL. Prefetch permission also requires the existing cache
prefetch switch; gapless successor preparation respects the network allowance.

Wi-Fi-only library audio streaming rejects a new server-audio source before cache fallback,
so a restricted request cannot silently become streaming after a cache failure.
Current playback is retained. Local downloads/documents, live radio, public shares and Cast
retain their existing playback policies. No network-wide HTTP interceptor is
introduced; authentication, progress, podcast sync and ordinary library metadata
are unaffected.

Wi-Fi-only **saved music session** sync defers queue transfer only. Queue edits
remain durable locally; Continue session explains that it waits for Wi-Fi and
can be retried after changing settings. It does not claim to restrict all
library traffic. Existing sync retry paths continue to own transfers.

## Integrated journey checklist

- Chrome and Firefox: Auto with missing bearer reports Mobile settings; manual
  Wi-Fi/Mobile switches selected quality on actual requests. Native: real
  emulator Wi-Fi/mobile change and explicit override do the same.
- Wi-Fi-only stream: block direct, converted, cached/buffer fallback and
  prepared successor paths; existing source remains stable; retry on Wi-Fi.
- Separate quality per network; legacy value migration; exact rendition cache
  identity even if settings change during asynchronous source resolution.
- Cache prefetch enabled, Wi-Fi permitted/Mobile denied, then Mobile allowed;
  verify actual requests, cancellation and gapless successor behavior.
- Saved session changes persist while blocked and after restart, Continue shows
  deferred state, Wi-Fi resume sends them; progress/auth/podcast calls unaffected.
- Native touch/Back, screen-reader names, browser keyboard selects/typing and
  Escape focus, narrow/light/dark rendering and long/unavailable profile labels.

No release, version bump, push or deployment is included.

## Focused browser acceptance, 2026-10-27

An isolated `--no-doubles --shuffle-playback` fixture at
`/home/paul/tmp/codex/player-m52-18890` served real 45-second Night Transit
tracks from `http://127.0.0.1:18890`. Server 0.8.2 build
`c6375805bfd3c159915172090c5e3516a52c1679-dirty` was exercised by
installed Chrome 144.0.7559.132 and Firefox 156.0. Each browser ran alone
with one Playwright worker and `GOMAXPROCS=2`:

```sh
cd player-server/test/e2e-web
GOMAXPROCS=2 PLAYER_MUSIC_FIXTURE=/home/paul/tmp/codex/player-m52-18890 \
  PLAYER_E2E_PULSE_SINK=player_e2e_null npx --no-install playwright test \
  -c playwright.music.config.ts tests/music-network-aware.test.ts \
  --project=chrome --workers=1
# Repeat with --project=firefox.
```

Chrome 3/3 and Firefox 3/3 passed. The focused runner checks unknown browser
bearer => Mobile settings; independent manual Wi-Fi and Mobile quality on real
`compat?profile=aac128` and `compat?profile=opus96` responses; source stability
until the next open; persisted preferences after reload; and AudioWorklet
output matching the fixture tone. Wi-Fi-only mode leaves the current song
playing while it blocks a cached song and a converted song without starting
either request. Switching back to Wi-Fi and pressing Play recovers real audio.

With a fresh scoped account, it checks that default Mobile prefetch issues no
next-song request, Wi-Fi prefetch and explicitly enabled Mobile prefetch cache
the actual successors, and queue edits remain locally durable through reload
and a temporary browser-offline period. Continue session explains Wi-Fi
deferral; real queue PUTs complete after reconnecting and selecting Wi-Fi.
Progress remains callable under the sync restriction. Keyboard Escape restores
focus, and narrow 390px settings were captured in dark and light themes with
no horizontal overflow. Evidence and WAV/screenshots are under
`/home/paul/tmp/codex/player-m52-evidence/{chrome,firefox}`. This is focused
M52 browser acceptance; native acceptance is recorded below.

## Focused native Android acceptance and fix, 2026-10-27

One read-only Android 14/API34 emulator (33.1.24) ran the release x86_64 APK
`0.8.2+4022` against a disposable `--no-doubles --shuffle-playback` fixture
on port 18852. The path-only proxy on host port 18853 recorded request paths
and status without credentials; QEMU WAV captured decoded output. Exact
commands, APK/server hashes, screenshots and state files are in
`/home/paul/tmp/codex/player-m52-evidence/native-summary.md`.

Android's active default network and the app's Auto label both changed from
validated Wi-Fi to validated MOBILE[HSPA] when Wi-Fi was disabled, then back
when enabled. With Wi-Fi set to Original, the app requested a real stream
and produced Long Road Home's expected tone. On Mobile with Data saver
Opus96, it requested `compat?profile=opus96` and produced Harbour's expected
tone. Editing the quality choices left the paused current source intact.

With Wi-Fi-only streaming enabled on Mobile, the already loaded Harbour
continued playing while a new library source made no audio request. The
native run exposed a defect: Previous changed the displayed song to a
blocked target and left the old playable decoder stranded behind Retry.
The fix now checks the target before changing queue metadata, decoder,
progress or controls. If the policy changes during asynchronous source
resolution, it restores the old occurrence and progress session. The player
shows a nonfatal Wi-Fi policy message. `music_sequence_test.dart` covers both
cases, including a wrapped folder expansion rollback; scoped Dart analysis
and 42 sequence/network/gapless tests passed.

The final patched universal local release APK (`0.8.2+22`, SHA-256
`5681c380851537873543bac30a26544fb140c1b562d0c49b12c10503c7f11dbf`)
was installed on the same emulator after the rollback fixes. On real Mobile,
blocked Previous kept Long Road Home PAUSED at the same position, displayed
the policy message, and made no new audio stream request. Play resumed the
same decoded 523.3 Hz song without a new request. Restoring Wi-Fi cleared
the message; Previous then loaded First Light with 440 Hz output. The
Wi-Fi retry used the previously cached song, so a fresh GET was unnecessary.
State, proxy requests and decoded audio are in `stream-block-final.json`.

Saved-session edits remained local through a force-stop/relaunch on real
Mobile under Wi-Fi-only sync: the local queue changed Repeat to All, the
server stayed Off, and no queue PUT was sent. Restoring Wi-Fi and tapping
Retry sent one successful PUT and cleared the pending state. Gapless successor
preparation was ready on Wi-Fi, became unavailable on restricted Mobile while
the paused song stayed at 409 ms, and became ready again on Wi-Fi. Native
permission revocation and a second physical network were not separately
tested; the focused browser/API ACL checks and real Wi-Fi/Mobile emulator
bearers cover the relevant authorization and network boundaries.

## Focused implementation checks

Commands run from component directories on 2026-10-23 (shared implementation
worktree, no final build):

- Android: `dart format` on changed native files; `flutter analyze --no-pub`
  with the ten changed native source paths: no issues.
- `flutter test --no-pub test/services/network_policy_test.dart test/services/playback_source_resolver_test.dart test/services/saved_queue_sync_test.dart test/utils/playback_errors_test.dart`: 26 passed.
- `flutter test --no-pub test/services/playback_cache_provider_test.dart test/services/playback_cache_test.dart test/services/gapless_playback_test.dart test/services/download_quality_test.dart`: eight cache/gapless/download-quality checks and three provider checks passed; two prefetch fixtures initially failed because Unknown now correctly defaults to Mobile. Fixtures now explicitly use Wi-Fi. A final focused provider run passes all six checks, including a new saved-cache/offline-policy bypass regression.
- Web: `node web/js/tests/network-policy.test.js`, `stream-quality.test.js`,
  `playback-cache.test.js`, `queue-sync.test.js`, `gapless-gain.test.js` all
  passed. The gapless fixture now explicitly selects Wi-Fi and installs its
  `window` mock before dynamically importing modules (its earlier static
  imports failed on `listening-progress.js`'s existing window listener).
- Owned tracked diff whitespace check passed. These are deterministic module
  checks, not browser or native device proof. The final source-entry restriction uses the existing pending-source retry path with persistent status instead of repeated toasts. Decoded gapless boundaries retain the prepared currentSrc URL. Live radio branches are preserved; the server library audio restriction does not cover radio, shares or Cast. Service-worker asset inclusion remains root-owned before combined review/verification.

After radio commit `2f5c460`, the serialized final hooks were applied and
`node web/js/tests/playback-source.test.js` passed, including new production
player checks for blocked direct/compat media requests, persistent policy status,
Play retry on Wi-Fi, and live radio's explicitly separate policy. The existing
intentional fake-Cast failure logs remain part of that passing test. Network
policy and gapless gain tests were rerun and passed. Native scope-label changes
were formatted; previous focused native passing results were reused. Root owns
the new module's service-worker registration. All required actual platform
journeys were pending at that implementation checkpoint; focused browser
acceptance is recorded above.

Round 1 reviewed the combined source guards, quality/cache/gapless policy,
saved-session durability/retry and UI scope labels without confirming a
blocking or nonblocking defect. No second review was needed. Root registered
`network-policy.js` in SW v72. At that checkpoint, actual platform journeys
and integrated suites/builds remained pending. Focused Chrome and Firefox
acceptance is recorded above; native and integrated gates are tracked
separately. Optional cover saving is outside this slice as stated above.
