package service

import (
	"context"
	"errors"
	"testing"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

func TestNoteService_GetNote_AccessCheck(t *testing.T) {
	ctx := context.Background()
	store := &repository.MockStore{
		MediaRepo: repository.MockMediaRepo{
			GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
				return &model.Media{ID: 1, SetID: 1}, nil
			},
		},
		UserRepo: repository.MockUserRepo{
			GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
				return &model.User{ID: id, IsAdmin: false}, nil
			},
		},
		SetRepo: repository.MockSetRepo{
			GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
				return &model.Set{ID: id}, nil
			},
		},
		SetPermissionRepo: repository.MockSetPermissionRepo{
			GetPermissionFunc: func(ctx context.Context, setID, userID int64) (*model.SetPermission, error) {
				return nil, nil
			},
		},
	}
	svc := NewNoteService(store, clock.RealClock{}, &accessHelper{store: store})
	_, err := svc.GetNote(ctx, 1, 2)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestNoteService_SetNote(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		method    string
		wantErr   bool
		noteErr   error
		upsertErr error
		deleteErr error
	}{
		{name: "get note", method: "get"},
		{name: "get note error", method: "get", wantErr: true, noteErr: errors.New("boom")},
		{name: "upsert note", method: "upsert"},
		{name: "upsert error", method: "upsert", wantErr: true, upsertErr: errors.New("boom")},
		{name: "delete note", method: "delete"},
		{name: "delete error", method: "delete", wantErr: true, deleteErr: errors.New("boom")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &repository.MockStore{
				MediaRepo: repository.MockMediaRepo{
					GetMediaByIDFunc: func(ctx context.Context, id int64) (*model.Media, error) {
						return &model.Media{ID: 1, SetID: 1}, nil
					},
				},
				UserRepo: repository.MockUserRepo{
					GetUserByIDFunc: func(ctx context.Context, id int64) (*model.User, error) {
						return &model.User{ID: id, IsAdmin: true}, nil
					},
				},
				SetRepo: repository.MockSetRepo{
					GetSetByIDFunc: func(ctx context.Context, id int64) (*model.Set, error) {
						return &model.Set{ID: id}, nil
					},
				},
				NoteRepo: repository.MockNoteRepo{
					GetNoteFunc: func(ctx context.Context, mediaID, userID int64) (*model.Note, error) {
						return &model.Note{MediaID: mediaID, UserID: userID, Content: "hello"}, tt.noteErr
					},
					UpsertNoteFunc: func(ctx context.Context, note *model.Note) error {
						return tt.upsertErr
					},
					DeleteNoteFunc: func(ctx context.Context, mediaID, userID int64) error {
						return tt.deleteErr
					},
				},
			}
			svc := NewNoteService(store, clock.RealClock{}, &accessHelper{store: store})
			var err error
			switch tt.method {
			case "get":
				_, err = svc.GetNote(ctx, 1, 1)
			case "upsert":
				err = svc.UpsertNote(ctx, &model.Note{MediaID: 1, UserID: 1, Content: "hi"})
			case "delete":
				err = svc.DeleteNote(ctx, 1, 1)
			}
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
