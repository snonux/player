Stage: IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION.
Two bounded expert rounds complete; duplicate web preview fixed and R2 clear.

# Custom artist and playlist artwork (bk3 / M11)

Implementation stage; integrated verification pending. Applies U01 and predecessors
nj3/oj3/zj3/3k3: stable catalog artist keys, persisted playlist IDs, current ACL.

Desktop/narrow web: artist detail heading and playlist detail artwork slot retain
square artwork dimensions. Below it: Change artwork… and Remove artwork (only
when present). System file picker cancellation preserves the image. Successful
replacement updates the preview in place; removal requires confirmation and
returns to the existing local cover/placeholder. No new keyboard shortcuts;
Tab/Enter and native chooser/confirmation preserve library shortcuts.

Android: the same detail header has square image, labeled Change artwork and
Remove artwork controls, native document picker, confirmation dialog and normal
Back. Existing Material theme/touch targets apply. Loading/error text remains
adjacent; failed writes retain the saved preview and offer retry. Changes are
server-persisted, not offline queued. Read permission loss clears old artwork.

Artist art is global, writable by administrators only. Playlist owner/admin uses
existing CanEdit; shared viewers see artwork without editing controls. Each
request checks current access. Custom image precedes local song covers;
external provider images remain secondary within About. JPEG/PNG/GIF input is
bounded by bytes and pixels, decoded fully and normalized to PNG (first GIF
frame). Persistent content digest revisions invalidate replacements across
clients; protected image responses cannot be served without revalidation.

Required integrated acceptance: Chrome, Firefox and native Android upload,
replace, cancel picker, remove cancel/confirm, invalid/oversize input, denied
viewer, access revocation, restart persistence, light/dark/narrow layout and
focus/Back. Deferred under implementation-first authorization; no platform
acceptance or full suites/builds claimed here.

Implementation checks (2026-10-23):
- `go test ./internal/service ./internal/repository ./internal/api -run 'TestNormalizeMusicArtwork|TestMusicArtwork|TestMusicQueueAlbums' -count=1` passed.
- Scoped Flutter analysis on API extension, artwork widget and artist/playlist
  screens passed with zero issues.
- `flutter test --no-pub test/widgets/music_artwork_section_test.dart test/screens/music_artists_screen_test.dart test/screens/music_playlists_screen_test.dart`
  passed all 13 tests. Existing screen fixtures now answer artwork status explicitly.
- Node syntax checks passed artwork module and both changed view modules.

No full regressions, final builds, errcheck or actual browser/emulator acceptance
run for this implementation slice. Native Kotlin picker still needs final Android
build and actual picker checks. Reviews and integrated acceptance remain pending.

Existing playlist catalog covers also prefer custom art: server list/detail returns
only the digest revision (no image BLOB reads for catalog metadata); web/native
fetch the protected image and fall back to ordinary song covers on load failure.
Returning from detail reloads catalog metadata, so removal/replacement invalidates
old image revisions. Artist catalog retains its existing text-only presentation.

R1 found one blocker: playlist detail rendered an ordinary/custom cover outside
its artwork editor, leaving two images and a stale cover after replacement,
removal or permission loss. The detail cover host now starts empty; the editor
owns its only preview and supplies the ordinary fallback. A focused Node test
mounts the production editor and invokes Change/Remove handlers, asserting one
image through replacement/removal and zero after permission denial, plus an
integration assertion that the detail host starts empty. This test, syntax check
and diff check passed. No other product files changed for the R1 repair.
