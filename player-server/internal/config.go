package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/snonux/player/internal/auth"
)

// Default configuration values.
const (
	DefaultPort      = 8080
	DefaultMediaRoot = "./media"
	DefaultDBPath    = "data.db"
	// DefaultMaxUploadSizeMB is 10 GB, chosen to accommodate large video files
	// without imposing an arbitrary low cap. Operators can lower it via the
	// MAX_UPLOAD_SIZE_MB environment variable for tighter constraints.
	DefaultMaxUploadSizeMB        = 10240
	DefaultSessionTimeoutHours    = 24
	DefaultGCIntervalMinutes      = 30
	DefaultShareDefaultExpiryDays = 7
	DefaultPodcastCheckMinutes    = 60
	DefaultMediaPageSize          = 100
	DefaultLogLevel               = "info"
	DefaultSecureCookies          = true
	// DefaultTranscodeCacheDirSuffix is appended to the database path to
	// form the cache directory when TRANSCODE_CACHE_DIR is unset
	// ("/data/media.db" -> "/data/media.db.transcode-cache"). The database
	// already lives on a writable volume (the container root filesystem is
	// read-only), and tying the name to the database file gives every
	// instance its own cache: renditions are named by media id, so two
	// databases sharing one directory (several dev or test instances with
	// DB_PATH=/tmp/*.db) would delete each other's files.
	DefaultTranscodeCacheDirSuffix = ".transcode-cache"
	// There is deliberately no default thumbnail cache directory: with
	// THUMBNAIL_CACHE_DIR unset, thumbnails stay in the .thumbnails
	// directory beside each media file, as they always did.

	// DefaultTranscodeCacheMaxMB bounds the transcode cache at 4 GiB.
	DefaultTranscodeCacheMaxMB = 4096
	// DefaultTranscodeMaxJobs is the number of parallel ffmpeg transcodes.
	// One job needs roughly 400 MB of memory at 1080p, so more jobs need a
	// larger container memory limit.
	DefaultTranscodeMaxJobs = 1
)

// Config holds all application configuration loaded from environment variables.
type Config struct {
	// ScanWatchSeconds enables source-metadata polling; zero disables it.
	ScanWatchSeconds int
	// ScanIntervalMinutes enables periodic full rescans; zero disables it.
	ScanIntervalMinutes int
	// MusicEnrichmentURL is an optional operator-configured external metadata agent.
	MusicEnrichmentURL string
	// PluginDir is an optional directory of installed WASM plugins
	// (each subdirectory has manifest.json + entry wasm). Empty disables
	// discovery; music playback never requires plugins.
	PluginDir string
	// SubsonicEnabled turns on the optional Subsonic/OpenSubsonic /rest
	// compatibility surface (A01a). Default false; Player custom APIs stay intact.
	SubsonicEnabled bool
	// JukeboxEnabled turns on optional A02a server-host audio (admin control).
	// Default false; ordinary client streaming never requires it.
	JukeboxEnabled bool
	// JukeboxMPVPath is the mpv binary for the server-audio adapter. Empty uses "mpv".
	JukeboxMPVPath         string
	Port                   int
	MediaRoot              string
	DBPath                 string
	MaxUploadSizeMB        int
	SessionTimeoutHours    int
	GCIntervalMinutes      int
	ShareDefaultExpiryDays int
	PodcastCheckMinutes    int
	MediaPageSize          int
	LogLevel               string
	SecureCookies          bool
	CORSAllowedOrigins     []string
	// TranscodeCacheDir holds the compatibility renditions of legacy media
	// (AVI/WMV/FLV/WMA). Defaults to DBPath + ".transcode-cache".
	TranscodeCacheDir string
	// ThumbnailCacheDir, when set, takes all generated thumbnails instead
	// of the .thumbnails directory beside each media file, mirroring the
	// media tree. Empty (the default) keeps them beside the media. It lets
	// sets be read-only and thumbnails live on a volume of their own.
	ThumbnailCacheDir string
	// TranscodeCacheMaxMB is the size the cache is pruned back to.
	TranscodeCacheMaxMB int
	// TranscodeMaxJobs is the number of ffmpeg transcodes run in parallel.
	TranscodeMaxJobs int
	// ProxyAuth enables reverse-proxy authentication when non-nil. See
	// PROXY_AUTH_TRUSTED_SOURCES. Untrusted clients cannot spoof identity via
	// the user header; Bearer and session-cookie auth remain available.
	ProxyAuth *auth.ProxyAuth
}

