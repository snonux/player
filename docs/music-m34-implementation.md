# Music entity favorites (M34 / kk3)

Implementation stage: IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION.
One bounded expert review found no blockers. Favorites-empty guidance and native
late-heart credential guards were tightened after review.
Prerequisites nj3/oj3/yj3/zj3/3k3 supply indexed identities, authorized catalogs and persistent playlists.

## Interaction design

Existing album, artist and playlist catalog cards carry a heart with its entity
scope in the accessible label. Details place the same control beside existing
primary actions (Android app bar). An album heart never favorites its songs.
Catalogs expose Favorites only, preserving existing role, metadata and layout
controls. Empty results keep the filter reachable. Clearing the filter returns
to the full catalog. Existing theme controls supply light/dark colors and existing
responsive cards/native rows supply narrow layouts; no new color palette.

Saving disables the individual heart until its idempotent request finishes.
Success changes its pressed/selected state and reports Added to favorites or
Removed from favorites. Failure retains the saved state and reports a connection
error; pressing the heart again retries. Server errors remain authoritative.
There is no offline mutation queue. Existing loading/error/retry states apply to
collection reads. Hidden/deleted entities cannot be newly favorited; previously
saved keys remain private and only appear after current read access checks.
Shared playlist readers can favorite the playlist without gaining editing rights.

Web controls are native buttons and a checkbox; no new shortcuts conflict with
vi navigation. Heart clicks stay within the current card and retain focus.
Android uses native labeled IconButtons, existing Back behavior and catalog
navigation. Long names use existing card truncation/detail text behavior.

## Storage and API

Separate music_entity_favorites rows use (user_id,kind,entity_key) as identity.
File favorites retain their existing table and behavior. PUT and DELETE
/api/v1/music/favorites/{album|artist|playlist}/{key} save explicit state after
permission-aware entity validation. The legacy /api alias is registered too.
Album, artist and playlist list/detail responses expose is_favorite.
Catalog lists accept favorite=true and filter after current access checks.
No raw stored key-list endpoint exposes inaccessible entities.

## Verification stage

Targeted coherence checks are recorded in task kk3 annotations. Full suites,
final builds, installed Chrome/Firefox and native emulator acceptance, light/dark
and narrow viewport captures are deferred under the 2026-10-22 implementation
first workflow. None is claimed by this implementation note.

Integrated scenario: favorite/unfavorite each entity in one client; reload and
switch clients; verify Favorites only and account isolation, permission loss,
shared read-only playlists, failed/offline saves and unchanged song/file hearts.
Exercise web native typing/focus/Back and Android touch/Back.
