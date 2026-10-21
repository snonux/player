# M26 persistent playlists

Design checkpoint, baseline 396b806 (0.8.2 / 0.8.2+22).

## Scope and boundary

Add account-owned persistent manual playlists: create/name/rename/delete,
plain-text comments, saved membership, visible song count, duration and cover,
and normal music-queue playback. Existing albums/artists, Sets, podcast,
offline, account and playback behavior remains the integration baseline.

An empty playlist has an obvious **Add music** action. It opens an album
picker over the existing permission-filtered Music catalog; choosing an album
adds its ordered songs. This minimal population path makes saved totals,
artwork and playback useful. It is deliberately narrower than M27 track
editing: no per-song/multi-list add dialog, duplicate-choice workflow,
containing-lists lookup, remove/reorder, queue-to-playlist, sharing,
visibility/admin reassignment, import/export or smart rules. Those remain
later candidates. Repeated album addition is allowed and retains entry IDs.

## Annotated web layout and transitions

Existing Music header becomes `Albums | Artists | Playlists`. Playlists uses
the existing media-card grid, theme tokens, artwork and spacing:

    Music · Playlists                       [Create playlist]
    [← Back to Sets]
    [cover / music placeholder] [name]
                               [N songs · m:ss]

Create opens the existing modal/focus pattern: heading `Create playlist`,
required `Name` (trimmed, 1–200 characters), optional multiline `Comment`
(maximum 2000 characters), `Cancel` and primary `Create playlist`. Save
navigates to the new detail with visible heading/focus. Errors remain in the
form and preserve typed values. Edit uses the same form with `Save changes`.

Detail reuses album-detail heading/rows:

    [← Back to playlists]
    [cover] Name                         [Edit playlist] [Delete playlist]
            Comment (wrap, plain text)
            N songs · m:ss
            [Play playlist] [Add music]
    [song title / artist] [duration] [Song actions]

Empty detail: `This playlist is empty.` followed by primary `Add music`;
Play is disabled. Add music opens a modal with permission-visible album
buttons showing title/artist; select album → save membership → refresh detail
and totals; status announces completion. Delete opens a confirmation naming
the playlist, explains that music files stay available, and offers `Cancel`
and `Delete playlist`; success returns to the catalog with visible focus.

Existing j/k/h/l/arrows select cards/rows, Enter opens/plays, Space/p,
N/P, queue and other shortcuts remain. No new global shortcut. Modal
controls retain native typing/Enter/Tab/Escape; closing restores opener.
Browser history and refresh preserve `music=playlists` / `music=playlist&id=`.
No drag interaction is introduced.

## Annotated native layout and transitions

Existing server navigation adds `Music · Playlists` beside Albums/Artists.
A native Scaffold/AppBar has `Create playlist`; catalog uses ListTile rows
with artwork, wrapping title and totals. Tap opens detail, Back returns to
catalog, then Library. Detail uses album-sized artwork/name/comment/totals,
large labeled `Play playlist` and `Add music` buttons; labeled overflow
contains `Edit playlist` and `Delete playlist`. Native AlertDialog forms use
TextFormFields, multiline Comment and Cancel/Save. A native album selection
sheet/dialog provides Add music. Delete requires explicit confirmation.
Songs use existing Media identities/audio-player sequence and song details.
Touch targets are at least 48dp; artwork/buttons have accessible names.

## State and authorization contract

Loading uses existing status/spinner; catalog empty text says `No playlists
yet. Create a playlist to save music for later.` with the primary action.
Failures show readable inline feedback and Retry. Offline create/edit/add/
delete fails visibly without claiming saved state; the form stays open.
Playlists require the server (no new offline collection storage). Long names
wrap/clamp with full accessible title; comments wrap without HTML rendering.
Dark/light use existing tokens/Material theme; narrow detail wraps actions.

All playlist queries/mutations include authenticated owner ID. Other users,
including admins, cannot read/mutate another user's playlist by ID (404).
The service obtains tracks through existing ACL-filtered MusicAlbums before
saving additions and before returning any rows/count/duration/cover.
Revoked/deleted/non-music tracks are hidden and excluded from aggregates;
existing saved membership remains durable until hard media deletion.
Foreign-key cascades and orphan/user-deletion cleanup include playlist rows.
Playlist IDs and entry IDs are opaque random strings, membership is ordered
and persisted in SQLite; totals and first available cover derive from current
visible media metadata so rescans/ACL changes cannot leak stale values.
Routes use handleBoth for /api and /api/v1, with a small playlist repository
and service rather than SQL in handlers. Document the API in api.md.

## Verification plan

Targeted real SQLite/service/API checks: durable CRUD/comments/membership,
ordered duplicates, correct aggregate/cover, owner isolation, invalid/blank/
overlong input, inaccessible/nonmusic/deleted members, transaction rollback,
media/user cascade and reload. Web/native focused tests cover forms/errors,
empty Add music, navigation/focus and existing playback integration.

