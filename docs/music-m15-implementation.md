# M15: Play and enqueue whole collections

Task `kl3`, based on HEAD `9fc425c`, shared versions 0.8.2 / 0.8.2+22.
Root approved the design before product coding. This extends M03/M16
catalogs, M26/M27 playlists and M12–M14 queue/recovery ownership.

The feature is implemented and accepted in installed Chrome, installed Firefox
and the native Android API 34 emulator. Final-APK detail, selection and real
offline/permission/retry checks passed across retained attempts; earlier actual
recovery/account checks remain accepted. Root authorized stop-only cleanup:
the owned app, server, emulator, reverse mapping and audio sink are stopped.
Fixture data and all evidence remain preserved. Root owns commit/task completion.

## Annotated screens

Desktop/narrow web collection detail retains existing artwork, title, credits,
totals, rows and playlist editing. Immediately below metadata:

```
[▶ Play album / Play artist / Play playlist] [Queue actions ▾]
                                            Play next
                                            Add to queue
[Select songs] / existing selected song actions     (album only, unchanged)
```

Play replaces the current queue and starts its first song, consistent with
existing collection Play. Enqueue never starts another song while an existing
queue is active. Next/append into an empty queue retains the existing paused,
selectable queue behavior. Collection replacement uses a minimal replace flag
on the existing insertion transaction; song Play now keeps its remainder.
Queue actions are secondary, accessible popovers with native
buttons and focus returned to the opener. Existing queue Undo applies to the
whole inserted batch. Empty collections disable playback actions.

Album, artist and playlist list screens gain this toolbar:

```
[Select albums / artists / playlists]
    → [N selected] [▶ Play selected] [Queue actions ▾] [Cancel selection]
[checkbox] [Artwork / name: explicit Open button]
```

Checkboxes appear only in selection mode. Catalog cards become role=group with
an explicit Open button (no controls nested inside role=button). Clicking the
card still opens normally outside selection mode; in selection mode it toggles
that collection. Checkbox clicks never navigate or play. The selected card/focus
is retained after enqueue. Leaving selection mode clears checks; Back/Escape
first cancels selection, then follows the existing parent hierarchy. Opening a
detail clears catalog selection. Artist detail's album cards support album
selection too. No per-card action clutter is introduced.

Android uses the same hierarchy with native FilledButton Play and a labeled
PopupMenuButton Queue actions, 48 dp minimum controls, wrapping toolbar, native
Checkbox and semantic names. AppBar/toolbar Select albums/artists/playlists
reveals checks; Back cancels selection before leaving the screen. Native song
selection and playlist per-occurrence editing retain their current behavior.
Play collections uses the existing insertion coordinator, avoiding a new player
route or duplicate source owner. Playback remains available in Now Playing.

Typography, card artwork/spacing and light/dark colors use existing CSS theme
variables and Flutter Theme color/text schemes. Long names wrap or ellipsize
with full accessible labels; narrow toolbars wrap and never overlap cards.

## Ordering and batch contract

Selection order is **visible catalog order**, independent of click order.
Albums resolve via existing album detail, using server disc/track, then
filename/path/id order. Playlists resolve via saved occurrence order, including
repeated songs; no deduplication is applied to playlist occurrences or across
selected collections. Thus selecting playlist A=[x,y,x] then B=[y,z] resolves
[x,y,x,y,z] when A precedes B on screen, regardless of check order.

An artist resolves the existing visible artist discography in its server order,
then each album in album song order, including only songs whose song_credits
contain that artist_key (any recorded role, consistent with the detail's full
credit-role scope). Deduplicate media IDs within one artist expansion, never
across selected artists/albums/playlists. This avoids enqueueing unrelated songs
from compilation releases merely because one song credits the artist. Album
artist credits already belong to the credited songs under the catalog contract.

Resolve the complete batch before one queue mutation. A failed/unavailable
collection fails the whole operation; leave the queue unchanged and preserve
selection. No partial insertion. Empty results show “No songs available.”
Play next inserts one contiguous FIFO block after prior Play next entries;
Add to queue appends one contiguous block. Existing queue occurrence IDs,
start-at-zero rules, no-wrap completion, account scoping, resume and Undo are
retained. No persistent playlist membership changes occur through these actions.

