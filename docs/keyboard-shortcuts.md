Keyboard Shortcuts
==================

Global shortcuts are registered in `web/js/keyboard.js`. They are **disabled** while the user is focused on an `INPUT`, `TEXTAREA`, or `contentEditable` element (except `Escape` to blur).

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
| `Esc` | Exit fullscreen, or deselect current item |
| `r` | Toggle shuffle on the current filtered result set |
| `s` | Toggle sets sidebar |
| `S` | Generate a share link for the selected media |
| `/` | Focus the quick search bar (debounced) |
| `n` | Open notes modal for the selected media |
| `i` | Show / hide media info |
| `C` | Minimize player |
| `d` | Detach / reattach player |
| `D` | Download selected media |
| `+` / `−` | Zoom in / out (image viewer) |
| `Shift+S` | Toggle slideshow (images) |
| `L` | My Shares |
| `Backspace` | Go up one folder |
| `?` | Show / hide help |

Search syntax: plain text searches file name. Modifiers: `min:30`, `max:55`, `tag:a,b`, `like:1`, `type:video`, `sort:random`, `minsize:10`, `maxsize:500`.

My Shares modal: `↑` / `↓` or `k` / `j` navigate, `Enter` copies, `Delete` revokes, `Esc` closes.