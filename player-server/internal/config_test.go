package internal

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
	if len(cfg.CORSAllowedOrigins) != 0 {
		t.Errorf("CORSAllowedOrigins: expected empty, got %v", cfg.CORSAllowedOrigins)
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
		{"PLAYER_CORS_ORIGINS", " http://localhost:5173,https://player.example.com ,, "},
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
	wantOrigins := []string{"http://localhost:5173", "https://player.example.com"}
	if !reflect.DeepEqual(cfg.CORSAllowedOrigins, wantOrigins) {
		t.Errorf("CORSAllowedOrigins: expected %v, got %v", wantOrigins, cfg.CORSAllowedOrigins)
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
		"PLAYER_CORS_ORIGINS",
		"TRANSCODE_CACHE_DIR", "TRANSCODE_CACHE_MAX_MB", "TRANSCODE_MAX_JOBS",
		"THUMBNAIL_CACHE_DIR",
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

func TestLoadConfig_TranscodeCache(t *testing.T) {
	tests := []struct {
		name      string
		env       []envPair
		wantDir   string
		wantMaxMB int
	}{
		// Local default: named after the default (relative) database file.
		{"defaults", nil, "data.db.transcode-cache", DefaultTranscodeCacheMaxMB},
		// Container: the cache follows DB_PATH onto the writable /data volume.
		{"follows DB_PATH", []envPair{{"DB_PATH", "/data/media.db"}}, "/data/media.db.transcode-cache", DefaultTranscodeCacheMaxMB},
		// Two instances with databases in one directory get separate caches.
		{"per database", []envPair{{"DB_PATH", "/tmp/player-e2e.db"}}, "/tmp/player-e2e.db.transcode-cache", DefaultTranscodeCacheMaxMB},
		{"explicit dir wins", []envPair{{"DB_PATH", "/data/media.db"}, {"TRANSCODE_CACHE_DIR", " /cache/renditions "}}, "/cache/renditions", DefaultTranscodeCacheMaxMB},
		{"max size", []envPair{{"TRANSCODE_CACHE_MAX_MB", "512"}}, "data.db.transcode-cache", 512},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv()
			t.Cleanup(clearEnv)
			setEnvPairs(t, tt.env)
			cfg, err := LoadConfig()
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}
			if cfg.TranscodeCacheDir != tt.wantDir {
				t.Errorf("TranscodeCacheDir = %q, want %q", cfg.TranscodeCacheDir, tt.wantDir)
			}
			if cfg.TranscodeCacheMaxMB != tt.wantMaxMB {
				t.Errorf("TranscodeCacheMaxMB = %d, want %d", cfg.TranscodeCacheMaxMB, tt.wantMaxMB)
			}
		})
	}
}

