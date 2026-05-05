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
| `mage test` | Run `go test ./...` |
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

Global shortcuts are registered in `web/js/keyboard.js`. They are **disabled** while the user is focused on an `INPUT`, `TEXTAREA`, or `contentEditable` element (except `Escape` to blur).

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
| `r` | Toggle shuffle on the current filtered result set |
| `s` | Generate a share link for the selected media |
| `/` | Focus the quick search bar (debounced) |
| `n` | Open notes modal for the selected media |

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
| `LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` | Log verbosity |
| `SECURE_COOKIES` | `true` | `true` / `false` | Set `Secure` flag on session cookies; set to `false` for plain-HTTP local deployments |

**Important:** The K8s `Deployment` overrides `DB_PATH` to `/data/media.db` and `MEDIA_ROOT` to `/media` so the PVC mounts are used. Do not rely on the local defaults in a container.

---

## Podcast Support

Podcasts are **special sets** (`sets.is_podcast = 1`). They reuse set permissions, browsing, and cover images, while adding feed management and episode tracking.

### Subscribing

Admin opens the **Podcasts** button in the admin panel (or calls `POST /api/podcasts`):
- Submit an RSS/Atom feed URL and optional folder name.
- Server creates a set folder, parses the feed, downloads the cover image, and inserts episodes into `podcast_episodes`.

### Episode Management

Episodes are stored in `podcast_episodes` and rendered in the browse grid for podcast sets:
- **Undownloaded** episodes show a **Download to server** button (calls `POST /api/podcasts/episodes/{id}/download`).
- **Downloaded** episodes become regular `media` rows and appear as normal media cards.
- Users can mark episodes as listened/unlistened via the checkmark button.

### Background Feed Checker

A background goroutine (`CheckFeeds`) refreshes feeds every hour (configurable via `PODCAST_CHECK_INTERVAL_MINUTES`). It uses conditional GET (`If-None-Match`, `If-Modified-Since`) to avoid re-downloading unchanged feeds.

### New Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/podcasts` | List podcast sets |
| `POST` | `/api/podcasts` | Subscribe to a new feed (admin) |
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

## Notes for Agents

- When modifying tests, always run `go test ./... -race -cover` before committing.
- Do not introduce package-level mutable state; inject via constructors.
- All repository access goes through the `repository.Store` interface.
- Frontend modules are plain ES modules — no transpilation step. Keep JS vanilla.
- CSS changes must use `var(--*)` tokens from `theme.css`.
- If you add new env vars, update both `internal/config.go` and this document.
