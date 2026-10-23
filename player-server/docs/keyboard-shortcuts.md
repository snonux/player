Keyboard Shortcuts
==================

Press `;` or `F2` to show an underlined shortcut for every visible control,
then type its letters. `F2` also works from a text field. Escape cancels hints.
Menus and dialogs show their own shortcuts automatically; text fields retain
normal typing. Large panels use two-letter shortcuts. Tab and native range
arrow keys remain available. Browser modifier chords retain their behavior.

Press `a` (or Shift+F10 / the context-menu key) for the selected thumbnail's
actions. Thumbnail buttons otherwise appear only on hover, in one horizontal
row; narrow cards scroll that row sideways. Touch opens the action menu.
Press `o` for player controls. Roomy windows show these controls directly below
the playback area; small windows keep them in the More controls menu.

Global shortcuts are registered in `web/js/keyboard.js`; the shared audio player handles `v` on library, share and detached pages. They are **disabled** while the user is focused on an `INPUT`, `TEXTAREA`, `SELECT`, or `contentEditable` element (except `Escape` to blur).

Audio/video players in the library and detached window have Save position and
Bookmarks buttons. Tab to these controls and press Enter to open the dialog.
Save an optional label for the captured time, or choose Use current position to
update that time. Tab through saved positions to jump or delete; Escape closes
the dialog and returns focus to its opener. Playback and library navigation
shortcuts stay inactive while the dialog is open. Notes and Favorite are also
available in the authenticated player, including images; public shares keep
personal actions private.

Lyrics: timed lines are buttons. Tab to a line and press Enter to seek to its
timestamp without changing play/pause; click or tap does the same. In library
and detached playback, Space/p remain playback controls. Public shares retain
native button Space activation (seek) and their visible playback controls.
Arrows, Page Up/Down, Home/End and j/k scroll lyrics
without changing the library selection. Auto-follow preserves focused lines
and pauses during manual reading; leave the lyrics region or select a line to
resume it. Plain lyrics remain text.

Escape closes an open dialog first. Otherwise it exits fullscreen; another
press returns to the thumbnail grid, keeping the selected card focused.
Audio/video keeps playing in the minimized player; closing an image view
stops its slideshow. Backspace follows the same player steps outside text
fields and dialogs, then resumes folder/set navigation once the grid is shown.

| Key | Action |
|-----|--------|
| `↑` / `↓` | Navigate media list (up / down) |
| `k` / `j` | Navigate media list (up / down) |
| `←` / `→` | Seek or switch pages |
| `h` / `l` | Seek or switch pages |
| `Enter` | Open selected media (navigate to detail) |
| `Space` / `p` | Play / pause the loaded item; play the selection when nothing is loaded |
| `N` / `P` | Next / previous track |
| `f` | Toggle fullscreen on the player wrapper |
| `v` | Cycle five audio visualizations and Off (also available in the player’s Visuals selector) |
| `Esc` | Close dialog, exit fullscreen, then minimize audio/video or close image view to return to thumbnails |
| `r` | Enable shuffle, or reshuffle the current filtered result set |
| `s` | Generate a share link for the selected media |
| `/` | Focus library quick search, or filter shortcuts while help is open |
| `g` | Toggle picture layout (also available in help) |
| `+` / `=` / `-` | Larger / smaller library previews (saved); zoom the image when image view is open |
| `0` | Reset library preview size; reset image zoom when image view is open |
| `n` | Open notes modal for the selected media |
| `i` | Show / hide media info |
| `C` | Minimize player |
| `d` | Detach / reattach player |
| `D` | Download selected media |
| `+` / `−` | Zoom in / out (image viewer) |
| `Shift+S` | Toggle slideshow (images) |
| `L` | My Shares (outside expanded images) |
| `Backspace` | Exit fullscreen or media view; otherwise go up one folder (or back to sets) |
| `?` | Show / hide help, including expanded and fullscreen images |

Search syntax: plain text searches file name. Modifiers: `min:30`, `max:55`, `tag:a,b`, `like:1`, `type:video`, `sort:random`, `minsize:10`, `maxsize:500`.

My Shares modal: `↑` / `↓` or `k` / `j` navigate, `Enter` copies, `Delete` revokes, `Esc` closes.

Expanded images (including fullscreen) own `h/l` and Left/Right for previous/next,
`j/k` and Up/Down for vertical pan, and `H/L` for horizontal pan. Pan works
only above fitted zoom. `+`, `=`, and `-` zoom; `0` restores fitted zoom and
centers the image, rather than toggling native 100% size. `Shift+S` toggles
slideshow. `f` toggles fullscreen and `i` opens media info.

In fullscreen, `c` toggles display fill-crop, `x` cycles its anchor, and
Shift+arrows nudge its position. While display crop is active, `j/k`, Up/Down,
and `H/L` also position the crop instead of panning the zoomed image.
Shift+arrows pan when crop is inactive. This is a display setting; it does
not edit the image file or provide ggaze's crop editing tool.

Typing, browser modifier chords, native Enter on buttons/links, and dialog
controls retain ownership. Image keys never move the hidden grid or seek
background audio. Fullscreen audio/video retains `h/l` and Left/Right seek.
Help filters shortcut rows by key or action, ignoring case. An empty filter shows
all shortcuts. Press `/` from a help button to focus and select the filter; while
typing, `/` and `?` stay text. Escape closes help and returns focus to its opener.

## Now Playing queue

`Q` opens or closes the album queue. Within the queue, `j`/`k` or arrows select,
`Enter` plays, `K`/`J` move up/down, and `Delete` removes the selected entry.
`ContextMenu` or `Shift+F10` opens its actions. Buttons provide the same actions.
Clear and Remove offer Undo; restoring contents never starts stopped audio.
Escape returns focus to the opener. Sets playback keeps its browse sequence;
playing a retained queue entry explicitly resumes the album queue.

`e` inserts checked collections in catalog order, the selected collection,
checked album songs, or the selected audio file with Play next;
`E` appends with Add to queue. Repeated Play next actions keep FIFO order.
Song actions (`a` / ⋮) also offer explicit Play now, which inserts and starts
the songs while preserving the remaining queue. Existing album Play/Enter/tap
still replaces the queue with the album. Each duplicate is a separate entry.

`Z` focuses Sleep duration in player options (outside typing and dialogs).
