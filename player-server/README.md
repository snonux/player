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
- **Audio visuals** — five sound-reactive views (Spectrum, Waveform, Radial, Mirror, Orbit) in library, share and detached playback. Use the player’s Visuals selector or `v` to cycle through them and Off; the preference is saved. Off shows cover artwork when available. Reduced-motion users start with Off. Browsers that block Web Audio keep native audio and artwork; press `v` or choose a mode during playback to start visuals after autoplay. `.m4b` files and file/folder/set names containing “audiobook” or “audio book” suppress visuals. This is a naming heuristic: ordinary MP3 audiobook chapters need an audiobook folder or set name, and share/detached pages can only inspect the media metadata they receive.
- **Picture layout** — optional justified thumbnail rows fit image aspect ratios without cropping. Press `g` to toggle, or use Picture layout in help (`?`); the preference is saved. Unknown dimensions use square tiles and extreme aspect ratios use bounded tiles with the full image visible. Sets without images keep the regular grid.
- **Audio / Audiobooks** — cover art display, progress tracking (resumes where you left off)
- **Podcasts** — subscribe to RSS/Atom feeds, download episodes to the server, mark listened/unlistened

### Organization

- **Sets** — media is grouped into sets (one subdirectory of `MEDIA_ROOT` per set)
- **Folder browsing** — navigate subfolders within a set via breadcrumbs
- **Tags** — add and remove tags on any media item; filter by tag
- **Favorites** — mark items as favorites; filter to favorites-only view
- **Notes** — attach personal notes to any media item
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
