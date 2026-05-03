package service

import (
	"context"
	"errors"
	"testing"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

type fakeUserHasher struct {
	fixed string
	err   error
}

func (f *fakeUserHasher) Hash(password string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.fixed, nil
}
func (f *fakeUserHasher) Compare(hash, password string) error {
	return nil
}

func TestUserAdminService_CreateUser(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		hashErr   error
		createErr error
		wantErr   bool
	}{
		{
			name: "ok",
		},
		{
			name:    "hash error",
			hashErr: errors.New("boom"),
			wantErr: true,
		},
		{
			name:      "create error",
			createErr: errors.New("boom"),
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &repository.MockStore{
				UserRepo: repository.MockUserRepo{
					CreateUserFunc: func(ctx context.Context, user *model.User) (int64, error) {
						return 1, tt.createErr
					},
				},
			}
			hasher := &fakeUserHasher{fixed: "hashed", err: tt.hashErr}
			svc := NewUserAdminService(store, clock.RealClock{}, hasher)
			user, err := svc.CreateUser(ctx, "alice", "secret", false)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if user.Username != "alice" {
				t.Fatalf("unexpected username %q", user.Username)
			}
		})
	}
}
