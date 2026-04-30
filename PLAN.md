# KISS Go Media Player — Implementation Plan

## Overview
A self-hosted, Kubernetes-deployable web media player written in Go (stdlib `net/http`) with a SQLite backend, vanilla JavaScript PWA frontend, and progressive offline capability. Designed for **KISS** (Keep It Stupidly Simple): minimal dependencies, zero frontend frameworks, fully interface-driven for testability.

**Target test coverage:** >80% using dependency injection, hand-written mocks, and SQLite `:memory:` instances.

---

## Architecture

- **Backend:** Go 1.23, `net/http`, stdlib + `modernc.org/sqlite`
- **Frontend:** Vanilla JS, CSS Custom Properties, HTML5 `<video>`/`<audio>` custom overlay controls
- **Build:** Mage (`Magefile.go`) with `Build`, `Test`, `Install` targets
- **Deploy:** Multi-stage `Dockerfile`; K8s `Deployment` + `Service` + two PVCs (`/data`, `/media`)
- **Storage:** SQLite on `/data`; media library on `/media`; `ffmpeg`/`ffprobe` installed in runtime image

---

## Project Structure

```
./
├── cmd/mediaplayer/
│   └── main.go              # Entrypoint: flags, config, wire dependencies, start server
├── internal/
│   ├── version.go           # const Version; printed via -version flag
│   ├── config.go            # Env-based config with validation
│   ├── clock/
│   │   ├── clock.go         # Clock interface (Now() time.Time)
│   │   └── real.go          # RealClock implementation
│   ├── model/
│   │   └── media.go         # Domain structs (zero external deps)
│   ├── repository/
│   │   ├── repository.go    # All repository interfaces
│   │   ├── sqlite.go        # Concrete SQLite implementations
│   │   ├── sqlite_test.go   # :memory: table-driven tests
│   │   └── mock.go          # Hand-written fakes for service/handler tests
│   ├── scanner/
│   │   ├── scanner.go       # Recursive set + media scan
│   │   ├── scanner_test.go
│   │   └── fs.go            # FS interface + OS implementation
│   ├── probe/
│   │   ├── probe.go         # ffprobe wrapper interface
│   │   ├── probe_test.go    # Golden JSON fixtures
│   │   └── testdata/
│   ├── thumb/
│   │   ├── thumb.go         # Thumbnail generator interface
│   │   └── thumb_test.go
│   ├── auth/
│   │   ├── auth.go          # Hasher interface, session manager
│   │   └── auth_test.go
│   ├── service/
│   │   ├── media.go         # MediaService (business layer)
│   │   ├── admin.go         # AdminService (user/set CRUD)
│   │   ├── progress.go      # Playback counter + resume logic
│   │   ├── progress_test.go # Heavy tests for 60s rule
│   │   ├── gc.go            # Garbage collector for soft-deleted items
│   │   └── gc_test.go       # Mock clock + repo tests
│   ├── api/
│   │   ├── server.go        # Route table, middleware wiring
│   │   ├── handlers.go        # Thin HTTP adapters
│   │   ├── handlers_test.go   # httptest + mocked services
│   │   └── middleware.go    # Session auth, admin gate, bootstrap bypass
│   └── setassign/
│       ├── assign.go        # Permission helper logic
│       └── assign_test.go
├── web/
│   ├── index.html           # SPA shell (auth-gated)
│   ├── login.html           # Login form
│   ├── bootstrap.html       # One-time admin bootstrap form
│   ├── css/
│   │   ├── base.css         # Reset, layout utilities
│   │   ├── theme.css        # Default dark palette (ALL var(--*) definitions)
│   │   ├── components.css   # Buttons, cards, forms, modals
│   │   ├── player.css       # Custom overlay, fullscreen progress-bar-visible
│   │   ├── layout.css       # Responsive grid/flex
│   │   ├── login.css        # Login page layout
  │   │   └── themes/        # (empty; themes toggled via data-theme in theme.css)

│   └── js/
│       ├── app.js           # Router, auth bootstrap, orchestration
│       ├── api.js           # Fetch wrapper (credentials: include)
│       ├── keyboard.js      # Global keydown listener (hjkl/arrows/p/f/Esc/r/s/3/Enter/Space)//
│       ├── selection.js     # `.selected` index management (mouse + keyboard sync)
│       ├── player.js         # Play/resume/switch/fullscreen logic
│       ├── favorites.js     # Heart toggle
│       ├── progress.js      # Periodic progress POST every 3s while playing
│       ├── shuffle.js       # Toggle random ordering of current filtered view
│       ├── themes.js        # Swap `<link>` href for dark/light mode toggle
│       ├── search.js        # `/` key quick search bar (debounced)
│       ├── notes.js         # Per-media notes modal (CRUD)
│       ├── admin.js         # User CRUD, permission matrix, rescan trigger
│       └── pwa.js           # Service Worker registration
├── Dockerfile               # Multi-stage (builder + alpine w/ ffmpeg)
├── k8s/
│   ├── deployment.yaml
│   ├── service.yaml
│   ├── pvc-db.yaml          # /data
│   └── pvc-media.yaml       # /media
├── Magefile.go
├── go.mod
├── go.sum                   # Generated
└── PLAN.md                  # This file
```

