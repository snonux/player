package app

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/ffsafe/ffsafetest"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/service"
)

func TestAudioFormatAPI(t *testing.T) {
	ffsafetest.RequireFFmpeg(t)
	e := newE2E(t, &fakeRunner{})
	ctx := context.Background()
	original, err := e.store.GetMediaByID(ctx, e.media["movie.mp4"])
	e.must(err)
	path := filepath.Join(filepath.Dir(original.AbsPath), "song.flac")
	ffsafetest.Generate(t, path, "-f", "lavfi", "-i", "sine=duration=0.2", "-ar", "48000", "-ac", "2", "-sample_fmt", "s16")
	id, err := e.store.CreateMedia(ctx, &model.Media{SetID: original.SetID, AbsPath: path, FileName: "song.flac", RelPath: "song.flac", Type: model.MediaTypeAudio, Codec: "flac", CreatedAt: time.Now()})
	e.must(err)
	e.media["song.flac"] = id
	base := "/api/media/" + strconv.FormatInt(id, 10) + "/format"
	check := func(body []byte) {
		t.Helper()
		var got service.AudioFormat
		e.must(json.Unmarshal(body, &got))
		if got.Codec != "flac" || got.CodecLabel != "FLAC" || got.SampleRate != 48000 || got.BitDepth != 16 ||
			got.Channels != 2 || !got.Lossless || got.Transcoded || got.Bitrate <= 0 {
			t.Fatalf("%+v", got)
		}
	}
	for _, tt := range []struct {
		path, user string
		code       int
	}{
		{base, "alice", 200}, {"/api/v1/media/" + strconv.FormatInt(id, 10) + "/format", "alice", 200}, {base, "admin", 200},
		{base, "bob", 403}, {base, "", 401}, {"/api/media/99999/format", "alice", 404}, {"/api/media/bad/format", "alice", 400},
	} {
		rr := e.get(tt.path, tt.user)
		if rr.Code != tt.code {
			t.Fatalf("%s %s: %d %s", tt.path, tt.user, rr.Code, rr.Body.String())
		}
		if rr.Code == 200 {
			check(rr.Body.Bytes())
		}
	}
	// Video answers an empty format rather than an error.
	rr := e.get("/api/media/"+strconv.FormatInt(e.media["movie.mp4"], 10)+"/format", "alice")
	if rr.Code != 200 || rr.Body.String() != "{\"lossless\":false,\"transcoded\":false}\n" {
		t.Fatalf("video: %d %q", rr.Code, rr.Body.String())
	}

	token := e.share("song.flac", "alice", `{"max_uses":1}`)
	sharePath := "/s/" + token + "/format"
	for range 2 {
		rr := e.shareDo("GET", sharePath, nil)
		if rr.Code != 200 || viewCookie(rr) != nil || e.usedCount(token) != 0 {
			t.Fatalf("probe spent use: %d", rr.Code)
		}
		check(rr.Body.Bytes())
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
		{sharePath, nil, 410}, {sharePath + "?view=" + meta.View, nil, 200}, {sharePath, http.Header{"Cookie": {"share_view=" + meta.View}}, 200},
	} {
		if rr := e.shareDo("GET", tt.path, tt.header); rr.Code != tt.code {
			t.Fatalf("share code %d want %d", rr.Code, tt.code)
		}
	}
	if e.usedCount(token) != 1 {
		t.Fatal("format spent extra use")
	}
	e.must(e.store.SoftDeleteMedia(ctx, id))
	if rr := e.get(base, "alice"); rr.Code != 404 {
		t.Fatal("deleted format", rr.Code)
	}
}
