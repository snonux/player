# Player and ggaze keyboard alignment

Audit task: `mc3`, 2026-10-14. This is an implementation plan, not a list of
already shipped bindings. The comparison uses the current working trees,
including the pending Player Escape changes in `jc3`.

The apps already share a useful vi-style foundation. They are similar enough
in the grid, but Player's expanded image mode currently blocks several shared
commands. Fix mode routing and add a few compatible controls before considering
any changes to established file or playback commands.

## Sources and current behavior

Player sources: `web/js/keyboard.js`, `web/js/app.js` (`keyboardHandlers`),
`web/js/selection.js`, `web/js/imageViewer.js`, `web/js/playback.js`,
`web/js/views/playback-nav.js`, `web/index.html` and
`docs/keyboard-shortcuts.md`. Code takes precedence over abbreviated help.

ggaze sources in `/home/paul/git/ggaze`: `src/shortcuts.c` (the single table
driving bindings/help/tooltips/menu), `src/window.c`, `src/edit-mode.c`,
`src/tool-ctrl.c`, `README.md` and `docs/ui-and-interactions.md`.

| Context / key | Player today | ggaze today | Decision |
|---|---|---|---|
| Grid `h/j/k/l`, arrows | Horizontal card steps, vertical row steps | Same directions, including folder cells | Keep; Player docs calling horizontal moves page/set switching are misleading |
| `Enter` | Open selected grid media/folder/set; native focused buttons except grid open buttons | Grid opens selected image/folder; edit panel toggles preset; tool applies | Keep mode and focused-control ownership |
| `Escape` | Dialog first, fullscreen exit, then media to thumbnails; audio/video continues minimized | Tool/panel/fullscreen/marks/grid steps; slideshow stop; double Escape in grid can quit | Same escape-hatch intent; keep app-specific playback/edit semantics (`jc3` already implements Player side) |
| `Backspace` | Fullscreen/media escape then parent folder/sets; typing stays native | Parent folder in grid only, bounded by original opened folder | Add ggaze large-view back steps without clearing marks or quitting |
| Image `h/l`, Left/Right | Previous/next image | Previous/next image, except edit-panel letters tune strength | Keep |
| Image `j/k`, Up/Down, `H/L` | Expanded-image branch ignores them | j/k and Up/Down pan vertically; H/L pan horizontally; edit/tool modes override letters | Add Player image pan outside crop/modal ownership |
| Image `+`, `=`, `-`, `0` | Zoom keys work; `0` absent | Zoom; `0` fit/100% toggle (grid resets thumbnail size) | Add Player `0` fitted zoom/pan reset; do not claim a 100% toggle |
| `f`, `i`, `?` | Fullscreen/info/help globally; ignored in expanded images | Fullscreen/info/help in ordinary large view | Enable Player image f/i; `pc3` owns image ? help routing |
| `S` | Image slideshow works; ordinary global S is consumed without action | Slideshow | Keep image slideshow common; describe scope |
| `PageUp/Down` | Fullscreen audio/video seek ±10%; image expanded branch ignores them; no grid action | Grid viewport page; large view previous/next image | Preserve media seek; defer extra grid/image paging until core routing is fixed |
| `g/G`, `Home/End` | No global bindings | First/last image | Useful later additive Player navigation, not a prerequisite |
| `/` | Library quick search; blocked in help today | No library search by design | Player `pc3` adds help-local /; no new ggaze search UI |
| `c`, `x`, Shift+arrows | Display fill-crop toggle/anchor/nudge; expanded-image early return blocks them | Non-destructive crop tool; x reverts edits in panel | Restore Player advertised controls; label display crop explicitly |
| `d/D` | Detach/download | Trash/permanent delete | Retain existing bindings; this is the highest-risk habit conflict |
| `u`, `t`, `s`, `r`, `Space` | Upload/tags/share/shuffle/play-pause | Undo/view toggle/save copy/straighten/hold-original | Keep application-specific actions with explicit help scopes |
| Shift+j/k | No global action | No explicit J/K rows; Shift+Up/Down extends marks in grid, pans in large | Add ggaze J/K symmetry with H/L, preserving crop tool override |