// The cache must not live inside the media root: the scanner would import
// renditions as media and eviction would run next to real media files.
func TestLoadConfig_TranscodeCacheInsideMediaRoot(t *testing.T) {
	tests := []struct {
		name    string
		env     []envPair
		wantErr bool
	}{
		{"explicit dir inside media root", []envPair{{"MEDIA_ROOT", "/media"}, {"TRANSCODE_CACHE_DIR", "/media/.cache"}}, true},
		{"explicit dir is the media root", []envPair{{"MEDIA_ROOT", "/media"}, {"TRANSCODE_CACHE_DIR", "/media/"}}, true},
		{"dot-dot back into media root", []envPair{{"MEDIA_ROOT", "/media"}, {"TRANSCODE_CACHE_DIR", "/data/../media/c"}}, true},
		// The derived default counts too: a database inside the media
		// root needs an explicit TRANSCODE_CACHE_DIR.
		{"default derived from DB_PATH in media root", []envPair{{"MEDIA_ROOT", "/media"}, {"DB_PATH", "/media/media.db"}}, true},
		{"relative paths", []envPair{{"MEDIA_ROOT", "media"}, {"TRANSCODE_CACHE_DIR", "media/cache"}}, true},
		{"sibling with common prefix", []envPair{{"MEDIA_ROOT", "/media"}, {"TRANSCODE_CACHE_DIR", "/media-cache"}}, false},
		{"media root inside cache parent", []envPair{{"MEDIA_ROOT", "/data/media"}, {"DB_PATH", "/data/media.db"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv()
			t.Cleanup(clearEnv)
			setEnvPairs(t, tt.env)
			_, err := LoadConfig()
			if (err != nil) != tt.wantErr {
				t.Fatalf("LoadConfig error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "TRANSCODE_CACHE_DIR") {
				t.Errorf("error should name the setting: %v", err)
			}
		})
	}
}

// The thumbnail cache is opt-in: unset, thumbnails stay beside the media.
func TestLoadConfig_ThumbnailCache(t *testing.T) {
	tests := []struct {
		name    string
		env     []envPair
		wantDir string
		wantErr bool
	}{
		{"unset: no cache", nil, "", false},
		{"unset even with DB_PATH", []envPair{{"DB_PATH", "/data/media.db"}}, "", false},
		{"explicit dir", []envPair{{"THUMBNAIL_CACHE_DIR", " /thumbnails "}}, "/thumbnails", false},
		{"sibling with common prefix", []envPair{{"MEDIA_ROOT", "/media"}, {"THUMBNAIL_CACHE_DIR", "/media-thumbs"}}, "/media-thumbs", false},
		{"inside media root", []envPair{{"MEDIA_ROOT", "/media"}, {"THUMBNAIL_CACHE_DIR", "/media/.cache"}}, "", true},
		{"is the media root", []envPair{{"MEDIA_ROOT", "/media"}, {"THUMBNAIL_CACHE_DIR", "/media/"}}, "", true},
		{"dot-dot back into media root", []envPair{{"MEDIA_ROOT", "/media"}, {"THUMBNAIL_CACHE_DIR", "/data/../media/t"}}, "", true},
		{"relative, inside media root", []envPair{{"MEDIA_ROOT", "media"}, {"THUMBNAIL_CACHE_DIR", "media/thumbs"}}, "", true},
		// A cache above the media root would make every media folder a
		// cache folder, whose emptied directories get removed.
		{"contains the media root", []envPair{{"MEDIA_ROOT", "/data/media"}, {"THUMBNAIL_CACHE_DIR", "/data"}}, "", true},
		{"is the file system root", []envPair{{"MEDIA_ROOT", "/data/media"}, {"THUMBNAIL_CACHE_DIR", "/"}}, "", true},
		{"relative, contains the media root", []envPair{{"MEDIA_ROOT", "data/media"}, {"THUMBNAIL_CACHE_DIR", "data/"}}, "", true},
		{"same directory as the transcode cache", []envPair{{"TRANSCODE_CACHE_DIR", "/cache"}, {"THUMBNAIL_CACHE_DIR", "/cache/"}}, "", true},
		{"same as the default transcode cache", []envPair{{"DB_PATH", "/data/media.db"}, {"THUMBNAIL_CACHE_DIR", "/data/media.db.transcode-cache"}}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv()
			t.Cleanup(clearEnv)
			setEnvPairs(t, tt.env)
			cfg, err := LoadConfig()
			if (err != nil) != tt.wantErr {
				t.Fatalf("LoadConfig error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				if !strings.Contains(err.Error(), "THUMBNAIL_CACHE_DIR") {
					t.Errorf("error should name the setting: %v", err)
				}
				return
			}
			if cfg.ThumbnailCacheDir != tt.wantDir {
				t.Errorf("ThumbnailCacheDir = %q, want %q", cfg.ThumbnailCacheDir, tt.wantDir)
			}
		})
	}
}

// A cache directory that only looks separate from the media root, because
// one of the two is reached through a symbolic link, is rejected as well.
func TestLoadConfig_CacheDirsThroughSymlinks(t *testing.T) {
	base := t.TempDir()
	media, elsewhere := filepath.Join(base, "media"), filepath.Join(base, "elsewhere")
	for _, dir := range []string{filepath.Join(media, "set"), elsewhere} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := func(target, name string) string {
		t.Helper()
		path := filepath.Join(base, name)
		if err := os.Symlink(target, path); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		return path
	}
	intoMedia := link(filepath.Join(media, "set"), "link-into-media")
	mediaLink := link(media, "media-link")
	toElsewhere := link(elsewhere, "link-elsewhere")
	// Its target does not exist yet, so where it leads cannot be checked;
	// it would lead into the media root as soon as someone creates it.
	dangling := link(filepath.Join(media, "set", "later"), "dangling")
	looping := link(filepath.Join(base, "looping"), "looping")
	tests := []struct {
		name    string
		env     []envPair
		wantErr string
	}{
		{"thumbnail cache links into the media root", []envPair{{"MEDIA_ROOT", media}, {"THUMBNAIL_CACHE_DIR", intoMedia}}, "THUMBNAIL_CACHE_DIR"},
		{"not yet existing folder below such a link", []envPair{{"MEDIA_ROOT", media}, {"THUMBNAIL_CACHE_DIR", filepath.Join(intoMedia, "new", "cache")}}, "THUMBNAIL_CACHE_DIR"},
		{"media root is a link, cache inside its target", []envPair{{"MEDIA_ROOT", mediaLink}, {"THUMBNAIL_CACHE_DIR", filepath.Join(media, "cache")}}, "THUMBNAIL_CACHE_DIR"},
		{"transcode cache links into the media root", []envPair{{"MEDIA_ROOT", media}, {"TRANSCODE_CACHE_DIR", intoMedia}}, "TRANSCODE_CACHE_DIR"},
		{"dangling link as thumbnail cache", []envPair{{"MEDIA_ROOT", media}, {"THUMBNAIL_CACHE_DIR", dangling}}, "THUMBNAIL_CACHE_DIR"},
		{"folder below a dangling link", []envPair{{"MEDIA_ROOT", media}, {"THUMBNAIL_CACHE_DIR", filepath.Join(dangling, "cache")}}, "THUMBNAIL_CACHE_DIR"},
		{"looping link as thumbnail cache", []envPair{{"MEDIA_ROOT", media}, {"THUMBNAIL_CACHE_DIR", looping}}, "THUMBNAIL_CACHE_DIR"},
		{"dangling link as media root", []envPair{{"MEDIA_ROOT", dangling}, {"THUMBNAIL_CACHE_DIR", elsewhere}}, "MEDIA_ROOT"},
		{"link to a separate directory is fine", []envPair{{"MEDIA_ROOT", media}, {"THUMBNAIL_CACHE_DIR", toElsewhere}, {"TRANSCODE_CACHE_DIR", filepath.Join(base, "renditions")}}, ""},
		{"both caches are the same directory through a link", []envPair{{"MEDIA_ROOT", media}, {"THUMBNAIL_CACHE_DIR", toElsewhere}, {"TRANSCODE_CACHE_DIR", elsewhere}}, "THUMBNAIL_CACHE_DIR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnv()
			t.Cleanup(clearEnv)
			setEnvPairs(t, tt.env)
			_, err := LoadConfig()
			if (err != nil) != (tt.wantErr != "") {
				t.Fatalf("LoadConfig error = %v, want error: %v", err, tt.wantErr != "")
			}
			if err != nil && !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error should name %s: %v", tt.wantErr, err)
			}
		})
	}
}

