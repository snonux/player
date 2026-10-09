package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"codeberg.org/snonux/player/internal"
	"codeberg.org/snonux/player/internal/repository"
)

// TestNewAPIServer_WiresCompatStream guards the production wiring of the
// compatibility stream. A service left out of the ServerServices literal
// compiles fine and only shows up as "501 not implemented" at runtime, which
// unit tests with hand-built servers cannot notice.
func TestNewAPIServer_WiresCompatStream(t *testing.T) {
	dir := t.TempDir()
	store, err := repository.Open(filepath.Join(dir, "media.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cfg := &internal.Config{
		MediaRoot:           filepath.Join(dir, "media"),
		SessionTimeoutHours: 1,
		GCIntervalMinutes:   1,
		PodcastCheckMinutes: 1,
		TranscodeCacheDir:   filepath.Join(dir, "transcode-cache"),
		TranscodeCacheMaxMB: 1,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	deps := Wire(cfg, store, logger, ctx)
	if deps.CompatSvc == nil {
		t.Fatal("Wire did not build the compat stream service")
	}
	server, err := NewAPIServer(deps, http.Dir(t.TempDir()), logger)
	if err != nil {
		t.Fatalf("NewAPIServer: %v", err)
	}

	tests := []struct {
		name string
		path string
		want int
	}{
		// The public share route reaches the service, which rejects the
		// unknown token; 501 would mean the service is not wired.
		{"share compat reaches service", "/s/unknown-token/compat", http.StatusNotFound},
		{"share stream still wired", "/s/unknown-token/stream", http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			server.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if rr.Code != tt.want {
				t.Errorf("GET %s = %d, want %d (%s)", tt.path, rr.Code, tt.want, rr.Body.String())
			}
		})
	}
}
