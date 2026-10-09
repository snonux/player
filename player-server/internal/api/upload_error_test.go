package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	internal "codeberg.org/snonux/player/internal"
	"codeberg.org/snonux/player/internal/auth"
	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/service"
)

// TestServer_UploadErrorBodies pins status and exact body of failed
// uploads. A refused file answers 415 with a fixed text, and an internal
// failure 500 with a fixed text: neither may show the error detail, which
// holds absolute server paths and ffprobe output.
func TestServer_UploadErrorBodies(t *testing.T) {
	const detail = "ffprobe /srv/media/uploads/evil.jpg: exit status 1: Format not on whitelist"
	tests := []struct {
		name     string
		err      error
		wantCode int
		wantBody string
	}{
		{"refused file", service.ErrUnreadableMedia, http.StatusUnsupportedMediaType,
			`{"error":"unsupported or unreadable media file"}`},
		{"refused file wrapped with detail", fmt.Errorf("%w: %s", service.ErrUnreadableMedia, detail),
			http.StatusUnsupportedMediaType, `{"error":"unsupported or unreadable media file"}`},
		{"internal failure with a path", errors.New("create file: open /srv/media/uploads/a.mp4: permission denied"),
			http.StatusInternalServerError, `{"error":"upload failed"}`},
		{"probe failure that is no refusal", errors.New("probe media: " + detail),
			http.StatusInternalServerError, `{"error":"upload failed"}`},
		{"unsupported extension", fmt.Errorf("%w: %s", service.ErrUnsupportedExtension, ".exe"),
			http.StatusBadRequest, `{"error":"unsupported file extension: .exe"}`},
	}
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24, MaxUploadSizeMB: 10}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := &service.MockMediaService{
				UploadMediaFunc: func(context.Context, int64, int64, string, io.Reader, int64) (*model.Media, error) {
					return nil, tt.err
				},
			}
			srv := newTestServer(t, store, nil, sm, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, nil)
			req := newUploadRequest(t, "1", "evil.jpg", "data")
			req.AddCookie(sessionCookieForStore(t, store, sm, 1))
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, req)
			if rr.Code != tt.wantCode {
				t.Errorf("status = %d, want %d", rr.Code, tt.wantCode)
			}
			if got := strings.TrimSpace(rr.Body.String()); got != tt.wantBody {
				t.Errorf("body = %s, want %s", got, tt.wantBody)
			}
		})
	}
}
