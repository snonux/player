package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

func TestPlaylistImportFollowRoundtripAndAuthority(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "player.db")
	store, err := repository.Open(dbPath)
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
	id := func(id int64, err error) int64 { t.Helper(); must(err); return id }
	now := time.Now().UTC()
	owner := id(store.CreateUser(ctx, &model.User{Username: "owner", PasswordHash: "x", CreatedAt: now}))
	reader := id(store.CreateUser(ctx, &model.User{Username: "reader", PasswordHash: "x", CreatedAt: now}))
	admin := id(store.CreateUser(ctx, &model.User{Username: "admin", PasswordHash: "x", IsAdmin: true, CreatedAt: now}))
	set := id(store.CreateSet(ctx, &model.Set{Name: "Music", RootPath: "Music", CreatedAt: now}))
	secret := id(store.CreateSet(ctx, &model.Set{Name: "Secret", RootPath: "Secret", CreatedAt: now}))
	for _, u := range []int64{owner, reader} {
		must(store.GrantPermission(ctx, &model.SetPermission{UserID: u, SetID: set, Role: model.RoleViewer}))
	}
	write := func(rel, text string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		must(os.MkdirAll(filepath.Dir(p), 0755))
		must(os.WriteFile(p, []byte(text), 0644))
	}
	tracks := []int64{}
	for _, rel := range []string{"音楽/東京.mp3", "音楽/#song.mp3"} {
		write("Music/"+rel, "media")
		tracks = append(tracks, id(store.CreateMedia(ctx, &model.Media{SetID: set, RelPath: rel, AbsPath: filepath.Join(root, "Music", rel), FileName: filepath.Base(rel), Type: model.MediaTypeAudio, Music: &model.MusicMetadata{Title: rel, Album: "Album", Artist: "Artist"}, CreatedAt: now})))
	}
	write("Secret/secret.mp3", "secret")
	_ = id(store.CreateMedia(ctx, &model.Media{SetID: secret, RelPath: "secret.mp3", FileName: "secret.mp3", Type: model.MediaTypeAudio, Music: &model.MusicMetadata{Title: "secret", Album: "Secret", Artist: "Hidden"}, CreatedAt: now}))
	svc := NewPlaylistService(store, NewMediaService(store, &clock.MockClock{T: now}, root, nil, nil)).WithImportRoot(root)
	source := "Music/lists/trip.m3u8"
	write(source, "\uFEFF#EXTM3U\r\n#PLAYLIST:旅\r\n#EXTINF:99,Ignored\r\n../音楽/東京.mp3\r\n../音楽/#song.mp3\n../音楽/東京.mp3\nmissing.mp3\n../../Secret/secret.mp3\n../../../outside.mp3\nhttps://127.0.0.1/private\n/etc/passwd\n")
	req := PlaylistImportRequest{Path: source, Base: "folder", Preview: true, Follow: true}
	preview, err := svc.ImportM3U(ctx, owner, req)
	must(err)
	if preview.Name != "旅" || preview.Available != 3 || preview.Unavailable != 2 || preview.Rejected != 3 {
		t.Fatalf("preview %+v", preview)
	}
	req.Preview = false
	req.Fingerprint = preview.Fingerprint
	imported, err := svc.ImportM3U(ctx, owner, req)
	must(err)
	p := imported.Playlist
	if got := []int64{p.Entries[0].MediaID, p.Entries[1].MediaID, p.Entries[2].MediaID}; !reflect.DeepEqual(got, []int64{tracks[0], tracks[1], tracks[0]}) {
		t.Fatal(got)
	}
	original := append([]model.PlaylistEntry(nil), p.Entries...)
	must(svc.SyncSources(ctx))
	p, err = svc.Get(ctx, owner, p.ID)
	must(err)
	if !reflect.DeepEqual(original, p.Entries) {
		t.Fatal("unchanged sync replaced occurrence IDs")
	}
	must(store.Close())
	store, err = repository.Open(dbPath)
	must(err)
	svc = NewPlaylistService(store, NewMediaService(store, &clock.MockClock{T: now}, root, nil, nil)).WithImportRoot(root)
	p, err = svc.Get(ctx, owner, p.ID)
	must(err)
	if p.Source == nil || !reflect.DeepEqual(original, p.Entries) {
		t.Fatal("restart lost binding/occurrences")
	}
	exported, err := svc.ExportM3U(ctx, owner, p.ID)
	must(err)
	write("Music/lists/repeated.m3u8", exported)
	repeatReq := PlaylistImportRequest{Path: "Music/lists/repeated.m3u8", Base: "library", Preview: true}
	repeated, err := svc.ImportM3U(ctx, owner, repeatReq)
	must(err)
	repeatReq.Preview = false
	repeatReq.Fingerprint = repeated.Fingerprint
	repeated, err = svc.ImportM3U(ctx, owner, repeatReq)
	must(err)
	if repeated.Playlist.Source != nil || len(repeated.Playlist.Entries) != 3 || repeated.Playlist.Entries[0].MediaID != tracks[0] || repeated.Playlist.Entries[2].MediaID != tracks[0] {
		t.Fatal("repeated M32 roundtrip")
	}

	if _, err := svc.EditSongs(ctx, owner, p.ID, "remove", p.Entries[0].ID, "", nil); err == nil {
		t.Fatal("manual edit while following")
	}
	visibility := "shared"
	p, err = svc.UpdateDetails(ctx, owner, p.ID, "Manual name", "Manual comment", &visibility, nil)
	must(err)
	v, err := svc.Get(ctx, reader, p.ID)
	must(err)
	encoded, _ := json.Marshal(v)
	if v.Source != nil || strings.Contains(string(encoded), source) || strings.Contains(string(encoded), "Secret") {
		t.Fatal(string(encoded))
	}
	write(source, "#PLAYLIST:Changed\n../音楽/#song.mp3\n")
	must(svc.SyncSources(ctx))
	p, err = svc.Get(ctx, owner, p.ID)
	must(err)
	if p.Name != "Manual name" || p.Comment != "Manual comment" || len(p.Entries) != 1 || p.Entries[0].MediaID != tracks[1] {
		t.Fatalf("sync %+v", p)
	}
	must(os.Remove(filepath.Join(root, source)))
	p, err = svc.Get(ctx, owner, p.ID)
	must(err)
	if p.Source.Status != "unavailable" || len(p.Entries) != 1 {
		t.Fatal("missing source lost songs")
	}
	write(source, "../音楽/東京.mp3\n")
	must(store.RevokePermission(ctx, set, owner))
	must(svc.SyncSources(ctx))
	raw, err := store.GetPlaylist(ctx, admin, p.ID)
	must(err)
	if raw.Source.Status != "unavailable" || len(raw.Entries) != 1 {
		t.Fatal("lost source permission did not retain songs")
	}
	must(store.GrantPermission(ctx, &model.SetPermission{UserID: owner, SetID: set, Role: model.RoleViewer}))
	p, err = svc.Get(ctx, owner, p.ID)
	must(err)
	if p.Source.Status != "ready" || p.Entries[0].MediaID != tracks[0] {
		t.Fatal("source did not recover")
	}
	// M32 exports have an explicit library-root base and retain duplicate order.
	export, err := svc.ExportM3U(ctx, owner, p.ID)
	must(err)
	write("Music/lists/export.m3u8", export)
	round := PlaylistImportRequest{Path: "Music/lists/export.m3u8", Base: "library", Preview: true}
	r, err := svc.ImportM3U(ctx, owner, round)
	must(err)
	round.Preview = false
	round.Fingerprint = r.Fingerprint
	r, err = svc.ImportM3U(ctx, owner, round)
	must(err)
	if r.Playlist.Source != nil || r.Playlist.Songs[0].ID != tracks[0] {
		t.Fatal("one-time roundtrip")
	}
	// File changes after preview require review, not an unexpected import.
	write("Music/lists/export.m3u8", "../音楽/#song.mp3\n")
	if _, err := svc.ImportM3U(ctx, owner, round); err == nil {
		t.Fatal("stale preview accepted")
	}
	// Ownership transfer detaches atomically; stale scheduled refresh cannot rebind.
	stale, err := store.GetPlaylist(ctx, owner, p.ID)
	must(err)
	p, err = svc.UpdateDetails(ctx, admin, p.ID, p.Name, p.Comment, nil, &reader)
	must(err)
	if p.Source != nil {
		t.Fatal("transfer kept source authority")
	}
	must(svc.refreshSource(ctx, *stale))
	p, err = svc.Get(ctx, reader, p.ID)
	must(err)
	if p.Source != nil {
		t.Fatal("stale refresh rebound source")
	}
}

