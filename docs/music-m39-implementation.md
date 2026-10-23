# Configurable music home (M39 / 3m3)

Implementation stage: reviewed in round 1; awaiting integrated verification. No feature acceptance or final build is claimed here.

## Design and interactions

The existing album discovery entry hosts Music home, retaining the album browser and playback chrome. Web opens **Music home · Discover albums**, then **Customize home**; Android opens **Discover albums**, then **Customize home**. Existing discovery sections keep their readable default order, followed by personal selections and pinned playlists.

Each customization row has a Show control and labeled Move up / Move down buttons. There is no required drag gesture. Changes apply immediately. Restore defaults shows every section, restores the default order and clears playlist pins. Web uses native disclosure controls, preserves focus on the changed control, and closes the customization disclosure with Escape before closing its parent. Enter and Space activate customization buttons without invoking library playback shortcuts. Android uses an AlertDialog with native switches and touch arrow buttons; Done or Back dismisses it. Existing collection navigation and queue actions retain their owners.

Visible sections show source descriptions and actionable empty states. Hiding all sections leaves a message pointing to Customize home and Restore defaults. Playlist loading, connection failure with Retry, no readable playlists and unavailable pins have separate states. Unavailable pins display only a generic count and can be removed together. Storage failures show an error rather than claiming persistence. Existing theme classes and Material controls supply light/dark styling; narrow layouts, contrast, actual focus journeys and native touch/Back still require integrated visual acceptance.

## Data and persistence boundaries

Preferences contain section keys, hidden keys and opaque playlist IDs, never cached playlist names. Both clients scope the record to the primary server origin and authenticated account ID using `player.music-home.v1:<origin>:<user ID>`. Missing, malformed or obsolete settings recover to readable defaults, with new known sections appended to saved order. These are device/browser preferences, not a server-synchronized home arrangement.

Web startup initializes `state.user` from the authenticated config's positive safe-integer `user_id` before library rendering and restored navigation. Logout clears identity before awaiting renderer or network work; a delayed startup response cannot restore it. Old home callbacks reject an identity change. Unknown identities use visit-only state and never read or write a shared undefined-account storage record. Android watches origin/account state, isolates pending results and writes, and removes previous-account names from an open customization dialog after an account switch.

Pinned metadata is fetched from the current readable-playlist endpoint. Missing or revoked IDs remain anonymous; names are not recovered from disk. Existing playlist detail/collection resolution rechecks access for opening and playback. A connection failure preserves pins and offers Retry rather than deleting them as if access had been revoked. Request generations prevent obsolete playlist-list responses from replacing newer results.

## Personal collections and playback

- **Liked-album mix:** shuffled albums favorited by this account; explicitly plays whole albums. This is not a liked-song mix or an external recommendation feed.
- **Rediscover older plays:** previously played albums ordered by oldest latest qualified personal play. Its label explains that the source is local listening data, not an anniversary or historical listening-event reconstruction.

Both use accessible album metadata and the existing album collection/queue actor. Custom collection views fetch supported ordinary album data and select locally; unsupported collection names are not sent to the server. Random and liked-mix views retain their displayed ordered snapshots. Album detail objects remain outside list sorting branches. Ordinary discovery collections and playlist playback retain their existing API behavior.

## Focused checks and remaining acceptance

Focused Node scripts passed: `account-lifecycle.test.js`, `music-home.test.js`, `music-layout.test.js`, `music-discovery.test.js`, `music-discovery-detail.test.js` and `navigation-url.test.js` under `player-server/web/js/tests/`. `node --check player-server/web/js/app.js` and `git diff --check` passed. The lifecycle test executes the actual startup/logout bodies with transport/UI seams and exercises production home storage wiring, including unknown identities, changed accounts, logout and delayed startup. These checks are not browser acceptance.

Owned Dart files were formatted. Targeted `flutter analyze --no-pub` over `lib/services/music_discovery.dart`, `lib/services/music_home.dart`, `lib/widgets/music_discovery_panel.dart`, `lib/screens/music_albums_screen.dart` and their three helper/widget test files passed without issues. `flutter test --no-pub test/services/music_discovery_test.dart test/services/music_home_test.dart test/widgets/music_discovery_panel_test.dart` passed four tests, including anonymous unavailable pins, persistence, default restoration/remount and the existing narrow discovery snapshot journey.

Per the implementation-first workflow, full regression suites, final server/APK builds and actual installed Chrome, Firefox and native Android emulator acceptance are deferred. Required integrated journeys include reorder/hide, pin/unpin, personal-selection playback and output, restart persistence, account/origin isolation, missing/revoked playlist access, offline/retry and no-data behavior. Exercise keyboard/focus in both browsers, touch/Back on Android, and light/dark/narrow layouts. Root coordinates service-worker asset integration. No task completion, release, version bump, push or deployment is authorized by this slice.
