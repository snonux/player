# M31 filesystem M3U import and following

Pre-code U01 contract: Music → Playlists retains existing cards, Create playlist,
artwork and queue chrome. A second primary action, Import M3U…, opens the existing
web dialog / native Material dialog. Fields: Server library path (for example
Music/lists/trip.m3u8), Paths relative to [Playlist folder | Library root], and
Follow source changes (off by default). Source files must be inside a set the
account can access. Library root explicitly supports M32 exports; ordinary M3U
paths default to the source file's folder. No upload or external URL fetch.

Preview shows name, available occurrences, unavailable count, rejected unsafe
count, and one-time versus ongoing behavior. Editing options invalidates preview.
Loading disables submit; errors/offline retain input and allow retry. Confirm
imports a new private playlist. Empty imports are allowed with explicit counts.
Order, Unicode and repeats survive. Long paths wrap. Existing modal focus,
Escape/Back, native typing, light/dark tokens and responsive controls apply;
no shortcut is allocated. Native uses standard labeled touch controls.

Editable detail shows source path/base/status and Sync now / Stop following.
Stop following requires confirmation and retains membership. While following,
manual membership editing is disabled (server enforced); metadata remains manual.
Ownership transfer detaches the source, preventing inherited filesystem authority.
Shared readers see ordinary accessible songs, never source paths or source counts.

Bindings refresh on owner list/detail reads, explicit Sync now and successful
library rescans. Missing/invalid/inaccessible sources retain last membership and
show a generic unavailable status. Successful refresh atomically replaces order
and counts; equal membership preserves occurrence IDs, making repeated scans
idempotent. Track ACLs are evaluated for the playlist owner on every refresh.
Source reads are bounded, UTF-8 only, regular files, confined to MEDIA_ROOT and
reject symlinks. Relative paths may use .. within the library; URLs, absolute
paths, backslashes and paths escaping the root are rejected without fetching.
Unavailable and inaccessible track names are never returned. EXTINF is ignored
as descriptive metadata; PLAYLIST supplies initial name only. No XSPF support.

Verification stage: implementation first. Targeted checks only, with full suites,
final builds and actual Chrome/Firefox/native Android journeys deferred to the
integrated phase. Required scenarios include Unicode relative paths/repeats,
unsafe/outside paths, M32 roundtrip, follow changes/idempotence, lost permissions,
source deletion/recovery, detach/transfer and restart. No acceptance claimed.

Focused implementation checks (2026-10-23, no runtime acceptance):
`GOMAXPROCS=2 go test ./internal/service ./internal/repository ./internal/api
./internal/app ./internal/scanner -run
'TestPlaylistImport|TestResolveM3UPath|TestPlaylistExport|TestM3ULocalPathSafety|TestPlaylistVisibility|TestPlaylistOwnership'
-count=1` passed: real SQLite service/parser/Unicode/repeats/M32 roundtrip,
binding persistence after DB reopen, source failure/ACL recovery, shared-reader
JSON privacy, stale refresh after ownership transfer, both HTTP aliases/auth,
and production rescan wiring. Repository/scanner compiled but had no matching
standalone tests in that selection.

Scoped Flutter analysis of API, playlist screen, import dialog and its tests
passed with zero issues. `flutter test --no-pub
test/widgets/playlist_import_dialog_test.dart
test/screens/music_playlists_screen_test.dart` passed 9 tests; one additional
focused source-status/read-only/detach-confirm screen case passed after it was
added. Import cases cover preview/counts/follow confirmation, editing invalidating
preview, retained offline input and dismissed late response. Existing duplicate
editing test now targets the song's tooltip, because the pre-existing M32 app-bar
menu invalidated its broad last-PopupMenuButton selector; its move/remove/Undo
assertions remain unchanged. Initial failed run and final logs are retained.
Node navigation URL and collection resolver tests passed (2 files); modified
API/view/SW syntax and diff whitespace checks passed. Scoped errcheck exits 1
with 238 diagnostics in the selected existing packages, none in new import
files or new code; this is not a clean errcheck claim or a full baseline audit.
Logs: `/home/paul/tmp/player-ik3-evidence/` (outside tracked source).

Follow state is stored as JSON in the existing playlist row; its existing user
cascade/orphan cleanup already owns the binding. No additional table or orphan
list is needed. Source handles are rooted inside the authorized immediate set,
check its opened inode, and open nonblocking before checking regular-file type;
source errors never return host paths. Existing metadata/visibility ownership
rules, M32 export/native save and duplicate-occurrence queue playback remain.
There is no new web asset module. Root owns the final SW cache-generation bump
when committing this reviewed slice (v47 belongs to the preceding presentation
slice; M31 will use v48). Versions unchanged.

Stage: **IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION**. Actual
installed Chrome and Firefox import/focus/typing/light-dark/narrow flows, native
Android touch/Back/account changes, source changes/rescan/restart and audible
playlist playback remain deferred along with full suites and final builds.
No server, browser, emulator or build runtime was started for this slice.


Root fresh review round 1 reported no blockers; no second review was required.
The nonblocking admin-control observation is fixed: web/native Sync now is
shown only when the signed-in user owns the binding. An administrator viewing
another owner's playlist retains Stop following, metadata editing and ownership
transfer. The service's owner-only filesystem refresh policy is unchanged.
Affected native screen/test analysis passed with zero issues, and the two
focused owner-sync/admin-detach cases passed. Node playlist-view syntax and
diff whitespace checks passed. Review-fix logs are `review-ui-analyze.log` and
`review-ui-test.log` in the external evidence directory. Product is refrozen;
root owns commit and the final cache-generation bump. Runtime acceptance remains
pending as documented above.

Stage: **IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION**. First
expert review found no blocker. The minor administrator action issue is fixed:
Sync now is owner-only in both clients, while administrator detach/edit/transfer
remain available. Two affected native owner/administrator cases and analysis
pass; no second review was needed. The focused Go/native/Node checks above
remain valid. Full suites, final builds and actual Chrome/Firefox/Android
import, follow-change and round-trip journeys remain deferred until backlog
implementation is complete. Task ik3 remains pending.
