package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
)

func TestMusicHistoryReplayRetryRetentionAndRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "history.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	clk := &clock.MockClock{T: now}
	store = store.WithClock(clk)
	user, err := store.CreateUser(ctx, &model.User{Username: "first", PasswordHash: "x", IsAdmin: true, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateUser(ctx, &model.User{Username: "second", PasswordHash: "x", IsAdmin: true, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	set, err := store.CreateSet(ctx, &model.Set{Name: "Music", RootPath: "/music", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	media, err := store.CreateMedia(ctx, &model.Media{SetID: set, Type: model.MediaTypeAudio, FileName: "song.mp3", AbsPath: "/music/song.mp3", RelPath: "song.mp3", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	play := func(id string, at time.Time, owner int64) {
		t.Helper()
		if err := mergeMusicPlay(ctx, store.db, owner, media, model.SyncedMusicPlay{ID: id, ObservedMS: at.UnixMilli(), AccumulatedSeconds: 60}); err != nil {
			t.Fatal(err)
		}
	}
	history := func(want int) {
		t.Helper()
		rows, err := store.ListMusicHistory(ctx, user, 0, 0, 50)
		if err != nil || len(rows) != want {
			t.Fatalf("history: %+v %v want %d", rows, err, want)
		}
	}
	play("first", now.Add(-time.Hour), user)
	play("first", now.Add(-time.Hour), user)
	play("replay", now, user)
	play("late", now.Add(-2*time.Hour), user)
	play("first", now, other)
	history(3)
	rows, err := store.ListMusicHistory(ctx, user, 0, 0, 2)
	if err != nil || len(rows) != 2 || !rows[0].PlayedAt.Equal(now) {
		t.Fatalf("ordered %+v %v", rows, err)
	}
	next, err := store.ListMusicHistory(ctx, user, rows[1].PlayedAt.UnixMilli(), rows[1].ID, 2)
	if err != nil || len(next) != 1 || !next[0].PlayedAt.Equal(now.Add(-2*time.Hour)) {
		t.Fatalf("page %+v %v", next, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	store = store.WithClock(clk)
	defer func() { _ = store.Close() }()
	history(3)
	if err := store.ClearMusicHistory(ctx, user); err != nil {
		t.Fatal(err)
	}
	history(0)
	play("replay", now, user)
	play("delayed-before-clear", now.Add(-time.Second), user)
	history(0)
	clk.T = now.Add(time.Second)
	play("after-clear", clk.T, user)
	history(1)
	settings := model.MusicHistorySettings{Recording: false, RetentionDays: 30}
	if err := store.SetMusicHistorySettings(ctx, user, settings); err != nil {
		t.Fatal(err)
	}
	clk.T = now.Add(2 * time.Second)
	play("paused", clk.T, user)
	history(1)
	clk.T = now.Add(3 * time.Second)
	settings.Recording = true
	if err := store.SetMusicHistorySettings(ctx, user, settings); err != nil {
		t.Fatal(err)
	}
	play("late-paused", now.Add(2*time.Second), user)
	history(1)
	clk.T = now.Add(40 * 24 * time.Hour)
	if err := store.SetMusicHistorySettings(ctx, user, settings); err != nil {
		t.Fatal(err)
	}
	history(0)
	play("after-clear", now.Add(time.Second), user)
	history(0)
	stats, err := store.ListMusicListening(ctx, user)
	if err != nil || stats[media].PersonalPlayCount != 7 {
		t.Fatalf("counter ledger altered %+v %v", stats, err)
	}
	otherRows, err := store.ListMusicHistory(ctx, other, 0, 0, 50)
	if err != nil || len(otherRows) != 1 {
		t.Fatalf("account isolation %+v %v", otherRows, err)
	}
	// Permission revocation hides a row before its title is loaded.
	if _, err := store.db.ExecContext(ctx, `UPDATE users SET is_admin=0 WHERE id=?`, other); err != nil {
		t.Fatal(err)
	}
	otherRows, err = store.ListMusicHistory(ctx, other, 0, 0, 50)
	if err != nil || len(otherRows) != 0 {
		t.Fatalf("revoked %+v %v", otherRows, err)
	}
	if err := store.DeleteUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	history(0)
}
