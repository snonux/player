package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/snonux/player/internal/model"
)

func TestProviderLyricsPersistenceRetentionAndCleanup(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "lyrics.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	user, err := store.CreateUser(ctx, &model.User{Username: "lyrics", PasswordHash: "x", CreatedAt: now})
	must(err)
	set, err := store.CreateSet(ctx, &model.Set{Name: "Music", RootPath: "/music", CreatedAt: now})
	must(err)
	create := func(n int) int64 {
		t.Helper()
		id, err := store.CreateMedia(ctx, &model.Media{SetID: set, Type: model.MediaTypeAudio, FileName: "song.mp3", AbsPath: "/music/" + time.Unix(int64(n), 0).String(), RelPath: time.Unix(int64(n), 0).String(), CreatedAt: now})
		must(err)
		return id
	}
	media := create(0)
	must(store.SaveProviderLyrics(ctx, user, media, "content-tags-provider", []byte(`{"lines":[{"text":"Retained"}]}`)))
	must(store.Close())
	store, err = Open(path)
	must(err)
	defer func() { _ = store.Close() }()
	data, err := store.LoadProviderLyrics(ctx, user, media, "content-tags-provider")
	must(err)
	if len(data) == 0 {
		t.Fatal("restart lost data")
	}
	for _, scope := range []struct {
		user, media int64
		identity    string
	}{{user + 1, media, "content-tags-provider"}, {user, media, "changed-provider"}, {user, media + 1, "content-tags-provider"}} {
		data, err = store.LoadProviderLyrics(ctx, scope.user, scope.media, scope.identity)
		must(err)
		if len(data) != 0 {
			t.Fatal("namespace leaked")
		}
	}
	_, err = store.db.ExecContext(ctx, `UPDATE provider_lyrics SET fetched_at=?`, time.Now().Add(-31*24*time.Hour))
	must(err)
	found, err := store.HasProviderLyrics(ctx, user, media)
	must(err)
	if found {
		t.Fatal("expired entry")
	}
	var count int
	must(store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM provider_lyrics`).Scan(&count))
	if count != 0 {
		t.Fatal("expired bytes retained")
	}
	for n := 1; n <= 130; n++ {
		id := create(n)
		must(store.SaveProviderLyrics(ctx, user, id, "identity", []byte(`{"lines":[]}`)))
	}
	must(store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM provider_lyrics`).Scan(&count))
	if count != 128 {
		t.Fatalf("retention count %d", count)
	}
	must(store.SaveProviderLyrics(ctx, user, media, "identity", []byte(`{"lines":[]}`)))
	must(store.HardDeleteMedia(ctx, media))
	found, err = store.HasProviderLyrics(ctx, user, media)
	must(err)
	if found {
		t.Fatal("media deletion leaked lyrics")
	}
	must(store.DeleteUser(ctx, user))
	must(store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM provider_lyrics`).Scan(&count))
	if count != 0 {
		t.Fatal("user deletion leaked lyrics")
	}
}