## State transitions and ownership

Idle → resolving (“Loading songs…” status; disable duplicate submission) →
ready → one existing Play now / Play next / Add to queue transaction → success
confirmation + existing whole-batch Undo. Resolve failures show an inline or
snackbar connection/unavailable reason; the original selection remains for
retry. Catalog loading/empty/error/Retry remain unchanged. Offline resolution
fails visibly and preserves the existing queue; no offline collection cache is
introduced. Access filtering remains server authoritative; a forbidden/missing
collection stops the batch without leaking another account's metadata.

Capture view generation, account/credential epoch and playback intent/source
ownership before asynchronous resolution. View navigation, sign-out/server
switch, or a newer source intent invalidates the pending batch. Late results
cannot replace newer playback or mutate the new account queue. Reuse existing
queue busy guards and mutation/claim/load boundary. Disable controls only for
the current operation; detached callbacks never set state on disposed screens.

Web j/k/h/l/arrows retain selected-card navigation; Enter opens the selected
collection. Native input handling and existing Space/p, /, r, Q remain intact.
Existing e/E are extended to selected/checked collections (Play next/append),
without allocating new letters. Visible Select and toolbar controls provide
keyboard hints/Tab and pointer/touch alternatives. Escape cancels collection
selection only after existing open-menu/player/modal priorities are satisfied.
Update shortcut help to describe collection targets. Search/select controls
keep their native typing and arrows; focus is restored after menu close.

## Verification plan

Targeted resolver/controller tests: reverse check order produces catalog order;
artist credits filter compilation songs; album ordering is preserved; duplicate
playlist occurrences and overlap across batches remain; empty, failed second
collection, account/view/source supersession never mutate the queue; FIFO next
and append preserve the current occurrence. Native widget tests cover selection,
Back cancellation, loading/failed retry and detail Play/enqueue controls.

Actual installed Chrome and Firefox: album/artist/playlist detail Play + next +
append; collection selection Play + next + append in catalog order; assert
visible queue occurrences plus actual playback source/output, Undo, same-account
reload recovery, permission and offline failures, late request/source ownership,
keyboard navigation/Enter/e/E/Escape/menu focus/search typing, pointer checks,
light/dark + narrow screenshots. Native API 34: matching touch journeys, actual
media session/source/output, multi-selection/Back, account and offline boundary,
queue recovery after force-stop/unlock, light/dark screenshots. Use existing
component runners, isolated fixture/accounts/database, evidence outside git.
Root coordinates all heavy gates/builds, review and final platform acceptance.

## Implementation and current verification

The shared resolvers expand whole batches before one existing queue mutation.
Catalog order is independent of check order; artist credits filter compilation
songs, playlist occurrences and cross-collection overlap are preserved. The
existing insertion transaction gained an optional collection replacement mode,
with source labels and one-step Undo. Song Play now keeps its prior insertion
semantics. Web controls use independent Open buttons inside card groups. Native
controls use touch selection, Back cancellation and a compact native queue menu.

The final native menu has an explicit semantic boundary. Its actual target
`[457,671,802,803]` at 2.75 pixels per dp is 48 dp high, disjoint from adjacent
Add songs `[444,825,811,957]`; semantic-center touch opens the proper menu. Root
reviewed those original screenshots and bounds before the final core sequence.

| Verification | Current result |
| --- | --- |
| Installed Chrome 144.0.7559.132 | All three cases accepted across retained attempts: details, selections/reload/account, boundaries |
| Installed Firefox 156.0 | All three cases accepted across retained attempts: details, selections/reload/account, boundaries |
| Native Android API 34, x86_64, density 440 | Final APK geometry, all three detail and all three selection contexts, complete offline/permission retry accepted across retained attempts; earlier recovery/account journey accepted |
| Full Flutter after the native repair | Analysis: 0 issues; all 1392 tests passed in 240.8 seconds |
| Go and web | Build/vet, required-FFmpeg race/coverage (220.28 seconds), all Node/SW checks (4.88 seconds) passed |
| errcheck | Unchanged 256-diagnostic baseline, 0 new diagnostics; not a clean result |

