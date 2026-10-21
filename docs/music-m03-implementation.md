# Music M03 artist catalog and credits

Task `zj3`, extending M01/M02 and U01. Interaction design recorded before
feature code; parent approved this scope before implementation.

Music gains Artists next to Albums in the existing web sidebar/native drawer.
The default Artists list includes names credited as track or album artists;
a secondary native select/dropdown labeled Role offers Track artist, Album
artist, Composer and Performer. Composer/performer-only people appear when
that role is selected. Names preserve punctuation and Unicode; explicit
repeated tags identify separate people. No guessed biography, photos, play
statistics or external enrichment is added.

Web layout: existing shell → Back to Sets, Artists heading, Role select →
existing focusable cards showing name and visible role labels, distinct album
and song counts. Enter/click opens Artist detail with name, credited roles,
counts and album cards labeled with this person's roles on each release.
Album links open the existing ordered album detail; track artist and album
artist links open the actual artist detail. Song Info exposes Composer and
Performer alongside distinct Track artist and Album artist labels. Artist
Back restores role, selected artist and scroll; album Back restores artist
and selected album. Existing j/k/h/l/arrows/Enter, / typing, Escape/Backspace,
Space/p and ; hints apply; no new global shortcut.

Android uses the existing Material scaffold, labeled drawer Artists entry,
Role dropdown, 48dp list tiles, native pushed artist/album routes, and Back.
Artist detail lists credited release roles and reuses album cards; pushed
routes retain the prior list scroll and role. Song details retain the Music
details expansion and add readable Composer/Performer fields. Album artist
links and song Track artist links open the native artist detail; multiple
structured people are individually linked.

Transitions: list loading → populated or explanatory empty; opening artist →
loading → detail; missing/revoked key → unavailable with Back/Retry; network
failure/offline → readable connection failure with Back/Retry. Retry reloads
current role/detail without resetting browsing position. Switching roles
resets selection to the new list. Long names wrap in detail and truncate only
with full accessible names on cards. Existing light/dark tokens, focus rings,
artwork placeholders and responsive spacing apply. Browsing never alters the
current playback snapshot. No destructive action needs confirmation.

Data extends existing JSON music metadata with composers/performers arrays;
scan/rescan and offline snapshots keep their existing identity/state behavior.
Artist aggregates derive from permission-filtered MusicAlbums songs, with
normalized name keys (existing ar_ hash convention), deduplicated song/album
counts, explicit role membership and ACL-safe artwork. Artist detail returns
only visible credited releases and songs; hidden/unknown keys are identical
404s. Album grouping/fallback identity stays unchanged; missing album artists
fall back to track artists as M02 already does. Untagged music remains under
Unknown artist for common browse, without invented composer/performer credits.

Acceptance extends the reusable M02 fixture/runners with a compilation
featuring distinct track artists, album artists, composer-only and performer-
only names; repeated credits, punctuation/Unicode, missing credits and a hidden
set. Real Chrome/Firefox journeys cover role switching, actual linked
artist→album→Back list-position/focus, light/dark/narrow, typing, empty/error,
permission revocation and retry; native API34 covers equivalent touch/Back and
scroll retention. Existing album playback remains exercised with authoritative
source/audio proof. Evidence belongs in /home/paul/tmp/zj3-evidence and a fresh
zj3 fixture; root owns builds, full gates, reviews and final completion.

## Implementation and focused verification checkpoint

The approved implementation adds permission-derived artists and explicit
track/album/composer/performer roles to existing album aggregates. It preserves
structured person boundaries and keeps the established Info label Artist
alongside Album artist for compatibility; linked controls identify Track artist
and Album artist explicitly. No enrichment or statistics were introduced.

Focused evidence in `/home/paul/tmp/zj3-evidence`: `go-targeted.log` passes the
artist role/count and real SQLite permission tests, repeated-FLAC composer/
performer parsing, and real scan/rescan change/removal with unchanged identity,
progress and source size. `flutter-targeted.log` passes 10 artist/album/model
checks, including offline Retry, secondary Role empty state and cached credits.
Scoped Flutter analysis is clean (`flutter-analyze-targeted-final.log`). Node
metadata, navigation URL and native Enter tests pass; Python runner compilation,
pyflakes and diff whitespace checks pass. The full TypeScript command was
attempted and failed on the baseline configuration's missing DOM library across
many existing files (`typescript.log`); this is not a clean TypeScript claim.

Fresh actual acceptance runners are `music-artists.test.ts` (Chrome and Firefox)
and `music_artists_e2e.py` (native API34). They require distinct visible names
for each role, actual performer detail, visible Composer/Performer Info fields,
original source audio, offline/permission Retry, and actual scrolled Artists →
artist discography → album → Back → Artists position restoration. A dedicated
synthetic long-name artist with fourteen releases makes a genuinely scrolled
discography possible without changing baseline album identities. Final runtime, full-gate and review results follow below.


## Final full gates and actual platform acceptance

