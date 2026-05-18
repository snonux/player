# Test Coverage Audit

Generated: 2026-05-18  
Tool: `go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out`  
Total coverage: **73.6%** (target: 60% per project style guide)

---

## 1. Per-Package Coverage Table

| Package | Coverage | Notes |
|---------|----------|-------|
| `cmd/player` | 87.8% | Entry-point wiring; `main` and `recoverBackgroundWorkerPanic` untested (normal for process-level code) |
| `internal` (config) | 94.1% | `loadNumericSettings` partially covered (88.4%) |
| `internal/api` | 78.8% | Several handler paths untested (share thumbnail, remuxed stream, browse-set) |
| `internal/auth` | 88.0% | Good; minor gaps in `generateID` and `CreateSession` error paths |
| `internal/clock` | 100.0% | Full |
| `internal/mediatype` | 42.5% | `MIMETypeForExt` almost entirely untested (11.5%); only a small subset of MIME types exercised |
| `internal/model` | 100.0% | Full |
| `internal/podcast` | 98.4% | Only `ParseFeed` HTTP-error path missing |
| `internal/probe` | 88.7% | `Probe` retry path and `Remux` context-cancellation edge cases are covered; image-EXIF path well tested |
| `internal/repository` | 59.6% | Lowest non-trivial package; `progress_transaction.go` is 0%, several podcast repo methods 0% |
| `internal/scanner` | 73.7% | `NewFSScanner` / `os.FS` adapter 0%; `thumbnailForImage`, `buildThumbnailPath` largely untested |
| `internal/service` | 75.5% | `Bootstrap`, `Login`, `ListTags`, `ToggleEpisodeComplete`, podcast sub-service lifecycle methods all 0% |
| `internal/setassign` | (no stmts) | Package contains no executable statements; only type definitions |
| `internal/thumb` | 94.4% | Near-full; only FFmpeg error path in `Generate` slightly below 100% |

---

## 2. Coverage Gap List

The gaps are grouped by severity (how much untested logic affects correctness risk).

### 2.1 Completely Untested Code Paths

These functions have 0% statement coverage and contain real logic (excluding mock scaffolding files which are 0% by design).

#### `internal/repository/progress_transaction.go` — entire file (0%)

`WithProgressTransaction`, `UpsertProgress`, `GetProgress`, `GetAccumulator`, `UpsertAccumulator`, `IncrementPlayCount` — the transactional progress wrapper is never exercised by any test. This is the code path used by the batch-progress offline sync endpoint, making it a higher-risk gap.

#### `internal/repository/podcast.go` — several methods (0%)

- `UpdateFeed` — feed metadata refresh path
- `ListFeedsBySetID` — listing feeds for a given media set
- `ListFeeds` — global feed listing
- `ListEpisodesByFeed` — episode listing

#### `internal/repository/media.go`

- `UpdateMediaThumbnail` — thumbnail path update after regeneration

#### `internal/repository/share.go`

- `ListSharesByUser` — user-owned shares listing

#### `internal/scanner/fs.go` — entire file (0%)

`ReadDir`, `Stat`, `MkdirAll`, `WalkDir` — the `osFS` adapter wrapping real OS calls. The scanner tests inject a `mockFS` instead, so the production adapter is never exercised.

#### `internal/scanner/scanner.go`

- `NewFSScanner` / `NewFSScannerWithLogger` — real constructors (0%); tests use `mockFS` directly
- `thumbnailForImage` — image thumbnail generation during scan
- `buildThumbnailPath` (21.4%) — path logic for thumbnail placement; most branches not covered

#### `internal/service/auth.go`

- `Bootstrap` (0%) — first-user creation flow
- `Login` (0%) — password-based login
- `CountUsers` (0%), `GetUserByID` (0%) — thin delegation wrappers

#### `internal/service/playback_hints.go`

- `NewPlaybackHintsService` (0%), `GetPlaybackHint` (0%) — the service constructor and its only public method; only the pure helper functions (`buildPlaybackHint`, `containerFromPath`, etc.) are tested in isolation

#### `internal/service/podcast_sub.go`

- `ListFeeds` (0%), `EditFeed` (0%), `UnsubscribeFeed` (0%) — podcast subscription lifecycle beyond the initial subscribe path
- `rollbackSet` (0%) — error-cleanup path

