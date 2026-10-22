# M04 multi-disc and compilation handling

## Approved interaction and data contract

Task `7k3` extends the existing album catalog and structured artist links.
The canonical `songs` list retains disc/track order, unknown numbers last,
then filename/path/id. Disc headings never become selectable song rows and
all playback actions retain the global song index across headings.

Embedded metadata adds `disc_subtitle` (discsubtitle/disc_subtitle/discname/
disc_name) and explicit `compilation` (compilation/tcmp/cpil; 1/true/yes).
These appear in existing Info/Music details, including saved snapshots.
Only flagged compilations without album artists use derived Various Artists
album grouping and album-role links. Embedded album artist and structured
track credits remain intact. MBID and explicit album-artist identities retain
the existing rules; ordinary missing album artist keeps its fallback.

Album detail adds `discs`: number (0 means unknown), optional subtitle and
cover_media_id, and ordered song_ids. These derive only from authorized
songs. First nonempty subtitle/art in canonical order wins within a disc.
Catalog and artist summaries omit disc detail. Existing nearest-folder,
ancestor and embedded artwork precedence and authenticated thumbnails apply.

Web layout: existing album cover/title/artist/Play actions, then compact art
and **Disc 1 — subtitle**, song rows, **Disc 2 — subtitle**, song rows.
Unknown disc is **Unnumbered disc**. Headings appear for multiple discs,
disc totals greater than one, disc numbers greater than one or subtitles.
Ordinary single-disc albums retain their simple rows. Multi-disc numbers
use disc-track. Heading text wraps with theme tokens; long links retain full
accessible names. Missing or broken artwork uses a placeholder.

Android uses the same hierarchy with Material headings and compact authenticated
artwork. Existing touch controls, 48dp links, song menus and Back apply.
No new keyboard shortcut or playback semantics are introduced. Web j/k/arrows,
Enter, Space/p, search typing, Info/Escape and restored selection remain usable.
Loading, empty, offline, unavailable/permission errors and Retry retain their
existing surfaces; absent subtitle adds no empty text and hidden counts are
never inferred.

Root owns actual installed Chrome/Firefox/API34 acceptance, combined reviews,
full gates and final builds. Acceptance must exercise two-disc ordinary albums
and compilations with multiple structured credits, distinct disc art/subtitles,
linked artist discography/Back, global-index playback and audible disc-boundary
progression, themes/narrow/long text, missing/broken/denied art, offline/ACL
recovery, rescan/clear/failed probe and restart persistence. Focused tests do
not establish platform acceptance.

Fixture work is additive: optional future --multi-disc generation and guarded
augmentation of the existing disposable fixture, executed by root only. It
creates known Music/M04 and Restricted/M04 targets, appends manifest entries
and refreshes IDs without replacing existing media, auth, database or sessions.

## Implementation and focused verification

The existing model/probe/JSON scanner path carries both fields without a schema
migration. The service derives disc descriptors after filtering and sorting;
all list/artist summaries omit descriptor details. Multiple real artist links
remain individually available. Clicking a row's artist remembers that row
before descending so Back restores the correct song. The web app shell revision
is `kiss-v38`. Both clients keep canonical song indices despite added headings.

Focused tests cover compilation-only fallback, explicit artist priority, unknown
disc order, structured punctuation, caller-safe subtitle/art/song IDs, physical
MP3/FLAC/M4A/Opus tags, scan changes/clearing/failed probes and stable user state,
metadata snapshot round trips, disc labels and the actual native second-disc
row tap's target source. Fixture checks reject unmarked/preexisting targets and
validate repeated FLAC artists plus embedded art after generation.

Actual Chrome/Firefox/API34 acceptance and full batch gates/builds remain pending
with the root orchestrator; runner compilation or focused tests are not platform
acceptance. No runtime was launched by the implementation worker.

## Root execution runbook

Preserve the existing fixture and execute augmentation once, after reviewing
this command (never recreate it):

```sh
python3 player-android/integration_test/support/music_discs.py augment /home/paul/tmp/0k3-fixture
```

This adds eight tracks and seven images under known M04 namespaces, appends the
manifest and checks every preexisting media ID before extending the ID map.
Existing manifest entries, auth, DB and sessions remain intact. Future fresh
fixtures can use `music_fixture.py up ... --multi-disc --release-metadata`.
Disc-local covers compete against ancestor and embedded pictures; the ordinary
album's second disc is red and its ancestor/embedded art is blue. A hidden third
disc must never contribute to viewer subtitles/art/counts. A no-art album
exercises placeholders. `art FIXTURE broken|initial` edits only the known
second-disc cover with a guarded backup; `retag FIXTURE changed|cleared|broken|
initial` edits only its known track. Runners restore both in finally blocks.

From `player-server/test/e2e-web`:

```sh
PLAYER_MUSIC_FIXTURE=/home/paul/tmp/0k3-fixture npx playwright test -c playwright.music.config.ts tests/music-discs.test.ts --workers=1
```