Disposable fixture/evidence: /home/paul/tmp/3k3-evidence (fresh run data).
Real Chrome and installed Firefox: create/type/rename/comment/add album,
check totals/artwork and actual queue source/audio; reload and new account
isolation; delete cancel/confirm; keyboard/native typing, error/offline,
narrow/light/dark captures. Native API34: same touch/Back journey, force-stop
restart persistence, artwork/totals and authoritative API saved state plus
media session/source audio. Runtime/build slots are handed off by root;
full gates/build/fresh reviews/commit/done belong to root. No release/version
change or user source-context modifications.

Root approved this design before feature coding on 2026-10-21. Minimal album
addition is explicitly authorized; later membership editing remains separate.

## Implementation checkpoint

Product code now uses `playlists` and `playlist_entries` SQLite tables,
`repository.PlaylistRepo`, `service.PlaylistService` and session-gated
`handleBoth` routes. Counts/duration/artwork are derived from currently
permitted Music catalog entries. User deletion explicitly removes nested
entries even on a legacy connection without cascades. New web module is
included in service-worker cache `kiss-v31`.

Web dialogs guard each opened panel's identity and open state as well as the
current view. Late success/error/finally callbacks from a closed dialog cannot
close or write errors into its successor. Native work checks the account
mutation epoch and API/view generation; forms retain input after save failures.

Targeted verification on frozen checkpoint (2026-10-21):

- `go test ./internal/service ./internal/repository ./internal/api -run
  'TestPlaylist|TestPlaylists|TestMusicAlbumsRealPermissions' -count=1`: PASS.
  Real SQLite restart, ownership (including foreign admins), ordered duplicate
  occurrences, metadata validation, ACL revocation, transaction rollback,
  media/user cleanup and both API prefixes/session requirements are covered.
- `flutter test --no-pub test/screens/music_playlists_screen_test.dart
  --reporter expanded`: 3 PASS (Unicode codepoint validation, retained offline
  form, empty/Add music Retry, delete cancellation and metadata edit).
- Scoped `flutter analyze --no-pub` of changed product files/new screen test:
  no issues. Node navigation URL round-trip and JS syntax checks PASS.
- Logs live outside the repository under `/home/paul/tmp/3k3-evidence/`.

Fresh review identified a duplicate-occurrence playback defect in the existing
native sequence handoff: identity lookup selects the first repeated media ID.
The approved repair preserves an optional requested occurrence index through
the route and maps it into the filtered music sequence. Missing, out-of-range
or media-mismatched indices retain the existing identity fallback. Regression
`[image,A,B,A,B]`, selecting source occurrence 3, failed with actual queue
index 0 instead of 2 before the mapping repair (`duplicate-before-finally.log`,
terminal 1). After repair, the complete audio-player and playlist screen test
files passed 53 tests (`duplicate-after-audio-playlist.log`); scoped analyze
reported no issues (`duplicate-analyze.log`). Tests also cover Next/final
no-wrap and invalid-index fallback. The first failure run's teardown lacked
`runAsync` cleanup and was terminated after retaining the same assertion;
the final failing-before run adds only `finally` cleanup and exits normally.
Actual native later-occurrence playback is now verified by the completed remaining-phase run below.

## Actual platform acceptance

All required M26 journeys now have positive evidence against the disposable
local server, with no product changes after the reviewed occurrence fix.
Root reviewed the original browser, populated native, account and dark detail
screenshots plus authoritative JSON and audio as the checkpoints arrived.

Server: `0.8.2`, checkout/build `396b80690bbdcba93b43c3197965bd7680db43f6-dirty`.
Native APK: `0.8.2`, versionCode `4022` (`pubspec 0.8.2+22`), SHA256
`a5b2090d0b78d12eeabb2214d8274eb9e35e9ac0afda2c7f3e39ad85c0198903`.
Generated fixture, credentials, database, media, screenshots and output audio
remain outside git in `/home/paul/tmp/3k3-fixture` and
`/home/paul/tmp/3k3-evidence`. Runtime commands and process fingerprints are
recorded in `runtime-commands.json`, `runtime-ownership.json` and
`emulator-ownership.json` there.

| Platform | Actual outcome | Evidence |
| --- | --- | --- |
| Installed Google Chrome 144.0.7559.132 | All 3 M26 scenarios pass | `web-settled.log`, `run/chrome/` |
| Installed Mozilla Firefox 156.0 | All 3 M26 scenarios pass | `web-settled.log`, `run/firefox/` |
| Native Android 14/API34 x86_64, AVD Player_FDroid_Test_API34, density 440 | All required phases pass across the retained checkpoint/resume runs below | `native-final.log`, `native-resumed.log`, `native-remaining.log`, `run/android/` |

Browser command (component-local `player-server/test/e2e-web/`):

