Stage: IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION.
Two bounded expert rounds complete; both R1 search blockers fixed and R2 clear.

# M35 personal five-star music ratings (lk3)

Implementation stage: awaiting bounded review and integrated verification. No
browser/emulator acceptance or release/build claim.

Existing completed catalog/ACL work nj3, oj3, yj3 and zj3 supplies source identity,
metadata, albums and artists. The separate hearts from kk3 remain independent.

## Interaction design and transitions

Web album/artist cards and details place a compact star selector beside the heart;
album songs and song/album/artist search results have their own labeled selector.
Native catalogs use a separate star menu alongside hearts, album detail provides
album and per-song controls, and search provides all three entity controls. A star
menu/selector offers 1–5 stars and **Unrated · Clear rating**. No shortcut is added;
web native selects keep typing/arrows/Enter and Escape follows existing hierarchy.
Native menus have ordinary touch targets, semantics and Back dismissal.

Catalog toolbar secondary **Rated only** and **Highest rated** controls combine
with existing favorites, roles, metadata facets and layout controls. Songs can be
filtered and sorted in search; album detail preserves disc/track playback order.
Existing light/dark theme controls and typography are reused without new colors.
Native controls sit in rows with existing actions; titles retain their existing
ellipsis/scroll behavior. Narrow-screen, long-title and light/dark appearance still
need real platform review during integrated acceptance.

Change flow: select stars → disable while saving → save explicit idempotent value
→ show saved value and concise feedback. Failed/offline/permission writes keep the
previous value and allow retry. Web refreshes authorized catalog data after saving
and restores the matching star selector where it remains visible; clearing a star
under Rated only removes that result and existing catalog focus recovery applies.
Native successful writes update the live entity map and current local filters;
provider/credential-generation guards discard late results after account changes.
Loading/error/retry states reuse existing catalog screens. Empty rated catalogs
explain that turning Rated only off allows rating another item. Native popups
close with Back; web label clicks never open/play a row.

## Persistence and API

`PUT /api[/v1]/music/ratings/{song|album|artist}/{key}` takes `{"rating":0..5}`;
zero clears. Song keys are canonical decimal media IDs. Catalog-derived ACL checks
run before reads/writes; response stars belong only to the authenticated account.
`rated=true` and `sort=rating` filter/order lists and search results independently
for each entity type. Details keep original song sequencing. Hearts are unchanged.

`music_ratings` has a user FK, bounded stars and unique account/kind/key. Songs
also have a media FK and canonical identity check; deletion cascades and existing
explicit cleanup/orphan passes include ratings. Nullable media IDs represent
album/artist keys. The shared orphan helper now guards `IS NOT NULL` before
`NOT IN`: SQLite treats NULL NOT IN an empty media table as true, which otherwise
deletes entity ratings at startup when no media rows exist. A real restart test
caught and verifies this boundary.

## Cheap checks and remaining gates

Passed targeted Go API/repository/service music tests, including explicit rating
set/change/clear, malformed/bounded values, account isolation, read-permission loss,
empty rated collections, song deletion and real database restart/user cleanup.
Final scoped Flutter analysis passed nine changed API/model/screen/widget/test files. Node rating, layout,
search and facet tests passed. Native deterministic rating control tests cover
set/change/clear/error rollback and filter/order; those and music layout tests pass
(4 tests).

The broader existing native catalog/layout test invocation returned 9 passes and
7 failures: six pumpAndSettle timeouts across album collection and search tests,
and one artist empty-message mismatch. The wording regression was corrected by
preserving the original message when Rated only is off. No cause is asserted for
the timeouts and no passing rerun is claimed. Root coordinates scoped follow-up.

Deferred by current user workflow: full Go/Flutter/Node regression and static
analysis gates, final builds, installed Chrome/Firefox and native emulator journeys,
actual light/dark/narrow/long-title review, keyboard/touch/Back, cross-client saved
state, account isolation, permission/offline/restart acceptance and evidence.

## Review round one fixes

The bounded fresh review found two blockers: Rated only web search could re-add
excluded songs through generic Files fallback, and native empty search ignored
Highest rated while deciding to show the initial prompt. Both are fixed. The web
production-renderer regression verifies no unfiltered fallback call/playable Files
when Rated only excludes a matching song. A native production-screen regression
opens Highest rated with an empty query, observes returned results, toggles Rated
only on then off, and verifies Highest rated stays applied. Targeted Node search
and native ratings tests pass; latest two-file analysis is clean. Existing catalog
pump timeout gaps remain deferred, with no broad rerun or platform claim.
