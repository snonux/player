# M42 Artist radio and Instant Mix

Implementation stage: awaiting integrated verification. Prerequisites nj3,
oj3 and xk3 are accepted; nk3 local related recommendations are implemented
and reviewed in acbe057, with formal acceptance deferred.

## Interaction and states

Existing song, album and artist details place mix controls before More like
this. Song/album use **Start mix**; artist uses **Artist radio**. The context
says related songs come from this library using shared credits and genres,
with at most 50 distinct songs. This is separate from internet radio stations.
A labeled **Minimum song rating** selector offers Any rating and 1+ through
5+ stars. The seed title appears in the resulting queue label and feedback.

Web uses existing themed headings, text, native select and button controls;
Android uses themed headings, a labeled dropdown and a FilledButton. Controls
stack or wrap with their existing detail containers. No shortcuts or gestures
are introduced. Existing keyboard typing, Back and modal focus behavior apply.
The actual light/dark/narrow/focus/TalkBack results remain unverified.

Idle → Finding mix songs → Starting mix → accepted seed/count feedback.
Controls disable while resolving. No matches explains the seed and suggests a
lower rating or more tags. Lookup, insertion and source-load failures show
failure feedback; busy queues ask the user to wait, and missing authentication
asks for sign-in. Failed lookup/no-match preserves the previous queue. A source
load failure retains the complete selected queue through the existing actor,
with its normal playback error/retry state, rather than reporting success.

Account/client changes, route departure and newer playback intent cancel
obsolete resolution. Native insertion returns an explicit outcome; mix success
requires an accepted queue, no source-load error, and current completion ownership.
The retained queue claim checks account lifetime independently of short-lived
mix resolution so later Next remains usable after another mix attempt.

## Algorithm and API

See [the API contract](../player-server/docs/api.md#artist-radio-and-instant-mix-m42).
Authorized catalog data enters matching before any scoring, ranking or limit.
Personal song ratings filter candidates; canonical IDs deduplicate them. The
eligible seed song is first, then shared tag count descending and media ID
ascending, capped at 50. Untagged/synthetic seeds and rating filters can return
a deterministic empty result. This is metadata similarity, not acoustic analysis.

## Focused verification and remaining work

Go MusicMix service/API tests cover exact ordering, deduplication, all seed
kinds, rating-before-limit, inaccessible sources, personal rating isolation and
empty/error parameter responses. Web action tests cover resolution ownership,
account/no-match/error boundaries and exact ordered queue insertion options
with accepted/rejected/thrown/busy outcomes at the insertion seam. These web
unit checks do not constitute actual browser playback proof.

Native widget tests exercise rating/no-match/retry, account and newer-intent
cancellation, actual queue insertion order/label/audio-loader invocation, rejected
and thrown insertion, busy rejection, real decoder failure and later Next after
a newer action intent. Scoped analysis includes existing helper callers.

Full regression suites, final server/APK builds and actual installed Chrome,
Firefox and native Android feature acceptance remain deferred by the user's
implementation-first workflow. Required integrated journeys include song/album/
artist seeds, queue contents and rating constraints, empty/error/offline/access
boundaries, account/source races, audio/source output, keyboard/touch/Back,
light/dark/narrow layouts and accessibility. No feature acceptance is claimed.
The broader existing collection widget file also has two reported screen-test
failures (missing audio-handler override and lazy Album row lookup); its four
focused shuffle resolution cases pass. No release/version/push is authorized.

Focused commands (implementation worktree, not a final release build):

```sh
cd player-server
go test ./internal/service ./internal/api -run MusicMix -count=1
node web/js/tests/music-mix.test.js
node web/js/tests/music-related.test.js
cd ../player-android
flutter test --no-pub test/widgets/music_mix_actions_test.dart
flutter test --no-pub test/widgets/music_collection_actions_test.dart --plain-name 'Shuffle options actual menu'
```

The native fixture keeps real source leases/queue/audio-loader behavior while
replacing the source stop callback to avoid unrelated network progress flushing.
The retained-claim case revokes the old lease, advances the action intent, then
selects the next queued song to require a fresh claim.
