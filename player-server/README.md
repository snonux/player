player
======

![Player logo](logo.png)

Player is an opinionated, self-hosted web media player designed around the KISS principle: minimal dependencies, no frontend frameworks, keyboard-first navigation. Built with Go, SQLite, and vanilla JavaScript.

Features
--------

### Media Playback

- **Videos** — streaming with seek (range requests), optional remuxing, thumbnail previews
- **Images** — lightbox viewer with zoom, slideshow mode, and pan support
- **Lyrics** — available plain or timed lyrics appear over the audio stage in library, share and detached playback. Use the Lyrics button to hide or show them; timed lines follow playback and seeking. Missing lyrics and lookup failures leave the player uncluttered. Focus the lyrics region to read with arrows, Page Up/Down, Home/End or j/k; playback shortcuts remain available. Lyrics also work with visuals Off and for audiobooks when supplied.
- **Audio visuals** — five sound-reactive views (Spectrum, Waveform, Radial, Mirror, Orbit) in library, share and detached playback. Use the player’s Visuals selector or `v` to cycle through them and Off; the preference is saved. Off shows cover artwork when available. Reduced-motion users start with Off. Visuals start from your play click even when fetching track details or preparing playback takes several seconds. Browsers that block Web Audio keep native audio and artwork. Use **Start visuals** after autoplay; runtime errors show a reason and **Retry visuals** instead of permanently disabling the selector. Selecting a mode or pressing `v` also retries during playback. `.m4b` files and file/folder/set names containing “audiobook” or “audio book” suppress visuals. This is a naming heuristic: ordinary MP3 audiobook chapters need an audiobook folder or set name, and share/detached pages can only inspect the media metadata they receive.
- **Floating player** — a 460px-wide audio window adapts to the viewport and grows vertically when there is room to show all controls below the media. Small windows offer volume, notes, favorites, bookmarks, lyrics and visualizations through **More player controls** (⋯). Press `o` to access them with the keyboard. Controls, menus and dialogs share the library's light/dark theme, focus treatment and spacing.
- **Library URLs** — folders, filters, pages and opened media appear in the address bar. Refresh or paste the URL in another browser to restore that view and the same media, paused at its saved position. Sign-in preserves the destination; normal library permissions still apply. Back and Forward restore previous views. Use a share link when giving access to someone without a library account.
- **Thumbnail actions** — hover shows a single horizontal row of buttons. Keyboard users press `a` for the selected thumbnail's action menu, or use the existing direct shortcuts. `;` or `F2` displays shortcuts for every visible control; menus underline their own letters.
- **Picture layout** — optional justified thumbnail rows fit image aspect ratios without cropping. Press `g` to toggle, or use Picture layout in help (`?`); the preference is saved. Unknown dimensions use square tiles and extreme aspect ratios use bounded tiles with the full image visible. Sets without images keep the regular grid.
- **Preview size** — use the visible Previews controls or `+` / `-` to resize library thumbnails; `0` resets the size. Five sizes are saved on this browser and apply to both regular and picture layouts. In image view these keys retain their image zoom actions.
- **Audio / Audiobooks** — cover art display, progress tracking (resumes where you left off)
- **Casting** — in Chrome (HTTPS or localhost), the player's cast button plays the current audio or video item on a Chromecast or other Google Cast device; play/pause, seeking and track changes then control the cast device, and stopping continues locally where it left off. The cast device fetches the media through a short-lived signed link, so it must reach the server at the page's address. Details: [docs/api.md](docs/api.md)
- **Podcasts** — subscribe to RSS/Atom feeds, download episodes to the server, mark listened/unlistened

### Organization

- **Sets** — media is grouped into sets (one subdirectory of `MEDIA_ROOT` per set)
- **Folder browsing** — navigate subfolders within a set via breadcrumbs
- **Tags** — add and remove tags on any media item; filter by tag
- **Favorites** — mark items as favorites; filter to favorites-only view
- **Notes** — attach personal notes to any media item
- **Position bookmarks** — use Save position or Bookmarks in audio/video playback to save an optional label, jump to a saved time or delete it. The dialog captures the opening position; Use current position updates it. Available in library and detached playback, independently of resume progress. Images use favorites.
- **Search** — full-text search with query syntax: `min:30`, `tag:a,b`, `like:1`, `type:video`, `sort:random`, `minsize:10`, etc.
- **Shuffle** — randomize playback of the current filtered result set

### Sharing & Access