The final APK is version 0.8.2 / code 4022, SHA256
`2e437e65be36fb27af7a68f3dd2f877d645c3af970a294d30734ee7d546e3689`.
Root independently verified its version/hash; the normal release build passed
in 58.27 seconds. The server is version 0.8.2, build `9fc425c-dirty`. Versions,
Gradle settings, dependency specification and lockfile remain unchanged.

## Accepted browser journeys

Each browser exercised album, artist and playlist detail Play, Play next,
append and whole-batch Undo. Catalog journeys selected two collections in
reverse check order, verified visible catalog order and duplicate occurrences,
then exercised all three actions. Original decoded PCM matched the fixture
source for every detail and selection kind. Queue occurrence IDs/index survive
same-account reload and an actual admin/user roundtrip, paused until explicit
Resume. Keyboard navigation, Enter, e/E, Escape, menu focus, native typing,
light/dark and 390 px narrow layouts were exercised. Root reviewed original
narrow screenshots and independently checked the source tones.

Both browsers also passed actual offline and permission failures with visible
shortcut loading/error feedback and an unchanged queue. A one-shot delay of a
real fetch proved single submission and rejected an old collection result after
a newer source started. The delay changes timing only; metadata/playback remain
real server responses.

Browser attempt 1 ended with 3 passes / 3 driver failures in 1.8 minutes. Both
boundary cases and Firefox selection passed. The detail drivers later targeted
a hidden expanded-player pause button after minimize; Chrome's selection helper
also raced the real logout redirect. The corrected driver uses actual global p
with visible non-input focus and awaits the login page. Only unfinished Chrome
detail/selection (2 passes, 46.8 seconds) and Firefox detail (1 pass, 27.4 seconds)
were rerun. Original failures and accepted earlier artifacts remain preserved.

## Historical native attempts and repairs

The first APK, SHA256
`f7ec9047bba1281853b15d248ab99f2fb784c1546fb0b59640274e2932288ff0`,
proved complete album/artist detail actions, original source PCM, exact paused
position/index retention, and a real force-stop/unlock/admin-user recovery
roundtrip. Its playlist source/base queue also worked. Its later menu touch
exposed a product accessibility defect: the only exact clickable Queue actions
node spanned `[44,671,1036,1111]` (160 dp high), so its center activated adjacent
Add songs. Stable bounds and original before/tap screenshots reproduced it.

Root approved a local Semantics container repair. The actual playlist-screen
widget regression failed before because the semantic rectangle overlapped Add
songs. It passes after: bounds equal the compact popup, exclude the adjacent
action, semantic-center touch opens Play next/Add to queue, and Add songs still
opens its own dialog. Focused screen/widget checks passed; the root then reran
full Flutter analysis and all 1392 tests and built the distinct final APK above.
The original APK, versions metadata, failures and partial proofs remain
historical evidence; they are not mislabeled as final-APK acceptance.

The initial selection driver also exhausted its short upward-scroll search
when returning from Synth Compilation to Night Transit. It now searches the
full native catalog and records stable clickable feature touch bounds. The
final core attempt reached terminal 1 after all three detail contexts, album
selection and complete real offline/permission retry passed. Artist selection
stopped because the fixed reverse swipe began at y=700 above the actual artist
scroll viewport `[0,825][1080,2154]`; playlist selection was not reached. Both
gesture endpoints now derive from the actual scrollable XML viewport, with
recorded bounds and stable clickable-node checks. Only artist and playlist
selection rerun separately with `--resume-device --scenario selection` and
`--kind artist` or `--kind playlist`. Offline proof cuts only the fingerprint-verified owned
server plus adb reverse, then restarts the same binary/database/media and
updates fixture/root ownership. No production data is involved.

A separate first-review issue was fixed before the original full gates: an old
native Play artist resolution could revive after pushing an album and returning
with Back. A monotonic route-departure epoch now permanently invalidates it.
The exact authenticated push/Back/deferred-result regression failed before and
passes after with unchanged queue and zero source loads. The second/final fresh
review was clear. The runtime semantic defect received root self-review and a
meaningful regression; no third fresh review was requested.