#### `internal/service/podcast_episode.go`

- `ToggleEpisodeComplete` (0%)

#### `internal/service/tag.go`

- `ListTags` (0%)

#### `internal/service/write.go`

- `copyFile` (0%) — file copy used during media import

#### `internal/api/handlers.go`

- `serveDetach` (0%) — JS-detach response handler
- `serveRemuxed` (0%) — remuxed stream handler

#### `internal/api/handlers_media.go`

- `handleGetSetCover` (0%), `handleBrowseSet` (0%), `handleListTags` (0%)

#### `internal/api/handlers_share.go`

- `handleShareThumbnail` (0%), `handleShareDownload` (0%), `handleMyShares` (0%)

---

### 2.2 Undertested Code Paths (Below 60%)

| Location | Coverage | Gap description |
|----------|----------|-----------------|
| `mediatype.MIMETypeForExt` | 11.5% | Large switch with ~50 MIME mappings; only a handful are exercised |
| `api.handleToggleComplete` | 29.4% | Podcast episode toggle — only error branch tested |
| `service.podcast_sub.findPodcastSet` | 33.3% | Set-lookup branching logic |
| `scanner.updateAudioThumbnails` | 42.9% | Audio cover-art update during scan |
| `service.podcast_checker.checkFeed` | 43.6% | Per-feed refresh including HTTP-error and parse-error paths |
| `scanner.gatherCoverImages` | 50.0% | Cover image collection from directories |
| `service.podcast_sub.findExistingFeed` | 50.0% | Duplicate-feed detection |
| `api.handleListPodcasts` | 50.0% | Error path not covered |
| `api.handleSubscribePodcast` | 52.4% | Multiple error branches untested |
| `service.browse.GetThumbnail` | 53.8% | Thumbnail lookup fallback paths |
| `api.handleDownloadEpisode` | 55.6% | Episode download error cases |
| `service.podcast_episode.persistDownloadedEpisode` | 55.6% | Post-download persistence error paths |
| `api.handleListAPITokens` | 57.1% | Token listing edge cases |
| `service.podcast_sub.SubscribeFeed` | 68.8% | Error + rollback paths |

---

### 2.3 Partially Covered but Noteworthy

| Location | Coverage | Gap |
|----------|----------|-----|
| `repository.sqlite.Open` | 68.8% | WAL mode and migration failure paths |
| `service.auth.NewAuthService` | 66.7% | Nil-session-manager branch |
| `service.user.DeleteUser` | 66.7% | Cascade logic |
| `service.progress.MarkNotStarted` | 66.7% | Not-found path |
| `service.gc.notifyRunDone` | 66.7% | Channel-closed path |
| `service.streamer.Open` | 71.4% | Error-handling branches |
| `api.handleError` | 71.4% | The `ErrForbidden` and context-cancelled branches |

---

## 3. Reusable Test Helpers

The following helpers are defined in `*_test.go` files and are relied upon by multiple tests within their package. Later tasks adding tests should reuse them rather than duplicating setup code.

### 3.1 `internal/repository` package

**`newTestStore(t *testing.T) *SQLite`** — `sqlite_test.go:12`  
Opens an in-memory SQLite database and runs all migrations. The canonical way to obtain a disposable repository in any repository-package test. Used by every test in the package.

**`mustCreateAPITokenUser(t, ctx, s, username) int64`** — `api_token_test.go:94`  
Creates a user row and returns the ID, calling `t.Fatal` on error. A convenience shortcut for tests that need a valid user before creating tokens.

**`assertAPIToken(t, token, id, userID, hash, name, lastUsedAt, expiresAt, createdAt)`** — `api_token_test.go:107`  
Field-by-field assertion for an `*model.APIToken`. Reuse whenever a test needs to verify token state after a repository operation.

**`assertTimePtr(t, got, want *time.Time, field string)`** — `api_token_test.go:128`  
Nil-safe pointer comparison for `*time.Time` fields. Used by `assertAPIToken`.

### 3.2 `internal/api` package

**`newTestServer(t, store, hasher, sm, cfg, browseSvc, writeSvc, shareSvc, tagSvc, favSvc, noteSvc, adminSvc, progressSvc, authSvc, fs, streamer...) *Server`** — `handlers_test.go:36`  
Wires up a full `*Server` with all dependencies. Any `nil` argument gets a sensible default (in-memory FS for static files, a stub `MockAuthService` with one admin user). This is the primary entry point for all HTTP-level handler tests.

