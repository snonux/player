package api

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	internal "github.com/snonux/player/internal"
	"github.com/snonux/player/internal/auth"
	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/service"
)

// startWithTimeouts serves h like NewGracefulServer does, but with short
// read/write deadlines so a slow transfer outlasts them quickly.
func startWithTimeouts(t *testing.T, h http.Handler, read, write time.Duration) *httptest.Server {
	t.Helper()
	ts := httptest.NewUnstartedServer(h)
	ts.Config.ReadTimeout = read
	ts.Config.WriteTimeout = write
	ts.Start()
	t.Cleanup(ts.Close)
	return ts
}

// TestUpload_SlowBodyOutlastsReadTimeout: an upload slower than the server's
// ReadTimeout must still arrive whole (it used to fail with 400 after 30s).
func TestUpload_SlowBodyOutlastsReadTimeout(t *testing.T) {
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24, MaxUploadSizeMB: 10}
	var got int64
	ms := &service.MockMediaService{
		UploadMediaFunc: func(_ context.Context, _, _ int64, _ string, r io.Reader, _ int64) (*model.Media, error) {
			n, _ := io.Copy(io.Discard, r)
			got = n
			return &model.Media{ID: 1}, nil
		},
	}
	srv := newTestServer(t, store, nil, sm, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, nil)
	ts := startWithTimeouts(t, srv, 200*time.Millisecond, 5*time.Second)

	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	part, _ := mw.CreateFormFile("file", "a.mp4")
	_, _ = part.Write(bytes.Repeat([]byte("x"), 4096))
	_ = mw.Close()
	body := form.Bytes()

	pr, pw := io.Pipe()
	go func() {
		// Dribble the body for ~4x the read timeout.
		chunk := len(body)/8 + 1
		for len(body) > 0 {
			n := min(len(body), chunk)
			if _, err := pw.Write(body[:n]); err != nil {
				return
			}
			body = body[n:]
			time.Sleep(100 * time.Millisecond)
		}
		_ = pw.Close()
	}()
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/sets/1/upload", pr)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(sessionCookieForStore(t, store, sm, 1))
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d (%s), want 200", resp.StatusCode, b)
	}
	if got != 4096 {
		t.Fatalf("service received %d bytes, want 4096", got)
	}
}

// TestDownload_SlowClientOutlastsWriteTimeout: an attachment download read
// slower than the server's WriteTimeout must not be cut midway.
func TestDownload_SlowClientOutlastsWriteTimeout(t *testing.T) {
	const size = 16 << 20
	path := filepath.Join(t.TempDir(), "big.mp4")
	if err := os.WriteFile(path, bytes.Repeat([]byte("y"), size), 0o600); err != nil {
		t.Fatal(err)
	}
	store := buildSessionStore(1)
	sm := auth.NewSessionManager(store, &clock.MockClock{T: time.Now()}, time.Hour)
	cfg := &internal.Config{SessionTimeoutHours: 24}
	ms := &service.MockMediaService{
		DownloadMediaFunc: func(context.Context, int64, int64) (*service.FileResult, error) {
			return &service.FileResult{Path: path, FileName: "big.mp4", FileSize: size}, nil
		},
	}
	srv := newTestServer(t, store, nil, sm, cfg, ms, ms, ms, ms, ms, ms, nil, nil, nil, nil)
	ts := startWithTimeouts(t, srv, 5*time.Second, 300*time.Millisecond)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/media/1/download", nil)
	req.AddCookie(sessionCookieForStore(t, store, sm, 1))
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var n int64
	buf := make([]byte, 1<<20)
	for {
		k, err := resp.Body.Read(buf)
		n += int64(k)
		if err != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n != size {
		t.Fatalf("read %d bytes, want %d", n, size)
	}
}
