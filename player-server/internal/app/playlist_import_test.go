package app

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/snonux/player/internal"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
	"github.com/snonux/player/internal/service"
)

func TestPlaylistImportRescanWiring(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	user, err := store.CreateUser(ctx, &model.User{Username: "owner", PasswordHash: "x", CreatedAt: time.Now().UTC()})
	must(err)
	set, err := store.CreateSet(ctx, &model.Set{Name: "Music", RootPath: "Music", CreatedAt: time.Now().UTC()})
	must(err)
	must(store.GrantPermission(ctx, &model.SetPermission{SetID: set, UserID: user, Role: model.RoleViewer}))
	must(os.MkdirAll(filepath.Join(root, "Music"), 0755))
	source := filepath.Join(root, "Music", "source.m3u8")
	must(os.WriteFile(source, []byte("#PLAYLIST:Scan\n"), 0644))
	deps := WireWithRunner(&internal.Config{MediaRoot: root, DBPath: filepath.Join(t.TempDir(), "player.db"), TranscodeMaxJobs: 1}, store, slog.New(slog.NewTextHandler(io.Discard, nil)), ctx, &fakeRunner{})
	req := service.PlaylistImportRequest{Path: "Music/source.m3u8", Base: "folder", Follow: true, Preview: true}
	preview, err := deps.Playlists.ImportM3U(ctx, user, req)
	must(err)
	req.Preview = false
	req.Fingerprint = preview.Fingerprint
	imported, err := deps.Playlists.ImportM3U(ctx, user, req)
	must(err)
	must(os.WriteFile(source, []byte("missing.mp3\n"), 0644))
	must(deps.Scanner.Scan(ctx, root, nil))
	// Read repository directly: an owner Get would itself refresh and conceal
	// a missing scanner completion hook.
	p, err := store.GetPlaylist(ctx, user, imported.Playlist.ID)
	must(err)
	if p.Source == nil || p.Source.Status != "ready" || p.Source.Unavailable != 1 {
		t.Fatalf("rescan did not refresh source: %+v", p.Source)
	}
}
