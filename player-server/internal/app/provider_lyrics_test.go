package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snonux/player/internal"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/service"
)

func TestProviderLyricsExplicitAccountAndContentBoundaries(t *testing.T) {
	var calls, mode atomic.Int32
	var sidecar string
	var revoke func()
	var replace func()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.URL.Query().Get("title") == "" || r.URL.Query().Get("artist") != "Fixture Artist" {
			t.Error("provider request leaked credentials or missed tags")
		}
		switch mode.Load() {
		case 1:
			http.Error(w, "fixture failure", 502)
			return
		case 2:
			if err := os.WriteFile(sidecar, []byte("[00:00]Local wins"), 0600); err != nil {
				t.Error(err)
			}
		case 3:
			w.WriteHeader(204)
			return
		case 4:
			revoke()
		case 5:
			if err := os.WriteFile(sidecar, []byte("[00:00]Local after failure"), 0600); err != nil {
				t.Error(err)
			}
			http.Error(w, "fixture failure", 503)
			return
		case 6:
			replace()
			http.Error(w, "fixture failure", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"language":"en","synced":true,"lines":[{"text":"Hello world","start":0,"end":5,"voice":"Lead","words":[{"text":"Hello ","start":0},{"text":"world","start":2}],"translations":{"fr":"Bonjour"}}]}`))
	}))
	defer provider.Close()
	e := newE2EWith(t, &fakeRunner{}, func(cfg *internal.Config) { cfg.LyricsProviderURL = provider.URL })
	ctx := context.Background()
	original, err := e.store.GetMediaByID(ctx, e.media["movie.mp4"])
	e.must(err)
	source := filepath.Join(filepath.Dir(original.AbsPath), "provider.mp3")
	sidecar = filepath.Join(filepath.Dir(source), "provider.lrc")
	e.must(os.WriteFile(source, []byte("fake audio"), 0600))
	id, err := e.store.CreateMedia(ctx, &model.Media{SetID: original.SetID, AbsPath: source, FileName: "provider.mp3", RelPath: "provider.mp3", Type: model.MediaTypeAudio, CreatedAt: time.Now()})
	e.must(err)
	tags := &model.MusicMetadata{Title: "Fixture Song", Artist: "Fixture Artist"}
	e.must(e.store.UpdateMediaMusic(ctx, id, tags, 10))
	base := "/api/media/" + strconv.FormatInt(id, 10) + "/lyrics"
	read := func(method, path, user string) service.Lyrics {
		t.Helper()
		rr := e.request(ctx, method, path, user, "")
		if rr.Code != 200 {
			t.Fatalf("%s %s: %d %s", method, path, rr.Code, rr.Body.String())
		}
		var value service.Lyrics
		e.must(json.Unmarshal(rr.Body.Bytes(), &value))
		return value
	}
	if value := read("GET", base, "alice"); !value.ProviderEnabled || len(value.Lines) != 0 || calls.Load() != 0 {
		t.Fatalf("GET launched provider: %+v", value)
	}
	for _, user := range []string{"", "bob"} {
		rr := e.request(ctx, "POST", base, user, "")
		if rr.Code != 401 && rr.Code != 403 {
			t.Fatal(rr.Code)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("unauthorized egress")
	}
	got := read("POST", base, "alice")
	if got.Source != "provider" || len(got.Lines) != 1 || len(got.Lines[0].Words) != 2 || got.Lines[0].Translations["fr"] != "Bonjour" {
		t.Fatalf("rich result: %+v", got)
	}
	read("GET", "/api/v1/media/"+strconv.FormatInt(id, 10)+"/lyrics", "alice")
	if calls.Load() != 1 {
		t.Fatal("GET refreshed")
	}
	if value := read("GET", base, "admin"); len(value.Lines) != 0 {
		t.Fatal("cross-account provider cache")
	}
	mode.Store(1)
	if value := read("POST", base, "alice"); value.Source != "provider" || value.ProviderState != "error" {
		t.Fatalf("failure destroyed cached lyrics: %+v", value)
	}
	// Same-size replacement with preserved mtime still changes ctime and bytes.
	info, err := os.Stat(source)
	e.must(err)
	e.must(os.WriteFile(source, []byte("other song"), 0600))
	e.must(os.Chtimes(source, info.ModTime(), info.ModTime()))
	if value := read("GET", base, "alice"); len(value.Lines) != 0 {
		t.Fatal("stale replacement lyrics")
	}
	mode.Store(0)
	read("POST", base, "alice")
	tags.Title = "Changed title"
	e.must(e.store.UpdateMediaMusic(ctx, id, tags, 10))
	if value := read("GET", base, "alice"); len(value.Lines) != 0 {
		t.Fatal("stale request tags")
	}
	mode.Store(2)
	if value := read("POST", base, "alice"); value.Source != "sidecar" || value.Lines[0].Text != "Local wins" {
		t.Fatalf("local priority race: %+v", value)
	}
	e.must(os.Remove(sidecar))
	mode.Store(3)
	if value := read("POST", base, "alice"); len(value.Lines) != 0 || value.ProviderState != "empty" {
		t.Fatalf("no match: %+v", value)
	}
	if value := read("DELETE", "/api/v1/media/"+strconv.FormatInt(id, 10)+"/lyrics", "alice"); len(value.Lines) != 0 {
		t.Fatal("remove")
	}
	before := calls.Load()
	read("GET", base, "alice")
	if calls.Load() != before {
		t.Fatal("removed cache triggered lookup")
	}
	revoke = func() {
		if err := e.store.RevokePermission(ctx, original.SetID, e.users["alice"]); err != nil {
			t.Error(err)
		}
	}
	mode.Store(0)
	read("POST", base, "alice")
	mode.Store(5)
	if value := read("POST", base, "alice"); value.Source != "sidecar" {
		t.Fatalf("failed lookup displaced new local lyrics: %+v", value)
	}
	e.must(os.Remove(sidecar))
	mode.Store(0)
	read("POST", base, "alice")
	replace = func() {
		if err := os.WriteFile(source, []byte("third song"), 0600); err != nil {
			t.Error(err)
		}
	}
	mode.Store(6)
	if value := read("POST", base, "alice"); value.Source == "provider" || len(value.Lines) > 0 {
		t.Fatalf("failed lookup reused replaced-source lyrics: %+v", value)
	}
	mode.Store(4)
	if rr := e.request(ctx, "POST", base, "alice", ""); rr.Code != 403 {
		t.Fatalf("revoked during lookup: %d %s", rr.Code, rr.Body.String())
	}
}