// envInt reads an integer environment variable, validates it with the given check,
// and sets the field via the provided setter. If the variable is unset, the setter
// is not called and the default remains.
func envInt(name string, check func(int) error, set func(int)) error {
	if v := os.Getenv(name); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid %s: %w", name, err)
		}
		if err := check(n); err != nil {
			return fmt.Errorf("invalid %s: %w", name, err)
		}
		set(n)
	}
	return nil
}

// envString reads a string environment variable, trims space, and sets the field
// via the provided setter if the variable is non-empty.
func envString(name string, set func(string)) {
	if v := os.Getenv(name); v != "" {
		set(strings.TrimSpace(v))
	}
}

// validLogLevels contains the acceptable values for LOG_LEVEL.
var validLogLevels = map[string]struct{}{
	"debug": {},
	"info":  {},
	"warn":  {},
	"error": {},
}

// defaultConfig returns a Config populated with package-level defaults.
func defaultConfig() *Config {
	return &Config{
		Port:                   DefaultPort,
		MediaRoot:              DefaultMediaRoot,
		DBPath:                 DefaultDBPath,
		MaxUploadSizeMB:        DefaultMaxUploadSizeMB,
		SessionTimeoutHours:    DefaultSessionTimeoutHours,
		GCIntervalMinutes:      DefaultGCIntervalMinutes,
		ShareDefaultExpiryDays: DefaultShareDefaultExpiryDays,
		PodcastCheckMinutes:    DefaultPodcastCheckMinutes,
		MediaPageSize:          DefaultMediaPageSize,
		LogLevel:               DefaultLogLevel,
		SecureCookies:          DefaultSecureCookies,
		TranscodeCacheMaxMB:    DefaultTranscodeCacheMaxMB,
		TranscodeMaxJobs:       DefaultTranscodeMaxJobs,
	}
}

// atLeastOne validates settings that must be a positive integer.
func atLeastOne(n int) error {
	if n < 1 {
		return fmt.Errorf("must be >= 1, got %d", n)
	}
	return nil
}

// loadNumericSettings reads numeric settings from the environment. PORT has
// its own range; every other numeric setting only needs to be positive, so
// those are driven from one table instead of repeating the validation.
func loadNumericSettings(cfg *Config) error {
	if err := envInt("PORT", func(n int) error {
		// Allow 0 so tests can bind to an ephemeral port.
		if n < 0 || n > 65535 {
			return fmt.Errorf("must be between 0 and 65535, got %d", n)
		}
		return nil
	}, func(n int) { cfg.Port = n }); err != nil {
		return err
	}

	positive := []struct {
		name string
		set  func(int)
	}{
		{"MAX_UPLOAD_SIZE_MB", func(n int) { cfg.MaxUploadSizeMB = n }},
		{"SESSION_TIMEOUT_HOURS", func(n int) { cfg.SessionTimeoutHours = n }},
		{"GC_INTERVAL_MINUTES", func(n int) { cfg.GCIntervalMinutes = n }},
		{"SHARE_DEFAULT_EXPIRY_DAYS", func(n int) { cfg.ShareDefaultExpiryDays = n }},
		{"PODCAST_CHECK_INTERVAL_MINUTES", func(n int) { cfg.PodcastCheckMinutes = n }},
		{"MEDIA_PAGE_SIZE", func(n int) { cfg.MediaPageSize = n }},
		{"TRANSCODE_CACHE_MAX_MB", func(n int) { cfg.TranscodeCacheMaxMB = n }},
		{"TRANSCODE_MAX_JOBS", func(n int) { cfg.TranscodeMaxJobs = n }},
	}
	for _, p := range positive {
		if err := envInt(p.name, atLeastOne, p.set); err != nil {
			return err
		}
	}
	for _, p := range []struct {
		name string
		max  int
		set  func(int)
	}{
		{"SCAN_WATCH_SECONDS", 86400, func(n int) { cfg.ScanWatchSeconds = n }},
		{"SCAN_INTERVAL_MINUTES", 10080, func(n int) { cfg.ScanIntervalMinutes = n }},
	} {
		if err := envInt(p.name, func(n int) error {
			if n < 0 || n > p.max || (p.name == "SCAN_WATCH_SECONDS" && n > 0 && n < 10) {
				return fmt.Errorf("must be 0 (off) or in supported range up to %d", p.max)
			}
			return nil
		}, p.set); err != nil {
			return err
		}
	}
	return nil
}

