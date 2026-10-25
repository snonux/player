package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/transcode"
)

type snapshotCopyRunner struct{}

func (snapshotCopyRunner) Transcode(ctx context.Context, job transcode.Job) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := os.ReadFile(job.Source.Path)
	if err != nil {
		return err
	}
	return os.WriteFile(job.Output, data, 0600)
}

func TestLyricsSnapshotOriginalRevisionPreservedMtime(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	ctx := context.Background()
	media, err := e.store.GetMediaByID(ctx, e.media["movie.mp4"])
	e.must(err)
	path := "/api/media/" + strconv.FormatInt(media.ID, 10) + "/download"
	before := e.get(path, "alice")
	info, err := os.Stat(media.AbsPath)
	e.must(err)
	body := []byte(strings.Repeat("B", int(info.Size())))
	e.must(os.WriteFile(media.AbsPath, body, 0600))
	e.must(os.Chtimes(media.AbsPath, info.ModTime(), info.ModTime()))
	after := e.get(path, "alice")
	if before.Code != 200 || after.Code != 200 {
		t.Fatalf("download codes %d/%d", before.Code, after.Code)
	}
	if before.Header().Get("ETag") == after.Header().Get("ETag") {
		t.Fatal("same-size preserved-mtime replacement reused served-byte validator")
	}
}

func TestLyricsSnapshotServedRevisionAcrossAudioProfiles(t *testing.T) {
	for _, route := range []string{"stream", "download", "compat", "compat?profile=aac128", "compat?profile=aac256", "compat?profile=mp3192", "compat?profile=opus96"} {
		t.Run(route, func(t *testing.T) {
			e := newE2E(t, snapshotCopyRunner{})
			ctx := context.Background()
			parent, err := e.store.GetMediaByID(ctx, e.media["movie.mp4"])
			e.must(err)
			path := filepath.Join(filepath.Dir(parent.AbsPath), "snapshot.wma")
			old := time.Now().Add(-time.Hour)
			e.must(os.WriteFile(path, []byte("AAAA"), 0600))
			e.must(os.Chtimes(path, old, old))
			id, err := e.store.CreateMedia(ctx, &model.Media{SetID: parent.SetID, Type: model.MediaTypeAudio, FileName: "snapshot.wma", RelPath: "snapshot.wma", AbsPath: path, Codec: "wmav2", FileSizeBytes: 4, CreatedAt: old})
			e.must(err)
			url := "/api/media/" + strconv.FormatInt(id, 10) + "/" + route
			request := func(method string, headers map[string]string) *httptest.ResponseRecorder {
				t.Helper()
				r := httptest.NewRequest(method, url, nil)
				session, err := e.deps.SM.CreateSession(ctx, e.users["alice"])
				e.must(err)
				r.AddCookie(&http.Cookie{Name: "session", Value: session})
				for key, value := range headers {
					r.Header.Set(key, value)
				}
				rr := httptest.NewRecorder()
				e.server.ServeHTTP(rr, r)
				return rr
			}
			first := request("GET", nil)
			if first.Code != 200 || first.Body.String() != "AAAA" || first.Header().Get("ETag") == "" {
				t.Fatalf("original served copy %d %s", first.Code, first.Body.String())
			}
			if head := request("HEAD", nil); head.Code != 200 || head.Body.Len() != 0 || head.Header().Get("ETag") != first.Header().Get("ETag") {
				t.Fatalf("HEAD provenance %d", head.Code)
			}
			e.must(os.WriteFile(path, []byte("BBBB"), 0600))
			e.must(os.Chtimes(path, old, old))
			second := request("HEAD", nil)
			if second.Code != 200 || second.Header().Get("ETag") == first.Header().Get("ETag") {
				t.Fatalf("replacement retained old source/rendition validator: %d", second.Code)
			}
			changed := request("GET", map[string]string{"Range": "bytes=0-1", "If-Range": first.Header().Get("ETag")})
			if changed.Code != 200 || changed.Body.String() != "BBBB" {
				t.Fatalf("old If-Range mixed encodes: %d %q", changed.Code, changed.Body.String())
			}
			current := request("GET", map[string]string{"Range": "bytes=0-1", "If-Range": second.Header().Get("ETag")})
			if current.Code != 206 || current.Body.String() != "BB" {
				t.Fatalf("new revision Range failed: %d %q", current.Code, current.Body.String())
			}
			unchanged := request("HEAD", map[string]string{"If-None-Match": second.Header().Get("ETag")})
			if unchanged.Code != 304 {
				t.Fatalf("ETag revalidation: %d", unchanged.Code)
			}
		})
	}
}
