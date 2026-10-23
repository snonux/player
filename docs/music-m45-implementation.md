Stage: IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION.
Two bounded expert rounds complete; all three R1 native blockers fixed, R2 clear.

# M45 automatic playback cache

Implementation stage: task ok3, awaiting bounded review and integrated platform
verification. Prerequisites nj3/oj3/1k3 are completed; quality selection 4k3 is
implemented/reviewed pending integrated verification. Versions remain 0.8.2+22.

## Interaction and layout

Web: existing Settings modal, after Appearance, gains Playback cache. Android:
existing Settings gains a Playback cache expansion tile beside navigation.
Both retain existing typography, theme tokens, native select/switch/button
controls and narrow-screen wrapping. No global shortcut is added. Web Tab/Shift
Tab and native select typing stay within existing modal keyboard behavior;
Escape returns through existing settings focus restoration. Android Back uses
the existing Settings route.

Expanded layout on both platforms:

    Playback cache                        [usage MB · songs]
    Disposable copies; kept downloads are separate.
    Cache budget                          [Off / 128 / 512 / 1024 MB]
    Buffering                             [Immediate / Whole song]
    Prefetch next queued song             [switch]
    Storage                               [browser explanation / native switch]
    [Transfer/error/status]
    [Cancel transfer] [Clear cache]
    Cached songs                          [title · MB list / empty message]

Default 128 MB and immediate streaming; next-song prefetch defaults off.
Changing the budget evicts least recently used complete copies. Clear cache
is safe and immediate: web keeps the current in-memory blob playing; Android
retains the playing file until it is released. Kept downloads, local documents
and server originals are unaffected. Cancellation removes incomplete copies.
The browser manages the cache location; Chrome and Firefox receive the same
CacheStorage/blob implementation. Android offers its internal app directory
or Android's app external files directory when available. Location changes
require stopped playback/transfers, clear disposable copies, and report an
unavailable external directory. These directories are removed on uninstall.

Loading storage, no cached songs, failed storage/settings updates and active
transfers have explicit visible states. Titles wrap/truncate on native controls
and wrap in web lists. Native playback cache listings are scoped to the selected
account; bytes are never treated as permanently kept downloads.

## Data and playback behavior

Copies are complete bounded audio responses. Partial/oversized/failed responses
never become ready. Disk writes are separate from the original streaming media
request, so automatic copying can use additional bandwidth; whole-song buffering
waits for that copy first. Cache failures leave ordinary streaming available.
Optional prefetch fetches only the next explicit queue entry.

Canonical server origin, account, media ID and selected quality identify native
files. Web cache containers are same-origin/account scoped and entries retain
the complete resolved stream URL including the quality query. Neither stores
credentials or transport gateway tokens as durable identities. Public shares,
Cast, local files and kept downloads retain their existing playback policies.
Online cache reuse checks access and ETag. Only transport errors permit reuse
without a fresh response; HTTP denials never grant offline access. Automatic
copies require the selected unlocked account, and account/source changes cancel
in-flight transfers. Web cached metadata fallback is limited to transport
failures and cached audio, allowing queue recovery within the authenticated
session. Browser reload still requires normal server sign-in/configuration.

U01 originally proposed Android-only M45. The later explicit three-platform
feature instruction takes precedence; this slice includes real web caching.

## Verification stage

Full suites, final server/APK builds, light/dark/narrow screenshots and actual
Chrome/Firefox/native emulator journeys are deliberately deferred to integrated
verification under the user's implementation-first workflow. Required journeys:
stream then sever network/replay, selected-quality separation, ETag replacement,
401/403/404 denial, budget eviction, prefetch/cancel, safe clear while playing,
account/server isolation and native storage switching. Unit or syntax results
are coherence checks only and do not constitute platform acceptance.

Cheap checks during implementation: Node cache boundary tests and existing
playback-source suite pass. Scoped Flutter analysis of nine cache/affected
source/test paths reports no issues; new cache/access tests pass (two tests).
A wider four-file targeted attempt had 27 passing checks and two failures:
existing audio-player screen fake `play()` overrides no longer match the
handler's `play({automatic})`, and the direct-handler queue recovery assertion
expects one play call but receives zero. The latter supplies its own inline
loader and does not invoke the cache provider. See
`/home/paul/tmp/ok3/native-targeted.log`; these are retained integration gaps,
not claimed passing checks. No full suites/builds or platform acceptance ran.

Review round 1 found three native blockers: lookup-persistence errors could
block network playback, denied cached songs could reopen on a later network
failure, and prefetch was skipped for cache-hit/whole-song-buffer paths. Fixed
as one batch. Cache infrastructure failures now leave the network source
available; access denials remove reusable metadata before fallible storage
cleanup while preserving playing bytes; prefetch runs independently of how
the current source was obtained. If atomic invalidation persistence fails,
Player attempts to remove the older index so restart discards unindexed copies.
Read-only storage retains the in-memory denial; normal cold-start unlock still
requires server authorization.

Production provider regressions exercise actual temp storage and loopback
HEAD/download responses with account-scoped dependencies: failed LRU index
write still streams; HEAD 403/API 404 denial followed by offline reopen fails
and persisted metadata cannot resurrect the copy; cache-hit and whole-song
buffering both prefetch the next queue song. Combined provider/cache tests:
7 PASS. Scoped analysis of the five modified cache/test paths: no issues.
Log: `/home/paul/tmp/ok3/cache-round1-tests.log`. This remains unit-level proof;
actual Chrome/Firefox/native platform acceptance is deferred.
