# Artist top songs (3l3 / M40)

Implementation stage: reviewed in round 1 with no findings; awaiting integrated verification.
Prerequisites nj3/oj3/zj3 are accepted; mk3 personal listening data is reviewed
in a73cc08 and awaits integrated verification.

Artist detail now places **Top songs** above the discography in the vanilla
web UI and native Android app. **Play top songs** replaces the music queue
with the ranked list. A song's labeled Play action starts that song and the
remaining ranked songs. The existing Play artist action stays available.
Both actions use the existing account/session queue actors and Undo behavior.

The server first loads the caller's permitted music catalog, then selects
unique canonical songs credited to the artist, including composer/performer
roles consistently with artist playback. If any song has qualified personal
plays, rank only personally played songs by that count. Otherwise use positive
server-wide media play counts. Ties sort by normalized title/filename and media
ID; return at most ten songs. No external popularity provider is configured or
required, and no claim of externally best-known songs is made. The section's
secondary source text distinguishes personal and server-wide data. With neither
source, it says **No top songs yet** and suggests Play artist to build a local
ranking. There is no arbitrary alphabetical list presented as popularity.

UI states recorded before coding: artist loading -> header and top songs ->
discography; offline/permission/read failures -> existing artist error + Retry
and Back. Ranking is additive artist-detail data, so refresh retries the same
permission-aware request. Native uses themed Material headings, wrapping titles,
48dp touch controls and existing mini-player/Back. Web uses theme text classes
and normal Tab/Enter buttons; vi grid navigation and selection stay scoped to
the discography, with existing semicolon control hints reaching song buttons.
No new global shortcuts or dialogs. Playback failure uses existing queue
feedback and inline web status/native snackbar.

Focused implementation checks cover source selection, personal/global
separation, stable ties, deduplication, unrelated credit exclusion, ten-song
limit and the real SQLite permission-filtered artist response. They do not
constitute platform acceptance.

Integrated verification remains required: full suites/static analysis/final
builds, real Chrome and Firefox keyboard/pointer/source-audio journeys and
native Android touch/Back/source-audio; personal ranking, global fallback with
no personal data, no-count empty state, ACL revocation and Retry, offline,
restart, light/dark/narrow/long-title rendering. No browser/emulator run or
release/version change is included in this implementation slice.

Cheap checks completed: `go test ./internal/service ./internal/api -run
'TestMusicArtistTopSongs|TestMusicArtistsRealPermissions' -count=1` passed;
`flutter analyze --no-pub` on the artist screen, top-songs widget, collection
catalog helper and artist test found no issues; `flutter test --no-pub
test/screens/music_artists_screen_test.dart` passed all four tests. The first
widget run exposed the pinned-header overflow; it was fixed with optional
`listHeader` on the existing catalog so artist detail and all ten ranked long
titles scroll to discography at the 800×600 test surface. A later fixture-only
pending related-footer Dio timer was resolved by stubbing the unrelated
relatedMusic read. No test timeout or assertion was weakened. Both changed web
modules passed `node --check`; `git diff --check` passed. Root added SW v66
and its new module asset. Full errcheck and full gates remain deferred.
