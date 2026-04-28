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
	DefaultLogLevel               = "info"
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
	LogLevel               string
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

// LoadConfig reads configuration from environment variables and returns
// a populated Config. Unset variables use the package defaults.
func LoadConfig() (*Config, error) {
	cfg := &Config{
		Port:                   DefaultPort,
		MediaRoot:              DefaultMediaRoot,
		DBPath:                 DefaultDBPath,
		MaxUploadSizeMB:        DefaultMaxUploadSizeMB,
		SessionTimeoutHours:    DefaultSessionTimeoutHours,
		GCIntervalMinutes:      DefaultGCIntervalMinutes,
		ShareDefaultExpiryDays: DefaultShareDefaultExpiryDays,
		LogLevel:               DefaultLogLevel,
	}

	validLevels := map[string]struct{}{
		"debug": {},
		"info":  {},
		"warn":  {},
		"error": {},
	}

	if err := envInt("PORT", func(n int) error {
		if n < 1 || n > 65535 {
			return fmt.Errorf("must be between 1 and 65535, got %d", n)
		}
		return nil
	}, func(n int) { cfg.Port = n }); err != nil {
		return nil, err
	}

	envString("MEDIA_ROOT", func(s string) { cfg.MediaRoot = s })
	envString("DB_PATH", func(s string) { cfg.DBPath = s })

	if err := envInt("MAX_UPLOAD_SIZE_MB", func(n int) error {
		if n < 1 {
			return fmt.Errorf("must be >= 1, got %d", n)
		}
		return nil
	}, func(n int) { cfg.MaxUploadSizeMB = n }); err != nil {
		return nil, err
	}

	if err := envInt("SESSION_TIMEOUT_HOURS", func(n int) error {
		if n < 1 {
			return fmt.Errorf("must be >= 1, got %d", n)
		}
		return nil
	}, func(n int) { cfg.SessionTimeoutHours = n }); err != nil {
		return nil, err
	}

	if err := envInt("GC_INTERVAL_MINUTES", func(n int) error {
		if n < 1 {
			return fmt.Errorf("must be >= 1, got %d", n)
		}
		return nil
	}, func(n int) { cfg.GCIntervalMinutes = n }); err != nil {
		return nil, err
	}

	if err := envInt("SHARE_DEFAULT_EXPIRY_DAYS", func(n int) error {
		if n < 1 {
			return fmt.Errorf("must be >= 1, got %d", n)
		}
		return nil
	}, func(n int) { cfg.ShareDefaultExpiryDays = n }); err != nil {
		return nil, err
	}

	if v := os.Getenv("LOG_LEVEL"); v != "" {
		level := strings.ToLower(strings.TrimSpace(v))
		if _, ok := validLevels[level]; !ok {
			return nil, fmt.Errorf("invalid LOG_LEVEL: must be one of debug, info, warn, error, got %q", level)
		}
		cfg.LogLevel = level
	}

	return cfg, nil
}
