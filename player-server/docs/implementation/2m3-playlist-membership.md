Stage: IMPLEMENTED/REVIEWED - AWAITING INTEGRATED VERIFICATION.
Two bounded expert rounds complete; target-state retry blocker fixed, R2 clear.

# M30 playlist duplicate and membership tools

Implementation stage only; acceptance awaits integrated verification. Prerequisites
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

Cheap checks and review outcomes are recorded in task annotations. Still required:
integrated full suites/builds; actual Chrome and Firefox keyboard/pointer journeys,
light/dark/narrow screenshots; native emulator touch/Back and long-content checks;
add/skip across two lists, read-only/source targets, inaccessible memberships,
restart durability and offline/retry saved state. No platform acceptance claimed.

R1 fixed an in-flight web selection mismatch: target checkboxes and duplicate
policy now freeze together with the submitted song selection, and unlock after
failure. `node web/js/tests/playlist-membership-retry.test.js` executes the
production picker handlers through offline failure, target editing and retry;
it passed. Membership currently composes List/Get and rebuilds visible catalog
snapshots per playlist (and may refresh owned sources twice); profile this with
large libraries during integrated verification before considering optimization.