From root, using the final combined APK and already running API34 emulator:

```sh
python3 player-android/test/e2e-live/music_discs_e2e.py /home/paul/tmp/0k3-fixture --apk /home/paul/tmp/0k3-evidence/batch-final/app-x86_64-release.apk --emulator-wav /home/paul/tmp/0k3-evidence/emulator-output.wav --resume-device
```

Native `--scenario core|errors|state` supports bounded continuation. Initial
sign-in uses isolated app data; `--resume-device` preserves data and adb mapping
at runner exit for root-owned cleanup. Core's cold restart uses the actual
saved-server/password unlock flow without clearing data. Errors stops only the
verified owned fixture server and removes adb reverse to close existing native
keepalive, then restarts the same binary/DB and refreshes its PID/start in state.
Root must refresh runtime-ownership metadata after all runner restart phases.

Each browser runs actual disc/compilation UI, all individual artist links and
Back, numbering/order, per-disc artwork pixels, keyboard/pointer/Info/typing,
light/dark/narrow/long subtitle screenshots, direct second-disc playback,
disc-boundary source tones and nonwrapping end. Boundaries use actual ACL,
offline, missing and deliberately broken source images with Retry. State cases
exercise M04 changed/cleared/failed-probe fields, stable media/favorite/progress
and same-DB server restart. Native uses equivalent touch/Back, real QEMU source
captures, visible artwork/placeholder semantics, natural boundary/end,
cold restart and state/error cases. Neither platform treats API-only results
as proof of visible grouping or audible playback.


The compilation fixture deliberately varies track artists: its middle song
credits **M04 Guest**, while its first/final songs preserve the two separate
**M04 A / B** and **M04 東京** credits. Core runners require those divergent
embedded credits and one Various Artists album with all three songs, and follow
Guest's middle-row discography link as well as both final-row credit links.
The service regression also proves that removing compilation flags splits
otherwise identical album titles by their ordinary track-artist fallback.

## Verified acceptance, 2026-10-22

M04 passed on installed Google Chrome 144.0.7559.132, Mozilla Firefox 156.0
and native Android 14/API 34 using the same final server/APK as M05. The
port-18814 fixture/database and one read-only emulator session were reused.
Both ordinary and compilation albums retain canonical numbering and playback
indices. The compilation has genuinely different track artists, remains one
Various Artists album, and links Guest and both separately credited people
to their actual discographies.

Browser acceptance covers real keyboard/pointer/typing/focus, dark desktop
and light narrow layouts, readable long subtitles, distinct disc-local art
over competing ancestor/embedded covers, real audible disc-boundary progression,
queue order and nonwrapping end. Real permission/offline Retry, missing art,
a genuinely invalid thumbnail response and visible fallback, metadata changes/
clearing/failed probe, stable identity/user state and server restart passed.

Native acceptance covers touch/Back and the same numbering, grouping, credits,
artwork and light/dark layouts. Original screenshots were reviewed: blue
disc one and red disc two are distinct and subtitles wrap. Independent emulator
audio analysis confirmed 770→810 Hz for the ordinary album and 910→950 Hz
for the compilation; selecting disc two directly played the correct source.
Both collections reached their last track without wrapping. Missing/broken
art, actual permission denial, server-offline error/Retry, saved-server app
restart, changed/cleared/failed-probe metadata and same-database server restart
also passed, retaining identity, favorite and progress.

Evidence lives outside git under `/home/paul/tmp/0k3-evidence/batch-final`:

| Scope | Passing evidence |
| --- | --- |
| Chrome | M04 state in attempt1, boundary in attempt3, strengthened compilation core in attempt5 |
| Firefox | All five combined journeys in attempt4; strengthened compilation core in attempt6 |
| Native ordinary album | `native-attempt3-m04` ordinary scenarios and independent completion/audio audit |
| Native compilation | `native-attempt4-m04-compilation` playback, credits, themes and terminal position |
| Native missing art/app restart | `native-attempt5-m04-boundaries` first two scenarios |
| Native broken art/ACL/offline Retry | `native-attempt6-m04-errors` exits zero |
| Native metadata/server restart | `native-attempt7-m04-state` exits zero |

Failed initial attempts remain retained. Later scoped reruns corrected runner
assumptions about decoded-image reuse, merged Android semantics and catalog
scroll range; product code and accepted build artifacts stayed frozen. Default
native invocation still covers both albums and every boundary. `--scenario
errors|state` and `--core-album ordinary|compilation|none` permit affected
scenario reruns; `none` retains missing-art/app-restart core follow-ups.

Shared versions remain 0.8.2/+22; the installed x86_64 APK matched SHA-256
`abb079b8ae86bc24eb2410193401322aff24d2aff0fe4d155075a48b6a1defee`.
Combined full suites and normal server/APK builds passed; see the M05
implementation notes for gates and artifact details. Test-only refinements
passed focused Go race/coverage, real fixture generation, Python oracles
and affected actual journeys. No release, version bump, push or deployment ran.
