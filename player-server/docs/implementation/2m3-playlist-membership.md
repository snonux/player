Stage: ACCEPTED after focused Chrome, Firefox and native Android verification
on 2026-10-27. Two bounded expert rounds completed; target-state retry blocker
was fixed and R2 was clear. The next full regression gate is after task 20.

# M30 playlist duplicate and membership tools

The implementation-stage contract below preceded runtime acceptance. Prerequisites
nj3 (U01), nl3 (M27) and oj3 (V01) are retained. Shared versions unchanged.

Interaction layout, extending the current M27 dialog:

```
Playlist > Add songs
  Find songs [________]               initial focus
  Repeated songs [Skip … / Add again] default Skip
  > Add to more playlists             collapsed, current playlist implicit
    [ ] Focus  [ ] Road trip          writable, manual targets only
  N selected
  [ ] Harbour · artist · album
      In playlists: Focus, Road trip  readable lists only
  [Cancel] [Add selected songs]        one atomic save for every target
```

Android uses the existing scrollable AlertDialog, native checkbox tiles and an
ExpansionTile for additional targets. Search preserves selection. Both UIs retain
song order from the existing picker and expose duplicate behavior explicitly.
Single-target adding needs no extra target step. No shortcut allocation changes;
web typing/selects retain native keys, existing modal focus restoration and Escape.
Android Cancel/Back dismiss the dialog. Long titles and membership names wrap in
existing theme/text controls, with scrollable dialog content. No new colors.

Playlist actions > Containing playlists is also available on shared/read-only and
source-following playlist detail. It lists containing readable playlists for each
currently visible song, without offering writes to those lists. The add picker
also shows this information alongside each selectable library song.

Loading/error/empty states reuse M27; failed writes retain the song/target choice
for retry. Save disables repeated submission; the server rechecks every target's
owner/admin permission and source binding inside one transaction. Permission,
source or missing target changes roll back the whole operation. Skip checks saved
occurrences and earlier input songs inside that write transaction. Add retains
each occurrence and input order; old add/undo/queue-save behavior is preserved.
Membership omits private foreign lists and inaccessible song identities.

Cheap checks and review outcomes are recorded in task annotations. The
implementation-stage verification plan included full builds, installed
Chrome/Firefox and native journeys, add/skip, permissions, restart and offline
retry; the focused platform results are recorded below.

R1 fixed an in-flight web selection mismatch: target checkboxes and duplicate
policy now freeze together with the submitted song selection, and unlock after
failure. `node web/js/tests/playlist-membership-retry.test.js` executes the
production picker handlers through offline failure, target editing and retry;
it passed. Membership currently composes List/Get and rebuilds visible catalog
snapshots per playlist (and may refresh owned sources twice); profile this with
large libraries during integrated verification before considering optimization.

## Focused installed-platform acceptance, 2026-10-27

Reviewed implementation commit `fda59ad` and integrated web test repair
`463eb32` were already present in the built server and APK. The disposable
server ran on port 18830 with `--no-doubles`. Server binary SHA-256:
`3eb987fd2ca8d5aa8e0130750ff96c912840b580a3c4e13bc82e76576ef1d5a9`.
Universal APK SHA-256:
`5681c380851537873543bac30a26544fb140c1b562d0c49b12c10503c7f11dbf`
(`0.8.2+22`). Neither build is a release. Evidence is outside the repository
at `/home/paul/tmp/codex/player-m30-evidence/`. The emulator and fixture were
stopped after verification.

Installed Chrome 144.0.7559.132 and Firefox 156.0 each passed the focused
Playwright journey 1/1 with one worker, serially and `GOMAXPROCS=2`. Its exact
command is in `player-server/test/e2e-web/README.md` under M30. Each browser
used the real Add songs picker to select First Light and an additional list.
The default Skip policy saved one occurrence in each list and a repeat Skip
left both at one. Add again saved a second distinct occurrence in each list.
Containing playlists showed two readable owned lists and a shared read-only
list for the song, while omitting a private foreign list. Narrow 390px dark
and light picker screenshots showed the checked target and controls without
horizontal overflow. Offline Save retained the selected song and target;
reconnect saved Harbour into both lists. A real request naming one writable
and one read-only shared target returned HTTP 403 with no partial write. Each
browser played the saved First Light occurrence and captured its expected
440 Hz audio. Escape closed the picker. Restarting the server retained each
browser's two lists with ordered media IDs `[1, 1, 2]`
(`server-restart-membership.json`).

One read-only Android 14/API34 emulator (emulator 33.1.24.0) installed that
APK and connected through `adb reverse tcp:18830 tcp:18830`. Two fresh
private lists let the native Add songs dialog choose First Light and the
second target. The server saved `[1]` in both; a second native Skip left
both unchanged; turning Skip off and choosing Add again saved `[1, 1]` with
distinct entry IDs. The dialog displayed long membership names in a
scrollable layout, offered only writable manual targets, and its separate
Containing playlists view listed readable owned/shared memberships for each
occurrence while omitting the private foreign list. Light and dark narrow
captures are `native-multi-target-light.png`,
`native-multi-target-dark.png` and `native-containing-light.png`.

With reverse removed and Wi-Fi/mobile data disabled, native Save showed
“Could not reach the server,” while the Harbour song, second target and
selection remained in the dialog (`native-offline-retained.png`). Reconnect
and the same Save produced `[1, 1, 2]` in both lists. Back canceled a separate
selected Long Road Home draft without mutation. Tapping Play playlist
produced 7.88 seconds of actual 440.0 Hz QEMU WAV output from First Light
(`native-duplicate-playback.wav/.json`). A second server restart followed by
native navigation back into the list still showed three songs. Final API
state preserved distinct occurrence IDs and excluded the private foreign
list (`native-final-state.json`).

Source-following target conflict and bulk transaction rollback/ACL cases
passed the focused Go tests recorded in the task; the installed-client
journeys exercised read-only filtering and denial. The larger-library
membership performance note above remains unprofiled, and the full combined
suite is due after completed task 20 under the user's ten-task cadence.
