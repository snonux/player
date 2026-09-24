package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

func TestProgressService_ContinueWatchingSurvivesSessionChanges(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	for _, scenario := range []struct {
		name  string
		clear func(context.Context, *repository.SQLite, time.Time) error
	}{
		{"logout", func(ctx context.Context, store *repository.SQLite, _ time.Time) error {
			return store.DeleteSession(ctx, "first")
		}},
		{"expired session GC", func(ctx context.Context, store *repository.SQLite, now time.Time) error {
			return store.DeleteExpiredSessions(ctx, now.Add(2*time.Hour))
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			store, err := repository.Open(":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = store.Close() }()
			userID, err := store.CreateUser(ctx, &model.User{Username: "viewer", PasswordHash: "hash", IsAdmin: true, CreatedAt: now})
			if err != nil {
				t.Fatal(err)
			}
			setID, err := store.CreateSet(ctx, &model.Set{Name: "set", RootPath: "/set", CreatedAt: now})
			if err != nil {
				t.Fatal(err)
			}
			mediaID, err := store.CreateMedia(ctx, &model.Media{SetID: setID, RelPath: "audio.mp3", FileName: "audio.mp3", AbsPath: "/set/audio.mp3", Type: model.MediaTypeAudio, CreatedAt: now})
			if err != nil {
				t.Fatal(err)
			}
			if err := store.CreateSession(ctx, &model.Session{ID: "first", UserID: userID, ExpiresAt: now.Add(time.Hour), CreatedAt: now}); err != nil {
				t.Fatal(err)
			}
			svc := NewProgressService(store, &clock.MockClock{T: now})
			for _, position := range []float64{0, 10, 20, 30, 40, 50, 60} {
				if err := svc.UpdateProgress(ctx, "first", userID, mediaID, position); err != nil {
					t.Fatal(err)
				}
			}
			if err := scenario.clear(ctx, store, now); err != nil {
				t.Fatal(err)
			}
			if err := store.CreateSession(ctx, &model.Session{ID: "second", UserID: userID, ExpiresAt: now.Add(3 * time.Hour), CreatedAt: now.Add(2 * time.Hour)}); err != nil {
				t.Fatal(err)
			}
			list, err := svc.ListInProgress(ctx, userID)
			if err != nil || len(list) != 1 || list[0].ID != mediaID {
				t.Fatalf("continue watching after session change: media=%+v err=%v", list, err)
			}
			progress, err := store.GetProgress(ctx, userID, mediaID)
			if err != nil || progress.PositionSeconds != 60 || progress.AccumulatedSeconds != 60 {
				t.Fatalf("saved progress: %+v err=%v", progress, err)
			}
		})
	}
}

func TestProgressService_ContinueWatchingCombinesShortSessions(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	userID, err := store.CreateUser(ctx, &model.User{Username: "viewer", PasswordHash: "hash", IsAdmin: true, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	setID, err := store.CreateSet(ctx, &model.Set{Name: "set", RootPath: "/set", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	mediaID, err := store.CreateMedia(ctx, &model.Media{SetID: setID, RelPath: "audio.mp3", FileName: "audio.mp3", AbsPath: "/set/audio.mp3", Type: model.MediaTypeAudio, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewProgressService(store, &clock.MockClock{T: now})
	for _, session := range []struct {
		id        string
		positions []float64
	}{
		{"first", []float64{0, 10, 20, 30, 40, 50}},
		{"second", []float64{50, 60}},
	} {
		if err := store.CreateSession(ctx, &model.Session{ID: session.id, UserID: userID, ExpiresAt: now.Add(time.Hour), CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
		for _, position := range session.positions {
			if err := svc.UpdateProgress(ctx, session.id, userID, mediaID, position); err != nil {
				t.Fatal(err)
			}
			if session.id == "second" && position == 50 {
				progress, err := store.GetProgress(ctx, userID, mediaID)
				if err != nil || progress.AccumulatedSeconds != 50 {
					t.Fatalf("unchanged resume position should not add playback: progress=%+v err=%v", progress, err)
				}
			}
		}
		if session.id == "first" {
			list, err := svc.ListInProgress(ctx, userID)
			if err != nil || len(list) != 0 {
				t.Fatalf("expected below threshold: media=%+v err=%v", list, err)
			}
			if err := store.DeleteSession(ctx, session.id); err != nil {
				t.Fatal(err)
			}
		}
	}
	list, err := svc.ListInProgress(ctx, userID)
	if err != nil || len(list) != 1 || list[0].ID != mediaID {
		t.Fatalf("expected combined playback to qualify: media=%+v err=%v", list, err)
	}
}

func TestProgressService_ContinueWatchingSurvivesTokenRotation(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	userID, err := store.CreateUser(ctx, &model.User{Username: "viewer", PasswordHash: "hash", IsAdmin: true, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	setID, err := store.CreateSet(ctx, &model.Set{Name: "set", RootPath: "/set", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	mediaID, err := store.CreateMedia(ctx, &model.Media{SetID: setID, RelPath: "audio.mp3", FileName: "audio.mp3", AbsPath: "/set/audio.mp3", Type: model.MediaTypeAudio, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewProgressService(store, &clock.MockClock{T: now})
	for i, positions := range [][]float64{{0, 10, 20, 30}, {30, 40, 50, 60}} {
		tokenID, err := store.Create(ctx, &model.APIToken{UserID: userID, TokenHash: fmt.Sprintf("token-%d", i), Name: "mobile", CreatedAt: now})
		if err != nil {
			t.Fatal(err)
		}
		sessionID := fmt.Sprintf("api-token:%d", tokenID)
		for _, position := range positions {
			if err := svc.UpdateProgress(ctx, sessionID, userID, mediaID, position); err != nil {
				t.Fatalf("bearer progress update: %v", err)
			}
		}
		if err := store.DeleteByID(ctx, tokenID); err != nil {
			t.Fatal(err)
		}
		acc, err := store.GetAccumulator(ctx, sessionID, mediaID)
		if err != nil || acc != nil {
			t.Fatalf("revoked token accumulator should be removed: acc=%+v err=%v", acc, err)
		}
	}
	list, err := svc.ListInProgress(ctx, userID)
	if err != nil || len(list) != 1 || list[0].ID != mediaID {
		t.Fatalf("expected continue watching after token rotation: media=%+v err=%v", list, err)
	}
	if err := svc.MarkFinished(ctx, userID, mediaID); err != nil {
		t.Fatal(err)
	}
	replayTokenID, err := store.Create(ctx, &model.APIToken{UserID: userID, TokenHash: "replay-token", Name: "mobile", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	replaySession := fmt.Sprintf("api-token:%d", replayTokenID)
	for _, position := range []float64{10, 20, 30, 40, 50} {
		if err := svc.UpdateProgress(ctx, replaySession, userID, mediaID, position); err != nil {
			t.Fatal(err)
		}
	}
	list, err = svc.ListInProgress(ctx, userID)
	if err != nil || len(list) != 0 {
		t.Fatalf("replay below 60 seconds should remain ineligible: media=%+v err=%v", list, err)
	}
	if err := svc.UpdateProgress(ctx, replaySession, userID, mediaID, 60); err != nil {
		t.Fatal(err)
	}
	list, err = svc.ListInProgress(ctx, userID)
	if err != nil || len(list) != 1 {
		t.Fatalf("replay at 60 seconds should qualify: media=%+v err=%v", list, err)
	}
	if err := svc.MarkFinished(ctx, userID, mediaID); err != nil {
		t.Fatal(err)
	}
	for _, position := range []float64{10, 20, 30, 40, 50, 60} {
		if err := svc.UpdateProgress(ctx, replaySession, userID, mediaID, position); err != nil {
			t.Fatal(err)
		}
	}
	list, err = svc.ListInProgress(ctx, userID)
	if err != nil || len(list) != 1 {
		t.Fatalf("same-token replay starting at 10 should qualify at 60: media=%+v err=%v", list, err)
	}
	media, err := store.GetMediaByID(ctx, mediaID)
	if err != nil || media.PlayCount != 2 {
		t.Fatalf("same-token replay should count twice: media=%+v err=%v", media, err)
	}
	if err := svc.MarkNotStarted(ctx, userID, mediaID); err != nil {
		t.Fatal(err)
	}
	replayAcc, err := store.GetAccumulator(ctx, replaySession, mediaID)
	if err != nil || replayAcc != nil {
		t.Fatalf("reset should remove token accumulator: acc=%+v err=%v", replayAcc, err)
	}
	list, err = svc.ListInProgress(ctx, userID)
	if err != nil || len(list) != 0 {
		t.Fatalf("mark not started should clear eligibility: media=%+v err=%v", list, err)
	}
}

func TestProgressService_ResetDoesNotClearAnotherUsersCounter(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	setID, err := store.CreateSet(ctx, &model.Set{Name: "set", RootPath: "/set", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	mediaID, err := store.CreateMedia(ctx, &model.Media{SetID: setID, RelPath: "audio.mp3", FileName: "audio.mp3", AbsPath: "/set/audio.mp3", Type: model.MediaTypeAudio, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewProgressService(store, &clock.MockClock{T: now})
	var userIDs []int64
	for _, username := range []string{"reset", "other"} {
		userID, err := store.CreateUser(ctx, &model.User{Username: username, PasswordHash: "hash", IsAdmin: true, CreatedAt: now})
		if err != nil {
			t.Fatal(err)
		}
		userIDs = append(userIDs, userID)
		if err := store.CreateSession(ctx, &model.Session{ID: username, UserID: userID, ExpiresAt: now.Add(time.Hour), CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
		for _, position := range []float64{0, 10, 20, 30, 40, 50} {
			if err := svc.UpdateProgress(ctx, username, userID, mediaID, position); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := svc.MarkFinished(ctx, userIDs[0], mediaID); err != nil {
		t.Fatal(err)
	}
	otherAcc, err := store.GetAccumulator(ctx, "other", mediaID)
	if err != nil || otherAcc == nil || otherAcc.AccumulatedSeconds != 50 {
		t.Fatalf("other user's accumulator changed on finish: acc=%+v err=%v", otherAcc, err)
	}
	if err := svc.MarkNotStarted(ctx, userIDs[0], mediaID); err != nil {
		t.Fatal(err)
	}
	otherAcc, err = store.GetAccumulator(ctx, "other", mediaID)
	if err != nil || otherAcc == nil || otherAcc.AccumulatedSeconds != 50 {
		t.Fatalf("other user's accumulator changed: acc=%+v err=%v", otherAcc, err)
	}
	if err := svc.UpdateProgress(ctx, "other", userIDs[1], mediaID, 60); err != nil {
		t.Fatal(err)
	}
	list, err := svc.ListInProgress(ctx, userIDs[1])
	if err != nil || len(list) != 1 {
		t.Fatalf("other user should qualify after 60 seconds: media=%+v err=%v", list, err)
	}
	media, err := store.GetMediaByID(ctx, mediaID)
	if err != nil || media.PlayCount != 1 {
		t.Fatalf("other user's play should count: media=%+v err=%v", media, err)
	}
	resetAcc, err := store.GetAccumulator(ctx, "reset", mediaID)
	if err != nil || resetAcc != nil {
		t.Fatalf("reset user's accumulator should be gone: acc=%+v err=%v", resetAcc, err)
	}
}