---

## Database Schema (SQLite)

```sql
PRAGMA foreign_keys = ON;

CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    is_admin INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE sets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    root_path TEXT UNIQUE NOT NULL,
    cover_thumbnail_path TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE set_permissions (
    set_id INTEGER NOT NULL REFERENCES sets(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT CHECK(role IN ('owner','viewer')) NOT NULL DEFAULT 'viewer',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (set_id, user_id)
);

CREATE TABLE media (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    set_id INTEGER NOT NULL REFERENCES sets(id) ON DELETE CASCADE,
    rel_path TEXT NOT NULL,
    file_name TEXT NOT NULL,
    abs_path TEXT NOT NULL,
    type TEXT CHECK(type IN ('video','audio')) NOT NULL,
    duration REAL,
    codec TEXT,
    resolution TEXT,
    bitrate INTEGER,
    file_size_bytes INTEGER,
    thumbnail_path TEXT,
    play_count INTEGER NOT NULL DEFAULT 0,
    deleted_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(set_id, rel_path)
);

CREATE TABLE tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE NOT NULL
);

CREATE TABLE media_tags (
    media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (media_id, tag_id)
);

CREATE TABLE favorites (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, media_id)
);

CREATE TABLE playback_progress (
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    position_seconds REAL NOT NULL,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, media_id)
);

CREATE TABLE playback_accumulator (
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    last_position REAL NOT NULL DEFAULT 0,
    accumulated_seconds REAL NOT NULL DEFAULT 0,
    counted INTEGER NOT NULL DEFAULT 0,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (session_id, media_id)
);

CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at DATETIME NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE shares (
    token TEXT PRIMARY KEY,
    media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    created_by INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL,
    max_uses INTEGER,
    used_count INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE media_notes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    media_id INTEGER NOT NULL REFERENCES media(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    content TEXT NOT NULL DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(media_id, user_id)
);

-- Indexes
CREATE INDEX idx_media_set_id ON media(set_id);
CREATE INDEX idx_media_rel_path ON media(set_id, rel_path);
CREATE INDEX idx_media_deleted_at ON media(deleted_at);
CREATE INDEX idx_media_type ON media(type);
CREATE INDEX idx_media_filename ON media(file_name);
CREATE INDEX idx_permissions_user ON set_permissions(user_id);
CREATE INDEX idx_permissions_set ON set_permissions(set_id);
CREATE INDEX idx_shares_expires ON shares(expires_at);
```

---

## API Routes

