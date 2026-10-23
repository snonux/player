package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/snonux/player/internal/model"
)

func TestTrashRoutesAndRestorePreserveMetadataAndCaches(t *testing.T) {
	runner := &fakeRunner{}
	e := newE2E(t, runner)
	ctx := context.Background()
	id := e.media["clip.avi"]
	item, err := e.store.GetMediaByID(ctx, id)
	e.must(err)
	e.must(e.store.GrantPermission(ctx, &model.SetPermission{SetID: item.SetID, UserID: e.users["alice"], Role: model.RoleOwner, CreatedAt: time.Now()}))
	token := e.share("clip.avi", "alice", "{}")
	base := fmt.Sprintf("/api/v1/media/%d", id)
	for _, action := range []struct{ path, body string }{
		{base + "/notes", `{"content":"keep my note"}`},
		{base + "/tags", `{"tag":"keep-tag"}`},
		{base + "/favorite", `{}`},
		{"/api/v1/progress", fmt.Sprintf(`{"media_id":%d,"position_seconds":42}`, id)},
	} {
		rr := e.request(ctx, http.MethodPost, action.path, "alice", action.body)
		if rr.Code != http.StatusOK {
			t.Fatalf("setup %s=%d %s", action.path, rr.Code, rr.Body.String())
		}
	}
	if rr := e.get(e.compatPath("clip.avi"), "alice"); rr.Code != http.StatusOK {
		t.Fatalf("cache setup=%d", rr.Code)
	}
	renditions, err := filepath.Glob(filepath.Join(e.deps.Cfg.TranscodeCacheDir, "*.mp4"))
	e.must(err)
	if len(renditions) != 1 {
		t.Fatalf("cache files=%v", renditions)
	}
	rr := e.request(ctx, http.MethodDelete, base, "alice", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("delete=%d %s", rr.Code, rr.Body.String())
	}
	other := fmt.Sprintf("/api/v1/media/%d", e.media["other.wmv"])
	if rr := e.request(ctx, http.MethodDelete, other, "admin", ""); rr.Code != http.StatusOK {
		t.Fatalf("foreign trash setup=%d", rr.Code)
	}
	if _, err := os.Stat(item.AbsPath); err != nil {
		t.Fatalf("source lost before expiry: %v", err)
	}
	if _, err := os.Stat(renditions[0]); err != nil {
		t.Fatalf("cache lost before expiry: %v", err)
	}
	for _, path := range []string{base, base + "/stream", base + "/download", base + "/compat", "/s/" + token} {
		if rr := e.get(path, "alice"); rr.Code != http.StatusNotFound && rr.Code != http.StatusGone {
			t.Fatalf("trashed content %s=%d", path, rr.Code)
		}
	}
	for _, prefix := range []string{"/api/", "/api/v1/"} {
		for _, tc := range []struct {
			user          string
			status, count int
		}{
			{"", 401, 0}, {"bob", 200, 0}, {"alice", 200, 1}, {"admin", 200, 2},
		} {
			rr := e.get(prefix+"trash", tc.user)
			if rr.Code != tc.status {
				t.Fatalf("%strash as %s=%d", prefix, tc.user, rr.Code)
			}
			if tc.status == 200 {
				var rows []model.Media
				e.must(json.Unmarshal(rr.Body.Bytes(), &rows))
				if len(rows) != tc.count {
					t.Fatalf("trash as %s=%v", tc.user, rows)
				}
				if rows == nil {
					t.Fatal("empty collection serialized null")
				}
			}
		}
	}
	rr = e.request(ctx, http.MethodPost, base+"/restore", "bob", "")
	if rr.Code != http.StatusForbidden {
		t.Fatalf("foreign restore=%d", rr.Code)
	}
	rr = e.request(ctx, http.MethodPost, base+"/restore", "alice", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("restore=%d", rr.Code)
	}
	for _, path := range []string{base + "/notes", "/api/v1/tags", base + "/compat", "/s/" + token} {
		if rr := e.get(path, "alice"); rr.Code != http.StatusOK {
			t.Fatalf("restored %s=%d %s", path, rr.Code, rr.Body.String())
		}
	}
	tags, err := e.store.ListTagsByMedia(ctx, id)
	e.must(err)
	if len(tags) != 1 || tags[0].Name != "keep-tag" {
		t.Fatalf("tags lost: %v", tags)
	}
	favorite, err := e.store.IsFavorite(ctx, e.users["alice"], id)
	e.must(err)
	if !favorite {
		t.Fatal("favorite lost on restore")
	}
	if runner.calls.Load() != 1 {
		t.Fatalf("restore regenerated unchanged rendition; calls=%d", runner.calls.Load())
	}
	note, err := e.store.GetNote(ctx, id, e.users["alice"])
	e.must(err)
	if note == nil || note.Content != "keep my note" {
		t.Fatalf("note lost=%+v", note)
	}
	progress, err := e.store.GetProgress(ctx, e.users["alice"], id)
	e.must(err)
	if progress == nil || progress.PositionSeconds != 42 {
		t.Fatalf("progress lost=%+v", progress)
	}
	// Force expiry after a second delete; exercise the actual wired cache removal.
	rr = e.request(ctx, http.MethodDelete, base, "alice", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("second delete=%d", rr.Code)
	}
	e.must(e.deps.GCWorker.WithAge(-time.Hour).RunOnce())
	for _, path := range []string{item.AbsPath, renditions[0]} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expired path remains %s: %v", path, err)
		}
	}
	rr = e.request(ctx, http.MethodPost, base+"/restore", "alice", "")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("restore purged=%d", rr.Code)
	}
	note, err = e.store.GetNote(ctx, id, e.users["alice"])
	e.must(err)
	if note != nil {
		t.Fatal("expired note did not cascade")
	}
}

func TestTrashRestoreMissingSourceHTTPKeepsTrashUntilSourceReturns(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	ctx := context.Background()
	id := e.media["pic.png"]
	original, err := e.store.GetMediaByID(ctx, id)
	e.must(err)
	base := fmt.Sprintf("/api/v1/media/%d", id)
	rr := e.request(ctx, http.MethodDelete, base, "admin", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("delete=%d", rr.Code)
	}
	e.must(os.Remove(original.AbsPath))
	rr = e.request(ctx, http.MethodPost, base+"/restore", "admin", "")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("missing source restore=%d %s", rr.Code, rr.Body.String())
	}
	active, err := e.store.GetMediaByID(ctx, id)
	e.must(err)
	if active != nil {
		t.Fatal("missing source became active")
	}
	rows, err := e.store.ListDeletedMedia(ctx)
	e.must(err)
	if len(rows) != 1 || rows[0].ID != id || rows[0].DeletedAt == nil {
		t.Fatalf("missing source trash=%v", rows)
	}
	e.must(os.WriteFile(original.AbsPath, []byte("returned source"), 0644))
	rr = e.request(ctx, http.MethodPost, base+"/restore", "admin", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("returned source restore=%d", rr.Code)
	}
}
