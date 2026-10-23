package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
)

func TestPlaylistExportOrderPathsAndReadACL(t *testing.T) {
	ctx := context.Background()
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	mustID := func(id int64, err error) int64 {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	now := time.Now().UTC()
	owner := mustID(store.CreateUser(ctx, &model.User{Username: "owner", PasswordHash: "x", CreatedAt: now}))
	reader := mustID(store.CreateUser(ctx, &model.User{Username: "reader", PasswordHash: "x", CreatedAt: now}))
	music := mustID(store.CreateSet(ctx, &model.Set{Name: "Music", RootPath: "Music", CreatedAt: now}))
	hidden := mustID(store.CreateSet(ctx, &model.Set{Name: "Restricted", RootPath: "Secret", CreatedAt: now}))
	for _, user := range []int64{owner, reader} {
		if err := store.GrantPermission(ctx, &model.SetPermission{UserID: user, SetID: music, Role: model.RoleViewer}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.GrantPermission(ctx, &model.SetPermission{UserID: owner, SetID: hidden, Role: model.RoleViewer}); err != nil {
		t.Fatal(err)
	}
	ids := []int64{}
	for i, rel := range []string{"#first 音.mp3", "album/next.mp3", "restricted.mp3", "bad\n#EXTINF:999,injected.mp3"} {
		set := music
		if i == 2 {
			set = hidden
		}
		ids = append(ids, mustID(store.CreateMedia(ctx, &model.Media{SetID: set, RelPath: rel, FileName: rel, Type: model.MediaTypeAudio, Duration: 12.9, Music: &model.MusicMetadata{Title: []string{"東京\n#EXTINF:1,metadata", "Next", "Hidden name", "Bad"}[i], Artist: "Artist", Album: "Album"}, CreatedAt: now})))
	}
	source := NewMediaService(store, &clock.MockClock{T: now}, "", nil, nil)
	svc := NewPlaylistService(store, source)
	p, err := svc.Create(ctx, owner, "Trip\n#PLAYLIST:injected", "")
	if err != nil {
		t.Fatal(err)
	}
	entries := []model.PlaylistEntry{}
	for i, index := range []int{1, 2, 0, 1, 3} {
		entries = append(entries, model.PlaylistEntry{ID: []string{"a", "b", "c", "d", "e"}[i], MediaID: ids[index]})
	}
	if ok, err := store.AppendPlaylist(ctx, owner, p.ID, entries); err != nil || !ok {
		t.Fatalf("append: %v %v", ok, err)
	}
	if _, err := svc.ExportM3U(ctx, reader, p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("private foreign: %v", err)
	}
	visibility := "shared"
	if _, err := svc.UpdateDetails(ctx, owner, p.ID, p.Name, "", &visibility, nil); err != nil {
		t.Fatal(err)
	}
	content, err := svc.ExportM3U(ctx, reader, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := "#EXTINF:12,Artist - Next\n./Music/album/next.mp3\n#EXTINF:12,Artist - 東京 #EXTINF:1,metadata\n./Music/#first 音.mp3\n#EXTINF:12,Artist - Next\n./Music/album/next.mp3\n"
	if !strings.HasSuffix(content, want) {
		t.Fatalf("order/metadata/path:\n%s", content)
	}
	for _, secret := range []string{"Secret", "Hidden name", "restricted.mp3", "injected.mp3", "http:", "token="} {
		if strings.Contains(content, secret) {
			t.Fatalf("leaked %q:\n%s", secret, content)
		}
	}
	if strings.Count(content, "\n#PLAYLIST:") != 1 || strings.Count(content, "\n#EXTINF:") != 3 {
		t.Fatal(content)
	}
	empty, err := svc.Create(ctx, reader, "Empty", "")
	if err != nil {
		t.Fatal(err)
	}
	content, err = svc.ExportM3U(ctx, reader, empty.ID)
	if err != nil || !strings.HasPrefix(content, "#EXTM3U\n#PLAYLIST:Empty\n") || strings.Contains(content, "#EXTINF:") {
		t.Fatalf("empty %q %v", content, err)
	}
}

func TestM3ULocalPathSafety(t *testing.T) {
	for _, value := range []string{"", "/absolute", "../up", "a/../up", "a/./file", "a//file", "back\\slash.mp3", "a\rfile", "a\n#EXTINF:1,title", "a\x00.mp3", string([]byte{255})} {
		if m3uLocalPath(value) {
			t.Errorf("accepted unsafe %q", value)
		}
	}
	for _, value := range []string{"#song.mp3", "a/#song.mp3", "https:filename.mp3", "音/space name.mp3"} {
		if !m3uLocalPath(value) {
			t.Errorf("rejected local %q", value)
		}
	}
}
