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

Probing (`internal/probe`), thumbnails and covers (`internal/thumb`) and the
MPEG-TS remuxer open every file through `internal/ffsafe`, never by its bare
name: `ffsafe.SourceArgs` for a file of any type, `ffsafe.InputArgs` for
audio/video only, `ffsafe.OutputArg` for a file ffmpeg writes. A playlist
disguised as media (`evil.avi` holding an ffconcat list) is refused instead
of followed. The prober reports such a file as `probe.ErrUnreadable` and
does not retry it (retries are for failures to run ffprobe or reach the
file); the scanner logs a warning and does not index it; an upload, podcast
download or "regenerate thumbnail" of it fails with
`service.ErrUnreadableMedia`, HTTP `415` and a fixed text. Error detail of a
failed upload (absolute paths, ffprobe output) is logged, never sent to the
client. See "ffmpeg input hardening" below before adding an ffmpeg call.

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
| `SECURE_COOKIES` | `true` | `true` / `false` | Set `Secure` flag on session cookies and share viewing cookies; set to `false` for plain-HTTP local deployments |
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
- **HEAD is the readiness probe:** clients poll the compat URL with `HEAD`. It runs the same access checks and starts or joins the transcode exactly like `GET`, answers `200` with the rendition's headers and no body when ready, and never consumes a share use. Share uses are counted per viewing (see "Share Viewings" below): a request with a valid viewing credential costs nothing; one without opens a viewing, but only for a `GET` and only after the rendition file has been opened, i.e. when content is certain to be delivered. Every `503` (GET and HEAD) carries `X-Transcode-Status: transcoding|busy` next to `Retry-After`, because a HEAD response has no JSON body.
- **Codec metadata:** the prober stores `"video/audio"` for video files and the audio codec alone for audio files (cover art is ignored), see `probe.codecString`. That is what lets the rule see e.g. AC-3 next to H.264. Rows probed by older versions hold the video codec only and are judged by container and video codec until re-probed.
- **Layers:** `internal/ffsafe` (input hardening) → `internal/transcode` (`Runner` interface + `FFmpegRunner`, `Cache`) → `service.CompatStreamService` (access checks, error mapping; depends on the `RenditionProvider` and `SharedMediaAccess` interfaces) → `api/handlers_compat.go`. Unit tests inject a fake `Runner`; `internal/app/app_test.go` drives the production wiring with a real SQLite store.
- **ffmpeg input hardening (do not relax):** every input goes through `ffsafe.InputArgs`: `-format_whitelist` (real containers only — no concat/hls/image sequences), `-protocol_whitelist file`, and `file:<absolute path>`. Without the format whitelist a file named `x.avi` that contains an ffconcat playlist makes ffmpeg read *other* files into the rendition. The container is still auto-detected among the allowed formats, so a mislabeled but genuine file (an MP4 named `.flv`) works. This holds for **every** ffmpeg/ffprobe call in the server — transcoder, library prober, thumbnail/cover generator and remuxer — and `ffsafe` is the only place that defines the allowed demuxers. Still images get their own rule, `ffsafe.ImageInputArgs` (chosen by `ffsafe.SourceArgs` for image extensions): the demuxer is picked from the file's first bytes and forced with `-f` (`jpeg_pipe`, `png_pipe`, `gif`, `webp_pipe`, `bmp_pipe`, `svg_pipe`, `mov` for AVIF), because ffmpeg's own detection hands images to `image2`, which expands `%d` patterns in the file *name* and reads sibling files (`a%03d.png` → `a000.png`); never whitelist `image2`. Content that is no supported image fails with `ffsafe.ErrNotAnImage` before ffmpeg starts. Outputs are named with `ffsafe.OutputArg` (the transcoder builds the same `file:<absolute path>` itself). Two kinds of tests guard this without needing ffmpeg. `TestEveryCommandGetsFFsafeArguments` (`internal/ffsafe/callsites_test.go`) parses every non-test Go file of the server and requires each function that references `exec.Command`/`exec.CommandContext` (under any import name) or builds an `exec.Cmd` literal to (a) call `ffsafe.InputArgs`, `SourceArgs` or `ImageInputArgs`, or (b) call a function of its package that does, or (c) be called only by such functions of its package; a command outside a function, a dot-import of `os/exec`, `os.StartProcess` and `syscall.Exec`/`ForkExec`/`StartProcess` are always rejected, and exceptions need an entry in `commandAllowlist` there. It matches functions by name, looks for callers in the function's own package only (keep functions that run prebuilt arguments unexported) and does not follow data flow, so it proves that a call site is wired to `ffsafe`, not that the right arguments are passed: that is what `TestProbeArgs`, `TestRemuxArgs`, `TestThumbArgs` and `TestFFmpegArgs` pin. A new call site needs both. What the whitelists rely on in ffmpeg itself (mov ignoring external data references by default, SVG decoded without a base location) is listed in the `ffsafe` package comment and must be re-verified when the runtime image's ffmpeg is upgraded.
- **Symlinks:** the service resolves symlinks and requires the real path to be inside the (resolved) `MEDIA_ROOT`. This is stricter than `/stream`, which only checks the path lexically: a set directory that is a symlink to somewhere outside `MEDIA_ROOT` plays via `/stream` but gets `403` on `/compat`.
- **Stream copy:** the runner asks `ffprobe` what the file really contains. Video is copied only if it is 8-bit 4:2:0 H.264 within 1920x1080 / level 4.1 and not from AVI (no timestamps); audio only if it is AAC-LC with at most two channels. A copy is verified with `ffprobe` (expected streams, non-zero duration) and repeated as a full encode if ffmpeg refuses it or the result is wrong; a video stream is also encoded rather than copied when the source file is larger than the space the output may take. The runner never classifies an oversized output — only the cache knows whether the budget (`ErrTooLarge`, remembered) or low free space (`ErrNoSpace`, not remembered) set the cap. Encodes are capped at 1920x1080, deinterlaced when flagged interlaced, and run under `nice`.
- **Cache:** renditions are complete files in `TRANSCODE_CACHE_DIR`, served with `http.ServeContent` (Range/seek works). The file name encodes media id, source size, source mtime and a profile version, so a changed source or changed ffmpeg arguments (`profileVersion` in `internal/transcode/transcode.go` — bump it when you change the arguments) invalidate the rendition. Output is written to a uniquely named `.tmp` file and renamed. The ETag also contains the rendition's creation time: a rebuilt rendition never shares an ETag with its predecessor.
- **Limits:** concurrent requests for one source share one job; `TRANSCODE_MAX_JOBS` (default 1) ffmpeg processes run, at most 8 jobs are running or queued, at most 2 per signed-in user and 2 for *all* share links together (`503` "busy" beyond that). A job may work for 2 h (queue time not counted). A source modified in the last 10 s is not started (`503`), it is probably still being uploaded; a modification time more than 10 s in the future (wrong camera clock) counts as settled, a smaller skew still waits.
- **Failure backoff (per media id):** a failed transcode is not retried for 1 min, doubling up to 30 min; a job that hit the 2 h timeout, or whose rendition exceeded the cache budget, is not retried for 24 h. All of these end at once when the source file is replaced (the entry is tied to the source's size and mtime), and the doubling starts over for the new file. A source that changed *during* the run is retried on the same 1 min+ schedule regardless of further changes, and answers `503` meanwhile. The failure is logged once at Error level when it happens; later requests answered from the backoff log at debug level only.
- **Bounding:** after each successful transcode and on every GC tick (`GC_INTERVAL_MINUTES`) the cache is pruned to `TRANSCODE_CACHE_MAX_MB`, least recently used first (the most recently used rendition and anything served in the last minute are spared); `.tmp` files untouched for an hour are removed; when the GC purges a media item it deletes its file, its row, its generated thumbnail and its renditions (stopping a running job). Only files matching the exact rendition name pattern are ever deleted.
- **Waiting and shutdown:** a request waits up to 20 s. If the transcode is not finished it answers `503` with `Retry-After: 5` and status `transcoding`; the transcode keeps running in the background (also when the client disconnects). On shutdown the cache is closed first — waiting requests return `503` busy at once, no new job starts, ffmpeg is killed — then the HTTP server drains, then the jobs are waited for at most 10 s, and the database is closed in any case.
- **Tests with real ffmpeg:** `internal/transcode/ffmpeg_real_test.go` skips when ffmpeg/ffprobe/libx264 are missing, as do the hardening tests of `ffsafe`, `probe`, `thumb` and `scanner` (`*Real*` tests; shared fixtures in `internal/ffsafe/ffsafetest`). CI runs them twice: with the ffmpeg of the golang image (`server-transcode`) and, as static test binaries, with the ffmpeg of the runtime image `alpine:3.21` (`server-ffmpeg-runtime`) — the two are different major versions. `PLAYER_SAMPLE_LIBRARY=<dir written by testdata/gen-all-formats.sh>` additionally enables `TestScan_RealSampleLibrary`, which checks that every supported extension still probes and thumbnails. Set `PLAYER_REQUIRE_FFMPEG=1` to turn every such skip into a failure (the `server-transcode` CI step does); the argument list itself is pinned by `TestFFmpegArgs` without ffmpeg.

---

## Share Viewings (how `max_uses` is counted)

A share's `max_uses` counts **viewings**, not HTTP requests. Browsers and players fetch one file with several ranged GETs; when each of them cost a use, a `max_uses=1` link died after the first second of playback. The HTTP contract is in `docs/api.md` ("Share viewings"); this is what to keep in mind when changing the code.

- **Flow:** a viewing costs one use, claimed by the single conditional `UPDATE` in `repository.UseShare` (keep it that way — it is what makes parallel opens of the last use safe). It is opened by a request without a valid credential that is `POST /s/{token}/view` (sent by the share page's script, `web/js/shareViewing.js`), `GET /s/{token}` with `Accept: application/json` (apps), or a `GET` straight to `/stream`, `/compat` or `/download` (pasted direct links and old app versions, at one use per request). The response hands over a credential; later requests presenting it cost nothing and work past `max_uses`.
- **The HTML page is free, and must stay free.** Chat apps, mail scanners and prefetching browsers fetch every link they see; if `GET /s/{token}` as HTML opened a viewing, a single-use link would be spent before its recipient opens it. So the HTML form, every `HEAD` and `/thumbnail` are "probes" (`service.ShareAccess.Probe`): never a use, never a cookie. The page opens the viewing itself with the `POST` — a method previewers do not send — and must not touch any media URL (player source, thumbnail, download) before it succeeded: a media request without the cookie opens a viewing of its own.
- **Credential:** `<expiry unix seconds>.<base64url HMAC-SHA256>` over the share token and the expiry (`service/share_viewing.go`). Stateless: there is no viewing table and nothing to garbage-collect. It is valid for `service.ShareViewingLifetime` (6 h) or until the share expires, whichever is first, cannot be forged or extended, and is useless for another token. Parsing accepts exactly one spelling (alphabet check before decoding, because Go's base64 decoder skips CR/LF; re-encode comparison after). Anything invalid is treated exactly like no credential, never as an error. It is a bearer token: whoever holds it can use the share until it expires or the share is revoked.
- **Key:** 32 random bytes in the `server_secrets` table, created on first use (`repository.ShareViewingKey`), cached by each `shareService`. Viewings therefore survive restarts and rolling updates; deleting the row (or the database) invalidates all running viewings, whose clients then simply open new ones.
- **Transport:** browsers get the cookie `share_view` (`HttpOnly`, `SameSite=Lax`, `Path=/s/{token}`, `Secure` per `SECURE_COOKIES`); the HTML page embeds plain URLs and never the credential. Clients without a cookie jar use the JSON form, whose URLs already end in `?view=<credential>` (`GetSharedMediaResult.WithViewCredential`). A request may present both; the service takes the first that verifies (parameter, then cookie), so a stale parameter does not shadow a good cookie. `HttpOnly` only keeps the value out of `document.cookie` — a same-origin script could still request the JSON form — and the cookie `Path` assumes the server is mounted at `/`.
- **Revocation and expiry stay immediate:** `shareService.usableShare` loads the share row on every request before anything else; the credential only waives the `max_uses` check. Do not add a path that trusts a credential without loading the share.
- **Do not log the credential.** Log `r.URL.Path`, never `r.URL.String()` / `RawQuery`, on share routes. All share responses carry `Cache-Control: no-store` and `Referrer-Policy: no-referrer` (`api.preparePublicShare`), so the `?view=` form does not leak through caches or the `Referer` header.
- **Layers:** `repository` (use counter, key) → `service.shareService` (`usableShare`, `EnsureShareViewing`, sign/verify) → `api/share_viewing.go` (cookie, parameter, error mapping) and the handlers in `handlers_share.go` / `handlers_compat.go`. The compat service reaches the share service through the narrow `SharedMediaAccess` interface.
- **Tests:** `internal/app/share_viewing_test.go` and `share_page_test.go` drive the production wiring with a real SQLite store (single-use playback with many ranged requests, previewers, the page's `POST`, invalid credentials, revocation, implicit viewing, HEAD, concurrency, expiry cap); `internal/service/share_viewing_test.go` covers the credential format and, with a movable clock on a real store, expiry and restart; `web/js/tests/share-viewing.test.js` the page's request; `test/e2e-web/tests/share-single-use.test.ts` plays a single-use MP4 share in a browser and checks that the `POST` precedes every media request.
- **The share page always settles** in a visible state, marked on `<body data-share-viewing="open|gone|failed">`: the player, or a message (`#share-status`). Its script registers the download button before it sets up the player and catches any exception from the setup, because by then the use is spent — a blank page would waste it. `share.html` must contain every element `playback.js` dereferences without a guard for the media types it shows (`#media-video`, `#media-audio`, `#media-image`, the progress and time elements); the image element was once missing and image shares showed nothing.
- **Routing:** share routes are registered through `Server.shareRoute`, which also answers other methods with `405` + `Allow`; unknown paths below `/s/` answer `404`. Without that they fall through to the session-protected catch-all and answer `401`.
- **What can still spend a use unintentionally:** a scanner that executes the page's JavaScript, any tool that requests the JSON form, and a browser that drops the cookie (then every media request opens its own viewing).

---

## Notes for Agents

- When modifying tests, always run `go test ./... -race -cover` before committing.
- Do not introduce package-level mutable state; inject via constructors.
- All repository access goes through the `repository.Store` interface.
- SQLite settings that must hold for every query (foreign keys, busy timeout, UTC timestamps) are DSN options built by `repository.connectionDSN`, never a `PRAGMA` run once with `db.Exec`: `database/sql` replaces a pooled connection after a driver error or a cancelled query, and the replacement would not have it. Foreign keys silently switching off is how deleted users once kept their sessions. `DeleteUser` also removes a user's rows explicitly, session and API-token lookups join `users`, and `removeOrphanRows` cleans up at startup; keep all three when adding a table that references `users` or `media` (list it in `repository/orphans.go`).
- Frontend modules are plain ES modules — no transpilation step. Keep JS vanilla.
- CSS changes must use `var(--*)` tokens from `theme.css`.
- If you add new env vars, update both `internal/config.go` and this document.
- **Timestamps are stored as text and compared as text** (share and session expiry, podcast scheduling, every `ORDER BY` on a time column). That is only correct because every value is written in UTC in one layout: `repository.Open` adds the driver option `_timezone=UTC` (the driver would otherwise write `time.String()` in whatever zone a value carries, and a share with a UTC `expires_at` expired three hours early on a server in UTC+3), and `normalizeStoredTimes` converts rows older versions wrote in a local zone at startup. See `internal/repository/time_utc.go`. When you add a time column, add it to `timeColumns` there; never bind a pre-formatted time string, and never open the database without `repository.Open`.

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
