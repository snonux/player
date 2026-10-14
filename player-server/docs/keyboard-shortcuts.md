Keyboard Shortcuts
==================

Global shortcuts are registered in `web/js/keyboard.js`. They are **disabled** while the user is focused on an `INPUT`, `TEXTAREA`, or `contentEditable` element (except `Escape` to blur).

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
| `L` | My Shares |
| `Backspace` | Exit fullscreen or media view; otherwise go up one folder (or back to sets) |
| `?` | Show / hide help |

Search syntax: plain text searches file name. Modifiers: `min:30`, `max:55`, `tag:a,b`, `like:1`, `type:video`, `sort:random`, `minsize:10`, `maxsize:500`.

My Shares modal: `↑` / `↓` or `k` / `j` navigate, `Enter` copies, `Delete` revokes, `Esc` closes.