| Method | Route | Auth | Description |
|--------|-------|------|-------------|
| `GET` | `/bootstrap.html` | public | One-time admin creation if `users` table empty |
| `POST` | `/api/bootstrap` | public | Submit first admin user (blocked if users exist) |
| `GET` | `/login.html` | public | Login form |
| `POST` | `/api/login` | public | Create session cookie |
| `POST` | `/api/logout` | session | Invalidate cookie + DB row |
| `GET` | `/api/sets` | session | List visible sets (admin = all) |
| `POST` | `/api/sets/:id/cover` | session (owner/admin) | Regenerate set cover thumbnail |
| `GET` | `/api/media` | session | Query: `set_id`, `type`, `search`, `tags`, `favorites`, `min_duration`, `max_duration`, `sort`, `limit`, `offset` |
| `GET` | `/api/media/:id` | session | Detail + metadata + favorite status + user's note |
| `GET` | `/api/media/:id/stream` | session | Serve original file with Range support |
| `GET` | `/api/media/:id/download` | session | `Content-Disposition: attachment` |
| `GET` | `/api/media/:id/thumbnail` | session | Serve thumbnail file |
| `POST` | `/api/media/:id/thumbnail` | session | Regenerate thumbnail at random offset |
| `POST` | `/api/media/:id/favorite` | session | Toggle favorite |
| `POST` | `/api/media/:id/tags` | session | Assign tag |
| `DELETE` | `/api/media/:id/tags/:tag` | session | Remove tag |
| `POST` | `/api/progress` | session | Update position + accumulate counter |
| `DELETE` | `/api/media/:id` | session (owner/admin) | Soft delete |
| `POST` | `/api/media/:id/restore` | session (owner/admin) | Restore from trash |
| `GET` | `/api/admin/trash` | admin | List soft-deleted items |
| `POST` | `/api/admin/rescan` | admin | Trigger filesystem rescan |
| `GET` | `/api/admin/users` | admin | List users |
| `POST` | `/api/admin/users` | admin | Create user |
| `DELETE` | `/api/admin/users/:id` | admin | Remove user (not self) |
| `GET` | `/api/admin/permissions` | admin | Set permissions matrix |
| `POST` | `/api/admin/permissions` | admin | Grant set access |
| `DELETE` | `/api/admin/permissions` | admin | Revoke set access |
| `POST` | `/api/sets/:id/upload` | session (owner/admin) | Upload file (multipart, max 100MB) |
| `POST` | `/api/media/:id/shares` | session | Create new share link (expires 14d default) |
| `GET` | `/api/media/:id/shares` | session | List active shares for media |
| `DELETE` | `/api/shares/:token` | session | Revoke share |
| `GET` | `/s/:token` | public | Share landing page with inline player |
| `GET` | `/s/:token/stream` | public | Stream shared file |
| `GET` | `/` | session | SPA shell `index.html` (or redirect) |
| `GET` | `/healthz` | public | Liveness probe (no DB) |
| `GET` | `/readyz` | public | Readiness probe (DB ping) |
| `GET` | `/api/media/:id/notes` | session | Get current user's note |
| `POST` | `/api/media/:id/notes` | session | Create or update note |
| `DELETE` | `/api/media/:id/notes` | session | Delete user's note |

---

## Feature Details

### 1. Sets and Scanner
- **Set** = immediate child directory of `MEDIA_ROOT`.
- Scanner walks each set **recursively**.
- `ffprobe` extracts duration, codec, resolution, bitrate.
- `os.Stat` gets `file_size_bytes`.
- `ffmpeg` generates thumbnail at ~10s (input-side seek `-ss`).
- Audio files render as **compact text rows** in UI (no thumbnail for single audio items).
- Sets display a **cover thumbnail** (`.cover.jpg` in set root) derived from a random video frame inside the set.

### 2. Auth
- `golang.org/x/crypto/bcrypt` for password hashing.
- HTTP-only cookie sessions stored in `sessions` table.
- **Bootstrap**: If `users` is empty, all routes redirect to `/bootstrap.html` until first admin is created.

### 3. Roles
- `set_permissions.role` = `owner` or `viewer`.
- **Owner/admin** can upload to that set.
- Admin sees all sets implicitly.

### 4. Playback
- Custom `<video>`/`<audio>` overlay with play/pause, seek bar, volume, mute, time, fullscreen toggle.
- **Fullscreen** (`f` key) uses Fullscreen API on the **wrapper container** so overlay controls (including progress bar) remain visible.
- **Auto-next**: On playback end, play next item from the current filtered/sorted/shuffled result set.

### 5. Playback Counter
- Client POSTs `position_seconds` every 3 seconds while playing.
- Server tracks `delta = new_pos - last_position`, clamped to `[0, 12]`.
- After cumulative `>= 60` seconds and `counted = false`, increments `media.play_count`.

### 6. Keyboard Navigation
- `↑↓` or `kj` — navigate media list.
- `←→` or `hl` — switch sets/pages.
- `Enter` — open selected media.
- `p` or `Space` — play/pause/switch to selected.
- `f` — toggle fullscreen.
- `Esc` — exit fullscreen or deselect.
- `r` — toggle shuffle on current filtered result set (lost on refresh).
- `s` — generate share link for selected media.
- `/` — trigger universal search input.

### 7. Selection
- `.selected` class highlights the focused media card.
- Keyboard focus and mouse click both update selection.
- Visually distinct from `.playing` (which shows a play indicator).

### 8. Filtering
- **Query params**: `set_id`, `type`, `search` (filename/substring), `tags` (AND logic, comma-separated), `favorites=true`, `min_duration`, `max_duration` (seconds).
- **Sort**: `name`, `date`, `duration`, `play_count`, `random`.
- **Search (`/`)**: Debounced 300ms. Searches filename, rel_path, codec, resolution, tags, notes, sets.

