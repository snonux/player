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

func TestMusicRelatedPermissions(t *testing.T) {
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
		r := httptest.NewRequest("GET", "/api/music/related/"+kind+"/"+key, nil)
		r.SetPathValue("kind", kind)
		r.SetPathValue("entity_key", key)
		r = r.WithContext(context.WithValue(ctx, sessionCtxKey, &model.Session{UserID: account}))
		w := httptest.NewRecorder()
		server.handleMusicRelated(w, r)
		return w
	}
	visible := request("song", strconv.FormatInt(ids[0], 10), user)
	var result []service.RelatedMusic
	if err := json.Unmarshal(visible.Body.Bytes(), &result); err != nil || visible.Code != 200 || len(result) != 1 || result[0].Title != "Visible" || strings.Contains(visible.Body.String(), "Secret") {
		t.Fatalf("ACL %d %s %v", visible.Code, visible.Body, err)
	}
	for _, tc := range []struct {
		kind, key string
		user      int64
		status    int
	}{
		{"song", strconv.FormatInt(ids[2], 10), user, 404},
		{"song", strconv.FormatInt(ids[0], 10), other, 404},
		{"playlist", "missing", user, 400},
	} {
		if got := request(tc.kind, tc.key, tc.user); got.Code != tc.status {
			t.Fatalf("status %d %s", got.Code, got.Body)
		}
	}
}
