package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
)

// Independent bearer tokens represent installations of the Android app. These
// requests use the production routes, services, auth and on-disk SQLite store.
func syncDeviceToken(t *testing.T, e *e2e, user, name string) string {
	t.Helper()
	token, err := e.deps.AuthSvc.CreateAPIToken(context.Background(), e.users[user], name, nil)
	e.must(err)
	return token.Plaintext
}

func syncDeviceRequest(t *testing.T, handler http.Handler, token, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(string(data)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func syncDevice(t *testing.T, handler http.Handler, token, path string, incoming model.PlaybackSync) model.PlaybackSync {
	t.Helper()
	rr := syncDeviceRequest(t, handler, token, http.MethodPost, path, incoming)
	if rr.Code != http.StatusOK {
		t.Fatalf("sync = %d: %s", rr.Code, rr.Body)
	}
	var result model.PlaybackSync
	if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Bookmarks == nil {
		t.Fatal("bookmarks must be a JSON array")
	}
	return result
}

func TestPlaybackSyncDevicesNewestEditWins(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	a := syncDeviceToken(t, e, "alice", "phone A")
	b := syncDeviceToken(t, e, "alice", "phone B")
	mid := e.media["movie.mp4"]
	path := fmt.Sprintf("/api/v1/media/%d/playback-sync", mid)
	legacyPath := fmt.Sprintf("/api/media/%d/playback-sync", mid)
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	progress := func(seconds float64, offset int64) *model.SyncedProgress {
		return &model.SyncedProgress{PositionSeconds: seconds, ObservedMS: base + offset}
	}
	bookmark := model.SyncedBookmark{Key: "device-a-chapter", PositionSeconds: 12, Label: "chapter", ObservedMS: base + 1000}
	syncDevice(t, e.server, a, path, model.PlaybackSync{Progress: progress(42, 1000), Bookmarks: []model.SyncedBookmark{bookmark}})
	got := syncDevice(t, e.server, b, legacyPath, model.PlaybackSync{})
	if got.Progress == nil || got.Progress.PositionSeconds != 42 || len(got.Bookmarks) != 1 || got.Bookmarks[0] != bookmark {
		t.Fatalf("second installation did not receive first edits: %+v", got)
	}

	newer := bookmark
	newer.Label, newer.ObservedMS = "edited on B", base+3000
	syncDevice(t, e.server, b, path, model.PlaybackSync{Progress: progress(84, 3000), Bookmarks: []model.SyncedBookmark{newer}})
	// A reconnects later, carrying older edits. Arrival order must not decide.
	got = syncDevice(t, e.server, a, path, model.PlaybackSync{Progress: progress(20, 2000), Bookmarks: []model.SyncedBookmark{bookmark}})
	if got.Progress.PositionSeconds != 84 || got.Progress.ObservedMS != base+3000 || got.Bookmarks[0] != newer {
		t.Fatalf("stale offline edit replaced newer edit: %+v", got)
	}
	// Equal timestamps keep the stored edit and retries never duplicate a row.
	tie := newer
	tie.Label = "equal-time replacement"
	got = syncDevice(t, e.server, a, path, model.PlaybackSync{Progress: progress(0, 3000), Bookmarks: []model.SyncedBookmark{tie, tie}})
	if got.Progress.PositionSeconds != 84 || len(got.Bookmarks) != 1 || got.Bookmarks[0] != newer {
		t.Fatalf("equal-time retry was not stable: %+v", got)
	}
	tombstone := newer
	tombstone.Deleted, tombstone.ObservedMS = true, base+4000
	syncDevice(t, e.server, b, path, model.PlaybackSync{Bookmarks: []model.SyncedBookmark{tombstone}})
	got = syncDevice(t, e.server, a, path, model.PlaybackSync{Bookmarks: []model.SyncedBookmark{newer}})
	if len(got.Bookmarks) != 1 || got.Bookmarks[0] != tombstone {
		t.Fatalf("stale copy resurrected deleted bookmark: %+v", got.Bookmarks)
	}
	visible, err := e.store.ListBookmarks(context.Background(), mid, e.users["alice"])
	e.must(err)
	if len(visible) != 0 {
		t.Fatalf("deleted bookmarks exposed by existing list API: %+v", visible)
	}

	// Another account with access still owns separate progress and bookmarks,
	// even if it happens to use the same bookmark sync key.
	media, err := e.store.GetMediaByID(context.Background(), mid)
	e.must(err)
	e.must(e.store.GrantPermission(context.Background(), &model.SetPermission{UserID: e.users["bob"], SetID: media.SetID, Role: model.RoleViewer, CreatedAt: time.Now()}))
	other := syncDeviceToken(t, e, "bob", "other account")
	isolated := syncDevice(t, e.server, other, path, model.PlaybackSync{})
	if isolated.Progress != nil || len(isolated.Bookmarks) != 0 {
		t.Fatalf("another account received Alice's state: %+v", isolated)
	}
	syncDevice(t, e.server, other, path, model.PlaybackSync{Bookmarks: []model.SyncedBookmark{bookmark}})
	if got = syncDevice(t, e.server, a, path, model.PlaybackSync{}); got.Bookmarks[0] != tombstone {
		t.Fatalf("other account changed Alice's bookmark: %+v", got.Bookmarks)
	}

	// A fresh repository and server graph can read the edits using the saved
	// second-device token: neither auth nor playback state is held by a client.
	reopened, err := repository.Open(filepath.Join(filepath.Dir(e.deps.Cfg.MediaRoot), "media.db"))
	e.must(err)
	t.Cleanup(func() { _ = reopened.Close() })
	deps := WireWithRunner(e.deps.Cfg, reopened, e.deps.Logger, context.Background(), &fakeRunner{})
	t.Cleanup(func() { stopTranscoding(deps) })
	server, err := NewAPIServer(deps, http.Dir(t.TempDir()), e.deps.Logger)
	e.must(err)
	got = syncDevice(t, server, b, path, model.PlaybackSync{})
	if got.Progress.PositionSeconds != 84 || got.Bookmarks[0] != tombstone {
		t.Fatalf("state did not persist across server recreation: %+v", got)
	}
}

func TestPlaybackSyncStreamedStatusAndWebDeletion(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	token := syncDeviceToken(t, e, "alice", "downloaded copy")
	mid := e.media["movie.mp4"]
	path := fmt.Sprintf("/api/v1/media/%d/playback-sync", mid)
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	setStatus := func(status string, offset time.Duration) {
		t.Helper()
		rr := syncDeviceRequest(t, e.server, token, "POST", "/api/v1/progress/status", map[string]any{
			"media_id": mid, "status": status, "observed_at": base.Add(offset),
		})
		if rr.Code != 200 {
			t.Fatalf("set %s = %d: %s", status, rr.Code, rr.Body)
		}
	}
	putPosition := func(seconds float64, offset time.Duration) model.PlaybackSync {
		t.Helper()
		return syncDevice(t, e.server, token, path, model.PlaybackSync{Progress: &model.SyncedProgress{
			PositionSeconds: seconds, ObservedMS: base.Add(offset).UnixMilli(),
		}})
	}
	putPosition(42, time.Second)
	setStatus("finished", 3*time.Second)
	if got := putPosition(20, 2*time.Second); got.Progress == nil || !got.Progress.Finished {
		t.Fatalf("old download position undid streaming completion: %+v", got.Progress)
	}
	if got := putPosition(10, 4*time.Second); got.Progress.Finished || got.Progress.PositionSeconds != 10 {
		t.Fatalf("new replay did not replace older completion: %+v", got.Progress)
	}
	setStatus("not_started", 6*time.Second+900*time.Microsecond)
	setStatus("finished", 5*time.Second)
	got := putPosition(90, 5*time.Second)
	if got.Progress == nil || got.Progress.Finished || got.Progress.PositionSeconds != 0 || got.Progress.ObservedMS != base.Add(6*time.Second).UnixMilli() {
		t.Fatalf("reset did not block stale offline status/position: %+v", got.Progress)
	}
	// The existing batch endpoint must honor the same reset barrier.
	rr := syncDeviceRequest(t, e.server, token, "POST", "/api/v1/progress/batch", map[string]any{
		"updates": []any{map[string]any{"media_id": mid, "position_seconds": 50, "observed_at": base.Add(6*time.Second + 500*time.Microsecond)}},
	})
	if rr.Code != 200 {
		t.Fatalf("batch = %d: %s", rr.Code, rr.Body)
	}
	if got = syncDevice(t, e.server, token, path, model.PlaybackSync{}); got.Progress.PositionSeconds != 0 {
		t.Fatalf("old streaming batch resurrected reset progress: %+v", got.Progress)
	}
	if got = putPosition(12, 7*time.Second); got.Progress.PositionSeconds != 12 {
		t.Fatalf("newer position did not replace reset: %+v", got.Progress)
	}

	bookmarkPath := fmt.Sprintf("/api/v1/media/%d/bookmarks", mid)
	rr = e.request(context.Background(), "POST", bookmarkPath, "alice", `{"position_seconds":12,"label":"from web"}`)
	if rr.Code != 201 {
		t.Fatalf("web bookmark create = %d: %s", rr.Code, rr.Body)
	}
	var webBookmark model.Bookmark
	e.must(json.Unmarshal(rr.Body.Bytes(), &webBookmark))
	got = syncDevice(t, e.server, token, path, model.PlaybackSync{})
	if len(got.Bookmarks) != 1 || got.Bookmarks[0].Label != "from web" {
		t.Fatalf("web bookmark did not reach download: %+v", got.Bookmarks)
	}
	stale := got.Bookmarks[0]
	rr = e.request(context.Background(), "DELETE", fmt.Sprintf("%s/%d", bookmarkPath, webBookmark.ID), "alice", "")
	if rr.Code != 204 {
		t.Fatalf("web bookmark delete = %d: %s", rr.Code, rr.Body)
	}
	got = syncDevice(t, e.server, token, path, model.PlaybackSync{Bookmarks: []model.SyncedBookmark{stale}})
	if len(got.Bookmarks) != 1 || !got.Bookmarks[0].Deleted || got.Bookmarks[0].ObservedMS <= stale.ObservedMS {
		t.Fatalf("web deletion lost to stale downloaded copy: %+v", got.Bookmarks)
	}
}

func TestPlaybackSyncConcurrentDevices(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	tokens := []string{
		syncDeviceToken(t, e, "alice", "concurrent phone A"),
		syncDeviceToken(t, e, "alice", "concurrent phone B"),
	}
	path := fmt.Sprintf("/api/v1/media/%d/playback-sync", e.media["movie.mp4"])
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	const edits = 16
	statuses := make(chan int, edits)
	start := make(chan struct{})
	for i := 0; i < edits; i++ {
		go func() {
			<-start
			rr := syncDeviceRequest(t, e.server, tokens[i%2], "POST", path, model.PlaybackSync{
				Progress:  &model.SyncedProgress{PositionSeconds: float64(i), ObservedMS: base + int64(i)},
				Bookmarks: []model.SyncedBookmark{{Key: "same-bookmark", PositionSeconds: float64(i), Label: fmt.Sprint(i), ObservedMS: base + int64(i)}},
			})
			statuses <- rr.Code
		}()
	}
	close(start)
	for i := 0; i < edits; i++ {
		if code := <-statuses; code != http.StatusOK {
			t.Errorf("concurrent edit = %d", code)
		}
	}
	got := syncDevice(t, e.server, tokens[0], path, model.PlaybackSync{})
	if got.Progress == nil || got.Progress.PositionSeconds != edits-1 || len(got.Bookmarks) != 1 || got.Bookmarks[0].Label != fmt.Sprint(edits-1) {
		t.Fatalf("concurrent requests lost newest edit: %+v", got)
	}
}

func TestPlaybackSyncAccessAndValidation(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	ctx := context.Background()
	mid := e.media["movie.mp4"]
	path := fmt.Sprintf("/api/v1/media/%d/playback-sync", mid)
	media, err := e.store.GetMediaByID(ctx, mid)
	e.must(err)
	media.Duration = 120
	e.must(e.store.UpdateMedia(ctx, media))
	for _, tc := range []struct {
		name, user, route, body string
		status                  int
	}{
		{"anonymous", "", path, `{}`, 401},
		{"no permission", "bob", path, `{}`, 403},
		{"missing media", "alice", "/api/v1/media/999999/playback-sync", `{}`, 404},
		{"invalid id", "alice", "/api/media/nope/playback-sync", `{}`, 400},
		{"malformed JSON", "alice", path, `{`, 400},
		{"invalid shape", "alice", path, `[]`, 400},
		{"zero timestamp", "alice", path, `{"progress":{"position_seconds":1,"observed_ms":0}}`, 400},
		{"negative position", "alice", path, `{"progress":{"position_seconds":-1,"observed_ms":1000}}`, 400},
		{"future overflow", "alice", path, `{"progress":{"position_seconds":1,"observed_ms":253402300800000}}`, 400},
		{"missing key", "alice", path, `{"bookmarks":[{"position_seconds":1,"observed_ms":1000}]}`, 400},
		{"past end", "alice", path, `{"bookmarks":[{"sync_key":"bad","position_seconds":121,"observed_ms":1000}]}`, 400},
		{"image bookmark", "alice", fmt.Sprintf("/api/v1/media/%d/playback-sync", e.media["pic.png"]), `{"bookmarks":[{"sync_key":"bad","position_seconds":1,"observed_ms":1000}]}`, 400},
		{"atomic validation", "alice", path, `{"progress":{"position_seconds":42,"observed_ms":1000},"bookmarks":[{"sync_key":"bad","position_seconds":-1,"observed_ms":1000}]}`, 400},
		{"oversized body", "alice", path, `{"bookmarks":[{"sync_key":"bad","position_seconds":1,"observed_ms":1000,"label":"` + strings.Repeat("x", 3<<20) + `"}]}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rr := e.request(ctx, "POST", tc.route, tc.user, tc.body)
			if rr.Code != tc.status {
				t.Fatalf("got %d, want %d: %s", rr.Code, tc.status, rr.Body)
			}
		})
	}
	p, err := e.store.GetProgress(ctx, e.users["alice"], mid)
	e.must(err)
	bookmarks, err := e.store.ListBookmarks(ctx, mid, e.users["alice"])
	e.must(err)
	if p != nil || len(bookmarks) != 0 {
		t.Fatalf("invalid request changed persistent state: progress=%+v bookmarks=%+v", p, bookmarks)
	}
	e.must(e.store.RevokePermission(ctx, media.SetID, e.users["alice"]))
	if rr := e.request(ctx, "POST", path, "alice", `{}`); rr.Code != 403 {
		t.Fatalf("revoked permission = %d: %s", rr.Code, rr.Body)
	}
}
