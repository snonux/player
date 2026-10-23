# Live listeners (M71 / dl3)

Implementation first; integrated Chrome, Firefox and native Android acceptance is pending. Dependencies nj3/oj3/qk3 are accepted; mk3 is reviewed at a73cc08 and pending integrated acceptance.

## Interaction and privacy

Music Home gains an optional collapsed **Listeners** section alongside existing secondary discovery controls. Opening it shows **Share my listening** (off by default), a short privacy explanation, and current listeners. Viewing never enables sharing. Sharing is transient per client and resets on account/origin change and app/page restart. No new keyboard shortcut: native details/button/checkbox controls preserve typing and existing navigation; Android uses an ExpansionTile and SwitchListTile, normal Back, and 48dp controls.

Desktop/narrow web and Android use the same single-column hierarchy:

```
Listeners [expand/collapse]
  [ ] Share my listening
  Off. Sharing shows your username, client and current song to
  signed-in people who can access that song.
  username · Web / Android · Playing / Paused
  Song title · artist
  0:32 / 3:14
```

Names and titles wrap; progress is a readable text timestamp, not a seek control. No listener controls modify another client's playback. Loading: "Loading listeners…"; empty: "No listeners sharing right now."; failed/offline: clear rows immediately and show "Listeners unavailable. Retry"; no inaccessible-song placeholders or counts. Standard theme tokens/Material text colors inherit dark/light contrast. Collapse retains sharing and heartbeat; leaving the authenticated shell or changing account disables it. Refresh never steals focus or announces changing progress repeatedly.

Only current authenticated server-library music played locally is supported. Public shares, device files, downloaded/offline sources, live radio and remote/detached/cast output are excluded. Server revalidates music scope independently; clients never infer listeners from resume/history/count data. Shared titles are fresh server metadata, not client-supplied strings. The panel explicitly explains this scope.

## Contract

Session-required dual-prefix GET/PUT/DELETE `/api/music/presence` and `/api/v1/music/presence`; all responses no-store. Every request binds `expected_user_id` to current authentication. PUT accepts client_id (opaque per-process/tab random ID), client (`web`/`android`), media_id, state (`playing`/`paused`), position_seconds, and increasing sequence. DELETE accepts client_id and sequence. Client IDs/authentication IDs never appear in responses. Sequence tombstones prevent a delayed heartbeat from undoing opt-out; account/session ownership remains private. Finite nonnegative positions are clamped to fresh song duration. Display uses reported progress (10s updates), never invented playback progress.

Constructor-injected, synchronized memory-only service, 45s TTL, maximum 8 clients per user and 256 rows overall; no schema changes or worker lifecycle. Authentication validity, publisher existence/current ACL, viewer ACL and music classification are rechecked for every read. Cookie sessions use current repository session rows; bearer sessions use current API-token rows, including expiry. Server restart drops all presence. Expiry/stop/error/revocation removes rows. No listening counters, progress saves, history, or scrobbling calls.

## Integration and verification

Root owns server.go construction/dual routes, app/Home slots, API documentation and service worker module versions. Independent new service/handler, web panel/controller and Android API/controller/panel modules avoid shared files held by rj3, ol3 and 4l3.

Cheap focused checks cover opt-in/opt-out ordering, expiry, two-account ACL/session/token revocation, stale asynchronous responses, account changes, scope exclusion, malformed positions and caps. Full suites/builds, light/dark/narrow visual evidence, real keyboard/touch journeys, and actual two-account play/pause/seek/disconnect/privacy checks in installed Chrome/Firefox/native Android remain deferred by the implementation-first workflow. No platform acceptance is claimed.

## Focused implementation checks

- `cd player-server && go test ./internal/service ./internal/api -run TestMusicPresence -count=1`: PASS (service 0.038s, API 0.024s); includes per-request publisher catalog reuse, opt-out ordering, credential/publisher/viewer revocation, non-resurrection until fresh heartbeat, TTL/caps/clamps and strict account/body handling.
- Node `web/js/tests/music-presence.test.js` and `playback-presence.test.js`: PASS. Covers private response invalidation, explicit sharing, offline row clearing, actual decoded transport position and captured source account boundaries. Web module syntax and `git diff --check`: PASS.
- `cd player-android && flutter analyze lib/api/music_presence_api.dart lib/services/music_presence.dart lib/widgets/music_presence_panel.dart lib/screens/home_screen.dart test/services/music_presence_test.dart`: PASS after fixing two brace-style diagnostics.
- `flutter test --no-pub test/services/music_presence_test.dart`: 2 PASS (viewing/sharing, unsupported source cleanup, error row clearing, old-account response invalidation and credential-bound cleanup).

Web source ownership is captured synchronously before media preparation. Gapless successors retain that captured owner; the snapshot reads the actual transport (decoded or native), and excludes explicit offline-pin queue entries. Android captures the authenticated API transport per keyed Home panel and checks request origin/user plus current credentials before reporting. No browser/emulator feature acceptance or full-suite/build claim is made.

Root final wiring is applied: authenticated server construction/routes, persistent web shell mount and logout cleanup, service worker v78 and the API contract. Production startup/logout body regression confirms mount after known identity, cleanup before identity clearing/slow output shutdown, and no remount after delayed config resolves following logout. Actual mount regression with `crypto.randomUUID` absent passes using existing `listeningOccurrenceID` random bytes. Four focused Node checks (startup lifecycle, mount, controller, authoritative snapshot), app/module/SW syntax and diff checks pass. No additional native, full-suite/build or runtime acceptance was run for these wiring/documentation changes.

R1 fixes: native presence now consumes `currentActivatedMediaRequest`, which excludes prepared, moving, loading and failed tracks while retaining the existing decoder on failed preparation. Two real `PlayerAudioHandler` delayed-next tests pass (successful activation and failed loader): metadata can display the next track while the old decoder remains ready at 7s, but presence stays empty until the successful next source activates at 0s. The actual web mount now clears cached account binding at pagehide and rebinds on persisted pageshow with sharing still off; regression verifies explicit re-opt-in works after BFCache restoration and cleanup removes listeners. Four focused Node checks pass; `flutter test --no-pub test/services/music_sequence_test.dart --name 'presence excludes delayed next'` passes 2 tests. No acceptance/build/full-suite claim is added.

Final narrow R2 confirmed both source-activation/BFCache blockers fixed with no new blocking defects; both review rounds used. Integrated verification remains pending.