// loadStringSettings reads MEDIA_ROOT, DB_PATH, TRANSCODE_CACHE_DIR and
// THUMBNAIL_CACHE_DIR from the environment. The transcode cache default is
// derived from the final DB_PATH, so it must be resolved after DB_PATH has
// been read. The thumbnail cache has no default (see Config).
func loadStringSettings(cfg *Config) {
	envString("MUSIC_ENRICHMENT_URL", func(s string) { cfg.MusicEnrichmentURL = s })
	envString("PLUGIN_DIR", func(s string) { cfg.PluginDir = s })
	envString("MEDIA_ROOT", func(s string) { cfg.MediaRoot = s })
	envString("DB_PATH", func(s string) { cfg.DBPath = s })
	cfg.TranscodeCacheDir = cfg.DBPath + DefaultTranscodeCacheDirSuffix
	envString("TRANSCODE_CACHE_DIR", func(s string) { cfg.TranscodeCacheDir = s })
	envString("THUMBNAIL_CACHE_DIR", func(s string) { cfg.ThumbnailCacheDir = s })
}

// loadLogLevel reads LOG_LEVEL from the environment and validates it.
func loadLogLevel(cfg *Config) error {
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		level := strings.ToLower(strings.TrimSpace(v))
		if _, ok := validLogLevels[level]; !ok {
			return fmt.Errorf("invalid LOG_LEVEL: must be one of debug, info, warn, error, got %q", level)
		}
		cfg.LogLevel = level
	}
	return nil
}

// loadSecureCookies reads SECURE_COOKIES from the environment.
func loadSecureCookies(cfg *Config) error {
	if v := os.Getenv("SECURE_COOKIES"); v != "" {
		b, err := strconv.ParseBool(strings.TrimSpace(v))
		if err != nil {
			return fmt.Errorf("invalid SECURE_COOKIES: %w", err)
		}
		cfg.SecureCookies = b
	}
	return nil
}

// loadSubsonicEnabled reads SUBSONIC_ENABLED from the environment.
func loadSubsonicEnabled(cfg *Config) error {
	if v := os.Getenv("SUBSONIC_ENABLED"); v != "" {
		b, err := strconv.ParseBool(strings.TrimSpace(v))
		if err != nil {
			return fmt.Errorf("invalid SUBSONIC_ENABLED: %w", err)
		}
		cfg.SubsonicEnabled = b
	}
	return nil
}

// loadJukeboxSettings reads JUKEBOX_ENABLED and JUKEBOX_MPV_PATH.
func loadJukeboxSettings(cfg *Config) error {
	if v := os.Getenv("JUKEBOX_ENABLED"); v != "" {
		b, err := strconv.ParseBool(strings.TrimSpace(v))
		if err != nil {
			return fmt.Errorf("invalid JUKEBOX_ENABLED: %w", err)
		}
		cfg.JukeboxEnabled = b
	}
	envString("JUKEBOX_MPV_PATH", func(s string) { cfg.JukeboxMPVPath = s })
	return nil
}

// loadCORSSettings reads comma-separated CORS origins from the environment.
func loadCORSSettings(cfg *Config) {
	if v := os.Getenv("PLAYER_CORS_ORIGINS"); v != "" {
		for _, origin := range strings.Split(v, ",") {
			origin = strings.TrimSpace(origin)
			if origin != "" {
				cfg.CORSAllowedOrigins = append(cfg.CORSAllowedOrigins, origin)
			}
		}
	}
}

// loadProxyAuthSettings reads reverse-proxy authentication settings.
func loadProxyAuthSettings(cfg *Config) error {
	pa, err := auth.ParseProxyAuth(
		os.Getenv("PROXY_AUTH_TRUSTED_SOURCES"),
		os.Getenv("PROXY_AUTH_USER_HEADER"),
		os.Getenv("PROXY_AUTH_LOGOUT_URL"),
	)
	if err != nil {
		return err
	}
	cfg.ProxyAuth = pa
	return nil
}

