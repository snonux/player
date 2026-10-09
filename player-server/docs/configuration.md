Configuration
=============

All settings are environment variables. Unset variables use defaults.

| Variable | Default | Validation | Description |
|----------|---------|------------|-------------|
| `PORT` | `8080` | 0–65535 | HTTP listen port (0 = ephemeral, used in tests) |
| `MEDIA_ROOT` | `./media` | — | Root path for media set directories |
| `DB_PATH` | `data.db` | — | SQLite database file path |
| `MAX_UPLOAD_SIZE_MB` | `100` | ≥ 1 | Max upload size per file (MB) |
| `SESSION_TIMEOUT_HOURS` | `24` | ≥ 1 | Cookie / session expiry |
| `GC_INTERVAL_MINUTES` | `30` | ≥ 1 | Garbage collector tick interval |
| `SHARE_DEFAULT_EXPIRY_DAYS` | `7` | ≥ 1 | Default share link lifetime |
| `PODCAST_CHECK_INTERVAL_MINUTES` | `60` | ≥ 1 | Podcast feed refresh interval |
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
