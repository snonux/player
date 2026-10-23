package service

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/snonux/player/internal/clock"
	"github.com/snonux/player/internal/model"
	"github.com/snonux/player/internal/repository"
)

// Nonfinite float values cannot be represented as JSON, so also exercise the
// service directly to ensure other callers cannot persist them.
func TestBookmarkServiceRejectsNonfinitePositions(t *testing.T) {
	ctx := context.Background()
	store, err := repository.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	userID, err := store.CreateUser(ctx, &model.User{Username: "admin", PasswordHash: "h", IsAdmin: true})
	if err != nil {
		t.Fatal(err)
	}
	setID, err := store.CreateSet(ctx, &model.Set{Name: "books", RootPath: "/books"})
	if err != nil {
		t.Fatal(err)
	}
	mediaID, err := store.CreateMedia(ctx, &model.Media{SetID: setID, AbsPath: "/books/book.mp3", RelPath: "book.mp3", FileName: "book.mp3", Type: model.MediaTypeAudio})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewBookmarkService(store, clock.RealClock{}, NewAccessHelper(store))
	for _, position := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -0.001} {
		if _, err := svc.CreateBookmark(ctx, mediaID, userID, position, "chapter"); !errors.Is(err, ErrInvalidBookmark) {
			t.Fatalf("position %v returned %v", position, err)
		}
	}
	if _, err := svc.CreateBookmark(ctx, mediaID, userID, 1, string([]byte{0xff})); !errors.Is(err, ErrInvalidBookmark) {
		t.Fatalf("invalid UTF-8 label returned %v", err)
	}
	items, err := store.ListBookmarks(ctx, mediaID, userID)
	if err != nil || len(items) != 0 {
		t.Fatalf("invalid positions persisted: %+v, %v", items, err)
	}
}
