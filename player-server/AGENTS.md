# Player — Agent Documentation

This file is written for coding agents working on the `player` project.

---

## Architecture Overview

The project is a **self-hosted media player** designed for simplicity (KISS): minimal dependencies, no frontend frameworks, interface-driven Go code for easy testing.

### Backend

- **Language / Runtime:** Go 1.23
- **HTTP Server:** `net/http` stdlib only; `http.ServeMux` with pattern matching
- **Database:** SQLite via `modernc.org/sqlite`
- **Media Processing:** `ffmpeg` / `ffprobe` installed in runtime container
- **Password Hashing:** `golang.org/x/crypto/bcrypt`

**Layered architecture:**

| Layer | Package | Role |
|-------|---------|------|
| Entrypoint | `cmd/player` | Flags, config, dependency wiring, server start |

| `mage build` | Compile the binary (`go build -o player ./cmd/player`) |
| `mage test` | Run `go test -race ./...` and the web JS unit tests (`mage webTest`) |
| `mage install` | Build and copy `player` to `$GOPATH/bin` (or `~/go/bin`) |
| `mage clean` | Remove the `player` binary |
| `mage docker-build` | Build container image as `player:latest` |
| `mage docker-push` | Push `player:latest` to registry |

---

## Kubernetes Deployment

The `k8s/` directory contains:

| File | Resource |
|------|----------|
| `k8s/deployment.yaml` | `Deployment` (1 replica, non-root `65534:65534`, probes) |
| `k8s/service.yaml` | `ClusterIP` Service on port 8080 |
| `k8s/pvc-db.yaml` | `PersistentVolumeClaim` (`ReadWriteOnce`, 1Gi) for `/data` |
| `k8s/pvc-media.yaml` | `PersistentVolumeClaim` (`ReadWriteMany`, 10Gi) for `/media` |
| `k8s/secret.yaml` | `Secret` (optional) for environment overrides (e.g., `ADMIN_PASSWORD`) |

Deploy everything:

```bash
kubectl apply -f k8s/
```

The `Deployment` overrides two critical settings for K8s:
- `DB_PATH=/data/media.db`
- `MEDIA_ROOT=/media`

Probes:
- **Liveness:** `GET /healthz` (no DB dependency)
- **Readiness:** `GET /readyz` (DB ping)

Security:
- `runAsNonRoot: true`
- `runAsUser: 65534` / `runAsGroup: 65534`
- `allowPrivilegeEscalation: false`
- `readOnlyRootFilesystem: true`

---

## Theming Guide

All colors live in `web/css/theme.css` as CSS Custom Properties on `:root`.

### Current Implementation

`themes.js` swaps the active theme by setting `document.documentElement.setAttribute('data-theme', ...)` and saves the preference to `localStorage`. Override blocks in `theme.css` handle the light variant:

```css
/* Default (dark) — defined on :root */
:root {
  --bg-body: #0f1117;
  --text-primary: #e6e8ef;
  --accent: #5e9eff;
  ...
}

/* Light theme overrides */
[data-theme="light"] {
  --bg-body: #f4f5f8;
  --text-primary: #12131a;
  --accent: #2b6cb0;
  ...
}
```

### Adding a New Theme

Option A — inline override (recommended for small additions):

1. Open `web/css/theme.css`.
2. Append a new attribute selector after the light block, e.g.:

```css
[data-theme="solarized"] {
  --bg-body: #002b36;
  --text-primary: #839496;
  --accent: #268bd2;
  ...
}
```

3. Wire the toggle in `web/js/themes.js` (or expose a selector UI in `index.html`) to call `apply('solarized')`.

Option B — separate file (if you prefer a stylesheet swap):

1. Create `web/css/themes/<name>.css` containing `:root { ... }` overrides.
2. Dynamically create or swap a `<link rel="stylesheet">` in `themes.js` instead of using `data-theme`.

**Rules:**
- No color literals in component styles — everything must go through `var(--*)`.
- Do not add inline styles in HTML or JS.

---

## Keyboard Shortcuts

The web UI is keyboard-first and favors vi-style navigation. Keep shortcuts and
visible focus usable after every UI action, including modal close and button
clicks. Preserve native typing and control activation; mouse and touch remain
available as supplementary paths. Cover real keyboard journeys in browser tests.

Global shortcuts are registered in `web/js/keyboard.js`. They are **disabled** while the user is focused on an `INPUT`, `TEXTAREA`, `SELECT`, or `contentEditable` element (except `Escape` to blur). A focused button or link keeps native `Enter` activation but no longer blocks letter shortcuts. `Space` activates buttons natively only inside dialogs; elsewhere it stays play/pause. On the set/folder open buttons `Enter` opens the grid selection, because `j/k/h/l` move the selection without moving DOM focus. While a dialog (`.modal-overlay.open`) is open, only its own keys, `Escape`, and `?` are handled; `Escape` closes it, keeps the selection, and returns focus to the selected card.

