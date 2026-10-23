# M49 offline music browsing — tl3

Implementation first; awaiting bounded review and integrated verification.
Dependencies: nj3/oj3/9k3/ck3 accepted; 4l3 durable collection pins reviewed in
`e231b5c`, pending formal platform acceptance. Dependency links remain intact.

## Interaction design recorded before implementation

Web: Login and Settings → Saved music collections → **Browse offline music**
opens `/index.html?offline=music`. The heading clearly identifies Offline music;
server origin and a **Saved server and account** selector identify local copies.
**Browse** selects Songs, Albums, Artists or Playlists. **Search downloaded music**
filters local titles, artists and albums. A row opens its saved song list and
**Play now**, **Play next**, **Add to queue**. Only available downloads are queued;
partial playlist snapshots show saved/total counts and reconnection guidance.
Existing playback chrome and queue remain available. Returning online requires
an explicit Sign In/Player link; reconnecting alone does not replace the queue.

Native: Downloads → **Browse offline music**, with native account/type selectors,
search field, generous ListTiles, a song/action bottom sheet, existing mini player
and queue sheet. Back closes the sheet then returns to Downloads. Metadata stays
verbatim, while UI labels use canonical English/German localization helpers.

States: local loading indicator; no downloaded music; no search matches;
retryable storage-read error; missing-file guidance; queue-busy feedback; truncated
long row labels with full detail heading; no online editing/profile/session
controls in offline mode. Existing theme tokens/native theme surfaces handle
light/dark and flexible controls/lists handle narrow layouts. Actual screenshots,
contrast/accessibility checks and touch/keyboard journeys remain deferred.
Web preserves j/k/h/l and arrows selection, native Enter activation, / search,
Esc/Backspace hierarchy, p/Space playback, r shuffle and Q queue, plus existing
queue navigation and scoped keyboard access. Text/select controls own typing.

## Storage and account privacy

Reuse durable pin CacheStorage/localStorage, Android download repository and
read-only adapters over both existing disposable playback caches;
no second media cache or playlist persistence model. Albums/artists derive from
saved embedded metadata; playlists retain existing pin membership/counts and
can use already cached copies of their songs. Per-song labels distinguish
Downloaded from Cached (disposable), with eviction guidance. New native cache
copies retain source music metadata; older title-only entries stay playable with
unknown album/artist. Removed/evicted files cannot stream as a fallback.

Web offline mode uses a separate in-memory selected local account. It never sets
`state.user`, unlocks credentials or restores an online user from localStorage.
The API wrapper fails locally; pinned byte reads skip online HEAD checks only in
this explicit mode. Offline player initialization skips server setup and profile,
lyrics, thumbnail, personal-action, Cast and detached requests. Media always uses
verified pinned blobs; missing bytes never fall back to a network stream.
Account changes stop playback, clear queue and Undo, cancel pending ownership,
release the current blob and discard outdated asynchronous metadata/byte reads.
Online authentication/access revalidation behavior outside offline mode remains.

Native account choices label locally downloaded snapshots and do not unlock
online APIs. First offline playback detaches the previous online queue's
persistence callbacks using the existing scope reset, preserving its saved
snapshot. Changing saved account stops playback and clears retained queue/Undo.
Online library navigation reinitializes queue persistence on its next scope.
Play next/Add reject another account's queue and request explicit Play now.
All files are revalidated through repository playback requests before insertion.

## Checks and remaining gates

IMPLEMENTED / self-reviewed — AWAITING INTEGRATED VERIFICATION (tl3/M49).

Passed `node player-server/web/js/tests/offline-browse.test.js` (metadata groups,
search, missing counts, zero network requests, explicit local identity,
account-switch invalidation) and existing `offline-pins.test.js` (durable bytes,
cache independence, removal/overlap/cancellation/denial/account boundaries).
Dart formatting applied to changed native files. Scoped Flutter analysis of the
new service/browser/cache adapters, existing Downloads/queue sheet and tests
passed without issues; `flutter test test/services/offline_music_library_test.dart
test/services/playback_cache_test.dart test/services/playback_cache_provider_test.dart`
passed all nine tests. Cache adapter checks include saved metadata, account/origin
isolation and eviction; existing provider tests retain online denial/prefetch checks.
Full suites/builds and actual Chrome, Firefox and native emulator journeys are
deliberately deferred by the implementation-first workflow. This is not platform
acceptance.

Integrated scenario: download album/playlist and a manual song; stop server;
cold-open Offline music; choose saved account; browse all four types; search;
queue/play via pointer/keyboard or touch/native controls; check actual audio and
saved media identity; delete/miss one file; switch accounts during loading;
restart; reconnect and sign in without replacing local playback inadvertently.
Run light/dark/narrow and long metadata, focus/Back, storage failure and
cross-account isolation checks on each required platform. Retain exact versions,
commands and artifacts outside committed media/build files.
