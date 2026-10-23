package api

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/service"
)

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
