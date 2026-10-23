package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
	"github.com/snonux/player/internal/service"
)

func TestMusicMixPermissionsAndPersonalRating(t *testing.T) {
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	now := time.Now().UTC()
	user, err := store.CreateUser(ctx, &model.User{Username: "viewer", PasswordHash: "x", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateUser(ctx, &model.User{Username: "other", PasswordHash: "x", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	ids := []int64{}
	for i := 0; i < 3; i++ {
		set, err := store.CreateSet(ctx, &model.Set{Name: strconv.Itoa(i), RootPath: strconv.Itoa(i), CreatedAt: now})
		if err != nil {
			t.Fatal(err)
		}
		if i < 2 {
			if err := store.GrantPermission(ctx, &model.SetPermission{SetID: set, UserID: user, Role: model.RoleViewer}); err != nil {
				t.Fatal(err)
			}
		}
		title := []string{"Source", "Visible", "Secret"}[i]
		id, err := store.CreateMedia(ctx, &model.Media{SetID: set, FileName: title + ".mp3", RelPath: title + ".mp3", Type: model.MediaTypeAudio, CreatedAt: now, Music: &model.MusicMetadata{Title: title, Album: title, Artist: title, Genres: []string{"Jazz"}}})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	browse := service.NewMediaService(store, &clock.MockClock{T: now}, "", nil, nil)
	server := &Server{store: store, media: MediaServices{Browse: browse}}
	request := func(kind, key string, account int64) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/music/mix/"+kind+"/"+key, nil)
		r.SetPathValue("kind", kind)
		r.SetPathValue("entity_key", strings.Split(key, "?")[0])
		r = r.WithContext(context.WithValue(ctx, sessionCtxKey, &model.Session{UserID: account}))
		w := httptest.NewRecorder()
		server.handleMusicMix(w, r)
		return w
	}
	if err := store.SetMusicRating(ctx, user, "song", strconv.FormatInt(ids[1], 10), 5); err != nil {
		t.Fatal(err)
	}
	if err := store.SetMusicRating(ctx, other, "song", strconv.FormatInt(ids[0], 10), 5); err != nil {
		t.Fatal(err)
	}
	visible := request("song", strconv.FormatInt(ids[0], 10), user)
	var result service.MusicMix
	if err := json.Unmarshal(visible.Body.Bytes(), &result); err != nil || visible.Code != 200 || len(result.Songs) != 2 || result.Songs[0].ID != ids[0] || result.Songs[1].ID != ids[1] || strings.Contains(visible.Body.String(), "Secret") {
		t.Fatalf("ACL %d %s %v", visible.Code, visible.Body, err)
	}
	filtered := request("song", strconv.FormatInt(ids[0], 10)+"?min_rating=5", user)
	if err := json.Unmarshal(filtered.Body.Bytes(), &result); err != nil || filtered.Code != 200 || len(result.Songs) != 1 || result.Songs[0].ID != ids[1] {
		t.Fatalf("personal rating %d %s", filtered.Code, filtered.Body)
	}
	if err := store.SetMusicRating(ctx, user, "song", strconv.FormatInt(ids[1], 10), 0); err != nil {
		t.Fatal(err)
	}
	empty := request("song", strconv.FormatInt(ids[1], 10)+"?min_rating=5", user)
	if err := json.Unmarshal(empty.Body.Bytes(), &result); err != nil || empty.Code != 200 || len(result.Songs) != 0 {
		t.Fatalf("seed filtered %s", empty.Body)
	}
	for _, tc := range []struct {
		kind, key string
		user      int64
		status    int
	}{
		{"song", strconv.FormatInt(ids[2], 10), user, 404},
		{"song", strconv.FormatInt(ids[0], 10), other, 404},
		{"playlist", "missing", user, 400},
		{"song", strconv.FormatInt(ids[0], 10) + "?min_rating=6", user, 400},
		{"song", strconv.FormatInt(ids[0], 10) + "?min_rating=bad", user, 400},
		{"song", strconv.FormatInt(ids[0], 10) + "?min_rating=", user, 400},
	} {
		if got := request(tc.kind, tc.key, tc.user); got.Code != tc.status {
			t.Fatalf("status %d %s", got.Code, got.Body)
		}
	}
}
