package service

import (
	"context"
	"testing"
	"time"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
)

func TestExplicitListeningOccurrencesPreserveDelayedReplay(t *testing.T) {
	ctx := context.Background()
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	now := time.Now().UTC().Truncate(time.Millisecond)
	clk := &clock.MockClock{T: now}
	svc := NewProgressService(store, clk)
	user, err := store.CreateUser(ctx, &model.User{Username: "listener", PasswordHash: "x", IsAdmin: true, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	set, err := store.CreateSet(ctx, &model.Set{Name: "Music", RootPath: "/music", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	media, err := store.CreateMedia(ctx, &model.Media{SetID: set, Type: model.MediaTypeAudio, FileName: "song.mp3", RelPath: "song.mp3", AbsPath: "/music/song.mp3", CreatedAt: now, Music: &model.MusicMetadata{Title: "Song", Artist: "Artist", Album: "Album"}})
	if err != nil {
		t.Fatal(err)
	}
	// The second play arrives first. The older occurrence must still qualify
	// even though its positions lose to the newer resume register.
	for _, occurrence := range []struct {
		id     string
		offset time.Duration
	}{{"second-occurrence-1", time.Minute}, {"first-occurrence-1", 0}, {"first-occurrence-1", 0}} {
		updates := []ProgressUpdate{}
		for i := 0; i <= 6; i++ {
			updates = append(updates, ProgressUpdate{MediaID: media, PositionSeconds: float64(i * 10), ObservedAt: now.Add(occurrence.offset + time.Duration(i)*time.Second), ListeningSessionID: occurrence.id})
		}
		if err := svc.BatchUpdateProgress(ctx, "same-login", user, updates); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := store.ListMusicListening(ctx, user)
	if err != nil || stats[media].PersonalPlayCount != 2 {
		t.Fatalf("replayed counter %+v %v", stats, err)
	}
	result, err := MusicHistory(ctx, store, NewMediaService(store, clk, "/music", nil, nil), user, 0, 0)
	if err != nil || len(result.Events) != 2 || result.Events[0].Title != "Song" || !result.Events[0].PlayedAt.After(result.Events[1].PlayedAt) {
		t.Fatalf("replayed history %+v %v", result, err)
	}
	if err := svc.BatchUpdateProgress(ctx, "same-login", user, []ProgressUpdate{{MediaID: media, ListeningSessionID: "bad"}}); err == nil {
		t.Fatal("invalid occurrence ID accepted")
	}
	// The old authentication-session accumulator must not receive a personal
	// play in addition to each explicit occurrence.
	if err := store.ClearMusicHistory(ctx, user); err != nil {
		t.Fatal(err)
	}
	if err := svc.BatchUpdateProgress(ctx, "same-login", user, []ProgressUpdate{{MediaID: media, ListeningSessionID: "first-occurrence-1", ObservedAt: now.Add(2 * time.Minute), PositionSeconds: 100}}); err != nil {
		t.Fatal(err)
	}
	rows, err := store.ListMusicHistory(ctx, user, 0, 0, 50)
	if err != nil || len(rows) != 0 {
		t.Fatalf("cleared replay reappeared %+v %v", rows, err)
	}
}