### 9. Upload
- `POST /api/sets/:id/upload` with `multipart/form-data`.
- Max file size: **100MB** (`MAX_UPLOAD_SIZE_MB=100`).
- If filename exists, append `(1)`, `(2)`, etc.
- After save, immediate `ffprobe` + `ffmpeg` thumbnail + insert into `media`.

### 10. Share Links
- `POST /api/media/:id/shares` generates a new random token.
- Default expiration: **7 days** from now.
- Public routes `/s/:token` and `/s/:token/stream` bypass auth.
- Each time `s` is pressed, a **new share** is created (old ones remain valid until expiry).

### 11. Soft Delete + GC
- `DELETE /api/media/:id` sets `deleted_at = NOW()`.
- Media hidden from normal views; shown in admin trash view.
- Admin/owner can restore before 7 days.
- Background goroutine (`time.Ticker`, default every 30 minutes) selects items where `deleted_at < NOW() - 7 days`.
- Physical file deleted via `os.Remove()`, then **hard DELETE** from DB row.

### 12. Thumbnail Regeneration
- `POST /api/media/:id/thumbnail` or set cover regeneration.
- Random offset `rand.Float64() * duration`; `ffmpeg -ss` input-side seek.
- Overwrites existing file.

### 13. Per-Media Notes
- Private **per-user** notes via `media_notes` table.
- UI: notes icon on media detail opens textarea modal.
- Auto-save or explicit save button.

### 14. Theming + Dark Mode
- All colors defined in `css/theme.css` using `var(--*)`.
- No inline styles anywhere.
- Sun/moon toggle in header swaps `<link>` tag to `light.css`.
- Zero color literals in component styles.

### 15. PWA
- `manifest.json` for installable app.
- Service Worker caches static assets for offline.
- App works as standalone install on mobile/desktop.

---

## Testability Requirements

- **Dependencies injected via constructors** (no package-level vars).
- **Interfaces at all boundaries**: DB, FS, `ffprobe`, `ffmpeg`, bcrypt, clock, sessions.
- **Heavy unit tests** with `:memory:` SQLite for repository layer.
- **Mock-based tests** for service layer using hand-written fakes.
- **HTTP tests** with `httptest` + mocked services for handlers.
- **Target:** >80% coverage via `go test ./... -race -coverprofile=cov.out`.

---

## Kubernetes Deployment

- **Image**: Multi-stage Dockerfile; final stage based on `alpine` with `apk add ffmpeg`.
- **Security**: non-root user (`65534:65534`), `runAsNonRoot: true`, `fsGroup: 65534`.
- **PVCs**: Separate volumes for `/data` (SQLite) and `/media` (library).
- **Probes**: `/healthz` (liveness, no DB), `/readyz` (readiness, DB ping).
- **Graceful shutdown**: Handle `SIGTERM`, close DB, finish active requests.

---

## Environment Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | HTTP listen port |
| `MEDIA_ROOT` | `./media` | Root path for set directories |
| `DB_PATH` | `data.db` | SQLite database file |
| `MAX_UPLOAD_SIZE_MB` | `100` | Max upload size per file |
| `SESSION_TIMEOUT_HOURS` | `24` | Cookie expiry |
| `GC_INTERVAL_MINUTES` | `30` | Garbage collector tick |
| `SHARE_DEFAULT_EXPIRY_DAYS` | `7` | Default share link lifetime |
| `LOG_LEVEL` | `info` | Log verbosity |

---

## Magefile Targets

- `mage` (default) → `mage build`
- `mage build`
- `mage test`
- `mage install`
- `mage clean`
- `mage docker-build`
- `mage docker-push`

---

## Completion Criteria

All 31 features implemented, tested, and deployable:
1. Sets & Scanner
2. SQLite DB
3. Auth & Sessions
4. Admin Panel
5. Playback (with fullscreen overlay)
6. Download
7. Tagging
8. Playback Counter (>60s)
9. Favorites
10. Progress Tracking
11. Soft Delete
12. GC Worker
13. Thumbnail Regeneration
14. Filtering (set/type/filename/tags/favorites/duration/filesize)
15. File Size Display
16. PWA
17. Theming (CSS variables)
18. Dark Mode Toggle
19. Upload (500MB, owner/admin)
20. Share Links (s key, 7 days)
21. Shuffle (r key, filtered scope)
22. Keyboard Navigation
23. Selection Highlight
24. Auto-Next Playback
25. Admin Bootstrap (`bootstrap.html`)
26. Set Cover Thumbnail
27. Audio/Podcast/Audiobook Text Rows
28. Audiobooks (audio tag)
29. Per-Media Notes (private per-user)
30. Quick Search ( `/` key)
31. Local Mode & K8s Deployability

