package service

import (
	"context"
	"errors"
	"testing"

	"github.com/paul/kiss-media-player/internal/model"
	"github.com/paul/kiss-media-player/internal/repository"
)

func TestService_NoRows_ReturnsNil(t *testing.T) {
	ctx := context.Background()

	t.Run("GetMediaDetail nil media", func(t *testing.T) {
		store := &repository.MockStore{
			MediaRepo: repository.MockMediaRepo{
				GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
					return nil, nil
				},
			},
		}
		svc := NewMediaService(store, newMockClock(), "/tmp/media")
		detail, err := svc.GetMediaDetail(ctx, 99, 1)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if detail != nil {
			t.Fatalf("expected nil detail, got %+v", detail)
		}
	})

	t.Run("GetNote nil", func(t *testing.T) {
		store := &repository.MockStore{
			NoteRepo: repository.MockNoteRepo{
				GetNoteFunc: func(ctx context.Context, mediaID, userID int64) (*model.Note, error) {
					return nil, nil
				},
			},
		}
		svc := NewMediaService(store, newMockClock(), "/tmp/media")
		note, err := svc.GetNote(ctx, 1, 1)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if note != nil {
			t.Fatalf("expected nil note, got %+v", note)
		}
	})

	t.Run("AssignTag creates missing tag", func(t *testing.T) {
		store := &repository.MockStore{
			TagRepo: repository.MockTagRepo{
				GetTagByNameFunc: func(ctx context.Context, name string) (*model.Tag, error) {
					return nil, nil
				},
				CreateTagFunc: func(ctx context.Context, name string) (int64, error) {
					return 42, nil
				},
				AssignTagFunc: func(ctx context.Context, mediaID, tagID int64) error {
					return nil
				},
			},
		}
		svc := NewMediaService(store, newMockClock(), "/tmp/media")
		if err := svc.AssignTag(ctx, 1, 1, "newtag"); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("UpdateProgress with nil accumulator", func(t *testing.T) {
		store := &repository.MockStore{
			PlaybackProgressRepo: repository.MockPlaybackProgressRepo{
				UpsertProgressFunc: func(ctx context.Context, progress *model.PlaybackProgress) error {
					return nil
				},
			},
			PlaybackAccumulatorRepo: repository.MockPlaybackAccumulatorRepo{
				GetAccumulatorFunc: func(ctx context.Context, sessionID string, mediaID int64) (*model.PlaybackAccumulator, error) {
					return nil, nil
				},
				UpsertAccumulatorFunc: func(ctx context.Context, acc *model.PlaybackAccumulator) error {
					return nil
				},
			},
			MediaRepo: repository.MockMediaRepo{
				IncrementPlayCountFunc: func(ctx context.Context, id int64) error {
					return nil
				},
			},
		}
		svc := NewProgressService(store, newMockClock())
		if err := svc.UpdateProgress(ctx, "sess", 1, 10, 5); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("ValidateShareToken nil", func(t *testing.T) {
		store := &repository.MockStore{
			ShareRepo: repository.MockShareRepo{
				GetShareByTokenFunc: func(ctx context.Context, token string) (*model.Share, error) {
					return nil, nil
				},
			},
		}
		svc := NewMediaService(store, newMockClock(), "/tmp/media")
		sh, err := svc.ValidateShareToken(ctx, "nope")
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if sh != nil {
			t.Fatalf("expected nil share, got %+v", sh)
		}
	})

	t.Run("StreamSharedMedia missing share", func(t *testing.T) {
		store := &repository.MockStore{
			ShareRepo: repository.MockShareRepo{
				GetShareByTokenFunc: func(ctx context.Context, token string) (*model.Share, error) {
					return nil, nil
				},
			},
		}
		svc := NewMediaService(store, newMockClock(), "/tmp/media")
		_, err := svc.StreamSharedMedia(ctx, "nope")
		if err == nil {
			t.Fatal("expected error for missing share")
		}
	})

	t.Run("verifyAccess missing permission", func(t *testing.T) {
		store := &repository.MockStore{
			MediaRepo: repository.MockMediaRepo{
				GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
					return &model.Media{ID: 1, SetID: 1}, nil
				},
			},
			UserRepo: repository.MockUserRepo{
				GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
					return &model.User{ID: 1, IsAdmin: false}, nil
				},
			},
			SetRepo: repository.MockSetRepo{
				GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
					return &model.Set{ID: 1}, nil
				},
			},
			SetPermissionRepo: repository.MockSetPermissionRepo{
				GetPermissionFunc: func(ctx context.Context, setID, userID int64) (*model.SetPermission, error) {
					return nil, nil
				},
			},
		}
		svc := NewMediaService(store, newMockClock(), "/tmp/media")
		_, err := svc.StreamMedia(ctx, 1, 1)
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("expected ErrForbidden, got %v", err)
		}
	})
}
