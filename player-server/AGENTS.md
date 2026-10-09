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
6. Regenerates thumbnails of indexed media that are still stored under an
   older naming scheme

Generated thumbnails live in `.thumbnails` next to the source file, named
`<full source name>.jpg` (`holiday.mp4` -> `.thumbnails/holiday.mp4.jpg`);
always derive the path with `thumb.ThumbnailPathFor`, and create or delete
thumbnails only through `thumb.FSMaker` (`service.ThumbnailMaker` in the
service layer), which generates into a temporary file, verifies the result
and renames it into place. Releases up to v0.2.2
used `<stem>.jpg`, which made same-stem files share a thumbnail, so **one
admin rescan is needed after upgrading** from those. See
`docs/admin.md` ("Thumbnail Naming and Upgrades") for details and cost.

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
| `TRANSCODE_CACHE_DIR` | `<DB_PATH>.transcode-cache` | writable directory outside `MEDIA_ROOT` | Cache for compatibility renditions (H.264/AAC) of legacy media; created on first use |
| `TRANSCODE_CACHE_MAX_MB` | `4096` | ≥ 1 | Size the transcode cache is pruned back to (least recently used first); also the largest single rendition |
| `TRANSCODE_MAX_JOBS` | `1` | ≥ 1 | Parallel ffmpeg transcodes; each needs about 400 MB of memory at 1080p |

**Important:** The K8s `Deployment` overrides `DB_PATH` to `/data/media.db` and `MEDIA_ROOT` to `/media` so the PVC mounts are used. Do not rely on the local defaults in a container.

**Transcode cache:** the container root filesystem is read-only and the process runs as UID 65534, so the cache must live on a writable volume. It defaults to a directory named after the database file: `data.db.transcode-cache` locally (gitignored), `/data/media.db.transcode-cache` with `DB_PATH=/data/media.db`. Naming it after the database keeps several instances with databases in one directory (dev, e2e runs in `/tmp`) from sharing — and deleting — each other's renditions.

- **Disk:** the cache is pruned back to `TRANSCODE_CACHE_MAX_MB` after every successful transcode and on every GC tick, always keeping the most recently used rendition. In between it can hold the budget plus one output per running job, each up to the budget (or less, if less is available): with `TRANSCODE_MAX_JOBS` = N that is up to (1 + N) times the budget. Every output is capped while ffmpeg writes it (`-fs`) to what the volume can give while keeping 512 MiB free, and to `TRANSCODE_CACHE_MAX_MB` at most. Older renditions are evicted for a new job only when the volume itself is short of space, and then no more than that job is assumed to need.
- **Renditions over budget:** `TRANSCODE_CACHE_MAX_MB` is also the largest rendition that can exist. A source is transcoded once; if the output reaches the budget it is discarded and the item answers `507` from then on, without running ffmpeg again, until the file changes, 24 hours pass or the server restarts. A source larger than eight times the budget (32 GiB at the default) is refused the same way without even trying: ordinary legacy video shrinks to a half or a quarter at best, so the attempt would run for hours for nothing. (Lightly compressed sources such as DV or MJPEG AVI shrink far more and are refused wrongly by that rule — raise the budget for them.) A source that would be stream-copied but is larger than the space allowed is re-encoded instead, which may fit. None of this evicts other renditions. A volume that is merely full at the moment also answers `507`, but that is not remembered: playback works again as soon as space is free. With the 4 GiB default the budget matters mainly for long H.264 films with AC-3/DTS audio: raise it if such files should play.
- **Sizes:** a rendition is usually smaller than a legacy video original but can be several times larger than low-bitrate audio (it is encoded at 128 kbit/s). The default location shares its volume with the SQLite database; for anything beyond a small library, point `TRANSCODE_CACHE_DIR` at a volume of its own.
- **Memory and CPU:** one 1080p transcode needs about 400 MB and as many threads as the process may use CPUs (the container CPU limit counts), divided by `TRANSCODE_MAX_JOBS`. `k8s/deployment.yaml` sets a 1Gi memory limit for the server plus one job; add roughly 400Mi per additional job.
- **Capacity of a small instance:** with the shipped limits (1 CPU, one job) a full re-encode runs on a single thread at low priority (`nice 10`) and may be slower than real time for 1080p material. A job is stopped after 2 hours of work; a film that does not finish in that time fails with `500` and is not retried for 24 hours (or until the file changes). Stream copies are not affected — they take seconds to minutes. The knobs: raise the container CPU limit (more threads per job), keep `TRANSCODE_MAX_JOBS` at 1 unless CPU and memory were raised accordingly (more jobs divide the same CPUs and only make each one slower).
- `TRANSCODE_CACHE_DIR` must not be inside `MEDIA_ROOT` (the scanner would import renditions as media); the server refuses to start otherwise. That includes the default when `DB_PATH` itself lies inside `MEDIA_ROOT` — set `TRANSCODE_CACHE_DIR` explicitly then.
- The cache's eviction order and failure backoff are kept in memory per process, which fits the single-replica deployment. During a rolling update two instances may briefly share the directory: that is safe (unique temporary files), each instance just evicts by its own view.
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
| `GET`, `HEAD` | `/api/media/{id}/compat` · `/api/v1/media/{id}/compat` | session | Rendition of a media item (registered via `handleBoth`, same access check as `/stream`) |
| `GET`, `HEAD` | `/s/{token}/compat` | none (share token) | Rendition of shared media |