func TestPlaylistImportSourceSafety(t *testing.T) {
	root := t.TempDir()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "Music"), 0755))
	must(os.MkdirAll(filepath.Join(root, "Secret"), 0755))
	must(os.WriteFile(filepath.Join(root, "Secret", "hidden.m3u"), []byte("#PLAYLIST:Hidden title\n"), 0644))
	must(os.Symlink(filepath.Join(root, "Secret", "hidden.m3u"), filepath.Join(root, "Music", "link.m3u")))
	source := albumSource{sets: []model.Set{{ID: 1, RootPath: "Music"}}}
	svc := NewPlaylistService(&repository.MockStore{}, source).WithImportRoot(root)
	for _, file := range []string{"Music/link.m3u", "Secret/hidden.m3u", "../hidden.m3u", "/etc/hidden.m3u", "http://host/hidden.m3u", "Music/absent.m3u"} {
		_, err := svc.ImportM3U(context.Background(), 1, PlaylistImportRequest{Path: file, Base: "folder", Preview: true})
		if err == nil || strings.Contains(err.Error(), "Hidden title") {
			t.Fatalf("source %s: %v", file, err)
		}
	}
	for _, data := range []string{string([]byte{255}), strings.Repeat("x", 1024*1024+1)} {
		must(os.WriteFile(filepath.Join(root, "Music", "bad.m3u8"), []byte(data), 0644))
		if _, err := svc.ImportM3U(context.Background(), 1, PlaylistImportRequest{Path: "Music/bad.m3u8", Base: "folder", Preview: true}); err == nil {
			t.Fatal("invalid source accepted")
		}
	}
}

func TestResolveM3UPath(t *testing.T) {
	for _, test := range []struct{ entry, base, want string }{{"../音.mp3", "folder", "Music/音.mp3"}, {"./Music/音.mp3", "library", "Music/音.mp3"}, {"../../../escape", "folder", ""}, {"/absolute", "folder", ""}, {"https://host/file", "folder", ""}, {"file:///etc/passwd", "folder", ""}, {"a\\b", "folder", ""}, {"a\x00b", "folder", ""}} {
		got, ok := resolveM3UPath("Music/lists/a.m3u8", test.base, test.entry)
		if got != test.want || ok != (test.want != "") {
			t.Errorf("%+v = %q %v", test, got, ok)
		}
	}
}