| Key | Action |
|-----|--------|
| `↑` / `↓` | Navigate media list (up / down) |
| `k` / `j` | Navigate media list (up / down) |
| `←` / `→` | Switch sets / pages |
| `h` / `l` | Switch sets / pages |
| `Enter` | Open selected media (navigate to detail) |
| `Space` / `p` | Play / pause / switch to selected item |
| `f` | Toggle fullscreen on the player wrapper |
| `Esc` | Exit fullscreen, or deselect current item |
| `r` | Enable shuffle, or reshuffle if shuffle is already active |
| `s` | Generate a share link for the selected media |
| `/` | Focus the quick search bar (debounced) |
| `n` | Open notes modal for the selected media |
| `t` | Open tags for the selected media |
| `F` | Toggle favorite for the selected media |
| `A` | Open the admin panel (admins only) |
| `u` | Upload to the current set |

---

## Admin Tasks

Admin endpoints are gated by `RequireAdmin` middleware (checks `users.is_admin`). The admin panel is opened via the hidden "Admin" button in the SPA header (shown only when the current user is an admin).

### Creating Users

1. Open the admin panel.
2. Enter username, password, and check "Is admin" if desired.
3. Submit — the frontend calls `POST /api/admin/users`.
4. Admins cannot delete themselves via `DELETE /api/admin/users/:id`.

### Managing Set Permissions

- `GET /api/admin/permissions` — list permissions matrix
- `POST /api/admin/permissions` — grant access to a set (`body: { set_id, user_id, role: "owner" | "viewer" }`)
- `DELETE /api/admin/permissions` — revoke access (`body: { set_id, user_id }`)

Roles:
- `owner` — can upload to the set, soft-delete / restore media, regenerate thumbnails
- `viewer` — can browse and play media in the set

Admins implicitly see all sets without explicit permission rows.

### Rescanning the Library

Click **Rescan** in the admin panel, or call:

```bash
curl -X POST -b session=<cookie> http://<host>/api/admin/rescan
```

This triggers `FSScanner.Scan()`, which:
1. Scans immediate subdirectories of `MEDIA_ROOT` as **sets**
2. Recursively walks each set for supported media files
3. Probes new files with `ffprobe`
4. Generates thumbnails for video files
5. Inserts new records into the `media` table

---

## Configuration via Environment Variables

`internal/config.go` loads all settings from the environment.

| Variable | Default | Validation | Description |
|----------|---------|------------|-------------|
| `PORT` | `8080` | 0–65535 | HTTP listen port (0 = ephemeral, used in tests) |
| `MEDIA_ROOT` | `./media` | — | Root path for media set directories |
| `DB_PATH` | `data.db` | — | SQLite database file path |
| `MAX_UPLOAD_SIZE_MB` | `100` | ≥ 1 | Max upload size per file (MB) |
| `SESSION_TIMEOUT_HOURS` | `24` | ≥ 1 | Cookie / session expiry |
| `GC_INTERVAL_MINUTES` | `30` | ≥ 1 | Garbage collector tick interval |
| `SHARE_DEFAULT_EXPIRY_DAYS` | `7` | ≥ 1 | Default share link lifetime |
| `MEDIA_PAGE_SIZE` | `100` | ≥ 1 | Items displayed per thumbnail grid page |
| `LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` | Log verbosity |
| `SECURE_COOKIES` | `true` | `true` / `false` | Set `Secure` flag on session cookies; set to `false` for plain-HTTP local deployments |
| `PLAYER_CORS_ORIGINS` | unset | comma-separated origins | Allowed browser origins for credentialed CORS requests; unset/empty emits no CORS headers |
| `TRANSCODE_CACHE_DIR` | `transcode-cache` next to `DB_PATH` | writable directory | Cache for compatibility renditions (H.264/AAC) of AVI/WMV/FLV/WMA media; created on first use |
| `TRANSCODE_CACHE_MAX_MB` | `4096` | ≥ 1 | Size the transcode cache is pruned back to (least recently used first) |

**Important:** The K8s `Deployment` overrides `DB_PATH` to `/data/media.db` and `MEDIA_ROOT` to `/media` so the PVC mounts are used. Do not rely on the local defaults in a container.

**Transcode cache:** the container root filesystem is read-only and the process runs as UID 65534, so the cache must live on a writable volume. With `DB_PATH=/data/media.db` it defaults to `/data/transcode-cache`.

