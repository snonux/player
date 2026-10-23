# M33 smart playlists (`ol3`)

Implementation stage; integrated Chrome, Firefox and native Android acceptance
remains pending. Prerequisites: U01 `nj3`, fixtures `oj3`, accepted playlists
`3k3`, reviewed ratings `lk3`, personal listening `mk3` and facets `vk3`.

## Interaction contract recorded before coding

Playlists keeps its existing cards and detail/song rows. **New smart playlist**
creates an ordinary named playlist then opens the enabled rule editor (canceling
rule setup leaves that named manual playlist); **Rules** on an
editable detail opens the same editor. Existing manual lists can enable rules
without losing saved manual entries; disabling rules restores those entries.
Filesystem-following lists must stop following before enabling rules. Smart
lists cannot receive manual additions/removals/reordering or filesystem imports.

Desktop web uses the current playlist modal, native Android a scrollable
Material form. Narrow web stacks each row's native controls; no horizontal
scroll or drag interaction. Layout:

```
Smart playlist rules                           [Close / Back]
[Enable smart rules]
Match [all / any] of these rules
[Genre       ] [contains ] [Synthwave       ] [Remove]
[Your rating ] [at least ] [4               ] [Remove]
[Add rule]
Order by [Title] [Ascending]   Maximum songs [100]
Summary: Match all: Genre contains Synthwave; Your rating at least 4.
         Title, ascending; up to 100 songs.
Your plays, favorite and rating rules use your account. Shared playlists may
show different songs to each viewer. Dates use UTC; Added means file added.
[Cancel]                                     [Save rules]
```

Supported fields: title/artist/album/genre, year, your qualified plays, your
rating (0–5), your file favorite, file added date and your last qualified play.
Text offers contains/is; numbers equals/at least/at most; favorite yes/no;
dates before/after (YYYY-MM-DD, UTC calendar day) or within the last N days.
Unknown last-play dates do not match date conditions. All/any is explicit,
not nested JSON. Advanced JSON is unnecessary for every supported rule.

Detail shows a readable rule summary, **Refresh results**, and current dynamic
ordered/limited songs. Playback/queue/export capture the current result through
the existing server read. Refresh re-evaluates current permissions and personal
data; no background timer or persisted result snapshot. Stable media-ID ties
make ordering deterministic. No new global shortcuts: form typing/selects keep
native keys; Escape closes web modal with existing focus restoration; Android
Back cancels draft. Canceling an unsaved draft never writes. Save errors preserve draft and focus;
disabling rules asks no destructive confirmation because manual entries remain.

Loading uses existing playlist loading. Empty smart detail says no songs match
and offers Rules to editors. Offline/error keeps draft with visible retry by
Save; no success until server responds. Permission loss reports failure and
normal refresh hides edit controls. Labels wrap, values retain full accessible
names, and existing theme tokens/Material theme cover light and dark.

## API and storage contract

`PUT /api[/v1]/playlists/{playlist_id}/rules` takes `{rules: object|null}`;
non-null rules have `match: all|any`, 1–20 `conditions` containing
`field/operator/value` strings, `sort`, `descending` and `limit` (1–1000).
Null disables dynamic selection. The result is the ordinary playlist view with
an additive `rules` property and generated ordered entries/songs. Viewer
personal data and current readable music apply before sorting/limiting.
Owner/admin edit rights and shared-read semantics match existing playlists.
Private foreign/missing lists remain 404. Invalid rules are 400, source
conflicts/manual song mutation on smart lists 409, readable foreign writes 403.
Rules JSON lives on the playlist row; manual entries remain independent.

## Verification still required

Cheap focused coherence checks are recorded below when run. Full suites,
server/APK builds, actual Chrome/Firefox and native emulator journeys, light/
dark/narrow capture and restart/offline/permission acceptance are deferred by
the current implementation-first workflow. Required integrated journey: create
metadata/plays/favorite/rating/date rules, change matching data, refresh and
verify exact ordered/limited results in each client and account. Exercise
shared-viewer differences, restricted/trashed songs, manual mutation rejection,
source conflict, persistence, error draft retention, web keyboard/Escape/focus,
and native touch/Back. No platform acceptance is claimed by focused tests.