- **Share links** — generate time-limited share links for any media item (no login required for recipients), optionally limited to a number of uses. A use is one viewing — one browser or device opening the link and playing the file, however many requests that takes — so a single-use link plays start to end exactly once. Revoking a link ends running viewings at once. Link previews of messengers and mail scanners do not use up a link: only opening it in a browser (or the app) and letting the page load does. Details and limits: [docs/api.md](docs/api.md#share-viewings-how-max_uses-is-counted)
- **User accounts** — multi-user with login, session cookies, and logout
- **RBAC permissions** — per-set `owner` and `viewer` roles; admins see everything implicitly
- **Admin panel** — create/delete users, manage permissions, trigger library rescans, manage trash

### Integrations

- **Podcast feed manager** — add feeds via URL, automatic background refresh with conditional GET
- **File upload** — upload media directly to a set from the browser
- **Library scanner** — `ffprobe`-backed metadata extraction and automatic thumbnail generation
- **PWA** — installable as a Progressive Web App (`manifest.json` + service worker)
- **Detached player** — pop out the player into its own window for multitasking

### Theming

- Dark and light themes with CSS custom properties
- Theme preference saved to `localStorage`
- Easy to add custom themes via `data-theme` attribute selectors

### Keyboard-First

Full vim-friendly keyboard navigation — disabled automatically when typing in inputs. See [Keyboard Shortcuts](docs/keyboard-shortcuts.md) for the full reference.

### Tech Stack

- **Backend:** Go 1.23, `net/http` stdlib, SQLite (`modernc.org/sqlite`), bcrypt
- **Media processing:** `ffmpeg` / `ffprobe` (in container)
- **Frontend:** Vanilla JS (ES modules), CSS custom properties, no build step
- **Container:** Alpine-based Docker image, runs as non-root (UID 65534)

Documentation
-------------

| Document | Description |
|----------|-------------|
| [Quick Start](docs/quick-start.md) | Running from source, Docker, Kubernetes, mage targets |
| [Configuration](docs/configuration.md) | Environment variables and defaults |
| [API Reference](docs/api.md) | Full REST API endpoint listing |
| [Keyboard Shortcuts](docs/keyboard-shortcuts.md) | All key bindings and search syntax |
| [Theming](docs/theming.md) | Theme system and adding custom themes |
| [Podcast Support](docs/podcasts.md) | Subscribing, episode management, feed checker |
| [Admin Guide](docs/admin.md) | Users, permissions, rescans, trash |

Legacy MPEG video
-----------------

The scanner accepts `.mpg` and `.mpeg` files (case insensitive). These MPEG
program streams use the same server-generated H.264/AAC playback copy in the
web and Android players, including public shares. The first play prepares the
copy; subsequent plays use the cache. Seeking works after preparation.
Original files and downloads retain their bytes and `video/mpeg` MIME type.
Run an admin rescan after upgrading to index files previously skipped.

## Play and queue music collections

Open the Sets sidebar and choose **Albums**, **Artists** or **Playlists** under
Music. A collection's **Play** button replaces the current queue and starts its
first song. **Queue actions** offers **Play next** and **Add to queue**; these
keep the current song and position. An empty queue stays paused until you
choose a song in Now Playing. Undo applies to the whole batch.

Use **Select albums**, **Select artists** or **Select playlists** in a catalog
to choose several collections, then **Play selected** or **Queue actions**.
Collections follow their displayed order, regardless of check order. Albums
keep disc/track order, artist playback includes that person's credited songs in
album order, and playlists retain saved order and repeated songs. Press **e**
or **E** on a selected collection to play it next or append it. Escape cancels
collection selection after closing an open menu or expanded player.

## Recover a music queue

The Now Playing music queue saves its order, selected occurrence and playback
position on this browser for the signed-in account. After reload, open **Now
Playing** (Q) and choose **Resume queue**. Recovery stays paused until you choose
to play. Repeated songs retain their separate places. A library link you open
keeps its own selected item while the saved queue remains available.

The queue belongs to this browser and server/account; it does not synchronize
to other devices. Songs you can no longer access are removed during recovery.
A connection failure retains the saved queue and offers **Retry restore** after
sign-in. **Clear queue** remains cleared after reload; Undo is available during
the current session. Private browsing or clearing browser storage removes local
recovery data.

## Repeat a music queue

Open **Now Playing** (Q) and use **Repeat** or press **Y** to cycle **Off → All →
One**. Off advances through the queue and stops after its last song. All wraps
to the first song. One restarts the current occurrence from zero when it ends.
Manual Next/Previous still choose adjacent songs in One; only All wraps those
controls at the queue ends. The button shows the current mode and stays active
for All/One. Sets browse playback keeps its existing behavior.

Repeat belongs to the same server/account queue on this browser, including an
empty queue. Reload restores it paused with the saved selection and position;
choose **Resume queue** to play. Clear and Undo keep the chosen repeat mode.

## Music identity and system controls

Music playback shows the real title, artist, album and available cover in the
full player and minimized Restore bar. Missing artist uses **Unknown artist**;
untagged songs use the filename stem. Missing or unavailable artwork clears the
previous cover. The Restore bar combines the identity in one truncated line;
full accessible labels retain the complete text.

An active browser OS media session receives the same identity and artwork.
System Previous/Next change tracks; seeking remains separate. Detached playback
owns its own session, and Cast takeover clears the local session. Clearing or
signing out removes obsolete identity. Actual Chrome 144 and Firefox 156 OS
metadata, readable artwork and all four transport controls were verified.

In the observed Chrome 144 environment, returning a paused detached track needs
the visible **Play** button to resume and activate system controls. The player
shows **Playback returned here. Press Play to resume.** A new source that failed
before playing also needs visible **Play** after reconnect. Firefox supported
those OS-only resume/retry paths. These are recorded browser activation limits;
setting metadata alone does not activate a stopped OS session.

See [the M54 verification record](../docs/music-m54-implementation.md) and
[the browser runner](test/e2e-web/README.md#rich-music-os-controls-m54) for accepted
phases, retained failures, commands and limits. Physical Cast receivers and
headsets were not exercised for this change.
