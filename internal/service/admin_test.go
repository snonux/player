package service

import (
	"context"
	"errors"
	"testing"

	"codeberg.org/snonux/play/internal/model"
	"codeberg.org/snonux/play/internal/repository"
)

type fakeScanner struct {
	scanFunc func(ctx context.Context, root string) error
}

func (f *fakeScanner) Scan(ctx context.Context, root string) error {
	if f.scanFunc != nil {
		return f.scanFunc(ctx, root)
	}
	return nil
}

type fakeHasher struct {
	fixed string
	err   error
}

func (f *fakeHasher) Hash(password string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.fixed, nil
}
func (f *fakeHasher) Compare(hash, password string) error {
	return nil
}

func TestAdminService_ListTrash(t *testing.T) {
	ctx := context.Background()
	store := &repository.MockStore{
		MediaRepo: repository.MockMediaRepo{
			ListDeletedMediaFunc: func(ctx context.Context) ([]model.Media, error) {
				return []model.Media{{ID: 1, FileName: "a.mp4"}}, nil
			},
		},
	}
	svc := NewAdminService(store, newMockClock(), &fakeHasher{fixed: "hash"}, nil, "")
	items, err := svc.ListTrash(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
}

func TestAdminService_TriggerRescan(t *testing.T) {
	ctx := context.Background()
	var scannedRoot string
	sc := &fakeScanner{
		scanFunc: func(_ context.Context, root string) error {
			scannedRoot = root
			return nil
		},
	}
	svc := NewAdminService(&repository.MockStore{}, newMockClock(), &fakeHasher{fixed: "hash"}, sc, "/media")
	if err := svc.TriggerRescan(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if scannedRoot != "/media" {
		t.Fatalf("expected root %q, got %q", "/media", scannedRoot)
	}
}

func TestAdminService_TriggerRescan_Error(t *testing.T) {
	ctx := context.Background()
	sc := &fakeScanner{
		scanFunc: func(_ context.Context, _ string) error {
			return errors.New("scan failed")
		},
	}
	svc := NewAdminService(&repository.MockStore{}, newMockClock(), &fakeHasher{fixed: "hash"}, sc, "/media")
	err := svc.TriggerRescan(ctx)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAdminService_TriggerRescan_NilScanner(t *testing.T) {
	ctx := context.Background()
	svc := NewAdminService(&repository.MockStore{}, newMockClock(), &fakeHasher{fixed: "hash"}, nil, "")
	err := svc.TriggerRescan(ctx)
	if err == nil {
		t.Fatal("expected error when scanner is nil")
	}
}

func TestAdminService_ListUsers(t *testing.T) {
	ctx := context.Background()
	store := &repository.MockStore{
		UserRepo: repository.MockUserRepo{
			ListUsersFunc: func(ctx context.Context) ([]model.User, error) {
				return []model.User{{ID: 1, Username: "alice"}}, nil
			},
		},
	}
	svc := NewAdminService(store, newMockClock(), &fakeHasher{fixed: "hash"}, nil, "")
	users, err := svc.ListUsers(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
}

func TestAdminService_CreateUser(t *testing.T) {
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
			hasher := &fakeHasher{fixed: "hashed", err: tt.hashErr}
			svc := NewAdminService(store, newMockClock(), hasher, nil, "")
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

func TestAdminService_DeleteUser(t *testing.T) {
	ctx := context.Background()
	var called bool
	store := &repository.MockStore{
		UserRepo: repository.MockUserRepo{
			DeleteUserFunc: func(ctx context.Context, id int64) error {
				called = true
				return nil
			},
		},
	}
	svc := NewAdminService(store, newMockClock(), &fakeHasher{fixed: "hash"}, nil, "")
	if err := svc.DeleteUser(ctx, 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected delete called")
	}
}

func TestAdminService_ListPermissions(t *testing.T) {
	ctx := context.Background()
	store := &repository.MockStore{
		SetRepo: repository.MockSetRepo{
			ListSetsFunc: func(ctx context.Context) ([]model.Set, error) {
				return []model.Set{{ID: 1}, {ID: 2}}, nil
			},
		},
		SetPermissionRepo: repository.MockSetPermissionRepo{
			ListPermissionsBySetFunc: func(ctx context.Context, setID int64) ([]model.SetPermission, error) {
				return []model.SetPermission{{SetID: setID, UserID: int64(setID) + 10}}, nil
			},
		},
	}
	svc := NewAdminService(store, newMockClock(), &fakeHasher{fixed: "hash"}, nil, "")
	perms, err := svc.ListPermissions(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(perms) != 2 {
		t.Fatalf("expected 2 permissions, got %d", len(perms))
	}
}

func TestAdminService_GrantPermission(t *testing.T) {
	ctx := context.Background()
	var granted *model.SetPermission
	store := &repository.MockStore{
		SetPermissionRepo: repository.MockSetPermissionRepo{
			GrantPermissionFunc: func(ctx context.Context, perm *model.SetPermission) error {
				granted = perm
				return nil
			},
		},
	}
	svc := NewAdminService(store, newMockClock(), &fakeHasher{fixed: "hash"}, nil, "")
	if err := svc.GrantPermission(ctx, 1, 2, model.RoleViewer); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if granted == nil {
		t.Fatal("expected permission granted")
	}
	if granted.Role != model.RoleViewer {
		t.Fatalf("unexpected role %q", granted.Role)
	}
}

func TestAdminService_RevokePermission(t *testing.T) {
	ctx := context.Background()
	var revoked bool
	store := &repository.MockStore{
		SetPermissionRepo: repository.MockSetPermissionRepo{
			RevokePermissionFunc: func(ctx context.Context, setID, userID int64) error {
				revoked = true
				return nil
			},
		},
	}
	svc := NewAdminService(store, newMockClock(), &fakeHasher{fixed: "hash"}, nil, "")
	if err := svc.RevokePermission(ctx, 1, 2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !revoked {
		t.Fatal("expected revoke called")
	}
}
