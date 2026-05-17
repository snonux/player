package internal

import (
	"os"
	"testing"
)

func TestLoadConfig_Defaults(t *testing.T) {
	// Ensure env vars are not set.
	clearEnv()

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != DefaultPort {
		t.Errorf("Port: expected %d, got %d", DefaultPort, cfg.Port)
	}
	if cfg.MediaRoot != DefaultMediaRoot {
		t.Errorf("MediaRoot: expected %q, got %q", DefaultMediaRoot, cfg.MediaRoot)
	}
	if cfg.DBPath != DefaultDBPath {
		t.Errorf("DBPath: expected %q, got %q", DefaultDBPath, cfg.DBPath)
	}
	if cfg.MaxUploadSizeMB != DefaultMaxUploadSizeMB {
		t.Errorf("MaxUploadSizeMB: expected %d, got %d", DefaultMaxUploadSizeMB, cfg.MaxUploadSizeMB)
	}
	if cfg.SessionTimeoutHours != DefaultSessionTimeoutHours {
		t.Errorf("SessionTimeoutHours: expected %d, got %d", DefaultSessionTimeoutHours, cfg.SessionTimeoutHours)
	}
	if cfg.GCIntervalMinutes != DefaultGCIntervalMinutes {
		t.Errorf("GCIntervalMinutes: expected %d, got %d", DefaultGCIntervalMinutes, cfg.GCIntervalMinutes)
	}
	if cfg.ShareDefaultExpiryDays != DefaultShareDefaultExpiryDays {
		t.Errorf("ShareDefaultExpiryDays: expected %d, got %d", DefaultShareDefaultExpiryDays, cfg.ShareDefaultExpiryDays)
	}
	if cfg.MediaPageSize != DefaultMediaPageSize {
		t.Errorf("MediaPageSize: expected %d, got %d", DefaultMediaPageSize, cfg.MediaPageSize)
	}
	if cfg.LogLevel != DefaultLogLevel {
		t.Errorf("LogLevel: expected %q, got %q", DefaultLogLevel, cfg.LogLevel)
	}
	if cfg.SecureCookies != DefaultSecureCookies {
		t.Errorf("SecureCookies: expected %v, got %v", DefaultSecureCookies, cfg.SecureCookies)
	}
}

func TestLoadConfig_EnvOverrides(t *testing.T) {
	clearEnv()
	setEnvPairs(t, []envPair{
		{"PORT", "9090"},
		{"MEDIA_ROOT", "/tmp/media"},
		{"DB_PATH", "/tmp/db.sqlite"},
		{"MAX_UPLOAD_SIZE_MB", "500"},
		{"SESSION_TIMEOUT_HOURS", "48"},
		{"GC_INTERVAL_MINUTES", "60"},
		{"SHARE_DEFAULT_EXPIRY_DAYS", "14"},
		{"MEDIA_PAGE_SIZE", "25"},
		{"LOG_LEVEL", "debug"},
		{"SECURE_COOKIES", "false"},
	})

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 9090 {
		t.Errorf("Port: expected 9090, got %d", cfg.Port)
	}
	if cfg.MediaRoot != "/tmp/media" {
		t.Errorf("MediaRoot: expected %q, got %q", "/tmp/media", cfg.MediaRoot)
	}
	if cfg.DBPath != "/tmp/db.sqlite" {
		t.Errorf("DBPath: expected %q, got %q", "/tmp/db.sqlite", cfg.DBPath)
	}
	if cfg.MaxUploadSizeMB != 500 {
		t.Errorf("MaxUploadSizeMB: expected 500, got %d", cfg.MaxUploadSizeMB)
	}
	if cfg.SessionTimeoutHours != 48 {
		t.Errorf("SessionTimeoutHours: expected 48, got %d", cfg.SessionTimeoutHours)
	}
	if cfg.GCIntervalMinutes != 60 {
		t.Errorf("GCIntervalMinutes: expected 60, got %d", cfg.GCIntervalMinutes)
	}
	if cfg.ShareDefaultExpiryDays != 14 {
		t.Errorf("ShareDefaultExpiryDays: expected 14, got %d", cfg.ShareDefaultExpiryDays)
	}
	if cfg.MediaPageSize != 25 {
		t.Errorf("MediaPageSize: expected 25, got %d", cfg.MediaPageSize)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel: expected %q, got %q", "debug", cfg.LogLevel)
	}
	if cfg.SecureCookies != false {
		t.Errorf("SecureCookies: expected false, got %v", cfg.SecureCookies)
	}
}

func TestLoadConfig_InvalidValues(t *testing.T) {
	cases := []struct {
		name    string
		env     []envPair
		wantErr string
	}{
		{
			name:    "invalid PORT",
			env:     []envPair{{"PORT", "abc"}},
			wantErr: "invalid PORT",
		},
		{
			name:    "PORT negative",
			env:     []envPair{{"PORT", "-1"}},
			wantErr: "invalid PORT",
		},
		{
			name:    "PORT out of range (70000)",
			env:     []envPair{{"PORT", "70000"}},
			wantErr: "invalid PORT",
		},
		{
			name:    "invalid MAX_UPLOAD_SIZE_MB",
			env:     []envPair{{"MAX_UPLOAD_SIZE_MB", "-1"}},
			wantErr: "invalid MAX_UPLOAD_SIZE_MB",
		},
		{
			name:    "invalid SESSION_TIMEOUT_HOURS",
			env:     []envPair{{"SESSION_TIMEOUT_HOURS", "0"}},
			wantErr: "invalid SESSION_TIMEOUT_HOURS",
		},
		{
			name:    "invalid GC_INTERVAL_MINUTES",
			env:     []envPair{{"GC_INTERVAL_MINUTES", "-5"}},
			wantErr: "invalid GC_INTERVAL_MINUTES",
		},
		{
			name:    "invalid SHARE_DEFAULT_EXPIRY_DAYS",
			env:     []envPair{{"SHARE_DEFAULT_EXPIRY_DAYS", "0"}},
			wantErr: "invalid SHARE_DEFAULT_EXPIRY_DAYS",
		},
		{
			name:    "invalid MEDIA_PAGE_SIZE",
			env:     []envPair{{"MEDIA_PAGE_SIZE", "0"}},
			wantErr: "invalid MEDIA_PAGE_SIZE",
		},
		{
			name:    "invalid LOG_LEVEL",
			env:     []envPair{{"LOG_LEVEL", "trace"}},
			wantErr: "invalid LOG_LEVEL",
		},
		{
			name:    "invalid SECURE_COOKIES",
			env:     []envPair{{"SECURE_COOKIES", "maybe"}},
			wantErr: "invalid SECURE_COOKIES",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv()
			setEnvPairs(t, tc.env)

			_, err := LoadConfig()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

type envPair struct {
	key   string
	value string
}

func clearEnv() {
	for _, k := range []string{
		"PORT", "MEDIA_ROOT", "DB_PATH",
		"MAX_UPLOAD_SIZE_MB", "SESSION_TIMEOUT_HOURS",
		"GC_INTERVAL_MINUTES", "SHARE_DEFAULT_EXPIRY_DAYS",
		"PODCAST_CHECK_INTERVAL_MINUTES", "MEDIA_PAGE_SIZE",
		"LOG_LEVEL", "SECURE_COOKIES",
	} {
		os.Unsetenv(k)
	}
}

func setEnvPairs(t *testing.T, pairs []envPair) {
	for _, p := range pairs {
		if err := os.Setenv(p.key, p.value); err != nil {
			t.Fatalf("setenv %s: %v", p.key, err)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSearch(s, substr))
}

func containsSearch(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
