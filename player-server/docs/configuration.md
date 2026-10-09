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
