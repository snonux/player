package internal

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Default configuration values.
const (
	DefaultPort                   = 8080
	DefaultMediaRoot              = "./media"
	DefaultDBPath                 = "data.db"
	DefaultMaxUploadSizeMB        = 100
	DefaultSessionTimeoutHours    = 24
	DefaultGCIntervalMinutes      = 30
	DefaultShareDefaultExpiryDays = 7
	DefaultPodcastCheckMinutes    = 60
	DefaultMediaPageSize          = 100
	DefaultLogLevel               = "info"
	DefaultSecureCookies          = true
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
	}
}

// loadNumericSettings reads numeric settings from the environment.
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

	if err := envInt("MAX_UPLOAD_SIZE_MB", func(n int) error {
		if n < 1 {
			return fmt.Errorf("must be >= 1, got %d", n)
		}
		return nil
	}, func(n int) { cfg.MaxUploadSizeMB = n }); err != nil {
		return err
	}

	if err := envInt("SESSION_TIMEOUT_HOURS", func(n int) error {
		if n < 1 {
			return fmt.Errorf("must be >= 1, got %d", n)
		}
		return nil
	}, func(n int) { cfg.SessionTimeoutHours = n }); err != nil {
		return err
	}

	if err := envInt("GC_INTERVAL_MINUTES", func(n int) error {
		if n < 1 {
			return fmt.Errorf("must be >= 1, got %d", n)
		}
		return nil
	}, func(n int) { cfg.GCIntervalMinutes = n }); err != nil {
		return err
	}

	if err := envInt("SHARE_DEFAULT_EXPIRY_DAYS", func(n int) error {
		if n < 1 {
			return fmt.Errorf("must be >= 1, got %d", n)
		}
		return nil
	}, func(n int) { cfg.ShareDefaultExpiryDays = n }); err != nil {
		return err
	}

	if err := envInt("PODCAST_CHECK_INTERVAL_MINUTES", func(n int) error {
		if n < 1 {
			return fmt.Errorf("must be >= 1, got %d", n)
		}
		return nil
	}, func(n int) { cfg.PodcastCheckMinutes = n }); err != nil {
		return err
	}

	if err := envInt("MEDIA_PAGE_SIZE", func(n int) error {
		if n < 1 {
			return fmt.Errorf("must be >= 1, got %d", n)
		}
		return nil
	}, func(n int) { cfg.MediaPageSize = n }); err != nil {
		return err
	}

	return nil
}

// loadStringSettings reads MEDIA_ROOT and DB_PATH from the environment.
func loadStringSettings(cfg *Config) {
	envString("MEDIA_ROOT", func(s string) { cfg.MediaRoot = s })
	envString("DB_PATH", func(s string) { cfg.DBPath = s })
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

	return cfg, nil
}
