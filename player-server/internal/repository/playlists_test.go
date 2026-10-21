package repository

import (
	"context"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/model"
)

func TestPlaylistAppendRollbackAndCascades(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	mustID := func(id int64, e error) int64 {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
		return id
	}
	now := time.Now().UTC()
	user := mustID(s.CreateUser(ctx, &model.User{Username: "owner", PasswordHash: "x", CreatedAt: now}))
	set := mustID(s.CreateSet(ctx, &model.Set{Name: "Music", RootPath: "Music", CreatedAt: now}))
	media := mustID(s.CreateMedia(ctx, &model.Media{SetID: set, FileName: "song.mp3", RelPath: "song.mp3", Type: model.MediaTypeAudio, CreatedAt: now}))
	p := model.Playlist{ID: "pl_test", Name: "Test"}
	if err := s.CreatePlaylist(ctx, user, p); err != nil {
		t.Fatal(err)
	}
	// A bad last entry must not retain the valid prefix or consume its position.
	if ok, err := s.AppendPlaylist(ctx, user, p.ID, []model.PlaylistEntry{{ID: "e1", MediaID: media}, {ID: "e2", MediaID: 9999}}); err == nil || ok {
		t.Fatalf("bad block committed %v %v", ok, err)
	}
	got, err := s.GetPlaylist(ctx, user, p.ID)
	if err != nil || len(got.Entries) != 0 {
		t.Fatalf("rollback %+v %v", got, err)
	}
	if ok, err := s.AppendPlaylist(ctx, user, p.ID, []model.PlaylistEntry{{ID: "e1", MediaID: media}}); err != nil || !ok {
		t.Fatalf("good append %v %v", ok, err)
	}
	if err := s.HardDeleteMedia(ctx, media); err != nil {
		t.Fatal(err)
	}
	var left int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM playlist_entries`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("entries remain=%d err=%v", left, err)
	}
	got, err = s.GetPlaylist(ctx, user, p.ID)
	if err != nil || len(got.Entries) != 0 {
		t.Fatalf("media cascade %+v %v", got, err)
	}
	// Legacy connections without cascades must still remove nested entries.
	if ok, err := s.AppendPlaylist(ctx, user, p.ID, []model.PlaylistEntry{}); err != nil || !ok {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO playlist_entries(id,playlist_id,media_id,position) VALUES('orphan',?,9999,1)`, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM playlist_entries`).Scan(&left); err != nil || left != 0 {
		t.Fatalf("entries remain=%d err=%v", left, err)
	}
	got, err = s.GetPlaylist(ctx, user, p.ID)
	if err != nil || got != nil {
		t.Fatalf("user cascade %+v %v", got, err)
	}
}
