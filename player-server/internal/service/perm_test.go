package service

import (
	"context"
	"errors"
	"testing"

	"codeberg.org/snonux/player/internal/clock"
	"codeberg.org/snonux/player/internal/model"
	"codeberg.org/snonux/player/internal/repository"
)

func TestPermissionAdminService_GrantPermission(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		storeErr error
		wantErr  bool
	}{
		{
			name: "ok",
		},
		{
			name:     "store error",
			storeErr: errors.New("boom"),
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var granted *model.SetPermission
			store := &repository.MockStore{
				SetPermissionRepo: repository.MockSetPermissionRepo{
					GrantPermissionFunc: func(ctx context.Context, perm *model.SetPermission) error {
						granted = perm
						return tt.storeErr
					},
				},
			}
			svc := NewPermissionAdminService(store, clock.RealClock{})
			err := svc.GrantPermission(ctx, 1, 2, model.RoleViewer)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if granted == nil {
				t.Fatal("expected permission granted")
			}
			if granted.Role != model.RoleViewer {
				t.Fatalf("unexpected role %q", granted.Role)
			}
		})
	}
}