```sh
PLAYER_MUSIC_FIXTURE=/home/paul/tmp/3k3-fixture PLAYER_E2E_PULSE_SINK=player_3k3 \
  npx playwright test -c playwright.music.config.ts music-playlists --workers=1
```

Final browser run: 6/6 PASS, terminal 0, 41.6s. Each browser exercised actual
create/name/comment, empty Add music, album insertion, real decoded cover,
3 songs / 36 seconds, rename and multiline plain-text comment, a new client context,
other-account UI isolation and owner return, keyboard `l`/Enter/`j`/`k`,
visible focus after delete Cancel, confirmed delete, offline retained form,
late old-dialog success/failure isolation, cached app-shell offline import
and Retry. Repeat album addition saved six distinct entries / 72 seconds and playing
the later duplicate selected queue index 3 then Next index 4. Actual output
was 440 Hz for 1.39 seconds in each browser. Light/dark originals at 1366 px and 390 px
wait for actual computed theme colors and show no horizontal overflow.

Earlier browser attempts remain retained: first 4 failures were floating
player pointer obstruction during a later duplicate click and ambiguous
same-label controls during dialog closing fades; second 2 failures were the
same Delete-label fade ambiguity. Runner corrections use documented `q`
before choosing a new source and scope real main-grid controls; no forced
clicks or product changes. Logs `web-playlists.log`, `web-final.log` and
external `web-{first,second}-attempt-results/` preserve them.

Native commands (component-local `player-android/`):

```sh
python3 -u test/e2e-live/music_playlists_e2e.py /home/paul/tmp/3k3-fixture \
  --apk /home/paul/tmp/3k3-evidence/app-x86_64-duplicate-fix-release.apk \
  --emulator-wav /home/paul/tmp/3k3-evidence/emulator-output.wav
# Resume already-accepted saved playlist from the next unfinished phase:
# append --resume-playlist pl_cda56059b86befefac505ea36447f41f
# and --resume-stage remaining for only duplicate/offline/delete checks.
```

The native creation/Add music phase passed in `native-final.log`: empty
playlist, ordered First Light/Long Road Home/Harbour,3 songs, 36 seconds and actual
decoded blue cover. `native-resumed.log` passed actual rename/comment,
force-stop/relaunch (saved account remains locked until sign-in), persisted
membership/artwork/totals, admin UI empty catalog and owner UI restoration,
light/dark details and original First Light output (440 Hz,2.98 seconds). Device
speaker STREAM_MUSIC volume was verified 15/15 via actual volume keys.

Final `native-remaining.log` exits0 with every remaining assertion passing:
tap the actual later Harbour row in the six-entry playlist, queue occurrence 6,
Previous→5, Next→6, original Harbour output (659.2 Hz,2.48 seconds), natural collection
end stopped at 6 without wrapping, delete Cancel, actual offline save failure
with retained typed comment, owned-server stop/restart on the same SQLite DB
with unchanged authoritative playlist, confirmed deletion and Back to Library.

Three earlier native driver failures are preserved externally. First, empty
Flutter EditText nodes omit their visual labels in UIAutomator, so the runner
now requires the actual two input controls and visible limit labels. Second,
the grid-card helper rejected valid 132 px Material buttons; the local control
helper now uses visible screen bounds. Third, initial AppBar Queue bounds
shift as loaded personal actions appear; the runner waits for loaded transport
controls and two stable fresh Queue snapshots before tapping. Checkpoint
resume retains positive earlier phases; no accepted phases were replayed and
no product/APK changes were needed for these driver corrections. Original
failures live in `native-{first,second,third}-attempt/` and their logs.

Root full gates: final Flutter analyze no issues and 1371 tests PASS; Go
build/vet/full FFmpeg race+coverage suite PASS; all web unit/service-worker
checks PASS. Errcheck ran and reported 256 existing diagnostics, 0 new; it is
not clean. Both fresh reviews completed (maximum 2), with the sole blocking
occurrence finding fixed and confirmed by the final narrow review.

Verification limits: Android proof is the API34 emulator, not physical
hardware or TalkBack operation. Browser narrow controls use actual pointer
and keyboard journeys; real browser touch hardware was not driven. Cast,
sharing, track remove/reorder, import/export and offline collection storage
are unchanged/later scopes. All M26-required platforms are verified.

Root accepted all required platform originals, logs, authoritative state and
source audio. Authorized stop-only cleanup completed: verified owned server
PID 1669694 / start 42322758 and QEMU PID 1665227 / start 42227133 stopped; only the
matching player_3k3 PulseAudio module 536870916 was unloaded. Port 18807 is free,
health is unreachable, adb devices is empty and the module is absent.
`cleanup-stop-only.json` records the fingerprints/checks; adb reverse reports
no device after emulator shutdown. Fixture DB/media/accounts, evidence, APK
and WAV files remain preserved. No release/push/deploy/version changes were
made. Root owns final staging, local commit and task completion.