// LoadConfig reads configuration from environment variables and returns
// a populated Config. Unset variables use the package defaults.
func LoadConfig() (*Config, error) {
	cfg := defaultConfig()

	loadStringSettings(cfg)
	loadCORSSettings(cfg)

	if err := loadNumericSettings(cfg); err != nil {
		return nil, err
	}

	if err := loadLogLevel(cfg); err != nil {
		return nil, err
	}

	if err := loadSecureCookies(cfg); err != nil {
		return nil, err
	}

	if err := loadSubsonicEnabled(cfg); err != nil {
		return nil, err
	}

	if err := loadJukeboxSettings(cfg); err != nil {
		return nil, err
	}

	if err := loadProxyAuthSettings(cfg); err != nil {
		return nil, err
	}

	if err := validateCacheDirs(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// validateCacheDirs rejects a transcode or thumbnail cache inside (or equal
// to) the media root, a thumbnail cache that contains the media root, and
// the two caches sharing one directory.
//
// In the media root the library scanner would import renditions as media
// (and a thumbnail cache directory as a set), and cache eviction would run
// in a directory holding real media. For the transcode cache this also
// applies to the derived default, i.e. when DB_PATH itself lies in the
// media root: set TRANSCODE_CACHE_DIR explicitly in that case.
//
// The thumbnail cache mirrors the media tree and has its emptied folders
// removed, so it must not contain the media root either: every media
// folder would then count as a cache folder.
func validateCacheDirs(cfg *Config) error {
	if err := outsideMediaRoot("TRANSCODE_CACHE_DIR", cfg.TranscodeCacheDir, cfg.MediaRoot); err != nil {
		return err
	}
	if cfg.ThumbnailCacheDir == "" {
		return nil
	}
	if err := outsideMediaRoot("THUMBNAIL_CACHE_DIR", cfg.ThumbnailCacheDir, cfg.MediaRoot); err != nil {
		return err
	}
	// outsideMediaRoot has already resolved all three once without error.
	thumbs, _ := realPath(cfg.ThumbnailCacheDir)
	root, _ := realPath(cfg.MediaRoot)
	renditions, _ := realPath(cfg.TranscodeCacheDir)
	if isOrContains(thumbs, root) {
		return fmt.Errorf("invalid THUMBNAIL_CACHE_DIR: %s must not contain MEDIA_ROOT %s", resolvedAs(cfg.ThumbnailCacheDir, thumbs), resolvedAs(cfg.MediaRoot, root))
	}
	if thumbs == renditions {
		return fmt.Errorf("invalid THUMBNAIL_CACHE_DIR: %q is the transcode cache directory", cfg.ThumbnailCacheDir)
	}
	return nil
}

// outsideMediaRoot returns an error naming the setting when dir is the
// media root or lies inside it.
func outsideMediaRoot(setting, dir, mediaRoot string) error {
	cache, err := realPath(dir)
	if err != nil {
		return fmt.Errorf("invalid %s: %w", setting, err)
	}
	root, err := realPath(mediaRoot)
	if err != nil {
		return fmt.Errorf("invalid MEDIA_ROOT: %w", err)
	}
	if isOrContains(root, cache) {
		return fmt.Errorf("invalid %s: %s must not be inside MEDIA_ROOT %s", setting, resolvedAs(dir, cache), resolvedAs(mediaRoot, root))
	}
	return nil
}

// resolvedAs quotes a configured path for an error message, adding what it
// resolved to when that is not obvious from the value (a relative path, a
// symbolic link): the reason for a rejection is often only visible there.
func resolvedAs(configured, resolved string) string {
	if filepath.Clean(configured) == resolved {
		return fmt.Sprintf("%q", configured)
	}
	return fmt.Sprintf("%q (%s)", configured, resolved)
}

// realPath makes path absolute and resolves the symbolic links in it, so
// that a cache directory which merely looks separate from the media root
// (a link into it, or a linked media root) is recognised. The part of the
// path that does not exist yet (a cache created on first use) is kept as it
// is, below the resolved existing part.
//
// A symbolic link that cannot be resolved (dangling, or a loop) is an
// error: where it will lead once its target exists cannot be checked now,
// and it could be into the media root.
func realPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	rest := ""
	for dir := abs; ; dir = filepath.Dir(dir) {
		resolved, err := filepath.EvalSymlinks(dir)
		if err == nil {
			return filepath.Join(resolved, rest), nil
		}
		if info, statErr := os.Lstat(dir); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%q is a symbolic link that cannot be resolved: %w", dir, err)
		}
		if dir == filepath.Dir(dir) {
			return abs, nil // nothing of it resolves; compare lexically
		}
		rest = filepath.Join(filepath.Base(dir), rest)
	}
}

// isOrContains reports whether the absolute path is dir itself or lies
// below it. The file system root contains everything.
func isOrContains(dir, path string) bool {
	if dir == path {
		return true
	}
	prefix := strings.TrimSuffix(dir, string(filepath.Separator)) + string(filepath.Separator)
	return strings.HasPrefix(path, prefix)
}