func TestLoadConfig_TranscodeCacheInvalidMax(t *testing.T) {
	for _, name := range []string{"TRANSCODE_CACHE_MAX_MB", "TRANSCODE_MAX_JOBS"} {
		for _, v := range []string{"0", "-5", "lots"} {
			clearEnv()
			t.Cleanup(clearEnv)
			setEnvPairs(t, []envPair{{name, v}})
			if _, err := LoadConfig(); err == nil {
				t.Errorf("%s=%q: expected error", name, v)
			}
		}
	}
}

func TestLoadConfig_TranscodeMaxJobs(t *testing.T) {
	clearEnv()
	t.Cleanup(clearEnv)
	cfg, err := LoadConfig()
	if err != nil || cfg.TranscodeMaxJobs != DefaultTranscodeMaxJobs || DefaultTranscodeMaxJobs != 1 {
		t.Fatalf("default TranscodeMaxJobs = %d, %v; want 1", cfg.TranscodeMaxJobs, err)
	}
	setEnvPairs(t, []envPair{{"TRANSCODE_MAX_JOBS", "3"}})
	if cfg, err = LoadConfig(); err != nil || cfg.TranscodeMaxJobs != 3 {
		t.Fatalf("TRANSCODE_MAX_JOBS=3 -> %d, %v", cfg.TranscodeMaxJobs, err)
	}
}