**`newTestFS(files map[string]string) http.FileSystem`** — `handlers_test.go:28`  
Builds a `testing/fstest.MapFS`-backed `http.FileSystem` from a string map. Used to provide a fake static-file tree without touching the disk.

**`assertCORSHeaders(t, h http.Header, origin string)`** — `cors_test.go:93`  
Checks that all expected CORS response headers are present for a given origin.

### 3.3 `internal/service` package

**`newMockClock() *clock.MockClock`** — `media_test.go:18`  
Returns a `MockClock` fixed at a stable reference time. Used by every service test that involves time-sensitive logic (progress accumulation, GC age, session expiry).

**`setupPodcastService(t) (*podcastService, *repository.MockStore)`** — `podcast_test.go:23`  
Builds a fully wired `podcastService` with a `MockStore`, a temp media root, a discarding `slog.Logger`, and a fixed clock. The canonical starting point for podcast-service tests.

### 3.4 `internal/scanner` package

**`mockDirEntry` / `mockFileInfo`** — `scanner_test.go:22-51`  
Lightweight implementations of `os.DirEntry` and `os.FileInfo` for injecting directory listings into `mockFS`.

**`mockFS`** — `scanner_test.go:59+`  
Implements the `FS` interface (the production `osFS` abstraction). Populated with `entries`, `fileInfos`, and a `walkList`; supports injecting `walkErr`. Used by all scanner unit tests instead of hitting the real filesystem.

### 3.5 `internal/probe` package

**`installFakeFFmpeg(t *testing.T, body string)`** — `remux_test.go:76`  
Writes a shell-script fake `ffmpeg` binary to a temp directory and prepends it to `PATH` for the duration of the test. Allows testing `FFRemuxer` behavior (cancellation, write errors, wait errors) without a real FFmpeg installation.

---

## 4. Test Patterns Observed

### Table-driven tests
The dominant pattern throughout the codebase. Subtests are defined as `[]struct{ name string; ... }` slices and iterated with `for _, tc := range tests { t.Run(tc.name, ...) }`. Examples: `TestProgressService_UpdateProgress` (`service/progress_test.go`), `TestBrowseService_BrowseSet` (`service/browse_test.go`), `TestParseFFprobeOutput` (`probe/probe_test.go`), `TestSQLite_UserRepo` (`repository/sqlite_test.go`).

### Struct-based fakes (not mocks)
`repository.MockStore`, `service.MockMediaService`, and `auth.MockSessionManager` are hand-written structs with optional `*Func` fields of matching function type. When the `*Func` field is nil the method returns a zero value; when set it delegates to the function. This allows per-test customisation without a mocking framework. See `internal/repository/mock.go`, `internal/service/mock.go`, `internal/auth/mock.go`.

These mocks are separately validated by their own tests (`mock_test.go` files in each package) to document zero-value behaviour and verify the `WithFuncs` wiring.

### In-memory SQLite for repository integration tests
All repository tests open an `":memory:"` database via `newTestStore`, run full schema migrations, and exercise real SQL. There is no mocking at the SQL layer; this gives high confidence in queries, scan functions, and constraint behaviour.

### `t.TempDir()` for filesystem tests
Service and scanner tests that need real files (podcast download, thumbnail generation, media upload) use `t.TempDir()` which is automatically cleaned up. They do not patch global paths.

### Fake binary injection via PATH
`installFakeFFmpeg` writes a shell script as `ffmpeg` to a temp dir and prepends it to `PATH`. The same pattern could be applied to `ffprobe` for probe tests that need deterministic binary output.

### No-rows / nil-result tables
`repository/sqlite_no_rows_test.go` and `service/no_rows_test.go` are dedicated files that verify every "get by ID" and similar method returns `nil, nil` (not an error) when the row does not exist. This is a contract test separating the "not found" semantic from the "DB error" semantic.

### HTTP handler tests via `httptest.NewRecorder`
API tests construct a `*Server` via `newTestServer`, call `ServeHTTP` directly with an `httptest.ResponseRecorder`, and assert on status codes and JSON bodies. Dependencies are injected as `MockMediaService`, `MockAdminService`, etc. with `*Func` fields set to return canned responses.
