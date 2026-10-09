package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	// DefaultTranscodeCacheMaxMB bounds the transcode cache at 4 GiB.
	DefaultTranscodeCacheMaxMB = 4096
	// DefaultTranscodeMaxJobs is the number of parallel ffmpeg transcodes.
	// One job needs roughly 400 MB of memory at 1080p, so more jobs need a
	// larger container memory limit.
	DefaultTranscodeMaxJobs = 1
)

// Config holds all application configuration loaded from environment variables.
type Config struct {
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
	// TranscodeCacheMaxMB is the size the cache is pruned back to.
	TranscodeCacheMaxMB int
	// TranscodeMaxJobs is the number of ffmpeg transcodes run in parallel.
	TranscodeMaxJobs int
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
	return nil
}

// loadStringSettings reads MEDIA_ROOT, DB_PATH and TRANSCODE_CACHE_DIR from
// the environment. The transcode cache default is derived from the final
// DB_PATH, so it must be resolved after DB_PATH has been read.
func loadStringSettings(cfg *Config) {
	envString("MEDIA_ROOT", func(s string) { cfg.MediaRoot = s })
	envString("DB_PATH", func(s string) { cfg.DBPath = s })
	cfg.TranscodeCacheDir = cfg.DBPath + DefaultTranscodeCacheDirSuffix
	envString("TRANSCODE_CACHE_DIR", func(s string) { cfg.TranscodeCacheDir = s })
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

	if err := validateTranscodeCacheDir(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// validateTranscodeCacheDir rejects a transcode cache inside (or equal to)
// the media root. There the library scanner would import the renditions as
// media, and cache eviction would run in a directory holding real media.
// This also applies to the derived default, i.e. when DB_PATH itself lies
// in the media root: set TRANSCODE_CACHE_DIR explicitly in that case.
func validateTranscodeCacheDir(cfg *Config) error {
	cache, err := filepath.Abs(cfg.TranscodeCacheDir)
	if err != nil {
		return fmt.Errorf("invalid TRANSCODE_CACHE_DIR: %w", err)
	}
	root, err := filepath.Abs(cfg.MediaRoot)
	if err != nil {
		return fmt.Errorf("invalid MEDIA_ROOT: %w", err)
	}
	if cache == root || strings.HasPrefix(cache, root+string(filepath.Separator)) {
		return fmt.Errorf("invalid TRANSCODE_CACHE_DIR: %q must not be inside MEDIA_ROOT %q", cfg.TranscodeCacheDir, cfg.MediaRoot)
	}
	return nil
}
