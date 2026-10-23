package api

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/service"
	"github.com/snonux/player/internal/transcode"
)

type delayedDownloadQuality struct {
	service.MockCompatStreamService
	path string
	err  error
}

func (q *delayedDownloadQuality) QualityStream(context.Context, int64, int64, string) (*transcode.Rendition, error) {
	time.Sleep(60 * time.Millisecond)
	return &transcode.Rendition{Path: q.path, ContentType: "audio/ogg"}, q.err
}

func TestMusicDownloadsHTTPPreparationDeadline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.ogg")
	if err := os.WriteFile(path, []byte("encoded song"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, failure := range []error{nil, service.ErrTranscodeNoSpace} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			browse := &service.MockMediaService{
				GetMediaDetailFunc: func(context.Context, int64, int64) (*service.MediaDetail, error) {
					return &service.MediaDetail{Media: &model.Media{Type: "audio"}}, nil
				},
				DownloadMediaFunc: func(context.Context, int64, int64) (*service.FileResult, error) {
					return &service.FileResult{FileName: "song.flac"}, nil
				},
			}
			server := &Server{media: MediaServices{Browse: browse, Compat: &delayedDownloadQuality{path: path, err: failure}}}
			httpServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r = r.WithContext(context.WithValue(r.Context(), sessionCtxKey, &model.Session{UserID: 7}))
				server.handleMusicDownloads(w, r)
			}))
			httpServer.Config.WriteTimeout = 20 * time.Millisecond
			httpServer.Start()
			defer httpServer.Close()
			response, err := httpServer.Client().Post(httpServer.URL, "application/json", strings.NewReader(`{"media_ids":[1],"profile":"opus96"}`))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = response.Body.Close() }()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if failure != nil {
				if response.StatusCode != 507 || !bytes.Contains(body, []byte("insufficient storage")) {
					t.Fatalf("status=%d body=%s", response.StatusCode, body)
				}
				return
			}
			if response.StatusCode != 200 {
				t.Fatalf("status=%d body=%s", response.StatusCode, body)
			}
			archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
			if err != nil || len(archive.File) != 1 {
				t.Fatalf("archive=%v err=%v", archive, err)
			}
		})
	}
}

func TestMusicDownloadsHTTP(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "song.mp3")
	if err := os.WriteFile(path, []byte("music"), 0600); err != nil {
		t.Fatal(err)
	}
	browse := &service.MockMediaService{
		GetMediaDetailFunc: func(_ context.Context, id, user int64) (*service.MediaDetail, error) {
			if user != 7 {
				return nil, service.ErrForbidden
			}
			if id == 99 {
				return nil, service.ErrForbidden
			}
			return &service.MediaDetail{Media: &model.Media{Type: "audio"}}, nil
		},
		DownloadMediaFunc: func(_ context.Context, id, user int64) (*service.FileResult, error) {
			return &service.FileResult{Path: path, FileName: "song.mp3"}, nil
		},
	}
	server := &Server{media: MediaServices{Browse: browse}, streamer: service.NewMediaStreamer(nil, root)}
	for _, tc := range []struct {
		body   string
		user   int64
		status int
	}{
		{`{"media_ids":[1,1,2]}`, 7, 200}, {`{"media_ids":[]}`, 7, 400}, {`{"media_ids":[99]}`, 7, 403}, {`{"media_ids":[1]}`, 8, 403}, {`{"media_ids":[1],"wrong":true}`, 7, 400},
	} {
		r := httptest.NewRequest("POST", "/api/music/downloads", strings.NewReader(tc.body))
		r = r.WithContext(context.WithValue(r.Context(), sessionCtxKey, &model.Session{UserID: tc.user}))
		w := httptest.NewRecorder()
		server.handleMusicDownloads(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s: status=%d body=%s", tc.body, w.Code, w.Body)
		}
		if w.Code == 200 {
			archive, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
			if err != nil {
				t.Fatal(err)
			}
			if len(archive.File) != 2 || w.Header().Get("X-Download-Count") != "2" || w.Header().Get("Content-Type") != "application/zip" {
				t.Fatalf("headers=%v count=%d", w.Header(), len(archive.File))
			}
		}
	}
}