- Disk usage can reach `TRANSCODE_CACHE_MAX_MB` **plus** the most recently used rendition (never evicted, even when it alone exceeds the limit) **plus** the renditions being written (up to three jobs; each roughly the size of its source). Size the volume for that.
- A transcode only starts while the volume keeps 512 MiB free on top of the source's size; otherwise the request fails with `507` instead of filling the disk. This matters because the default location shares its volume with the SQLite database. For anything beyond a small library, point `TRANSCODE_CACHE_DIR` at a volume of its own.
- `TRANSCODE_CACHE_DIR` must not be inside `MEDIA_ROOT` (the scanner would import renditions as media); the server refuses to start otherwise. That includes the default when `DB_PATH` itself lies inside `MEDIA_ROOT` — set `TRANSCODE_CACHE_DIR` explicitly then.
- At startup the server logs a warning when `ffmpeg` is missing or the cache directory is not writable.

---

## Podcast Support

Podcasts live under one shared **podcast set** (`sets.is_podcast = 1`, `root_path = "podcast"`). Each subscribed podcast is a `podcast_feeds` row and gets its own folder inside that single set.

### Subscribing

Admin opens the **Podcasts** button in the admin panel (or calls `POST /api/podcasts`):
- Submit an RSS/Atom feed URL.
- Server creates or reuses the shared `podcast` set, creates a feed folder inside it, parses the feed, downloads the cover image, and inserts episodes into `podcast_episodes`.

### Episode Management

Episodes are stored in `podcast_episodes` and rendered in the browse grid under the shared podcast set:
- **Undownloaded** episodes show a **Download to server** button (calls `POST /api/podcasts/episodes/{id}/download`).
- **Downloaded** episodes become regular `media` rows and appear as normal media cards.
- Users can mark episodes as listened/unlistened via the checkmark button.

### Background Feed Checker

A background goroutine (`CheckFeeds`) refreshes feeds every hour (configurable via `PODCAST_CHECK_INTERVAL_MINUTES`). It uses conditional GET (`If-None-Match`, `If-Modified-Since`) to avoid re-downloading unchanged feeds.

### New Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/podcasts` | List subscribed podcast feeds |
| `POST` | `/api/podcasts` | Subscribe to a new feed (admin) |
| `DELETE` | `/api/podcasts/{id}` | Unsubscribe a feed and delete its downloaded episodes (admin) |
| `GET` | `/api/podcasts/{id}/episodes` | List episodes with status |
| `POST` | `/api/podcasts/episodes/{id}/download` | Server-side download |
| `POST` | `/api/podcasts/episodes/{id}/complete` | Toggle completion |

### New Files

- `internal/model/podcast.go` — PodcastFeed, PodcastEpisode, PodcastStatus
- `internal/repository/podcast.go` — CRUD and queries
- `internal/podcast/feed.go` — RSS/Atom parser using `gofeed`
- `internal/podcast/cover.go` — Cover image downloader
- `internal/service/podcast.go` — Business logic and background checker
- `internal/api/handlers_podcast.go` — REST handlers
- `internal/service/import.go` — Shared `ImportMediaFile` helper (used by uploads + downloader)
- `web/js/podcasts.js` — Feed manager modal and episode renderer

---

## Compatibility Stream (server-side transcoding)

