Stage: IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION.
One bounded expert review found no blockers; integrated acceptance remains pending.

# M41 · Related library music (nk3)

Stage: implementation prepared; bounded review and integrated verification pending.
Prerequisites nj3 (U01), oj3 (V01), yj3 (M02), zj3 (M03) are accepted.

## Interaction design

The existing details remain primary. Related suggestions follow the album's songs,
the artist's albums, or a song's metadata. They never start playback on load.

```
Album: artwork / title / credits / existing Play, heart, rating
       ordered songs and their existing actions
       More like this · From your library / shared music tags
       [artwork] Release title
                 Genre: Jazz · Artist/credit: A
                 [Open album] [Play]
Artist: identity / roles / counts / existing controls
        albums
        More like this → artist cards with genre explanation / Open artist / Play
Song: existing information
      More like this → song title / explanation / Open song / Play
```

Web uses the current heading, card-body, album-cover and button styles/theme
colors. Native uses Material Card, title styles, 80px artwork, labeled buttons
and the existing collection controls. Cards stack on narrow layouts; long names
wrap. No new shortcut: Tab/Enter activates the labeled buttons without changing
the existing catalog selection scope, vi navigation, typing or player shortcuts.
Opening a result uses the normal catalog history, song dialog or native pushed
route. Catalog/native Back returns to the source; Escape closes the web song
information dialog and returns focus through the existing modal focus manager. Result playback explicitly replaces the queue,
using the existing collection resolution and queue machinery. Native collection
cards retain their existing Shuffle/queue actions.

Loading → local results, explanatory empty state, or error with Retry. A valid
untagged source returns empty results; unavailable or permission-revoked source
returns 404. Offline/provider-like library read failures show an error with Retry
while primary details remain usable. No external provider is required or called;
there is no acoustic inference. Synthetic Unknown artist/Various Artists names
do not create a relationship. Existing favorites, ratings and personal counts
in the primary details are preserved.

## Matching contract

Authenticated same-kind recommendations use only the current caller's readable
non-podcast/non-audiobook catalog. Songs/albums match exact normalized genre or
structured credited person; artists match genres on their credited songs. Rank
by number of shared metadata values, then normalized title and stable key.
Return at most 12 distinct entities, excluding the source, with a shared-tag
explanation. Missing years, names or genres are never treated as similarities.
No additional database or provider configuration is introduced.

## Targeted checks and remaining acceptance

Passed targeted checks:

- `cd player-server && go test ./internal/service ./internal/api -run TestMusicRelated -count=1`
- `cd player-server && go vet ./internal/service ./internal/api`
- `cd player-android && flutter analyze --no-pub lib/api/player_api_client.dart lib/widgets/music_related_section.dart lib/widgets/music_collection_actions.dart lib/screens/music_albums_screen.dart lib/screens/music_artists_screen.dart lib/screens/media_detail_screen.dart` (no issues; three initial brace-style infos corrected)
- `node --check` on the new module, catalog and song information modules; `git diff --check`.
- `node player-server/web/js/tests/music-related.test.js`: actual installed Play listeners with deferred A/B resolution; the newer click owns playback before either read completes and stale A cannot mutate the queue.

Related playback claims source intent at click (after the existing queue-loading guard). Native artwork uses the shared authenticated loader and endpoint router.

Errcheck and full regression checks are deferred with the integrated gates. Full suites, final builds, light/dark/narrow visual review, actual installed
Google Chrome and Firefox keyboard/pointer/audio journeys, and native Android
emulator touch/Back/audio/offline/restart/ACL acceptance are deferred to the
integrated verification phase under the current user workflow. No screenshot or
platform acceptance is claimed. Verify deterministic matches, open/play each
kind, source exclusion, empty tags, denied sources and real read failure/Retry.
