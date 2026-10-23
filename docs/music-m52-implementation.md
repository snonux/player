# M52 network-aware music settings

Task `6l3`: implementation stage. Prerequisites `nj3`/`oj3` accepted;
stream quality `4k3` (`9cd5343`), playback cache `ok3` (`8a7256e`) and separate
download quality `5l3` (`8612150`) supply the existing playback policies.
Full suites, final builds and actual Chrome, Firefox and native acceptance are
deferred to the user-authorized integrated verification phase. This document
is not platform acceptance evidence. Optional cover data saver is outside this
implementation slice.

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

## Deferred integrated journeys

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
journeys remain unverified until the integrated phase.

Round 1 reviewed the combined source guards, quality/cache/gapless policy,
saved-session durability/retry and UI scope labels without confirming a
blocking or nonblocking defect. No second review was needed. Root registered
`network-policy.js` in SW v72. This is a reviewed implementation only; actual
Chrome/Firefox/Android/network journeys and integrated suites/builds remain
pending. Optional cover saving is outside this slice as stated above.