Browsers cannot decode AVI/WMV/FLV/WMA (nor MPEG-4 part 2, MPEG-2, AC-3/DTS audio, ...) and Android ExoPlayer cannot decode WMV/FLV/WMA, so the server offers an ffmpeg-produced rendition: H.264 + AAC in MP4 for video, AAC in MP4 (M4A) for audio.

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/api/media/{id}/compat` · `/api/v1/media/{id}/compat` | session | Rendition of a media item (registered via `handleBoth`, same access check as `/stream`) |
| `GET` | `/s/{token}/compat` | none (share token) | Rendition of shared media |

- **One rule for all clients:** `model.Media.NeedsCompatStream()` — true when at least one client cannot decode the original (legacy container *or* legacy codec; some flagged formats, e.g. AVI, do play on Android). It surfaces as `"transcoded": true` in every media JSON object, and as `playback_url` + `transcoded` in `GET /api/media/{id}/playback` and the `GET /s/{token}` JSON. Clients play `playback_url`; they must not re-derive the rule from the file extension. `needs_transcode` in the playback hint is an older, broader heuristic and is not the switch. The endpoint only serves flagged media: other audio/video gets `400` ("play the stream endpoint instead"), images `415`.
- **Codec metadata:** the prober stores `"video/audio"` for video files (`probe.codecString`), which is what lets the rule see e.g. AC-3 next to H.264. Rows probed before that hold the video codec only and are judged by container and video codec until re-probed.
- **Layers:** `internal/transcode` (`Runner` interface + `FFmpegRunner`, `Cache`) → `service.CompatStreamService` (access checks, error mapping; depends on the `RenditionProvider` and `SharedMediaAccess` interfaces) → `api/handlers_compat.go`. Unit tests inject a fake `Runner`; `internal/app/app_test.go` drives the production wiring with a real SQLite store.
- **ffmpeg input hardening (do not relax):** the demuxer is pinned from the file extension (`demuxerFor`), `-protocol_whitelist file` is set and the input is passed as `file:<absolute path>`. Without the pinned demuxer a file named `x.avi` that contains an ffconcat/HLS playlist makes ffmpeg read *other* files into the rendition. The service additionally resolves symlinks and requires the real path to be inside `MEDIA_ROOT`.
- **Stream copy:** the runner asks `ffprobe` what the file really contains; 8-bit 4:2:0 H.264 video (except in AVI, which has no timestamps) and AAC audio are copied instead of re-encoded, with an automatic fall back to encoding when the copy fails. Encodes are capped at 1920x1080, deinterlaced when flagged interlaced, and run under `nice` with half the CPUs per job.
- **Cache:** renditions are complete files in `TRANSCODE_CACHE_DIR`, served with `http.ServeContent` (Range/seek works). The file name encodes media id, source size, source mtime and a profile version, so a changed source or changed ffmpeg arguments (`profileVersion` in `internal/transcode/transcode.go` — bump it when you change the arguments) invalidate the rendition. Output is written to a uniquely named `.tmp` file and renamed, so several instances can share the directory. The ETag also contains the rendition's creation time: a rebuilt rendition never shares an ETag with its predecessor.
- **Limits:** concurrent requests for one source share one job; at most 3 ffmpeg processes run, at most 8 jobs are running or queued, at most 2 per user or share token (`503` "busy" beyond that). A source that failed is not retried for 1 min, doubling up to 30 min. A job may work for 2 h (queue time not counted).
- **Bounding:** before each transcode and on every GC tick (`GC_INTERVAL_MINUTES`) the cache is pruned to `TRANSCODE_CACHE_MAX_MB`, least recently used first (the most recently used rendition and anything served in the last minute are spared); `.tmp` files untouched for an hour are removed; the GC also deletes the renditions of hard-deleted media and stops their running jobs. Only files matching the exact rendition name pattern are ever deleted.
- **Waiting:** a request waits up to 20 s. If the transcode is not finished it answers `503` with `Retry-After: 5` and `{"status":"transcoding"}`; the transcode keeps running in the background (also when the client disconnects). On shutdown running ffmpeg processes are killed and waited for.
- **Tests with real ffmpeg:** `internal/transcode/ffmpeg_real_test.go` skips when ffmpeg/ffprobe/libx264 are missing. Set `PLAYER_REQUIRE_FFMPEG=1` to turn that skip into a failure (the `server-transcode` CI step does); the argument list itself is pinned by `TestFFmpegArgs` without ffmpeg.

---

## Notes for Agents

- When modifying tests, always run `go test ./... -race -cover` before committing.
- Do not introduce package-level mutable state; inject via constructors.
- All repository access goes through the `repository.Store` interface.
- Frontend modules are plain ES modules — no transpilation step. Keep JS vanilla.
- CSS changes must use `var(--*)` tokens from `theme.css`.
- If you add new env vars, update both `internal/config.go` and this document.

---

## Auth Middleware — Bearer-or-Cookie Unified Pattern

`RequireSession` in `internal/api/middleware.go` implements a single unified
authentication path: it tries a Bearer token first (via the `Authorization:
Bearer <token>` header) and falls back to the `session` cookie if no Bearer
header is present. Both paths resolve to the same `*model.Session` value in the
request context, so all downstream handlers are auth-method-agnostic. Do not
add a separate middleware or a parallel code path for Bearer-only or
cookie-only routes — doing so would create duplicate auth logic and silently
break the unified contract. If you need to handle a new auth mechanism, extend
`authenticate()` inside `Middleware` and keep the single `RequireSession`
wrapper as the sole gate for session-required routes.

## Route Registration — `handleBoth` Convention

Every session-required and admin route must be registered via `handleBoth` in
`server.go`, never via `mux.Handle` or `mux.HandleFunc` directly. `handleBoth`
registers the handler under both `/api/<path>` (legacy / web-app prefix) and
`/api/v1/<path>` (stable contract prefix) in one call, ensuring the two
prefixes remain in sync. If you add a new route and use `mux.HandleFunc`
directly, only one prefix will be registered and the multi-client contract will
silently drift — the web SPA or mobile client will break without any compile-
time or test-time signal. Public endpoints that already have explicit v1 aliases
(e.g. `/api/login` / `/api/v1/auth/login`) are exempt because they use custom
path naming that does not follow the `/api/` → `/api/v1/` mechanical transform.
