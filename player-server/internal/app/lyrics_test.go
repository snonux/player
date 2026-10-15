package app

import (
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/service"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestLyricsAPI(t *testing.T) {
	e := newE2E(t, &fakeRunner{})
	ctx := context.Background()
	original, err := e.store.GetMediaByID(ctx, e.media["movie.mp4"])
	e.must(err)
	path := filepath.Join(filepath.Dir(original.AbsPath), "song.mp3")
	e.must(os.WriteFile(path, []byte("fake audio"), 0600))
	id, err := e.store.CreateMedia(ctx, &model.Media{SetID: original.SetID, AbsPath: path, FileName: "song.mp3", RelPath: "song.mp3", Type: model.MediaTypeAudio, CreatedAt: time.Now()})
	e.must(err)
	e.media["song.mp3"] = id
	e.must(os.WriteFile(filepath.Join(filepath.Dir(path), "song.lrc"), []byte("[00:00]hello\n[00:01.5]world"), 0600))
	base := "/api/media/" + strconv.FormatInt(id, 10) + "/lyrics"
	for _, tt := range []struct {
		path, user string
		code       int
	}{
		{base, "alice", 200}, {"/api/v1/media/" + strconv.FormatInt(id, 10) + "/lyrics", "alice", 200}, {base, "admin", 200}, {base, "bob", 403}, {base, "", 401}, {"/api/media/99999/lyrics", "alice", 404}, {"/api/media/bad/lyrics", "alice", 400},
	} {
		rr := e.get(tt.path, tt.user)
		if rr.Code != tt.code {
			t.Fatalf("%s %s: %d %s", tt.path, tt.user, rr.Code, rr.Body.String())
		}
		if rr.Code == 200 {
			var got service.Lyrics
			e.must(json.Unmarshal(rr.Body.Bytes(), &got))
			if !got.Synced || got.Source != "sidecar" || len(got.Lines) != 2 || *got.Lines[1].Start != 1.5 {
				t.Fatalf("%+v", got)
			}
		}
	}
	token := e.share("song.mp3", "alice", `{"max_uses":1}`)
	sharePath := "/s/" + token + "/lyrics"
	for range 2 {
		rr := e.shareDo("GET", sharePath, nil)
		if rr.Code != 200 || viewCookie(rr) != nil || e.usedCount(token) != 0 {
			t.Fatalf("probe spent use: %d", rr.Code)
		}
	}
	meta, code := e.openJSON(token)
	if code != 200 {
		t.Fatal(code)
	}
	for _, tt := range []struct {
		path   string
		header http.Header
		code   int
	}{
		{sharePath, nil, 410}, {sharePath + "?view=invalid", nil, 410}, {sharePath + "?view=" + meta.View, nil, 200}, {sharePath, http.Header{"Cookie": {"share_view=" + meta.View}}, 200},
	} {
		rr := e.shareDo("GET", tt.path, tt.header)
		if rr.Code != tt.code {
			t.Fatalf("share code %d want %d", rr.Code, tt.code)
		}
	}
	if e.usedCount(token) != 1 {
		t.Fatal("lyrics spent extra use")
	}
	rr := e.request(ctx, "DELETE", "/api/shares/"+token, "alice", "")
	if rr.Code != 200 {
		t.Fatal(rr.Code)
	}
	if rr := e.shareDo("GET", sharePath+"?view="+meta.View, nil); rr.Code != 404 {
		t.Fatalf("revoked lyrics %d", rr.Code)
	}
	e.must(os.Remove(filepath.Join(filepath.Dir(path), "song.lrc")))
	rr = e.get(base, "alice")
	var got service.Lyrics
	e.must(json.Unmarshal(rr.Body.Bytes(), &got))
	if rr.Code != 200 || got.Lines == nil || len(got.Lines) != 0 || got.Source != "" || got.Synced {
		t.Fatalf("empty: %d %s", rr.Code, rr.Body.String())
	}
	e.must(e.store.SoftDeleteMedia(ctx, id))
	if rr := e.get(base, "alice"); rr.Code != 404 {
		t.Fatal("deleted lyrics", rr.Code)
	}
}
