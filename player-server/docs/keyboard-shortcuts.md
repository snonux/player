Keyboard Shortcuts
==================

Global shortcuts are registered in `web/js/keyboard.js`. They are **disabled** while the user is focused on an `INPUT`, `TEXTAREA`, `SELECT`, or `contentEditable` element (except `Escape` to blur).

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
| `Space` / `p` | Play / pause / switch to selected item |
| `N` / `P` | Next / previous track |
| `f` | Toggle fullscreen on the player wrapper |
| `Esc` | Close dialog, exit fullscreen, then minimize audio/video or close image view to return to thumbnails |
| `r` | Enable shuffle, or reshuffle the current filtered result set |
| `s` | Generate a share link for the selected media |
| `/` | Focus the quick search bar (debounced) |
| `n` | Open notes modal for the selected media |
| `i` | Show / hide media info |
| `C` | Minimize player |
| `d` | Detach / reattach player |
| `D` | Download selected media |
| `+` / `−` | Zoom in / out (image viewer) |
| `Shift+S` | Toggle slideshow (images) |
| `L` | My Shares (outside expanded images) |
| `Backspace` | Exit fullscreen or media view; otherwise go up one folder (or back to sets) |
| `?` | Show / hide help |

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