- **One rule for all clients:** `model.Media.NeedsCompatStream()` — true when at least one client cannot decode the original (legacy container *or* legacy codec; some flagged formats, e.g. AVI, do play on Android). Audio media is judged by audio codecs only. It surfaces as `"transcoded": true` in every media JSON object, and as `playback_url` + `transcoded` in `GET /api/media/{id}/playback` and the `GET /s/{token}` JSON. Clients play `playback_url`; they must not re-derive the rule from the file extension. `needs_transcode` in the playback hint is an older, broader heuristic and is not the switch (it is never false for an item flagged `transcoded`). The endpoint only serves flagged media: other audio/video gets `400` ("play the stream endpoint instead"), images `415`.
- **HEAD is the readiness probe:** clients poll the compat URL with `HEAD`. It runs the same access checks and starts or joins the transcode exactly like `GET`, answers `200` with the rendition's headers and no body when ready, and never consumes a share use. A share use is counted only for a `GET`, after the rendition file has been opened, i.e. when content is certain to be delivered. Every `503` (GET and HEAD) carries `X-Transcode-Status: transcoding|busy` next to `Retry-After`, because a HEAD response has no JSON body.
- **Codec metadata:** the prober stores `"video/audio"` for video files and the audio codec alone for audio files (cover art is ignored), see `probe.codecString`. That is what lets the rule see e.g. AC-3 next to H.264. Rows probed by older versions hold the video codec only and are judged by container and video codec until re-probed.
- **Layers:** `internal/ffsafe` (input hardening) → `internal/transcode` (`Runner` interface + `FFmpegRunner`, `Cache`) → `service.CompatStreamService` (access checks, error mapping; depends on the `RenditionProvider` and `SharedMediaAccess` interfaces) → `api/handlers_compat.go`. Unit tests inject a fake `Runner`; `internal/app/app_test.go` drives the production wiring with a real SQLite store.
- **ffmpeg input hardening (do not relax):** every input goes through `ffsafe.InputArgs`: `-format_whitelist` (real containers only — no concat/hls/image sequences), `-protocol_whitelist file`, and `file:<absolute path>`. Without the format whitelist a file named `x.avi` that contains an ffconcat playlist makes ffmpeg read *other* files into the rendition. The container is still auto-detected among the allowed formats, so a mislabeled but genuine file (an MP4 named `.flv`) works. Use the same helper for any other ffmpeg/ffprobe call on user media (the library prober, thumbnailer and remuxer do not use it yet — task c83).
- **Symlinks:** the service resolves symlinks and requires the real path to be inside the (resolved) `MEDIA_ROOT`. This is stricter than `/stream`, which only checks the path lexically: a set directory that is a symlink to somewhere outside `MEDIA_ROOT` plays via `/stream` but gets `403` on `/compat`.
- **Stream copy:** the runner asks `ffprobe` what the file really contains. Video is copied only if it is 8-bit 4:2:0 H.264 within 1920x1080 / level 4.1 and not from AVI (no timestamps); audio only if it is AAC-LC with at most two channels. A copy is verified with `ffprobe` (expected streams, non-zero duration) and repeated as a full encode if ffmpeg refuses it or the result is wrong; a video stream is also encoded rather than copied when the source file is larger than the space the output may take. The runner never classifies an oversized output — only the cache knows whether the budget (`ErrTooLarge`, remembered) or low free space (`ErrNoSpace`, not remembered) set the cap. Encodes are capped at 1920x1080, deinterlaced when flagged interlaced, and run under `nice`.
- **Cache:** renditions are complete files in `TRANSCODE_CACHE_DIR`, served with `http.ServeContent` (Range/seek works). The file name encodes media id, source size, source mtime and a profile version, so a changed source or changed ffmpeg arguments (`profileVersion` in `internal/transcode/transcode.go` — bump it when you change the arguments) invalidate the rendition. Output is written to a uniquely named `.tmp` file and renamed. The ETag also contains the rendition's creation time: a rebuilt rendition never shares an ETag with its predecessor.
- **Limits:** concurrent requests for one source share one job; `TRANSCODE_MAX_JOBS` (default 1) ffmpeg processes run, at most 8 jobs are running or queued, at most 2 per signed-in user and 2 for *all* share links together (`503` "busy" beyond that). A job may work for 2 h (queue time not counted). A source modified in the last 10 s is not started (`503`), it is probably still being uploaded; a modification time more than 10 s in the future (wrong camera clock) counts as settled, a smaller skew still waits.
- **Failure backoff (per media id):** a failed transcode is not retried for 1 min, doubling up to 30 min; a job that hit the 2 h timeout, or whose rendition exceeded the cache budget, is not retried for 24 h. All of these end at once when the source file is replaced (the entry is tied to the source's size and mtime), and the doubling starts over for the new file. A source that changed *during* the run is retried on the same 1 min+ schedule regardless of further changes, and answers `503` meanwhile. The failure is logged once at Error level when it happens; later requests answered from the backoff log at debug level only.
- **Bounding:** after each successful transcode and on every GC tick (`GC_INTERVAL_MINUTES`) the cache is pruned to `TRANSCODE_CACHE_MAX_MB`, least recently used first (the most recently used rendition and anything served in the last minute are spared); `.tmp` files untouched for an hour are removed; when the GC purges a media item it deletes its file, its row, its generated thumbnail and its renditions (stopping a running job). Only files matching the exact rendition name pattern are ever deleted.
- **Waiting and shutdown:** a request waits up to 20 s. If the transcode is not finished it answers `503` with `Retry-After: 5` and status `transcoding`; the transcode keeps running in the background (also when the client disconnects). On shutdown the cache is closed first — waiting requests return `503` busy at once, no new job starts, ffmpeg is killed — then the HTTP server drains, then the jobs are waited for at most 10 s, and the database is closed in any case.
- **Tests with real ffmpeg:** `internal/transcode/ffmpeg_real_test.go` skips when ffmpeg/ffprobe/libx264 are missing. Set `PLAYER_REQUIRE_FFMPEG=1` to turn every such skip into a failure (the `server-transcode` CI step does); the argument list itself is pinned by `TestFFmpegArgs` without ffmpeg.

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
