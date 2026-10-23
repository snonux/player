# Music discovery collections (M38 / pl3)

Implemented and reviewed in two bounded rounds; integrated verification remains.
Prerequisite implementations: vk3, kk3, lk3, mk3 (including 2l3 history/replay),
with accepted U01/V01 interaction design and fixtures. No version or release change.

## Interaction and states

The existing Albums catalog remains the browse entry. Web adds a compact
**Discover albums** disclosure above it. Android adds a labeled **Discover
albums** button that opens a scrollable native list. Both expose the same six
sections: Recently added, Recently played, Most played, Random albums, Favorite
albums and Top rated albums. Each section explains its source, previews up to
four albums, offers **Play all N albums**, and provides **View all**. Play uses
all N albums in that section's ordered snapshot; the preview is an excerpt.

Web uses the existing buttons, focus treatment and text/theme colors. Android
uses themed Material cards, ListTiles and labeled native buttons. Long preview
titles wrap on web and use two-line ellipsis on Android; artist text remains
secondary. Discovery sections scroll with content rather than occupying a large
pinned header. Existing artwork and grid/table/column controls remain in full
album browse. Genre and **Year / decade** entry points reuse the existing facet
controls, including inclusive decade ranges, rather than a second filter system.

View all opens the existing album cards with the collection's order. Collection
order disables ordinary sort/direction controls; normal browse retains saved
sorting. Full album detail, selection and queue actions reuse their existing
actors. Random snapshots retain order while browsing detail and returning;
a fresh page/app start or explicit discovery refresh may choose another order.
The web URL carries a validated `mcollection` choice; an explicit album detail
route keeps precedence. It does not encode random samples.

Back returns from album detail to its source catalog. Native Back returns from
a collection to discovery, then to ordinary album browse, before leaving the
screen. Web discovery uses native disclosure Enter/Space and local Escape;
existing card j/k/h/l, arrows, Enter, player keys and shortcut hints keep their
owners. No global shortcut is added. Native actions use touch controls.

Loading, connection/permission errors and Retry use the existing catalog flow.
Discovery does not invent cached metadata or infer access while offline.
Each empty collection gives a concrete next action: scan music, listen to music,
favorite an album or rate one. A failed collection resolution reports the
existing queue-action error and preserves the current queue. Account changes
clear native collection state and rebuild from the new authorized catalog.

## Sources and API

`GET /api/music/albums` and its `/api/v1/` alias accept optional `collection`:
`recently_added`, `recently_played`, `most_played`, `random`, `favorites`,
`top_rated`. Unknown choices return 400. Other existing filters still apply.
Detail routes retain their detail response. Empty collections return `[]`.

Album summaries/details add `added_at`, the latest creation date among the
album's caller-visible music files. Replacing or editing music tags does not
manufacture a newer date. The existing permission-aware catalog groups albums
before summaries; hidden songs cannot contribute dates, counts or membership.
Recently played uses the account's latest qualified play date. Most played sums
its qualified song plays. Favorites and top rated use the account's album heart
and album stars; global file counts and song ratings do not substitute for them.
No-play albums are absent from play-based collections; unrated albums are absent
from top rated. Remaining ties use normalized title and canonical album key.
Random includes every accessible album once, in shuffled order.

Preview helpers consume that same authorized album payload. Album Play resolves
canonical album keys through the existing collection resolver immediately before
using the ordinary queue owner; album disc/track order is preserved. A failed
access/load does not partially replace playback. Existing large-queue recovery
storage limits are unchanged; no collection silently truncates to its preview.

## Focused checks and remaining acceptance

Run so far: Node `web/js/tests/music-discovery.test.js` and
`web/js/tests/navigation-url.test.js` pass; catalog/discovery JavaScript syntax
checks and owned diff whitespace checks pass. Leaf Go discovery tests pass:
`go test ./internal/service -run 'TestDiscoverAlbums|TestMusicAlbumAddedDate' -count=1`.
Focused API `go test ./internal/api -run TestMusicAlbumsRealPermissions -count=1`
passes (collection ACL/date, empty sources and invalid collection included).
`dart format` on the seven native source/test paths passes. Scoped
`flutter analyze --no-pub` on those seven paths is clean. Native
`flutter test --no-pub test/services/music_discovery_test.dart test/widgets/music_discovery_panel_test.dart`
passes both tests: personal-source ranking/membership and narrow-screen
long-title/empty-source/View all snapshot behavior. Initial analysis found two
style diagnostics, both fixed. The initial panel test tapped before scrolling
completed; settling and ensuring visibility fixed the test and the rerun passes.
These focused checks are implementation evidence, not real platform acceptance.

Full regression suites, final server/APK builds, errcheck, and actual installed
Chrome, Firefox and native Android emulator acceptance are deferred to integrated
verification by the implementation-first workflow. No new-feature platform
acceptance is claimed. Required journeys remain: populate independent accounts'
qualified plays/hearts/stars; inspect and Play every collection in order;
exercise random/detail/Back and genre/year/decade filtering; verify saved queue
source/audio; inspect light/dark/narrow and long titles; test keyboard focus and
native touch/Back, empty sources, denied/deleted media, offline/retry and restart.

Round 1 found that random collection array ordering also ran on album-detail
objects, and that shuffle tests could pass without shuffling. Both ordering
branches now exclude album detail. Production render tests cover in-app Random
→ View all → album and a cold random/detail URL; both pass. Web and native
helper tests now require the exact injected zero-RNG permutation `[b,c,a]`.
The affected native helper test and analysis pass. Round 2 confirmed both fixes
without finding another blocker; reported passing checks were reused. Root
added the album-collection API contract and SW v70 module asset. Full suites,
builds and actual Chrome/Firefox/native acceptance remain pending.