Focused implementation checks (before shared UI hooks):

- `cd player-server && go test ./internal/service ./internal/api ./internal/repository -run 'TestSmartPlaylist|TestPlaylistsPersistencePermissionsAndLifecycle' -count=1` — passed. Real SQLite covers viewer rating/favorite refresh, ACL exclusion, deterministic order/limit, shared personal differences, mutation guards and retained manual entries; API covers strict payloads, saved rules and disabling. Pure predicates cover qualified-play counts, UTC day/relative bounds and never-played dates.
- `cd player-android && flutter test --no-pub test/screens/music_playlist_rules_screen_test.dart` — passed. Actual widget edits and saves a rule, preserves its draft after a failed write, and retries. This is not emulator acceptance.
- New web editor `node --check` and imported typed-operator/readable-summary Unicode smoke — passed. Full mounted UI/keyboard journeys pending shared hook and integrated acceptance.
- New native editor and API extension scoped `flutter analyze --no-pub` clean. Native dates use the Material calendar picker; rules remain a canceled draft until Save succeeds.

Initial cheap-check corrections retained honestly: API fixture username `rules`
made an overly broad string assertion fail until it checked the property key;
widget Save was initially unbuilt in a lazy list, fixed by scrolling the test.
No passing platform runtime is inferred from these checks.

Final shared-hook coherence checks:

- `cd player-android && flutter analyze --no-pub lib/api/playlist_rules_api.dart lib/screens/music_playlist_rules_screen.dart lib/screens/music_playlists_screen.dart lib/l10n/music_localizations.dart test/screens/music_playlist_rules_screen_test.dart test/screens/music_playlists_screen_test.dart` — clean.
- `cd player-android && flutter test --no-pub test/screens/music_playlist_rules_screen_test.dart test/screens/music_playlists_screen_test.dart` — **11 passed**, including actual smart detail Rules/Refresh, hidden manual controls, readable rule editor and native Back, plus existing manual playlist/import/export errors. Logs `/tmp/player-ol3-native-tests.log` and `/tmp/player-ol3-native-tests-final.log` retain the failed and final runs. Existing playlist tests initially lacked the required audio handler provider; the fixture now uses the established idle handler override. The new detail assertion needed scrolling to materialize its lazy summary.
- `cd player-server && go test ./internal/service ./internal/api ./internal/repository -run 'TestSmartPlaylist|TestPlaylist' -count=1` — passed.
- `node player-server/web/js/tests/i18n.test.js` and `node player-server/web/js/tests/music-artwork.test.js` — passed; production mounted language bindings, metadata isolation, catalog placeholders, canonical keyboard tokens and artwork mounting remain intact. New web API/editor/playlist modules pass `node --check`; imported typed operators and Unicode summaries pass a focused smoke check. `git diff --check` clean.

The 26 owned implementation/doc/test paths are recorded for the orchestrator in
`/tmp/player-ol3-ownedpaths.json`; root coordinates only the new rules route and
service-worker asset/cache hook among shared source. No full regression suite,
server/APK build, browser/emulator runtime, release, version bump, push or
deployment has been performed for this slice. Task remains pending for bounded
review and integrated verification.

Pre-review self-check correction: the smart editor now passes the canonical
`Smart playlist rules` title key to the shared dialog. The production-template
Node locale check captures that real call and verifies both initial locales
switch both directions. `node player-server/web/js/tests/i18n.test.js`, new
editor syntax and diff checks pass. This adds the existing i18n regression test
to the owned manifest (26 paths). No Go/native rerun needed for this web fix.

Full-scope R1 found no blocking defects (1/2 rounds used); no second round required. Implementation remains pending integrated verification.
