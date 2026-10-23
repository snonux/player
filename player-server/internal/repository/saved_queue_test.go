package repository

import (
	"github.com/snonux/player/internal/model"
	"context"
	"path/filepath"
	"testing"
)

func TestSavedQueueRevisionRetryIsolationRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	owner := seedOwned(t, s, "queue")
	other := seedOwned(t, s, "other")
	ctx := context.Background()
	q := model.SavedQueue{MutationID: "first", Entries: []model.QueueOccurrence{{ID: 1, MediaID: owner.mediaID}, {ID: 2, MediaID: owner.mediaID}}, CurrentID: 2, Position: 7.25, Repeat: "off", Canonical: []int64{1, 2}, UpNext: []int64{}}
	got, ok, err := s.PutSavedQueue(ctx, owner.userID, 0, q)
	if err != nil || !ok || got.Revision != 1 {
		t.Fatalf("first save: %+v %v %v", got, ok, err)
	}
	got, ok, err = s.PutSavedQueue(ctx, owner.userID, 0, q)
	if err != nil || !ok || got.Revision != 1 {
		t.Fatalf("retry: %+v %v %v", got, ok, err)
	}
	q.MutationID = "stale"
	q.Position = 1
	got, ok, err = s.PutSavedQueue(ctx, owner.userID, 0, q)
	if err != nil || ok || got.Position != 7.25 {
		t.Fatalf("stale overwritten: %+v %v %v", got, ok, err)
	}
	isolated, err := s.GetSavedQueue(ctx, other.userID)
	if err != nil || isolated.Revision != 0 || len(isolated.Entries) != 0 {
		t.Fatalf("isolation: %+v %v", isolated, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err = s.GetSavedQueue(ctx, owner.userID)
	if err != nil || got.CurrentID != 2 || got.Position != 7.25 || len(got.Entries) != 2 {
		t.Fatalf("restart lost occurrence: %+v %v", got, err)
	}
	if err := s.DeleteUser(ctx, owner.userID); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetSavedQueue(ctx, owner.userID)
	if err != nil || got.Revision != 0 {
		t.Fatalf("deleted user retained queue: %+v %v", got, err)
	}
}