The first release build also failed in ignored generated plugin registration
because `--no-pub` retained an integration-test plugin. Normal Flutter tooling
regeneration and a normal release build fixed the generated metadata; no product
or dependency change was made. That failed build remains preserved externally.

## Commands, artifacts and final acceptance

All media, databases, APKs, WAVs, screenshots and logs are outside git:
`/home/paul/tmp/kl3-fixture` and `/home/paul/tmp/kl3-evidence`.
Exact runtime commands and terminal mapping are in `runtime-command-plan.txt`;
PID/start/executable/module ownership is in `runtime-ownership.json`. Full gate
commands/results are in `full-gates-results.json` and
`post-semantics-full-gates-results.json`. Per-platform versions, append-only
JSONL records and original screenshots/WAVs live under `chrome/`, `firefox/`
and `android/`. Initial Android versions are separately preserved as
`android/versions-attempt1-initial-apk.json`.

Component-local gate commands were `flutter analyze --no-pub`,
`flutter test --no-pub --concurrency=1 --reporter expanded`, `go build ./...`,
`go vet ./...`, `PLAYER_REQUIRE_FFMPEG=1 go test -race -cover -count=1 ./...`,
`mage webTest` and `errcheck ./...`. Normal final APK builds use
`flutter build apk --release --target-platform android-x64 --split-per-abi`.

The final native core command, from the monorepo root, was:

```sh
python3 player-android/test/e2e-live/music_collections_e2e.py \
  /home/paul/tmp/kl3-fixture \
  --apk /home/paul/tmp/kl3-evidence/app-x86_64-release-final.apk \
  --emulator-wav /home/paul/tmp/kl3-evidence/emulator-output.wav \
  --ownership-path /home/paul/tmp/kl3-evidence/runtime-ownership.json \
  --scenario core
```

The core command reached terminal 1: all three detail contexts, album
selection and complete offline/permission retry passed, while artist selection
hit the documented driver scroll failure before playlist selection. Root
accepted those completed phases. Only the two unfinished selection contexts
were rerun on the same final APK:

```sh
python3 player-android/test/e2e-live/music_collections_e2e.py \
  /home/paul/tmp/kl3-fixture \
  --emulator-wav /home/paul/tmp/kl3-evidence/emulator-output.wav \
  --ownership-path /home/paul/tmp/kl3-evidence/runtime-ownership.json \
  --resume-device --scenario selection --kind artist
# Repeat the bounded command with --kind playlist.
```

Both bounded runs reached terminal 0. Root independently accepted their queue
originals, occurrence/order/current-position evidence, source WAVs and Back
checks. Artist selection resolved `[7,6,4,38]`, preserving occurrence 49/index 0/
7.842 seconds through next/append. Playlist selection resolved `[4,7,4,6]`,
preserving duplicate occurrences and occurrence 61/index 0/7.434 seconds.
The logs are `native-final-core.log`, `native-final-artist-selection.log` and
`native-final-playlist-selection.log`. Earlier native recovery/account proof
and browser attempt mapping are described above; failed attempts remain intact.

After accepting all required actual platform cases, root explicitly authorized
stop-only cleanup. `stop-only-results.json` records verified server
1773814/start 43233598 and QEMU 1746279/start 43016649 identity, owned app
force-stop, removal of only reverse TCP 18810, termination of only those owned
processes, and unloading module 536870916 after matching exact `player_kl3`
arguments. Both processes are gone, port 18810 has no listener, the emulator is
gone and the owned module is absent. The shared Pulse daemon was preserved.
Root independently verified the stopped processes, free port, absent
device/module and retained fixture/evidence.
The isolated database/media, accounts, APKs and all evidence remain intact.

This task does not release, push, deploy or bump versions. Physical Android
hardware and TalkBack are not claimed; the required actual native target is
the API 34 emulator. Browser pointer/keyboard paths and native touch/Back were
exercised; no Firefox touch emulation is claimed.
