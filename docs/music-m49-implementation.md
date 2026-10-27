# M49 offline music browsing — tl3

Implemented and accepted in Chrome, Firefox, and the native Android app on
2026-10-27.
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
light/dark and flexible controls/lists handle narrow layouts. Browser and native
screenshots, keyboard/touch journeys, and native Back behavior were checked.
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

## Verification

The focused browser journey passed 2/2 in Chrome 144.0.7559.132 and 2/2 in
Firefox 156.0. It covered an empty local store, real album/playlist pins,
disposable cached music, a cold disconnected reload, all four browse types,
search and keyboard navigation, partial playlist membership, queue playback
with captured 440 Hz output, saved-account isolation, and explicit reconnect.
There were zero API requests during the disconnected and reconnected-local
phase. Evidence is under `/home/paul/tmp/codex/player-m49-evidence/`.

The Android API 34 x86_64 release APK (0.8.2+4022) passed 11/11 focused
checks: real album and playlist downloads, cold local launch with its adb
reverse route removed, all four browse types, search, light/dark, Back, queue
playback with QEMU audio, explicit reconnect, and a 1-of-2 playlist that queued
only its saved song and showed reconnection guidance. Evidence is under
`/tmp/player-m49-native-final/`. A separate native check switched between two
downloaded accounts while music played; it stopped playback and cleared the
queue. A removed file produced missing-file guidance without network fallback.
The empty local store and an unreadable download index were checked on device;
Retry recovered when storage returned. Boundary screenshots and observations
are under `/tmp/player-m49-native-boundary/` and
`/tmp/player-m49-native-final-storage/`.

`GOMAXPROCS=2 go test -p 2 ./... -race -cover`, the web offline browse unit
test, scoped Flutter analysis, and the Android release build passed after the
fixes. The preceding ten-task regression also passed all 1,749 Flutter tests
and all ten focused browser specs in both browsers. This task did not trigger
another full regression; the next overall regression is due after ten more
completed tasks.