Player's image branch runs before the later fullscreen-image delta helper.
With the current `isLightboxOpen` predicate, expanded images take that early
branch even in fullscreen; the later helper's j/k and page mappings therefore
do not describe the ordinary expanded-image journey. Player audio/video h/l
seeks only while fullscreen (5 seconds, 15 on repeat), otherwise moves grid
selection. ggaze edit-panel arrows deliberately remain global while letters
select/tune presets; this intentional difference must remain visible in help.

## Mode contract

Use modal-editor inspiration as predictable context ownership: typing and
native control activation first, then the visible dialog/tool, then the image
or playback view, then grid navigation. Keep vi directions and useful arrow
aliases. Display the current mode's available commands and return visible
focus to the active view after closing a dialog. A full Vim/Helix emulation,
prefix grammar, command mode or configurable binding system is unnecessary
for this alignment.

Do not make Player `d` a deletion alias or ggaze `D` a download alias. That
would make a learned download habit destructive. New shared keys should be
additive and harmless. Player trash/restore work belongs to existing `oc3`;
any future destructive shortcut proposal needs its own explicit interaction
design, confirmation policy and tests.

## Implementation order and tasks

1. **H — Player `wc3`**, project `player.player-server`: place modifier,
   editable-focus and native-control guards before specialized modal and
   Space routing. Confirmed gaps: shares/info j/k handlers precede typing and
   chord guards; global Space precedes Ctrl/Meta/Alt rejection. Preserve
   unmodified modal navigation, Escape, sidebar Space and selected-card Enter.
2. **M — Player `xc3`**, project `player`, inline dependencies `wc3,jc3`:
   repair expanded-image f/i/c/x/Shift+arrow routing, add j/k and H/L pan and
   0 fitted reset. Preserve crop precedence and audio/video seek. Update help
   and shortcut docs to describe actual scopes. Root task scope permits both
   dependencies to resolve; implementation remains in `player-server/`.
3. **Existing Player `pc3`** owns searchable help, help-local `/` and image
   `?` routing. Its annotation records coordination with `wc3`/`xc3`; no
   duplicate help task was created. `pc3` and `xc3` touch the same router and
   should be scheduled sequentially. Keep `jc3` Escape work intact.
4. **M — ggaze `yc3`**, project `ggaze`: add global J/K aliases of existing
   vertical mark-extension/pan actions, and contextual Backspace from large
   view. Backspace takes one step: slideshow stop, tool cancel, panel close
   keeping edits, fullscreen exit, then grid; only in grid does it climb the
   bounded folder hierarchy. It retains marks/cursor and never quits. Do not
   blindly reuse Escape's action, which clears marks and can quit. Preserve
   crop Shift+j/k resize and tool/panel routing.

Deferred: Player boundary/page navigation and a single data-driven shortcut
registry (ggaze already has one). These are useful follow-ups after the
small behavioral fixes, but no speculative redesign tasks were created.

## Verification and limits

This audit read source, current help/docs, repository instructions, Player's
pending task list (including `jc3`, `pc3`, `oc3`) and ggaze's empty pending task
list. New tasks include self-contained source references, acceptance criteria,
workflow annotations and checked dependencies. No shortcut implementation was
changed and no browser/GTK runtime verification or build was performed for
this documentation-only audit.

Implementation checks: Player `mage webTest` and focused Playwright keyboard
journeys using an isolated local server per `test/e2e-web/README.md`. Cover
expanded/fullscreen images, dialogs and editable fields, crop/slideshow,
native buttons, selection restoration and unchanged audio/video seeking.
For ggaze, use real shortcut dispatch in `tests/test_shortcut.c` and
grid/folder journeys in `tests/test_grid_cancel.c`; run applicable unit and
integration checks in minimal and GEGL configurations. Use isolated Xvfb
exactly as ggaze `AGENTS.md` specifies, one integration lane per display.

## Implemented image controls (`xc3`)

Expanded and fullscreen images now accept `f`, `i`, `c`, `x`, and
Shift+arrows. `j/k` and Up/Down pan vertically, `H/L` pan horizontally, and
`0` resets zoom/pan to fitted view. Pan requires zoom above fit; active display
crop receives position keys first. `h/l`, Left/Right, and `Shift+S` preserve
previous/next and slideshow. Dialog/native/typing ownership and fullscreen
Escape steps remain unchanged. Display fill-crop only changes presentation,
not the file, and differs from ggaze's editing tool. Image `?` and searchable
help remain owned by `pc3`.