The parent completed Flutter analysis with zero issues and all 1,364 tests,
Go build/vet and full race/coverage tests, and the complete Node/service-worker
suite successfully. Logs: `flutter-analyze-full.log`, `flutter-test-full.log`,
`go-build-full.log`, `go-vet-full.log`, `go-test-full.log`, `web-unit-full.log`
in `/home/paul/tmp/zj3-evidence`. Required errcheck ran and retained the same
256 pre-existing diagnostics, with zero new diagnostics against the prior
baseline (`errcheck-current.log`, `errcheck-comparison.json`); it is not clean.
Scoped M03 TypeScript compiles with explicit DOM library options
(`typescript-m03-dom.log`); the repository's full baseline TypeScript
configuration still omits that library. Fresh independent review round one
found no blocking product issue. No additional review round was needed.

The fresh isolated fixture is `/home/paul/tmp/zj3-fixture`, port 18803, with
synthetic media, separate random-password administrator/viewer accounts and no
service doubles. Server binary `player-final` reports version 0.8.2 and revision
`c223e50f265dcd5c41e226f86ebb243705aaa8a7-dirty`. Root built and independently
verified `app-x86_64-release.apk`, version 0.8.2/code 4022, SHA-256
`2dd993403012706cf3f600777c3205dab4e68306f138e2f5869f3a2f5759ceec`.
Exact invocation and process ownership are external in `runtime-commands.json`,
`runtime-ownership.json` and `emulator-ownership.json`.

Actual installed Google Chrome 144.0.7559.132 and Mozilla Firefox 156.0 passed
all six M03 cases in `browser-attempt2.log` (25.2 seconds, terminal exit 0).
Each browser verified visibly distinct track, album, composer-only and
performer-only entries, actual performer and composer discography, individually
linked structured people, visible Song Info Composer/Performer values, native
typing, real browser offline Retry and actual permission revocation/empty
results. The original Unicode FLAC source played audibly at approximately
311.1 Hz. Real Artists scroll offsets 507/505 and discography offsets 1762/1720
were nonzero and restored with exact selected-card position and focus after
artist → album → Back → Artists. Light/dark, desktop/narrow and long-name
original images were inspected by implementer and parent. Narrow card text
uses the existing ellipsis with full accessible names/title; detail role
headings remain readable.

The first browser attempt retained four failures and two position passes:
the harness expected a performer tag in an M4A that the iTunes muxer did not
store, and attempted sidebar input while the existing search overlay remained
open. The performer oracle now requires the actual repeated-FLAC Unicode
release, and typing closes search with normal Escape. Product code did not
change. Attempts one and two initially shared `runtime/{chrome,firefox}`;
final accepted originals there are from attempt two, while JSON retains both
attempts and `browser-attempt1-results` preserves original failed screenshots
and error contexts. The prefix snapshot `runtime-attempt1-and2-prefix` is
explicitly labeled as combined. No additional run was made solely to change
evidence directories.

Native acceptance used one owned cold boot of `Player_FDroid_Test_API34`,
Android 14/API 34, x86_64, density 440, with QEMU WAV output and the root-built
APK above. `native-attempt1.log` verifies all four visibly distinct role lists,
performer/composer discography, structured track/album links, role retention
on Back, readable credits and light/dark layouts. Original FLAC source output
matched 415.3 Hz for 3.93 seconds. Actual scrolled Artists required two swipes
and discography four swipes; the original and restored artist bounds were
both `[0,1552,1080,1970]` and release bounds both `[44,1703,1036,2110]`.
Long-name light/dark originals were inspected. Cold app restart remembered
the locked viewer account; explicit password sign-in reopened Artists →
Composer → the correct two-album/three-song discography.

That initial native run retained one failed offline scenario: removing adb
reverse left an existing Dio keepalive connection usable, so the requested
error correctly did not appear. The oracle was preserved. Focused errors-only
recovery (`native-errors-recovery.log`, terminal exit 0) stopped only the owned
fixture server with verified binary/PID/start identity, confirmed it unreachable
and removed reverse. The actual app displayed an offline error with Retry.
Restarting the same server binary/database/media and restoring reverse let
Retry open the same artist. Actual set permission revocation then produced the
unavailable-artist screen; restoring permission and Retry recovered that same
artist. Normal runner teardown cleared app data and reverse only. Initial
native evidence remains in `runtime-native-attempt1/android`; focused error
originals and JSON are separate in `runtime-native-errors-recovery/android`.
The parent accepted all required original screenshots, logs and state/audio
proof before cleanup.

These results establish M03 on all three required platforms. The browser suite
uses pointer/keyboard input; system Firefox's known BiDi touch-emulation limit
remains unchanged. Native screen-reader labels were exercised through semantics;
TalkBack and physical Android hardware were not driven. QEMU capture uses stream
time, with no new gapless/EQ claim. No biography, external enrichment, play/rating
statistics, release/version bump, push or deployment was added.

The owned server, emulator and isolated sink are stopped only after parent
acceptance. The external fixture database/media/accounts, APK, WAVs and all
attempt evidence remain preserved; no destructive fixture-down command is used.
Cleanup identity and terminal checks are recorded in `runtime-cleanup.json`.
