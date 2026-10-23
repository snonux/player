# M32 playlist export implementation

Pre-code interaction specification (U01): playlist detail keeps its existing
artwork, title, totals, queue controls and owner actions. Add a labeled
**Playlist actions** overflow, available to owners and readable shared-playlist
viewers; its **Export M3U8…** item opens the existing themed dialog style.
No new keyboard shortcut is allocated: Tab/Enter open actions, Escape/Back
closes the dialog and returns to its launcher. Native touch targets use standard
Material menu/dialog controls. Long names and filenames wrap in the dialog.

Dialog: **Export playlist**, playlist name, **UTF-8 M3U8**, exact filename
`playlist-{playlist_id}.m3u8`, then: “Paths are relative to the server's media
root. Save this file beside the corresponding library folders in the other
tool. Only songs you can access with exportable paths are included; repeated
songs keep their order. This file does not grant access to music.” Web offers
Cancel / Download M3U8; Android offers Cancel / Save M3U8 and then the native
system document destination/name chooser (also cloud document providers).

Ready → fetching (primary button disabled, visible Preparing…) → browser file
saved/download begun or Android chooser → saved/cancelled. Offline/server/
permission failures keep the dialog open with an alert and enabled retry.
An account or playlist change invalidates pending work. Empty playlists export
valid headers. No edit permission or public share link is required; ordinary
signed-in private/shared read ACLs and track permissions still apply.

Format contract for future M31 local roundtrip: UTF-8, LF, no BOM, `#EXTM3U`,
`#PLAYLIST:` name, generic omission note, then ordered `#EXTINF:` integer
seconds (`-1` unknown) and artist/title with filename fallback, each followed
by `./{set.root_path}/{media.rel_path}` using literal forward-slash local
paths. Repeated occurrences remain repeated lines. Paths never contain host
absolute paths, stream URLs or credentials. Unsafe paths (traversal, controls,
invalid UTF-8, backslashes) are omitted; metadata controls collapse to spaces.
Leading `./` disambiguates filenames beginning with `#` and URI-like text.
M31 import is a separate task and is not implemented here.

Integrated verification remains pending: actual Chrome/Firefox keyboard and
pointer download, Android native save/cancel/Back and document-provider error,
light/dark/narrow views, empty/repeated/restricted songs and local fixture
reimport after M31. Targeted checks are recorded in the task handoff; no
runtime acceptance is claimed by this implementation record.

Focused implementation checks: `go test ./internal/service ./internal/api -run
'TestPlaylistExport|TestM3ULocalPathSafety' -count=1` passed (real SQLite
private/shared ACL, duplicates/order, title/path injection, empty export, both
API prefixes/auth/headers). Scoped `flutter analyze --no-pub` on playlist screen,
export service and screen tests found no issues. `flutter test --no-pub
test/screens/music_playlists_screen_test.dart --plain-name export` passed two
cases: failed fetch/retry/native picker cancellation/success, and dismissed
fetch preventing a late native save. Node syntax checks passed for API and
playlist view; diff whitespace check passed. Kotlin bridge compilation and
actual system picker behavior await the integrated Android build/runtime.
Export uses indexed metadata and intentionally does not open/stat audio sources;
an indexed path whose file disappeared remains in the file until the library
index is updated. No filesystem existence or roundtrip acceptance is claimed.

Stage: **IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION**. First
expert review found no blocker. The focused checks above pass; installed
Chrome/Firefox downloads, the native document-provider lifecycle, account
changes and local reimport remain deferred to integrated verification after
backlog implementation. Task jk3 remains pending.
